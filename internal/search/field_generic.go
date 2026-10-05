//go:build !amd64 || purego

package search

const fastFieldAvailable = false

func multiplyBMI2(result, left, right *fieldElement) {
	multiplyGeneric(result, left, right)
}

func squareBMI2(result, source *fieldElement) {
	multiplyGeneric(result, source, source)
}

func advanceBatch(points []extendedPoint, products []fieldElement) {
	advanceBatchGeneric(points, products)
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
