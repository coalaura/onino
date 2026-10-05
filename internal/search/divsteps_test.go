package search

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestDivstepsInvariants(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	minimumCoefficient := new(big.Int).Lsh(prime, 1)
	minimumCoefficient.Neg(minimumCoefficient)

	random := rand.New(rand.NewPCG(590, 25519))

	for sample := range 1024 {
		input := new(big.Int).SetUint64(uint64(sample))

		if sample >= 4 {
			input = fieldInteger(fieldElement{random.Uint64(), random.Uint64(), random.Uint64(), random.Uint64()})
		}

		input.Mod(input, prime)

		remaining := new(big.Int).Set(input)

		first := divstepsPrime

		var (
			second divstepsInteger
			left   divstepsInteger
		)

		right := divstepsInteger{1, 0, 0, 0, 0}
		zeta := int64(-1)

		for index := range second {
			second[index] = int64(remaining.Uint64()) & divstepsMask

			remaining.Rsh(remaining, 62)
		}

		for group := range 10 {
			var matrix divstepsMatrix

			zeta = divsteps59(zeta, uint64(first[0]), uint64(second[0]), &matrix)

			divstepsUpdateCoefficients(&left, &right, &matrix)
			divstepsUpdateIntegers(&first, &second, &matrix)

			integers := [4]divstepsInteger{first, second, left, right}

			for index := range integers {
				for limb := range 4 {
					if integers[index][limb] < 0 || integers[index][limb] > divstepsMask {
						t.Fatalf("sample %d group %d: noncanonical low limb", sample, group)
					}
				}
			}

			firstInteger := divstepsBig(first)
			secondInteger := divstepsBig(second)

			leftInteger := divstepsBig(left)
			rightInteger := divstepsBig(right)

			if leftInteger.Cmp(minimumCoefficient) <= 0 || leftInteger.Cmp(prime) >= 0 || rightInteger.Cmp(minimumCoefficient) <= 0 || rightInteger.Cmp(prime) >= 0 {
				t.Fatalf("sample %d group %d: coefficient bounds", sample, group)
			}

			leftInteger.Mul(leftInteger, input)
			leftInteger.Sub(leftInteger, firstInteger)
			leftInteger.Mod(leftInteger, prime)

			rightInteger.Mul(rightInteger, input)
			rightInteger.Sub(rightInteger, secondInteger)
			rightInteger.Mod(rightInteger, prime)

			if leftInteger.Sign() != 0 || rightInteger.Sign() != 0 {
				t.Fatalf("sample %d group %d: coefficient congruences", sample, group)
			}
		}

		if divstepsBig(second).Sign() != 0 {
			t.Fatalf("sample %d: gcd did not converge after 590 steps", sample)
		}

		gcd := new(big.Int).GCD(nil, nil, prime, input)

		actual := divstepsBig(first)
		actual.Abs(actual)

		if actual.Cmp(gcd) != 0 {
			t.Fatalf("sample %d: incorrect gcd", sample)
		}
	}
}

func divstepsBig(value divstepsInteger) *big.Int {
	result := big.NewInt(value[4])

	for index := 3; index >= 0; index-- {
		result.Lsh(result, 62)
		result.Add(result, big.NewInt(value[index]))
	}

	return result
}
