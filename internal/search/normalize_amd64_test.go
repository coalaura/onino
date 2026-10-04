//go:build !purego

package search

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestNormalizationDifferential(t *testing.T) {
	if !fastFieldAvailable {
		t.Skip("BMI2 and ADX unavailable")
	}

	values := []fieldElement{
		{},
		{1, 0, 0, 0},
		{0xffffffffffffffec, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffed, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffee, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0, 0, 0, 0x8000000000000000},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
	}

	for _, numerator := range values {
		for _, denominator := range values {
			point := extendedPoint{yCoordinate: numerator, zCoordinate: denominator}
			points := []extendedPoint{point, point, point}
			checkNormalization(t, points)
		}
	}

	random := rand.New(rand.NewPCG(301, 89))

	sizes := []int{1, 2, 3, 17, 512}

	for _, size := range sizes {
		points := make([]extendedPoint, size)

		for index := range points {
			point := &points[index]

			coordinates := []*fieldElement{&point.xCoordinate, &point.yCoordinate, &point.zCoordinate, &point.tCoordinate}

			for _, coordinate := range coordinates {
				for limb := range coordinate {
					coordinate[limb] = random.Uint64()
				}
			}
		}

		checkNormalization(t, points)
	}
}

func FuzzNormalization(f *testing.F) {
	if !fastFieldAvailable {
		f.Skip("BMI2 and ADX unavailable")
	}

	f.Add(make([]byte, 64))
	f.Add([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) != 64 {
			return
		}

		var point extendedPoint

		for limb := range point.yCoordinate {
			point.yCoordinate[limb] = binary.LittleEndian.Uint64(input[limb*8:])
			point.zCoordinate[limb] = binary.LittleEndian.Uint64(input[32+limb*8:])
		}

		points := []extendedPoint{point, point, point}

		checkNormalization(t, points)
	})
}

func checkNormalization(t *testing.T, points []extendedPoint) {
	t.Helper()

	products := make([]fieldElement, len(points))
	publicKeys := make([][32]byte, len(points))

	original := append([]extendedPoint(nil), points...)

	products[0] = points[0].zCoordinate

	for index := 1; index < len(points); index++ {
		products[index].multiply(&products[index-1], &points[index].zCoordinate)
	}

	var reciprocal fieldElement

	last := len(points) - 1
	reciprocal.invert(&products[last])

	normalizeBMI2(&points[last], &products[last], &publicKeys[last], len(points), &reciprocal)

	prime := new(big.Int).Lsh(big.NewInt(1), 255)

	prime.Sub(prime, big.NewInt(19))

	for index := range points {
		if points[index] != original[index] {
			t.Fatal("normalization modified projective coordinates")
		}

		inverse := new(big.Int).ModInverse(fieldInteger(points[index].zCoordinate), prime)

		if inverse == nil {
			inverse = new(big.Int)
		}

		checkFieldResult(t, "retained inverse Z", &products[index], inverse)

		expected := new(big.Int).Mul(fieldInteger(points[index].yCoordinate), inverse)

		expected.Mod(expected, prime)

		encoded := publicKeys[index]

		for offset := range 16 {
			encoded[offset], encoded[31-offset] = encoded[31-offset], encoded[offset]
		}

		if new(big.Int).SetBytes(encoded[:]).Cmp(expected) != 0 {
			t.Fatal("noncanonical or incorrect affine Y")
		}
	}
}
