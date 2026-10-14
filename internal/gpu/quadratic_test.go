//go:build gpu

package gpu

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestQuadraticTable(t *testing.T) {
	table := makeTable(Plan{count: 3})
	position := (pairCount+1)*24 + 8*4

	if table[position] != 3 {
		t.Fatal("probe count moved")
	}

	position++

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

	for index := range pairCount / 2 {
		first := quadraticInteger(table[2*index*24+16:][:8])
		second := quadraticInteger(table[(2*index+1)*24+16:][:8])

		first.Mul(first, first)
		second.Mul(second, second)

		sum := new(big.Int).Add(first, second)
		product := new(big.Int).Mul(first, second)

		sum.Mod(sum, prime)
		product.Mod(product, prime)

		if sum.Cmp(quadraticInteger(table[position+index*8:][:8])) != 0 || product.Cmp(quadraticInteger(table[position+(pairCount/2+index)*8:][:8])) != 0 {
			t.Fatalf("coefficients %d", index)
		}
	}

	jump := quadraticInteger(table[pairCount*24+16:][:8])
	jump.Mul(jump, jump).Mod(jump, prime)

	if jump.Cmp(quadraticInteger(table[position+pairCount*8:])) != 0 {
		t.Fatal("jump square")
	}
}

func TestQuadraticShader(t *testing.T) {
	requireVulkan(t)

	table := makeTable(Plan{})
	position := (pairCount+1)*24 + 8*4 + 1

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	limit := new(big.Int).Lsh(big.NewInt(1), 256)

	random := rand.New(rand.NewPCG(20261014, 1))

	commands := make([]command, 2048)
	expected := make([][32]byte, len(commands))

	for index := range commands {
		pair := index % (pairCount / 2)

		update := &commands[index]
		update.Action = 14

		for limb := range 8 {
			update.Center[limb] = random.Uint32()
		}

		if index < 512 {
			value := new(big.Int).Mul(prime, big.NewInt(int64(index/128)))
			value.Add(value, big.NewInt(int64(index%128-64))).Mod(value, limit)

			encoded := littleInteger(value)
			putLimbs(update.Center[:8], encoded[:])
		}

		copy(update.Center[8:16], table[position+pair*8:][:8])
		copy(update.Center[16:], table[position+(pairCount/2+pair)*8:][:8])

		center := quadraticInteger(update.Center[:8])
		first := quadraticInteger(table[2*pair*24+16:][:8])
		second := quadraticInteger(table[(2*pair+1)*24+16:][:8])

		first.Mul(first, center).Mul(first, first).Sub(big.NewInt(1), first)
		second.Mul(second, center).Mul(second, second).Sub(big.NewInt(1), second)
		first.Mul(first, second).Mod(first, prime)

		expected[index] = littleInteger(first)
	}

	checkRawShader(t, commands, expected)
}

func quadraticInteger(limbs []uint32) *big.Int {
	var encoded [32]byte

	for index, limb := range limbs {
		binary.BigEndian.PutUint32(encoded[28-4*index:], limb)
	}

	return new(big.Int).SetBytes(encoded[:])
}
