//go:build !purego

package search

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"filippo.io/edwards25519"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestIFMACanonicalFilter(t *testing.T) {
	state := testIFMAGenerator(t)

	plan := pattern.PrefixPlan{Count: 1}

	for iteration := range 1000 {
		value := state.centers[iteration%len(state.centers)].y

		if iteration < 32 {
			for limb := range value {
				for lane := range ifmaLanes {
					value[limb][lane] = radixMask
				}
			}

			for lane := range ifmaLanes {
				value[0][lane] -= uint64(iteration + lane)
			}
		}

		original := value
		word := original.lane(iteration % ifmaLanes)

		var encoded [32]byte

		word.putBytes(&encoded)

		plan.Probes[0] = pattern.PrefixProbe{Mask: ^uint64(0), Value: binary.LittleEndian.Uint64(encoded[:8])}

		mask := ifmaCanonicalFilter(&value, &plan)

		for lane := range ifmaLanes {
			word = original.lane(lane)
			word.putBytes(&encoded)
			canonical := value.lane(lane)

			for index := range canonical {
				if canonical[index] != binary.LittleEndian.Uint64(encoded[index*8:]) {
					t.Fatal("not canonical")
				}
			}

			want := binary.LittleEndian.Uint64(encoded[:8]) == plan.Probes[0].Value
			if (mask>>lane&1 != 0) != want {
				t.Fatal("mask mismatch")
			}
		}
	}
}

func TestIFMAFilteredSearch(t *testing.T) {
	cases := simdSearchCases()[:3]
	cases = append(cases, benchmarkCase{name: "long", patterns: []string{"abcdefghijklmnopqrstuvwxyz234567abcdefghijklmnopqr."}})

	fixture := testPairedGenerator(t)

	err := fixture.nextBatch()
	if err != nil {
		t.Fatal(err)
	}

	fixture.completeSign(0)
	key := fixture.snapshot(0).key()
	longPrefix := key.Hostname()[:50]

	cases = append(cases, benchmarkCase{name: "long_hit", patterns: []string{longPrefix + "."}})

	// Preserve the filter word while making the full prefix fail.
	rejected := []byte(longPrefix)
	rejected[49] = 'a'

	if longPrefix[49] == 'a' {
		rejected[49] = 'b'
	}

	cases = append(cases, benchmarkCase{name: "long_reject", patterns: []string{string(rejected) + "."}})

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			vector := testIFMAGenerator(t)
			scalar := testPairedGenerator(t)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				t.Fatal(err)
			}

			vector.plan = matcher.PrefixPlan()
			if vector.plan.Count == 0 {
				t.Fatal("missing plan")
			}

			for range 130 {
				err = scalar.nextBatch()
				if err != nil {
					t.Fatal(err)
				}

				err = vector.nextFilteredBatch()
				if err != nil {
					t.Fatal(err)
				}

				for index := range batchSize {
					word := binary.LittleEndian.Uint64(scalar.publicKeys[index][:8])
					want := false

					for _, probe := range vector.plan.Probes[:vector.plan.Count] {
						want = want || word&probe.Mask == probe.Value
					}

					got := vector.masks[index/(ifmaLanes*2)]>>(index%(ifmaLanes*2))&1 != 0
					if got != want || got && vector.publicKeys[index] != scalar.publicKeys[index] {
						t.Fatal("complete range filter mismatch")
					}
				}
			}

			vector = testIFMAGenerator(t)
			vector.plan = matcher.PrefixPlan()

			scalar = testPairedGenerator(t)

			var (
				scalarStats Stats
				vectorStats Stats
				keys        []onion.Key
			)

			for range 130 {
				keys = keys[:0]

				err = scalar.searchBatch(matcher, func(key onion.Key) error {
					keys = append(keys, key)

					return nil
				}, &scalarStats)

				if err != nil {
					t.Fatal(err)
				}

				cursor := 0

				err = vector.searchFilteredBatch(matcher, func(key onion.Key) error {
					if cursor >= len(keys) || keys[cursor] != key {
						t.Fatal("match order/key mismatch")
					}

					checkKey(t, &key)
					checkPairedKey(t, key)

					cursor++

					return nil
				}, &vectorStats)

				if err != nil {
					t.Fatal(err)
				}

				if cursor != len(keys) || scalarStats != vectorStats || scalar.cursor != vector.cursor || scalar.position != vector.position || scalar.secrets != vector.secrets || scalar.steps != vector.steps {
					t.Fatal("search state mismatch")
				}
			}

			if test.name == "long_hit" && vectorStats.Saved == 0 || test.name == "long_reject" && vectorStats.Saved != 0 {
				t.Fatalf("long prefix fixture did not exercise expected match: %+v", vectorStats)
			}
		})
	}
}

