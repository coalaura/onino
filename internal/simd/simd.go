// Package simd detects capabilities independently of execution overrides.
package simd

import (
	"fmt"
	"strings"
)

const (
	Auto Mode = iota
	Portable
	BMI2
	BMI2ADX
	IFMA
)

type Mode uint8

type Features struct {
	Known     bool
	BMI2      bool
	ADX       bool
	AVX2      bool
	AVX2CPU   bool
	IFMACPU   bool
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

func (mode Mode) String() string {
	switch mode {
	case Auto:
		return "auto"
	case Portable:
		return "portable"
	case BMI2:
		return "bmi2"
	case BMI2ADX:
		return "bmi2-adx"
	case IFMA:
		return "ifma"
	default:
		return "invalid"
	}
}

func (features Features) String() string {
	if !features.Known {
		return "detection unavailable in this build"
	}

	names := make([]string, 0, 6)

	if features.AVX2 {
		names = append(names, "AVX2")
	} else if features.AVX2CPU {
		names = append(names, "AVX2 (no OS state)")
	}

	if features.BMI2 {
		names = append(names, "BMI2")
	}

	if features.ADX {
		names = append(names, "ADX")
	}

	if features.IFMA {
		names = append(names, "AVX-512 IFMA")
	} else if features.IFMACPU {
		names = append(names, "IFMA (not OS-usable)")
	} else if features.Keccak {
		names = append(names, "AVX-512F")
	}

	if len(names) == 0 {
		return "no optional capabilities reported"
	}

	text := strings.Join(names, ", ")

	if features.BMI2 && !features.ADX {
		text += "; ADX not reported"
	}

	return text
}

func Parse(value string) (Mode, error) {
	switch value {
	case "auto":
		return Auto, nil
	case "portable":
		return Portable, nil
	case "bmi2":
		return BMI2, nil
	case "bmi2-adx":
		return BMI2ADX, nil
	case "ifma":
		return IFMA, nil
	default:
		return Auto, fmt.Errorf("invalid simd mode %q: expected auto, portable, bmi2, bmi2-adx or ifma", value)
	}
}

func Detect() Features {
	return decode(query())
}

func decode(value registers) Features {
	// XGETBV is legal only after XSAVE and OSXSAVE. All AVX-512 forms,
	// including YMM IFMA, require XMM, YMM, opmask and both ZMM state bits.
	features := Features{Known: value.maximum != 0}

	if value.maximum < 7 {
		return features
	}

	features.BMI2 = value.leaf7b&(1<<8) != 0
	features.ADX = value.leaf7b&(1<<19) != 0
	features.AVX2CPU = value.leaf7b&(1<<5) != 0
	features.IFMACPU = value.leaf7b&(1<<21) != 0

	required := uint32(1<<26 | 1<<27 | 1<<28)
	if value.leaf1&required != required || value.xcr0&0x6 != 0x6 {
		return features
	}

	features.AVX2 = features.AVX2CPU

	if value.xcr0&0xe6 != 0xe6 || value.leaf7b&(1<<16) == 0 {
		return features
	}

	features.IFMA = features.IFMACPU
	features.VL = value.leaf7b&(1<<31) != 0
	features.Keccak = true
	features.BW = value.leaf7b&(1<<30) != 0
	features.DQ = value.leaf7b&(1<<17) != 0
	features.VPOPCNTDQ = value.leaf7c&(1<<14) != 0

	return features
}
