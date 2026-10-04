//go:build !amd64 || purego

package search

const fastFieldAvailable = false

func multiplyBMI2(result, left, right *fieldElement) {
	multiplyGeneric(result, left, right)
}

func advanceBatch(points []extendedPoint, products []fieldElement) {
	advanceBatchGeneric(points, products)
}

func normalizeBMI2(point *extendedPoint, product *fieldElement, publicKey *[32]byte, count int, reciprocal *fieldElement) {
	panic("unavailable")
}
