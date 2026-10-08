package search

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
	"github.com/coalaura/onino/internal/simd"
)

const publishBatches = 128

// Options selects persistent search workers. An empty CPUs slice leaves
// placement to the OS; otherwise it must contain one distinct CPU per worker.
// Progress and save callbacks are serialized and never overlap. Callbacks must
// return for shutdown to complete; cancellation does not interrupt a save.
// Monitor exposes published counters for an external reporting coordinator.
type Options struct {
	Workers  int
	CPUs     []cpu.CPU
	Progress func(Stats)
	Monitor  *Monitor
	SIMD     simd.Mode
	Config   *Configuration
	// Ready runs once when the first queued/parallel worker is initialized.
	Ready func()
}

// The unused tail separates live counters even when the allocation itself is
// not cache-line aligned. Workers keep ordinary counters in their own stacks.
type counterSlot struct {
	checked atomic.Uint64
	saved   atomic.Uint64
	_       [240]byte
}

type parallelHooks struct {
	create   func(int, *pattern.Matcher) (*worker, error)
	pin      func(cpu.CPU) (func() error, error)
	interval time.Duration
}

type parallelRun struct {
	ctx       context.Context
	matcher   *pattern.Matcher
	save      SaveFunc
	options   Options
	hooks     parallelHooks
	slots     []counterSlot
	done      chan struct{}
	queue     *saveQueue
	callbacks sync.Mutex
	failure   sync.Mutex
	stopped   atomic.Bool
	ready     sync.Once
	err       error
}

var errPeerStopped = errors.New("another search worker failed")

func (slot *counterSlot) publish(stats Stats) {
	slot.checked.Store(stats.Checked)
	slot.saved.Store(stats.Saved)
}

func (run *parallelRun) totals() Stats {
	var stats Stats

	if run.queue != nil {
		stats.Saved = run.queue.saved.Load()
	}

	for index := range run.slots {
		// Read saved first so a snapshot never counts a save before its check.
		stats.Saved += run.slots[index].saved.Load()
		stats.Checked += run.slots[index].checked.Load()
	}

	return stats
}

func (run *parallelRun) fail(err error) {
	if err == nil || errors.Is(err, errPeerStopped) {
		return
	}

	run.failure.Lock()
	defer run.failure.Unlock()

	run.err = errors.Join(run.err, err)
	run.stopped.Store(true)
	run.options.Monitor.Stop()
}

func (run *parallelRun) saveKey(key onion.Key) error {
	run.callbacks.Lock()
	defer run.callbacks.Unlock()

	if run.stopped.Load() {
		return errPeerStopped
	}

	err := run.save(key)
	if err != nil {
		// Publish failure before allowing another worker into the callback.
		run.fail(fmt.Errorf("save matching key: %w", err))

		return errPeerStopped
	}

	return err
}

func (run *parallelRun) work(index int) {
	var stats Stats

	defer func() {
		run.slots[index].publish(stats)
		run.done <- struct{}{}
	}()

	if len(run.options.CPUs) != 0 {
		restore, err := run.hooks.pin(run.options.CPUs[index])
		if err != nil {
			run.fail(fmt.Errorf("pin worker %d: %w", index+1, err))

			return
		}

		defer func() {
			err := restore()
			if err != nil {
				run.fail(fmt.Errorf("restore worker %d affinity: %w", index+1, err))
			}
		}()
	}

	if run.stopped.Load() || run.ctx.Err() != nil {
		return
	}

	state, err := run.hooks.create(index, run.matcher)
	if err != nil {
		run.fail(fmt.Errorf("initialize worker %d: %w", index+1, err))

		return
	}

	if run.options.Ready != nil {
		run.ready.Do(run.options.Ready)
	}

	save := run.saveKey
	remaining := publishBatches

	if run.queue != nil {
		sink := matchSink{queue: run.queue, slot: &run.slots[index]}

		if state.paired != nil {
			state.paired.sink = sink
		} else if state.accelerated != nil {
			state.accelerated.setSink(sink)
		} else {
			state.walk.sink = sink
		}
	}

	for !run.stopped.Load() && run.ctx.Err() == nil {
		err = state.searchBatch(run.matcher, save, &stats)
		if err != nil {
			run.fail(err)

			return
		}

		remaining--
		if remaining == 0 {
			run.slots[index].publish(stats)
			remaining = publishBatches
		}
	}
}

