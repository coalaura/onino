package search

import (
	"io"

	"github.com/coalaura/onino/internal/pattern"
)

type worker struct {
	paired *pairedGenerator
	walk   *generator
}

func (state *worker) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	if state.paired != nil {
		return state.paired.searchBatch(matcher, save, stats)
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
	// Pairing amortizes misses. Very frequent matchers instead benefit from
	// one independent seed per candidate and projective reseeding.
	if matcher.PreferIndependent() {
		state := &generator{random: random}

		err := state.reset()
		if err != nil {
			return nil, err
		}

		return &worker{walk: state}, nil
	}

	state := &pairedGenerator{random: random}

	err := state.reset()
	if err != nil {
		return nil, err
	}

	return &worker{paired: state}, nil
}
