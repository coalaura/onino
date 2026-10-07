//go:build gpu

package gpu

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestSubmissionTransitions(t *testing.T) {
	requireVulkan(t)

	// An impossible probe makes the expected counts independent of random seeds.
	plan := Plan{count: 1}
	plan.probes[0][2] = 1

	engine, err := openDevice(testDevice(t), true, 128, 8, makeTable(plan), searchShader)
	if err != nil {
		t.Fatal(err)
	}

	defer engine.close()

	commands := make([]command, 128)

	for index := range commands {
		_, commands[index], err = newSeed(1)
		if err != nil {
			t.Fatal(err)
		}
	}

	configurations := []configuration{{streams: 64, rounds: 2}, {streams: 128, rounds: 3}, {streams: 16, rounds: 4}, {streams: 128, rounds: 1}, {streams: 128, rounds: 1}}

	for index, setting := range configurations {
		updates := commands[:0]
		first := 0

		switch index {
		case 0:
			updates = commands[:64]
		case 1:
			first = 64
			updates = commands[64:]
		}

		err = engine.dispatch(index%2, updates, first, setting.streams, setting.rounds, false)
		if err != nil {
			t.Fatal(err)
		}

		completed, collectError := engine.collect(index % 2)
		if collectError != nil {
			t.Fatal(collectError)
		}

		expected := uint32(setting.streams * setting.rounds * 128)
		if completed.checked != expected || completed.pending != 0 || len(completed.hits) != 0 {
			t.Fatalf("transition %d: checked=%d want=%d pending=%d hits=%d", index, completed.checked, expected, completed.pending, len(completed.hits))
		}
	}

	err = engine.dispatch(0, nil, 0, 128, 1, true)
	if err != nil {
		t.Fatal(err)
	}

	completed, err := engine.collect(0)
	if err != nil || completed.checked != 0 || len(completed.hits) != 0 {
		t.Fatalf("collection-only reused a count: %+v, %v", completed, err)
	}

	if engine.validationErrors() != 0 {
		t.Fatal("Vulkan validation errors")
	}
}

func TestCalibrationDrainsSlowSaves(t *testing.T) {
	requireVulkan(t)

	patterns := []string{"a."}

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	saved := uint64(0)

	save := func(key onion.Key, found time.Time) error {
		if !matcher.Match(key.Public) {
			t.Error("saved a nonmatching key")
		}

		saved++
		cancel()
		time.Sleep(time.Millisecond)

		return nil
	}

	plan, err := Compile(patterns)
	if err != nil {
		t.Fatal(err)
	}

	stats, metrics, err := Run(ctx, plan, matcher, save, Options{Device: testDevice(t), Validation: true, AutoStreams: true, AutoRounds: true, Capacity: 1})
	if !errors.Is(err, context.Canceled) || saved == 0 || stats.Saved != saved || metrics.Hits != saved {
		t.Fatalf("calibration cancellation lost accepted matches: stats=%+v metrics=%+v saved=%d error=%v", stats, metrics, saved, err)
	}

	if metrics.Selection != 0 || metrics.Drain < time.Duration(saved/2)*time.Millisecond {
		t.Fatalf("expected cancellation during selection and slow save drain: %+v", metrics)
	}
}

func BenchmarkSubmission(b *testing.B) {
	if os.Getenv("ONINO_VULKAN_TEST") != "1" {
		b.Skip("set ONINO_VULKAN_TEST=1 to benchmark Vulkan")
	}

	plan := Plan{count: 1}
	plan.probes[0][2] = 1

	engine, err := openDevice(-1, false, 16384, 256, makeTable(plan), searchShader)
	if err != nil {
		b.Fatal(err)
	}

	defer engine.close()

	commands := make([]command, 16384)

	for index := range commands {
		_, commands[index], err = newSeed(1)
		if err != nil {
			b.Fatal(err)
		}
	}

	err = engine.submit(0, commands, 16, false)
	if err != nil {
		b.Fatal(err)
	}

	_, err = engine.collect(0)
	if err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()

	for b.Loop() {
		err = engine.dispatch(0, nil, 0, 16384, 16, false)
		if err != nil {
			b.Fatal(err)
		}

		completed, collectError := engine.collect(0)
		if collectError != nil || completed.checked != 16384*16*128 {
			b.Fatalf("submission accounting: %+v %v", completed, collectError)
		}
	}
}
