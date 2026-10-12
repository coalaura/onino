// Preserve the ten-limb test record encoding independently of the arithmetic radix.
struct Encoding { uint limb[10]; };
Encoding storageLimbs(Field value) {
	value = canonical(value);
	Encoding result;
	uint position = 0;
	for (uint index = 0; index < 10; index++) {
		uint width = 26u - (index & 1u);
		uint word = position / 32;
		uint shift = position % 32;
		uint64_t pair = uint64_t(value.limb[word]);
		if (word < 7) { pair |= uint64_t(value.limb[word + 1]) << 32; }
		result.limb[index] = uint(pair >> shift) & ((1u << width) - 1u);
		position += width;
	}
	return result;
}
