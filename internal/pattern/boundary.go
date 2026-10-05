package pattern

import (
	"encoding/binary"
	"fmt"
	"math/bits"

	"github.com/coalaura/onino/internal/onion"
)

type boundaryMatcher struct {
	body      *Matcher
	filter    *Matcher
	checks    [16]*Matcher
	checksums uint16
	suffixes  []uint16
}

type boundaryGroup struct {
	pattern   parsedPattern
	checksums uint16
}

func (matcher *boundaryMatcher) match(data *[32]byte) bool {
	if matcher.body != nil && matcher.body.Match(*data) {
		return true
	}

	if !matcher.filter.Match(*data) {
		return false
	}

	checksum := onion.Checksum(data)
	nibble := checksum[0] >> 4

	if matcher.checksums != 0 {
		return matcher.checksums&(uint16(1)<<nibble) != 0
	}

	check := matcher.checks[nibble]
	return check != nil && check.Match(*data)
}

func (matcher *boundaryMatcher) matchSuffix(data *[32]byte) bool {
	// Up to three suffix symbols depend on at most eleven public-key bits.
	// The table merges alternatives before hashing, including complete coverage.
	index := int(binary.BigEndian.Uint16(data[30:])) & (len(matcher.suffixes) - 1)

	accepted := matcher.suffixes[index]
	if accepted == 0 || accepted == 0xffff {
		return accepted != 0
	}

	checksum := onion.Checksum(data)
	return accepted&(uint16(1)<<(checksum[0]>>4)) != 0
}

func compilePatterns(patterns []string, vector bool) (*Matcher, error) {
	body := make([]parsedPattern, 0, len(patterns))
	groups := make([]boundaryGroup, 0, len(patterns))

	seen := make(map[string]bool, len(patterns))
	groupIndex := make(map[bitPattern]int, len(patterns))

	shortSuffix := true
	suffixLength := 0

	for index, text := range patterns {
		if seen[text] {
			continue
		}

		seen[text] = true

		parsed, err := parsePattern(text)
		if err != nil {
			return nil, fmt.Errorf("pattern %d %q: %w", index, text, err)
		}

		public, checksum, condition, valid := publicPattern(parsed)
		if !valid {
			return nil, fmt.Errorf("pattern %d %q cannot match the first 52 hostname characters", index, text)
		}

		shortSuffix = shortSuffix && parsed.anchored && parsed.prefix == "" && len(parsed.suffix) <= 3
		suffixLength = max(suffixLength, len(parsed.suffix))

		if checksum < 0 {
			body = append(body, public)

			continue
		}

		if !parsed.anchored && parsed.first < parsed.last {
			interior := parsed
			interior.last--
			body = append(body, interior)
		}

		position, exists := groupIndex[condition]
		if !exists {
			position = len(groups)
			groupIndex[condition] = position
			groups = append(groups, boundaryGroup{pattern: public})
		}

		groups[position].checksums |= uint16(1) << uint(checksum)
	}

	if len(groups) == 0 {
		return compilePublicPatterns(body, vector), nil
	}

	boundary := new(boundaryMatcher)

	public := make([]parsedPattern, 0, len(body)+len(groups))
	public = append(public, body...)

	tails := make([]parsedPattern, 0, len(groups))

	var checks [16][]parsedPattern

	for _, group := range groups {
		public = append(public, group.pattern)

		if group.checksums == 0xffff {
			body = append(body, group.pattern)

			continue
		}

		tails = append(tails, group.pattern)

		for nibble := range checks {
			if group.checksums&(uint16(1)<<nibble) != 0 {
				checks[nibble] = append(checks[nibble], group.pattern)
			}
		}
	}

	if len(tails) == 0 {
		return compilePublicPatterns(body, vector), nil
	}

	matcher := &Matcher{kind: matcherBoundary, boundary: boundary}
	boundary.filter = compilePublicPatterns(tails, vector)

	if len(body) != 0 {
		boundary.body = compilePublicPatterns(body, vector)
	}

	if len(groups) == 1 {
		boundary.checksums = groups[0].checksums
	} else {
		for nibble := range checks {
			if len(checks[nibble]) != 0 {
				boundary.checks[nibble] = compilePublicPatterns(checks[nibble], vector)
			}
		}
	}

	filter := compilePublicPatterns(public, vector)

	matcher.frequent = filter.frequent
	matcher.independent = filter.independent
	matcher.signDependent = true

	if !matcher.frequent {
		matcher.signFilter = compileMatcher(public, vector, true)
	}

	if shortSuffix {
		matcher.kind = matcherSuffix
		boundary.suffixes = compileSuffixTable(groups, suffixLength)
	}

	return matcher, nil
}

// publicPattern splits an occurrence at character 52 from its checksum nibble.
// All ordinary occurrences remain in the raw matcher with that endpoint excluded.
func publicPattern(parsed parsedPattern) (parsedPattern, int, bitPattern, bool) {
	var condition bitPattern

	checksum := -1
	public := parsed

	if !parsed.anchored {
		if parsed.first > parsed.last {
			return public, checksum, condition, false
		}

		if parsed.last+len(parsed.literal) < encodedSize {
			valid := addLiteral(&condition, parsed.literal, parsed.first)
			return public, checksum, condition, valid
		}

		public = parsedPattern{suffix: parsed.literal, anchored: true}
	}

	if len(public.prefix) > encodedSize || len(public.suffix) > encodedSize {
		return public, checksum, condition, false
	}

	if len(public.prefix) == encodedSize {
		checksum = int(symbolValue(public.prefix[encodedSize-1]) & 15)
		public.prefix = paddedLiteral(public.prefix)
	}

	if public.suffix != "" {
		value := int(symbolValue(public.suffix[len(public.suffix)-1]) & 15)
		if checksum >= 0 && checksum != value {
			return public, checksum, condition, false
		}

		checksum = value
		public.suffix = paddedLiteral(public.suffix)
	}

	valid := addLiteral(&condition, public.prefix, 0) && addLiteral(&condition, public.suffix, encodedSize-len(public.suffix))
	if valid && checksum >= 0 && condition.mask == [4]uint64{^uint64(0), ^uint64(0), ^uint64(0), ^uint64(0)} {
		var data [32]byte

		for index, value := range condition.value {
			binary.LittleEndian.PutUint64(data[index*8:], value)
		}

		actual := onion.Checksum(&data)
		valid = int(actual[0]>>4) == checksum
		checksum = -1
	}

	return public, checksum, condition, valid
}

func paddedLiteral(literal string) string {
	last := symbolValue(literal[len(literal)-1])
	if last&15 == 0 {
		return literal
	}

	buffer := []byte(literal)
	buffer[len(buffer)-1] = 'a' + last&16

	return string(buffer)
}

func compileSuffixTable(groups []boundaryGroup, length int) []uint16 {
	entries := 1 << (length*5 - 4)
	table := make([]uint16, entries)

	for _, group := range groups {
		var condition bitPattern

		addLiteral(&condition, group.pattern.suffix, encodedSize-len(group.pattern.suffix))

		// Convert the little-endian raw-word constraint to the big-endian tail index.
		mask := int(bits.ReverseBytes16(uint16(condition.mask[3] >> 48)))
		value := int(bits.ReverseBytes16(uint16(condition.value[3] >> 48)))

		for index := range table {
			if index&mask == value {
				table[index] |= group.checksums
			}
		}
	}

	return table
}
