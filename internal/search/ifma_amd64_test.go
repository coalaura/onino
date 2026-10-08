//go:build !purego

package search

import (
	"math/big"
	"math/rand/v2"
	"testing"

	"github.com/coalaura/onino/internal/simd"
)

func TestIFMAArithmetic(t *testing.T) {
	features := simd.Detect()
	if !features.IFMA || ifmaLanes == 4 && !features.VL {
		t.Skip("IFMA unavailable")
	}

	modulus := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

	random := rand.New(rand.NewPCG(42, 91))

	for iteration := range 2048 {
		var (
			left  ifmaElement
			right ifmaElement
		)

		for limb := range left {
			for lane := range left[limb] {
				left[limb][lane] = random.Uint64() & radixMask
				right[limb][lane] = random.Uint64() & radixMask

				if iteration < 8 {
					left[limb][lane] = radixMask - uint64(iteration)
					right[limb][lane] = radixMask - uint64(lane)
				}

				if iteration == 8 {
					left[limb][lane] = 0
					right[limb][lane] = 0
				}
			}
		}

		for operation := range 4 {
			var result ifmaElement

			switch operation {
			case 0:
				ifmaMultiply(&result, &left, &right)
			case 1:
				ifmaSquare(&result, &left)
			case 2:
				ifmaAdd(&result, &left, &right)
			case 3:
				ifmaSubtract(&result, &left, &right)
			}

			for lane := range ifmaLanes {
				first := ifmaBig(&left, lane)
				second := ifmaBig(&right, lane)
				want := new(big.Int)

				switch operation {
				case 0:
					want.Mul(first, second)
				case 1:
					want.Mul(first, first)
				case 2:
					want.Add(first, second)
				case 3:
					want.Sub(first, second)
				}

				want.Mod(want, modulus)

				got := ifmaBig(&result, lane)
				got.Mod(got, modulus)

				if got.Cmp(want) != 0 {
					t.Fatalf("iteration %d op %d lane %d: got %x want %x", iteration, operation, lane, got, want)
				}

				for limb := range result {
					if result[limb][lane] > radixMask {
						t.Fatalf("unnormalized limb %d: %x", limb, result[limb][lane])
					}
				}
			}

			alias := left

			switch operation {
			case 0:
				ifmaMultiply(&alias, &alias, &right)
			case 1:
				ifmaSquare(&alias, &alias)
			case 2:
				ifmaAdd(&alias, &alias, &right)
			case 3:
				ifmaSubtract(&alias, &alias, &right)
			}

			if alias != result {
				t.Fatal("left alias differs")
			}

			if operation != 1 {
				alias = right

				switch operation {
				case 0:
					ifmaMultiply(&alias, &left, &alias)
				case 2:
					ifmaAdd(&alias, &left, &alias)
				case 3:
					ifmaSubtract(&alias, &left, &alias)
				}

				if alias != result {
					t.Fatal("right alias differs")
				}
			}
		}
	}
}

func BenchmarkIFMAPrimitive(b *testing.B) {
	features := simd.Detect()
	if !features.IFMA || ifmaLanes == 4 && !features.VL {
		b.Skip("IFMA unavailable")
	}

	var (
		vector ifmaElement
		scalar [8]fieldElement
	)

	for lane := range scalar {
		scalar[lane] = pairedD
		vector.setLane(lane%ifmaLanes, &pairedD)
	}

	b.Run("scalar8", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for lane := range scalar {
				scalar[lane].multiply(&scalar[lane], &scalar[lane])
			}
		}
	})

	b.Run("ifma8", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for range 8 / ifmaLanes {
				ifmaMultiply(&vector, &vector, &vector)
			}
		}
	})

	b.Run("square8", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for range 8 / ifmaLanes {
				ifmaSquare(&vector, &vector)
			}
		}
	})

	b.Run("scalar_square8", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for lane := range scalar {
				scalar[lane].square(&scalar[lane])
			}
		}
	})

	b.Run("conversion8", func(b *testing.B) {
		b.ReportAllocs()

		for b.Loop() {
			for lane := range scalar {
				vector.setLane(lane%ifmaLanes, &scalar[lane])
				scalar[lane] = vector.lane(lane % ifmaLanes)
			}
		}
	})
}

func ifmaBig(source *ifmaElement, lane int) *big.Int {
	result := new(big.Int)

	for limb := 4; limb >= 0; limb-- {
		result.Lsh(result, 51)
		result.Add(result, new(big.Int).SetUint64(source[limb][lane]))
	}

	return result
}
