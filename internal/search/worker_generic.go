//go:build purego || !amd64

package search

import (
	"io"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

type acceleratedWorker struct{}

func (state *acceleratedWorker) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	panic("unavailable SIMD worker")
}

func (state *acceleratedWorker) setSink(sink matchSink) {}

func newWorkerWithSIMD(random io.Reader, matcher *pattern.Matcher, features simd.Features) (*worker, error) {
	return newWorker(random, matcher)
}
