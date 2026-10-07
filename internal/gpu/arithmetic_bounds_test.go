//go:build gpu

package gpu

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestArithmeticBounds(t *testing.T) {
	requireVulkan(t)

	commands := make([]command, 1024)
	expected := make([][32]byte, len(commands))

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

	random := rand.New(rand.NewPCG(17, 29))

	for index := range commands {
		operation := uint32(index % 8)
		commands[index].Action = operation

		for limb := range 10 {
			radix := uint32(1) << (26 - uint(limb&1))
			limit := radix

			if operation >= 2 && operation <= 6 {
				limit *= 3
			}

			first := random.Uint32N(limit)
			second := random.Uint32N(limit)

			switch index / 8 {
			case 0:
				first = limit - 1
				second = limit - 1
			case 1:
				first = 0
				second = limit - 1
			case 2:
				first = limit - 1
				second = 0
			case 3:
				first = radix - 1
				second = 1
			case 4:
				first = radix - 1
				second = radix - 1
			default:
				// One limb at each carry boundary, with all other limbs maximal.
				boundary := index/8 - 5
				if boundary < 30 {
					first = limit - 1
					second = limit - 1

					if limb == boundary/3 {
						first = radix - 1 + uint32(boundary%3)
						if first >= limit {
							first = 0
						}
					}
				}
			}

			commands[index].Center[limb] = first
			commands[index].Center[10+limb] = second
		}

		left := integerLimbs(commands[index].Center[:10])
		right := integerLimbs(commands[index].Center[10:20])

		result := new(big.Int)

		switch operation {
		case 0:
			result.Add(left, right)
		case 1:
			result.Sub(left, right)
		case 2:
			result.Mul(left, right)
		case 3:
			result.Mul(left, left)
		case 4:
			result.Exp(left, new(big.Int).Sub(prime, big.NewInt(2)), prime)
		case 5:
			result.Exp(left, new(big.Int).Lsh(big.NewInt(1), 254), prime)
		case 6:
			result.Exp(left, big.NewInt(64), prime)
		case 7:
			result.Mul(new(big.Int).Sub(left, right), new(big.Int).Add(left, right))
		}

		result.Mod(result, prime)

		expected[index] = littleInteger(result)
	}

	engine, err := openDevice(testDevice(t), true, len(commands), len(commands)*2, makeTable(Plan{}), arithmeticShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	err = engine.submit(0, commands, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	completed, err := engine.collect(0)
	if err != nil {
		t.Fatal(err)
	}

	if len(completed.hits) != len(commands)*2 {
		t.Fatalf("arithmetic records: got %d, want %d", len(completed.hits), len(commands)*2)
	}

	for index := range commands {
		first := completed.hits[2*index]

		second := completed.hits[2*index+1]
		if second.Low != 0 {
			t.Fatalf("operation %d case %d: output violated pre-canonical limb bounds", commands[index].Action, index)
		}

		limbs := []uint32{first.Stream, first.Generation, first.Steps, first.Kind, first.Low, first.High, second.Stream, second.Generation, second.Steps, second.Kind}

		for limb, value := range limbs {
			if value >= uint32(1)<<(26-uint(limb&1)) {
				t.Fatalf("operation %d case %d: unbounded limb %d: %d", commands[index].Action, index, limb, value)
			}
		}

		actual := encodeLimbs(limbs)
		if actual != expected[index] {
			t.Fatalf("operation %d case %d: GPU %x reference %x", commands[index].Action, index, actual, expected[index])
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func integerLimbs(limbs []uint32) *big.Int {
	result := new(big.Int)
	shift := uint(0)

	for index, limb := range limbs {
		term := new(big.Int).Lsh(new(big.Int).SetUint64(uint64(limb)), shift)

		result.Add(result, term)

		shift += 26 - uint(index&1)
	}

	return result
}
