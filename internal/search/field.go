package search

import (
	"encoding/binary"
	"math/bits"

	"filippo.io/edwards25519/field"
)

// Four full-width limbs represent a residue modulo 2^255-19. Arithmetic may
// retain a noncanonical representative; serialization performs the reduction.
// Folding a carry at bit 256 adds 38, since 2^256 = 38 modulo the field prime.
type fieldElement [4]uint64

//go:inline
func (result *fieldElement) add(left, right *fieldElement) {
	first, carry := bits.Add64(left[0], right[0], 0)
	second, carry := bits.Add64(left[1], right[1], carry)
	third, carry := bits.Add64(left[2], right[2], carry)
	fourth, carry := bits.Add64(left[3], right[3], carry)
	first, carry = bits.Add64(first, carry*38, 0)
	second, carry = bits.Add64(second, 0, carry)
	third, carry = bits.Add64(third, 0, carry)
	fourth, carry = bits.Add64(fourth, 0, carry)

	// If the correction overflowed, the wrapped result is below 38, so a
	// second correction cannot carry out of the first limb.
	first += carry * 38

	*result = fieldElement{first, second, third, fourth}
}

//go:inline
func (result *fieldElement) subtract(left, right *fieldElement) {
	first, borrow := bits.Sub64(left[0], right[0], 0)
	second, borrow := bits.Sub64(left[1], right[1], borrow)
	third, borrow := bits.Sub64(left[2], right[2], borrow)
	fourth, borrow := bits.Sub64(left[3], right[3], borrow)
	first, borrow = bits.Sub64(first, borrow*38, 0)
	second, borrow = bits.Sub64(second, 0, borrow)
	third, borrow = bits.Sub64(third, 0, borrow)
	fourth, borrow = bits.Sub64(fourth, 0, borrow)

	// A second borrow leaves the first limb near 2^64, so subtracting 38
	// again cannot borrow into the remaining limbs.
	first -= borrow * 38
	*result = fieldElement{first, second, third, fourth}
}

//go:inline
func (result *fieldElement) multiply(left, right *fieldElement) {
	if fastFieldAvailable {
		multiplyBMI2(result, left, right)
	} else {
		multiplyGeneric(result, left, right)
	}
}

func (result *fieldElement) invert(source *fieldElement) {
	// One inversion per batch: retain the established fixed addition chain,
	// converting representations only at this cold boundary.
	var encoded [32]byte

	source.putBytes(&encoded)

	value, _ := new(field.Element).SetBytes(encoded[:])

	value.Invert(value)

	result.setField(value)
}

func (result *fieldElement) setField(source *field.Element) {
	encoded := source.Bytes()

	result[0] = binary.LittleEndian.Uint64(encoded[0:8])
	result[1] = binary.LittleEndian.Uint64(encoded[8:16])
	result[2] = binary.LittleEndian.Uint64(encoded[16:24])
	result[3] = binary.LittleEndian.Uint64(encoded[24:32])
}

//go:inline
func (source *fieldElement) canonical() fieldElement {
	first, carry := bits.Add64(source[0], 19*(source[3]>>63), 0)
	second, carry := bits.Add64(source[1], 0, carry)
	third, carry := bits.Add64(source[2], 0, carry)

	fourth := (source[3] & 0x7fffffffffffffff) + carry

	reducedFirst, carry := bits.Add64(first, 19, 0)
	reducedSecond, carry := bits.Add64(second, 0, carry)
	reducedThird, carry := bits.Add64(third, 0, carry)

	reducedFourth := fourth + carry

	mask := uint64(0) - (reducedFourth >> 63)

	return fieldElement{
		first ^ ((first ^ reducedFirst) & mask),
		second ^ ((second ^ reducedSecond) & mask),
		third ^ ((third ^ reducedThird) & mask),
		(fourth ^ ((fourth ^ reducedFourth) & mask)) & 0x7fffffffffffffff,
	}
}

//go:inline
func (source *fieldElement) putBytes(output *[32]byte) {
	canonical := source.canonical()

	binary.LittleEndian.PutUint64(output[0:8], canonical[0])
	binary.LittleEndian.PutUint64(output[8:16], canonical[1])
	binary.LittleEndian.PutUint64(output[16:24], canonical[2])
	binary.LittleEndian.PutUint64(output[24:32], canonical[3])
}

//go:inline
func (source *fieldElement) isNegative() byte {
	canonical := source.canonical()

	return byte(canonical[0] & 1)
}

func multiplyGeneric(result, left, right *fieldElement) {
	var product [8]uint64

	for leftIndex := range left {
		var carry uint64

		for rightIndex := range right {
			high, low := bits.Mul64(left[leftIndex], right[rightIndex])
			low, overflow := bits.Add64(low, product[leftIndex+rightIndex], 0)
			high += overflow
			low, overflow = bits.Add64(low, carry, 0)
			high += overflow
			product[leftIndex+rightIndex] = low
			carry = high
		}

		product[leftIndex+4] = carry
	}

	var (
		folded fieldElement
		carry  uint64
	)

	for index := range folded {
		high, low := bits.Mul64(product[index+4], 38)
		low, overflow := bits.Add64(low, product[index], 0)

		high += overflow

		low, overflow = bits.Add64(low, carry, 0)

		high += overflow

		folded[index] = low

		carry = high
	}

	correction := fieldElement{carry * 38, 0, 0, 0}

	result.add(&folded, &correction)
}
