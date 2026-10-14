//go:build gpu

package gpu

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestBatchedInverseShader(t *testing.T) {
	requireVulkan(t)

	commands := make([]command, 8192)
	expected := make([][32]byte, len(commands))

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	limit := new(big.Int).Lsh(big.NewInt(1), 256)

	random := rand.New(rand.NewPCG(20261014, 4))

	for index := range commands {
		commands[index].Action = 17

		for limb := range 8 {
			commands[index].Center[limb] = random.Uint32()
		}

		if index < 2048 {
			value := new(big.Int).Mul(prime, big.NewInt(int64(index%3)))
			value.Add(value, big.NewInt(int64(index/3-341)))
			value.Mod(value, limit)

			encoded := littleInteger(value)

			for limb := range 8 {
				commands[index].Center[limb] = binary.LittleEndian.Uint32(encoded[4*limb:])
			}
		}

		value := integerLimbs(commands[index].Center[:8])
		value.Mod(value, prime)

		if value.Sign() != 0 {
			value.ModInverse(value, prime)
		}

		expected[index] = littleInteger(value)
	}

	checkRawShader(t, commands, expected)
}
