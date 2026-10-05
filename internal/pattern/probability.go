package pattern

import (
	"fmt"
	"math"
	"math/bits"
	"math/rand/v2"
	"sort"
)

const probabilityNodeLimit = 65536

type probabilityCondition struct {
	mask  [5]uint64
	value [5]uint64
}

func (condition *probabilityCondition) matches(input *[5]uint64) bool {
	for index, mask := range condition.mask {
		if input[index]&mask != condition.value[index] {
			return false
		}
	}

	return true
}

// EstimateProbability estimates the OR of all patterns under uniform public-key
// bits and four independent checksum bits. It is startup-only and does not
// change the matcher. Curve encodings and successive search candidates are not
// truly independent uniform inputs, so waiting times remain estimates.
func EstimateProbability(patterns []string) (float64, error) {
	conditions, err := probabilityConditions(patterns)
	if err != nil {
		return 0, err
	}

	if len(conditions) == 0 {
		return 0, nil
	}

	if len(conditions) == 1 {
		return conditionProbability(&conditions[0]), nil
	}

	probability, complete := exactProbability(conditions, probabilityNodeLimit)
	if complete {
		return probability, nil
	}

	return sampleProbability(conditions), nil
}

func probabilityConditions(patterns []string) ([]probabilityCondition, error) {
	conditions := make([]probabilityCondition, 0, len(patterns))
	seen := make(map[probabilityCondition]bool, len(patterns))

	for index, text := range patterns {
		parsed, err := parsePattern(text)
		if err != nil {
			return nil, fmt.Errorf("pattern %d %q: %w", index, text, err)
		}

		possible := false
		first := parsed.first
		last := parsed.last

		if parsed.anchored {
			first = 0
			last = 0
		}

		for position := first; position <= last; position++ {
			occurrence := parsed

			if !parsed.anchored {
				occurrence.first = position
				occurrence.last = position
			}

			_, checksum, public, valid := publicPattern(occurrence)
			if !valid {
				continue
			}

			var condition probabilityCondition

			copy(condition.mask[:], public.mask[:])
			copy(condition.value[:], public.value[:])

			if checksum >= 0 {
				condition.mask[4] = 15
				condition.value[4] = uint64(checksum)
			}

			possible = true

			if !seen[condition] {
				seen[condition] = true
				conditions = append(conditions, condition)
			}
		}

		if !possible {
			return nil, fmt.Errorf("pattern %d %q: impossible in the first 52 hostname characters", index, text)
		}
	}

	return conditions, nil
}

// A bounded decision diagram handles overlaps exactly in the usual case. Large
// unions fall back to weighted union sampling: draw a condition proportional to
// its probability, then a uniform input satisfying it, and weight by 1/coverage.
// Unlike sampling random keys, this also works for astronomically rare matches.
func sampleProbability(conditions []probabilityCondition) float64 {
	weights := make([]float64, len(conditions))

	var (
		total float64
		lower float64
	)

	for index := range conditions {
		weight := conditionProbability(&conditions[index])
		total += weight
		lower = max(lower, weight)
		weights[index] = total
	}

	if total == 0 {
		return 0
	}

	// Bound startup work, not statistical confidence. This deterministic PRNG is
	// exclusively for estimation and never supplies search keys or their seeds.
	samples := max(256, min(4096, 4_000_000/len(conditions)))
	random := rand.New(rand.NewPCG(0x6f6e696e6f, 0x70726f626162696c))
	covered := 0.0

	for range samples {
		choice := random.Float64() * total

		index := sort.Search(len(weights), func(index int) bool {
			return weights[index] > choice
		})

		condition := &conditions[min(index, len(conditions)-1)]

		var input [5]uint64

		for word := range input {
			input[word] = random.Uint64()&^condition.mask[word] | condition.value[word]
		}

		matches := 0

		for index := range conditions {
			if conditions[index].matches(&input) {
				matches++
			}
		}

		covered += 1 / float64(matches)
	}

	return max(lower, min(1, total*covered/float64(samples)))
}

func conditionProbability(condition *probabilityCondition) float64 {
	constrained := 0

	for _, mask := range condition.mask {
		constrained += bits.OnesCount64(mask)
	}

	return math.Ldexp(1, -constrained)
}
