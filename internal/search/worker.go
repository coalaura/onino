package search

import (
	"io"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

type worker struct {
	paired      *pairedGenerator
	walk        *generator
	accelerated *acceleratedWorker
}

func (state *worker) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	if state.paired != nil {
		return state.paired.searchBatch(matcher, save, stats)
	}

	if state.accelerated != nil {
		return state.accelerated.searchBatch(matcher, save, stats)
	}

	if state.walk.round == reseedRounds {
		err := state.walk.reset()
		if err != nil {
			return err
		}
	}

	return state.walk.searchBatch(matcher, save, stats)
}

func newWorker(random io.Reader, matcher *pattern.Matcher) (*worker, error) {
	return newScalarWorker(random, matcher.PreferIndependent(), defaultFieldMode)
}

func newScalarWorker(random io.Reader, independent bool, mode simd.Mode) (*worker, error) {
	// Pairing amortizes misses. Very frequent matchers instead benefit from
	// one independent seed per candidate and projective reseeding.
	if independent {
		state := &generator{random: random, fieldMode: mode}

		err := state.reset()
		if err != nil {
			return nil, err
		}

		return &worker{walk: state}, nil
	}

	state := &pairedGenerator{random: random, fieldMode: mode}

	err := state.reset()
	if err != nil {
		return nil, err
	}

	return &worker{paired: state}, nil
}
