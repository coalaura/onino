package pattern

import (
	"crypto/sha3"
	"testing"

	"github.com/coalaura/onino/internal/simd"
)

func TestSIMDChecksumSelection(t *testing.T) {
	features := simd.Detect(simd.Auto)
	if !features.Keccak {
		t.Skip("AVX-512F unavailable")
	}

	fixtures := [][]string{{".a"}, {".ab"}, {".abc"}, {".abcd"}, {".abcd", "abc."}, {"somethingrare"}, {".ab", ".cd", ".ef"}}

	for _, texts := range fixtures {
		matcher, err := CompilePatterns(texts)
		if err != nil {
			t.Fatal(err)
		}

		if !matcher.UsesChecksum() {
			continue
		}

		random := sha3.NewSHAKE256()

		for range 10000 {
			var data [32]byte

			random.Read(data[:])

			if matcher.MatchChecksum(data) != matcher.Match(data) {
				t.Fatalf("checksum selection changed decision for %v", texts)
			}
		}
	}
}
