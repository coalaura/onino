package search

import (
	"encoding/binary"
	"math/bits"

	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/simd"
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
	result.multiplyWith(left, right, defaultFieldMode)
}

//go:inline
func (result *fieldElement) multiplyWith(left, right *fieldElement, mode simd.Mode) {
	switch mode {
	case simd.BMI2ADX:
		multiplyBMI2(result, left, right)
	case simd.BMI2:
		multiplyBMI2Only(result, left, right)
	default:
		multiplyGeneric(result, left, right)
	}
}

//go:inline
func (result *fieldElement) square(source *fieldElement) {
	result.squareWith(source, defaultFieldMode)
}

//go:inline
func (result *fieldElement) squareWith(source *fieldElement, mode simd.Mode) {
	switch mode {
	case simd.BMI2ADX:
		squareBMI2(result, source)
	case simd.BMI2:
		squareBMI2Only(result, source)
	default:
		squareGeneric(result, source)
	}
}

func (result *fieldElement) invert(source *fieldElement) {
	result.invertDivsteps(source)
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
	// A column has at most four products plus the preceding carry. Keep
	// the third word explicitly: the sum can exceed 128 bits.
	// Every input is consumed before storing the possibly aliased result.
	middle, low := bits.Mul64(left[0], right[0])
	product0 := low
	low, middle, high := accumulateProduct(middle, 0, 0, left[0], right[1])
	low, middle, high = accumulateProduct(low, middle, high, left[1], right[0])
	product1 := low
	low, middle, high = accumulateProduct(middle, high, 0, left[0], right[2])
	low, middle, high = accumulateProduct(low, middle, high, left[1], right[1])
	low, middle, high = accumulateProduct(low, middle, high, left[2], right[0])
	product2 := low
	low, middle, high = accumulateProduct(middle, high, 0, left[0], right[3])
	low, middle, high = accumulateProduct(low, middle, high, left[1], right[2])
	low, middle, high = accumulateProduct(low, middle, high, left[2], right[1])
	low, middle, high = accumulateProduct(low, middle, high, left[3], right[0])
	product3 := low
	low, middle, high = accumulateProduct(middle, high, 0, left[1], right[3])
	low, middle, high = accumulateProduct(low, middle, high, left[2], right[2])
	low, middle, high = accumulateProduct(low, middle, high, left[3], right[1])
	product4 := low
	low, middle, high = accumulateProduct(middle, high, 0, left[2], right[3])
	low, middle, high = accumulateProduct(low, middle, high, left[3], right[2])
	product5 := low
	// The complete product is below 2^512, so the final third word is zero.
	low, middle, _ = accumulateProduct(middle, high, 0, left[3], right[3])

	reduceProduct(result, product0, product1, product2, product3, product4, product5, low, middle)
}

func squareGeneric(result, source *fieldElement) {
	// Symmetric products need only ten multiplies, but doubled cross-products
	// need 129 bits. The same 192-bit column bound applies as in multiplication.
	middle, low := bits.Mul64(source[0], source[0])
	product0 := low
	low, middle, high := accumulateDoubleProduct(middle, 0, 0, source[0], source[1])
	product1 := low
	low, middle, high = accumulateDoubleProduct(middle, high, 0, source[0], source[2])
	low, middle, high = accumulateProduct(low, middle, high, source[1], source[1])
	product2 := low
	low, middle, high = accumulateDoubleProduct(middle, high, 0, source[0], source[3])
	low, middle, high = accumulateDoubleProduct(low, middle, high, source[1], source[2])
	product3 := low
	low, middle, high = accumulateDoubleProduct(middle, high, 0, source[1], source[3])
	low, middle, high = accumulateProduct(low, middle, high, source[2], source[2])
	product4 := low
	low, middle, high = accumulateDoubleProduct(middle, high, 0, source[2], source[3])
	product5 := low
	// As in multiplication, no bit can survive beyond the eighth word.
	low, middle, _ = accumulateProduct(middle, high, 0, source[3], source[3])

	reduceProduct(result, product0, product1, product2, product3, product4, product5, low, middle)
}

func accumulateProduct(low, middle, high, left, right uint64) (uint64, uint64, uint64) {
	productHigh, productLow := bits.Mul64(left, right)
	low, carry := bits.Add64(low, productLow, 0)
	middle, carry = bits.Add64(middle, productHigh, carry)
	high += carry

	return low, middle, high
}

func accumulateDoubleProduct(low, middle, high, left, right uint64) (uint64, uint64, uint64) {
	productHigh, productLow := bits.Mul64(left, right)
	// Doubling a full-width product needs 129 bits, including this top bit.
	top := productHigh >> 63
	productHigh = productHigh<<1 | productLow>>63
	productLow <<= 1
	low, carry := bits.Add64(low, productLow, 0)
	middle, carry = bits.Add64(middle, productHigh, carry)
	high += top + carry

	return low, middle, high
}

func multiplyAdd(left, right, word, carry uint64) (uint64, uint64) {
	// A product plus two words is at most 2^128-1.
	high, low := bits.Mul64(left, right)
	low, overflow := bits.Add64(low, word, 0)
	high += overflow
	low, overflow = bits.Add64(low, carry, 0)
	high += overflow

	return low, high
}

//go:inline
func reduceProduct(result *fieldElement, product0, product1, product2, product3, product4, product5, product6, product7 uint64) {
	first, carry := multiplyAdd(product4, 38, product0, 0)
	second, carry := multiplyAdd(product5, 38, product1, carry)
	third, carry := multiplyAdd(product6, 38, product2, carry)
	fourth, carry := multiplyAdd(product7, 38, product3, carry)

	// The first fold leaves carry <= 38. After adding at most 1444,
	// any overflow leaves a word below 1444, so the final 38 cannot carry.
	first, carry = bits.Add64(first, carry*38, 0)
	second, carry = bits.Add64(second, 0, carry)
	third, carry = bits.Add64(third, 0, carry)
	fourth, carry = bits.Add64(fourth, 0, carry)
	first += carry * 38

	*result = fieldElement{first, second, third, fourth}
}
