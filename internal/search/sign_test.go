package search

import (
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestDeferredSignAgainstScalarMultiplication(t *testing.T) {
	state := testGenerator(t)

	var signs [2]int

	for range 4 {
		state.nextBatch(true)

		for index := range state.publicKeys {
			if state.publicKeys[index][31]&0x80 != 0 {
				t.Fatal("partial key contains a sign")
			}

			projectiveX := state.points[index].xCoordinate

			completeSign(&state.points[index], &state.products[index], &state.publicKeys[index])

			if state.points[index].xCoordinate != projectiveX {
				t.Fatal("completion changed projective X")
			}

			key := state.key(index)

			checkKey(t, &key)

			signs[key.Public[31]>>7]++
		}

		err := state.reseed(13)
		if err != nil {
			t.Fatal(err)
		}
	}

	if signs[0] == 0 || signs[1] == 0 {
		t.Fatal("missing sign coverage")
	}
}

func TestDeferredBatchMultipleHits(t *testing.T) {
	state := testGenerator(t)

	reference := *state
	reference.next()

	patterns := make([]string, batchSize)

	for index := range patterns {
		key := onion.Key{Public: reference.publicKeys[index]}

		patterns[index] = key.Hostname()[:52] + "."
	}

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	var (
		stats Stats
		count int
	)

	err = state.searchBatch(matcher, func(key onion.Key) error {
		checkKey(t, &key)

		if key.Public != reference.publicKeys[count] {
			t.Fatal("saved incomplete or wrong key")
		}

		count++

		return nil
	}, &stats)

	if err != nil {
		t.Fatal(err)
	}

	if stats.Checked != batchSize || stats.Saved != batchSize || count != batchSize {
		t.Fatalf("lost hits: %+v, callback count %d", stats, count)
	}

	for index := range state.secrets {
		if state.secrets[index] == reference.secrets[index] || state.started[index] != state.round {
			t.Fatal("matched lane was not reseeded")
		}
	}
}
