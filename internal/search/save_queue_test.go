package search

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

type discoveredKey struct {
	key   onion.Key
	found time.Time
}

func TestQueuedBackpressureAndDrain(t *testing.T) {
	modes := []string{"walk", "paired"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				matcher := parallelMatcher(t, allSuffixPatterns(1)...)
				state := queuedTestWorker(t, mode, matcher)

				hooks := testParallelHooks()
				hooks.interval = time.Millisecond

				hooks.create = func(int, *pattern.Matcher) (*worker, error) {
					return state, nil
				}

				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				started := time.Now()

				release := make(chan struct{})
				done := make(chan searchResult, 1)
				keys := make([]discoveredKey, 0, batchSize)

				var active atomic.Int32

				options := Options{
					Workers: 1,
					Progress: func(stats Stats) {
						if active.Add(1) != 1 || stats.Saved > stats.Checked {
							t.Error("overlapping callbacks or inconsistent counters")
						}

						active.Add(-1)
					},
				}

				go func() {
					stats, err := runQueued(ctx, matcher, func(key onion.Key, found time.Time) error {
						if active.Add(1) != 1 {
							t.Error("overlapping callbacks")
						}

						keys = append(keys, discoveredKey{key: key, found: found})
						if len(keys) == 1 {
							<-release
						}

						active.Add(-1)

						return nil
					}, options, hooks)

					done <- searchResult{stats: stats, err: err}
				}()

				synctest.Wait()

				var sink *matchSink

				if state.paired != nil {
					sink = &state.paired.sink
				} else {
					sink = &state.walk.sink
				}

				if len(keys) != 1 || len(sink.queue.matches) != saveQueueCapacity || sink.slot.checked.Load() != saveQueueCapacity+2 || sink.queue.saved.Load() != 0 {
					t.Fatal("worker did not fill the bounded queue while saving was blocked")
				}

				// The blocked match's timestamp must also precede the queue delay.
				time.Sleep(time.Second)

				cancel()
				close(release)

				synctest.Wait()

				result := <-done
				if !errors.Is(result.err, context.Canceled) || result.stats != (Stats{Checked: batchSize, Saved: batchSize}) || len(keys) != batchSize {
					t.Fatalf("queue did not drain the current batch: %+v, keys=%d", result, len(keys))
				}

				nonces := make(map[[32]byte]bool, len(keys))
				publicKeys := make(map[[32]byte]bool, len(keys))

				for index := range keys {
					match := &keys[index]
					checkKey(t, &match.key)

					nonce := [32]byte(match.key.Secret[32:])
					if nonces[nonce] || publicKeys[match.key.Public] {
						t.Fatal("queued matches shared a seed or public key")
					}

					nonces[nonce] = true
					publicKeys[match.key.Public] = true
					expected := started

					if index >= saveQueueCapacity+2 {
						expected = started.Add(time.Second)
					}

					if !match.found.Equal(expected) {
						t.Fatalf("match %d lost discovery time: %v, want %v", index, match.found, expected)
					}
				}
			})
		})
	}
}

func TestQueuedConcurrentHits(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher := parallelMatcher(t, allSuffixPatterns(2)...)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		release := make(chan struct{})
		done := make(chan searchResult, 1)
		keys := make([]onion.Key, 0, 4*batchSize)

		go func() {
			stats, err := runQueued(ctx, matcher, func(key onion.Key, _ time.Time) error {
				keys = append(keys, key)
				if len(keys) == 1 {
					<-release
				}

				return nil
			}, Options{Workers: 4}, testParallelHooks())

			done <- searchResult{stats: stats, err: err}
		}()

		synctest.Wait()

		cancel()
		close(release)

		synctest.Wait()

		result := <-done
		if !errors.Is(result.err, context.Canceled) || result.stats != (Stats{Checked: 4 * batchSize, Saved: 4 * batchSize}) || len(keys) != 4*batchSize {
			t.Fatalf("concurrent drain lost matches: %+v, keys=%d", result, len(keys))
		}

		nonces := make(map[[32]byte]bool, len(keys))

		for index := range keys {
			checkKey(t, &keys[index])

			nonce := [32]byte(keys[index].Secret[32:])
			if nonces[nonce] {
				t.Fatal("workers saved related keys")
			}

			nonces[nonce] = true
		}
	})
}

