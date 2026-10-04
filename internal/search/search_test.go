package search

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha3"
	"crypto/sha512"
	"errors"
	"io"
	"math/big"
	"slices"
	"testing"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestBatchesAgainstScalarMultiplication(t *testing.T) {
	state := testGenerator(t)

	for batch := range 16 {
		state.next()

		for index := range state.publicKeys {
			key := state.key(index)
			checkKey(t, &key)
		}

		if batch == 5 {
			err := state.reseed(13)
			if err != nil {
				t.Fatal(err)
			}
		}
	}

	err := state.reset()
	if err != nil {
		t.Fatal(err)
	}

	state.next()

	for index := range state.publicKeys {
		key := state.key(index)
		checkKey(t, &key)
	}
}

func TestFixedStepExceptionalPoints(t *testing.T) {
	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())
	references := []*edwards25519.Point{
		edwards25519.NewIdentityPoint(),
		new(edwards25519.Point).Set(step),
		new(edwards25519.Point).Negate(step),
		new(edwards25519.Point).Add(step, step),
	}
	points := make([]extendedPoint, len(references))
	products := make([]field.Element, len(references))
	publicKeys := make([][32]byte, len(references))

	for index := range points {
		points[index].set(references[index])
	}

	for range 3 {
		generatePoints(points, products, publicKeys)

		for index := range references {
			references[index].Add(references[index], step)

			if !bytes.Equal(publicKeys[index][:], references[index].Bytes()) {
				t.Fatalf("incorrect complete addition for point %d", index)
			}
		}
	}
}

func TestOffsetSecretCarries(t *testing.T) {
	for boundary := 0; boundary <= 31; boundary++ {
		var secret [64]byte

		for index := range boundary {
			secret[index] = 255
		}

		secret[0] &= 248
		secret[31] = 64
		copy(secret[32:], bytes.Repeat([]byte{0xa5}, 32))
		steps := []uint64{0, 1, 63, reseedRounds}

		for _, count := range steps {
			actual := offsetSecret(secret, count)
			bigEndian := slices.Clone(secret[:32])
			slices.Reverse(bigEndian)
			expected := new(big.Int).SetBytes(bigEndian)
			expected.Add(expected, new(big.Int).SetUint64(count*8))
			expected.FillBytes(bigEndian)
			slices.Reverse(bigEndian)

			if !bytes.Equal(actual[:32], bigEndian) || !bytes.Equal(actual[32:], secret[32:]) {
				t.Fatalf("incorrect scalar carry at byte %d, step %d", boundary, count)
			}
		}
	}
}

func TestRunKeepsEveryMatch(t *testing.T) {
	matcher, err := pattern.CompilePatterns([]string{".a", ".q"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keys := make([]onion.Key, 0, batchSize*2)

	stats, err := Run(ctx, matcher, func(key onion.Key) error {
		checkKey(t, &key)

		keys = append(keys, key)
		if len(keys) == batchSize+1 {
			cancel()
		}

		return nil
	})

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run returned %v", err)
	}

	if stats.Checked != batchSize*2 || stats.Saved != batchSize*2 {
		t.Fatalf("lost a candidate or match: %+v", stats)
	}

	seen := make(map[[32]byte]bool, len(keys))

	for index := range keys {
		key := &keys[index]
		if seen[key.Public] {
			t.Fatal("duplicate public key")
		}

		seen[key.Public] = true

		if index >= batchSize && bytes.Equal(key.Secret[32:], keys[index-batchSize].Secret[32:]) {
			t.Fatal("matching lane was not independently reseeded")
		}
	}
}

func TestRunCancellationAndSaveError(t *testing.T) {
	matcher, err := pattern.CompilePatterns([]string{".a", ".q"})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false

	stats, err := Run(ctx, matcher, func(key onion.Key) error {
		called = true

		return nil
	})

	if !errors.Is(err, context.Canceled) || called || stats != (Stats{}) {
		t.Fatalf("canceled search: %+v, %v, called=%v", stats, err, called)
	}

	saveError := errors.New("disk full")

	stats, err = Run(context.Background(), matcher, func(key onion.Key) error {
		return saveError
	})

	if !errors.Is(err, saveError) || stats.Checked != 1 || stats.Saved != 0 {
		t.Fatalf("save failure: %+v, %v", stats, err)
	}
}

func TestSeedingFailure(t *testing.T) {
	state := generator{random: bytes.NewReader(nil)}

	err := state.reset()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expected entropy error, got %v", err)
	}
}

func TestSearchBatchAllocations(t *testing.T) {
	state := testGenerator(t)

	matcher, err := pattern.CompilePatterns([]string{"somewhere", "start.enda"})
	if err != nil {
		t.Fatal(err)
	}

	allocations := testing.AllocsPerRun(100, func() {
		state.next()

		for index := range state.publicKeys {
			matcher.Match(state.publicKeys[index])
		}
	})

	if allocations != 0 {
		t.Fatalf("search batch allocated %v times", allocations)
	}
}

func testGenerator(tb testing.TB) *generator {
	tb.Helper()

	state := &generator{random: sha3.NewSHAKE256()}

	err := state.reset()
	if err != nil {
		tb.Fatal(err)
	}

	return state
}

func checkKey(t *testing.T, key *onion.Key) {
	t.Helper()

	if key.Secret[0]&7 != 0 || key.Secret[31]&192 != 64 {
		t.Fatal("secret scalar is not clamped")
	}

	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(key.Secret[:32])
	if err != nil {
		t.Fatal(err)
	}

	reference := new(edwards25519.Point).ScalarBaseMult(scalar)
	if !bytes.Equal(reference.Bytes(), key.Public[:]) {
		t.Fatal("incremental public key differs from scalar multiplication")
	}

	// Independently verify expanded-secret interoperability with Go's Ed25519
	// verifier. The signing construction is RFC 8032, without a seed shortcut.
	message := []byte("onino expanded secret interoperability")

	nonceHash := sha512.New()

	nonceHash.Write(key.Secret[32:])
	nonceHash.Write(message)

	nonce, err := new(edwards25519.Scalar).SetUniformBytes(nonceHash.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}

	noncePoint := new(edwards25519.Point).ScalarBaseMult(nonce)

	var signature [64]byte

	copy(signature[:32], noncePoint.Bytes())

	challengeHash := sha512.New()

	challengeHash.Write(signature[:32])
	challengeHash.Write(key.Public[:])
	challengeHash.Write(message)

	challenge, err := new(edwards25519.Scalar).SetUniformBytes(challengeHash.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}

	response := new(edwards25519.Scalar).MultiplyAdd(challenge, scalar, nonce)

	copy(signature[32:], response.Bytes())

	if !ed25519.Verify(key.Public[:], message, signature[:]) {
		t.Fatal("expanded secret does not produce valid Ed25519 signatures")
	}
}
