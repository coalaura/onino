package search

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/coalaura/onino/internal/cpu"
	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

const saveQueueCapacity = 64

// SaveMatchFunc persists a finalized match and its original discovery time.
// Calls are serialized with progress callbacks. Returning an error stops search.
type SaveMatchFunc func(key onion.Key, found time.Time) error

// A candidate owns its secrets; it must never refer back to a reseeded worker.
type candidate struct {
	public [32]byte
	secret [64]byte
	steps  uint64
	found  time.Time
}

type matchSink struct {
	queue *saveQueue
	slot  *counterSlot
}

type saveQueue struct {
	matches chan candidate
	failed  chan struct{}
	done    chan struct{}
	save    SaveMatchFunc
	saved   atomic.Uint64
}

func (match candidate) key() onion.Key {
	return onion.Key{Public: match.public, Secret: offsetSecret(match.secret, match.steps)}
}

func (sink *matchSink) submit(match candidate, save SaveFunc, stats *Stats) error {
	if sink.queue == nil {
		err := save(match.key())
		if err != nil {
			return fmt.Errorf("save matching key: %w", err)
		}

		stats.Saved++

		return nil
	}

	match.found = time.Now()

	// Publish the check before the saver can publish its successful save.
	sink.slot.publish(*stats)

	return sink.queue.enqueue(match)
}

func (queue *saveQueue) enqueue(match candidate) error {
	select {
	case <-queue.failed:
		return errPeerStopped
	default:
	}

	// Cancellation finishes the current batch and drains accepted matches.
	// Only a save failure may abandon a match waiting for queue capacity.
	select {
	case queue.matches <- match:
		return nil
	case <-queue.failed:
		return errPeerStopped
	}
}

func (run *parallelRun) saveMatches() {
	queue := run.queue
	defer close(queue.done)

	for match := range queue.matches {
		run.callbacks.Lock()

		err := queue.save(match.key(), match.found)
		if err != nil {
			run.fail(fmt.Errorf("save matching key: %w", err))
			close(queue.failed)
			run.callbacks.Unlock()

			return
		}

		queue.saved.Add(1)
		run.callbacks.Unlock()
	}
}

// RunQueued searches with a bounded queue and one dedicated saver, including
// when only one search worker is requested. Workers reseed after enqueueing.
// Cancellation finishes in-flight batches and drains the queue before returning.
// A save failure stops workers and further saves; Saved counts only successes.
// Callbacks must return for shutdown to complete. It does not change GOMAXPROCS.
func RunQueued(ctx context.Context, matcher *pattern.Matcher, save SaveMatchFunc, options Options) (Stats, error) {
	if matcher == nil || save == nil {
		return Stats{}, errors.New("search requires a matcher and a save function")
	}

	err := validateOptions(options)
	if err != nil {
		return Stats{}, err
	}

	err = ctx.Err()
	if err != nil {
		return Stats{}, err
	}

	hooks := parallelHooks{create: createSecureWorker, pin: cpu.Pin, interval: progressInterval}

	return runQueued(ctx, matcher, save, options, hooks)
}

func runQueued(ctx context.Context, matcher *pattern.Matcher, save SaveMatchFunc, options Options, hooks parallelHooks) (Stats, error) {
	queue := &saveQueue{
		matches: make(chan candidate, saveQueueCapacity),
		failed:  make(chan struct{}),
		done:    make(chan struct{}),
		save:    save,
	}

	return runWorkers(ctx, matcher, nil, options, hooks, queue)
}
