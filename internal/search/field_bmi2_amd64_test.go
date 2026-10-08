//go:build !purego

package search

import (
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/coalaura/onino/internal/simd"
)

func TestBMI2OnlyArithmetic(t *testing.T) {
	if !simd.Detect().BMI2 {
		t.Skip("BMI2 not reported on this CPU")
	}

	values := []fieldElement{
		{},
		{1, 0, 0, 0},
		{0xffffffffffffffff, 0, 0, 0},
		{0, 0, 0, 0x8000000000000000},
		{0xffffffffffffffec, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffed, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffee, 0xffffffffffffffff, 0xffffffffffffffff, 0x7fffffffffffffff},
		{0xffffffffffffffd9, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{0xffffffffffffffda, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
		{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff},
	}

	for _, left := range values {
		for _, right := range values {
			checkBMI2Only(t, left, right)
		}
	}

	random := rand.New(rand.NewPCG(192, 817))

	for range 50000 {
		left := fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()}
		right := fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()}

		checkBMI2Only(t, left, right)
	}
}

func checkBMI2Only(t *testing.T, left, right fieldElement) {
	t.Helper()

	prime := new(big.Int).Lsh(big.NewInt(1), 255)

	prime.Sub(prime, big.NewInt(19))

	expected := new(big.Int).Mul(fieldInteger(left), fieldInteger(right))

	expected.Mod(expected, prime)

	var result fieldElement

	multiplyBMI2Only(&result, &left, &right)
	checkFieldResult(t, "bmi2 multiply", &result, expected)

	result = left
	multiplyBMI2Only(&result, &result, &right)
	checkFieldResult(t, "bmi2 left alias", &result, expected)

	result = right
	multiplyBMI2Only(&result, &left, &result)
	checkFieldResult(t, "bmi2 right alias", &result, expected)

	expected.Mul(fieldInteger(left), fieldInteger(left))
	expected.Mod(expected, prime)
	squareBMI2Only(&result, &left)
	checkFieldResult(t, "bmi2 square", &result, expected)

	result = left
	squareBMI2Only(&result, &result)
	checkFieldResult(t, "bmi2 square alias", &result, expected)

	result = left
	multiplyBMI2Only(&result, &result, &result)
	checkFieldResult(t, "bmi2 both aliases", &result, expected)
}
