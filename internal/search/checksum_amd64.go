//go:build !purego

package search

import "github.com/coalaura/onino/internal/pattern"

// These loops keep checksum selection outside the original per-candidate paths.
func (state *generator) searchChecksumBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	filter := matcher.SignFilter()

	state.nextBatch(filter != nil)

	for index := range state.publicKeys {
		stats.Checked++

		if filter != nil {
			if !filter.Match(state.publicKeys[index]) {
				continue
			}

			completeSign(&state.points[index], &state.products[index], &state.publicKeys[index])
		}

		if !matcher.MatchChecksum(state.publicKeys[index]) {
			continue
		}

		err := state.sink.submit(state.snapshot(index), save, stats)
		if err != nil {
			return err
		}

		err = state.reseed(index)
		if err != nil {
			return err
		}
	}

	return nil
}

func (state *pairedGenerator) searchChecksumBatch(matcher *pattern.Matcher, save SaveFunc, stats *Stats) error {
	filter := matcher.SignFilter()

	for range batchSize {
		if state.cursor == batchSize {
			err := state.nextBatch()
			if err != nil {
				return err
			}
		}

		index := state.cursor

		state.cursor++
		stats.Checked++

		if filter != nil && !filter.Match(state.publicKeys[index]) {
			continue
		}

		state.completeSign(index)

		if !matcher.MatchChecksum(state.publicKeys[index]) {
			continue
		}

		err := state.sink.submit(state.snapshot(index), save, stats)
		if err != nil {
			return err
		}

		state.cursor += 1 - (index & 1)

		err = state.reseed(index / 2)
		if err != nil {
			return err
		}
	}

	return nil
}