// RunWithOptions preserves the direct single-worker path when neither placement
// nor monitoring is requested. The application owns GOMAXPROCS.
func RunWithOptions(ctx context.Context, matcher *pattern.Matcher, save SaveFunc, options Options) (Stats, error) {
	if matcher == nil || save == nil {
		return Stats{}, errors.New("search requires a matcher and a save function")
	}

	err := validateOptions(options)
	if err != nil {
		return Stats{}, err
	}

	if options.Workers == 1 && len(options.CPUs) == 0 && options.Monitor == nil && options.Config == nil {
		return runWithSIMD(ctx, matcher, save, options.Progress, options.SIMD)
	}

	err = ctx.Err()
	if err != nil {
		return Stats{}, err
	}

	config, err := options.resolve(matcher)
	if err != nil {
		return Stats{}, err
	}

	hooks := secureHooks(config)

	return runParallel(ctx, matcher, save, options, hooks)
}

func runParallel(ctx context.Context, matcher *pattern.Matcher, save SaveFunc, options Options, hooks parallelHooks) (Stats, error) {
	return runWorkers(ctx, matcher, save, options, hooks, nil)
}

func runWorkers(ctx context.Context, matcher *pattern.Matcher, save SaveFunc, options Options, hooks parallelHooks, queue *saveQueue) (Stats, error) {
	defer options.Monitor.Stop()

	run := parallelRun{
		ctx:     ctx,
		matcher: matcher,
		save:    save,
		options: options,
		hooks:   hooks,
		slots:   make([]counterSlot, options.Workers),
		done:    make(chan struct{}, options.Workers),
		queue:   queue,
	}

	options.Monitor.Observe(run.totals)

	var ticks <-chan time.Time

	if options.Progress != nil {
		ticker := time.NewTicker(hooks.interval)
		defer ticker.Stop()

		ticks = ticker.C
	}

	if queue != nil {
		go run.saveMatches()
	}

	for index := range options.Workers {
		go run.work(index)
	}

	for remaining := options.Workers; remaining > 0; {
		select {
		case <-run.done:
			options.Monitor.Stop()
			remaining--
		case <-ticks:
			// A blocked save must not trap the coordinator on a mutex. A
			// report can wait until the next tick; successful saves cannot.
			if run.callbacks.TryLock() {
				options.Progress(run.totals())
				run.callbacks.Unlock()
			}
		}
	}

	if queue != nil {
		close(queue.matches)
		<-queue.done
	}

	if run.err != nil {
		return run.totals(), run.err
	}

	return run.totals(), ctx.Err()
}

func createSecureWorker(_ int, matcher *pattern.Matcher) (*worker, error) {
	// Each generator obtains independent seeds directly from the OS CSPRNG.
	return newWorker(rand.Reader, matcher)
}

func secureHooks(config Configuration) parallelHooks {
	return parallelHooks{
		create: func(_ int, matcher *pattern.Matcher) (*worker, error) {
			return newWorkerWithConfig(rand.Reader, matcher, config)
		},
		pin:      cpu.Pin,
		interval: progressInterval,
	}
}

func validateOptions(options Options) error {
	if options.SIMD > simd.IFMA {
		return errors.New("invalid SIMD mode")
	}

	if options.Workers < 1 {
		return errors.New("search requires at least one worker")
	}

	if len(options.CPUs) == 0 {
		return nil
	}

	if len(options.CPUs) != options.Workers {
		return errors.New("placement requires one CPU per worker")
	}

	seen := make(map[cpu.CPU]bool, options.Workers)

	for _, processor := range options.CPUs {
		identity := cpu.CPU{Group: processor.Group, Number: processor.Number}
		if seen[identity] {
			return errors.New("placement requires distinct logical CPUs")
		}

		seen[identity] = true
	}

	return nil
}
