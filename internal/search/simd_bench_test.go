package search

import (
	"io"
	"os"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

func benchmarkSIMDWorker(random io.Reader, matcher *pattern.Matcher) (*worker, error) {
	mode := os.Getenv("ONINO_BENCH_BACKEND")
	if mode == "" {
		mode = "auto"
	}

	selected, err := simd.Parse(mode)
	if err != nil {
		return nil, err
	}

	config, err := Resolve(selected, matcher)
	if err != nil {
		return nil, err
	}

	return newWorkerWithConfig(random, matcher, config)
}
