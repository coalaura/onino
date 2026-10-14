//go:build gpu

package gpu

import (
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestFusionShader(t *testing.T) {
	requireVulkan(t)

	commands := make([]command, 4096)
	expected := make([][32]byte, len(commands))

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	limit := new(big.Int).Lsh(big.NewInt(1), 256)

	random := rand.New(rand.NewPCG(20261014, 9))

	for index := range commands {
		commands[index].Action = 16

		for limb := range 8 {
			commands[index].Center[limb] = random.Uint32()
		}

		if index < 256 {
			value := new(big.Int).Mul(prime, big.NewInt(int64(index%3)))
			value.Add(value, big.NewInt(int64(index/3)))
			value.Mod(value, limit)

			encoded := littleInteger(value)

			for limb := range 8 {
				commands[index].Center[limb] = uint32(encoded[4*limb]) | uint32(encoded[4*limb+1])<<8 | uint32(encoded[4*limb+2])<<16 | uint32(encoded[4*limb+3])<<24
			}
		}

		value := integerLimbs(commands[index].Center[:8])
		value.Mul(value, value)

		value.Sub(big.NewInt(1), value)
		value.Mod(value, prime)

		expected[index] = littleInteger(value)
	}

	checkRawShader(t, commands, expected)
}
