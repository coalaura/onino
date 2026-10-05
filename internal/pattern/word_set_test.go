package pattern

import (
	"math/rand/v2"
	"strconv"
	"testing"
)

func TestWordSetsAgainstStrings(t *testing.T) {
	random := rand.New(rand.NewPCG(731, 9950))

	counts := []int{2, 3, 4, 8, 16, 32, 63, 64}
	forms := []string{"prefix", "shared", "mixed_lengths", "suffix", "mixed_suffix", "cross_word", "combined", "scans", "characters", "dictionary"}

	for _, count := range counts {
		for _, form := range forms {
			t.Run(form+"/"+strconv.Itoa(count), func(t *testing.T) {
				inputs := make([][32]byte, count)
				patterns := make([]string, 0, count+2)

				for index := range inputs {
					inputs[index] = randomInput(random)

					if form == "shared" {
						copy(inputs[index][:2], inputs[0][:2])
					}

					text := visibleEncoding(inputs[index][:])
					literal := text[:6] + "."

					switch form {
					case "mixed_lengths":
						literal = text[:3+index%10] + "."
					case "suffix":
						literal = "." + text[46:]
					case "mixed_suffix":
						literal = "." + text[40+index%10:]
					case "cross_word":
						literal = text[:12+index%3] + "."
					case "combined":
						literal = text[:6] + "." + text[46:]
					case "dictionary":
						literal = text[10:20]
					}

					patterns = append(patterns, literal)
				}

				patterns = append(patterns, patterns[0])

				switch form {
				case "scans":
					patterns = append(patterns, "xyz")
				case "characters":
					patterns = append(patterns, "z")
				case "dictionary":
					patterns = append(patterns, "donate.", "mirror.")
				}

				for _, matcher := range compileImplementations(t, patterns) {
					for _, data := range inputs {
						if !matcher.Match(data) {
							t.Fatal("constructed positive did not match")
						}

						checkDictionary(t, matcher, patterns, data)

						for bit := range 256 {
							changed := data
							changed[bit/8] ^= 1 << (bit % 8)
							checkDictionary(t, matcher, patterns, changed)
						}
					}

					for range 256 {
						checkDictionary(t, matcher, patterns, randomInput(random))
					}

					allocations := testing.AllocsPerRun(1000, func() {
						matcher.Match(inputs[0])
					})

					if allocations != 0 {
						t.Fatalf("Match allocates %g times", allocations)
					}
				}
			})
		}
	}
}

// Internal bounded occurrences also reach finish's tables at nonzero word offsets.
func TestWordSetOffsets(t *testing.T) {
	random := rand.New(rand.NewPCG(734, 64))

	positions := []int{0, 13, 26, 39}

	for _, position := range positions {
		inputs := make([][32]byte, 4)
		parsed := make([]parsedPattern, len(inputs))

		for index := range inputs {
			inputs[index] = randomInput(random)

			text := testEncoding.EncodeToString(inputs[index][:])

			parsed[index] = parsedPattern{literal: text[position : position+6+index], first: position, last: position}
		}

		matcher := compileMatcher(parsed, false, false)
		if matcher.kind != matcherSingleWordSet || int(matcher.offset) != position*5/64*8 {
			t.Fatalf("position %d did not exercise a word set: kind=%d offset=%d", position, matcher.kind, matcher.offset)
		}

		for sample := range 1024 {
			data := inputs[sample%len(inputs)]

			if sample >= len(inputs) {
				data = randomInput(random)
			}

			text := testEncoding.EncodeToString(data[:])

			var want bool

			for _, pattern := range parsed {
				want = want || text[position:position+len(pattern.literal)] == pattern.literal
			}

			if matcher.Match(data) != want {
				t.Fatalf("word offset %d differs from base32 on %x", position, data)
			}
		}
	}
}
