//go:build !purego

package search

import (
	"crypto/sha3"
	"encoding/binary"
	"math/rand/v2"
	"testing"

	"github.com/coalaura/onino/internal/pattern"
)

func TestIFMAPairedFilter(t *testing.T) {
	state := testIFMAGenerator(t)
	state.prepare(&pairedTable[0])

	random := rand.New(rand.NewPCG(81, 512))

	var poison ifmaElement

	for limb := range poison {
		for lane := range ifmaLanes {
			poison[limb][lane] = ^uint64(0)
		}
	}

	plans := []pattern.PrefixPlan{
		{Count: 1},
		{Probes: [8]pattern.PrefixProbe{{Mask: 0, Value: 1}}, Count: 1},
		{Count: 1},
		{Count: 8},
	}

	for iteration := range 512 {
		scratch := state.scratch[iteration%len(state.scratch)]

		inputs := []*ifmaElement{&scratch.a, &scratch.b, &scratch.plusInverse, &scratch.minusInverse}

		if iteration >= len(state.scratch) {
			for inputIndex, input := range inputs {
				for limb := range input {
					for lane := range ifmaLanes {
						value := random.Uint64() & radixMask

						if iteration < 128 {
							// Mix zero, one, p-1, p, p+1 and maximum normalized
							// values across both operands, reciprocals and lanes.
							edge := (iteration + inputIndex*3 + lane) % 6
							value = radixMask

							if edge < 2 {
								value = 0

								if limb == 0 {
									value = uint64(edge)
								}
							} else if limb == 0 && edge < 5 {
								value -= uint64(21 - edge)
							}
						}

						input[limb][lane] = value
					}
				}
			}
		}

		original := scratch

		var (
			wantPlus  ifmaElement
			wantMinus ifmaElement
		)

		referenceIFMAPairedFilter(&wantPlus, &wantMinus, &scratch, &plans[0])

		// An all-survivor invocation exposes the canonical result of every
		// candidate, including lanes rejected by each selective invocation.
		for lane := range ifmaLanes {
			for side := range 2 {
				numerator := scratch.b.lane(lane)
				other := scratch.a.lane(lane)
				inverse := scratch.plusInverse.lane(lane)
				want := wantPlus.lane(lane)

				if side == 0 {
					numerator.add(&numerator, &other)
				} else {
					numerator.subtract(&numerator, &other)
					inverse = scratch.minusInverse.lane(lane)
					want = wantMinus.lane(lane)
				}

				numerator.multiply(&numerator, &inverse)

				var encoded [32]byte

				numerator.putBytes(&encoded)

				for word := range want {
					if want[word] != binary.LittleEndian.Uint64(encoded[word*8:]) {
						t.Fatalf("scalar canonical mismatch: iteration %d lane %d side %d", iteration, lane, side)
					}
				}
			}
		}

		selected := wantPlus.lane(iteration % ifmaLanes)
		plans[2].Probes[0] = pattern.PrefixProbe{Mask: ^uint64(0), Value: selected[0]}

		for probe := range plans[3].Probes {
			selected = wantMinus.lane(probe)
			plans[3].Probes[probe] = pattern.PrefixProbe{Mask: ^uint64(0) >> (probe * 3), Value: selected[0] & (^uint64(0) >> (probe * 3))}
		}

		for planIndex := range plans {
			plan := &plans[planIndex]

			plus := poison
			minus := poison

			wantMask := referenceIFMAPairedFilter(&wantPlus, &wantMinus, &scratch, plan)
			mask := ifmaPairedFilter(&plus, &minus, &scratch, plan)

			if mask != wantMask || scratch != original {
				t.Fatalf("mask/scratch mismatch: iteration %d plan %d got %x want %x", iteration, planIndex, mask, wantMask)
			}

			for lane := range ifmaLanes {
				for limb := range plus {
					plusValue := poison[limb][lane]
					minusValue := poison[limb][lane]

					if mask>>(lane*2)&1 != 0 {
						plusValue = wantPlus[limb][lane]
					}

					if mask>>(lane*2+1)&1 != 0 {
						minusValue = wantMinus[limb][lane]
					}

					if plus[limb][lane] != plusValue || minus[limb][lane] != minusValue {
						t.Fatalf("coordinate/store mismatch: iteration %d plan %d lane %d limb %d", iteration, planIndex, lane, limb)
					}
				}
			}
		}
	}
}

func BenchmarkIFMAPairedFilter(b *testing.B) {
	state := testIFMAGenerator(b)
	state.prepare(&pairedTable[0])

	matcher, err := pattern.CompilePatterns([]string{"somethingrare."})
	if err != nil {
		b.Fatal(err)
	}

	plan := matcher.PrefixPlan()

	var (
		plus  ifmaElement
		minus ifmaElement
		mask  uint64
	)

	b.ReportAllocs()

	for b.Loop() {
		mask = ifmaPairedFilter(&plus, &minus, &state.scratch[0], &plan)
	}

	benchmarkHits = mask
}

func BenchmarkPairedFilterSearch(b *testing.B) {
	cases := []benchmarkCase{
		{name: "rare", patterns: []string{"somethingrare."}},
		{name: "three", patterns: []string{"donate.", "mirror.", "secure."}},
		{name: "long", patterns: []string{"abcdefghijklmnopqrstuvwxyz234567abcdefghijklmnopqr."}},
		{name: "frequent", patterns: []string{"ab."}},
		{name: "walk", patterns: []string{"a."}},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			state, err := benchmarkSIMDWorker(sha3.NewSHAKE256(), matcher)
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

			if stats.Checked != uint64(b.N)*batchSize {
				b.Fatal("candidate count differs")
			}

			b.ReportMetric(float64(stats.Saved), "saved")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func referenceIFMAPairedFilter(plus, minus *ifmaElement, scratch *ifmaScratch, plan *pattern.PrefixPlan) uint64 {
	var numerator ifmaElement

	ifmaAdd(&numerator, &scratch.b, &scratch.a)
	ifmaMultiply(plus, &numerator, &scratch.plusInverse)
	ifmaSubtract(&numerator, &scratch.b, &scratch.a)
	ifmaMultiply(minus, &numerator, &scratch.minusInverse)

	plusMask := ifmaCanonicalFilter(plus, plan)
	minusMask := ifmaCanonicalFilter(minus, plan)

	var mask uint64

	for lane := range ifmaLanes {
		mask |= (plusMask >> lane & 1) << (lane * 2)
		mask |= (minusMask >> lane & 1) << (lane*2 + 1)
	}

	return mask
}
