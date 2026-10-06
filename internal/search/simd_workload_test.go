package search

import (
	"crypto/sha3"
	"testing"

	"github.com/coalaura/onino/internal/pattern"
)

func BenchmarkSIMDWorkload(b *testing.B) {
	cases := []benchmarkCase{
		{name: "three", patterns: []string{"donate.", "mirror.", "secure."}},
		{name: "anywhere", patterns: []string{"somethingrare"}},
		{name: "all_hits", patterns: allSuffixPatterns(1)},
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

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}
