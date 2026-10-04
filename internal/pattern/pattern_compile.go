package pattern

import (
	"fmt"
	"math/bits"
	"strings"
)

type parsedPattern struct {
	prefix   string
	suffix   string
	literal  string
	first    int
	last     int
	anchored bool
}

func (matcher *Matcher) addProbe(pattern bitPattern) {
	wordIndex := 0

	for index := 1; index < len(pattern.mask); index++ {
		if bits.OnesCount64(pattern.mask[index]) > bits.OnesCount64(pattern.mask[wordIndex]) {
			wordIndex = index
		}
	}

	table := &matcher.tables[wordIndex]

	probe := wordProbe{
		mask:  pattern.mask[wordIndex],
		value: pattern.value[wordIndex],
	}

	table.probes = append(table.probes, probe)

	pattern.mask[wordIndex] = 0
	pattern.value[wordIndex] = 0

	table.checks = append(table.checks, pattern)
}

func (matcher *Matcher) finish() {
	probeCount := 0

	for wordIndex := range matcher.tables {
		table := &matcher.tables[wordIndex]

		probeCount += len(table.probes)
		needsChecks := false

		for index := range table.checks {
			if table.checks[index].mask != [4]uint64{} {
				needsChecks = true

				break
			}
		}

		if !needsChecks {
			table.checks = nil
		}
	}

	if matcher.dictionary != nil {
		if probeCount == 0 && len(matcher.scans) == 0 && len(matcher.characters) == 0 {
			matcher.kind = matcherDictionary
		}

		return
	}

	if len(matcher.scans) != 0 || len(matcher.characters) != 0 {
		if probeCount == 0 && len(matcher.characters) == 0 {
			matcher.kind = matcherScans
		} else if probeCount == 0 && len(matcher.scans) == 0 {
			matcher.kind = matcherCharacters
		}

		return
	}

	if probeCount == 0 {
		matcher.kind = matcherEmpty

		return
	}

	if probeCount != 1 {
		return
	}

	for wordIndex := range matcher.tables {
		table := &matcher.tables[wordIndex]
		if len(table.probes) == 1 {
			matcher.kind = matcherSingle
			matcher.single = table.probes[0]
			matcher.offset = uint8(wordIndex * 8)

			if len(table.checks) != 0 {
				matcher.kind = matcherOneWide
				matcher.rest = &table.checks[0]
			}

			matcher.tables = [4]probeTable{}

			return
		}
	}
}

func (matcher *Matcher) addPattern(parsed parsedPattern, vector bool, ignoreSign bool) bool {
	if parsed.anchored {
		var pattern bitPattern

		if !addLiteral(&pattern, parsed.prefix, 0) || !addLiteral(&pattern, parsed.suffix, encodedSize-len(parsed.suffix)) {
			return false
		}

		matcher.prepareSign(&pattern, ignoreSign)
		matcher.addProbe(pattern)

		matcher.frequent = matcher.frequent || len(parsed.prefix)+len(parsed.suffix) <= 1

		return true
	}

	if len(parsed.literal) == 1 {
		matcher.frequent = true
		matcher.signDependent = true
		value := symbolValue(parsed.literal[0])

		search := characterSearch{
			value: uint64(value) * characterLowBits,
			first: characterHighBits,
			last:  characterHighBits & 0xfffff,
		}

		if parsed.first != 0 {
			search.first &^= uint64(1) << 39
			search.last &^= uint64(1) << 4
		}

		matcher.characters = append(matcher.characters, search)

		return true
	}

	var (
		plan     scanPlan
		checks   [encodedSize]bitPattern
		possible = false
	)

	// For very few candidate positions, direct word probes beat vector setup.
	vector = vector && parsed.last-parsed.first >= 8

	if vector {
		var value uint32

		for index := range min(3, len(parsed.literal)) {
			value = value<<5 | uint32(symbolValue(parsed.literal[index]))
		}

		plan.mask = 0x7fff7fff

		if len(parsed.literal) == 2 {
			value <<= 5
			plan.mask = 0x7fe07fe0
		}

		plan.value = value | value<<16
	}

	for position := parsed.first; position <= parsed.last; position++ {
		var pattern bitPattern

		if !addLiteral(&pattern, parsed.literal, position) {
			continue
		}

		possible = true
		matcher.prepareSign(&pattern, ignoreSign)

		// The unknown bit belongs to symbol 49 (zero-based). A triplet filter
		// crossing it must become a masked exact probe, not an exact triplet.
		crossesSign := position <= 49 && position+min(3, len(parsed.literal)) > 49
		if !vector || ignoreSign && crossesSign {
			matcher.addProbe(pattern)

			continue
		}

		checks[position] = pattern

		// AVX2's lane-local packs permute position bits 2,3,4 into 4,2,3.
		packed := position&0x23 | (position&0x18)>>1 | (position&0x04)<<2
		plan.positions |= uint64(1) << packed
	}

	if possible && vector {
		matcher.scans = append(matcher.scans, plan)

		// The filter covers the entire literal at lengths two and three.
		if len(parsed.literal) <= 3 {
			matcher.scanChecks = append(matcher.scanChecks, nil)
		} else {
			matcher.scanChecks = append(matcher.scanChecks, new(checks))
		}
	}

	return possible
}

