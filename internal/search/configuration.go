package search

import (
	"fmt"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

// Configuration keeps detected capabilities separate from the execution choice.
// Resolve it before constructing a worker: forced kernels can fault during reset.
type Configuration struct {
	Requested     simd.Mode
	Engine        simd.Mode
	Scalar        simd.Mode
	Features      simd.Features
	Independent   bool
	Filtered      bool
	Checksum      bool
	Matching      string
	NeedsChecksum bool
}

var defaultFieldMode = selectScalar(simd.Detect(), scalarAssemblyAvailable)

func (options Options) resolve(matcher *pattern.Matcher) (Configuration, error) {
	if options.Config != nil {
		return *options.Config, nil
	}

	return Resolve(options.SIMD, matcher)
}

func (config Configuration) Forced() bool {
	return config.Requested != simd.Auto
}

func (config Configuration) Arithmetic() string {
	switch config.Engine {
	case simd.BMI2:
		return "scalar (bmi2)"
	case simd.BMI2ADX:
		return "scalar (bmi2+adx)"
	case simd.IFMA:
		return "avx512 (ifma)"
	default:
		return "go (generic)"
	}
}

func (config Configuration) Generator() string {
	if config.Independent {
		return "independent walks"
	}

	return "paired"
}

func (config Configuration) Supported() bool {
	switch config.Engine {
	case simd.BMI2:
		return config.Features.BMI2
	case simd.BMI2ADX:
		return config.Features.BMI2 && config.Features.ADX
	case simd.IFMA:
		return config.Features.IFMA && (!ifmaNeedsVL || config.Features.VL)
	default:
		return true
	}
}

func Resolve(mode simd.Mode, matcher *pattern.Matcher) (Configuration, error) {
	return resolveConfiguration(mode, matcher, simd.Detect(), scalarAssemblyAvailable, vectorAssemblyAvailable)
}

func BuildDescription() string {
	if scalarAssemblyAvailable {
		return "amd64: portable, bmi2, bmi2-adx, ifma"
	}

	return "portable arithmetic only"
}

func resolveConfiguration(mode simd.Mode, matcher *pattern.Matcher, features simd.Features, scalarAvailable, vectorAvailable bool) (Configuration, error) {
	config := Configuration{
		Requested:     mode,
		Engine:        mode,
		Scalar:        selectScalar(features, scalarAvailable),
		Features:      features,
		Independent:   matcher.PreferIndependent(),
		Checksum:      vectorAvailable && features.Keccak && matcher.UsesChecksum(),
		Matching:      matcher.MatchingEngine(),
		NeedsChecksum: matcher.UsesChecksum(),
	}

	switch mode {
	case simd.Auto:
		config.Engine = config.Scalar
		if vectorAvailable && features.IFMA && (!ifmaNeedsVL || features.VL) && !config.Independent {
			config.Engine = simd.IFMA
		}
	case simd.Portable:
	case simd.BMI2, simd.BMI2ADX:
		if !scalarAvailable {
			return Configuration{}, fmt.Errorf("--simd=%s is not compiled into this binary (requires amd64 without purego)", mode)
		}
	case simd.IFMA:
		if !vectorAvailable {
			return Configuration{}, fmt.Errorf("--simd=ifma is not compiled into this binary (requires amd64 without purego)")
		}
	default:
		return Configuration{}, fmt.Errorf("invalid SIMD mode %d", mode)
	}

	if config.Engine == simd.IFMA {
		// Independent walks are a performance preference, not an IFMA limitation.
		config.Independent = false
		config.Filtered = matcher.PrefixPlan().Count != 0

		if config.Filtered && !config.Checksum {
			config.Matching = "fused IFMA prefix + scalar verification"
		}
	} else {
		config.Scalar = config.Engine
	}

	return config, nil
}

func selectScalar(features simd.Features, available bool) simd.Mode {
	if available && features.BMI2 {
		if features.ADX {
			return simd.BMI2ADX
		}

		return simd.BMI2
	}

	return simd.Portable
}
