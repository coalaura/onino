package search

import (
	"bytes"
	"context"
	"crypto/sha3"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

type searchResult struct {
	stats Stats
	err   error
}

func TestParallelHits(t *testing.T) {
	matcher := parallelMatcher(t, ".a", ".q")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	keys := make([]onion.Key, 0, batchSize*4)

	var active atomic.Int32

	options := Options{
		Workers: 4,
		Progress: func(Stats) {
			if active.Add(1) != 1 {
				t.Error("overlapping callbacks")
			}

			active.Add(-1)
		},
	}

	hooks := testParallelHooks()
	hooks.interval = time.Millisecond

	stats, err := runParallel(ctx, matcher, func(key onion.Key) error {
		if active.Add(1) != 1 {
			t.Error("overlapping callbacks")
		}

		keys = append(keys, key)
		if len(keys) == batchSize+1 {
			cancel()
		}

		active.Add(-1)

		return nil
	}, options, hooks)

	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}

	if stats.Saved != uint64(len(keys)) || stats.Checked != stats.Saved || stats.Checked%batchSize != 0 {
		t.Fatalf("inexact final batch counters: %+v, callbacks=%d", stats, len(keys))
	}

	secrets := make(map[[32]byte]bool, len(keys))
	publicKeys := make(map[[32]byte]bool, len(keys))

	for index := range keys {
		key := &keys[index]
		checkKey(t, key)

		nonce := [32]byte(key.Secret[32:])
		if secrets[nonce] || publicKeys[key.Public] {
			t.Fatal("workers or reseeded lanes shared a secret/public key")
		}

		secrets[nonce] = true
		publicKeys[key.Public] = true
	}
}

func TestParallelSaveFailure(t *testing.T) {
	matcher := parallelMatcher(t, ".a", ".q")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	want := errors.New("disk full")

	var calls int

	stats, err := RunWithOptions(ctx, matcher, func(onion.Key) error {
		calls++
		if calls == 19 {
			cancel()

			return want
		}

		return nil
	}, Options{Workers: 4})

	if !errors.Is(err, want) || calls != 19 || stats.Saved != 18 || stats.Checked < 19 || stats.Checked > 22 {
		t.Fatalf("save failure lost error or counts: %+v %v calls=%d", stats, err, calls)
	}
}

func TestParallelPinned(t *testing.T) {
	topology, err := cpu.Discover()
	if err != nil {
		t.Fatal(err)
	}

	if !topology.Known {
		t.Skip("physical CPU topology unavailable")
	}

	count := min(4, len(topology.CPUs))

	processors, err := cpu.Select(topology, count, true)
	if err != nil {
		t.Fatal(err)
	}

	matcher := parallelMatcher(t, ".a", ".q")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	calls := uint64(0)
	options := Options{Workers: count, CPUs: processors}

	stats, err := RunWithOptions(ctx, matcher, func(onion.Key) error {
		calls++
		cancel()

		return nil
	}, options)

	if !errors.Is(err, context.Canceled) || stats.Saved != calls || stats.Checked != calls || calls == 0 || calls%batchSize != 0 {
		t.Fatalf("pinned workers: %+v %v calls=%d", stats, err, calls)
	}
}

func TestParallelStartupAndCleanup(t *testing.T) {
	matcher := parallelMatcher(t, "unlikelyprefix.")

	var (
		restored    atomic.Int32
		initialized atomic.Int32
	)

	want := errors.New("affinity restoration failed")
	hooks := testParallelHooks()

	hooks.pin = func(cpu.CPU) (func() error, error) {
		return func() error {
			restored.Add(1)

			return want
		}, nil
	}

	hooks.create = func(int, *pattern.Matcher) (*worker, error) {
		initialized.Add(1)

		return nil, io.EOF
	}

	options := Options{Workers: 2, CPUs: []cpu.CPU{{Number: 0}, {Number: 1}}}

	stats, err := runParallel(context.Background(), matcher, cheapSave, options, hooks)
	if !errors.Is(err, io.EOF) || !errors.Is(err, want) || stats != (Stats{}) || restored.Load() != 2 || initialized.Load() < 1 {
		t.Fatalf("startup cleanup: %+v %v restored=%d initialized=%d", stats, err, restored.Load(), initialized.Load())
	}

	hooks.pin = func(cpu.CPU) (func() error, error) {
		return nil, want
	}

	stats, err = runParallel(context.Background(), matcher, cheapSave, options, hooks)
	if !errors.Is(err, want) || stats != (Stats{}) {
		t.Fatalf("pin error: %+v %v", stats, err)
	}
}

func TestParallelReseedFailure(t *testing.T) {
	matcher := parallelMatcher(t, ".a", ".q")
	hooks := testParallelHooks()

	hooks.create = func(index int, matcher *pattern.Matcher) (*worker, error) {
		state, err := deterministicWorker(index, matcher)
		if err != nil {
			return nil, err
		}

		state.walk.random = bytes.NewReader(nil)

		return state, nil
	}

	calls := 0

	stats, err := runParallel(context.Background(), matcher, func(onion.Key) error {
		calls++

		return nil
	}, Options{Workers: 2}, hooks)

	if !errors.Is(err, io.EOF) || stats.Saved != uint64(calls) || calls < 1 || calls > 2 || stats.Checked < uint64(calls) || stats.Checked > 2 {
		t.Fatalf("reseed failure: %+v %v calls=%d", stats, err, calls)
	}
}

