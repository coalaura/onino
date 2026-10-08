package main

import (
	"math"
)

type matchEstimate struct {
	candidates50 float64
	candidates95 float64
}

func newMatchEstimate(probability float64) matchEstimate {
	if math.IsNaN(probability) || math.IsInf(probability, 0) || probability < 0 || probability > 1 {
		return matchEstimate{candidates50: math.NaN(), candidates95: math.NaN()}
	}

	if probability == 1 {
		return matchEstimate{candidates50: 1, candidates95: 1}
	}

	if probability == 0 {
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
	buffer = append(buffer, "  Candidates  50% ~"...)
	buffer = appendCandidateCount(buffer, estimate.candidates50)
	buffer = append(buffer, "; 95% ~"...)
	buffer = appendCandidateCount(buffer, estimate.candidates95)

	return append(buffer, "\n  Waits estimate a new match from now at the recent combined rate.\n  Uniform-key model; overlaps accounted for, large unions approximated.\n"...)
}

func appendCandidateCount(buffer []byte, count float64) []byte {
	if math.IsInf(count, 1) {
		return append(buffer, "impossible"...)
	}

	return appendCompact(buffer, count)
}

func appendWait(buffer []byte, candidates, rate float64) []byte {
	if math.IsNaN(candidates) || candidates <= 0 {
		return append(buffer, "--"...)
	}

	if math.IsInf(candidates, 1) {
		return append(buffer, "impossible"...)
	}

	if rate == 0 {
		return append(buffer, "stalled"...)
	}

	if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return append(buffer, "--"...)
	}

	buffer = append(buffer, '~')

	seconds := candidates / rate

	if math.IsInf(seconds, 1) {
		// Divide in log space only when a finite ratio exceeds float64 seconds.
		logYears := math.Log10(candidates) - math.Log10(rate) - math.Log10(365.25*86400)
		exponent := math.Floor(logYears)

		buffer = appendCompact(buffer, math.Pow(10, logYears-exponent))
		buffer = append(buffer, 'e')
		buffer = appendExact(buffer, uint64(exponent))

		return append(buffer, 'y')
	}

	return appendWaitEstimate(buffer, seconds)
}

func appendWaitEstimate(buffer []byte, seconds float64) []byte {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 {
		return append(buffer, "--"...)
	}

	if seconds < 1 {
		return append(buffer, "<1s"...)
	}

	if seconds < 60 {
		buffer = appendCompact(buffer, math.Ceil(seconds))

		return append(buffer, 's')
	}

	if seconds < 3600 {
		buffer = appendCompact(buffer, seconds/60)

		return append(buffer, 'm')
	}

	if seconds < 86400 {
		buffer = appendCompact(buffer, seconds/3600)

		return append(buffer, 'h')
	}

	if seconds < 365.25*86400 {
		buffer = appendCompact(buffer, seconds/86400)

		return append(buffer, 'd')
	}

	// Float seconds avoid time.Duration's roughly 292-year overflow limit.
	years := seconds / (365.25 * 86400)
	if years >= 10000 {
		buffer = appendScientific(buffer, years)
	} else {
		buffer = appendCompact(buffer, years)
	}

	return append(buffer, 'y')
}
