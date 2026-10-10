package search

import (
	"crypto/sha512"
	"fmt"
	"io"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

const (
	pairedOffsets = 64
	pairedCenters = batchSize / 2
)

type pairedAffine struct {
	x  fieldElement
	y  fieldElement
	xy fieldElement
}

type pairedScratch struct {
	a            fieldElement
	b            fieldElement
	c            fieldElement
	denominator  fieldElement
	product      fieldElement
	plusInverse  fieldElement
	minusInverse fieldElement
}

type pairedGenerator struct {
	centers    [pairedCenters]pairedAffine
	scratch    [pairedCenters]pairedScratch
	secrets    [pairedCenters][64]byte
	steps      [pairedCenters]uint64
	publicKeys [batchSize][32]byte
	seedBuffer [32]byte
	random     io.Reader
	position   int
	cursor     int
	sink       matchSink
	fieldMode  simd.Mode
}

var (
	pairedOne   = fieldElement{1, 0, 0, 0}
	pairedD     = makePairedD()
	pairedTable = makePairedTable()
	pairedJump  = makePairedJump()
)

func (point *pairedAffine) set(source *edwards25519.Point) {
	xCoordinate, yCoordinate, zCoordinate, _ := source.ExtendedCoordinates()

	var inverse field.Element

	inverse.Invert(zCoordinate)

	xCoordinate.Multiply(xCoordinate, &inverse)
	yCoordinate.Multiply(yCoordinate, &inverse)

	point.x.setField(xCoordinate)
	point.y.setField(yCoordinate)
	multiplyGeneric(&point.xy, &point.x, &point.y)
}

func (state *pairedGenerator) reseed(index int) error {
	for {
		_, err := io.ReadFull(state.random, state.seedBuffer[:])
		if err != nil {
			return fmt.Errorf("seed paired center: %w", err)
		}

		secret := sha512.Sum512(state.seedBuffer[:])

		secret[0] &= 248
		secret[31] &= 63
		secret[31] |= 64

		limit := offsetSecret(secret, reseedRounds)
		if limit[31]&128 != 0 {
			continue
		}

		centerSecret := offsetSecret(secret, pairedOffsets)

		scalar, err := new(edwards25519.Scalar).SetBytesWithClamping(centerSecret[:32])
		if err != nil {
			return err
		}

		state.centers[index].set(new(edwards25519.Point).ScalarBaseMult(scalar))
		state.secrets[index] = secret
		state.steps[index] = pairedOffsets

		return nil
	}
}

func (state *pairedGenerator) reset() error {
	if state.fieldMode == simd.Auto {
		state.fieldMode = defaultFieldMode
	}

	state.position = 0
	state.cursor = batchSize

	for index := range state.centers {
		err := state.reseed(index)
		if err != nil {
			return err
		}
	}

	return nil
}

func (state *pairedGenerator) advanceCenters() error {
	// All centers share a single inversion at a table transition. The secret
	// interval advances by 2*N+1, so neither half repeats an earlier candidate.
	state.prepare(&pairedJump)

	for index := range state.centers {
		if state.steps[index]+3*pairedOffsets+1 > reseedRounds {
			err := state.reseed(index)
			if err != nil {
				return err
			}

			continue
		}

		center := &state.centers[index]
		scratch := &state.scratch[index]

		var (
			numerator fieldElement
			cross     fieldElement
		)

		numerator.multiplyWith(&center.x, &pairedJump.y, state.fieldMode)
		cross.multiplyWith(&pairedJump.x, &center.y, state.fieldMode)
		numerator.add(&numerator, &cross)
		center.x.multiplyWith(&numerator, &scratch.minusInverse, state.fieldMode)
		numerator.add(&scratch.b, &scratch.a)
		center.y.multiplyWith(&numerator, &scratch.plusInverse, state.fieldMode)
		center.xy.multiplyWith(&center.x, &center.y, state.fieldMode)

		state.steps[index] += 2*pairedOffsets + 1
	}

	return nil
}

func (state *pairedGenerator) prepare(offset *pairedAffine) {
	if state.fieldMode == simd.BMI2 {
		pairedPrepareBMI2Only(&state.centers[0], &state.scratch[0], offset)

		var inverse fieldElement

		inverse.invert(&state.scratch[pairedCenters-1].product)
		pairedInverseBMI2Only(&state.scratch[pairedCenters-1], &inverse)

		return
	}

	if state.fieldMode == simd.BMI2ADX {
		pairedPrepareBMI2(&state.centers[0], &state.scratch[0], offset)

		var inverse fieldElement

		inverse.invert(&state.scratch[pairedCenters-1].product)

		pairedInverseBMI2(&state.scratch[pairedCenters-1], &inverse)

		return
	}

	var product fieldElement

	for index := range state.centers {
		center := &state.centers[index]
		scratch := &state.scratch[index]

		multiplyGeneric(&scratch.a, &center.x, &offset.x)
		multiplyGeneric(&scratch.b, &center.y, &offset.y)
		multiplyGeneric(&scratch.c, &center.xy, &offset.xy)

		squareGeneric(&scratch.denominator, &scratch.c)
		scratch.denominator.subtract(&pairedOne, &scratch.denominator)

		if index == 0 {
			product = scratch.denominator
		} else {
			multiplyGeneric(&product, &product, &scratch.denominator)
		}

		scratch.product = product
	}

	var (
		inverse    fieldElement
		reciprocal fieldElement
		factor     fieldElement
	)

	inverse.invert(&product)

	for index := pairedCenters - 1; index >= 0; index-- {
		scratch := &state.scratch[index]

		if index == 0 {
			reciprocal = inverse
		} else {
			multiplyGeneric(&reciprocal, &inverse, &state.scratch[index-1].product)
			multiplyGeneric(&inverse, &inverse, &scratch.denominator)
		}

		multiplyGeneric(&factor, &scratch.c, &reciprocal)
		scratch.plusInverse.add(&reciprocal, &factor)
		scratch.minusInverse.subtract(&reciprocal, &factor)
	}
}

func (state *pairedGenerator) nextBatch() error {
	if state.position == pairedOffsets {
		err := state.advanceCenters()
		if err != nil {
			return err
		}

		state.position = 0
	}

	state.prepare(&pairedTable[state.position])

	state.position++
	state.cursor = 0

	for index := range state.centers {
		scratch := &state.scratch[index]

		var (
			numerator fieldElement
			ordinate  fieldElement
		)

		numerator.add(&scratch.b, &scratch.a)
		ordinate.multiplyWith(&numerator, &scratch.plusInverse, state.fieldMode)
		ordinate.putBytes(&state.publicKeys[index*2])
		numerator.subtract(&scratch.b, &scratch.a)
		ordinate.multiplyWith(&numerator, &scratch.minusInverse, state.fieldMode)
		ordinate.putBytes(&state.publicKeys[index*2+1])
	}

	return nil
}

func (state *pairedGenerator) completeSign(index int) {
	center := &state.centers[index/2]
	offset := &pairedTable[state.position-1]
	scratch := &state.scratch[index/2]

	var (
		first  fieldElement
		second fieldElement
	)

	first.multiplyWith(&center.x, &offset.y, state.fieldMode)
	second.multiplyWith(&center.y, &offset.x, state.fieldMode)

	if index&1 == 0 {
		first.add(&first, &second)
		first.multiplyWith(&first, &scratch.minusInverse, state.fieldMode)
	} else {
		first.subtract(&first, &second)
		first.multiplyWith(&first, &scratch.plusInverse, state.fieldMode)
	}

	state.publicKeys[index][31] = state.publicKeys[index][31]&0x7f | first.isNegative()<<7
}

func (state *pairedGenerator) key(index int) onion.Key {
	return state.snapshot(index).key()
}

func (state *pairedGenerator) snapshot(index int) candidate {
	steps := state.steps[index/2]

	if index&1 == 0 {
		steps += uint64(state.position)
	} else {
		steps -= uint64(state.position)
	}

	return candidate{public: state.publicKeys[index], secret: state.secrets[index/2], steps: steps}
}

func (state *pairedGenerator) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	filter := matcher.SignFilter()

	for range batchSize {
		if state.cursor == batchSize {
			err := state.nextBatch()
			if err != nil {
				return err
			}
		}

		index := state.cursor

		state.cursor++
		stats.Checked++

		if filter != nil && !filter.Match(state.publicKeys[index]) {
			continue
		}

		state.completeSign(index)

		if !matcher.Match(state.publicKeys[index]) {
			continue
		}

		err := state.sink.submit(state.snapshot(index), save, stats)
		if err != nil {
			return err
		}

		// The pending minus candidate shares this seed. Skip it before reseeding
		// and replenish from the next batch; it is never counted as checked.
		state.cursor += 1 - (index & 1)

		err = state.reseed(index / 2)
		if err != nil {
			return err
		}
	}

	return nil
}

