//go:build gpu

package gpu

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestRawFieldCorrections(t *testing.T) {
	requireVulkan(t)

	limit := new(big.Int).Lsh(big.NewInt(1), 256)
	prime := new(big.Int).Sub(new(big.Int).Rsh(new(big.Int).Set(limit), 1), big.NewInt(19))
	values := make([]*big.Int, 0, 512)
	anchors := []*big.Int{big.NewInt(0), prime, new(big.Int).Lsh(new(big.Int).Set(prime), 1), limit}

	for _, anchor := range anchors {
		for delta := int64(-40); delta <= 40; delta++ {
			value := new(big.Int).Add(anchor, big.NewInt(delta))
			if value.Sign() >= 0 && value.Cmp(limit) < 0 {
				values = append(values, value)
			}
		}
	}

	commands := make([]command, 0, 16384)
	expected := make([][32]byte, 0, cap(commands))

	appendCase := func(operation uint32, first, second *big.Int, carry uint32) {
		update := command{Action: operation, Expected: carry}

		left := littleInteger(first)
		right := littleInteger(second)

		for limb := range 8 {
			update.Center[limb] = binary.LittleEndian.Uint32(left[limb*4:])
			update.Center[8+limb] = binary.LittleEndian.Uint32(right[limb*4:])
		}

		result := new(big.Int).Set(first)

		switch operation {
		case 8:
			result.Add(result, second)

			for result.Cmp(limit) >= 0 {
				result.Sub(result, limit).Add(result, big.NewInt(38))
			}
		case 9:
			result.Sub(result, second)

			for result.Sign() < 0 {
				result.Add(result, limit).Sub(result, big.NewInt(38))
			}
		case 10:
			result.Add(result, new(big.Int).SetUint64(uint64(carry)*38))

			for result.Cmp(limit) >= 0 {
				result.Sub(result, limit).Add(result, big.NewInt(38))
			}
		case 11:
			result.Mod(result, prime)
		case 13:
			adjustment := new(big.Int).SetUint64(uint64(carry) * 38)

			for range 64 {
				result.Add(result, second)
				result.Mul(result, result)
				result.Sub(result, first)
				result.Add(result, adjustment).Mod(result, prime)
			}
		}

		commands = append(commands, update)
		expected = append(expected, littleInteger(result))
	}

	for _, value := range values {
		appendCase(11, value, big.NewInt(0), 0)
		appendCase(8, value, new(big.Int).Sub(limit, big.NewInt(1)), 0)
		appendCase(9, big.NewInt(0), value, 0)
		appendCase(13, value, new(big.Int).Sub(limit, big.NewInt(1)), 38)
	}

	for carry := uint32(0); carry <= 38; carry++ {
		for bit := uint(32); bit <= 256; bit += 32 {
			for delta := int64(-2); delta <= 2; delta++ {
				boundary := new(big.Int).Lsh(big.NewInt(1), bit)
				boundary.Sub(boundary, big.NewInt(int64(carry)*38+delta))

				if boundary.Sign() >= 0 && boundary.Cmp(limit) < 0 {
					appendCase(10, boundary, big.NewInt(0), carry)
				}
			}
		}
	}

	for bit := uint(32); bit <= 256; bit += 32 {
		for delta := int64(-40); delta <= 40; delta++ {
			boundary := new(big.Int).Lsh(big.NewInt(1), bit)
			boundary.Sub(limit, boundary).Add(boundary, big.NewInt(delta))

			if boundary.Sign() >= 0 && boundary.Cmp(limit) < 0 {
				appendCase(9, big.NewInt(0), boundary, 0)
			}
		}
	}

	random := rand.New(rand.NewPCG(419, 631))

	for range 2048 {
		first := make([]byte, 32)
		second := make([]byte, 32)

		for word := range 4 {
			binary.LittleEndian.PutUint64(first[word*8:], random.Uint64())
			binary.LittleEndian.PutUint64(second[word*8:], random.Uint64())
		}

		left := new(big.Int).SetBytes(first)
		right := new(big.Int).SetBytes(second)

		appendCase(8, left, right, 0)
		appendCase(9, left, right, 0)
		appendCase(10, left, right, random.Uint32N(39))
	}

	checkRawShader(t, commands, expected)
}

func TestCanonicalPrefixWord(t *testing.T) {
	requireVulkan(t)

	limit := new(big.Int).Lsh(big.NewInt(1), 256)
	prime := new(big.Int).Sub(new(big.Int).Rsh(new(big.Int).Set(limit), 1), big.NewInt(19))
	anchors := []*big.Int{big.NewInt(0), prime, new(big.Int).Lsh(new(big.Int).Set(prime), 1), limit}

	commands := make([]command, 0, 4096)
	expected := make([][32]byte, 0, cap(commands))

	appendCase := func(value *big.Int) {
		update := command{Action: 12}
		encoded := littleInteger(value)

		for limb := range 8 {
			update.Center[limb] = binary.LittleEndian.Uint32(encoded[limb*4:])
		}

		canonical := littleInteger(new(big.Int).Mod(value, prime))
		clear(canonical[8:])

		commands = append(commands, update)
		expected = append(expected, canonical)
	}

	for _, anchor := range anchors {
		for delta := int64(-80); delta <= 80; delta++ {
			value := new(big.Int).Add(anchor, big.NewInt(delta))
			if value.Sign() >= 0 && value.Cmp(limit) < 0 {
				appendCase(value)
			}
		}
	}

	// Every high comparison bit must participate, including the high bit of
	// each middle limb; also exercise low-word carries after subtracting p.
	for bit := range uint(256) {
		value := new(big.Int).Sub(limit, big.NewInt(1))
		value.SetBit(value, int(bit), 0)

		appendCase(value)
		appendCase(new(big.Int).Lsh(big.NewInt(1), bit))
	}

	random := rand.New(rand.NewPCG(631, 947))

	for range 2048 {
		var encoded [32]byte

		for word := range 4 {
			binary.LittleEndian.PutUint64(encoded[word*8:], random.Uint64())
		}

		appendCase(new(big.Int).SetBytes(encoded[:]))
	}

	checkRawShader(t, commands, expected)
}

func checkRawShader(t *testing.T, commands []command, expected [][32]byte) {
	t.Helper()

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

		limbs := [...]uint32{first.Stream, first.Generation, first.Steps, first.Kind, first.Low, first.High, second.Stream, second.Generation}

		var actual [32]byte

		for limb, value := range limbs {
			binary.LittleEndian.PutUint32(actual[limb*4:], value)
		}

		if actual != expected[index] {
			t.Fatalf("case %d operation %d carry %d: GPU %x reference %x", index, commands[index].Action, commands[index].Expected, actual, expected[index])
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}