func TestQueuedSaveFailureUnblocksWorkers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher := parallelMatcher(t, allSuffixPatterns(1)...)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		release := make(chan struct{})
		done := make(chan searchResult, 1)
		want := errors.New("disk full")
		calls := 0

		go func() {
			stats, err := runQueued(ctx, matcher, func(onion.Key, time.Time) error {
				calls++
				<-release

				return want
			}, Options{Workers: 2}, testParallelHooks())

			done <- searchResult{stats: stats, err: err}
		}()

		synctest.Wait()

		cancel()

		synctest.Wait()

		select {
		case <-done:
			t.Fatal("returned before the in-flight save completed")
		default:
		}

		close(release)

		synctest.Wait()

		result := <-done
		if !errors.Is(result.err, want) || calls != 1 || result.stats != (Stats{Checked: saveQueueCapacity + 3}) {
			t.Fatalf("save failure lost error or exact backpressure counters: %+v, calls=%d", result, calls)
		}
	})
}

func TestQueuedReseedFailureDrainsMatch(t *testing.T) {
	failures := []error{nil, errors.New("save failed after reseed failure")}

	for _, failure := range failures {
		matcher := parallelMatcher(t, allSuffixPatterns(1)...)
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

		stats, err := runQueued(context.Background(), matcher, func(key onion.Key, found time.Time) error {
			checkKey(t, &key)
			calls++

			return failure
		}, Options{Workers: 1}, hooks)

		expected := Stats{Checked: 1, Saved: 1}

		if failure != nil {
			expected.Saved = 0

			if !errors.Is(err, failure) {
				t.Fatalf("lost save failure: %v", err)
			}
		}

		if !errors.Is(err, io.EOF) || stats != expected || calls != 1 {
			t.Fatalf("lost accepted match after reseed failure: %+v %v, calls=%d", stats, err, calls)
		}
	}
}

func TestQueuedSnapshotsSurviveReseed(t *testing.T) {
	state := testPairedGenerator(t)

	for range pairedOffsets + 1 {
		err := state.nextBatch()
		if err != nil {
			t.Fatal(err)
		}
	}

	matches := make([]candidate, batchSize)

	for index := range matches {
		state.completeSign(index)
		matches[index] = state.snapshot(index)
	}

	err := state.reset()
	if err != nil {
		t.Fatal(err)
	}

	clear(state.publicKeys[:])

	for index := range matches {
		key := matches[index].key()
		checkKey(t, &key)
	}
}

func TestQueuedHitPathAllocations(t *testing.T) {
	matcher := parallelMatcher(t, allSuffixPatterns(1)...)
	modes := []string{"walk", "paired"}

	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			state := queuedTestWorker(t, mode, matcher)

			queue := &saveQueue{
				matches: make(chan candidate, saveQueueCapacity),
				failed:  make(chan struct{}),
				done:    make(chan struct{}),
				save: func(key onion.Key, _ time.Time) error {
					return cheapSave(key)
				},
			}

			run := parallelRun{queue: queue}

			var (
				stats Stats
				slot  counterSlot
			)

			sink := matchSink{queue: queue, slot: &slot}

			if state.paired != nil {
				state.paired.sink = sink
			} else {
				state.walk.sink = sink
			}

			go run.saveMatches()

			allocations := testing.AllocsPerRun(10, func() {
				err := state.searchBatch(matcher, nil, &stats)
				if err != nil {
					t.Fatal(err)
				}
			})

			close(queue.matches)
			<-queue.done

			if allocations != 0 || queue.saved.Load() != stats.Checked || stats.Saved != 0 {
				t.Fatalf("queued hit path: allocs=%g stats=%+v saved=%d", allocations, stats, queue.saved.Load())
			}
		})
	}
}

func TestRunQueuedValidation(t *testing.T) {
	matcher := parallelMatcher(t, "rare.")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stats, err := RunQueued(ctx, matcher, func(onion.Key, time.Time) error {
		t.Fatal("pre-canceled search saved a key")

		return nil
	}, Options{Workers: 1})

	if !errors.Is(err, context.Canceled) || stats != (Stats{}) {
		t.Fatalf("pre-canceled queue: %+v %v", stats, err)
	}

	_, err = RunQueued(context.Background(), matcher, nil, Options{Workers: 1})
	if err == nil {
		t.Fatal("accepted missing save callback")
	}
}

func queuedTestWorker(t *testing.T, mode string, matcher *pattern.Matcher) *worker {
	t.Helper()

	if mode == "paired" {
		return &worker{paired: testPairedGenerator(t)}
	}

	state, err := deterministicWorker(0, matcher)
	if err != nil {
		t.Fatal(err)
	}

	return state
}
