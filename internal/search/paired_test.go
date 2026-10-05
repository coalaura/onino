package search

import (
	"bytes"
	"context"
	"crypto/sha3"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"math/rand/v2"
	"testing"

	"filippo.io/edwards25519"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestPairedFormula(t *testing.T) {
	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())
	negative := new(edwards25519.Point).Negate(step)

	references := []*edwards25519.Point{edwards25519.NewIdentityPoint(), step, negative, new(edwards25519.Point).Add(step, step)}

	encodings := [][32]byte{{}, {1}, {0xec, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f}}

	for _, encoded := range encodings {
		point, err := new(edwards25519.Point).SetBytes(encoded[:])
		if err != nil {
			t.Fatal(err)
		}

		references = append(references, point, new(edwards25519.Point).Negate(point))
	}

	state := testPairedGenerator(t)

	for index, reference := range references {
		state.centers[index].set(reference)
	}

	offset := edwards25519.NewIdentityPoint()

	for position := range pairedOffsets {
		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		offset.Add(offset, step)

		for index, reference := range references {
			for side := range 2 {
				candidate := new(edwards25519.Point)

				if side == 0 {
					candidate.Add(reference, offset)
				} else {
					candidate.Subtract(reference, offset)
				}

				state.completeSign(index*2 + side)

				if !bytes.Equal(candidate.Bytes(), state.publicKeys[index*2+side][:]) {
					t.Fatalf("pair formula: center %d offset %d side %d", index, position+1, side)
				}
			}
		}
	}
}

func TestPairedReciprocals(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	values := []fieldElement{
		{},
		{2, 0, 0, 0},
		{0xffffffffffffffff, 0, 0, 0},
		{0, 0, 0, 0x8000000000000000},
		{0xffffffffffffffed, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffda, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
	}

	state := new(pairedGenerator)

	offset := pairedAffine{xy: pairedOne}

	random := rand.New(rand.NewPCG(37, 91))

	for round := range 8 {
		for index := range state.centers {
			value := fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()}

			if round == 0 && index < len(values) {
				value = values[index]
			}

			state.centers[index].xy = value
		}

		state.prepare(&offset)

		for index := range state.centers {
			coupling := fieldInteger(state.centers[index].xy)

			denominator := new(big.Int).Sub(big.NewInt(1), coupling)

			expected := new(big.Int).ModInverse(denominator, prime)
			if expected == nil {
				t.Fatal("unexpected zero denominator")
			}

			checkFieldResult(t, "plus reciprocal", &state.scratch[index].plusInverse, expected)

			denominator.Add(big.NewInt(1), coupling)

			expected = expected.ModInverse(denominator, prime)
			if expected == nil {
				t.Fatal("unexpected zero denominator")
			}

			checkFieldResult(t, "minus reciprocal", &state.scratch[index].minusInverse, expected)
		}
	}
}

