#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#extension GL_EXT_control_flow_attributes : require
#include "field32.glsl"
#include "divsteps.glsl"

uint64_t provisionalProductWord(Field first, Field second) {
	// K is accumulated modulo 2^128. Column sums need at most 70 bits.
	uint64_t top = uint64_t(first.limb[7]) * second.limb[7];
	uint64_t lower = uint64_t(uint(top)) * 38ul;
	uint64_t upper = (top >> 32) * 38ul + (lower >> 32);
	uint64_t accumulated = (upper << 32) | uint(lower);
	uint overflow = uint(upper >> 32);
	uint words[4];
	[[unroll]] for (uint column = 6; column <= 9; column++) {
		// Separate sums fit in 64 bits: low < 9*2^32, high < 46*2^32.
		uint64_t lowSum = uint(accumulated);
		uint64_t highSum = (accumulated >> 32) + (uint64_t(overflow) << 32);
		[[unroll]] for (uint left = column > 7 ? column - 7 : 0; left < 8 && left <= column; left++) {
			uint64_t product = uint64_t(first.limb[left]) * second.limb[column - left];
			lowSum += uint(product);
			highSum += product >> 32;
		}
		words[column - 6] = uint(lowSum);
		accumulated = highSum + (lowSum >> 32);
		overflow = 0;
	}
	uint64_t quotient = uint64_t(words[1] >> 31) | (uint64_t(words[2]) << 1) | (uint64_t(words[3]) << 33);
	uint64_t low = uint64_t(first.limb[0]) * second.limb[0];
	uint cross = first.limb[0] * second.limb[1] + first.limb[1] * second.limb[0];
	return low + (uint64_t(cross) << 32) + 19ul * quotient;
}

Field denominatorPair(Field centerSquare, Field sum, Field product) {
	return add(small(1), mul(centerSquare, sub(mul(centerSquare, product), sum)));
}
uint provisionalProductLow(Field first, Field second) {
	// Only quotient bits 0..31 survive; column 9 starts at quotient bit 33.
	uint64_t top = uint64_t(first.limb[7]) * second.limb[7];
	uint64_t lower = uint64_t(uint(top)) * 38ul;
	uint64_t upper = (top >> 32) * 38ul + (lower >> 32);
	uint64_t accumulated = (upper << 32) | uint(lower);
	uint overflow = uint(upper >> 32);
	uint words[3];
	[[unroll]] for (uint column = 6; column <= 8; column++) {
		uint64_t lowSum = uint(accumulated);
		uint64_t highSum = (accumulated >> 32) + (uint64_t(overflow) << 32);
		[[unroll]] for (uint left = column > 7 ? column - 7 : 0; left < 8 && left <= column; left++) {
			uint64_t product = uint64_t(first.limb[left]) * second.limb[column - left];
			lowSum += uint(product);
			highSum += product >> 32;
		}
		words[column - 6] = uint(lowSum);
		accumulated = highSum + (lowSum >> 32);
		overflow = 0;
	}
	uint quotient = (words[1] >> 31) | (words[2] << 1);
	return first.limb[0] * second.limb[0] + 19u * quotient;
}

Field squares(Field value, uint count) {
	for (uint index = 0; index < count; index++) { value = square(value); }
	return value;
}
Field inverse(Field value) { return inverseBatched(value); }
