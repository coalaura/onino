//go:build purego || !amd64

package search

import (
	"io"

	"github.com/coalaura/onino/internal/pattern"
)

const (
	vectorAssemblyAvailable = false
	ifmaNeedsVL             = false
)

type acceleratedWorker struct{}

func (state *acceleratedWorker) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	panic("unavailable SIMD worker")
}

func (state *acceleratedWorker) setSink(sink matchSink) {}

func newWorkerWithConfig(random io.Reader, matcher *pattern.Matcher, config Configuration) (*worker, error) {
	return newScalarWorker(random, config.Independent, config.Scalar)
}
