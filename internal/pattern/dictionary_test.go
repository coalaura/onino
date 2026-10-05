package pattern

import (
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type dictionaryBenchmarkCase struct {
	name string
	text string
}

var dictionaryBenchmarkHit bool

func TestDictionaryCrossover(t *testing.T) {
	random := rand.New(rand.NewPCG(32, 128))
	sizes := []int{31, 32, 33, 63, 64}

	for _, size := range sizes {
		background := dictionaryPatterns(size)

		for range 40 {
			data := randomInput(random)
			encoded := visibleEncoding(data[:])

			alternatives := []string{encoded[7:17], encoded[:3] + ".", "." + encoded[49:], "." + encoded[12:14] + ".", encoded[:4] + "." + encoded[47:]}

			for _, alternative := range alternatives {
				patterns := append(slices.Clip(background), alternative)

				matcher, err := CompilePatterns(patterns)
				if err != nil {
					t.Fatal(err)
				}

				checkDictionary(t, matcher, patterns, data)
				data[31] ^= 128
				checkDictionary(t, matcher, patterns, data)
				data[31] ^= 128
				checkDictionary(t, matcher, patterns, randomInput(random))
			}
		}
	}
}

func TestDictionaryAlignments(t *testing.T) {
	random := rand.New(rand.NewPCG(29, 255))
	background := dictionaryPatterns(128)
	lengths := []int{2, 3, 4, 5, 6, 7, 10, 49, 50, 51, 52}

	for _, length := range lengths {
		for start := 0; start+length <= encodedSize; start++ {
			data := randomInput(random)
			encoded := visibleEncoding(data[:])
			literal := encoded[start : start+length]

			forms := []string{literal, encoded[:start+length] + ".", "." + encoded[start:], encoded[:start+length] + "." + encoded[start:]}

			if start > 0 && start+length < encodedSize {
				forms = append(forms, "."+literal+".")
			}

			for _, text := range forms {
				patterns := append(slices.Clip(background), text)

				matcher, err := CompilePatterns(patterns)
				if err != nil {
					t.Fatal(err)
				}

				checkDictionary(t, matcher, patterns, data)
				data[31] ^= 128
				checkDictionary(t, matcher, patterns, data)
				data[31] ^= 128
			}
		}
	}
}

func TestDictionaryDifferential(t *testing.T) {
	random := rand.New(rand.NewPCG(512, 50))

	patterns := dictionaryPatterns(512)

	for range 100 {
		data := randomInput(random)

		encoded := visibleEncoding(data[:])

		additional := []string{
			encoded[:49] + ".",
			encoded[:50] + ".",
			encoded[:51] + ".",
			encoded + ".",
			"." + encoded[47:],
			"." + encoded[47:51] + ".",
			encoded[49:],
			encoded[:40] + "." + encoded[30:],
			"aaa" + encoded[10:20],
		}

		for _, literal := range additional {
			list := append(slices.Clip(patterns), literal)

			matcher, err := CompilePatterns(list)
			if err != nil {
				t.Fatal(err)
			}

			for mutation := range 32 {
				changed := data

				if mutation != 0 {
					changed[mutation] ^= byte(mutation * 7)
				}

				checkDictionary(t, matcher, list, changed)
			}
		}
	}

	// Many verifiers share one triplet. Reject the early alternatives, then
	// continue to a later occurrence and the final verifier that actually matches.
	shared := make([]string, 0, 512)

	for _, text := range patterns {
		shared = append(shared, "aaa"+text)
	}

	shared = append(shared, strings.Repeat("a", 30))

	matcher, err := CompilePatterns(shared)
	if err != nil {
		t.Fatal(err)
	}

	checkDictionary(t, matcher, shared, [32]byte{})

	// Mixed small alternatives keep their original character/probe fast paths.
	shared = append(shared, ".q", "bc", ".de.")

	matcher, err = CompilePatterns(shared)
	if err != nil {
		t.Fatal(err)
	}

	for range 1000 {
		checkDictionary(t, matcher, shared, randomInput(random))
	}
}

func TestDictionaryFingerprintCollisions(t *testing.T) {
	patterns := dictionaryPatterns(64)

	literals := []string{"aaabbb", "cccaaa", "aaaccc", "bbbaaa"}

	patterns = append(patterns, literals...)
	patterns = append(patterns, "aaabbb.", ".cccaaaq", "aaabbb.cccaaaq", "xyz", ".bc.")

	matcher, err := CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	// Shared triplets with different preceding/following symbols must not
	// turn the union fingerprint into an accepting condition. Check every
	// alignment, mutations outside the triplet, and both deferred signs.
	for _, literal := range literals {
		for start := 0; start+len(literal) < encodedSize; start++ {
			text := strings.Repeat("z", start) + literal + strings.Repeat("z", encodedSize-start-len(literal)-1) + "q"

			decoded, err := testEncoding.DecodeString(text)
			if err != nil {
				t.Fatal(err)
			}

			data := [32]byte(decoded)

			checkDictionary(t, matcher, patterns, data)

			for index := range data {
				changed := data
				changed[index] ^= 0xff

				checkDictionary(t, matcher, patterns, changed)
			}
		}
	}
}

func FuzzDictionary(f *testing.F) {
	f.Add(make([]byte, 32), uint8(49), uint8(3))
	f.Add([]byte("abcdefghijklmnopqrstuvwxyz012345"), uint8(47), uint8(0))

	background := dictionaryPatterns(256)

	f.Fuzz(func(t *testing.T, input []byte, start, form uint8) {
		if len(input) != 32 {
			return
		}

		data := [32]byte(input)
		encoded := visibleEncoding(data[:])

		position := int(start) % 50
		literal := encoded[position:]

		switch form % 4 {
		case 0:
			literal = encoded[:position+3] + "."
		case 1:
			literal = "." + literal
		case 2:
			literal = encoded[:position+3] + "." + literal
		}

		patterns := append(slices.Clip(background), literal)

		matcher, err := CompilePatterns(patterns)
		if err != nil {
			t.Fatal(err)
		}

		checkDictionary(t, matcher, patterns, data)

		for index := range data {
			data[index] ^= byte(index + 17)
		}

		checkDictionary(t, matcher, patterns, data)
	})
}

func BenchmarkDictionary(b *testing.B) {
	sizes := []int{64, 128, 256, 512}

	for _, size := range sizes {
		patterns := dictionaryPatterns(size)

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			b.Run("compile", func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					matcher, err := CompilePatterns(patterns)
					if err != nil {
						b.Fatal(err)
					}

					dictionaryBenchmarkHit = matcher.Match([32]byte{})
				}
			})
		})
	}
}

