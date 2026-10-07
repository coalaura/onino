//go:build gpu

package gpu

import (
	"errors"
	"math"
	"slices"
	"time"
)

const (
	startupBudget     = 10 * time.Second
	submissionLimit   = 150 * time.Millisecond
	flightLimit       = 120 * time.Millisecond
	trialDuration     = 300 * time.Millisecond
	transitionReserve = 500 * time.Millisecond
)

type configuration struct {
	streams int
	rounds  int
}

type observation struct {
	configuration configuration
	rate          float64
	tail          time.Duration
	hits          uint64
	checked       uint64
	backlog       int
}

type calibration struct {
	options    Options
	deadline   time.Time
	maximum    int
	acceptance float64
	measure    func(configuration, time.Duration) (observation, error)
	now        func() time.Time
}

func (tuner *calibration) room(duration time.Duration) bool {
	return tuner.now().Add(duration + transitionReserve).Before(tuner.deadline)
}

// validate grows useful work conservatively. Explicit settings are either reached or rejected;
// the small validation dispatches are accounted search, never replayed benchmarks.
func (tuner *calibration) validate(target configuration, previous observation) (observation, error) {
	current := previous.configuration

	for current != target {
		next := current
		if next.streams < target.streams {
			next.streams = min(target.streams, next.streams*2)
		} else {
			next.rounds = min(target.rounds, next.rounds*2)
		}

		predicted := scaledTail(previous, next)
		if predicted > flightLimit/2 {
			return previous, errors.New("GPU configuration exceeds the conservative submission budget; reduce --gpu-streams or --gpu-rounds")
		}

		measured, err := tuner.measure(next, 0)
		if err != nil {
			return previous, err
		}

		previous = measured
		current = next
	}

	return previous, nil
}

func (tuner *calibration) selectInitial() (observation, error) {
	initial := configuration{streams: min(DefaultStreams, tuner.maximum), rounds: 1}
	duration := 50 * time.Millisecond

	if !tuner.options.AutoStreams {
		initial.streams = tuner.options.Streams

		if !tuner.options.AutoRounds {
			duration = 0
		}
	}

	incumbent, err := tuner.measure(initial, duration)
	if err != nil {
		return incumbent, err
	}

	if !tuner.options.AutoStreams || !tuner.options.AutoRounds {
		target := initial

		if !tuner.options.AutoStreams {
			target.streams = tuner.options.Streams
		}

		if !tuner.options.AutoRounds {
			target.rounds = tuner.options.Rounds
		}

		incumbent, err = tuner.validate(target, incumbent)
		if err != nil {
			return incumbent, err
		}
	}

	if !tuner.options.AutoStreams && !tuner.options.AutoRounds {
		return incumbent, nil
	}

	fallback := incumbent

	// Independent streams are explored before rounds. One-round tails bound growth even
	// when a frequent filter pauses streams before the requested round count is reached.
	for tuner.options.AutoStreams && incumbent.configuration.streams < tuner.maximum && tuner.room(100*time.Millisecond) {
		next := incumbent.configuration
		next.streams = min(tuner.maximum, next.streams*2)

		if scaledTail(incumbent, next) > flightLimit/2 {
			break
		}

		candidate, measureError := tuner.measure(next, 50*time.Millisecond)
		if measureError != nil {
			return incumbent, measureError
		}

		incumbent = candidate

		if better(candidate, fallback, 0.025) {
			fallback = candidate
		}

		if candidate.backlog > next.streams/4 || tuner.acceptance*float64(next.streams*128) > 256 {
			break
		}
	}

	anchor := incumbent

	if !tuner.room(trialDuration) {
		return fallback, nil
	}

	incumbent, err = tuner.measure(anchor.configuration, trialDuration)
	if err != nil {
		return fallback, err
	}

	candidates := tuningCandidates(anchor, tuner.options, tuner.acceptance)

	best := incumbent
	runner := incumbent

	for _, next := range candidates {
		if !tuner.room(4*trialDuration + 100*time.Millisecond) {
			break
		}

		if scaledTail(anchor, next) > flightLimit/2 {
			continue
		}

		candidate, measureError := tuner.measure(next, trialDuration)
		if measureError != nil {
			return best, measureError
		}

		if better(candidate, best, 0.025) {
			runner = best
			best = candidate
		} else if candidate.configuration != best.configuration && (runner.configuration == best.configuration || better(candidate, runner, 0.025)) {
			runner = candidate
		}
	}

	if best.configuration == runner.configuration || !tuner.room(3*trialDuration) {
		return best, nil
	}

	// Return to the runner, repeat the winner, then return again. A time trend or a
	// noisy combined CPU sample raises the required improvement instead of winning a tie.
	before, err := tuner.measure(runner.configuration, trialDuration)
	if err != nil {
		return best, err
	}

	repeated, err := tuner.measure(best.configuration, trialDuration)
	if err != nil {
		return best, err
	}

	after, err := tuner.measure(runner.configuration, trialDuration)
	if err != nil {
		return best, err
	}

	noise := math.Abs(before.rate-after.rate) / max(before.rate, after.rate, 1)

	runner.rate = (before.rate + after.rate) / 2
	runner.tail = max(before.tail, after.tail)

	best.rate = repeated.rate
	best.tail = max(best.tail, repeated.tail)

	if better(best, runner, max(0.025, noise)) {
		return best, nil
	}

	return runner, nil
}

