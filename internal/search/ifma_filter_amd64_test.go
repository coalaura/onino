//go:build !purego

package search

import (
	"encoding/binary"
	"testing"

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
		})
	}
}
