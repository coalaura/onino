package pattern

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestAnchoredDictionary(t *testing.T) {
	random := rand.New(rand.NewPCG(15, 52))
	background := anchoredPatterns(128)
	lengths := []int{1, 2, 3, 4, 10, 49, 50, 51, 52}

	for range 30 {
		data := randomInput(random)
		encoded := visibleEncoding(data[:])

		for _, length := range lengths {
			forms := []string{encoded[:length] + ".", "." + encoded[encodedSize-length:], encoded[:length] + "." + encoded[length-1:]}

			for _, text := range forms {
				patterns := append(slices.Clip(background), text)

				matcher, err := CompilePatterns(patterns)
				if err != nil {
					t.Fatal(err)
				}

				indexed := matcher.anchors != nil || matcher.boundary != nil && (matcher.boundary.filter.anchors != nil || matcher.boundary.body != nil && matcher.boundary.body.anchors != nil)
				if !indexed {
					t.Fatal("anchored index not selected")
				}

				for bit := range 256 {
					changed := data
					changed[bit/8] ^= 1 << (bit % 8)
					checkDictionary(t, matcher, patterns, changed)
				}

				checkDictionary(t, matcher, patterns, data)
			}
		}
	}
}

func FuzzAnchoredDictionary(f *testing.F) {
	f.Add(make([]byte, 32), uint8(49))
	f.Add([]byte("abcdefghijklmnopqrstuvwxyz012345"), uint8(3))

	background := anchoredPatterns(128)

	f.Fuzz(func(t *testing.T, input []byte, length uint8) {
		if len(input) != 32 {
			return
		}

		data := [32]byte(input)
		text := visibleEncoding(data[:])

		position := int(length)%encodedSize + 1
		patterns := append(slices.Clip(background), text[:position]+"."+text[position-1:])

		matcher, err := CompilePatterns(patterns)
		if err != nil {
			t.Fatal(err)
		}

		checkDictionary(t, matcher, patterns, data)
		data[31] ^= 128
		checkDictionary(t, matcher, patterns, data)
		data[position%32] ^= 255
		checkDictionary(t, matcher, patterns, data)
	})
}

func anchoredPatterns(count int) []string {
	patterns := dictionaryPatterns(count)

	for index, text := range patterns {
		switch index % 3 {
		case 0:
			patterns[index] = "aaa" + text + "."
		case 1:
			patterns[index] = "." + text + "aaaa"
		case 2:
			patterns[index] = text[:5] + "." + text[5:] + "a"
		}
	}

	return patterns
}
