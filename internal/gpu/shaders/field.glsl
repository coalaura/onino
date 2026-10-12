#extension GL_EXT_shader_explicit_arithmetic_types_int64 : require
#include "field32.glsl"

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
