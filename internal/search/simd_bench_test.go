package search

import (
	"errors"
	"io"
	"os"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

func benchmarkSIMDWorker(random io.Reader, matcher *pattern.Matcher) (*worker, error) {
	mode := os.Getenv("ONINO_BENCH_BACKEND")
	if mode != "auto" && mode != "avx2" && mode != "keccak" {
		return newWorker(random, matcher)
	}

	selected := simd.Auto

	if mode == "avx2" {
		selected = simd.AVX2
	}

	features := simd.Detect(selected)

	if mode == "keccak" {
		if !features.Keccak {
			return nil, errors.New("AVX-512F unavailable")
		}

		features.IFMA = false
	}

	return newWorkerWithSIMD(random, matcher, features)
}
