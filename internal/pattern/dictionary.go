package pattern

import (
	"encoding/binary"
	"math/bits"
	"slices"
)

const (
	dictionaryThreshold = 128
	dictionaryStride    = 4
)

// The bitmap and rank directory occupy 5 KiB regardless of dictionary size.
// Only occupied triplets have buckets; verifiers retain one literal rather than
// a 52-position table. Position masks reject disallowed anchors before checking.
// One anchor per offset residue covers every start when scanning every fourth
// triplet. Anchors share immutable parsed verifiers; literals shorter than six
// symbols retain the existing matcher because they cannot cover all residues.
type tripletDictionary struct {
	membership [512]uint64
	ranks      [512]uint16
	buckets    []tripletBucket
	checks     []dictionaryCheck
}

type tripletBucket struct {
	positions uint64
	first     uint32
	end       uint32
}

type dictionaryCheck struct {
	pattern *parsedPattern
	offset  int
}

func (dictionary *tripletDictionary) match(data *[32]byte, ignoreSign bool) bool {
	for block := range 5 {
		word := binary.BigEndian.Uint64(data[block*5:])
		if dictionary.matchWindows(word, block*8, 8, data, ignoreSign) {
			return true
		}
	}

	// Seven final bytes followed by zero padding cover windows 40 through 49.
	word := binary.BigEndian.Uint64(data[24:]) << 8
	return dictionary.matchWindows(word, 40, 10, data, ignoreSign)
}

//go:inline
func (dictionary *tripletDictionary) matchWindows(word uint64, first, count int, data *[32]byte, ignoreSign bool) bool {
	skip := (dictionaryStride - first%dictionaryStride) % dictionaryStride
	word <<= skip * 5

	for position := first + skip; position < first+count; position += dictionaryStride {
		triplet := uint16(word >> 49)
		if dictionary.matchTriplet(triplet, position, data, ignoreSign) {
			return true
		}

		// Symbol 49's value bit 1 is the compressed point's unknown sign.
		if ignoreSign && position >= 47 {
			alternate := triplet ^ (uint16(1) << (position*5 - 234))
			if dictionary.matchTriplet(alternate, position, data, ignoreSign) {
				return true
			}
		}

		word <<= 5 * dictionaryStride
	}

	return false
}

//go:inline
func (dictionary *tripletDictionary) matchTriplet(triplet uint16, position int, data *[32]byte, ignoreSign bool) bool {
	word := dictionary.membership[triplet>>6]
	mask := uint64(1) << (triplet & 63)

	if word&mask == 0 {
		return false
	}

	index := int(dictionary.ranks[triplet>>6]) + bits.OnesCount64(word&(mask-1))

	bucket := &dictionary.buckets[index]
	if bucket.positions&(uint64(1)<<position) == 0 {
		return false
	}

	for index := bucket.first; index < bucket.end; index++ {
		check := &dictionary.checks[index]

		pattern := check.pattern
		if pattern.anchored {
			if matchLiteral(data, pattern.prefix, 0, ignoreSign) && matchLiteral(data, pattern.suffix, encodedSize-len(pattern.suffix), ignoreSign) {
				return true
			}
		} else {
			start := position - check.offset
			if start >= pattern.first && start <= pattern.last && matchLiteral(data, pattern.literal, start, ignoreSign) {
				return true
			}
		}
	}

	return false
}

func compileDictionary(patterns []parsedPattern) *tripletDictionary {
	frequencies := make(map[uint16]int, len(patterns)*4)

	for _, pattern := range patterns {
		literal := anchorLiteral(pattern)

		positions := 1

		if !pattern.anchored {
			positions = pattern.last - pattern.first + 1
		}

		for offset := 0; offset+3 <= len(literal); offset++ {
			frequencies[literalTriplet(literal, offset)] += positions
		}
	}

	groups := make(map[uint16][]dictionaryCheck, len(patterns))

	for index := range patterns {
		pattern := &patterns[index]

		literal := anchorLiteral(*pattern)
		if len(literal) < dictionaryStride+2 {
			continue
		}

		base := 0
		residues := dictionaryStride

		if pattern.anchored {
			residues = 1

			if len(pattern.suffix) > len(pattern.prefix) {
				base = encodedSize - len(pattern.suffix)
			}
		}

		for residue := range residues {
			offset := residue

			if pattern.anchored {
				offset = (dictionaryStride - base%dictionaryStride) % dictionaryStride
			}

			triplet := literalTriplet(literal, offset)

			for candidate := offset + dictionaryStride; candidate+3 <= len(literal); candidate += dictionaryStride {
				value := literalTriplet(literal, candidate)
				if frequencies[value] < frequencies[triplet] {
					offset = candidate
					triplet = value
				}
			}

			groups[triplet] = append(groups[triplet], dictionaryCheck{pattern: pattern, offset: base + offset})
		}
	}

	keys := make([]uint16, 0, len(groups))

	for triplet := range groups {
		keys = append(keys, triplet)
	}

	slices.Sort(keys)

	dictionary := &tripletDictionary{
		buckets: make([]tripletBucket, 0, len(groups)),
		checks:  make([]dictionaryCheck, 0, len(patterns)*dictionaryStride),
	}

	for _, triplet := range keys {
		checks := groups[triplet]

		bucket := tripletBucket{first: uint32(len(dictionary.checks))}

		for _, check := range checks {
			if check.pattern.anchored {
				bucket.positions |= uint64(1) << check.offset
			} else {
				for position := check.pattern.first; position <= check.pattern.last; position++ {
					bucket.positions |= uint64(1) << (position + check.offset)
				}
			}
		}

		dictionary.checks = append(dictionary.checks, checks...)
		bucket.end = uint32(len(dictionary.checks))

		dictionary.buckets = append(dictionary.buckets, bucket)
		dictionary.membership[triplet>>6] |= uint64(1) << (triplet & 63)
	}

	rank := 0

	for index, word := range dictionary.membership {
		dictionary.ranks[index] = uint16(rank)
		rank += bits.OnesCount64(word)
	}

	return dictionary
}

func anchorLiteral(pattern parsedPattern) string {
	if !pattern.anchored {
		return pattern.literal
	}

	if len(pattern.suffix) > len(pattern.prefix) {
		return pattern.suffix
	}

	return pattern.prefix
}

func literalTriplet(literal string, offset int) uint16 {
	return uint16(symbolValue(literal[offset]))<<10 | uint16(symbolValue(literal[offset+1]))<<5 | uint16(symbolValue(literal[offset+2]))
}

//go:inline
func matchLiteral(data *[32]byte, literal string, first int, ignoreSign bool) bool {
	for index := range len(literal) {
		position := first + index
		inputBit := position * 5
		offset := inputBit / 8
		word := uint16(data[offset]) << 8

		if offset < 31 {
			word |= uint16(data[offset+1])
		}

		value := byte(word>>(11-inputBit%8)) & 31
		difference := value ^ symbolValue(literal[index])

		if ignoreSign && position == 49 {
			difference &^= 2
		}

		if difference != 0 {
			return false
		}
	}

	return true
}
