//go:build gpu

package gpu

import (
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

const alphabet = "abcdefghijklmnopqrstuvwxyz234567"

type Plan struct {
	probes [8][4]uint32
	count  uint32
}

// Compile accepts one to eight literal anchored prefixes. Beyond twelve characters, the GPU applies a necessary filter and the host verifies the complete pattern.
func Compile(patterns []string) (Plan, error) {
	var plan Plan

	if len(patterns) == 0 || len(patterns) > len(plan.probes) {
		return plan, errors.New("GPU search requires one to eight anchored prefixes")
	}

	for index, pattern := range patterns {
		prefix, anchored := strings.CutSuffix(pattern, ".")
		if !anchored || len(prefix) == 0 || len(prefix) > 51 {
			return Plan{}, fmt.Errorf("GPU pattern %q must be a literal anchored prefix of 1–51 characters followed by a dot", pattern)
		}

		var (
			mask  [8]byte
			value [8]byte
		)

		for position, character := range prefix {
			digit := strings.IndexRune(alphabet, character)
			if digit < 0 {
				return Plan{}, fmt.Errorf("GPU pattern %q contains a non-base32 character", pattern)
			}

			if position >= 12 {
				continue
			}

			for bit := range 5 {
				offset := position*5 + bit
				mask[offset/8] |= 1 << (7 - offset%8)
				value[offset/8] |= byte((digit>>(4-bit))&1) << (7 - offset%8)
			}
		}

		plan.probes[index] = [4]uint32{binary.LittleEndian.Uint32(mask[:4]), binary.LittleEndian.Uint32(mask[4:]), binary.LittleEndian.Uint32(value[:4]), binary.LittleEndian.Uint32(value[4:])}
	}

	plan.count = uint32(len(patterns))

	return plan, nil
}
