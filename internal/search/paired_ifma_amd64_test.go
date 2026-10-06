//go:build !purego

package search

import (
	"crypto/sha3"
	"os"
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

func TestIFMAPreparation(t *testing.T) {
	vector := testIFMAGenerator(t)
	scalar := testPairedGenerator(t)

	for offset := range pairedTable {
		vector.prepare(&pairedTable[offset])

		var products [ifmaLanes]fieldElement

		for lane := range products {
			products[lane] = pairedOne
		}

		for index := range pairedCenters {
			lane := index % ifmaLanes
			scratch := &vector.scratch[index/ifmaLanes]

			var (
				coupling    fieldElement
				denominator fieldElement
			)

			coupling.multiply(&scalar.centers[index].xy, &pairedTable[offset].xy)

			denominator.square(&coupling)
			denominator.subtract(&pairedOne, &denominator)

			products[lane].multiply(&products[lane], &denominator)

			got := [3]fieldElement{scratch.c.lane(lane), scratch.denominator.lane(lane), scratch.product.lane(lane)}
			want := [3]fieldElement{coupling, denominator, products[lane]}

			for field := range got {
				var (
					actual   [32]byte
					expected [32]byte
				)

				got[field].putBytes(&actual)
				want[field].putBytes(&expected)

				if actual != expected {
					t.Fatalf("offset %d center %d intermediate %d differs", offset, index, field)
				}
			}
		}
	}
}

func TestIFMARanges(t *testing.T) {
	vector := testIFMAGenerator(t)
	scalar := testPairedGenerator(t)

	for batch := range 130 {
		err := scalar.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		err = vector.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		if scalar.publicKeys != vector.publicKeys {
			t.Fatalf("ordinate range differs at batch %d", batch)
		}

		for index := range batchSize {
			scalar.completeSign(index)
			vector.completeSign(index)
		}

		if scalar.publicKeys != vector.publicKeys {
			t.Fatalf("signed range differs at batch %d", batch)
		}

		checkPairedKey(t, vector.snapshot(batch%batchSize).key())

		if batch%9 == 0 {
			err = scalar.reseed(3)
			if err != nil {
				t.Fatal(err)
			}

			err = vector.reseed(3)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestIFMASearch(t *testing.T) {
	cases := simdSearchCases()
	cases = append(cases, benchmarkCase{name: "all_hits", patterns: allSuffixPatterns(1)})

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			vector := testIFMAGenerator(t)
			scalar := testPairedGenerator(t)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				t.Fatal(err)
			}

			var (
				scalarStats Stats
				vectorStats Stats
				keys        []onion.Key
			)

			for range 3 {
				keys = keys[:0]

				err = scalar.searchBatch(matcher, func(key onion.Key) error {
					keys = append(keys, key)

					return nil
				}, &scalarStats)

				if err != nil {
					t.Fatal(err)
				}

				cursor := 0

				err = vector.searchBatch(matcher, func(key onion.Key) error {
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

				if cursor != len(keys) || scalarStats != vectorStats || scalar.cursor != vector.cursor || scalar.position != vector.position || scalar.secrets != vector.secrets || scalar.steps != vector.steps || scalar.publicKeys != vector.publicKeys {
					t.Fatal("complete search state differs")
				}
			}
		})
	}
}

func BenchmarkSIMDGeneration(b *testing.B) {
	if os.Getenv("ONINO_BENCH_BACKEND") != "ifma8" {
		state := testPairedGenerator(b)

		b.ReportAllocs()

		for b.Loop() {
			err := state.nextBatch()
			if err != nil {
				b.Fatal(err)
			}
		}
	} else {
		state := testIFMAGenerator(b)

		b.ReportAllocs()

		for b.Loop() {
			err := state.nextBatch()
			if err != nil {
				b.Fatal(err)
			}
		}
	}

	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/batchSize, "ns/key")
}

func BenchmarkSIMDSearch(b *testing.B) {
	for _, test := range simdSearchCases() {
		b.Run(test.name, func(b *testing.B) {
			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			var stats Stats

			if os.Getenv("ONINO_BENCH_BACKEND") == "auto" || os.Getenv("ONINO_BENCH_BACKEND") == "avx2" {
				state, createErr := benchmarkSIMDWorker(sha3.NewSHAKE256(), matcher)
				if createErr != nil {
					b.Fatal(createErr)
				}

				b.ReportAllocs()

				for b.Loop() {
					err = state.searchBatch(matcher, benchmarkSave, &stats)
					if err != nil {
						b.Fatal(err)
					}
				}
			} else if os.Getenv("ONINO_BENCH_BACKEND") == "filtered" {
				state := testIFMAGenerator(b)
				state.plan = matcher.PrefixPlan()

				if state.plan.Count == 0 {
					b.Skip("no prefix plan")
				}

				b.ReportAllocs()

				for b.Loop() {
					err = state.searchFilteredBatch(matcher, benchmarkSave, &stats)
					if err != nil {
						b.Fatal(err)
					}
				}
			} else if os.Getenv("ONINO_BENCH_BACKEND") != "ifma8" {
				state := testPairedGenerator(b)

				b.ReportAllocs()

				for b.Loop() {
					err = state.searchBatch(matcher, benchmarkSave, &stats)
					if err != nil {
						b.Fatal(err)
					}
				}
			} else {
				state := testIFMAGenerator(b)
				b.ReportAllocs()

				for b.Loop() {
					err = state.searchBatch(matcher, benchmarkSave, &stats)
					if err != nil {
						b.Fatal(err)
					}
				}
			}

			if stats.Checked != uint64(b.N)*batchSize {
				b.Fatal("candidate count differs")
			}

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func simdSearchCases() []benchmarkCase {
	return []benchmarkCase{
		{name: "rare", patterns: []string{"somethingrare."}},
		{name: "three", patterns: []string{"donate.", "mirror.", "secure."}},
		{name: "frequent", patterns: []string{"ab."}},
		{name: "dictionary", patterns: benchmarkDictionary(512, false)},
		{name: "shared", patterns: benchmarkDictionary(512, true)},
		{name: "suffix2", patterns: []string{".aa"}},
		{name: "suffix3", patterns: []string{".aaa"}},
		{name: "anywhere", patterns: []string{"somethingrare"}},
	}
}

func testIFMAGenerator(t testing.TB) *ifmaGenerator {
	t.Helper()

	features := simd.Detect(simd.Auto)
	if !features.IFMA || ifmaLanes == 4 && !features.VL {
		t.Skip("IFMA unavailable")
	}

	state := &ifmaGenerator{random: sha3.NewSHAKE256()}

	err := state.reset()
	if err != nil {
		t.Fatal(err)
	}

	return state
}
