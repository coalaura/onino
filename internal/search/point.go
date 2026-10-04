package search

import (
	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
)

type extendedPoint struct {
	xCoordinate fieldElement
	yCoordinate fieldElement
	zCoordinate fieldElement
	tCoordinate fieldElement
}

type affineStep struct {
	yPlusX  fieldElement
	yMinusX fieldElement
	xy2D    fieldElement
}

var searchStep = makeSearchStep()

func (point *extendedPoint) set(source *edwards25519.Point) {
	xCoordinate, yCoordinate, zCoordinate, tCoordinate := source.ExtendedCoordinates()
	point.xCoordinate.setField(xCoordinate)
	point.yCoordinate.setField(yCoordinate)
	point.zCoordinate.setField(zCoordinate)
	point.tCoordinate.setField(tCoordinate)
}

//go:inline
func (point *extendedPoint) advance() {
	// Complete mixed Edwards addition, with the fixed affine point 8*B.
	// Precomputing its three products removes the general-point conversion
	// and one multiplication from every addition (seven multiplies total).
	var (
		plus       fieldElement
		minus      fieldElement
		cross      fieldElement
		doubledZ   fieldElement
		difference fieldElement
		sum        fieldElement
		lower      fieldElement
		upper      fieldElement
	)

	plus.add(&point.yCoordinate, &point.xCoordinate)
	minus.subtract(&point.yCoordinate, &point.xCoordinate)
	plus.multiply(&plus, &searchStep.yPlusX)
	minus.multiply(&minus, &searchStep.yMinusX)

	cross.multiply(&point.tCoordinate, &searchStep.xy2D)
	doubledZ.add(&point.zCoordinate, &point.zCoordinate)
	difference.subtract(&plus, &minus)
	sum.add(&plus, &minus)

	lower.subtract(&doubledZ, &cross)
	upper.add(&doubledZ, &cross)

	point.xCoordinate.multiply(&difference, &lower)
	point.yCoordinate.multiply(&sum, &upper)
	point.zCoordinate.multiply(&lower, &upper)
	point.tCoordinate.multiply(&difference, &sum)
}

func generatePoints(points []extendedPoint, products []fieldElement, publicKeys [][32]byte) {
	// Montgomery's trick: one inversion for the entire batch, with 3*(n-1)
	// extra multiplications. Accumulate products while advancing each point
	// to avoid a separate strided pass over the coordinates.
	points[0].advance()
	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		points[index].advance()
		products[index].multiply(&products[index-1], &points[index].zCoordinate)
	}

	var (
		reciprocal fieldElement
		inverseZ   fieldElement
		affineX    fieldElement
		affineY    fieldElement
	)

	reciprocal.invert(&products[len(points)-1])

	for index := len(points) - 1; index >= 0; index-- {
		point := &points[index]

		if index == 0 {
			inverseZ = reciprocal
		} else {
			inverseZ.multiply(&reciprocal, &products[index-1])
			reciprocal.multiply(&reciprocal, &point.zCoordinate)
		}

		affineY.multiply(&point.yCoordinate, &inverseZ)
		affineX.multiply(&point.xCoordinate, &inverseZ)

		affineY.putBytes(&publicKeys[index])

		publicKeys[index][31] |= affineX.isNegative() << 7
	}
}

func makeSearchStep() affineStep {
	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())

	xCoordinate, yCoordinate, zCoordinate, _ := step.ExtendedCoordinates()

	var inverseZ field.Element

	inverseZ.Invert(zCoordinate)

	xCoordinate.Multiply(xCoordinate, &inverseZ)
	yCoordinate.Multiply(yCoordinate, &inverseZ)

	// Edwards25519's curve constant d = -121665/121666.
	numeratorBytes := [32]byte{0x41, 0xdb, 0x01}
	denominatorBytes := [32]byte{0x42, 0xdb, 0x01}

	numerator, _ := new(field.Element).SetBytes(numeratorBytes[:])
	denominator, _ := new(field.Element).SetBytes(denominatorBytes[:])

	denominator.Invert(denominator)
	numerator.Multiply(numerator, denominator)
	numerator.Negate(numerator)
	numerator.Add(numerator, numerator)

	var result affineStep

	result.yPlusX.setField(new(field.Element).Add(yCoordinate, xCoordinate))
	result.yMinusX.setField(new(field.Element).Subtract(yCoordinate, xCoordinate))

	product := new(field.Element).Multiply(xCoordinate, yCoordinate)

	product.Multiply(product, numerator)

	result.xy2D.setField(product)

	return result
}
