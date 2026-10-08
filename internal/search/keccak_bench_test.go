package search

import (
	"os"
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/simd"
)

func BenchmarkSIMDChecksum(b *testing.B) {
	var public [32]byte

	b.ReportAllocs()

	if os.Getenv("ONINO_BENCH_BACKEND") == "keccak" {
		if !simd.Detect().Keccak {
			b.Skip("AVX-512F unavailable")
		}

		for b.Loop() {
			checksum := onion.ChecksumAVX512(&public)

			public[0] = checksum[0]
			public[1] = checksum[1]
		}

		return
	}

	for b.Loop() {
		checksum := onion.Checksum(&public)

		public[0] = checksum[0]
		public[1] = checksum[1]
	}
}
