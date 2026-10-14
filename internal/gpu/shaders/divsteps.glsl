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
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

// Batched half-delta recurrence from libsecp256k1, adapted from the local
// CPU implementation to nine signed radix-2^30 limbs and 20 batches of 30.
const int divMask = 0x3fffffff;
struct SignedField { int limb[9]; };
struct Transition { int firstFirst; int firstSecond; int secondFirst; int secondSecond; };
int modulusLimb(uint index) { return index == 8 ? 32767 : (index == 0 ? divMask - 18 : divMask); }

Transition divsteps30(inout int zeta, uint first, uint second) {
	uint firstFirst = 1;
	uint firstSecond = 0;
	uint secondFirst = 0;
	uint secondSecond = 1;
	for (uint step = 0; step < 30; step++) {
		uint negative = uint(zeta >> 31);
		uint odd = 0u - (second & 1u);
		second += ((first ^ negative) - negative) & odd;
		secondFirst += ((firstFirst ^ negative) - negative) & odd;
		secondSecond += ((firstSecond ^ negative) - negative) & odd;
		negative &= odd;
		zeta = int(uint(zeta) ^ negative) - 1;
		first += second & negative;
		firstFirst += secondFirst & negative;
		firstSecond += secondSecond & negative;
		second >>= 1;
		firstFirst <<= 1;
		firstSecond <<= 1;
	}
	return Transition(int(firstFirst), int(firstSecond), int(secondFirst), int(secondSecond));
}

void updateIntegers(inout SignedField first, inout SignedField second, Transition matrix) {
	int64_t firstCarry = int64_t(matrix.firstFirst) * first.limb[0] + int64_t(matrix.firstSecond) * second.limb[0];
	int64_t secondCarry = int64_t(matrix.secondFirst) * first.limb[0] + int64_t(matrix.secondSecond) * second.limb[0];
	firstCarry >>= 30;
	secondCarry >>= 30;
	for (uint index = 1; index < 9; index++) {
		firstCarry += int64_t(matrix.firstFirst) * first.limb[index] + int64_t(matrix.firstSecond) * second.limb[index];
		secondCarry += int64_t(matrix.secondFirst) * first.limb[index] + int64_t(matrix.secondSecond) * second.limb[index];
		first.limb[index - 1] = int(firstCarry) & divMask;
		second.limb[index - 1] = int(secondCarry) & divMask;
		firstCarry >>= 30;
		secondCarry >>= 30;
	}
	first.limb[8] = int(firstCarry);
	second.limb[8] = int(secondCarry);
}

void updateCoefficients(inout SignedField first, inout SignedField second, Transition matrix) {
	int firstSign = first.limb[8] >> 31;
	int secondSign = second.limb[8] >> 31;
	int firstCorrection = (matrix.firstFirst & firstSign) + (matrix.firstSecond & secondSign);
	int secondCorrection = (matrix.secondFirst & firstSign) + (matrix.secondSecond & secondSign);
	int64_t firstCarry = int64_t(matrix.firstFirst) * first.limb[0] + int64_t(matrix.firstSecond) * second.limb[0];
	int64_t secondCarry = int64_t(matrix.secondFirst) * first.limb[0] + int64_t(matrix.secondSecond) * second.limb[0];
	// (-19)^-1 modulo 2^30, derived for this modulus, not secp256k1.
	firstCorrection -= int(0x179435e5u * uint(firstCarry) + uint(firstCorrection)) & divMask;
	secondCorrection -= int(0x179435e5u * uint(secondCarry) + uint(secondCorrection)) & divMask;
	firstCarry += int64_t(firstCorrection) * modulusLimb(0);
	secondCarry += int64_t(secondCorrection) * modulusLimb(0);
	firstCarry >>= 30;
	secondCarry >>= 30;
	for (uint index = 1; index < 9; index++) {
		firstCarry += int64_t(matrix.firstFirst) * first.limb[index] + int64_t(matrix.firstSecond) * second.limb[index] + int64_t(firstCorrection) * modulusLimb(index);
		secondCarry += int64_t(matrix.secondFirst) * first.limb[index] + int64_t(matrix.secondSecond) * second.limb[index] + int64_t(secondCorrection) * modulusLimb(index);
		first.limb[index - 1] = int(firstCarry) & divMask;
		second.limb[index - 1] = int(secondCarry) & divMask;
		firstCarry >>= 30;
		secondCarry >>= 30;
	}
	first.limb[8] = int(firstCarry);
	second.limb[8] = int(secondCarry);
}

Field inverseBatched(Field value) {
	value = canonical(value);
	SignedField first;
	SignedField second;
	SignedField left;
	SignedField right;
	for (uint index = 0; index < 9; index++) {
		first.limb[index] = modulusLimb(index);
		uint bit = index * 30;
		uint word = bit / 32;
		uint shift = bit % 32;
		uint bits = value.limb[word] >> shift;
		if (shift != 0 && word < 7) { bits |= value.limb[word + 1] << (32 - shift); }
		second.limb[index] = int(bits) & divMask;
		left.limb[index] = 0;
		right.limb[index] = index == 0 ? 1 : 0;
	}
	int zeta = -1;
	for (uint batch = 0; batch < 20; batch++) {
		Transition matrix = divsteps30(zeta, uint(first.limb[0]), uint(second.limb[0]));
		updateCoefficients(left, right, matrix);
		updateIntegers(first, second, matrix);
	}
	int negative = left.limb[8] >> 31;
	int negate = first.limb[8] >> 31;
	for (uint index = 0; index < 9; index++) {
		left.limb[index] += modulusLimb(index) & negative;
		left.limb[index] = (left.limb[index] ^ negate) - negate;
	}
	for (uint index = 0; index < 8; index++) {
		left.limb[index + 1] += left.limb[index] >> 30;
		left.limb[index] &= divMask;
	}
	negative = left.limb[8] >> 31;
	for (uint index = 0; index < 9; index++) { left.limb[index] += modulusLimb(index) & negative; }
	for (uint index = 0; index < 8; index++) {
		left.limb[index + 1] += left.limb[index] >> 30;
		left.limb[index] &= divMask;
	}
	Field result = small(0);
	for (uint index = 0; index < 9; index++) {
		uint bit = index * 30;
		uint word = bit / 32;
		uint shift = bit % 32;
		result.limb[word] |= uint(left.limb[index]) << shift;
		if (shift != 0 && word < 7) { result.limb[word + 1] |= uint(left.limb[index]) >> (32 - shift); }
	}
	return result;
}
