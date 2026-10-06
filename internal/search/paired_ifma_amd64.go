//go:build !purego

package search

import (
	"crypto/sha512"
	"fmt"
	"io"
	"math/bits"

	"filippo.io/edwards25519"
	"github.com/coalaura/onino/internal/pattern"
)

type ifmaAffine struct {
	x  ifmaElement
	y  ifmaElement
	xy ifmaElement
}

type ifmaScratch struct {
	a            ifmaElement
	b            ifmaElement
	c            ifmaElement
	denominator  ifmaElement
	product      ifmaElement
	plusInverse  ifmaElement
	minusInverse ifmaElement
}

type ifmaGenerator struct {
	centers    [pairedCenters / ifmaLanes]ifmaAffine
	scratch    [pairedCenters / ifmaLanes]ifmaScratch
	secrets    [pairedCenters][64]byte
	steps      [pairedCenters]uint64
	publicKeys [batchSize][32]byte
	seedBuffer [32]byte
	random     io.Reader
	position   int
	cursor     int
	sink       matchSink
	plan       pattern.PrefixPlan
	masks      [pairedCenters / ifmaLanes]uint16
}

func (state *ifmaGenerator) reseed(index int) error {
	for {
		_, err := io.ReadFull(state.random, state.seedBuffer[:])
		if err != nil {
			return fmt.Errorf("seed IFMA center: %w", err)
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

		var point pairedAffine

		point.set(new(edwards25519.Point).ScalarBaseMult(scalar))

		center := &state.centers[index/ifmaLanes]
		center.x.setLane(index%ifmaLanes, &point.x)
		center.y.setLane(index%ifmaLanes, &point.y)
		center.xy.setLane(index%ifmaLanes, &point.xy)

		state.secrets[index] = secret
		state.steps[index] = pairedOffsets

		return nil
	}
}

func (state *ifmaGenerator) reset() error {
	state.position = 0
	state.cursor = batchSize

	for index := range pairedCenters {
		err := state.reseed(index)
		if err != nil {
			return err
		}
	}

	return nil
}

func (state *ifmaGenerator) prepare(offset *pairedAffine) {
	vectorOffset := ifmaAffine{x: broadcastIFMA(&offset.x), y: broadcastIFMA(&offset.y), xy: broadcastIFMA(&offset.xy)}

	ifmaForward(&state.centers, &state.scratch, &vectorOffset)

	var (
		inverse ifmaElement
		factor  ifmaElement
	)

	ifmaHybridInverse(&inverse, &state.scratch[len(state.scratch)-1].product)
	ifmaReverse(&state.scratch, &inverse, &factor)
}

func (state *ifmaGenerator) advanceCenters() error {
	state.prepare(&pairedJump)

	xOffset := broadcastIFMA(&pairedJump.x)
	yOffset := broadcastIFMA(&pairedJump.y)

	var (
		numerator ifmaElement
		cross     ifmaElement
	)

	for index := range state.centers {
		center := &state.centers[index]
		scratch := &state.scratch[index]

		ifmaMultiply(&numerator, &center.x, &yOffset)
		ifmaMultiply(&cross, &center.y, &xOffset)
		ifmaAdd(&numerator, &numerator, &cross)
		ifmaMultiply(&center.x, &numerator, &scratch.minusInverse)
		ifmaAdd(&numerator, &scratch.b, &scratch.a)
		ifmaMultiply(&center.y, &numerator, &scratch.plusInverse)
		ifmaMultiply(&center.xy, &center.x, &center.y)
	}

	for index := range state.steps {
		if state.steps[index]+3*pairedOffsets+1 > reseedRounds {
			err := state.reseed(index)
			if err != nil {
				return err
			}
		} else {
			state.steps[index] += 2*pairedOffsets + 1
		}
	}

	return nil
}

func (state *ifmaGenerator) nextBatch() error {
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
	state.serialize()

	return nil
}

func (state *ifmaGenerator) serialize() {
	var (
		numerator ifmaElement
		plus      ifmaElement
		minus     ifmaElement
	)

	for index := range state.scratch {
		scratch := &state.scratch[index]

		ifmaAdd(&numerator, &scratch.b, &scratch.a)
		ifmaMultiply(&plus, &numerator, &scratch.plusInverse)
		ifmaSubtract(&numerator, &scratch.b, &scratch.a)
		ifmaMultiply(&minus, &numerator, &scratch.minusInverse)

		for lane := range ifmaLanes {
			ordinate := plus.lane(lane)
			ordinate.putBytes(&state.publicKeys[index*ifmaLanes*2+lane*2])
			ordinate = minus.lane(lane)
			ordinate.putBytes(&state.publicKeys[index*ifmaLanes*2+lane*2+1])
		}
	}
}

func (state *ifmaGenerator) nextFilteredBatch() error {
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

	var (
		plus  ifmaElement
		minus ifmaElement
	)

	for index := range state.scratch {
		scratch := &state.scratch[index]

		mask := ifmaPairedFilter(&plus, &minus, scratch, &state.plan)
		state.masks[index] = uint16(mask)

		for mask != 0 {
			candidate := bits.TrailingZeros64(mask)
			ordinate := &plus

			if candidate&1 != 0 {
				ordinate = &minus
			}

			value := ordinate.lane(candidate / 2)
			value.putBytes(&state.publicKeys[index*ifmaLanes*2+candidate])

			mask &= mask - 1
		}
	}

	return nil
}

func (state *ifmaGenerator) searchFilteredBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	for range batchSize {
		if state.cursor == batchSize {
			err := state.nextFilteredBatch()
			if err != nil {
				return err
			}
		}

		index := state.cursor

		state.cursor++
		stats.Checked++

		if state.masks[index/(ifmaLanes*2)]>>(index%(ifmaLanes*2))&1 == 0 {
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

		state.cursor += 1 - (index & 1)

		err = state.reseed(index / 2)
		if err != nil {
			return err
		}
	}

	return nil
}

func (state *ifmaGenerator) completeSign(index int) {
	group := index / (ifmaLanes * 2)
	lane := index / 2 % ifmaLanes

	center := &state.centers[group]
	offset := &pairedTable[state.position-1]
	scratch := &state.scratch[group]

	first := center.x.lane(lane)
	second := center.y.lane(lane)

	first.multiply(&first, &offset.y)
	second.multiply(&second, &offset.x)

	if index&1 == 0 {
		inverse := scratch.minusInverse.lane(lane)

		first.add(&first, &second)
		first.multiply(&first, &inverse)
	} else {
		inverse := scratch.plusInverse.lane(lane)

		first.subtract(&first, &second)
		first.multiply(&first, &inverse)
	}

	state.publicKeys[index][31] = state.publicKeys[index][31]&0x7f | first.isNegative()<<7
}

func (state *ifmaGenerator) snapshot(index int) candidate {
	steps := state.steps[index/2]

	if index&1 == 0 {
		steps += uint64(state.position)
	} else {
		steps -= uint64(state.position)
	}

	return candidate{public: state.publicKeys[index], secret: state.secrets[index/2], steps: steps}
}

func (state *ifmaGenerator) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
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

		state.cursor += 1 - (index & 1)

		err = state.reseed(index / 2)
		if err != nil {
			return err
		}
	}

	return nil
}

func broadcastIFMA(source *fieldElement) ifmaElement {
	var result ifmaElement

	result.setLane(0, source)

	for limb := range result {
		for lane := 1; lane < ifmaLanes; lane++ {
			result[limb][lane] = result[limb][0]
		}
	}

	return result
}

func ifmaHybridInverse(result, source *ifmaElement) {
	var (
		values   [ifmaLanes]fieldElement
		products [ifmaLanes]fieldElement
	)

	for lane := range values {
		values[lane] = source.lane(lane)

		if lane == 0 {
			products[lane] = values[lane]
		} else {
			products[lane].multiply(&products[lane-1], &values[lane])
		}
	}

	var (
		inverse    fieldElement
		reciprocal fieldElement
	)

	inverse.invert(&products[ifmaLanes-1])

	for lane := ifmaLanes - 1; lane >= 0; lane-- {
		if lane == 0 {
			reciprocal = inverse
		} else {
			reciprocal.multiply(&inverse, &products[lane-1])
			inverse.multiply(&inverse, &values[lane])
		}

		result.setLane(lane, &reciprocal)
	}
}
