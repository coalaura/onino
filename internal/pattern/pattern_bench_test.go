package pattern

import (
	"bytes"
	"math/rand/v2"
	"strconv"
	"testing"
)

type matcherBenchmark struct {
	name     string
	patterns []string
}

type encodedPattern struct {
	prefix   []byte
	suffix   []byte
	literal  []byte
	interior bool
}

func BenchmarkMatcher(bench *testing.B) {
	cases := []matcherBenchmark{
		{name: "prefix", patterns: []string{"start."}},
		{name: "suffix", patterns: []string{".enda"}},
		{name: "prefix_suffix", patterns: []string{"start.enda"}},
		{name: "one_character", patterns: []string{"b"}},
		{name: "two_characters", patterns: []string{"ab"}},
		{name: "short", patterns: []string{"abc"}},
		{name: "anywhere", patterns: []string{"middle"}},
		{name: "interior", patterns: []string{".middle."}},
		{name: "long", patterns: []string{"abcdefghijklmnopqrstuvwx"}},
		{name: "mixed", patterns: []string{"start.enda", ".middle.", "begin.", ".finisha", "anywhere"}},
		{name: "eight_searches", patterns: []string{"hello", "world", "onino", "middle", "search", "pattern", "vanity", "address"}},
	}

	inputs := make([][32]byte, 256)

	random := rand.New(rand.NewPCG(9, 10))

	for index := range inputs {
		inputs[index] = randomInput(random)
	}

	for _, test := range cases {
		bench.Run(test.name, func(bench *testing.B) {
			bench.Run("compiled", func(bench *testing.B) {
				matcher, err := CompilePatterns(test.patterns)
				if err != nil {
					bench.Fatal(err)
				}

				benchmarkMatches(bench, matcher, inputs)
			})

			bench.Run("scalar", func(bench *testing.B) {
				matcher, err := compilePatterns(test.patterns, false)
				if err != nil {
					bench.Fatal(err)
				}

				benchmarkMatches(bench, matcher, inputs)
			})

			bench.Run("encode_then_search", func(bench *testing.B) {
				patterns := make([]encodedPattern, len(test.patterns))

				for index, text := range test.patterns {
					parsed, err := parsePattern(text)
					if err != nil {
						bench.Fatal(err)
					}

					patterns[index] = encodedPattern{
						prefix:   []byte(parsed.prefix),
						suffix:   []byte(parsed.suffix),
						literal:  []byte(parsed.literal),
						interior: parsed.first != 0,
					}
				}

				var (
					encoded [encodedSize]byte
					index   = 0
				)

				bench.ReportAllocs()
				bench.SetBytes(32)

				for bench.Loop() {
					testEncoding.Encode(encoded[:], inputs[index&255][:])
					matchEncoded(patterns, &encoded)
					index++
				}
			})
		})
	}
}

func BenchmarkMatcherHits(bench *testing.B) {
	cases := []matcherBenchmark{
		{name: "prefix", patterns: []string{"start."}},
		{name: "suffix", patterns: []string{".enda"}},
		{name: "prefix_suffix", patterns: []string{"start.enda"}},
		{name: "one_character", patterns: []string{"b"}},
		{name: "two_characters", patterns: []string{"ab"}},
		{name: "three_characters", patterns: []string{"abc"}},
		{name: "anywhere", patterns: []string{"middlea"}},
		{name: "interior", patterns: []string{".middlea."}},
		{name: "long", patterns: []string{"abcdefghijklmnopqrsta"}},
		{name: "eight_searches", patterns: []string{"hello", "world", "onino", "middle", "search", "pattern", "vanity", "addressa"}},
	}

	for _, test := range cases {
		bench.Run(test.name, func(bench *testing.B) {
			matcher, err := CompilePatterns(test.patterns)
			if err != nil {
				bench.Fatal(err)
			}

			inputs := matchingInputs(bench, test.patterns[len(test.patterns)-1])
			benchmarkMatches(bench, matcher, inputs)
		})
	}
}

func BenchmarkMatcherPatternCount(bench *testing.B) {
	counts := []int{1, 8, 64, 512}

	random := rand.New(rand.NewPCG(13, 14))

	inputs := make([][32]byte, 256)

	for index := range inputs {
		inputs[index] = randomInput(random)
	}

	for _, count := range counts {
		bench.Run(strconv.Itoa(count), func(bench *testing.B) {
			patterns := make([]string, count)

			for index := range patterns {
				data := randomInput(random)
				patterns[index] = visibleEncoding(data[:])[:6]
			}

			matcher, err := CompilePatterns(patterns)
			if err != nil {
				bench.Fatal(err)
			}

			benchmarkMatches(bench, matcher, inputs)
		})
	}
}

func matchEncoded(patterns []encodedPattern, encoded *[encodedSize]byte) bool {
	for index := range patterns {
		pattern := &patterns[index]
		if len(pattern.literal) == 0 {
			if bytes.HasPrefix(encoded[:], pattern.prefix) && bytes.HasSuffix(encoded[:], pattern.suffix) {
				return true
			}

			continue
		}

		text := encoded[:]

		if pattern.interior {
			text = text[1 : len(text)-1]
		}

		if bytes.Contains(text, pattern.literal) {
			return true
		}
	}

	return false
}

func benchmarkMatches(bench *testing.B, matcher *Matcher, inputs [][32]byte) {
	bench.Helper()
	bench.ReportAllocs()
	bench.SetBytes(32)

	index := 0
	mask := len(inputs) - 1

	for bench.Loop() {
		matcher.Match(inputs[index&mask])

		index++
	}
}

func matchingInputs(bench *testing.B, pattern string) [][32]byte {
	bench.Helper()

	parsed, err := parsePattern(pattern)
	if err != nil {
		bench.Fatal(err)
	}

	inputs := make([][32]byte, 256)

	random := rand.New(rand.NewPCG(15, 16))

	for index := range inputs {
		data := randomInput(random)

		encoded := []byte(visibleEncoding(data[:]))

		if parsed.anchored {
			copy(encoded, parsed.prefix)
			copy(encoded[encodedSize-len(parsed.suffix):], parsed.suffix)
		} else {
			last := parsed.last
			if parsed.literal[len(parsed.literal)-1] != 'a' && parsed.literal[len(parsed.literal)-1] != 'q' && last+len(parsed.literal) == encodedSize {
				last--
			}

			position := parsed.first + index%(last-parsed.first+1)
			copy(encoded[position:], parsed.literal)
		}

		decoded, decodeErr := testEncoding.DecodeString(string(encoded))
		if decodeErr != nil {
			bench.Fatal(decodeErr)
		}

		inputs[index] = [32]byte(decoded)
	}

	return inputs
}
