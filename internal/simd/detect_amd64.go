//go:build !purego

package simd

func cpuid(leaf, subleaf uint32) (eax, ebx, ecx, edx uint32)

func xgetbv() uint64

func query() registers {
	maximum, _, _, _ := cpuid(0, 0)
	if maximum < 7 {
		return registers{maximum: maximum}
	}

	_, _, leaf1, _ := cpuid(1, 0)
	required := uint32(1<<26 | 1<<27 | 1<<28)

	if leaf1&required != required {
		return registers{maximum: maximum, leaf1: leaf1}
	}

	state := xgetbv()
	_, leaf7b, leaf7c, _ := cpuid(7, 0)

	return registers{maximum: maximum, leaf1: leaf1, leaf7b: leaf7b, leaf7c: leaf7c, xcr0: state}
}
