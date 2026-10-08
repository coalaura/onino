package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/search"
)

type numberCase struct {
	value float64
	want  string
}

type waitCase struct {
	candidates float64
	rate       float64
	want       string
}

func TestCompactNumbers(t *testing.T) {
	cases := []numberCase{
		{value: 0, want: "0"},
		{value: math.Copysign(0, -1), want: "0"},
		{value: 999.4, want: "999"},
		{value: 999.5, want: "1k"},
		{value: 999499, want: "999k"},
		{value: 999500, want: "1M"},
		{value: 26.4e6, want: "26.4M"},
		{value: 4.01e9, want: "4.01G"},
		{value: 999.5e18, want: "1e21"},
		{value: 1e78, want: "1e78"},
		{value: 1.234e-78, want: "1.23e-78"},
		{value: math.MaxFloat64, want: "1.8e308"},
		{value: math.SmallestNonzeroFloat64, want: "4.94e-324"},
		{value: math.NaN(), want: "--"},
		{value: math.Inf(1), want: "--"},
		{value: math.Inf(-1), want: "--"},
		{value: -1, want: "--"},
	}

	for _, test := range cases {
		got := string(appendCompact(nil, test.value))
		if got != test.want {
			t.Fatalf("%g: %q, want %q", test.value, got, test.want)
		}
	}

	exact := string(appendExact(nil, math.MaxUint64))
	if exact != "18,446,744,073,709,551,615" {
		t.Fatal(exact)
	}
}

func TestWaitStatesAndExtremeValues(t *testing.T) {
	cases := []waitCase{
		{candidates: 100, rate: 0, want: "stalled"},
		{candidates: math.NaN(), rate: 100, want: "--"},
		{candidates: 0, rate: 100, want: "--"},
		{candidates: -100, rate: 100, want: "--"},
		{candidates: math.Inf(1), rate: 100, want: "impossible"},
		{candidates: math.Inf(1), rate: 0, want: "impossible"},
		{candidates: 100, rate: -1, want: "--"},
		{candidates: 100, rate: math.NaN(), want: "--"},
		{candidates: 100, rate: math.Inf(1), want: "--"},
		{candidates: 100, rate: 200, want: "~<1s"},
		{candidates: 1e78, rate: 1, want: "~3.17e70y"},
	}

	for _, test := range cases {
		got := string(appendWait(nil, test.candidates, test.rate))
		if got != test.want {
			t.Fatalf("%g at %g/s = %q, want %q", test.candidates, test.rate, got, test.want)
		}
	}

	extreme := string(appendWait(nil, math.MaxFloat64, math.SmallestNonzeroFloat64))
	if !strings.HasPrefix(extreme, "~") || !strings.Contains(extreme, "e") || !strings.HasSuffix(extreme, "y") || strings.ContainsAny(extreme, "+- ") {
		t.Fatalf("finite ratio overflowed: %s", extreme)
	}

	invalid := []float64{-1, 1.01, math.NaN(), math.Inf(1), math.Inf(-1)}

	for _, probability := range invalid {
		estimate := newMatchEstimate(probability)
		if !math.IsNaN(estimate.candidates50) || !math.IsNaN(estimate.candidates95) {
			t.Fatalf("invalid probability accepted: %g -> %+v", probability, estimate)
		}
	}
}

func TestProgressIsOneCompactLine(t *testing.T) {
	counts := []uint64{0, 999, 1000, 26_400_000, 133_200_000_000, math.MaxUint64}
	rates := []float64{0, 0.000001, 999.9, 4.01e9, math.MaxFloat64, math.NaN()}

	estimate := newMatchEstimate(math.Ldexp(1, -256))

	for _, count := range counts {
		for _, rate := range rates {
			stats := search.Stats{Checked: count, Saved: count}

			line := string(appendSearchStatus(nil, stats, time.Duration(math.MaxInt64), rate, estimate, ""))
			if strings.Count(line, "\n") != 1 || !strings.HasSuffix(line, "\n") || strings.ContainsAny(line, "\r\x1b") || len(line) > 110 {
				t.Fatalf("not a compact plain-text line (%d): %q", len(line), line)
			}
		}
	}
}
