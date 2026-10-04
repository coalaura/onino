// Package search implements a single-worker, continuous vanity-key search.
package search

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

// Stats counts candidates checked and matches successfully saved.
type Stats struct {
	Checked uint64
	Saved   uint64
}

// SaveFunc persists a match synchronously. The key is passed by value so the
// recipient may retain it. Returning an error stops the search.
type SaveFunc func(key onion.Key) error

func (state *generator) searchBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	filter := matcher.SignFilter()

	state.nextBatch(filter != nil)

	for index := range state.publicKeys {
		stats.Checked++

		if !state.matches(index, matcher, filter) {
			continue
		}

		err := save(state.key(index))
		if err != nil {
			return fmt.Errorf("save matching key: %w", err)
		}

		stats.Saved++

		// Each saved lane must start a new independent walk before advancing.
		err = state.reseed(index)
		if err != nil {
			return err
		}
	}

	return nil
}

// Run searches until cancellation or an error. It uses one worker, checks every
// candidate with matcher, and keeps searching after each successfully saved key.
// Matching is over the public key's standalone 52-symbol base32 encoding.
func Run(ctx context.Context, matcher *pattern.Matcher, save SaveFunc) (Stats, error) {
	return RunWithProgress(ctx, matcher, save, nil)
}

// RunWithProgress is Run with an optional synchronous progress callback, called
// about every four seconds at a batch boundary on the same search worker.
func RunWithProgress(ctx context.Context, matcher *pattern.Matcher, save SaveFunc, report func(Stats)) (Stats, error) {
	var stats Stats

	if matcher == nil || save == nil {
		return stats, errors.New("search requires a matcher and a save function")
	}

	err := ctx.Err()
	if err != nil {
		return stats, err
	}

	state, err := newWorker(rand.Reader, matcher)
	if err != nil {
		return stats, err
	}

	reporter := progressReporter{report: report}

	if report != nil {
		reporter.next = time.Now().Add(progressInterval)
	}

	for {
		err = ctx.Err()
		if err != nil {
			return stats, err
		}

		err = state.searchBatch(matcher, save, &stats)
		if err != nil {
			return stats, err
		}

		reporter.update(stats)
	}
}
