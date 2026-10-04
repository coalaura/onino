package main

import (
	"strconv"
	"time"

	"github.com/coalaura/onino/internal/search"
)

func appendSearchStatus(buffer []byte, stats search.Stats, elapsed time.Duration, rate float64, final bool) []byte {
	var digits [32]byte

	number := strconv.AppendUint(digits[:0], stats.Checked, 10)

	buffer = append(buffer, "Checked "...)
	buffer = appendGroupedDigits(buffer, number)
	buffer = append(buffer, " keys"...)

	if final {
		number = strconv.AppendUint(digits[:0], stats.Saved, 10)
		buffer = append(buffer, ", saved "...)
		buffer = appendGroupedDigits(buffer, number)
		buffer = append(buffer, " matches"...)
	}

	buffer = append(buffer, " in "...)
	buffer = append(buffer, elapsed.String()...)
	buffer = append(buffer, " ("...)

	number = strconv.AppendFloat(digits[:0], rate, 'f', 0, 64)

	buffer = appendGroupedDigits(buffer, number)

	return append(buffer, " keys/s).\n"...)
}

func appendGroupedDigits(buffer, digits []byte) []byte {
	first := len(digits) % 3
	if first == 0 {
		first = 3
	}

	buffer = append(buffer, digits[:first]...)

	for index := first; index < len(digits); index += 3 {
		buffer = append(buffer, ',')
		buffer = append(buffer, digits[index:index+3]...)
	}

	return buffer
}
