package pattern

import (
	"encoding/base32"
	"math/rand/v2"
	"strings"
	"sync"
	"testing"
)

const testAlphabet = "abcdefghijklmnopqrstuvwxyz234567"

type patternTestCase struct {
	name     string
	patterns []string
	data     [32]byte
	want     bool
}

var testEncoding = base32.NewEncoding(testAlphabet).WithPadding(base32.NoPadding)

func TestPatternSemantics(t *testing.T) {
	data := randomInput(rand.New(rand.NewPCG(1, 2)))

	encoded := testEncoding.EncodeToString(data[:])

	allA := strings.Repeat("a", encodedSize)

	cases := []patternTestCase{
		{name: "empty list", data: data},
		{name: "prefix", patterns: []string{encoded[:8] + "."}, data: data, want: true},
		{name: "suffix", patterns: []string{"." + encoded[44:]}, data: data, want: true},
		{name: "both", patterns: []string{encoded[:8] + "." + encoded[44:]}, data: data, want: true},
		{name: "interior", patterns: []string{"." + encoded[8:16] + "."}, data: data, want: true},
		{name: "anywhere prefix", patterns: []string{encoded[:8]}, data: data, want: true},
		{name: "anywhere suffix", patterns: []string{encoded[44:]}, data: data, want: true},
		{name: "anywhere middle", patterns: []string{encoded[8:16]}, data: data, want: true},
		{name: "interior excludes prefix", patterns: []string{"." + encoded[:16] + "."}, data: data},
		{name: "interior excludes suffix", patterns: []string{"." + encoded[36:] + "."}, data: data},
		{name: "interior later occurrence", patterns: []string{".aaaa."}, want: true},
		{name: "full length", patterns: []string{encoded}, data: data, want: true},
		{name: "full prefix", patterns: []string{encoded + "."}, data: data, want: true},
		{name: "full suffix", patterns: []string{"." + encoded}, data: data, want: true},
		{name: "empty gap", patterns: []string{encoded[:26] + "." + encoded[26:]}, data: data, want: true},
		{name: "overlap", patterns: []string{encoded[:40] + "." + encoded[30:]}, data: data, want: true},
		{name: "full overlap", patterns: []string{encoded + "." + encoded}, data: data, want: true},
		{name: "disjunction", patterns: []string{"zzzzzzzz.", encoded[5:17], "yyyyyyyy."}, data: data, want: true},
		{name: "duplicates", patterns: []string{encoded, encoded}, data: data, want: true},
		{name: "no match", patterns: []string{"zzzzzzzz.", "yyyyyyyy", ".xxxxxq"}, data: data},
		{name: "zero full", patterns: []string{allA}, want: true},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			checkImplementations(t, test.patterns, test.data, test.want)
		})
	}

	var zero Matcher

	if zero.Match(data) {
		t.Fatal("zero-value matcher matched")
	}
}

func TestInvalidPatterns(t *testing.T) {
	patterns := []string{
		"", ".", "..", "...", ".a.b", "a.b.", "a..b", "a.b.c", ".a.b.",
		"A", "0", "1", "8", "9", "=", "a b", "a\x00b", "é", "😀",
		".b", ".finish", "start.end", strings.Repeat("a", 51) + "b",
		strings.Repeat("a", 53), strings.Repeat("a", 53) + ".", "." + strings.Repeat("a", 53),
		"." + strings.Repeat("a", 51) + ".",
		strings.Repeat("a", 40) + "." + strings.Repeat("b", 20) + "a",
	}

	for _, pattern := range patterns {
		matcher, err := CompilePatterns([]string{"valid.", pattern})
		if err == nil || matcher != nil {
			t.Fatalf("CompilePatterns(%q) = %v, %v; want nil matcher and error", pattern, matcher, err)
		}
	}
}

