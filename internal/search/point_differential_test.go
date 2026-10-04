package search

import (
	"encoding/binary"
	"math/rand/v2"
	"testing"
)

func TestAdvancementDifferential(t *testing.T) {
	values := []fieldElement{
		{},
		{1, 0, 0, 0},
		{0xffffffffffffffff, 0, 0, 0},
		{0, 0, 0, 0x8000000000000000},
		{0xffffffffffffffec, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffed, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffee, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
	}

	for _, left := range values {
		for _, right := range values {
			point := extendedPoint{xCoordinate: left, yCoordinate: right, zCoordinate: left, tCoordinate: right}

			checkAdvancement(t, point)
		}
	}

	random := rand.New(rand.NewPCG(81, 19))

	for range 5000 {
		var point extendedPoint

		coordinates := []*fieldElement{&point.xCoordinate, &point.yCoordinate, &point.zCoordinate, &point.tCoordinate}

		for _, coordinate := range coordinates {
			for index := range coordinate {
				coordinate[index] = random.Uint64()
			}
		}

		checkAdvancement(t, point)
	}
}

func FuzzAdvancement(f *testing.F) {
	f.Add(make([]byte, 128))

	maximal := make([]byte, 128)

	for index := range maximal {
		maximal[index] = 255
	}

	f.Add(maximal)

	f.Fuzz(func(t *testing.T, input []byte) {
		if len(input) != 128 {
			return
		}

		var point extendedPoint

		coordinates := []*fieldElement{&point.xCoordinate, &point.yCoordinate, &point.zCoordinate, &point.tCoordinate}

		for coordinateIndex, coordinate := range coordinates {
			for index := range coordinate {
				coordinate[index] = binary.LittleEndian.Uint64(input[coordinateIndex*32+index*8:])
			}
		}

		checkAdvancement(t, point)
	})
}

func checkAdvancement(t *testing.T, point extendedPoint) {
	t.Helper()

	// Unrestricted coordinates also exercise noncanonical residues and carries.
	// Curve-point correctness is checked independently by scalar multiplication.
	actual := [3]extendedPoint{point, point, point}
	expected := actual

	var (
		actualProducts   [3]fieldElement
		expectedProducts [3]fieldElement
	)

	advanceBatch(actual[:], actualProducts[:])
	advanceBatchGeneric(expected[:], expectedProducts[:])

	if actual != expected || actualProducts != expectedProducts {
		t.Fatalf("fused advancement differs from unfused formula for %+v", point)
	}
}
