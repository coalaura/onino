package pattern

import (
	"math/rand/v2"
	"testing"
)

func TestSignFilter(t *testing.T) {
	random := rand.New(rand.NewPCG(47, 50))

	for sample := range 32 {
		data := randomInput(random)
		data[31] = data[31]&0x7f | byte(sample%2)<<7

		encoded := visibleEncoding(data[:])

		patterns := []string{
			encoded[:49] + ".",
			encoded[:50] + ".",
			encoded[:51] + ".",
			encoded + ".",
			"." + encoded[48:],
			"." + encoded[47:51] + ".",
			encoded[47:51],
			encoded[48:51],
			encoded[49:51],
			encoded[:50] + "." + encoded[48:],
			encoded[:2] + "." + encoded[50:],
		}

		for _, literal := range patterns {
			checkSignFilter(t, []string{literal}, data)
			checkSignFilter(t, []string{"zzzzzzzz.", literal, ".aaaa."}, data)
		}
	}
}

func FuzzSignFilter(f *testing.F) {
	f.Add([]byte("abcdefghijklmnopqrstuvwxyz012345"), uint8(47), uint8(51))
	f.Add(make([]byte, 32), uint8(49), uint8(52))

	f.Fuzz(func(t *testing.T, input []byte, first, last uint8) {
		if len(input) != 32 {
			return
		}

		data := [32]byte(input)
		encoded := visibleEncoding(data[:])

		start := int(first) % 51
		end := start + 2 + int(last)%(51-start)

		literal := encoded[start:end]
		patterns := []string{literal, encoded[:end] + ".", "." + encoded[start:]}

		checkSignFilter(t, patterns, data)
	})
}

func checkSignFilter(t *testing.T, patterns []string, data [32]byte) {
	t.Helper()

	for _, matcher := range compileImplementations(t, patterns) {
		filter := matcher.SignFilter()
		if filter == nil {
			t.Fatal("expected a sign filter")
		}

		for position := range 257 {
			changed := data

			if position < 256 {
				changed[position/8] ^= 1 << (position % 8)
			}

			partial := changed
			partial[31] &= 0x7f

			want := referenceFilter(patterns, partial)
			if filter.Match(partial) != want {
				t.Fatalf("filter differs from union of signs: %q, %x", patterns, changed)
			}
		}
	}
}