func TestAllBitAlignments(t *testing.T) {
	random := rand.New(rand.NewPCG(3, 4))

	for length := 1; length <= encodedSize; length++ {
		for position := 0; position+length <= encodedSize; position++ {
			data := randomInput(random)

			encoded := testEncoding.EncodeToString(data[:])

			literal := encoded[position : position+length]

			checkImplementations(t, []string{literal}, data, true)

			if position > 0 && position+length < encodedSize {
				checkImplementations(t, []string{"." + literal + "."}, data, true)
			}

			for range 4 {
				changed := data

				changed[random.IntN(len(changed))] ^= byte(1 << random.IntN(8))

				changedEncoding := testEncoding.EncodeToString(changed[:])

				want := strings.Contains(changedEncoding, literal)

				checkImplementations(t, []string{literal}, changed, want)
			}
		}
	}
}

func TestFinalCharacter(t *testing.T) {
	for value := range 256 {
		var data [32]byte

		data[31] = byte(value)
		encoded := testEncoding.EncodeToString(data[:])

		for _, character := range testAlphabet {
			literal := string(character)

			checkImplementations(t, []string{literal}, data, strings.Contains(encoded, literal))
			checkImplementations(t, []string{"." + literal + "."}, data, strings.Contains(encoded[1:51], literal))

			if character == 'a' || character == 'q' {
				checkImplementations(t, []string{"." + literal}, data, strings.HasSuffix(encoded, literal))

				continue
			}

			_, err := CompilePatterns([]string{"." + literal})
			if err == nil {
				t.Fatalf("impossible suffix %q accepted", literal)
			}
		}
	}
}

func TestRandomizedAgainstBase32(t *testing.T) {
	random := rand.New(rand.NewPCG(5, 6))

	for iteration := range 200 {
		seed := randomInput(random)

		encoded := testEncoding.EncodeToString(seed[:])

		patterns := make([]string, 0, 16)

		for index := range 16 {
			first := random.IntN(encodedSize)
			last := first + 1 + random.IntN(encodedSize-first)

			literal := encoded[first:last]

			switch index % 5 {
			case 0:
				patterns = append(patterns, literal)
			case 1:
				patterns = append(patterns, encoded[:last]+".")
			case 2:
				patterns = append(patterns, "."+encoded[first:])
			case 3:
				patterns = append(patterns, encoded[:last]+"."+encoded[first:])
			case 4:
				if len(literal) <= 50 {
					patterns = append(patterns, "."+literal+".")
				}
			}
		}

		compiled := compileImplementations(t, patterns)

		for sample := range 100 {
			data := seed

			if sample != 0 {
				data = randomInput(random)
			}

			text := testEncoding.EncodeToString(data[:])
			want := referenceMatch(patterns, text)

			for _, matcher := range compiled {
				if matcher.Match(data) != want {
					t.Fatalf("iteration %d: %q against %q: want %v", iteration, patterns, text, want)
				}
			}
		}
	}
}

func TestSingleCharacterPositions(t *testing.T) {
	for _, character := range []byte(testAlphabet) {
		background := byte('b')
		if character == background {
			background = 'c'
		}

		last := byte('a')
		if character == last {
			last = 'q'
		}

		for position := range encodedSize {
			if position == encodedSize-1 && character != 'a' && character != 'q' {
				continue
			}

			text := []byte(strings.Repeat(string(background), encodedSize))

			text[encodedSize-1] = last
			text[position] = character

			decoded, err := testEncoding.DecodeString(string(text))
			if err != nil {
				t.Fatal(err)
			}

			data := [32]byte(decoded)
			checkImplementations(t, []string{string(character)}, data, true)
			checkImplementations(t, []string{"." + string(character) + "."}, data, position > 0 && position < encodedSize-1)
		}
	}
}

func TestCandidateVerificationContinues(t *testing.T) {
	text := "abcxyz" + strings.Repeat("a", encodedSize-6)

	decoded, err := testEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}

	data := [32]byte(decoded)

	checkImplementations(t, []string{"abczzz", "abcxzz"}, data, false)
	checkImplementations(t, []string{"abczzz", "abcxzz", "abcxyz"}, data, true)
	checkImplementations(t, []string{"abczzz", "xyz"}, data, true)

	text = "abczzz" + strings.Repeat("a", 23) + "abcxyz" + strings.Repeat("a", 17)

	decoded, err = testEncoding.DecodeString(text)
	if err != nil {
		t.Fatal(err)
	}

	checkImplementations(t, []string{"abcxyz"}, [32]byte(decoded), true)
}

