package search

import (
	"context"
	"crypto/sha3"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

var benchmarkProgress Stats

func TestRunProgress(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher, err := pattern.CompilePatterns(allSuffixPatterns(1))
		if err != nil {
			t.Fatal(err)
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		started := time.Now()

		reports := make([]Stats, 0, 2)
		saved := 0

		save := func(key onion.Key) error {
			switch saved {
			case 0:
				time.Sleep(3 * time.Second)
			case batchSize:
				time.Sleep(time.Second)
			case batchSize * 2:
				time.Sleep(4 * time.Second)
			}

			saved++

			return nil
		}

		report := func(stats Stats) {
			elapsed := time.Since(started)
			expected := time.Duration(len(reports)+1) * 4 * time.Second

			if elapsed != expected {
				t.Fatalf("reported after %s, want %s", elapsed, expected)
			}

			if stats.Checked != uint64(saved) || stats.Saved != uint64(saved) {
				t.Fatalf("inconsistent progress: %+v, saved %d", stats, saved)
			}

			reports = append(reports, stats)
			if len(reports) == 2 {
				cancel()
			}
		}

		stats, err := RunWithProgress(ctx, matcher, save, report)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("search returned %v", err)
		}

		if len(reports) != 2 || reports[0].Checked != batchSize*2 || stats.Checked != batchSize*3 || stats != reports[1] {
			t.Fatalf("incorrect progress or cancellation: %+v, final %+v", reports, stats)
		}
	})
}

func BenchmarkProgress(b *testing.B) {
	modes := []string{"disabled", "enabled"}

	for _, mode := range modes {
		b.Run(mode, func(b *testing.B) {
			matcher, err := pattern.CompilePatterns([]string{"somethingrare."})
			if err != nil {
				b.Fatal(err)
			}

			state, err := newWorker(sha3.NewSHAKE256(), matcher)
			if err != nil {
				b.Fatal(err)
			}

			reporter := progressReporter{next: time.Now().Add(progressInterval)}

			if mode == "enabled" {
				reporter.report = recordBenchmarkProgress
			}

			var stats Stats

			b.ReportAllocs()

			for b.Loop() {
				err = state.searchBatch(matcher, benchmarkSave, &stats)
				if err != nil {
					b.Fatal(err)
				}

				reporter.update(stats)
			}

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func recordBenchmarkProgress(stats Stats) {
	benchmarkProgress = stats
}
