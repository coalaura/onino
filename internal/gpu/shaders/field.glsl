// Radix 2^26,2^25 over F_(2^255-19). All public operations return bounded limbs.
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
Field carry(uint64_t values[10]) {
	for (uint pass = 0; pass < 3; pass++) {
		for (uint index = 0; index < 9; index++) {
			values[index + 1] += values[index] >> width(index);
			values[index] &= uint64_t(mask(index));
		}
		values[0] += 19ul * (values[9] >> 25);
		values[9] &= 0x1fffffful;
	}
	Field result;
	for (uint index = 0; index < 10; index++) { result.limb[index] = uint(values[index]); }
	return result;
}
Field add(Field first, Field second) {
	uint64_t values[10];
	for (uint index = 0; index < 10; index++) { values[index] = uint64_t(first.limb[index]) + second.limb[index]; }
	return carry(values);
}
Field sub(Field first, Field second) {
	uint64_t values[10];
	for (uint index = 0; index < 10; index++) {
		uint prime = mask(index) - (index == 0 ? 18u : 0u);
		values[index] = uint64_t(first.limb[index]) + 2ul * prime - second.limb[index];
	}
	return carry(values);
}
Field mul(Field first, Field second) {
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
	for (uint index = 0; index < count; index++) { value = square(value); }
	return value;
}
Field inverse(Field value) {
	// Addition chain for 2^255-21: 254 squarings and 11 multiplications.
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
