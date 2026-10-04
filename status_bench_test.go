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

	var buffer [192]byte

	b.ReportAllocs()

	for b.Loop() {
		line := appendSearchStatus(buffer[:0], stats, elapsed, 23000000, false)

		benchmarkStatusSize = len(line)
	}
}
