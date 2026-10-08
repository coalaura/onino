//go:build !purego

package search

import (
	"io"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

const (
	vectorAssemblyAvailable = true
	ifmaNeedsVL             = ifmaLanes == 4
)

type acceleratedWorker struct {
	generator *ifmaGenerator
	paired    *pairedGenerator
	walk      *generator
	filtered  bool
	checksum  bool
}

func (state *acceleratedWorker) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	if state.paired != nil {
		return state.paired.searchChecksumBatch(matcher, save, stats)
	}

	if state.walk != nil {
		if state.walk.round == reseedRounds {
			err := state.walk.reset()
			if err != nil {
				return err
			}
		}

		return state.walk.searchChecksumBatch(matcher, save, stats)
	}

	if state.checksum {
		return state.generator.searchChecksumBatch(matcher, save, stats)
	}

	if state.filtered {
		return state.generator.searchFilteredBatch(matcher, save, stats)
	}

	return state.generator.searchBatch(matcher, save, stats)
}

func (state *acceleratedWorker) setSink(sink matchSink) {
	if state.paired != nil {
		state.paired.sink = sink

		return
	}

	if state.walk != nil {
		state.walk.sink = sink

		return
	}

	state.generator.sink = sink
}

func newWorkerWithConfig(random io.Reader, matcher *pattern.Matcher, config Configuration) (*worker, error) {
	if config.Engine != simd.IFMA {
		state, err := newScalarWorker(random, config.Independent, config.Scalar)
		if err != nil {
			return nil, err
		}

		if config.Checksum {
			state.accelerated = &acceleratedWorker{paired: state.paired, walk: state.walk}
			state.paired = nil
			state.walk = nil
		}

		return state, nil
	}

	state := &acceleratedWorker{generator: &ifmaGenerator{random: random, fieldMode: config.Scalar}}
	state.generator.plan = matcher.PrefixPlan()
	state.filtered = config.Filtered
	state.checksum = config.Checksum

	err := state.generator.reset()
	if err != nil {
		return nil, err
	}

	return &worker{accelerated: state}, nil
}
