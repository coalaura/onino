//go:build !purego

package search

var fastFieldAvailable = supportsBMI2ADX()

//go:noescape
func supportsBMI2ADX() bool

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func multiplyBMI2(result, left, right *fieldElement)

//go:noescape
//go:abiinternal point=AX scratch=BX step=CX ->
func advanceBMI2(point *extendedPoint, scratch *[8]fieldElement, step *affineStep)

func advanceBatch(points []extendedPoint, products []fieldElement) {
	if !fastFieldAvailable {
		advanceBatchGeneric(points, products)

		return
	}

	var scratch [8]fieldElement

	advanceBMI2(&points[0], &scratch, &searchStep)

	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		advanceBMI2(&points[index], &scratch, &searchStep)
		multiplyBMI2(&products[index], &products[index-1], &points[index].zCoordinate)
	}
}
