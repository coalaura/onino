package pattern

import "encoding/binary"

const encodedSize = 52

const (
	matcherEmpty = iota
	matcherSingle
	matcherOneWide
	matcherScans
	matcherCharacters
	matcherGeneral
	matcherDictionary
)

// Matcher is an immutable, concurrency-safe OR of compiled patterns. Its zero
// value matches nothing. Input is interpreted as lowercase, unpadded RFC 4648
// base32, without constructing the encoded string.
type Matcher struct {
	single        wordProbe
	kind          uint8
	offset        uint8
	rest          *bitPattern
	scans         []scanPlan
	scanChecks    []*[encodedSize]bitPattern
	characters    []characterSearch
	tables        [4]probeTable
	signFilter    *Matcher
	signDependent bool
	frequent      bool
	dictionary    *tripletDictionary
	ignoreSign    bool
}

type wordProbe struct {
	mask  uint64
	value uint64
}

type bitPattern struct {
	mask  [4]uint64
	value [4]uint64
}

// Separate hot 16-byte probes from cold verification data: a miss never reads
// the latter. A table with only single-word patterns needs no verification data.
type probeTable struct {
	probes []wordProbe
	checks []bitPattern
}

// Four hot filters fit in 64 bytes, with no struct padding. Mask and value each
// repeat a 16-bit lane twice for broadcasting over overlapping 15-bit windows.
type scanPlan struct {
	positions uint64
	mask      uint32
	value     uint32
}

// SignFilter returns a necessary-condition matcher that ignores only byte 31
// bit 7, or nil when eagerly completing the sign is preferable. A surviving
// candidate must have its real sign completed and pass Match before acceptance.
// The filter can be the receiver when none of its patterns inspect that bit.
func (matcher *Matcher) SignFilter() *Matcher {
	return matcher.signFilter
}

// Match reports whether any compiled pattern matches data. It performs no
// allocation or base32 encoding and may be called concurrently.
//
//go:inline
func (matcher *Matcher) Match(data [32]byte) bool {
	switch matcher.kind {
	case matcherEmpty:
		return false
	case matcherSingle, matcherOneWide:
		word := binary.LittleEndian.Uint64(data[matcher.offset:])
		if word&matcher.single.mask != matcher.single.value {
			return false
		}

		return matcher.kind == matcherSingle || matcher.rest.matchesInput(&data)
	case matcherScans:
		return matcher.matchScans(&data)
	case matcherCharacters:
		return matcher.matchCharacters(&data)
	case matcherDictionary:
		return matcher.dictionary.match(&data, matcher.ignoreSign)
	}

	return matcher.matchGeneral(&data)
}

func (matcher *Matcher) matchGeneral(data *[32]byte) bool {
	if matcher.matchCharacters(data) {
		return true
	}

	words := [4]uint64{
		binary.LittleEndian.Uint64(data[0:8]),
		binary.LittleEndian.Uint64(data[8:16]),
		binary.LittleEndian.Uint64(data[16:24]),
		binary.LittleEndian.Uint64(data[24:32]),
	}

	for wordIndex := range matcher.tables {
		table := &matcher.tables[wordIndex]
		word := words[wordIndex]

		for index := range table.probes {
			probe := &table.probes[index]
			if word&probe.mask != probe.value {
				continue
			}

			if len(table.checks) == 0 || table.checks[index].matches(&words) {
				return true
			}
		}
	}

	if matcher.dictionary != nil && matcher.dictionary.match(data, matcher.ignoreSign) {
		return true
	}

	return matcher.matchScans(data)
}

func (pattern *bitPattern) matchesInput(data *[32]byte) bool {
	words := [4]uint64{
		binary.LittleEndian.Uint64(data[0:8]),
		binary.LittleEndian.Uint64(data[8:16]),
		binary.LittleEndian.Uint64(data[16:24]),
		binary.LittleEndian.Uint64(data[24:32]),
	}

	return pattern.matches(&words)
}

func (pattern *bitPattern) matches(words *[4]uint64) bool {
	difference := (words[0] ^ pattern.value[0]) & pattern.mask[0]
	difference |= (words[1] ^ pattern.value[1]) & pattern.mask[1]
	difference |= (words[2] ^ pattern.value[2]) & pattern.mask[2]
	difference |= (words[3] ^ pattern.value[3]) & pattern.mask[3]

	return difference == 0
}
