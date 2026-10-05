// Copyright (c) 2020 Peter Dettman
// Copyright (c) 2026 coalaura
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package search

import "math/bits"

// Adapted from libsecp256k1/src/modinv64_impl.h, specialized to 2^255-19.
// Ten fixed groups of 59 half-delta divsteps suffice for 256-bit inputs;
// matrices are scaled by 2^62. See doc/safegcd_implementation.md upstream
// for the half-delta bound and the coefficient bounds used below.
const (
	divstepsMask         = int64(1<<62 - 1)
	divstepsPrimeInverse = uint64(0x39435e50d79435e5)
)

// Radix 2^62 with four nonnegative low limbs and a signed top limb.
type divstepsInteger [5]int64

type divstepsMatrix struct {
	firstFirst   int64
	firstSecond  int64
	secondFirst  int64
	secondSecond int64
}

type divstepsWide struct {
	low  uint64
	high int64
}

var divstepsPrime = divstepsInteger{divstepsMask - 18, divstepsMask, divstepsMask, divstepsMask, 127}

func (result *fieldElement) invertDivsteps(source *fieldElement) {
	// Canonicalize before conversion, including arbitrary 256-bit inputs.
	// Zero maps to zero; copying first permits result == source.
	value := source.canonical()
	first := divstepsPrime

	second := divstepsInteger{
		int64(value[0]) & divstepsMask,
		int64(value[0]>>62|value[1]<<2) & divstepsMask,
		int64(value[1]>>60|value[2]<<4) & divstepsMask,
		int64(value[2]>>58|value[3]<<6) & divstepsMask,
		int64(value[3] >> 56),
	}

	left := divstepsInteger{}
	right := divstepsInteger{1, 0, 0, 0, 0}
	zeta := int64(-1)

	for range 10 {
		var matrix divstepsMatrix

		zeta = divsteps59(zeta, uint64(first[0]), uint64(second[0]), &matrix)

		divstepsUpdateCoefficients(&left, &right, &matrix)
		divstepsUpdateIntegers(&first, &second, &matrix)
	}

	left.normalize(first[4])

	*result = fieldElement{
		uint64(left[0]) | uint64(left[1])<<62,
		uint64(left[1])>>2 | uint64(left[2])<<60,
		uint64(left[2])>>4 | uint64(left[3])<<58,
		uint64(left[3])>>6 | uint64(left[4])<<56,
	}
}

func (value *divstepsInteger) normalize(sign int64) {
	// Coefficients lie in (-2p, p). Correct the gcd's sign and bring the
	// result to [0, p) with two masked additions and fixed carry passes.
	negative := value[4] >> 63
	negate := sign >> 63

	for index := range value {
		value[index] += divstepsPrime[index] & negative
		value[index] = (value[index] ^ negate) - negate
	}

	for index := range 4 {
		value[index+1] += value[index] >> 62
		value[index] &= divstepsMask
	}

	negative = value[4] >> 63

	for index := range value {
		value[index] += divstepsPrime[index] & negative
	}

	for index := range 4 {
		value[index+1] += value[index] >> 62
		value[index] &= divstepsMask
	}
}

//go:inline
func (value *divstepsWide) accumulate(left, right int64) {
	// Correct the unsigned high half to obtain a signed 128-bit product.
	high, low := bits.Mul64(uint64(left), uint64(right))
	high -= uint64(left>>63) & uint64(right)
	high -= uint64(right>>63) & uint64(left)

	low, carry := bits.Add64(value.low, low, 0)

	value.low = low
	value.high += int64(high + carry)
}

//go:inline
func (value *divstepsWide) shift() {
	value.low = value.low>>62 | uint64(value.high)<<2
	value.high >>= 62
}

