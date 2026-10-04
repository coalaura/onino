// Package search implements a single-worker, continuous vanity-key search.
package search

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

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

// Run searches until cancellation or an error. It uses one worker, checks every
// candidate with matcher, and keeps searching after each successfully saved key.
// Matching is over the public key's standalone 52-symbol base32 encoding.
func Run(ctx context.Context, matcher *pattern.Matcher, save SaveFunc) (Stats, error) {
	var stats Stats

	if matcher == nil || save == nil {
		return stats, errors.New("search requires a matcher and a save function")
	}

	err := ctx.Err()
	if err != nil {
		return stats, err
	}

	state := generator{random: rand.Reader}

	err = state.reset()
	if err != nil {
		return stats, err
	}

	for {
		err = ctx.Err()
		if err != nil {
			return stats, err
		}

		if state.round == reseedRounds {
			err = state.reset()
			if err != nil {
				return stats, err
			}
		}

		state.next()

		for index := range state.publicKeys {
			stats.Checked++

			if !matcher.Match(state.publicKeys[index]) {
				continue
			}

			err = save(state.key(index))
			if err != nil {
				return stats, fmt.Errorf("save matching key: %w", err)
			}

			stats.Saved++

			// Never export two related scalars from the same walk. Other lanes
			// are independently seeded, so every hit in this batch can be saved.
			err = state.reseed(index)
			if err != nil {
				return stats, err
			}
		}
	}
}
