package search

import (
	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/simd"
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
	point.advanceWith(defaultFieldMode)
}

//go:inline
func (point *extendedPoint) advanceWith(mode simd.Mode) {
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
	plus.multiplyWith(&plus, &searchStep.yPlusX, mode)
	minus.multiplyWith(&minus, &searchStep.yMinusX, mode)

	cross.multiplyWith(&point.tCoordinate, &searchStep.xy2D, mode)
	doubledZ.add(&point.zCoordinate, &point.zCoordinate)
	difference.subtract(&plus, &minus)
	sum.add(&plus, &minus)

	lower.subtract(&doubledZ, &cross)
	upper.add(&doubledZ, &cross)

	point.xCoordinate.multiplyWith(&difference, &lower, mode)
	point.yCoordinate.multiplyWith(&sum, &upper, mode)
	point.zCoordinate.multiplyWith(&lower, &upper, mode)
	point.tCoordinate.multiplyWith(&difference, &sum, mode)
}

func generatePoints(points []extendedPoint, products []fieldElement, publicKeys [][32]byte) {
	generateBatch(points, products, publicKeys, false)
}

func generateBatch(points []extendedPoint, products []fieldElement, publicKeys [][32]byte, deferSign bool) {
	generateBatchWith(points, products, publicKeys, deferSign, defaultFieldMode)
}

func generateBatchWith(points []extendedPoint, products []fieldElement, publicKeys [][32]byte, deferSign bool, mode simd.Mode) {
	// Montgomery's trick: one inversion for the entire batch, with 3*(n-1)
	// extra multiplications. Accumulate products while advancing each point
	// to avoid a separate strided pass over the coordinates.
	advanceBatchWith(points, products, mode)

	var (
		reciprocal fieldElement
		inverseZ   fieldElement
		affineX    fieldElement
		affineY    fieldElement
	)

	reciprocal.invert(&products[len(points)-1])

	if deferSign && mode == simd.BMI2 {
		last := len(points) - 1

		normalizeBMI2Only(&points[last], &products[last], &publicKeys[last], len(points), &reciprocal)

		return
	}

	if deferSign && mode == simd.BMI2ADX {
		last := len(points) - 1

		normalizeBMI2(&points[last], &products[last], &publicKeys[last], len(points), &reciprocal)

		return
	}

	for index := len(points) - 1; index >= 0; index-- {
		point := &points[index]

		if index == 0 {
			inverseZ = reciprocal
		} else {
			inverseZ.multiplyWith(&reciprocal, &products[index-1], mode)
			reciprocal.multiplyWith(&reciprocal, &point.zCoordinate, mode)
		}

		affineY.multiplyWith(&point.yCoordinate, &inverseZ, mode)
		affineY.putBytes(&publicKeys[index])

		if deferSign {
			// Reverse traversal has consumed this prefix product. Retain inverse
			// Z here until matching, without touching the next walk's projective X.
			products[index] = inverseZ

			continue
		}

		affineX.multiplyWith(&point.xCoordinate, &inverseZ, mode)

		publicKeys[index][31] |= affineX.isNegative() << 7
	}
}

func completeSign(point *extendedPoint, inverseZ *fieldElement, publicKey *[32]byte) {
	completeSignWith(point, inverseZ, publicKey, defaultFieldMode)
}

func completeSignWith(point *extendedPoint, inverseZ *fieldElement, publicKey *[32]byte, mode simd.Mode) {
	var affineX fieldElement

	affineX.multiplyWith(&point.xCoordinate, inverseZ, mode)

	publicKey[31] = publicKey[31]&0x7f | affineX.isNegative()<<7
}

func advanceBatchGeneric(points []extendedPoint, products []fieldElement) {
	advanceBatchGenericWith(points, products, defaultFieldMode)
}

func advanceBatchGenericWith(points []extendedPoint, products []fieldElement, mode simd.Mode) {
	points[0].advanceWith(mode)
	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		points[index].advanceWith(mode)
		products[index].multiplyWith(&products[index-1], &points[index].zCoordinate, mode)
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
