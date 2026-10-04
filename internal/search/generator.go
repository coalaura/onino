package search

import (
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"io"
	"math/bits"

	"filippo.io/edwards25519"

	"github.com/coalaura/onino/internal/onion"
)

const (
	// Larger batches barely improve inversion amortization while increasing
	// the hot working set. 512 keeps coordinates and scratch near 96 KiB.
	batchSize    = 512
	reseedRounds = 1 << 32
)

type generator struct {
	// Keep hot coordinates and inversion scratch separate from cold secrets.
	points     [batchSize]extendedPoint
	products   [batchSize]fieldElement
	publicKeys [batchSize][32]byte
	secrets    [batchSize][64]byte
	started    [batchSize]uint64
	seedBuffer [32]byte
	round      uint64
	random     io.Reader
}

func (generator *generator) reseed(index int) error {
	// Reuse the entropy buffer: passing a local array through io.Reader would
	// otherwise allocate once for every lane initialization and saved hit.
	for {
		_, err := io.ReadFull(generator.random, generator.seedBuffer[:])
		if err != nil {
			return fmt.Errorf("seed search lane: %w", err)
		}

		secret := sha512.Sum512(generator.seedBuffer[:])

		secret[0] &= 248
		secret[31] &= 63
		secret[31] |= 64

		// Reserve a full epoch of additions without crossing the clamped
		// scalar range. Rejection is negligible, but boundary correctness isn't.
		limit := offsetSecret(secret, reseedRounds)
		if limit[31]&128 != 0 {
			continue
		}

		scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(secret[:32])
		if err != nil {
			return err
		}

		point := new(edwards25519.Point).ScalarBaseMult(scalar)

		generator.points[index].set(point)
		generator.secrets[index] = secret
		generator.started[index] = generator.round

		return nil
	}
}

func (generator *generator) reset() error {
	generator.round = 0

	for index := range generator.points {
		err := generator.reseed(index)
		if err != nil {
			return err
		}
	}

	return nil
}

func (generator *generator) next() {
	generator.round++

	generatePoints(generator.points[:], generator.products[:], generator.publicKeys[:])
}

func (generator *generator) key(index int) onion.Key {
	steps := generator.round - generator.started[index]
	return onion.Key{
		Public: generator.publicKeys[index],
		Secret: offsetSecret(generator.secrets[index], steps),
	}
}

func offsetSecret(secret [64]byte, steps uint64) [64]byte {
	// Steps are bounded by reseedRounds. Adding eight preserves clamping;
	// the second half is the independently seeded Ed25519 nonce prefix.
	carry := steps << 3

	for offset := 0; offset < 32; offset += 8 {
		word := binary.LittleEndian.Uint64(secret[offset:])

		word, carry = bits.Add64(word, carry, 0)

		binary.LittleEndian.PutUint64(secret[offset:], word)
	}

	return secret
}
