//go:build !amd64 || purego

package simd

func query() registers {
	return registers{}
}
