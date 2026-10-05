package pattern

import (
	"math/rand/v2"
	"strings"
	"testing"
)

func TestSuffixPaddingRegression(t *testing.T) {
	hostname := "chopodvybvnngdeduyrlohgnxqmaddfyau2eeo4v3kr3vhpt6aahu2qd.onion"

	decoded, err := testEncoding.DecodeString(hostname[:56])
	if err != nil {
		t.Fatal(err)
	}

	data := [32]byte(decoded[:32])
	if !strings.HasSuffix(testEncoding.EncodeToString(data[:]), "aaa") {
		t.Fatal("fixture does not exercise the old padding-only match")
	}

	if visibleEncoding(data[:]) != hostname[:encodedSize] {
		t.Fatal("fixture checksum does not match its public key")
	}

	checkImplementations(t, []string{".aaa"}, data, false)
	checkImplementations(t, []string{"." + hostname[49:52]}, data, true)
}

func TestShortSuffixTablesAgainstHostname(t *testing.T) {
	sets := [][]string{
		{".b", ".q", ".7"},
		{".ab", ".aq", ".zz"},
		{".aab", ".aaq", ".aaa", ".777"},
		{".b", ".aq", ".aaa"},
	}

	for _, patterns := range sets {
		matchers := compileImplementations(t, patterns)

		random := rand.New(rand.NewPCG(97, 53))

		var matched bool

		for tail := range 1 << 16 {
			data := randomInput(random)
			data[30] = byte(tail >> 8)
			data[31] = byte(tail)
			encoded := visibleEncoding(data[:])
			want := referenceMatch(patterns, encoded)
			matched = matched || want

			for _, matcher := range matchers {
				if matcher.Match(data) != want {
					t.Fatalf("%q against %q: want %v", patterns, encoded, want)
				}
			}
		}

		if !matched {
			t.Fatalf("no positive coverage for %q", patterns)
		}
	}
}

func TestSuffixChecksumCoverage(t *testing.T) {
	patterns := make([]string, 16)

	for index := range patterns {
		patterns[index] = ".zz" + string(testAlphabet[index])
	}

	decoded, err := testEncoding.DecodeString(strings.Repeat("a", 49) + "zza")
	if err != nil {
		t.Fatal(err)
	}

	data := [32]byte(decoded)
	matchers := compileImplementations(t, patterns)

	for _, matcher := range matchers {
		if matcher.boundary != nil {
			t.Fatal("complete checksum coverage must not require runtime hashing")
		}

		for value := range 256 {
			data[0] = byte(value)

			if !matcher.Match(data) {
				t.Fatalf("complete checksum coverage rejected %q", visibleEncoding(data[:]))
			}
		}
	}
}

func TestFullySpecifiedChecksum(t *testing.T) {
	data := randomInput(rand.New(rand.NewPCG(17, 43)))
	encoded := visibleEncoding(data[:])

	last := strings.IndexByte(testAlphabet, encoded[51])

	for nibble := range 16 {
		text := encoded[:51] + string(testAlphabet[last&16|nibble])
		patterns := []string{text, text + ".", "." + text, text[:26] + "." + text[26:], text + "." + text}

		for _, pattern := range patterns {
			matcher, err := CompilePatterns([]string{pattern})

			if nibble != last&15 {
				if err == nil || matcher != nil {
					t.Fatalf("accepted fully specified public key with incorrect checksum: %q", pattern)
				}

				continue
			}

			if err != nil {
				t.Fatal(err)
			}

			if matcher.boundary != nil || !matcher.Match(data) {
				t.Fatalf("valid fully specified key must match without runtime hashing: %q", pattern)
			}
		}
	}
}

func TestBoundaryMatchAllocations(t *testing.T) {
	data := randomInput(rand.New(rand.NewPCG(89, 13)))
	encoded := visibleEncoding(data[:])

	wrong := strings.IndexByte(testAlphabet, encoded[51]) ^ 1

	patterns := []string{"." + encoded[51:], "." + encoded[49:], "." + encoded[45:], encoded[45:], "." + encoded[49:51] + string(testAlphabet[wrong])}

	for _, text := range patterns {
		matcher, err := CompilePatterns([]string{text})
		if err != nil {
			t.Fatal(err)
		}

		allocations := testing.AllocsPerRun(1000, func() {
			matcher.Match(data)
		})

		if allocations != 0 {
			t.Fatalf("%q allocated %v times", text, allocations)
		}
	}
}
