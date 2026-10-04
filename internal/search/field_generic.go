//go:build !amd64 || purego

package search

const fastFieldAvailable = false

func multiplyBMI2(result, left, right *fieldElement) {
	multiplyGeneric(result, left, right)
}
