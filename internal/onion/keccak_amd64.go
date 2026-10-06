//go:build !purego

package onion

// ChecksumAVX512 requires AVX-512F and enabled OS ZMM/opmask state. Callers
// select it at initialization using simd.Detect, independently of IFMA.
func ChecksumAVX512(public *[32]byte) [2]byte {
	var input [48]byte

	copy(input[:], checksumPrefix)
	copy(input[15:], public[:])

	input[47] = version

	word := keccakImmediate(&input)
	return [2]byte{byte(word), byte(word >> 8)}
}

//go:noescape
//go:abiinternal input=AX -> checksum=AX
func keccakImmediate(input *[48]byte) (checksum uint64)
