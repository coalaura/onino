//go:build gpu

package gpu

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"

	"filippo.io/edwards25519"
)

func TestRadix32Representations(t *testing.T) {
	requireVulkan(t)

	commands := make([]command, 2048)
	expected := make([][32]byte, len(commands))

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	limit := new(big.Int).Lsh(big.NewInt(1), 256)

	random := rand.New(rand.NewPCG(101, 303))

	for index := range commands {
		operation := uint32(index % 8)
		commands[index].Action = operation

		var (
			first  [32]byte
			second [32]byte
		)

		for word := range 4 {
			binary.LittleEndian.PutUint64(first[word*8:], random.Uint64())
			binary.LittleEndian.PutUint64(second[word*8:], random.Uint64())
		}

		if index/8 < 128 {
			boundary := index / 8

			left := new(big.Int).Sub(limit, big.NewInt(int64(boundary%64+1)))
			right := new(big.Int).Sub(limit, big.NewInt(1))

			if boundary >= 64 {
				left.Add(prime, big.NewInt(int64(boundary%64-32)))
				right.SetInt64(int64(boundary % 64))
			}

			first = littleInteger(left)
			second = littleInteger(right)
		}

		for limb := range 8 {
			commands[index].Center[limb] = binary.LittleEndian.Uint32(first[limb*4:])
			commands[index].Center[8+limb] = binary.LittleEndian.Uint32(second[limb*4:])
		}

		left := new(big.Int)
		right := new(big.Int)

		for limb := 7; limb >= 0; limb-- {
			left.Lsh(left, 32).Add(left, new(big.Int).SetUint64(uint64(commands[index].Center[limb])))
			right.Lsh(right, 32).Add(right, new(big.Int).SetUint64(uint64(commands[index].Center[8+limb])))
		}

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
		t.Fatalf("records: %d", len(completed.hits))
	}

	for index := range commands {
		first := completed.hits[2*index]
		second := completed.hits[2*index+1]

		limbs := []uint32{first.Stream, first.Generation, first.Steps, first.Kind, first.Low, first.High, second.Stream, second.Generation, second.Steps, second.Kind}

		actual := encodeLimbs(limbs)
		if actual != expected[index] || second.Low != 0 {
			t.Fatalf("case %d operation %d GPU %x CPU %x bounds %d", index, commands[index].Action, actual, expected[index], second.Low)
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestExceptionalPointPairs(t *testing.T) {
	requireVulkan(t)

	plan := Plan{count: 1}

	engine, err := openDevice(testDevice(t), true, 1, 2, makeTable(plan), diagnosticShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	step := new(edwards25519.Point).Set(edwards25519.NewGeneratorPoint())

	for range 3 {
		step.Add(step, step)
	}

	for sign := range 2 {
		center := new(edwards25519.Point).Set(step)

		if sign != 0 {
			center.Negate(center)
		}

		generation := uint32(sign + 1)

		commands := []command{{Center: affine(center, false), Expected: generation - 1, Generation: generation, Action: 2}}

		offset := edwards25519.NewIdentityPoint()

		for candidate := range 2 * pairCount {
			if candidate%2 == 0 {
				offset.Add(offset, step)
			}

			err = engine.submit(0, commands, 2, false)
			if err != nil {
				t.Fatal(err)
			}

			completed, collectError := engine.collect(0)
			if collectError != nil {
				t.Fatal(collectError)
			}

			if len(completed.hits) != 2 || completed.checked != 1 {
				t.Fatalf("sign %d candidate %d: hits=%d checked=%d", sign, candidate, len(completed.hits), completed.checked)
			}

			first := completed.hits[0]
			second := completed.hits[1]

			limbs := []uint32{first.Steps, first.Kind, first.Low, first.High, second.Stream, second.Generation, second.Steps, second.Kind, second.Low, second.High & 0x1ffffff}

			actual := encodeLimbs(limbs)
			actual[31] |= byte(second.High >> 25 << 7)

			expected := new(edwards25519.Point)

			if candidate%2 == 0 {
				expected.Add(center, offset)
			} else {
				expected.Subtract(center, offset)
			}

			if actual != [32]byte(expected.Bytes()) {
				t.Fatalf("sign %d candidate %d: GPU %x CPU %x", sign, candidate, actual, expected.Bytes())
			}

			commands[0] = command{Expected: generation, Action: 1}
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}