func divsteps59(zeta int64, first, second uint64, matrix *divstepsMatrix) int64 {
	// zeta = -(delta + 1/2); initial 8 scales 59 steps to a 62-bit shift.
	firstFirst := uint64(8)

	var (
		firstSecond uint64
		secondFirst uint64
	)

	secondSecond := uint64(8)

	for range 59 {
		negative := uint64(zeta >> 63)
		odd := uint64(0) - (second & 1)
		firstChange := (first ^ negative) - negative
		rowFirstChange := (firstFirst ^ negative) - negative
		rowSecondChange := (firstSecond ^ negative) - negative
		second += firstChange & odd
		secondFirst += rowFirstChange & odd
		secondSecond += rowSecondChange & odd
		negative &= odd
		zeta = int64(uint64(zeta)^negative) - 1
		first += second & negative
		firstFirst += secondFirst & negative
		firstSecond += secondSecond & negative
		second >>= 1
		firstFirst <<= 1
		firstSecond <<= 1
	}

	*matrix = divstepsMatrix{
		firstFirst:   int64(firstFirst),
		firstSecond:  int64(firstSecond),
		secondFirst:  int64(secondFirst),
		secondSecond: int64(secondSecond),
	}

	return zeta
}

func divstepsUpdateIntegers(first, second *divstepsInteger, matrix *divstepsMatrix) {
	var (
		firstCarry  divstepsWide
		secondCarry divstepsWide
	)

	firstCarry.accumulate(matrix.firstFirst, first[0])
	firstCarry.accumulate(matrix.firstSecond, second[0])
	secondCarry.accumulate(matrix.secondFirst, first[0])
	secondCarry.accumulate(matrix.secondSecond, second[0])

	// The transition guarantees 62 zero low bits before this division.
	firstCarry.shift()
	secondCarry.shift()

	for index := 1; index < 5; index++ {
		firstCarry.accumulate(matrix.firstFirst, first[index])
		firstCarry.accumulate(matrix.firstSecond, second[index])
		secondCarry.accumulate(matrix.secondFirst, first[index])
		secondCarry.accumulate(matrix.secondSecond, second[index])

		first[index-1] = int64(firstCarry.low) & divstepsMask
		second[index-1] = int64(secondCarry.low) & divstepsMask

		firstCarry.shift()
		secondCarry.shift()
	}

	first[4] = int64(firstCarry.low)
	second[4] = int64(secondCarry.low)
}

func divstepsUpdateCoefficients(left, right *divstepsInteger, matrix *divstepsMatrix) {
	// Add multiples of p to cancel the low 62 bits while keeping both
	// coefficients in (-2p, p), then divide by 2^62 exactly.
	leftSign := left[4] >> 63
	rightSign := right[4] >> 63

	leftCorrection := (matrix.firstFirst & leftSign) + (matrix.firstSecond & rightSign)
	rightCorrection := (matrix.secondFirst & leftSign) + (matrix.secondSecond & rightSign)

	var (
		leftCarry  divstepsWide
		rightCarry divstepsWide
	)

	leftCarry.accumulate(matrix.firstFirst, left[0])
	leftCarry.accumulate(matrix.firstSecond, right[0])
	rightCarry.accumulate(matrix.secondFirst, left[0])
	rightCarry.accumulate(matrix.secondSecond, right[0])

	leftCorrection -= int64(divstepsPrimeInverse*leftCarry.low+uint64(leftCorrection)) & divstepsMask
	rightCorrection -= int64(divstepsPrimeInverse*rightCarry.low+uint64(rightCorrection)) & divstepsMask

	leftCarry.accumulate(divstepsPrime[0], leftCorrection)
	rightCarry.accumulate(divstepsPrime[0], rightCorrection)

	leftCarry.shift()
	rightCarry.shift()

	for index := 1; index < 5; index++ {
		leftCarry.accumulate(matrix.firstFirst, left[index])
		leftCarry.accumulate(matrix.firstSecond, right[index])
		rightCarry.accumulate(matrix.secondFirst, left[index])
		rightCarry.accumulate(matrix.secondSecond, right[index])
		leftCarry.accumulate(divstepsPrime[index], leftCorrection)
		rightCarry.accumulate(divstepsPrime[index], rightCorrection)

		left[index-1] = int64(leftCarry.low) & divstepsMask
		right[index-1] = int64(rightCarry.low) & divstepsMask

		leftCarry.shift()
		rightCarry.shift()
	}

	left[4] = int64(leftCarry.low)
	right[4] = int64(rightCarry.low)
}
