//go:build !amd64 || purego

package search

import "github.com/coalaura/onino/internal/simd"

const (
	fastFieldAvailable      = false
	scalarAssemblyAvailable = false
)

func multiplyBMI2(result, left, right *fieldElement) {
	panic("unavailable BMI2+ADX arithmetic")
}

func squareBMI2(result, source *fieldElement) {
	panic("unavailable BMI2+ADX arithmetic")
}

func advanceBatch(points []extendedPoint, products []fieldElement) {
	advanceBatchGenericWith(points, products, simd.Portable)
}

func advanceBatchWith(points []extendedPoint, products []fieldElement, mode simd.Mode) {
	advanceBatchGenericWith(points, products, mode)
}

func normalizeBMI2(point *extendedPoint, product *fieldElement, publicKey *[32]byte, count int, reciprocal *fieldElement) {
	panic("unavailable")
}

func pairedPrepareBMI2(center *pairedAffine, scratch *pairedScratch, offset *pairedAffine) {
	panic("unavailable")
}

func pairedInverseBMI2(scratch *pairedScratch, inverse *fieldElement) {
	panic("unavailable")
}

func multiplyBMI2Only(result, left, right *fieldElement) {
	panic("unavailable BMI2 arithmetic")
}

func squareBMI2Only(result, source *fieldElement) {
	panic("unavailable BMI2 arithmetic")
}

func normalizeBMI2Only(point *extendedPoint, product *fieldElement, publicKey *[32]byte, count int, reciprocal *fieldElement) {
	panic("unavailable BMI2 arithmetic")
}

func pairedPrepareBMI2Only(center *pairedAffine, scratch *pairedScratch, offset *pairedAffine) {
	panic("unavailable BMI2 arithmetic")
}

func pairedInverseBMI2Only(scratch *pairedScratch, inverse *fieldElement) {
	panic("unavailable BMI2 arithmetic")
}