func TestParallelBlockedSave(t *testing.T) {
	matcher := parallelMatcher(t, ".a", ".q")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan searchResult, 1)

	want := errors.New("blocked save failed")

	hooks := testParallelHooks()
	hooks.interval = time.Millisecond

	options := Options{Workers: 2, Progress: func(Stats) {}}

	go func() {
		stats, err := runParallel(ctx, matcher, func(onion.Key) error {
			close(entered)
			<-release

			return want
		}, options, hooks)

		done <- searchResult{stats: stats, err: err}
	}()

	<-entered
	cancel()

	select {
	case <-done:
		t.Fatal("returned while a synchronous save was in flight")
	case <-time.After(10 * time.Millisecond):
	}

	close(release)

	select {
	case result := <-done:
		if !errors.Is(result.err, want) || result.stats.Saved != 0 || result.stats.Checked < 1 || result.stats.Checked > 2 {
			t.Fatalf("blocked save result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown deadlocked")
	}
}

func TestParallelProgressAndCancellation(t *testing.T) {
	matcher := parallelMatcher(t, "unlikelyprefix.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var (
		previous Stats
		reports  int
	)

	hooks := testParallelHooks()
	hooks.interval = time.Millisecond

	options := Options{
		Workers: 2,
		Progress: func(stats Stats) {
			if stats.Checked < previous.Checked || stats.Saved > stats.Checked {
				t.Error("invalid cumulative progress")
			}

			previous = stats
			reports++

			if stats.Checked >= publishBatches*batchSize*2 {
				cancel()
			}
		},
	}

	stats, err := runParallel(ctx, matcher, cheapSave, options, hooks)
	if !errors.Is(err, context.Canceled) || reports == 0 || stats.Checked < previous.Checked || stats.Checked%batchSize != 0 {
		t.Fatalf("progress/final result: %+v %v", stats, err)
	}

	stats, err = RunWithOptions(ctx, matcher, cheapSave, Options{Workers: 4})
	if !errors.Is(err, context.Canceled) || stats != (Stats{}) {
		t.Fatalf("pre-canceled search: %+v %v", stats, err)
	}
}

func TestParallelOptions(t *testing.T) {
	matcher := parallelMatcher(t, "rare.")
	cases := []Options{
		{},
		{Workers: -1},
		{Workers: 2, CPUs: []cpu.CPU{{Number: 1}}},
		{Workers: 2, CPUs: []cpu.CPU{{Number: 1}, {Number: 1, Core: 2}}},
	}

	for _, options := range cases {
		_, err := RunWithOptions(context.Background(), matcher, cheapSave, options)
		if err == nil {
			t.Fatalf("accepted invalid options: %+v", options)
		}
	}
}

func TestParallelHitPathAllocations(t *testing.T) {
	matcher := parallelMatcher(t, ".a", ".q")

	state, err := deterministicWorker(0, matcher)
	if err != nil {
		t.Fatal(err)
	}

	run := parallelRun{save: cheapSave}
	save := run.saveKey

	var (
		stats Stats
		slot  counterSlot
	)

	allocations := testing.AllocsPerRun(10, func() {
		err = state.searchBatch(matcher, save, &stats)
		if err != nil {
			t.Fatal(err)
		}

		slot.publish(stats)
	})

	if allocations != 0 {
		t.Fatalf("steady-state hit/publication path allocated %v times", allocations)
	}
}

func TestSecureWorkersIndependent(t *testing.T) {
	matcher := parallelMatcher(t, "unlikelyprefix.")

	first, err := createSecureWorker(0, matcher)
	if err != nil {
		t.Fatal(err)
	}

	second, err := createSecureWorker(1, matcher)
	if err != nil {
		t.Fatal(err)
	}

	if first.paired == second.paired || first.paired.secrets == second.paired.secrets {
		t.Fatal("workers share a generator or seed stream")
	}

	before := *second.paired

	var stats Stats

	err = first.searchBatch(matcher, cheapSave, &stats)
	if err != nil {
		t.Fatal(err)
	}

	err = first.paired.reseed(0)
	if err != nil {
		t.Fatal(err)
	}

	if *second.paired != before {
		t.Fatal("advancing/reseeding one worker changed its peer's pending candidates or scratch")
	}
}

func TestParallelBlockedReport(t *testing.T) {
	matcher := parallelMatcher(t, "unlikelyprefix.")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	done := make(chan searchResult, 1)
	hooks := testParallelHooks()
	hooks.interval = time.Millisecond

	options := Options{
		Workers: 2,
		Progress: func(Stats) {
			select {
			case entered <- struct{}{}:
			default:
			}

			<-release
		},
	}

	go func() {
		stats, err := runParallel(ctx, matcher, cheapSave, options, hooks)
		done <- searchResult{stats: stats, err: err}
	}()

	<-entered
	cancel()
	close(release)

	select {
	case result := <-done:
		if !errors.Is(result.err, context.Canceled) || result.stats.Checked%batchSize != 0 {
			t.Fatalf("blocked report result: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report shutdown deadlocked")
	}
}

func parallelMatcher(t *testing.T, patterns ...string) *pattern.Matcher {
	t.Helper()

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	return matcher
}

func testParallelHooks() parallelHooks {
	return parallelHooks{create: deterministicWorker, pin: cpu.Pin, interval: progressInterval}
}

func deterministicWorker(index int, matcher *pattern.Matcher) (*worker, error) {
	random := sha3.NewSHAKE256()
	seed := [8]byte{byte(index), byte(index >> 8)}
	random.Write(seed[:])

	return newWorker(random, matcher)
}

//go:noinline
func cheapSave(onion.Key) error {
	return nil
}
