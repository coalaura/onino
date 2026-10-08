//go:build !purego

package search

import (
	"bytes"
	"crypto/sha3"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"filippo.io/edwards25519"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

func newWorkerWithSIMD(random io.Reader, matcher *pattern.Matcher, features simd.Features) (*worker, error) {
	config, err := resolveConfiguration(simd.Auto, matcher, features, scalarAssemblyAvailable, vectorAssemblyAvailable)
	if err != nil {
		return nil, err
	}

	return newWorkerWithConfig(random, matcher, config)
}

func TestSIMDSelection(t *testing.T) {
	patterns := []string{"somethingrare.", ".a", ".aa"}

	for _, text := range patterns {
		matcher, err := pattern.CompilePatterns([]string{text})
		if err != nil {
			t.Fatal(err)
		}

		features := simd.Detect()

		state, err := newWorkerWithSIMD(sha3.NewSHAKE256(), matcher, features)
		if err != nil {
			t.Fatal(err)
		}

		expectedIFMA := features.IFMA && (ifmaLanes == 8 || features.VL) && !matcher.PreferIndependent()
		expectedChecksum := features.Keccak && matcher.UsesChecksum()

		expected := expectedIFMA || expectedChecksum
		if (state.accelerated != nil) != expected {
			t.Fatalf("wrong backend for %s", text)
		}

		if expected && (state.paired != nil || state.walk != nil) {
			t.Fatal("allocated unselected backend")
		}

		if expected {
			selected := state.accelerated
			if (selected.generator != nil) != expectedIFMA {
				t.Fatal("wrong IFMA state allocation")
			}

			if expectedIFMA && (selected.paired != nil || selected.walk != nil) {
				t.Fatal("allocated legacy state with IFMA")
			}

			if !expectedIFMA && (selected.walk != nil) != matcher.PreferIndependent() {
				t.Fatal("wrong checksum generator")
			}
		}

		state, err = newWorkerWithSIMD(sha3.NewSHAKE256(), matcher, simd.Features{})
		if err != nil || state.accelerated != nil {
			t.Fatalf("no reported capabilities: %v", err)
		}

		features.IFMA = false

		state, err = newWorkerWithSIMD(sha3.NewSHAKE256(), matcher, features)
		if err != nil || (state.accelerated != nil) != expectedChecksum {
			t.Fatalf("missing IFMA: %v", err)
		}

		if state.accelerated != nil && state.accelerated.generator != nil {
			t.Fatal("allocated IFMA state without IFMA")
		}
	}
}

func TestSIMDChecksumWorkers(t *testing.T) {
	features := simd.Detect()
	if !features.Keccak {
		t.Skip("AVX-512 checksum unavailable")
	}

	withoutIFMA := features
	withoutIFMA.IFMA = false

	variants := []simd.Features{features, withoutIFMA}
	patterns := [][]string{{".a"}, {".aa"}, {".aaa"}, {"ab.", ".aa"}, allSuffixPatterns(1)}

	for _, variant := range variants {
		for _, texts := range patterns {
			matcher, err := pattern.CompilePatterns(texts)
			if err != nil {
				t.Fatal(err)
			}

			original, err := newWorker(sha3.NewSHAKE256(), matcher)
			if err != nil {
				t.Fatal(err)
			}

			selected, err := newWorkerWithSIMD(sha3.NewSHAKE256(), matcher, variant)
			if err != nil {
				t.Fatal(err)
			}

			var (
				originalStats Stats
				selectedStats Stats
			)

			keys := make([]onion.Key, 0, batchSize)

			for range 10 {
				keys = keys[:0]

				err = original.searchBatch(matcher, func(key onion.Key) error {
					keys = append(keys, key)

					return nil
				}, &originalStats)

				if err != nil {
					t.Fatal(err)
				}

				cursor := 0

				err = selected.searchBatch(matcher, func(key onion.Key) error {
					if cursor >= len(keys) || key != keys[cursor] {
						t.Fatal("checksum worker changed key or order")
					}

					checkKey(t, &key)
					checkPairedKey(t, key)

					cursor++

					return nil
				}, &selectedStats)

				if err != nil {
					t.Fatal(err)
				}

				if cursor != len(keys) || originalStats != selectedStats {
					t.Fatal("checksum worker accounting differs")
				}
			}
		}
	}
}

func TestIFMAEpochAndErrors(t *testing.T) {
	state := testIFMAGenerator(t)

	var low [64]byte

	low[31] = 64

	var high [64]byte

	for index := range 32 {
		high[index] = 255
	}

	high[0] = 248
	high[31] = 127

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

		var center pairedAffine

		center.set(new(edwards25519.Point).ScalarBaseMult(scalar))
		state.centers[0].x.setLane(index, &center.x)
		state.centers[0].y.setLane(index, &center.y)
		state.centers[0].xy.setLane(index, &center.xy)
	}

	for range pairedOffsets + 1 {
		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		for index := range 4 {
			state.completeSign(index)

			checkPairedKey(t, state.snapshot(index).key())
		}
	}

	for index := range secrets {
		if state.steps[index] != pairedOffsets || state.secrets[index] == secrets[index] {
			t.Fatal("expired center not reseeded")
		}
	}

	matcher, err := pattern.CompilePatterns(allSuffixPatterns(1))
	if err != nil {
		t.Fatal(err)
	}

	saveError := errors.New("save failure")

	state = testIFMAGenerator(t)

	var stats Stats

	err = state.searchBatch(matcher, func(key onion.Key) error {
		return saveError
	}, &stats)

	if !errors.Is(err, saveError) || stats.Checked != 1 || stats.Saved != 0 {
		t.Fatalf("save error: %+v %v", stats, err)
	}

	state = testIFMAGenerator(t)
	state.random = bytes.NewReader(nil)

	stats = Stats{}

	err = state.searchBatch(matcher, benchmarkSave, &stats)

	if !errors.Is(err, io.EOF) || stats.Checked != 1 || stats.Saved != 1 {
		t.Fatalf("reseed error: %+v %v", stats, err)
	}
}

func FuzzIFMA(f *testing.F) {
	features := simd.Detect()
	if !features.IFMA || ifmaLanes == 4 && !features.VL {
		f.Skip("IFMA unavailable")
	}

	f.Add(make([]byte, 64))
	f.Add(bytes.Repeat([]byte{255}, 64))

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) != 64 {
			return
		}

		var (
			left  fieldElement
			right fieldElement
		)

		for index := range 4 {
			left[index] = binary.LittleEndian.Uint64(input[index*8:])
			right[index] = binary.LittleEndian.Uint64(input[32+index*8:])
		}

		vectorLeft := broadcastIFMA(&left)
		vectorRight := broadcastIFMA(&right)

		ifmaMultiply(&vectorLeft, &vectorLeft, &vectorRight)

		left.multiply(&left, &right)

		var expected [32]byte

		left.putBytes(&expected)

		for lane := range ifmaLanes {
			actual := vectorLeft.lane(lane)

			var encoded [32]byte

			actual.putBytes(&encoded)

			if encoded != expected {
				t.Fatal("IFMA mismatch")
			}
		}
	})
}
