package pattern

import (
	"encoding/binary"
	"math/bits"
	"slices"
)

const anchoredThreshold = 64

// Both fragments contain 15 known input bits. The suffix fragment deliberately
// excludes the point sign, so exact and signless matchers share the same index.
type anchoredDictionary struct {
	tables [2]anchorTable
}

type anchorTable struct {
	membership [512]uint64
	ranks      [512]uint16
	buckets    []anchorBucket
	checks     []bitPattern
}

type anchorBucket struct {
	first uint32
	end   uint32
}

func (dictionary *anchoredDictionary) match(data *[32]byte, ignoreSign bool) bool {
	fragments := [2]uint16{binary.BigEndian.Uint16(data[:2]) >> 1, binary.LittleEndian.Uint16(data[30:]) & 0x7fff}

	for side := range dictionary.tables {
		table := &dictionary.tables[side]
		fragment := fragments[side]

		word := table.membership[fragment>>6]
		mask := uint64(1) << (fragment & 63)

		if word&mask == 0 {
			continue
		}

		index := int(table.ranks[fragment>>6]) + bits.OnesCount64(word&(mask-1))
		bucket := table.buckets[index]

		for checkIndex := bucket.first; checkIndex < bucket.end; checkIndex++ {
			check := &table.checks[checkIndex]

			difference := (binary.LittleEndian.Uint64(data[:8]) ^ check.value[0]) & check.mask[0]
			difference |= (binary.LittleEndian.Uint64(data[8:16]) ^ check.value[1]) & check.mask[1]
			difference |= (binary.LittleEndian.Uint64(data[16:24]) ^ check.value[2]) & check.mask[2]

			last := (binary.LittleEndian.Uint64(data[24:]) ^ check.value[3]) & check.mask[3]

			if ignoreSign {
				last &^= uint64(1) << 63
			}

			if difference|last == 0 {
				return true
			}
		}
	}

	return false
}

func compileAnchors(patterns []parsedPattern) *anchoredDictionary {
	frequencies := [2]map[uint16]int{make(map[uint16]int, len(patterns)), make(map[uint16]int, len(patterns))}

	for _, pattern := range patterns {
		if !indexedAnchor(pattern) {
			continue
		}

		fragments := anchorFragments(pattern)

		if len(pattern.prefix) >= 3 {
			frequencies[0][fragments[0]]++
		}

		if len(pattern.suffix) >= 4 {
			frequencies[1][fragments[1]]++
		}
	}

	groups := [2]map[uint16][]bitPattern{make(map[uint16][]bitPattern, len(patterns)), make(map[uint16][]bitPattern, len(patterns))}

	for _, pattern := range patterns {
		if !indexedAnchor(pattern) {
			continue
		}

		fragments := anchorFragments(pattern)
		side := 0

		if len(pattern.prefix) < 3 || len(pattern.suffix) >= 4 && frequencies[1][fragments[1]] < frequencies[0][fragments[0]] {
			side = 1
		}

		var check bitPattern

		addLiteral(&check, pattern.prefix, 0)
		addLiteral(&check, pattern.suffix, encodedSize-len(pattern.suffix))

		groups[side][fragments[side]] = append(groups[side][fragments[side]], check)
	}

	dictionary := new(anchoredDictionary)

	for side := range groups {
		table := &dictionary.tables[side]

		keys := make([]uint16, 0, len(groups[side]))
		count := 0

		for fragment, checks := range groups[side] {
			keys = append(keys, fragment)
			count += len(checks)
		}

		slices.Sort(keys)

		table.buckets = make([]anchorBucket, 0, len(keys))
		table.checks = make([]bitPattern, 0, count)

		for _, fragment := range keys {
			bucket := anchorBucket{first: uint32(len(table.checks))}

			table.checks = append(table.checks, groups[side][fragment]...)

			bucket.end = uint32(len(table.checks))

			table.buckets = append(table.buckets, bucket)
			table.membership[fragment>>6] |= uint64(1) << (fragment & 63)
		}

		rank := 0

		for index, word := range table.membership {
			table.ranks[index] = uint16(rank)
			rank += bits.OnesCount64(word)
		}
	}

	return dictionary
}

func indexedAnchor(pattern parsedPattern) bool {
	return pattern.anchored && (len(pattern.prefix) >= 3 || len(pattern.suffix) >= 4)
}

func anchorFragments(pattern parsedPattern) [2]uint16 {
	var check bitPattern

	addLiteral(&check, pattern.prefix, 0)
	addLiteral(&check, pattern.suffix, encodedSize-len(pattern.suffix))

	first := bits.ReverseBytes16(uint16(check.value[0])) >> 1
	last := uint16(check.value[3]>>48) & 0x7fff

	return [2]uint16{first, last}
}
