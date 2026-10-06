//go:build !purego

package onion

import (
	"crypto/sha3"
	"testing"

	"github.com/coalaura/onino/internal/simd"
)

func TestKeccakImmediate(t *testing.T) {
	if !simd.Detect(simd.Auto).Keccak {
		t.Skip("AVX-512F unavailable")
	}

	random := sha3.NewSHAKE256()

	var (
		public [32]byte
		input  [48]byte
	)

	copy(input[:], checksumPrefix)

	input[47] = version

	for iteration := range 10000 {
		if iteration != 0 {
			random.Read(public[:])
		}

		copy(input[15:], public[:])

		expected := sha3.Sum256(input[:])
		actual := ChecksumAVX512(&public)

		if actual != [2]byte{expected[0], expected[1]} {
			t.Fatalf("iteration %d: %x != %x", iteration, actual, expected[:2])
		}
	}
}
