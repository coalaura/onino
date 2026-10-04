package search

import (
	"math/big"
	"testing"

	"filippo.io/edwards25519"
)

type paperRootCase struct {
	name      string
	value     *big.Int
	character int
}

func TestEdwardsFieldAssumptions(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	abscissaCoefficient := big.NewInt(-1)

	curveCoefficient := new(big.Int).ModInverse(big.NewInt(121666), prime)

	curveCoefficient.Mul(curveCoefficient, big.NewInt(-121665))
	curveCoefficient.Mod(curveCoefficient, prime)

	actual := fieldInteger(pairedD)
	actual.Mod(actual, prime)

	if actual.Cmp(curveCoefficient) != 0 {
		t.Fatal("incorrect Edwards coefficient")
	}

	coefficientDifference := new(big.Int).Sub(abscissaCoefficient, curveCoefficient)

	parameter := paperQuotient(curveCoefficient, coefficientDifference, prime)
	if parameter.Cmp(big.NewInt(121665)) != 0 {
		t.Fatal("direct-Y specialization is not t=121665")
	}

	cases := []paperRootCase{
		{name: "a", value: abscissaCoefficient, character: 1},
		{name: "d", value: curveCoefficient, character: -1},
		{name: "ad", value: new(big.Int).Mul(abscissaCoefficient, curveCoefficient), character: -1},
		{name: "d/a", value: paperQuotient(curveCoefficient, abscissaCoefficient, prime), character: -1},
		{name: "a/d", value: paperQuotient(abscissaCoefficient, curveCoefficient, prime), character: -1},
		{name: "(a-d)/a", value: paperQuotient(coefficientDifference, abscissaCoefficient, prime), character: 1},
		{name: "(d-a)/d", value: paperQuotient(new(big.Int).Neg(coefficientDifference), curveCoefficient, prime), character: -1},
		{name: "A^2-4", value: big.NewInt(486662*486662 - 4), character: -1},
	}

	for _, test := range cases {
		if big.Jacobi(test.value, prime) != test.character {
			t.Fatalf("unexpected quadratic character for %s", test.name)
		}

		root := new(big.Int).ModSqrt(test.value, prime)
		if (root != nil) != (test.character == 1) {
			t.Fatalf("unexpected root existence for %s", test.name)
		}
	}
}

func TestPaperDirectY(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())

	point := edwards25519.NewIdentityPoint()

	ordinate := fieldInteger(pairedTable[0].y)

	square := new(big.Int).Mul(ordinate, ordinate)
	factor := new(big.Int).Sub(square, big.NewInt(1))
	alpha := new(big.Int).Mul(big.NewInt(121665), factor)
	beta := new(big.Int).Sub(big.NewInt(1), alpha)
	gamma := new(big.Int).Mul(big.NewInt(121666), factor)

	for range 256 {
		previous := new(edwards25519.Point).Subtract(point, step)
		next := new(edwards25519.Point).Add(point, step)

		var (
			currentAffine  pairedAffine
			previousAffine pairedAffine
			nextAffine     pairedAffine
		)

		currentAffine.set(point)
		previousAffine.set(previous)
		nextAffine.set(next)

		currentY := fieldInteger(currentAffine.y)
		previousY := fieldInteger(previousAffine.y)
		nextY := fieldInteger(nextAffine.y)

		currentSquare := new(big.Int).Mul(currentY, currentY)
		term := new(big.Int).Mul(alpha, currentSquare)

		numerator := new(big.Int).Sub(currentSquare, term)
		numerator.Add(numerator, gamma)
		numerator.Mod(numerator, prime)

		denominator := new(big.Int).Add(term, beta)
		denominator.Mul(denominator, previousY)
		denominator.Mod(denominator, prime)

		if denominator.Sign() == 0 {
			t.Fatal("unexpected direct-Y pole in the prime-order walk")
		}

		actual := paperQuotient(numerator, denominator, prime)
		nextY.Mod(nextY, prime)

		if actual.Cmp(nextY) != 0 {
			t.Fatal("Proposition 9 does not reproduce the fixed-step walk")
		}

		point.Set(next)
	}
}

func TestPaperFourfoldY(t *testing.T) {
	prime := new(big.Int).Lsh(big.NewInt(1), 255)
	prime.Sub(prime, big.NewInt(19))

	coefficient := fieldInteger(pairedD)
	parameter := paperQuotient(big.NewInt(-4), coefficient, prime)

	numeratorFactor := new(big.Int).Sub(parameter, big.NewInt(2))
	numeratorFactor.Mul(numeratorFactor, big.NewInt(2))

	denominatorFactor := new(big.Int).Mul(parameter, big.NewInt(-4))
	denominatorFactor.Add(denominatorFactor, big.NewInt(16))

	point := edwards25519.NewIdentityPoint()
	step := edwards25519.NewGeneratorPoint()

	for range 256 {
		var affine pairedAffine

		affine.set(point)

		abscissa := fieldInteger(affine.x)
		ordinate := fieldInteger(affine.y)

		invariant := new(big.Int).Mul(abscissa, ordinate)
		invariant.Mul(invariant, invariant)
		invariant.Mul(invariant, coefficient)
		invariant.Mod(invariant, prime)

		square := new(big.Int).Mul(invariant, invariant)
		symmetric := new(big.Int).Mul(square, square)
		sixSquare := new(big.Int).Mul(square, big.NewInt(6))

		symmetric.Add(symmetric, sixSquare)
		symmetric.Add(symmetric, big.NewInt(1))

		cross := new(big.Int).Add(square, big.NewInt(1))
		cross.Mul(cross, invariant)

		numerator := new(big.Int).Mul(numeratorFactor, cross)
		numerator.Sub(numerator, symmetric)

		denominator := new(big.Int).Mul(denominatorFactor, square)
		fourCross := new(big.Int).Mul(cross, big.NewInt(4))

		denominator.Add(denominator, fourCross)
		denominator.Sub(denominator, symmetric)
		denominator.Mod(denominator, prime)

		if denominator.Sign() == 0 {
			t.Fatal("unexpected fourfold recovery pole")
		}

		actual := paperQuotient(numerator, denominator, prime)

		fourfold := new(edwards25519.Point).Add(point, point)
		fourfold.Add(fourfold, fourfold)

		affine.set(fourfold)

		expected := fieldInteger(affine.y)
		expected.Mod(expected, prime)

		if actual.Cmp(expected) != 0 {
			t.Fatal("equation 18 does not recover the ordinate of 4P")
		}

		point.Add(point, step)
	}
}

func paperQuotient(numerator, denominator, prime *big.Int) *big.Int {
	result := new(big.Int).ModInverse(denominator, prime)

	result.Mul(result, numerator)
	result.Mod(result, prime)

	return result
}
