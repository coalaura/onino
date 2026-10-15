//go:build gpu

package gpu

import (
	"encoding/binary"
	"math/big"
	"math/rand/v2"
	"testing"
)

func TestPartialProductShader(t *testing.T) {
	requireVulkan(t)

	prime := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))
	limit := new(big.Int).Lsh(big.NewInt(1), 256)

	random := rand.New(rand.NewPCG(20261014, 2))

	commands := make([]command, 8448)
	expected := make([][32]byte, len(commands))

	var (
		corrections int
		lowWraps    int
	)

	for index := range commands {
		update := &commands[index]
		update.Action = 15

		for limb := range 16 {
			update.Center[limb] = random.Uint32()
		}

		if index < 1024 {
			first := new(big.Int).Mul(prime, big.NewInt(int64(index/256)))
			first.Add(first, big.NewInt(int64(index%256-128))).Mod(first, limit)

			encoded := littleInteger(first)
			putLimbs(update.Center[:8], encoded[:])
		}

		first := quadraticInteger(update.Center[:8])

		if index%2 == 0 {
			inverse := new(big.Int).ModInverse(first, prime)
			if inverse != nil {
				second := new(big.Int).Mul(inverse, big.NewInt(int64(index%127-63)))
				second.Mod(second, prime)
				second.Add(second, new(big.Int).Mul(prime, big.NewInt(int64(index%3))))
				second.Mod(second, limit)

				encoded := littleInteger(second)
				putLimbs(update.Center[8:16], encoded[:])
			}
		}

		if index >= 8192 {
			// Force long carry chains in both operands, beyond the random cases.
			first.Sub(limit, big.NewInt(int64(index-8191)))
			second := new(big.Int).Sub(limit, big.NewInt(int64(8448-index)))

			firstEncoded := littleInteger(first)
			secondEncoded := littleInteger(second)

			putLimbs(update.Center[:8], firstEncoded[:])
			putLimbs(update.Center[8:16], secondEncoded[:])
		}

		second := quadraticInteger(update.Center[8:16])

		product := new(big.Int).Mul(first, second)
		quotient := new(big.Int).Div(new(big.Int).Set(product), prime)
		approximation := new(big.Int).Mul(new(big.Int).SetUint64(uint64(update.Center[7])*uint64(update.Center[15])), big.NewInt(38))

		for left := range 8 {
			for right := range 8 {
				column := left + right
				if column < 6 || column > 9 {
					continue
				}

				term := new(big.Int).SetUint64(uint64(update.Center[left]) * uint64(update.Center[8+right]))
				term.Lsh(term, uint(32*(column-6)))

				approximation.Add(approximation, term)
			}
		}

		approximation.Rsh(approximation, 63)

		difference := quotient.Uint64() - approximation.Uint64()
		if difference > 1 {
			t.Fatalf("quotient bound at %d: %d", index, difference)
		}

		corrections += int(difference)

		word := product.Uint64() + 19*approximation.Uint64()
		residue := new(big.Int).Mod(product, prime).Uint64()

		if residue != word && residue != word+19 {
			t.Fatalf("necessary condition at %d", index)
		}

		if uint32(word) > ^uint32(0)-19 && uint32(residue) == uint32(word)+19 {
			lowWraps++
		}

		binary.LittleEndian.PutUint64(expected[index][:], word)
	}

	if corrections == 0 {
		t.Fatal("missing quotient correction coverage")
	}

	if lowWraps == 0 {
		t.Fatal("missing low-word correction wraparound coverage")
	}

	checkRawShader(t, commands, expected)

	for index := range commands {
		commands[index].Action = 18
		clear(expected[index][4:])
	}

	checkRawShader(t, commands, expected)
}

func TestSelectiveProbeResumption(t *testing.T) {
	masks := []uint64{0x0000000000000fff, 0xfff0000000000000, 0x010101010101ffff, 0xf0f0f0f000000000, 0xaaaaaaaa55555555, 0xffffffffffffffff, 0x000000000000ffff, 0xffff000000000000}
	checkSelectiveProbeResumption(t, masks)
}

func TestNarrowProbeResumption(t *testing.T) {
	masks := []uint64{0x00000fff, 0xfff00000, 0x0101ffff, 0xf0f0f0f0, 0xaaaa5555, 0xffffffff, 0x0000ffff, 0xffff0000}
	checkSelectiveProbeResumption(t, masks)
}

func checkSelectiveProbeResumption(t *testing.T, masks []uint64) {
	t.Helper()
	requireVulkan(t)

	state, initial, err := newSeed(1)
	if err != nil {
		t.Fatal(err)
	}

	words := make([]uint64, 6*pairCount)
	steps := make([]uint32, len(words))

	for index := range words {
		center := uint32(pairCount + (2*pairCount+1)*(index/(2*pairCount)))
		distance := uint32(index/2%pairCount + 1)

		steps[index] = center + distance

		if index%2 != 0 {
			steps[index] = center - distance
		}

		key, reconstructError := reconstruct(state.secret, steps[index])
		if reconstructError != nil {
			t.Fatal(reconstructError)
		}

		words[index] = binary.LittleEndian.Uint64(key.Public[:8])
	}

	plan := Plan{count: 8}
	targets := []int{97, 98, 300, 301, 650, 651, 97, 98}

	for index, target := range targets {
		mask := masks[index]
		word := words[target] & mask
		plan.probes[index] = [4]uint32{uint32(mask), uint32(mask >> 32), uint32(word), uint32(word >> 32)}
	}

	engine, err := openDevice(testDevice(t), true, 1, 1, makeTable(plan), searchShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	commands := []command{initial}
	checked := uint32(0)

	for index, word := range words {
		matched := false

		for probe := range plan.count {
			entry := plan.probes[probe]
			matched = matched || uint32(word)&entry[0] == entry[2] && uint32(word>>32)&entry[1] == entry[3]
		}

		if !matched {
			continue
		}

		for {
			completed := dispatch(t, engine, commands, false)
			checked += completed.checked

			clear(commands)

			if len(completed.hits) == 0 {
				if checked > uint32(index) {
					t.Fatalf("selective filter lost candidate %d", index)
				}

				continue
			}

			result := completed.hits[0]
			if checked != uint32(index+1) || result.Steps != steps[index] || uint64(result.Low)|uint64(result.High)<<32 != word {
				t.Fatalf("selective filter ordering at %d: checked=%d hit=%+v", index, checked, result)
			}

			commands[0] = command{Expected: 1, Action: 1}

			break
		}
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}
