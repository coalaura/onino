//go:build gpu

package gpu

import (
	"crypto/rand"
	"crypto/sha512"
	"encoding/binary"
	"math/bits"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/onion"
)

type seed struct {
	secret     [64]byte
	generation uint32
}

func newSeed(generation uint32) (seed, command, error) {
	for {
		var entropy [32]byte

		_, err := rand.Read(entropy[:])
		if err != nil {
			return seed{}, command{}, err
		}

		secret := sha512.Sum512(entropy[:])
		secret[0] &= 248
		secret[31] &= 63
		secret[31] |= 64

		limit := offsetSecret(secret, 1<<32)
		if limit[31]&128 != 0 {
			continue
		}

		center := offsetSecret(secret, 64)

		scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(center[:32])
		if err != nil {
			return seed{}, command{}, err
		}

		point := new(edwards25519.Point).ScalarBaseMult(scalar)
		refill := command{Center: affine(point, false), Expected: generation - 1, Generation: generation, Action: 2}

		return seed{secret: secret, generation: generation}, refill, nil
	}
}

func reconstruct(secret [64]byte, steps uint32) (onion.Key, error) {
	key := onion.Key{Secret: offsetSecret(secret, uint64(steps))}

	scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(key.Secret[:32])
	if err != nil {
		return onion.Key{}, err
	}

	copy(key.Public[:], new(edwards25519.Point).ScalarBaseMult(scalar).Bytes())

	return key, nil
}

func makeTable(plan Plan) []uint32 {
	table := make([]uint32, 65*30+8*4+1)

	for index := range 65 {
		steps := uint64(index + 1)

		if index == 64 {
			steps = 129
		}

		var encoded [32]byte

		binary.LittleEndian.PutUint64(encoded[:8], steps*8)

		scalar, _ := new(edwards25519.Scalar).SetCanonicalBytes(encoded[:])
		point := new(edwards25519.Point).ScalarBaseMult(scalar)

		coordinates := affine(point, true)
		copy(table[index*30:], coordinates[:])
	}

	for index := range plan.probes {
		copy(table[65*30+index*4:], plan.probes[index][:])
	}

	table[len(table)-1] = plan.count

	return table
}

func affine(point *edwards25519.Point, withD bool) [30]uint32 {
	xCoordinate, yCoordinate, zCoordinate, _ := point.ExtendedCoordinates()

	var (
		reciprocal field.Element
		mixed      field.Element
	)

	reciprocal.Invert(zCoordinate)
	xCoordinate.Multiply(xCoordinate, &reciprocal)
	yCoordinate.Multiply(yCoordinate, &reciprocal)
	mixed.Multiply(xCoordinate, yCoordinate)

	if withD {
		var (
			numerator   [32]byte
			denominator [32]byte
		)

		binary.LittleEndian.PutUint32(numerator[:4], 121665)
		binary.LittleEndian.PutUint32(denominator[:4], 121666)

		first, _ := new(field.Element).SetBytes(numerator[:])
		second, _ := new(field.Element).SetBytes(denominator[:])

		second.Invert(second)
		first.Multiply(first, second)
		first.Negate(first)
		mixed.Multiply(&mixed, first)
	}

	var result [30]uint32

	putLimbs(result[:10], xCoordinate.Bytes())
	putLimbs(result[10:20], yCoordinate.Bytes())
	putLimbs(result[20:], mixed.Bytes())

	return result
}

func putLimbs(destination []uint32, encoded []byte) {
	position := 0

	for index := range destination {
		width := 26 - index%2

		for bit := range width {
			destination[index] |= uint32((encoded[position/8]>>(position%8))&1) << bit
			position++
		}
	}
}

func offsetSecret(secret [64]byte, steps uint64) [64]byte {
	value, carry := bits.Add64(binary.LittleEndian.Uint64(secret[:8]), steps<<3, 0)
	binary.LittleEndian.PutUint64(secret[:8], value)

	for index := 8; index < 32; index += 8 {
		value, carry = bits.Add64(binary.LittleEndian.Uint64(secret[index:index+8]), 0, carry)
		binary.LittleEndian.PutUint64(secret[index:index+8], value)
	}

	return secret
}
