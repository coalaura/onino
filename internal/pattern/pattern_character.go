package pattern

import "encoding/binary"

const (
	characterLowBits  = uint64(0x0842108421)
	characterHighBits = uint64(0x8421084210)
	characterLowFour  = uint64(0x7bdef7bdef)
)

type characterSearch struct {
	value uint64
	first uint64
	last  uint64
}

func (matcher *Matcher) matchCharacters(data *[32]byte) bool {
	for index := range matcher.characters {
		if matcher.characters[index].matches(data) {
			return true
		}
	}

	return false
}

func (search *characterSearch) matches(data *[32]byte) bool {
	// Each five-byte block is eight packed base32 symbols. SWAR compares all
	// eight together; no symbol extraction or conversion is necessary.
	word := binary.BigEndian.Uint64(data[0:8]) >> 24
	if zeroCharacters(word^search.value)&search.first != 0 {
		return true
	}

	word = binary.BigEndian.Uint64(data[5:13]) >> 24
	if zeroCharacters(word^search.value)&characterHighBits != 0 {
		return true
	}

	word = binary.BigEndian.Uint64(data[10:18]) >> 24
	if zeroCharacters(word^search.value)&characterHighBits != 0 {
		return true
	}

	word = binary.BigEndian.Uint64(data[15:23]) >> 24
	if zeroCharacters(word^search.value)&characterHighBits != 0 {
		return true
	}

	word = binary.BigEndian.Uint64(data[20:28]) >> 24
	if zeroCharacters(word^search.value)&characterHighBits != 0 {
		return true
	}

	// Reuse an in-bounds load for bytes 25..29; bytes 30..31 form the final
	// four symbols after appending RFC 4648's four zero padding bits.
	word = binary.BigEndian.Uint64(data[24:32]) >> 16
	if zeroCharacters(word^search.value)&characterHighBits != 0 {
		return true
	}

	word = uint64(binary.BigEndian.Uint16(data[30:32])) << 4
	return zeroCharacters(word^search.value)&search.last != 0
}

func zeroCharacters(value uint64) uint64 {
	// Exact per-five-bit zero detection. The usual subtract-and-mask trick
	// propagates borrows and can falsely match beside an excluded endpoint.
	return ^(((value & characterLowFour) + characterLowFour) | value | characterLowFour)
}
