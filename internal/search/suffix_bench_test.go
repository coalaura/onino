package search

import (
	"crypto/rand"
	"crypto/sha3"
	"testing"

	"github.com/coalaura/onino/internal/pattern"
)

func BenchmarkSuffixCosts(b *testing.B) {
	cases := []benchmarkCase{
		{name: "rare_prefix", patterns: []string{"somethingrare."}},
		{name: "prefix1", patterns: []string{"a."}},
		{name: "suffix1", patterns: []string{".a"}},
		{name: "suffix2", patterns: []string{".aa"}},
		{name: "suffix3", patterns: []string{".aaa"}},
		{name: "suffix4", patterns: []string{".aaaa"}},
	}

	modes := []string{"matching_only", "reseed_shake", "reseed_random"}

	for _, test := range cases {
		for _, mode := range modes {
			b.Run(test.name+"/"+mode, func(b *testing.B) {
				matcher, err := pattern.CompilePatterns(test.patterns)
				if err != nil {
					b.Fatal(err)
				}

				state, err := newWorker(sha3.NewSHAKE256(), matcher)
				if err != nil {
					b.Fatal(err)
				}

				if mode == "reseed_random" {
					if state.paired != nil {
						state.paired.random = rand.Reader
					} else {
						state.walk.random = rand.Reader
					}
				}

				var stats Stats

				b.ReportAllocs()

				for b.Loop() {
					if mode == "matching_only" {
						err = benchmarkMatchBatch(state, matcher, &stats)
					} else {
						err = state.searchBatch(matcher, benchmarkSave, &stats)
					}

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
				b.ReportMetric(float64(stats.Saved)/float64(stats.Checked), "hits/key")
			})
		}
	}
}

// Count real matches without exporting keys or replacing their search state.
// This diagnostic keeps the selected engine, sign filter and checksum work.
func benchmarkMatchBatch(state *worker, matcher *pattern.Matcher, stats *Stats) error {
	filter := matcher.SignFilter()

	if state.paired != nil {
		paired := state.paired

		for range batchSize {
			if paired.cursor == batchSize {
				err := paired.nextBatch()
				if err != nil {
					return err
				}
			}

			index := paired.cursor

			paired.cursor++
			stats.Checked++

			if filter != nil && !filter.Match(paired.publicKeys[index]) {
				continue
			}

			paired.completeSign(index)

			if matcher.Match(paired.publicKeys[index]) {
				stats.Saved++
			}
		}

		return nil
	}

	walk := state.walk

	if walk.round == reseedRounds {
		err := walk.reset()
		if err != nil {
			return err
		}
	}

	walk.nextBatch(filter != nil)

	for index := range walk.publicKeys {
		stats.Checked++

		if walk.matches(index, matcher, filter) {
			stats.Saved++
		}
	}

	return nil
}