func TestRunPairedCancellation(t *testing.T) {
	patterns := make([]string, 0, 64)

	for _, symbol := range "abcdefghijklmnopqrstuvwxyz234567" {
		patterns = append(patterns, "."+string(symbol)+"a", "."+string(symbol)+"q")
	}

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	if matcher.SignFilter() == nil {
		t.Fatal("test must exercise paired generation")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stats, err := Run(ctx, matcher, func(key onion.Key) error {
		checkKey(t, &key)
		cancel()

		return nil
	})

	if !errors.Is(err, context.Canceled) || stats.Checked != batchSize || stats.Saved != batchSize {
		t.Fatalf("paired cancellation did not finish exactly one batch: %+v, %v", stats, err)
	}
}

func TestPairedKeys(t *testing.T) {
	state := testPairedGenerator(t)

	for batch := range pairedOffsets*2 + 2 {
		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		for index := range state.publicKeys {
			state.completeSign(index)

			checkPairedKey(t, state.key(index))
		}

		if batch%17 == 0 {
			err = state.reseed(3)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestIndependentShortSearch(t *testing.T) {
	patterns := []string{"bc", ".bc."}

	for _, text := range patterns {
		matcher, err := pattern.CompilePatterns([]string{text})
		if err != nil {
			t.Fatal(err)
		}

		state, err := newWorker(sha3.NewSHAKE256(), matcher)
		if err != nil {
			t.Fatal(err)
		}

		if state.walk == nil || matcher.SignFilter() == nil {
			t.Fatal("short searches need independent seeds while retaining deferred signs")
		}
	}
}

func TestPairedBoundaryScalars(t *testing.T) {
	state := testPairedGenerator(t)

	var (
		low  [64]byte
		high [64]byte
	)

	low[31] = 64

	for index := range 32 {
		high[index] = 255
	}

	high[0] = 248
	high[31] = 127

	// Leave exactly the promised epoch headroom below the last clamped scalar.
	binary.LittleEndian.PutUint64(high[:8], ^uint64(7)-reseedRounds*8)

	secrets := [][64]byte{low, high}

	for index, secret := range secrets {
		state.secrets[index] = secret
		state.steps[index] = reseedRounds - pairedOffsets

		centerSecret := offsetSecret(secret, state.steps[index])

		scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(centerSecret[:32])
		if err != nil {
			t.Fatal(err)
		}

		state.centers[index].set(new(edwards25519.Point).ScalarBaseMult(scalar))
	}

	for range pairedOffsets + 1 {
		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		for index := range len(secrets) * 2 {
			state.completeSign(index)

			checkPairedKey(t, state.key(index))
		}
	}

	for index := range secrets {
		if state.steps[index] != pairedOffsets || state.secrets[index] == secrets[index] {
			t.Fatal("expired center was not independently reseeded")
		}
	}
}

func TestPairedHitInvalidation(t *testing.T) {
	state := testPairedGenerator(t)

	matcher, err := pattern.CompilePatterns([]string{".a", ".q"})
	if err != nil {
		t.Fatal(err)
	}

	keys := make([]onion.Key, 0, batchSize*2)
	seeds := make(map[[32]byte]bool, batchSize*2)

	save := func(key onion.Key) error {
		checkKey(t, &key)

		nonce := [32]byte(key.Secret[32:])
		if seeds[nonce] {
			t.Fatal("exported two keys derived from the same seed")
		}

		seeds[nonce] = true
		keys = append(keys, key)

		return nil
	}

	var stats Stats

	for range 2 {
		err = state.searchBatch(matcher, save, &stats)
		if err != nil {
			t.Fatal(err)
		}
	}

	if stats.Checked != batchSize*2 || stats.Saved != stats.Checked || len(keys) != batchSize*2 || state.position != 4 {
		t.Fatalf("discarded candidates counted or not replenished: %+v position %d", stats, state.position)
	}

	for _, key := range keys {
		checkPairedKey(t, key)
	}
}

func TestPairedAllocations(t *testing.T) {
	state := testPairedGenerator(t)

	matcher, err := pattern.CompilePatterns([]string{"ab."})
	if err != nil {
		t.Fatal(err)
	}

	var stats Stats

	allocations := testing.AllocsPerRun(100, func() {
		err = state.searchBatch(matcher, benchmarkSave, &stats)
		if err != nil {
			t.Fatal(err)
		}
	})

	if allocations != 0 {
		t.Fatalf("paired search allocated %v times", allocations)
	}
}

func TestPairedPendingSides(t *testing.T) {
	encoding := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

	for side := range 3 {
		state := testPairedGenerator(t)

		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		index := side % 2
		state.completeSign(index)

		expected := state.key(index)
		patternKey := expected.Public

		if side == 2 {
			patternKey[31] ^= 128
		}

		matcher, compileErr := pattern.CompilePatterns([]string{encoding.EncodeToString(patternKey[:]) + "."})
		if compileErr != nil {
			t.Fatal(compileErr)
		}

		var stats Stats

		save := func(key onion.Key) error {
			if key != expected {
				t.Fatal("saved the wrong pending side")
			}

			checkKey(t, &key)

			return nil
		}

		err = state.searchBatch(matcher, save, &stats)
		if err != nil {
			t.Fatal(err)
		}

		wantSaved := uint64(1)

		if side == 2 {
			wantSaved = 0
		}

		if stats.Checked != batchSize || stats.Saved != wantSaved {
			t.Fatalf("incorrect side/sign accounting: side %d, %+v", side, stats)
		}

		reseeded := !bytes.Equal(state.secrets[0][32:], expected.Secret[32:])
		if reseeded != (side != 2) {
			t.Fatal("seed changed without a hit or survived a saved hit")
		}
	}
}

func TestPairedErrors(t *testing.T) {
	state := &pairedGenerator{random: bytes.NewReader(nil)}

	err := state.reset()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("paired reset: %v", err)
	}

	matcher, err := pattern.CompilePatterns([]string{".a", ".q"})
	if err != nil {
		t.Fatal(err)
	}

	state = testPairedGenerator(t)

	saveError := errors.New("save failure")

	var stats Stats

	err = state.searchBatch(matcher, func(key onion.Key) error {
		return saveError
	}, &stats)

	if !errors.Is(err, saveError) || stats.Checked != 1 || stats.Saved != 0 {
		t.Fatalf("paired save error: %+v, %v", stats, err)
	}

	state = testPairedGenerator(t)
	state.random = bytes.NewReader(nil)

	stats = Stats{}

	err = state.searchBatch(matcher, benchmarkSave, &stats)

	if !errors.Is(err, io.EOF) || stats.Checked != 1 || stats.Saved != 1 {
		t.Fatalf("paired reseed error: %+v, %v", stats, err)
	}
}

func FuzzPaired(f *testing.F) {
	f.Add(make([]byte, 32), uint8(0))
	f.Add(bytes.Repeat([]byte{255}, 32), uint8(63))

	state := &pairedGenerator{}

	for index := range state.centers {
		state.centers[index].y = pairedOne
	}

	f.Fuzz(func(t *testing.T, input []byte, position uint8) {
		if len(input) != 32 {
			return
		}

		scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(input)
		if err != nil {
			t.Fatal(err)
		}

		reference := new(edwards25519.Point).ScalarBaseMult(scalar)

		state.centers[0].set(reference)
		state.position = int(position) % pairedOffsets

		offsetScalar := [32]byte{}

		binary.LittleEndian.PutUint64(offsetScalar[:], uint64(state.position+1)*8)

		stepScalar, err := new(edwards25519.Scalar).SetCanonicalBytes(offsetScalar[:])
		if err != nil {
			t.Fatal(err)
		}

		offset := new(edwards25519.Point).ScalarBaseMult(stepScalar)

		err = state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		for side := range 2 {
			candidate := new(edwards25519.Point)

			if side == 0 {
				candidate.Add(reference, offset)
			} else {
				candidate.Subtract(reference, offset)
			}

			state.completeSign(side)

			if !bytes.Equal(candidate.Bytes(), state.publicKeys[side][:]) {
				t.Fatal("paired fuzz mismatch")
			}
		}
	})
}

func BenchmarkPaired(b *testing.B) {
	cases := []benchmarkCase{
		{name: "rare", patterns: []string{"somethingrare."}},
		{name: "frequent", patterns: []string{"ab."}},
		{name: "all_hits", patterns: []string{".a", ".q"}},
		{name: "512", patterns: benchmarkDictionary(512, false)},
		{name: "shared512", patterns: benchmarkDictionary(512, true)},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			state := testPairedGenerator(b)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			var stats Stats

			b.ReportAllocs()

			for b.Loop() {
				err = state.searchBatch(matcher, benchmarkSave, &stats)
				if err != nil {
					b.Fatal(err)
				}
			}

			if stats.Checked != uint64(b.N)*batchSize || test.name == "all_hits" && stats.Saved != stats.Checked {
				b.Fatalf("lost candidates: %+v", stats)
			}

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func BenchmarkEngineCrossover(b *testing.B) {
	cases := []benchmarkCase{
		{name: "prefix1", patterns: []string{"a."}},
		{name: "prefix2", patterns: []string{"ab."}},
		{name: "suffix2", patterns: []string{".aa"}},
		{name: "anywhere2", patterns: []string{"bc"}},
		{name: "anywhere3", patterns: []string{"abc"}},
		{name: "interior2", patterns: []string{".bc."}},
	}

	engines := []string{"walk", "paired"}

	for _, test := range cases {
		for _, engine := range engines {
			b.Run(test.name+"/"+engine, func(b *testing.B) {
				matcher, err := pattern.CompilePatterns(test.patterns)
				if err != nil {
					b.Fatal(err)
				}

				paired := testPairedGenerator(b)
				walk := testGenerator(b)

				var stats Stats

				b.ReportAllocs()

				for b.Loop() {
					if engine == "walk" {
						err = walk.searchBatch(matcher, benchmarkSave, &stats)
					} else {
						err = paired.searchBatch(matcher, benchmarkSave, &stats)
					}

					if err != nil {
						b.Fatal(err)
					}
				}

				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
			})
		}
	}
}

func BenchmarkPairedGeneration(b *testing.B) {
	cases := []string{"ordinate", "complete"}

	for _, name := range cases {
		b.Run(name, func(b *testing.B) {
			state := testPairedGenerator(b)
			b.ReportAllocs()

			for b.Loop() {
				err := state.nextBatch()
				if err != nil {
					b.Fatal(err)
				}

				if name == "complete" {
					for index := range state.publicKeys {
						state.completeSign(index)
					}
				}
			}

			benchmarkPublic = state.publicKeys[0]

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/batchSize, "ns/key")
		})
	}
}

func testPairedGenerator(t testing.TB) *pairedGenerator {
	t.Helper()

	state := &pairedGenerator{random: sha3.NewSHAKE256()}

	err := state.reset()
	if err != nil {
		t.Fatal(err)
	}

	return state
}

func checkPairedKey(t testing.TB, key onion.Key) {
	t.Helper()

	if key.Secret[0]&7 != 0 || key.Secret[31]&192 != 64 {
		t.Fatal("paired scalar is not clamped")
	}

	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(key.Secret[:32])
	if err != nil {
		t.Fatal(err)
	}

	reference := new(edwards25519.Point).ScalarBaseMult(scalar)
	if !bytes.Equal(reference.Bytes(), key.Public[:]) {
		t.Fatal("paired key differs from independent scalar multiplication")
	}
}