func tuningCandidates(anchor observation, options Options, acceptance float64) []configuration {
	candidates := make([]configuration, 0, 10)

	streams := anchor.configuration.streams
	rounds := anchor.configuration.rounds

	if options.AutoRounds {
		targets := [...]time.Duration{2 * time.Millisecond, 5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}

		for _, target := range targets {
			rounds = max(1, min(MaxRounds, int(float64(target)*float64(anchor.configuration.rounds)/float64(max(anchor.tail, time.Microsecond)))))

			if acceptance*128 > 0.05 {
				rounds = 1
			}

			candidates = appendConfiguration(candidates, configuration{streams: streams, rounds: rounds})
		}
	} else {
		candidates = appendConfiguration(candidates, anchor.configuration)
	}

	if options.AutoStreams {
		divisors := [...]int{2, 4}

		for _, divisor := range divisors {
			nearby := max(1, streams/divisor)
			nearbyRounds := rounds

			if options.AutoRounds && acceptance*128 <= 0.05 {
				nearbyRounds = min(MaxRounds, rounds*divisor)
			}

			candidates = appendConfiguration(candidates, configuration{streams: nearby, rounds: nearbyRounds})
		}
	}

	if options.AutoStreams && options.AutoRounds && streams >= 4096 && acceptance*128 <= 0.05 {
		candidates = appendConfiguration(candidates, configuration{streams: 4096, rounds: 2})

		if streams >= 8192 {
			candidates = appendConfiguration(candidates, configuration{streams: 8192, rounds: 8})
		}

		if streams >= 16384 {
			candidates = appendConfiguration(candidates, configuration{streams: 16384, rounds: 4})
		}
	}

	return candidates
}

func appendConfiguration(configurations []configuration, candidate configuration) []configuration {
	if slices.Contains(configurations, candidate) {
		return configurations
	}

	return append(configurations, candidate)
}

func better(candidate, incumbent observation, noise float64) bool {
	if candidate.rate > incumbent.rate*(1+noise) {
		return true
	}

	if incumbent.rate > candidate.rate*(1+noise) {
		return false
	}

	if candidate.tail < incumbent.tail*9/10 {
		return true
	}

	return candidate.tail <= incumbent.tail*11/10 && candidate.configuration.streams < incumbent.configuration.streams
}

func scaledTail(previous observation, next configuration) time.Duration {
	work := float64(next.streams*next.rounds) / float64(previous.configuration.streams*previous.configuration.rounds)

	if previous.hits > 0 {
		// A paused dispatch may have executed only one round. Never use it as evidence
		// that an entire multi-round dispatch fits when a later range has no hits.
		work *= float64(previous.configuration.rounds)
	}

	return time.Duration(float64(previous.tail) * max(1, work) * 1.5)
}

func reducedConfiguration(current configuration, options Options) (configuration, error) {
	if options.AutoRounds && current.rounds > 1 {
		current.rounds = max(1, current.rounds/2)

		return current, nil
	}

	if options.AutoStreams && current.streams > 1 {
		current.streams = max(1, current.streams/2)

		return current, nil
	}

	return current, errors.New("GPU submission exceeded the responsiveness budget; explicit configuration cannot be reduced")
}
