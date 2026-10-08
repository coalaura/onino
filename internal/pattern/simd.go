package pattern

import (
	"encoding/binary"

	"github.com/coalaura/onino/internal/onion"
)

// MatchingEngine describes the compiled matcher, independently of arithmetic.
func (matcher *Matcher) MatchingEngine() string {
	if matcher.usesVectorMatching() {
		return "AVX2 scan + scalar verification"
	}

	if matcher.PrefixPlan().Count != 0 {
		return "scalar prefix"
	}

	if matcher.kind == matcherSuffix {
		return "scalar suffix"
	}

	return "scalar"
}

func (matcher *Matcher) usesVectorMatching() bool {
	if matcher == nil {
		return false
	}

	if len(matcher.scans) != 0 {
		return true
	}

	if matcher.signFilter != nil && matcher.signFilter != matcher && matcher.signFilter.usesVectorMatching() {
		return true
	}

	if matcher.boundary == nil {
		return false
	}

	boundary := matcher.boundary
	if boundary.body.usesVectorMatching() || boundary.filter.usesVectorMatching() {
		return true
	}

	for _, check := range boundary.checks {
		if check.usesVectorMatching() {
			return true
		}
	}

	return false
}

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