func TestIFMAFilteredLifecycle(t *testing.T) {
	state := testIFMAGenerator(t)
	state.plan = pattern.PrefixPlan{Count: 1}
	state.steps[0] = reseedRounds - pairedOffsets

	var secret [64]byte

	secret[31] = 64
	state.secrets[0] = secret

	centerSecret := offsetSecret(secret, state.steps[0])

	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(centerSecret[:32])
	if err != nil {
		t.Fatal(err)
	}

	var center pairedAffine

	center.set(new(edwards25519.Point).ScalarBaseMult(scalar))

	state.centers[0].x.setLane(0, &center.x)
	state.centers[0].y.setLane(0, &center.y)
	state.centers[0].xy.setLane(0, &center.xy)

	for range pairedOffsets + 1 {
		err = state.nextFilteredBatch()
		if err != nil {
			t.Fatal(err)
		}

		for _, mask := range state.masks {
			if mask != 0xffff {
				t.Fatal("all-survivor mask differs")
			}
		}

		for index := range 2 {
			state.completeSign(index)

			checkPairedKey(t, state.snapshot(index).key())
		}
	}

	if state.steps[0] != pairedOffsets || state.secrets[0] == secret {
		t.Fatal("expired center not reseeded")
	}

	matcher, err := pattern.CompilePatterns(allSuffixPatterns(1))
	if err != nil {
		t.Fatal(err)
	}

	var stats Stats

	position := state.position

	err = state.searchFilteredBatch(matcher, func(key onion.Key) error {
		checkKey(t, &key)
		checkPairedKey(t, key)

		return nil
	}, &stats)

	// Skipped siblings do not consume the checked-candidate budget, so this consumes the prepared batch and the next one.
	if err != nil || stats != (Stats{Checked: batchSize, Saved: batchSize}) || state.position != position+1 || state.cursor != batchSize {
		t.Fatalf("sibling invalidation/accounting: %+v %v", stats, err)
	}

	saveError := errors.New("save failure")

	stats = Stats{}

	err = state.searchFilteredBatch(matcher, func(key onion.Key) error {
		return saveError
	}, &stats)

	if !errors.Is(err, saveError) || stats != (Stats{Checked: 1}) {
		t.Fatalf("save error: %+v %v", stats, err)
	}

	state.random = bytes.NewReader(nil)
	stats = Stats{}

	err = state.searchFilteredBatch(matcher, benchmarkSave, &stats)

	if !errors.Is(err, io.EOF) || stats != (Stats{Checked: 1, Saved: 1}) {
		t.Fatalf("reseed error: %+v %v", stats, err)
	}
}

func TestIFMAFilteredCancellation(t *testing.T) {
	testIFMAGenerator(t)

	matcher, err := pattern.CompilePatterns([]string{"ab."})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls uint64

	stats, err := Run(ctx, matcher, func(key onion.Key) error {
		checkKey(t, &key)
		checkPairedKey(t, key)

		calls++

		cancel()

		return nil
	})

	if !errors.Is(err, context.Canceled) || stats.Checked == 0 || stats.Checked%batchSize != 0 || stats.Saved != calls || calls == 0 {
		t.Fatalf("cancellation/accounting: %+v %v calls=%d", stats, err, calls)
	}

	stats, err = Run(ctx, matcher, benchmarkSave)
	if !errors.Is(err, context.Canceled) || stats != (Stats{}) {
		t.Fatalf("pre-canceled search: %+v %v", stats, err)
	}
}
