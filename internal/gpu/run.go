//go:build gpu

package gpu

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/search"
)

const (
	DefaultStreams = 256
	DefaultRounds  = 4
	MaxStreams     = 16384
	MaxRounds      = 64
)

type Options struct {
	Device     int
	Validation bool
	Streams    int
	Rounds     int
	Capacity   int
	Monitor    *search.Monitor
	// Ready runs once after device and seed setup, before the first submission.
	Ready func(device string)
}

type Metrics struct {
	Device      string
	Execution   time.Duration
	Elapsed     time.Duration
	Gaps        time.Duration
	Submissions uint64
	UploadBytes uint64
	ReadBytes   uint64
}

type feedback struct {
	stream  uint32
	command command
}

type discovered struct {
	hit   hit
	found time.Time
}

type verifier struct {
	seeds   []seed
	matcher *pattern.Matcher
	save    search.SaveMatchFunc
	saved   atomic.Uint64
}

func (worker *verifier) verify(candidate discovered) (feedback, error) {
	result := candidate.hit
	if result.Stream >= uint32(len(worker.seeds)) {
		return feedback{}, errors.New("GPU returned an invalid stream")
	}

	state := &worker.seeds[result.Stream]
	if state.generation != result.Generation {
		return feedback{}, errors.New("GPU returned a stale generation")
	}

	if result.Kind > 1 {
		return feedback{}, errors.New("GPU returned an invalid hit kind")
	}

	if result.Kind == 0 {
		key, err := reconstruct(state.secret, result.Steps)
		if err != nil {
			return feedback{}, err
		}

		word := binary.LittleEndian.Uint64(key.Public[:8])
		reported := uint64(result.Low) | uint64(result.High)<<32

		if word != reported {
			return feedback{}, fmt.Errorf("GPU candidate verification failed for stream %d generation %d offset %d", result.Stream, result.Generation, result.Steps)
		}

		if !worker.matcher.Match(key.Public) {
			return feedback{stream: result.Stream, command: command{Expected: state.generation, Action: 1}}, nil
		}

		err = worker.save(key, candidate.found)
		if err != nil {
			return feedback{}, err
		}

		worker.saved.Add(1)
	}

	if state.generation == ^uint32(0) {
		return feedback{}, errors.New("GPU stream generation exhausted")
	}

	replacement, refill, err := newSeed(state.generation + 1)
	if err != nil {
		return feedback{}, err
	}

	*state = replacement

	return feedback{stream: result.Stream, command: refill}, nil
}

// Run keeps at most two bounded dispatches in flight. Verification and persistence run on a separate goroutine; ordinary cancellation drains accepted hits and persistent overflow before returning.
func Run(ctx context.Context, plan Plan, matcher *pattern.Matcher, save search.SaveMatchFunc, options Options) (search.Stats, Metrics, error) {
	defer options.Monitor.Stop()

	var (
		stats   search.Stats
		metrics Metrics
		checked atomic.Uint64
	)

	if matcher == nil || save == nil || plan.count == 0 {
		return stats, metrics, errors.New("GPU search requires a plan, matcher and save callback")
	}

	if options.Streams == 0 {
		options.Streams = DefaultStreams
	}

	if options.Rounds == 0 {
		options.Rounds = DefaultRounds
	}

	if options.Capacity == 0 {
		options.Capacity = options.Streams
	}

	if options.Streams < 1 || options.Streams > MaxStreams || options.Rounds < 1 || options.Rounds > MaxRounds || options.Capacity < 1 || options.Capacity > options.Streams || options.Device < -1 {
		return stats, metrics, errors.New("invalid GPU device, stream, round or readback capacity")
	}

	err := ctx.Err()
	if err != nil {
		return stats, metrics, err
	}

	table := makeTable(plan)

	engine, err := openDevice(options.Device, options.Validation, options.Streams, options.Capacity, table, searchShader)
	if err != nil {
		return stats, metrics, err
	}

	defer engine.close()

	metrics.Device = engine.name

	worker := verifier{seeds: make([]seed, options.Streams), matcher: matcher, save: save}

	options.Monitor.Observe(func() search.Stats {
		saved := worker.saved.Load()

		return search.Stats{Checked: checked.Load(), Saved: saved}
	})

	commands := make([]command, options.Streams)

	for index := range worker.seeds {
		worker.seeds[index], commands[index], err = newSeed(1)
		if err != nil {
			return stats, metrics, err
		}
	}

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	candidates := make(chan discovered, options.Streams)
	refills := make(chan feedback, options.Streams)
	verified := make(chan error, 1)

	go func() {
		var verifyError error

		for candidate := range candidates {
			if verifyError != nil {
				continue
			}

			var reply feedback

			reply, verifyError = worker.verify(candidate)
			if verifyError != nil {
				options.Monitor.Stop()
				cancel()

				continue
			}

			refills <- reply
		}

		verified <- verifyError
	}()

	if options.Ready != nil {
		options.Ready(metrics.Device)
	}

	started := time.Now()

	var (
		inFlight    int
		nextSlot    int
		collectSlot int
		pending     uint32
	)

	for {
		stopping := workCtx.Err() != nil
		if stopping {
			options.Monitor.Stop()
		}

		if inFlight < 2 && !stopping {
			drainRefills(refills, commands)

			err = engine.submit(nextSlot, commands, options.Rounds, false)
			if err != nil {
				break
			}

			clear(commands)

			metrics.Submissions++
			metrics.UploadBytes += uint64(len(commands) * 132)

			inFlight++
			nextSlot ^= 1

			continue
		}

		if inFlight == 0 {
			if pending == 0 {
				break
			}

			// No acknowledgements during draining: paused streams stay paused.
			err = engine.submit(nextSlot, commands, 1, true)
			if err != nil {
				break
			}

			metrics.Submissions++
			metrics.UploadBytes += uint64(len(commands) * 132)

			inFlight++
			nextSlot ^= 1
		}

		var completed collection

		completed, err = engine.collect(collectSlot)
		if err != nil {
			break
		}

		inFlight--
		collectSlot ^= 1
		pending = completed.pending

		stats.Checked += uint64(completed.checked)
		checked.Store(stats.Checked)

		metrics.Execution += completed.elapsed
		metrics.Gaps += completed.gap
		metrics.ReadBytes += uint64(16 + len(completed.hits)*24)

		found := time.Now()

		for _, result := range completed.hits {
			candidates <- discovered{hit: result, found: found}
		}
	}

	options.Monitor.Stop()
	close(candidates)

	verifyError := <-verified

	stats.Saved = worker.saved.Load()
	metrics.Elapsed = time.Since(started)

	if verifyError != nil {
		return stats, metrics, verifyError
	}

	if err != nil {
		return stats, metrics, err
	}

	if engine.validationErrors() != 0 {
		return stats, metrics, errors.New("vulkan validation reported errors")
	}

	return stats, metrics, ctx.Err()
}

func drainRefills(refills <-chan feedback, commands []command) {
	for {
		select {
		case refill := <-refills:
			commands[refill.stream] = refill.command
		default:
			return
		}
	}
}