func (matcher *Matcher) prepareSign(pattern *bitPattern, ignoreSign bool) {
	matcher.signDependent = matcher.signDependent || pattern.mask[3]&(uint64(1)<<63) != 0

	if ignoreSign {
		pattern.mask[3] &^= uint64(1) << 63
		pattern.value[3] &^= uint64(1) << 63
	}
}

// CompilePatterns accepts lowercase a-z, digits 2-7, and these dot forms:
// "text." (prefix), ".text" (suffix), "pre.suf" (both), ".text."
// (strictly interior), and "text" (anywhere). Prefix and suffix may overlap
// when their bits agree. Every supplied pattern must have at least one possible
// 32-byte input; malformed or impossible patterns return an error and no matcher.
// Empty pattern lists match nothing; empty patterns are invalid.
func CompilePatterns(patterns []string) (*Matcher, error) {
	return compilePatterns(patterns, vectorAvailable)
}

func compilePatterns(patterns []string, vector bool) (*Matcher, error) {
	matcher, err := compileMatcher(patterns, vector, false)
	if err != nil || matcher.frequent {
		return matcher, err
	}

	if !matcher.signDependent {
		matcher.signFilter = matcher

		return matcher, nil
	}

	if matcher.kind == matcherDictionary {
		filter := *matcher
		filter.ignoreSign = true

		matcher.signFilter = &filter

		return matcher, nil
	}

	matcher.signFilter, err = compileMatcher(patterns, vector, true)

	return matcher, err
}

func compileMatcher(patterns []string, vector bool, ignoreSign bool) (*Matcher, error) {
	matcher := &Matcher{kind: matcherGeneral}

	seen := make(map[string]struct{}, len(patterns))
	parsedPatterns := make([]parsedPattern, 0, len(patterns))

	eligible := 0

	for index, text := range patterns {
		if _, exists := seen[text]; exists {
			continue
		}

		seen[text] = struct{}{}

		parsed, err := parsePattern(text)
		if err != nil {
			return nil, fmt.Errorf("pattern %d %q: %w", index, text, err)
		}

		var validation bitPattern

		possible := false

		if parsed.anchored {
			possible = addLiteral(&validation, parsed.prefix, 0) && addLiteral(&validation, parsed.suffix, encodedSize-len(parsed.suffix))
		} else if parsed.first <= parsed.last {
			possible = addLiteral(&validation, parsed.literal, parsed.first)
		}

		if !possible {
			return nil, fmt.Errorf("pattern %d %q cannot match a 32-byte base32 value", index, text)
		}

		parsedPatterns = append(parsedPatterns, parsed)

		if !parsed.anchored && len(parsed.literal) >= 3 && parsed.last-parsed.first >= 8 {
			eligible++
		}
	}

	if eligible >= dictionaryThreshold {
		matcher.dictionary = compileDictionary(parsedPatterns)
		matcher.ignoreSign = ignoreSign
		matcher.signDependent = true
	}

	for _, parsed := range parsedPatterns {
		if matcher.dictionary == nil || len(anchorLiteral(parsed)) < 3 {
			matcher.addPattern(parsed, vector, ignoreSign)
		}
	}

	matcher.finish()

	return matcher, nil
}

func parsePattern(text string) (parsedPattern, error) {
	if text == "" {
		return parsedPattern{}, fmt.Errorf("empty pattern")
	}

	for index := range len(text) {
		character := text[index]
		if character != '.' && (character < 'a' || character > 'z') && (character < '2' || character > '7') {
			return parsedPattern{}, fmt.Errorf("invalid character at byte %d; expected lowercase a-z, 2-7, or a dot", index)
		}
	}

	dots := strings.Count(text, ".")
	if dots == 0 {
		return parsedPattern{literal: text, last: encodedSize - len(text)}, nil
	}

	if dots == 2 && text[0] == '.' && text[len(text)-1] == '.' && len(text) > 2 {
		literal := text[1 : len(text)-1]
		return parsedPattern{literal: literal, first: 1, last: encodedSize - len(literal) - 1}, nil
	}

	if dots != 1 || len(text) == 1 {
		return parsedPattern{}, fmt.Errorf("expected one of: text, text., .text, pre.suf, .text. (nonempty literals)")
	}

	prefix, suffix, _ := strings.Cut(text, ".")
	return parsedPattern{prefix: prefix, suffix: suffix, anchored: true}, nil
}

func addLiteral(pattern *bitPattern, literal string, position int) bool {
	if position < 0 || position+len(literal) > encodedSize {
		return false
	}

	for index := range len(literal) {
		value := symbolValue(literal[index])

		for digit := range 5 {
			inputBit := (position+index)*5 + digit
			set := value&(1<<(4-digit)) != 0

			if inputBit >= 256 {
				if set {
					return false
				}

				continue
			}

			wordIndex := inputBit / 64
			shift := (inputBit/8%8)*8 + 7 - inputBit%8

			mask := uint64(1) << shift
			if pattern.mask[wordIndex]&mask != 0 && (pattern.value[wordIndex]&mask != 0) != set {
				return false
			}

			pattern.mask[wordIndex] |= mask

			if set {
				pattern.value[wordIndex] |= mask
			}
		}
	}

	return true
}

func symbolValue(character byte) byte {
	if character >= '2' && character <= '7' {
		return character - '2' + 26
	}

	return character - 'a'
}
