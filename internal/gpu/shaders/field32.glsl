// Radix-2^32 field operations, reduced modulo 2^256-38 = 2p.
// Multiplication, squaring and reduction adapted from Prefix32 v2.2.0:
// Copyright (c) 2026 0xROOTPLS
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
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

// Every 256-bit input is permitted. Arguments are values, including aliased callers.
struct Field { uint limb[8]; };
Field small(uint value) {
	Field result;
	for (uint index = 0; index < 8; index++) { result.limb[index] = 0; }
	result.limb[0] = value;
	return result;
}
Field fold(Field value, uint carry) {
	// add supplies carry <=1; reduceProduct supplies carry <=38. After
	// wrapping an adjustment <=1444, the entire value is <=1443.
	uint64_t adjustment = uint64_t(carry) * 38ul;
	for (uint index = 0; index < 8; index++) {
		uint64_t sum = uint64_t(value.limb[index]) + adjustment;
		value.limb[index] = uint(sum);
		adjustment = sum >> 32;
	}
	value.limb[0] += uint(adjustment) * 38;
	return value;
}
Field add(Field first, Field second) {
	Field result = small(0);
	uint64_t carry = 0ul;
	for (uint index = 0; index < 8; index++) {
		uint64_t sum = uint64_t(first.limb[index]) + second.limb[index] + carry;
		result.limb[index] = uint(sum);
		carry = sum >> 32;
	}
	return fold(result, uint(carry));
}
Field sub(Field first, Field second) {
	Field result = small(0);
	uint borrow = 0;
	for (uint index = 0; index < 8; index++) {
		uint64_t difference = uint64_t(first.limb[index]) - second.limb[index] - borrow;
		result.limb[index] = uint(difference);
		borrow = uint(difference >> 63);
	}
	// If the first -38 borrows, its wrapped low limb is >=2^32-38;
	// the second -38 therefore cannot borrow into the next limb.
	borrow *= 38;
	for (uint index = 0; index < 8; index++) {
		uint64_t difference = uint64_t(result.limb[index]) - borrow;
		result.limb[index] = uint(difference);
		borrow = uint(difference >> 63);
	}
	result.limb[0] -= borrow * 38;
	return result;
}
Field reduceProduct(uint product[16]) {
	Field result = small(0);
	uint64_t carry = 0ul;
	for (uint index = 0; index < 8; index++) {
		// <=39*(2^32-1)+38; the outgoing carry is <=38.
		uint64_t sum = uint64_t(product[index]) + 38ul * product[index + 8] + carry;
		result.limb[index] = uint(sum);
		carry = sum >> 32;
	}
	return fold(result, uint(carry));
}
uint multiplyAccumulate(uint first, uint second, uint accumulated, inout uint carry) {
	// Each schoolbook cell is <= (2^32-1)^2+2*(2^32-1)=2^64-1.
	uint64_t sum = uint64_t(first) * second + accumulated + carry;
	carry = uint(sum >> 32);
	return uint(sum);
}
Field mul(Field first, Field second) {
	uint product[16];
	for (uint index = 0; index < 16; index++) { product[index] = 0; }
	for (uint left = 0; left < 8; left++) {
		uint carry = 0;
		for (uint right = 0; right < 8; right++) {
			product[left + right] = multiplyAccumulate(first.limb[left], second.limb[right], product[left + right], carry);
		}
		product[left + 8] = carry;
	}
	return reduceProduct(product);
}
Field square(Field value) {
	uint product[16];
	for (uint index = 0; index < 16; index++) { product[index] = 0; }
	for (uint left = 0; left < 7; left++) {
		uint carry = 0;
		for (uint right = left + 1; right < 8; right++) {
			product[left + right] = multiplyAccumulate(value.limb[left], value.limb[right], product[left + right], carry);
		}
		product[left + 8] = carry;
	}
	for (int index = 15; index > 0; index--) { product[index] = (product[index] << 1) | (product[index - 1] >> 31); }
	product[0] = 0;
	uint64_t carry = 0ul;
	for (uint index = 0; index < 8; index++) {
		uint64_t diagonal = uint64_t(value.limb[index]) * value.limb[index];
		uint64_t sum = uint64_t(product[2 * index]) + uint(diagonal) + carry;
		product[2 * index] = uint(sum);
		sum = uint64_t(product[2 * index + 1]) + (diagonal >> 32) + (sum >> 32);
		product[2 * index + 1] = uint(sum);
		carry = sum >> 32;
	}
	return reduceProduct(product);
}
Field canonical(Field value) {
	// A 256-bit representation may be >=2p (the final 38 integers), so two
	// conditional subtractions are required, including an outgoing 256th carry.
	for (uint pass = 0; pass < 2; pass++) {
		Field candidate = small(0);
		uint64_t carry = 19ul;
		for (uint index = 0; index < 8; index++) {
			uint64_t sum = uint64_t(value.limb[index]) + carry;
			candidate.limb[index] = uint(sum);
			carry = sum >> 32;
		}
		if (carry != 0ul || (candidate.limb[7] >> 31) != 0) {
			candidate.limb[7] ^= 0x80000000u;
			value = candidate;
		}
	}
	return value;
}
uint64_t firstWord(Field value) { return uint64_t(value.limb[0]) | (uint64_t(value.limb[1]) << 32); }
uint64_t canonicalFirstWord(Field value) {
	// q=[v>=p]+[v>=2p]. The top bit supplies the ordinary subtraction;
	// the all-ones suffix detects p's final 19 and 2p's final 38 inputs.
	uint quotient = value.limb[7] >> 31;
	uint upper = value.limb[7] | 0x80000000u;
	for (uint index = 1; index < 7; index++) { upper &= value.limb[index]; }
	quotient += uint(upper == 0xffffffffu && value.limb[0] >= 0xffffffedu - 19 * quotient);
	return firstWord(value) + 19ul * quotient;
}