func BenchmarkDictionaryMatch(b *testing.B) {
	sizes := []int{64, 512}

	for _, size := range sizes {
		patterns := dictionaryPatterns(size)
		// Derive an adversarial candidate from the selected anchor, independently
		// of the backend selected for the benchmark itself.
		parsed := make([]parsedPattern, len(patterns))

		for index, literal := range patterns {
			parsed[index] = parsedPattern{literal: literal, last: encodedSize - len(literal)}
		}

		dictionary := compileDictionary(parsed)
		check := dictionary.checks[0]
		anchor := check.pattern.literal[check.offset : check.offset+3]

		cases := []dictionaryBenchmarkCase{
			{name: "miss", text: strings.Repeat("z", 51) + "a"},
			{name: "early_hit", text: patterns[0] + strings.Repeat("a", 42)},
			{name: "false_positives", text: strings.Repeat(anchor, 17) + "a"},
			{name: "late_hit", text: strings.Repeat(anchor, 10) + patterns[size-1] + strings.Repeat("a", 12)},
		}

		b.Run(strconv.Itoa(size), func(b *testing.B) {
			for _, test := range cases {
				b.Run(test.name, func(b *testing.B) {
					matcher, err := CompilePatterns(patterns)
					if err != nil {
						b.Fatal(err)
					}

					decoded, err := testEncoding.DecodeString(test.text)
					if err != nil {
						b.Fatal(err)
					}

					data := [32]byte(decoded)
					want := referenceMatch(patterns, test.text)
					hits := 0

					b.ReportAllocs()

					for b.Loop() {
						if matcher.Match(data) {
							hits++
						}
					}

					dictionaryBenchmarkHit = hits != 0

					if want && hits != b.N || !want && hits != 0 {
						b.Fatalf("incorrect hit count: %d, want match %v", hits, want)
					}
				})
			}
		})
	}
}

func BenchmarkCompileCrossover(b *testing.B) {
	forms := []string{"ordinary", "shared", "short", "mixed"}
	sizes := []int{16, 32, 64, 128}

	for _, form := range forms {
		for _, size := range sizes {
			patterns := dictionaryPatterns(size)

			if form == "shared" {
				for index := range patterns {
					patterns[index] = "aaa" + patterns[index][:7]
				}
			}

			switch form {
			case "short":
				patterns = append(patterns, "xyz", ".bc.", "ab.")
			case "mixed":
				patterns = append(patterns, "zzzzzz.", ".qqqqqqqa", "abcd.wxyza")
			}

			b.Run(form+"/"+strconv.Itoa(size), func(b *testing.B) {
				b.ReportAllocs()

				for b.Loop() {
					matcher, err := CompilePatterns(patterns)
					if err != nil {
						b.Fatal(err)
					}

					dictionaryBenchmarkHit = matcher.Match([32]byte{})
				}
			})
		}
	}
}

func checkDictionary(t *testing.T, matcher *Matcher, patterns []string, data [32]byte) {
	t.Helper()

	text := visibleEncoding(data[:])
	if matcher.Match(data) != referenceMatch(patterns, text) {
		t.Fatalf("dictionary differs from base32 reference on %x", data)
	}

	filter := matcher.SignFilter()
	if filter == nil {
		return
	}

	data[31] &= 0x7f

	want := referenceFilter(patterns, data)
	if filter.Match(data) != want {
		t.Fatalf("dictionary sign filter differs from union on %x", data)
	}
}

func dictionaryPatterns(count int) []string {
	random := rand.New(rand.NewPCG(63, 511))
	patterns := make([]string, count)

	for index := range patterns {
		data := randomInput(random)
		patterns[index] = visibleEncoding(data[:])[:10]
	}

	return patterns
}
