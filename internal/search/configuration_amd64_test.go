//go:build amd64 && !purego

package search

import (
	"crypto/sha3"
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

func TestScalarChecksumSelection(t *testing.T) {
	features := simd.Detect()
	if !features.Keccak {
		t.Skip("AVX-512 checksum unavailable")
	}

	matcher, err := pattern.CompilePatterns([]string{".aa"})
	if err != nil {
		t.Fatal(err)
	}

	var reference []onion.Key

	for _, mode := range executableModes() {
		if mode == simd.IFMA {
			continue
		}

		config, resolveErr := Resolve(mode, matcher)
		if resolveErr != nil || !config.Checksum || config.Matching != "scalar suffix" {
			t.Fatalf("%s checksum configuration: %+v, %v", mode, config, resolveErr)
		}

		state, workerErr := newWorkerWithConfig(sha3.NewSHAKE256(), matcher, config)
		if workerErr != nil {
			t.Fatal(workerErr)
		}

		checkWorkerMode(t, state, config)

		var (
			stats Stats
			saved int
		)

		for range pairedOffsets + 2 {
			searchErr := state.searchBatch(matcher, func(key onion.Key) error {
				checkPairedKey(t, key)

				if !matcher.Match(key.Public) {
					t.Fatal("accelerated checksum accepted a nonmatch")
				}

				if mode == simd.Portable {
					reference = append(reference, key)
				} else if saved >= len(reference) || key != reference[saved] {
					t.Fatalf("%s changed saved key %d", mode, saved)
				}

				saved++

				return nil
			}, &stats)

			if searchErr != nil {
				t.Fatal(searchErr)
			}
		}

		if saved == 0 || saved != len(reference) || stats.Saved != uint64(saved) || stats.Checked != (pairedOffsets+2)*batchSize {
			t.Fatalf("%s checksum accounting: %+v, %d reference keys", mode, stats, len(reference))
		}

		checkWorkerMode(t, state, config)
	}
}

func checkWorkerMode(t *testing.T, state *worker, config Configuration) {
	t.Helper()

	if config.Engine == simd.IFMA {
		if state.accelerated == nil || state.accelerated.generator == nil || state.accelerated.generator.fieldMode != config.Scalar {
			t.Fatal("forced IFMA or its resolved hybrid arithmetic was replaced")
		}

		return
	}

	paired := state.paired
	walk := state.walk

	if state.accelerated != nil {
		paired = state.accelerated.paired
		walk = state.accelerated.walk
	}

	if config.Independent {
		if walk == nil || walk.fieldMode != config.Engine {
			t.Fatal("independent walk restored automatic arithmetic")
		}
	} else if paired == nil || paired.fieldMode != config.Engine {
		t.Fatal("paired generator restored automatic arithmetic")
	}
}
