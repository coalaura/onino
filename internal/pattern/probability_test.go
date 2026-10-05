package pattern

import (
	"math"
	"math/rand/v2"
	"strings"
	"testing"
)

type probabilityCase struct {
	name     string
	patterns []string
	want     float64
}

func TestEstimateProbability(t *testing.T) {
	full := visibleEncoding(make([]byte, 32))
	suffixes := make([]string, 0, 32)

	for _, symbol := range testAlphabet {
		suffixes = append(suffixes, "."+string(symbol))
	}

	cases := []probabilityCase{
		{name: "empty", want: 0},
		{name: "prefix", patterns: []string{"example."}, want: math.Ldexp(1, -35)},
		{name: "visible suffix", patterns: []string{".examplea"}, want: math.Ldexp(1, -40)},
		{name: "combined", patterns: []string{"example.a"}, want: math.Ldexp(1, -40)},
		{name: "short suffix", patterns: []string{".b"}, want: 1.0 / 32},
		{name: "checksum alternatives", patterns: []string{".a", ".b"}, want: 2.0 / 32},
		{name: "overlapping anchors", patterns: []string{full + "." + full}, want: math.Ldexp(1, -256)},
		{name: "duplicates and containment", patterns: []string{"abc.", "abc.", "abcd."}, want: math.Ldexp(1, -15)},
		{name: "disjoint", patterns: []string{"a.", "b."}, want: 2.0 / 32},
		{name: "intersecting", patterns: []string{"a.", ".a"}, want: 2.0/32 - 1.0/1024},
		{name: "all hit", patterns: suffixes, want: 1},
		{name: "interior", patterns: []string{".x."}, want: 1 - math.Pow(31.0/32, 50)},
		{name: "anywhere", patterns: []string{"x"}, want: 1 - math.Pow(31.0/32, 52)},
		{name: "anywhere a", patterns: []string{"a"}, want: 1 - math.Pow(31.0/32, 52)},
		{name: "self overlapping literal", patterns: []string{"aa"}, want: repeatedSymbolProbability()},
	}

	for _, entry := range cases {
		t.Run(entry.name, func(t *testing.T) {
			got, err := EstimateProbability(entry.patterns)
			if err != nil {
				t.Fatal(err)
			}

			if math.Abs(got-entry.want) > max(entry.want*1e-12, 1e-90) {
				t.Fatalf("probability = %.16g, want %.16g", got, entry.want)
			}
		})
	}

	invalid := []string{"", "EXAMPLE.", "a..b", strings.Repeat("a", 53), "." + strings.Repeat("a", 51) + "."}

	for _, text := range invalid {
		_, err := EstimateProbability([]string{text})
		if err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestProbabilityDiagramExhaustive(t *testing.T) {
	random := rand.New(rand.NewPCG(17, 29))

	conditions := make([]probabilityCondition, 12)

	for range 30 {
		for index := range conditions {
			mask := random.Uint64() & 63
			checksumMask := random.Uint64() & 15

			conditions[index] = probabilityCondition{mask: [5]uint64{mask, 0, 0, 0, checksumMask}, value: [5]uint64{random.Uint64() & mask, 0, 0, 0, random.Uint64() & checksumMask}}
		}

		matches := 0

		for value := range uint64(1024) {
			input := [5]uint64{value & 63, 0, 0, 0, value >> 6}

			for index := range conditions {
				if conditions[index].matches(&input) {
					matches++

					break
				}
			}
		}

		got, complete := exactProbability(conditions, probabilityNodeLimit)
		want := float64(matches) / 1024

		if !complete || got != want {
			t.Fatalf("diagram = %g (%v), exhaustive = %g", got, complete, want)
		}
	}
}

func TestProbabilitySampling(t *testing.T) {
	conditions, err := probabilityConditions([]string{"a.", "a.a", "b.a"})
	if err != nil {
		t.Fatal(err)
	}

	_, complete := exactProbability(conditions, 4)
	if complete {
		t.Fatal("diagram exceeded its node budget")
	}

	got := sampleProbability(conditions)
	want := 1.0/32 + 1.0/1024

	if math.Abs(got-want) > want*0.03 {
		t.Fatalf("weighted estimate = %g, want approximately %g", got, want)
	}

	// Conditioning on a match must retain rare probabilities, rather than
	// rounding to zero as a naive random-key sampling scheme would.
	for index := range conditions {
		conditions[index].mask[1] = ^uint64(0)
		conditions[index].mask[2] = ^uint64(0)
	}

	rare := sampleProbability(conditions)
	if rare != math.Ldexp(got, -128) {
		t.Fatalf("rare weighted estimate = %g, want %g", rare, math.Ldexp(got, -128))
	}
}

func BenchmarkEstimateProbability(bench *testing.B) {
	cases := []matcherBenchmark{
		{name: "prefix", patterns: []string{"example."}},
		{name: "anywhere", patterns: []string{"example"}},
		{name: "dictionary512", patterns: dictionaryPatterns(512)},
	}

	for _, entry := range cases {
		bench.Run(entry.name, func(bench *testing.B) {
			bench.ReportAllocs()

			for bench.Loop() {
				_, err := EstimateProbability(entry.patterns)
				if err != nil {
					bench.Fatal(err)
				}
			}
		})
	}
}

func repeatedSymbolProbability() float64 {
	notEndingA := 1.0

	var (
		endingA float64
		matched float64
	)

	for range encodedSize {
		chanceA := 1.0 / 32

		matched += endingA * chanceA
		nextEndingA := notEndingA * chanceA
		notEndingA = (notEndingA + endingA) * (1 - chanceA)
		endingA = nextEndingA
	}

	return matched
}
