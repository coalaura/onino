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
	Device      int
	Validation  bool
	Streams     int
	Rounds      int
	Capacity    int
	AutoStreams bool
	AutoRounds  bool
	Started     time.Time
	Monitor     *search.Monitor
	Diagnostic  func(string)
	Selected    func(device string, streams, rounds int, first, selected time.Duration)
	Initialized func(device string, initialization time.Duration)
	// Ready runs once after the first useful submission has been accepted.
	Ready func(device string)
}

type Metrics struct {
	Device         string
	Execution      time.Duration
	Elapsed        time.Duration
	Gaps           time.Duration
	Submissions    uint64
	UploadBytes    uint64
	ReadBytes      uint64
	Copy           time.Duration
	Record         time.Duration
	Submit         time.Duration
	Tail           time.Duration
	GapTail        time.Duration
	Recordings     uint64
	Hits           uint64
	Pending        uint32
	Backlog        int
	ReadbackBytes  uint64
	FirstWork      time.Duration
	Selection      time.Duration
	StopSubmitting time.Duration
	Drain          time.Duration
	Streams        int
	Rounds         int
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

	maximum := options.Streams

	if options.AutoStreams {
		maximum = MaxStreams
	}

	if options.Capacity == 0 {
		options.Capacity = min(maximum, 256)
	}

	if options.Streams < 1 || options.Streams > MaxStreams || options.Rounds < 1 || options.Rounds > MaxRounds || options.Capacity < 1 || options.Capacity > maximum || options.Device < -1 {
		return stats, metrics, errors.New("invalid GPU device, stream, round or readback capacity")
	}

	err := ctx.Err()
	if err != nil {
		return stats, metrics, err
	}

	if options.Started.IsZero() {
		options.Started = time.Now()
	}

	table := makeTable(plan)
	allocation := maximum

	if options.AutoStreams {
		allocation = 0
	}

	initializing := time.Now()

	engine, err := openDevice(options.Device, options.Validation, allocation, options.Capacity, table, searchShader)
	if err != nil {
		return stats, metrics, err
	}

	defer engine.close()

	maximum = engine.streams

	metrics.Device = engine.name

	if options.Initialized != nil {
		options.Initialized(metrics.Device, time.Since(initializing))
	}

	if options.Diagnostic != nil {
		engine.diagnostics(options.Diagnostic)
		options.Diagnostic(engine.memory())
	}

	worker := verifier{seeds: make([]seed, maximum), matcher: matcher, save: save}

	options.Monitor.Observe(func() search.Stats {
		saved := worker.saved.Load()

		return search.Stats{Checked: checked.Load(), Saved: saved}
	})

	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	candidates := make(chan discovered, maximum)
	refills := make(chan feedback, maximum)
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

	controller := execution{
		ctx:         workCtx,
		options:     options,
		engine:      engine,
		worker:      &worker,
		stats:       &stats,
		metrics:     &metrics,
		checked:     &checked,
		commands:    make([]command, maximum),
		candidates:  candidates,
		refills:     refills,
		firstUpdate: maximum,
		lastUpdate:  maximum,
		started:     time.Now(),
	}

	tuner := calibration{
		options:    options,
		deadline:   options.Started.Add(startupBudget),
		maximum:    maximum,
		acceptance: plan.acceptance(),
		measure:    controller.measure,
		now:        time.Now,
	}

	selected, err := tuner.selectInitial()
	if err == nil {
		metrics.Streams = selected.configuration.streams
		metrics.Rounds = selected.configuration.rounds
		metrics.Selection = time.Since(options.Started)

		if options.Selected != nil {
			options.Selected(metrics.Device, metrics.Streams, metrics.Rounds, metrics.FirstWork, metrics.Selection)
		}

		err = controller.search(selected)
	}

	stopped := time.Now()
	drainError := controller.drain()

	if err == nil {
		err = drainError
	}

	options.Monitor.Stop()
	close(candidates)

	verifyError := <-verified

	stats.Saved = worker.saved.Load()

	metrics.Elapsed = time.Since(controller.started)
	metrics.Drain = time.Since(stopped)

	engine.costs(&metrics)

	if options.Diagnostic != nil {
		options.Diagnostic(fmt.Sprintf("overall device_mps=%.3f (summed GPU execution %.6fs) backend_active_mps=%.3f (controller wall %.6fs) submissions=%d", float64(stats.Checked)/max(metrics.Execution.Seconds(), 1e-9)/1e6, metrics.Execution.Seconds(), float64(stats.Checked)/max(metrics.Elapsed.Seconds(), 1e-9)/1e6, metrics.Elapsed.Seconds(), metrics.Submissions))
		options.Diagnostic(fmt.Sprintf("lifecycle first_work_ms=%.3f selected_ms=%.3f stopped_ms=%.3f drain_ms=%.3f recordings=%d upload_bytes=%d readback_bytes=%d", float64(metrics.FirstWork)/1e6, float64(metrics.Selection)/1e6, float64(metrics.StopSubmitting)/1e6, float64(metrics.Drain)/1e6, metrics.Recordings, metrics.UploadBytes, metrics.ReadbackBytes))
	}

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

func drainUpdates(refills <-chan feedback, commands []command, first, last int) (int, int) {
	for {
		select {
		case refill := <-refills:
			commands[refill.stream] = refill.command

			if first == len(commands) {
				last = int(refill.stream) + 1
			}

			first = min(first, int(refill.stream))
			last = max(last, int(refill.stream)+1)
		default:
			return first, last
		}
	}
}
