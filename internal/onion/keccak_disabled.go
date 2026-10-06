//go:build purego || !amd64

package onion

func ChecksumAVX512(public *[32]byte) [2]byte {
	return Checksum(public)
}
