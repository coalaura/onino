package search

import (
	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"
)

type extendedPoint struct {
	xCoordinate field.Element
	yCoordinate field.Element
	zCoordinate field.Element
	tCoordinate field.Element
}

type affineStep struct {
	yPlusX  field.Element
	yMinusX field.Element
	xy2D    field.Element
}

var searchStep = makeSearchStep()

func (point *extendedPoint) set(source *edwards25519.Point) {
	xCoordinate, yCoordinate, zCoordinate, tCoordinate := source.ExtendedCoordinates()
	point.xCoordinate = *xCoordinate
	point.yCoordinate = *yCoordinate
	point.zCoordinate = *zCoordinate
	point.tCoordinate = *tCoordinate
}

//go:inline
func (point *extendedPoint) advance() {
	// Complete mixed Edwards addition, with the fixed affine point 8*B.
	// Precomputing its three products removes the general-point conversion
	// and one multiplication from every addition (seven multiplies total).
	var (
		plus       field.Element
		minus      field.Element
		cross      field.Element
		doubledZ   field.Element
		difference field.Element
		sum        field.Element
		lower      field.Element
		upper      field.Element
	)

	plus.Add(&point.yCoordinate, &point.xCoordinate)
	minus.Subtract(&point.yCoordinate, &point.xCoordinate)
	plus.Multiply(&plus, &searchStep.yPlusX)
	minus.Multiply(&minus, &searchStep.yMinusX)

	cross.Multiply(&point.tCoordinate, &searchStep.xy2D)
	doubledZ.Add(&point.zCoordinate, &point.zCoordinate)
	difference.Subtract(&plus, &minus)
	sum.Add(&plus, &minus)

	lower.Subtract(&doubledZ, &cross)
	upper.Add(&doubledZ, &cross)

	point.xCoordinate.Multiply(&difference, &lower)
	point.yCoordinate.Multiply(&sum, &upper)
	point.zCoordinate.Multiply(&lower, &upper)
	point.tCoordinate.Multiply(&difference, &sum)
}

func generatePoints(points []extendedPoint, products []field.Element, publicKeys [][32]byte) {
	// Montgomery's trick: one inversion for the entire batch, with 3*(n-1)
	// extra multiplications. Accumulate products while advancing each point
	// to avoid a separate strided pass over the coordinates.
	points[0].advance()
	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		points[index].advance()
		products[index].Multiply(&products[index-1], &points[index].zCoordinate)
	}

	var (
		reciprocal field.Element
		inverseZ   field.Element
		affineX    field.Element
		affineY    field.Element
	)

	reciprocal.Invert(&products[len(points)-1])

	for index := len(points) - 1; index >= 0; index-- {
		point := &points[index]

		if index == 0 {
			inverseZ = reciprocal
		} else {
			inverseZ.Multiply(&reciprocal, &products[index-1])
			reciprocal.Multiply(&reciprocal, &point.zCoordinate)
		}

		affineY.Multiply(&point.yCoordinate, &inverseZ)
		affineX.Multiply(&point.xCoordinate, &inverseZ)

		copy(publicKeys[index][:], affineY.Bytes())

		publicKeys[index][31] |= byte(affineX.IsNegative() << 7)
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

	result.yPlusX.Add(yCoordinate, xCoordinate)
	result.yMinusX.Subtract(yCoordinate, xCoordinate)
	result.xy2D.Multiply(xCoordinate, yCoordinate)
	result.xy2D.Multiply(&result.xy2D, numerator)

	return result
}
