package search

import (
	"strings"
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

func TestIndependentVisibleSuffixBatch(t *testing.T) {
	for length := 1; length <= 4; length++ {
		state := testGenerator(t)

		reference := *state
		reference.next()

		var keys [batchSize]onion.Key

		for index := range keys {
			keys[index] = reference.key(index)
		}

		patterns := visibleSuffixPatterns(keys[:], length)

		expected := make(map[[32]byte]onion.Key, len(keys))

		for index := range keys {
			if visibleSuffixMatches(&keys[index], patterns) {
				expected[keys[index].Public] = keys[index]
			}
		}

		checkVisibleSuffixBatch(t, state.searchBatch, patterns, expected)
	}
}

func TestPairedVisibleSuffixBatch(t *testing.T) {
	for length := 1; length <= 4; length++ {
		state := testPairedGenerator(t)
		reference := testPairedGenerator(t)

		err := reference.nextBatch()
		if err != nil {
			t.Fatal(err)
		}

		var keys [batchSize]onion.Key

		for index := range keys {
			reference.completeSign(index)
			keys[index] = reference.key(index)
		}

		patterns := visibleSuffixPatterns(keys[:], length)

		expected := make(map[[32]byte]onion.Key, len(keys))

		// Follow the engine's seed-replacement schedule, but eagerly complete every
		// sign and compare actual hostname strings instead of compiled filters.
		for range batchSize {
			if reference.cursor == batchSize {
				err = reference.nextBatch()
				if err != nil {
					t.Fatal(err)
				}
			}

			index := reference.cursor

			reference.cursor++
			reference.completeSign(index)

			key := reference.key(index)
			if !visibleSuffixMatches(&key, patterns) {
				continue
			}

			expected[key.Public] = key
			reference.cursor += 1 - (index & 1)

			err = reference.reseed(index / 2)
			if err != nil {
				t.Fatal(err)
			}
		}

		checkVisibleSuffixBatch(t, state.searchBatch, patterns, expected)
	}
}

func checkVisibleSuffixBatch(t *testing.T, searchBatch func(*pattern.Matcher, SaveFunc, *Stats) error, patterns []string, expected map[[32]byte]onion.Key) {
	t.Helper()

	matcher, err := pattern.CompilePatterns(patterns)
	if err != nil {
		t.Fatal(err)
	}

	wantSaved := uint64(len(expected))

	var stats Stats

	err = searchBatch(matcher, func(key onion.Key) error {
		if want, exists := expected[key.Public]; !exists || want != key {
			t.Fatalf("unexpected, duplicate or incomplete key: %s", key.Hostname())
		}

		checkKey(t, &key)
		delete(expected, key.Public)

		return nil
	}, &stats)

	if err != nil {
		t.Fatal(err)
	}

	if len(expected) != 0 || stats.Checked != batchSize || stats.Saved != wantSaved {
		t.Fatalf("lost visible-suffix matches: patterns %q, stats %+v, want %d saves, %d missing", patterns, stats, wantSaved, len(expected))
	}
}

func visibleSuffixPatterns(keys []onion.Key, length int) []string {
	patterns := make([]string, 0, 8)

	for index := 0; index < len(keys); index += batchSize / 8 {
		hostname := keys[index].Hostname()
		patterns = append(patterns, "."+hostname[52-length:52])
	}

	return patterns
}

func visibleSuffixMatches(key *onion.Key, patterns []string) bool {
	hostname := key.Hostname()

	for _, text := range patterns {
		if strings.HasSuffix(hostname[:52], text[1:]) {
			return true
		}
	}

	return false
}

func allSuffixPatterns(length int) []string {
	alphabet := "abcdefghijklmnopqrstuvwxyz234567"

	patterns := make([]string, 1<<(length*5))

	literal := make([]byte, length+1)
	literal[0] = '.'

	for index := range patterns {
		value := index

		for position := length; position > 0; position-- {
			literal[position] = alphabet[value&31]
			value >>= 5
		}

		patterns[index] = string(literal)
	}

	return patterns
}
