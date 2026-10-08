package main

import (
	"testing"
	"time"

	"github.com/coalaura/onino/internal/search"
)

var benchmarkStatusSize int

func BenchmarkSearchStatus(b *testing.B) {
	stats := search.Stats{Checked: 1234567890123, Saved: 1234}

	elapsed := 14*time.Hour + 53*time.Minute + 21*time.Second

	estimate := newMatchEstimate(1.0 / (1 << 35))

	var buffer [320]byte

	b.ReportAllocs()

	for b.Loop() {
		line := appendSearchStatus(buffer[:0], stats, elapsed, 23000000, estimate, "")

		benchmarkStatusSize = len(line)
	}
}
