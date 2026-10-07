//go:build gpu

package gpu

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

type tuningFixture struct {
	now       time.Time
	calls     []configuration
	durations []time.Duration
	failAt    int
}

func (fixture *tuningFixture) measure(config configuration, duration time.Duration) (observation, error) {
	fixture.calls = append(fixture.calls, config)
	fixture.durations = append(fixture.durations, duration)
	fixture.now = fixture.now.Add(duration + 5*time.Millisecond)

	if fixture.failAt == len(fixture.calls) {
		return observation{}, context.Canceled
	}

	// More independent streams help up to 8192; further state gives no benefit.
	rate := float64(min(config.streams, 8192)) * float64(min(config.rounds, 4))
	tail := time.Duration(config.streams*config.rounds) * time.Microsecond / 4

	return observation{configuration: config, rate: rate, tail: tail}, nil
}

func (fixture *tuningFixture) tuner(options Options) calibration {
	return calibration{
		options:  options,
		deadline: fixture.now.Add(startupBudget),
		maximum:  MaxStreams,
		measure:  fixture.measure,
		now: func() time.Time {
			return fixture.now
		},
	}
}

func TestTuningOverridesAndDeadline(t *testing.T) {
	settings := []Options{
		{Streams: 4096, Rounds: 2},
		{Streams: 4096, Rounds: 2, AutoRounds: true},
		{Streams: 4096, Rounds: 2, AutoStreams: true},
		{AutoStreams: true, AutoRounds: true},
	}

	for _, options := range settings {
		fixture := tuningFixture{now: time.Unix(0, 0)}
		tuner := fixture.tuner(options)

		selected, err := tuner.selectInitial()
		if err != nil {
			t.Fatal(err)
		}

		if (!options.AutoStreams && selected.configuration.streams != options.Streams) || (!options.AutoRounds && selected.configuration.rounds != options.Rounds) {
			t.Fatalf("overrode explicit setting: %+v -> %+v", options, selected)
		}

		if fixture.now.After(tuner.deadline) {
			t.Fatal("exploration exceeded startup deadline")
		}

		if !options.AutoStreams && !options.AutoRounds {
			for _, duration := range fixture.durations {
				if duration != 0 {
					t.Fatal("manual settings performed performance exploration")
				}
			}
		}
	}

	fixture := tuningFixture{now: time.Unix(0, 0)}

	tuner := fixture.tuner(Options{AutoStreams: true, AutoRounds: true})
	tuner.deadline = fixture.now

	selected, err := tuner.selectInitial()
	if err != nil || selected.configuration != (configuration{streams: DefaultStreams, rounds: 1}) || len(fixture.calls) != 1 {
		t.Fatalf("late initialization must retain first validated work: %+v, %v", selected, err)
	}
}

func TestTuningNoiseAndResponsiveness(t *testing.T) {
	incumbent := observation{configuration: configuration{streams: 8192, rounds: 4}, rate: 100, tail: 10 * time.Millisecond}
	candidate := observation{configuration: configuration{streams: 16384, rounds: 4}, rate: 102, tail: 20 * time.Millisecond}

	if better(candidate, incumbent, 0.025) {
		t.Fatal("noise justified more expensive configuration")
	}

	candidate.rate = 110

	if !better(candidate, incumbent, 0.025) || better(candidate, incumbent, 0.15) {
		t.Fatal("repeat variability was not used as the improvement threshold")
	}

	incumbent.hits = 1
	if scaledTail(incumbent, incumbent.configuration) < 60*time.Millisecond {
		t.Fatal("paused rounds were treated as fully executed rounds")
	}

	fixture := tuningFixture{now: time.Unix(0, 0)}
	tuner := fixture.tuner(Options{Streams: MaxStreams, Rounds: MaxRounds})

	_, err := tuner.selectInitial()
	if err == nil {
		t.Fatal("accepted predicted unresponsive manual configuration")
	}

	for failure := 1; failure <= 5; failure++ {
		fixture = tuningFixture{now: time.Unix(0, 0), failAt: failure}
		tuner = fixture.tuner(Options{AutoStreams: true, AutoRounds: true})

		_, err = tuner.selectInitial()
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("lost lifecycle error at trial %d: %v", failure, err)
		}
	}
}

func TestTuningDeadlineRetainsBestProbe(t *testing.T) {
	fixture := tuningFixture{now: time.Unix(0, 0)}

	tuner := fixture.tuner(Options{AutoStreams: true, AutoRounds: true})
	tuner.deadline = fixture.now.Add(700 * time.Millisecond)

	tuner.measure = func(config configuration, duration time.Duration) (observation, error) {
		measured, err := fixture.measure(config, duration)
		measured.rate = 1e6 / float64(config.streams)

		return measured, err
	}

	selected, err := tuner.selectInitial()
	if err != nil {
		t.Fatal(err)
	}

	if len(fixture.calls) != 2 || selected.configuration.streams != DefaultStreams {
		t.Fatalf("deadline retained the last probe instead of the best: %+v, %v", selected, fixture.calls)
	}
}

func TestTuningReturnsToIncumbent(t *testing.T) {
	fixture := tuningFixture{now: time.Unix(0, 0)}

	tuner := fixture.tuner(Options{AutoStreams: true, AutoRounds: true})

	selected, err := tuner.selectInitial()
	if err != nil {
		t.Fatal(err)
	}

	count := len(fixture.calls)
	if count < 3 || fixture.calls[count-3] != fixture.calls[count-1] || fixture.calls[count-2] == fixture.calls[count-1] {
		t.Fatalf("missing repeated comparison and return: %v", fixture.calls)
	}

	if selected.rate < 0.975*8192*4 {
		t.Fatalf("failed to find throughput plateau: %+v", selected)
	}
}

func TestFilterAcceptanceUnion(t *testing.T) {
	patterns := []string{"a.", "ab.", "a.", "b.", "abcdefghijklmno."}

	plan, err := Compile(patterns)
	if err != nil || plan.acceptance() != 2.0/32 {
		t.Fatalf("overlapping union: %.12g, %v", plan.acceptance(), err)
	}

	plan, err = Compile([]string{"abcdefghijklmnop.", "abcdefghijklabcd."})
	if err != nil || plan.acceptance() != math.Ldexp(1, -60) {
		t.Fatalf("filter-length limit: %.12g, %v", plan.acceptance(), err)
	}

	anchor := observation{configuration: configuration{streams: 256, rounds: 1}, tail: time.Millisecond}
	configs := tuningCandidates(anchor, Options{AutoStreams: true, AutoRounds: true}, 1.0/32)

	for _, config := range configs {
		if config.rounds != 1 {
			t.Fatal("frequent hits caused round growth")
		}
	}
}

func TestDirtyRefillsSurviveInactiveStreams(t *testing.T) {
	commands := make([]command, 16)
	refills := make(chan feedback, 2)

	refills <- feedback{stream: 12, command: command{Expected: 3, Action: 1}}

	first, last := drainUpdates(refills, commands, len(commands), len(commands))

	refills <- feedback{stream: 2, command: command{Generation: 2, Action: 2}}

	first, last = drainUpdates(refills, commands, first, last)
	if first != 2 || last != 13 || commands[12].Expected != 3 || commands[2].Generation != 2 {
		t.Fatal("dirty range lost a pending inactive acknowledgement")
	}
}
