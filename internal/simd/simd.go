// Package simd controls onino's optional instruction sets without changing the
// binary's compilation baseline or the existing AVX2/BMI2/ADX implementations.
package simd

import "fmt"

const (
	Auto Mode = iota
	AVX2
)

type Mode uint8

type Features struct {
	IFMA      bool
	VL        bool
	Keccak    bool
	BW        bool
	DQ        bool
	VPOPCNTDQ bool
}

type registers struct {
	maximum uint32
	leaf1   uint32
	leaf7b  uint32
	leaf7c  uint32
	xcr0    uint64
}

func Parse(value string) (Mode, error) {
	switch value {
	case "auto":
		return Auto, nil
	case "avx2":
		return AVX2, nil
	default:
		return Auto, fmt.Errorf("invalid simd mode %q: expected auto or avx2", value)
	}
}

func Detect(mode Mode) Features {
	return detect(mode, query)
}

func detect(mode Mode, read func() registers) Features {
	if mode != Auto {
		return Features{}
	}

	return decode(read())
}

func decode(value registers) Features {
	// XGETBV is legal only after XSAVE and OSXSAVE. All AVX-512 forms,
	// including YMM IFMA, require XMM, YMM, opmask and both ZMM state bits.
	constRequired := uint32(1<<26 | 1<<27 | 1<<28)
	if value.maximum < 7 || value.leaf1&constRequired != constRequired || value.xcr0&0xe6 != 0xe6 || value.leaf7b&(1<<16) == 0 {
		return Features{}
	}

	return Features{
		IFMA:      value.leaf7b&(1<<21) != 0,
		VL:        value.leaf7b&(1<<31) != 0,
		Keccak:    true,
		BW:        value.leaf7b&(1<<30) != 0,
		DQ:        value.leaf7b&(1<<17) != 0,
		VPOPCNTDQ: value.leaf7c&(1<<14) != 0,
	}
}
