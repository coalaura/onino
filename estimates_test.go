package main

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/search"
)

func TestMatchEstimate(t *testing.T) {
	probability := math.Ldexp(1, -35)
	estimate := newMatchEstimate(probability)

	thresholds := []float64{0.5, 0.95}

	counts := []float64{estimate.candidates50, estimate.candidates95}

	for index, count := range counts {
		chance := -math.Expm1(count * math.Log1p(-probability))
		previous := -math.Expm1((count - 1) * math.Log1p(-probability))

		if chance < thresholds[index] || previous >= thresholds[index] {
			t.Fatalf("%g candidates: previous=%g chance=%g threshold=%g", count, previous, chance, thresholds[index])
		}
	}

	rare := newMatchEstimate(math.Ldexp(1, -256))
	if math.IsInf(rare.candidates50, 0) || rare.candidates50 <= 1e76 || rare.candidates95 <= rare.candidates50 {
		t.Fatalf("rare probability lost precision: %+v", rare)
	}

	all := newMatchEstimate(1)
	if all.candidates50 != 1 || all.candidates95 != 1 {
		t.Fatalf("all-hit estimate: %+v", all)
	}

	none := newMatchEstimate(0)
	if !math.IsInf(none.candidates50, 1) || !math.IsInf(none.candidates95, 1) {
		t.Fatalf("empty estimate: %+v", none)
	}
}

func TestEstimateStatus(t *testing.T) {
	estimate := newMatchEstimate(math.Ldexp(1, -35))

	stats := search.Stats{Checked: 1000000000, Saved: 2}

	var buffer [320]byte

	startup := string(appendCandidateEstimate(buffer[:0], estimate))
	if !strings.Contains(startup, "50% ~23,816,355,775") || !strings.Contains(startup, "; 95% ~102,932,577,139") || !strings.Contains(startup, "recent combined rate") {
		t.Fatalf("unexpected startup estimate: %s", startup)
	}

	line := string(appendSearchStatus(buffer[:0], stats, 40*time.Second, 25000000, estimate, false))
	if !strings.Contains(line, "25,000,000 keys/s recent") || !strings.Contains(line, "50% 15m53s, 95% 1h8m38s") {
		t.Fatalf("unexpected progress: %s", line)
	}

	// Already checked candidates and previous saves do not shorten the next
	// independent wait. The estimate changes only with the recent rate.
	stats.Checked *= 10
	stats.Saved = 100

	later := string(appendSearchStatus(buffer[:0], stats, 400*time.Second, 25000000, estimate, false))

	_, wait, _ := strings.Cut(line, "; est.")
	_, laterWait, _ := strings.Cut(later, "; est.")

	if wait != laterWait {
		t.Fatalf("elapsed work incorrectly shortened the wait: %s vs %s", wait, laterWait)
	}

	zero := string(appendSearchStatus(buffer[:0], search.Stats{}, 0, 0, estimate, false))
	if !strings.Contains(zero, "50% --, 95% --") {
		t.Fatal(zero)
	}

	final := string(appendSearchStatus(buffer[:0], stats, time.Minute, 100, estimate, true))
	if strings.Contains(final, "wait") || !strings.Contains(final, "saved 100 matches") || !strings.Contains(final, "100 keys/s overall avg") {
		t.Fatal(final)
	}
}

func TestEstimateFormattingAndAllocations(t *testing.T) {
	var buffer [320]byte

	got := string(appendWaitEstimate(buffer[:0], 0.5))
	if got != "<1s" {
		t.Fatal(got)
	}

	got = string(appendWaitEstimate(buffer[:0], 2*86400))
	if got != "2.0d" {
		t.Fatal(got)
	}

	got = string(appendWaitEstimate(buffer[:0], 1e50))
	if !strings.HasSuffix(got, "y") || strings.Contains(got, "-") {
		t.Fatal(got)
	}

	estimate := newMatchEstimate(math.Ldexp(1, -256))

	stats := search.Stats{Checked: math.MaxUint64, Saved: math.MaxUint64}

	allocations := testing.AllocsPerRun(100, func() {
		appendCandidateEstimate(buffer[:0], estimate)
		appendSearchStatus(buffer[:0], stats, time.Hour, 25000000, estimate, false)
		appendSearchStatus(buffer[:0], stats, time.Hour, 25000000, estimate, true)
	})

	if allocations != 0 {
		t.Fatalf("estimate formatting allocated: %g", allocations)
	}
}