func TestMatcherConcurrent(t *testing.T) {
	patterns := []string{"abc.", ".aq", "onino", ".middle.", "b"}

	matcher, err := CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	inputs := make([][32]byte, 128)
	expected := make([]bool, len(inputs))

	random := rand.New(rand.NewPCG(11, 12))

	for index := range inputs {
		inputs[index] = randomInput(random)
		expected[index] = referenceMatch(patterns, testEncoding.EncodeToString(inputs[index][:]))
	}

	var workers sync.WaitGroup

	for range 16 {
		workers.Go(func() {
			for index := range inputs {
				if matcher.Match(inputs[index]) != expected[index] {
					t.Errorf("concurrent input %d: want %v", index, expected[index])
				}
			}
		})
	}

	workers.Wait()
}

func TestMatcherAllocations(t *testing.T) {
	patterns := []string{"hello.", ".worlda", "onino", ".middle.", "ab.cq"}

	matcher, err := CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	data := randomInput(rand.New(rand.NewPCG(7, 8)))

	allocations := testing.AllocsPerRun(1000, func() {
		matcher.Match(data)
	})

	if allocations != 0 {
		t.Fatalf("Match allocates %g times", allocations)
	}
}

func FuzzMatcher(fuzz *testing.F) {
	fuzz.Add(make([]byte, 32), "a")
	fuzz.Add([]byte("abcdefghijklmnopqrstuvwxyz234567"), ".bc.")
	fuzz.Add([]byte("01234567890123456789012345678901"), "ab.cq")

	fuzz.Fuzz(func(t *testing.T, input []byte, pattern string) {
		if len(input) != 32 {
			return
		}

		matcher, err := CompilePatterns([]string{pattern})
		if err != nil {
			return
		}

		data := [32]byte(input)
		encoded := testEncoding.EncodeToString(data[:])

		want := referenceMatch([]string{pattern}, encoded)
		if matcher.Match(data) != want {
			t.Fatalf("%q against %q: want %v", pattern, encoded, want)
		}
	})
}

func compileImplementations(t *testing.T, patterns []string) []*Matcher {
	t.Helper()

	implementations := make([]*Matcher, 0, 2)

	scalar, err := compilePatterns(patterns, false)
	if err != nil {
		t.Fatal(err)
	}

	implementations = append(implementations, scalar)

	if vectorAvailable {
		vector, vectorErr := compilePatterns(patterns, true)
		if vectorErr != nil {
			t.Fatal(vectorErr)
		}

		implementations = append(implementations, vector)
	}

	return implementations
}

func checkImplementations(t *testing.T, patterns []string, data [32]byte, want bool) {
	t.Helper()

	for _, matcher := range compileImplementations(t, patterns) {
		if matcher.Match(data) != want {
			t.Fatalf("%q against %q: want %v (vector %v)", patterns, testEncoding.EncodeToString(data[:]), want, len(matcher.scans) != 0)
		}
	}
}

func randomInput(random *rand.Rand) [32]byte {
	var data [32]byte

	for index := range data {
		data[index] = byte(random.Uint32())
	}

	return data
}

func referenceMatch(patterns []string, encoded string) bool {
	for _, pattern := range patterns {
		if strings.HasPrefix(pattern, ".") && strings.HasSuffix(pattern, ".") {
			if strings.Contains(encoded[1:len(encoded)-1], pattern[1:len(pattern)-1]) {
				return true
			}

			continue
		}

		prefix, suffix, dotted := strings.Cut(pattern, ".")
		if dotted {
			if strings.HasPrefix(encoded, prefix) && strings.HasSuffix(encoded, suffix) {
				return true
			}
		} else if strings.Contains(encoded, pattern) {
			return true
		}
	}

	return false
}
