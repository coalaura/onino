//go:build !purego

package search

import (
	"encoding/binary"

	"github.com/coalaura/onino/internal/pattern"
)

const radixMask = uint64(1<<51 - 1)

// Limbs are normalized to [0, 2^51). Values need not be canonical. No IFMA
// multiplicand exceeds 52 bits, including doubled square cross terms.
type ifmaElement [5][ifmaLanes]uint64

func (result *ifmaElement) setLane(index int, source *fieldElement) {
	var encoded [32]byte

	source.putBytes(&encoded)

	result[0][index] = binary.LittleEndian.Uint64(encoded[0:8]) & radixMask
	result[1][index] = binary.LittleEndian.Uint64(encoded[6:14]) >> 3 & radixMask
	result[2][index] = binary.LittleEndian.Uint64(encoded[12:20]) >> 6 & radixMask
	result[3][index] = binary.LittleEndian.Uint64(encoded[19:27]) >> 1 & radixMask
	result[4][index] = binary.LittleEndian.Uint64(encoded[24:32]) >> 12 & radixMask
}

func (source *ifmaElement) lane(index int) fieldElement {
	// Normalization makes these shifts lossless; the scalar encoder performs
	// the mandatory final reduction modulo p before exposing candidate bytes.
	return fieldElement{
		source[0][index] | source[1][index]<<51,
		source[1][index]>>13 | source[2][index]<<38,
		source[2][index]>>26 | source[3][index]<<25,
		source[3][index]>>39 | source[4][index]<<12,
	}
}

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func ifmaMultiply(result, left, right *ifmaElement)

//go:noescape
//go:abiinternal result=AX source=BX ->
func ifmaSquare(result, source *ifmaElement)

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func ifmaAdd(result, left, right *ifmaElement)

//go:noescape
//go:abiinternal result=AX left=BX right=CX ->
func ifmaSubtract(result, left, right *ifmaElement)

//go:noescape
//go:abiinternal centers=AX scratch=BX offset=CX ->
func ifmaForward(centers *[pairedCenters / ifmaLanes]ifmaAffine, scratch *[pairedCenters / ifmaLanes]ifmaScratch, offset *ifmaAffine)

//go:noescape
//go:abiinternal scratch=AX inverse=BX factor=CX ->
func ifmaReverse(scratch *[pairedCenters / ifmaLanes]ifmaScratch, inverse, factor *ifmaElement)

// ifmaCanonicalFilter requires normalized limbs and 1 <= plan.Count <= 8.
//
//go:noescape
//go:abiinternal value=AX plan=BX -> mask=AX
func ifmaCanonicalFilter(value *ifmaElement, plan *pattern.PrefixPlan) (mask uint64)
