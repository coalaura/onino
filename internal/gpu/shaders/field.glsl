// Radix 2^26,2^25 over F_(2^255-19). Tight limbs are < radix; lazy limbs are < 3*radix.
#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
struct Field { uint limb[10]; };

uint width(uint index) { return 26u - (index & 1u); }
uint mask(uint index) { return (1u << width(index)) - 1u; }
Field small(uint value) {
	Field result;
	for (uint index = 0; index < 10; index++) { result.limb[index] = 0; }
	result.limb[0] = value;
	return result;
}
Field normalize(Field value) {
	// Lazy -> tight. The first ripple has carries <= 3 and leaves limb 0 < radix+57.
	// A second wrap requires a small low limb, so its final +19 cannot overflow it.
	for (uint pass = 0; pass < 2; pass++) {
		for (uint index = 0; index < 9; index++) {
			value.limb[index + 1] += value.limb[index] >> width(index);
			value.limb[index] &= mask(index);
		}
		value.limb[0] += 19u * (value.limb[9] >> 25);
		value.limb[9] &= 0x1ffffffu;
	}
	return value;
}
Field carry(uint64_t values[10]) {
	// Mul/square of lazy inputs: every accumulator, including incoming carries, is < 2^63.
	// After the first ripple/fold limb 0 < 2^37. Carry it again before narrowing;
	// limb 1 is then < radix+2048 and all other limbs are tight, safely within uint32.
	for (uint index = 0; index < 9; index++) {
		values[index + 1] += values[index] >> width(index);
		values[index] &= uint64_t(mask(index));
	}
	values[0] += 19ul * (values[9] >> 25);
	values[9] &= 0x1fffffful;
	values[1] += values[0] >> 26;
	values[0] &= 0x3fffffful;
	Field result;
	for (uint index = 0; index < 10; index++) { result.limb[index] = uint(values[index]); }
	return normalize(result);
}
Field add(Field first, Field second) {
	// Tight + tight -> < 2*radix. Consumers must multiply/square or normalize next.
	Field result;
	for (uint index = 0; index < 10; index++) { result.limb[index] = first.limb[index] + second.limb[index]; }
	return result;
}
Field sub(Field first, Field second) {
	// Tight - tight + 2p -> nonnegative and < 3*radix, without uint32 overflow.
	Field result;
	for (uint index = 0; index < 10; index++) {
		uint prime = mask(index) - (index == 0 ? 18u : 0u);
		result.limb[index] = first.limb[index] + 2u * prime - second.limb[index];
	}
	return result;
}
Field mul(Field first, Field second) {
	// Lazy * lazy -> tight; the largest unreduced sum is 5046283313212293387.
	uint64_t values[10];
	for (uint index = 0; index < 10; index++) { values[index] = 0ul; }
	for (uint left = 0; left < 10; left++) {
		for (uint right = 0; right < 10; right++) {
			uint index = left + right;
			uint factor = ((left & right & 1u) != 0u ? 2u : 1u) * (index >= 10 ? 19u : 1u);
			values[index % 10] += uint64_t(first.limb[left]) * second.limb[right] * factor;
		}
	}
	return carry(values);
}
Field square(Field value) {
	// The symmetric sum has the same bound as mul, including doubled off-diagonals.
	uint64_t values[10];
	for (uint index = 0; index < 10; index++) { values[index] = 0ul; }
	for (uint left = 0; left < 10; left++) {
		for (uint right = left; right < 10; right++) {
			uint index = left + right;
			uint factor = ((left & right & 1u) != 0u ? 2u : 1u) * (index >= 10 ? 19u : 1u) * (left == right ? 1u : 2u);
			values[index % 10] += uint64_t(value.limb[left]) * value.limb[right] * factor;
		}
	}
	return carry(values);
}
Field squares(Field value, uint count) {
	// Each square restores tight bounds, independent of the repetition count.
	for (uint index = 0; index < count; index++) { value = square(value); }
	return value;
}
Field inverse(Field value) {
	// Lazy -> tight. Addition chain for 2^255-21: 254 squarings and 11 multiplications.
	Field two = square(value);
	Field nine = mul(squares(two, 2), value);
	Field eleven = mul(nine, two);
	Field five = mul(square(eleven), nine);
	Field ten = mul(squares(five, 5), five);
	Field twenty = mul(squares(ten, 10), ten);
	Field fifty = mul(squares(mul(squares(twenty, 20), twenty), 10), ten);
	Field hundred = mul(squares(fifty, 50), fifty);
	Field twoHundred = mul(squares(hundred, 100), hundred);
	return mul(squares(mul(squares(twoHundred, 50), fifty), 5), eleven);
}
Field canonical(Field value) {
	// Tight -> canonical: the represented integer is < 2^255, so subtract p at most once.
	bool greater = true;
	for (int index = 9; index >= 0; index--) {
		uint prime = mask(uint(index)) - (index == 0 ? 18u : 0u);
		if (value.limb[index] != prime) { greater = value.limb[index] > prime; break; }
	}
	if (greater) {
		uint borrow = 0;
		for (uint index = 0; index < 10; index++) {
			uint prime = mask(index) - (index == 0 ? 18u : 0u);
			uint next = uint(value.limb[index] < prime + borrow);
			value.limb[index] = (value.limb[index] - prime - borrow) & mask(index);
			borrow = next;
		}
	}
	return value;
}
uint64_t firstWord(Field value) {
	return uint64_t(value.limb[0]) | (uint64_t(value.limb[1]) << 26) | (uint64_t(value.limb[2]) << 51);
}
