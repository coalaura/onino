package search

import (
	"strconv"
	"testing"

	"github.com/coalaura/onino/internal/pattern"
)

func BenchmarkAnchoredSearch(b *testing.B) {
	for _, test := range anchoredBenchmarkCases() {
		b.Run(test.name, func(b *testing.B) {
			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			state := testPairedGenerator(b)

			var stats Stats

			b.ReportAllocs()

			for b.Loop() {
				err = state.searchBatch(matcher, benchmarkSave, &stats)
				if err != nil {
					b.Fatal(err)
				}
			}

			if stats.Checked != uint64(b.N)*batchSize {
				b.Fatalf("lost candidates: %+v", stats)
			}

			benchmarkHits = stats.Saved

			b.ReportMetric(float64(stats.Checked)/b.Elapsed().Seconds(), "keys/s")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func anchoredBenchmarkCases() []benchmarkCase {
	cases := []benchmarkCase{
		{name: "donate", patterns: []string{"donate."}},
		{name: "privacy", patterns: []string{"privacy."}},
		{name: "rare", patterns: []string{"somethingrare."}},
		{name: "three6", patterns: []string{"donate.", "mirror.", "secure."}},
		{name: "three7", patterns: []string{"donates.", "mirrors.", "secured."}},
		{name: "mixed_lengths", patterns: []string{"donate.", "mirrors.", "securedabc."}},
		{name: "shared", patterns: []string{"donateabc.", "donatedef.", "donateghi.", "donatejkl."}},
		{name: "word_edge", patterns: []string{"abcdefghijkl.", "mnopqrstuvwx.", "yz234567abcd."}},
		{name: "cross_word", patterns: []string{"abcdefghijklm.", "nopqrstuvwxyz.", "abcdefghijklmnopqrstuvwxy2."}},
	}

	sizes := []int{2, 4, 8, 16, 32, 63, 64}

	for _, size := range sizes {
		patterns := benchmarkDictionary(size, false)

		for index := range patterns {
			patterns[index] += "."
		}

		cases = append(cases, benchmarkCase{name: "set" + strconv.Itoa(size), patterns: patterns})
	}

	return cases
}
