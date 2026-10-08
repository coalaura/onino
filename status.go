package main

import (
	"math"
	"strconv"
	"time"

	"github.com/coalaura/onino/internal/search"
)

const decimalUnits = "kMGTPE"

func appendSearchStatus(buffer []byte, stats search.Stats, elapsed time.Duration, rate float64, estimate matchEstimate, phase string) []byte {
	buffer = appendElapsed(buffer, elapsed)
	buffer = append(buffer, " | "...)

	if phase != "" {
		buffer = append(buffer, phase...)
		buffer = append(buffer, " | "...)
	}

	buffer = appendCompact(buffer, float64(stats.Checked))
	buffer = append(buffer, " checked | "...)
	buffer = appendCompact(buffer, rate)
	buffer = append(buffer, "/s | saved "...)
	buffer = appendCompact(buffer, float64(stats.Saved))
	buffer = append(buffer, " | wait 50% "...)
	buffer = appendWait(buffer, estimate.candidates50, rate)
	buffer = append(buffer, " / 95% "...)
	buffer = appendWait(buffer, estimate.candidates95, rate)

	return append(buffer, '\n')
}

func appendCompact(buffer []byte, value float64) []byte {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return append(buffer, "--"...)
	}

	if value == 0 {
		return append(buffer, '0')
	}

	if value < 0.01 || value >= 999.5e18 {
		return appendScientific(buffer, value)
	}

	unit := -1

	for value >= 1000 && unit < len(decimalUnits)-1 {
		value /= 1000
		unit++
	}

	precision := max(0, 2-int(math.Floor(math.Log10(value))))

	factor := math.Pow10(precision)
	value = math.Round(value*factor) / factor

	if value >= 1000 && unit < len(decimalUnits)-1 {
		value /= 1000
		unit++
	}

	buffer = strconv.AppendFloat(buffer, value, 'f', -1, 64)

	if unit >= 0 {
		buffer = append(buffer, decimalUnits[unit])
	}

	return buffer
}

func appendScientific(buffer []byte, value float64) []byte {
	var digits [32]byte

	number := strconv.AppendFloat(digits[:0], value, 'e', 2, 64)
	exponent := 0

	for number[exponent] != 'e' {
		exponent++
	}

	end := exponent

	for end > 0 && number[end-1] == '0' {
		end--
	}

	if end > 0 && number[end-1] == '.' {
		end--
	}

	buffer = append(buffer, number[:end]...)
	buffer = append(buffer, 'e')

	if number[exponent+1] == '-' {
		buffer = append(buffer, '-')
	}

	start := exponent + 2

	for start < len(number)-1 && number[start] == '0' {
		start++
	}

	return append(buffer, number[start:]...)
}

func appendElapsed(buffer []byte, elapsed time.Duration) []byte {
	seconds := max(0, elapsed.Seconds())
	if seconds < 60 {
		buffer = strconv.AppendFloat(buffer, math.Floor(seconds), 'f', 0, 64)

		return append(buffer, 's')
	}

	if seconds < 86400 {
		return append(buffer, elapsed.Truncate(time.Second).String()...)
	}

	buffer = appendCompact(buffer, seconds/86400)

	return append(buffer, 'd')
}

func appendExact(buffer []byte, count uint64) []byte {
	var digits [32]byte

	return appendGroupedDigits(buffer, strconv.AppendUint(digits[:0], count, 10))
}

func appendGroupedDigits(buffer, digits []byte) []byte {
	first := len(digits) % 3
	if first == 0 {
		first = min(3, len(digits))
	}

	buffer = append(buffer, digits[:first]...)

	for index := first; index < len(digits); index += 3 {
		buffer = append(buffer, ',')
		buffer = append(buffer, digits[index:index+3]...)
	}

	return buffer
}