func makePairedD() fieldElement {
	numeratorBytes := [32]byte{0x41, 0xdb, 0x01}
	denominatorBytes := [32]byte{0x42, 0xdb, 0x01}

	numerator, _ := new(field.Element).SetBytes(numeratorBytes[:])
	denominator, _ := new(field.Element).SetBytes(denominatorBytes[:])

	denominator.Invert(denominator)
	numerator.Multiply(numerator, denominator)
	numerator.Negate(numerator)

	var result fieldElement

	result.setField(numerator)

	return result
}

func makePairedTable() [pairedOffsets]pairedAffine {
	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())

	point := edwards25519.NewIdentityPoint()

	var table [pairedOffsets]pairedAffine

	for index := range table {
		point.Add(point, step)

		table[index].set(point)
		multiplyGeneric(&table[index].xy, &table[index].xy, &pairedD)
	}

	return table
}

func makePairedJump() pairedAffine {
	step := new(edwards25519.Point).MultByCofactor(edwards25519.NewGeneratorPoint())

	point := edwards25519.NewIdentityPoint()

	for range 2*pairedOffsets + 1 {
		point.Add(point, step)
	}

	var result pairedAffine

	result.set(point)

	multiplyGeneric(&result.xy, &result.xy, &pairedD)

	return result
}
