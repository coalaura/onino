//go:build !purego

package search

import "github.com/coalaura/onino/internal/simd"

const scalarAssemblyAvailable = true

var fastFieldAvailable = supportsBMI2ADX()

//go:noescape
func supportsBMI2ADX() bool

// The historical *BMI2 entry points require both BMI2 and ADX. The separate
// *BMI2Only leaves use ordinary carries and are selected by --simd=bmi2.
//
//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func multiplyBMI2(result, left, right *fieldElement)

//go:noescape
//go:abiinternal result=AX source=BX ->
func squareBMI2(result, source *fieldElement)

//go:noescape
//go:abiinternal point=AX scratch=BX step=CX ->
func advanceBMI2(point *extendedPoint, scratch *[8]fieldElement, step *affineStep)

//go:noescape
//go:abiinternal point=AX product=BX publicKey=CX count=DI reciprocal=SI ->
func normalizeBMI2(point *extendedPoint, product *fieldElement, publicKey *[32]byte, count int, reciprocal *fieldElement)

//go:noescape
//go:abiinternal center=AX scratch=BX offset=CX ->
func pairedPrepareBMI2(center *pairedAffine, scratch *pairedScratch, offset *pairedAffine)

//go:noescape
//go:abiinternal scratch=AX inverse=BX ->
func pairedInverseBMI2(scratch *pairedScratch, inverse *fieldElement)

func advanceBatch(points []extendedPoint, products []fieldElement) {
	advanceBatchWith(points, products, defaultFieldMode)
}

func advanceBatchWith(points []extendedPoint, products []fieldElement, mode simd.Mode) {
	if mode == simd.Portable {
		advanceBatchGenericWith(points, products, mode)

		return
	}

	var scratch [8]fieldElement

	if mode == simd.BMI2 {
		advanceBMI2Only(&points[0], &scratch, &searchStep)

		products[0] = points[0].zCoordinate

		for index := 1; index < len(points); index++ {
			advanceBMI2Only(&points[index], &scratch, &searchStep)
			multiplyBMI2Only(&products[index], &products[index-1], &points[index].zCoordinate)
		}

		return
	}

	advanceBMI2(&points[0], &scratch, &searchStep)

	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		advanceBMI2(&points[index], &scratch, &searchStep)
		multiplyBMI2(&products[index], &products[index-1], &points[index].zCoordinate)
	}
}
