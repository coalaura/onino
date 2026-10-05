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
	full := strings.Repeat("a", 52)

	cases := []probabilityCase{
		{name: "empty", want: 0},
		{name: "prefix", patterns: []string{"example."}, want: math.Ldexp(1, -35)},
		{name: "suffix padding", patterns: []string{".examplea"}, want: math.Ldexp(1, -36)},
		{name: "combined", patterns: []string{"example.a"}, want: math.Ldexp(1, -36)},
		{name: "overlapping anchors", patterns: []string{full + "." + full}, want: math.Ldexp(1, -256)},
		{name: "duplicates and containment", patterns: []string{"abc.", "abc.", "abcd."}, want: math.Ldexp(1, -15)},
		{name: "disjoint", patterns: []string{"a.", "b."}, want: 2.0 / 32},
		{name: "intersecting", patterns: []string{"a.", ".a"}, want: 1.0/32 + 0.5 - 1.0/64},
		{name: "all hit", patterns: []string{".a", ".q"}, want: 1},
		{name: "interior", patterns: []string{".x."}, want: 1 - math.Pow(31.0/32, 50)},
		{name: "anywhere", patterns: []string{"x"}, want: 1 - math.Pow(31.0/32, 51)},
		{name: "anywhere padding", patterns: []string{"a"}, want: 1 - 0.5*math.Pow(31.0/32, 51)},
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

	invalid := []string{"", "EXAMPLE.", ".end", strings.Repeat("a", 53), "." + strings.Repeat("a", 51) + "."}

	for _, text := range invalid {
		_, err := EstimateProbability([]string{text})
		if err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestProbabilityDiagramExhaustive(t *testing.T) {
	random := rand.New(rand.NewPCG(17, 29))

	conditions := make([]bitPattern, 12)

	for range 30 {
		for index := range conditions {
			mask := random.Uint64() & 1023
			conditions[index] = bitPattern{mask: [4]uint64{mask}, value: [4]uint64{random.Uint64() & mask}}
		}

		matches := 0

		for value := range uint64(1024) {
			input := [4]uint64{value}

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
	want := 3.0 / 64

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

	for position := range encodedSize {
		chanceA := 1.0 / 32

		if position == encodedSize-1 {
			chanceA = 0.5
		}

		matched += endingA * chanceA
		nextEndingA := notEndingA * chanceA
		notEndingA = (notEndingA + endingA) * (1 - chanceA)
		endingA = nextEndingA
	}

	return matched
}
