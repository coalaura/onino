package pattern

import "math/bits"

// Keep the usual no-candidate path inline; verification stays out of line.
//
//go:inline
func (matcher *Matcher) matchScans(data *[32]byte) bool {
	if len(matcher.scans) == 0 {
		return false
	}

	index, candidates := filterCandidates(data, &matcher.scans[0], len(matcher.scans))
	if index < 0 {
		return false
	}

	return matcher.verifyScans(data, candidates, index)
}

func (matcher *Matcher) verifyScans(data *[32]byte, candidates uint64, index int) bool {
	for {
		checks := matcher.scanChecks[index]
		if checks == nil {
			return true
		}

		for candidates != 0 {
			packed := bits.TrailingZeros64(candidates)

			// Undo the lane-local packing permutation used by the AVX2 filter.
			position := packed&0x23 | (packed&0x0c)<<1 | (packed&0x10)>>2
			if checks[position].matchesInput(data) {
				return true
			}

			candidates &= candidates - 1
		}

		first := index + 1
		if first == len(matcher.scans) {
			return false
		}

		next, remaining := filterCandidates(data, &matcher.scans[first], len(matcher.scans)-first)
		if next < 0 {
			return false
		}

		index = first + next
		candidates = remaining
	}
}
