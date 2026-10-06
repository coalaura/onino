package pattern

import (
	"encoding/binary"

	"github.com/coalaura/onino/internal/onion"
)

// UsesChecksum identifies workers that can benefit from immediate SHA3.
func (matcher *Matcher) UsesChecksum() bool {
	return matcher.kind == matcherBoundary || matcher.kind == matcherSuffix
}

// MatchChecksum is used only by workers selected with AVX-512F and OS state
// support. Keeping this entry separate preserves the original inlined matcher.
func (matcher *Matcher) MatchChecksum(data [32]byte) bool {
	if matcher.kind == matcherSuffix {
		return matcher.boundary.matchSuffixAVX512(&data)
	}

	return matcher.boundary.matchAVX512(&data)
}

func (matcher *boundaryMatcher) matchAVX512(data *[32]byte) bool {
	if matcher.body != nil && matcher.body.Match(*data) {
		return true
	}

	if !matcher.filter.Match(*data) {
		return false
	}

	checksum := onion.ChecksumAVX512(data)
	nibble := checksum[0] >> 4

	if matcher.checksums != 0 {
		return matcher.checksums&(uint16(1)<<nibble) != 0
	}

	check := matcher.checks[nibble]
	return check != nil && check.Match(*data)
}

func (matcher *boundaryMatcher) matchSuffixAVX512(data *[32]byte) bool {
	index := int(binary.BigEndian.Uint16(data[30:])) & (len(matcher.suffixes) - 1)

	accepted := matcher.suffixes[index]
	if accepted == 0 || accepted == 0xffff {
		return accepted != 0
	}

	checksum := onion.ChecksumAVX512(data)
	return accepted&(uint16(1)<<(checksum[0]>>4)) != 0
}
