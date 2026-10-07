package main

import (
	"math"
	"strconv"
	"time"
)

type matchEstimate struct {
	candidates50 float64
	candidates95 float64
}

func newMatchEstimate(probability float64) matchEstimate {
	if probability >= 1 {
		return matchEstimate{candidates50: 1, candidates95: 1}
	}

	if probability <= 0 {
		return matchEstimate{candidates50: math.Inf(1), candidates95: math.Inf(1)}
	}

	// log1p preserves rare probabilities that would round 1-p to exactly one.
	miss := math.Log1p(-probability)

	return matchEstimate{
		candidates50: math.Ceil(math.Log(0.5) / miss),
		candidates95: math.Ceil(math.Log(0.05) / miss),
	}
}

func appendCandidateEstimate(buffer []byte, estimate matchEstimate) []byte {
	buffer = append(buffer, "Estimated candidates for a match: 50% ~"...)
	buffer = appendCandidateCount(buffer, estimate.candidates50)
	buffer = append(buffer, "; 95% ~"...)
	buffer = appendCandidateCount(buffer, estimate.candidates95)

	return append(buffer, ". Wait estimates use the recent combined rate.\n"...)
}

func appendCandidateCount(buffer []byte, count float64) []byte {
	if math.IsInf(count, 1) {
		return append(buffer, "unbounded"...)
	}

	if count >= 1e15 {
		return strconv.AppendFloat(buffer, count, 'g', 4, 64)
	}

	var digits [32]byte

	number := strconv.AppendFloat(digits[:0], count, 'f', 0, 64)

	return appendGroupedDigits(buffer, number)
}

func appendWaitEstimate(buffer []byte, seconds float64) []byte {
	if math.IsNaN(seconds) || seconds < 0 {
		return append(buffer, "--"...)
	}

	if math.IsInf(seconds, 1) {
		return append(buffer, "unbounded"...)
	}

	if seconds < 1 {
		return append(buffer, "<1s"...)
	}

	if seconds < 86400 {
		duration := time.Duration(math.Ceil(seconds)) * time.Second

		return append(buffer, duration.String()...)
	}

	if seconds < 365.25*86400 {
		buffer = strconv.AppendFloat(buffer, seconds/86400, 'f', 1, 64)

		return append(buffer, 'd')
	}

	// Float seconds avoid time.Duration's roughly 292-year overflow limit.
	buffer = strconv.AppendFloat(buffer, seconds/(365.25*86400), 'g', 3, 64)

	return append(buffer, 'y')
}
