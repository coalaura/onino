//go:build !amd64 || purego

package search

const fastFieldAvailable = false

func multiplyBMI2(result, left, right *fieldElement) {
	multiplyGeneric(result, left, right)
}

func advanceBatch(points []extendedPoint, products []fieldElement) {
	advanceBatchGeneric(points, products)
}
