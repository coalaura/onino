package search

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/binary"
	"strconv"
	"strings"
	"testing"

	"github.com/coalaura/onino/internal/onion"
	"github.com/coalaura/onino/internal/pattern"
)

var benchmarkPublic [32]byte

func BenchmarkWorkloads(b *testing.B) {
	cases := []benchmarkCase{
		{name: "prefix", patterns: []string{"somethingrare."}},
		{name: "suffix", patterns: []string{".somethingrarea"}},
		{name: "combined", patterns: []string{"something.rarea"}},
		{name: "anywhere", patterns: []string{"somethingrare"}},
		{name: "interior", patterns: []string{".somethingrare."}},
		{name: "mixed", patterns: []string{"hello.", ".worlda", "start.enda", ".middle.", "anywhere"}},
		{name: "prefix50", patterns: []string{"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwx."}},
		{name: "frequent", patterns: []string{"ab."}},
		{name: "all_hits", patterns: []string{".a", ".q"}},
		{name: "64", patterns: benchmarkDictionary(64, false)},
		{name: "128", patterns: benchmarkDictionary(128, false)},
		{name: "256", patterns: benchmarkDictionary(256, false)},
		{name: "512", patterns: benchmarkDictionary(512, false)},
		{name: "shared64", patterns: benchmarkDictionary(64, true)},
		{name: "shared512", patterns: benchmarkDictionary(512, true)},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			state := testGenerator(b)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			var hits uint64

			filter := matcher.SignFilter()

			b.ReportAllocs()

			for b.Loop() {
				state.nextBatch(filter != nil)

				for index := range state.publicKeys {
					if state.matches(index, matcher, filter) {
						hits++
					}
				}
			}

			benchmarkHits = hits
			benchmarkPublic = state.publicKeys[0]

			if test.name == "all_hits" && hits != uint64(b.N)*batchSize {
				b.Fatalf("lost candidates: %d", hits)
			}

			b.ReportMetric(float64(b.N)*batchSize/b.Elapsed().Seconds(), "keys/s")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/batchSize, "ns/key")
		})
	}
}

func BenchmarkGeneration(b *testing.B) {
	state := testGenerator(b)

	b.ReportAllocs()

	for b.Loop() {
		state.next()
	}

	benchmarkPublic = state.publicKeys[0]

	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/batchSize, "ns/key")
}

func BenchmarkHitHandling(b *testing.B) {
	b.Run("reseed", func(b *testing.B) {
		state := testGenerator(b)

		b.ReportAllocs()

		for b.Loop() {
			err := state.reseed(0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("random_reseed", func(b *testing.B) {
		state := testGenerator(b)
		state.random = rand.Reader

		b.ReportAllocs()

		for b.Loop() {
			err := state.reseed(0)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("snapshot_callback", func(b *testing.B) {
		state := testGenerator(b)
		state.next()

		var save SaveFunc = benchmarkSave

		b.ReportAllocs()

		for b.Loop() {
			err := save(state.key(0))
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

func BenchmarkDictionarySearch(b *testing.B) {
	forms := []string{"prefix", "suffix", "combined", "long"}
	sizes := []int{64, 512}

	for _, form := range forms {
		for _, size := range sizes {
			patterns := benchmarkDictionary(size, false)

			for index, text := range patterns {
				switch form {
				case "prefix":
					patterns[index] = text + "."
				case "suffix":
					patterns[index] = "." + text + "a"
				case "combined":
					patterns[index] = text[:5] + "." + text[5:] + "a"
				case "long":
					patterns[index] = text + strings.Repeat("a", 32)
				}
			}

			b.Run(form+"/"+strconv.Itoa(size), func(b *testing.B) {
				state := testGenerator(b)

				matcher, err := pattern.CompilePatterns(patterns)
				if err != nil {
					b.Fatal(err)
				}

				var stats Stats

				b.ReportAllocs()

				for b.Loop() {
					err = state.searchBatch(matcher, benchmarkSave, &stats)
					if err != nil {
						b.Fatal(err)
					}
				}

				if stats.Checked != uint64(b.N)*batchSize {
					b.Fatalf("lost candidates: %+v", stats)
				}

				benchmarkHits = stats.Saved

				b.ReportMetric(float64(stats.Checked)/b.Elapsed().Seconds(), "keys/s")
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
			})
		}
	}
}

func BenchmarkFullSearch(b *testing.B) {
	cases := []benchmarkCase{
		{name: "rare", patterns: []string{"somethingrare."}},
		{name: "frequent", patterns: []string{"ab."}},
		{name: "all_hits", patterns: []string{".a", ".q"}},
		{name: "512", patterns: benchmarkDictionary(512, false)},
		{name: "shared512", patterns: benchmarkDictionary(512, true)},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			state := testGenerator(b)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			var stats Stats

			b.ReportAllocs()

			for b.Loop() {
				err = state.searchBatch(matcher, benchmarkSave, &stats)
				if err != nil {
					b.Fatal(err)
				}
			}

			if stats.Checked != uint64(b.N)*batchSize {
				b.Fatalf("lost candidates: %+v", stats)
			}

			if test.name == "all_hits" && stats.Saved != stats.Checked {
				b.Fatalf("lost matches: %+v", stats)
			}

			benchmarkHits = stats.Saved

			b.ReportMetric(float64(stats.Checked)/b.Elapsed().Seconds(), "keys/s")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(stats.Checked), "ns/key")
		})
	}
}

func BenchmarkSearchSizes(b *testing.B) {
	sizes := []int{128, 256, 512, 1024, 2048}

	cases := []benchmarkCase{
		{name: "prefix", patterns: []string{"somethingrare."}},
		{name: "512", patterns: benchmarkDictionary(512, false)},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			for _, size := range sizes {
				b.Run(strconv.Itoa(size), func(b *testing.B) {
					state := testGenerator(b)

					points := make([]extendedPoint, size)
					products := make([]fieldElement, size)
					publicKeys := make([][32]byte, size)

					for index := range points {
						points[index] = state.points[index%batchSize]
					}

					var hits uint64

					filter := matcher.SignFilter()

					b.ReportAllocs()

					for b.Loop() {
						generateBatch(points, products, publicKeys, filter != nil)

						for index := range publicKeys {
							if filter != nil {
								if !filter.Match(publicKeys[index]) {
									continue
								}

								completeSign(&points[index], &products[index], &publicKeys[index])
							}

							if matcher.Match(publicKeys[index]) {
								hits++
								benchmarkSave(onion.Key{Public: publicKeys[index]})
							}
						}
					}

					benchmarkHits = hits

					b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(size), "ns/key")
				})
			}
		})
	}
}

//go:noinline
func benchmarkSave(key onion.Key) error {
	benchmarkPublic = key.Public

	return nil
}

func benchmarkDictionary(count int, shared bool) []string {
	encoding := base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

	patterns := make([]string, count)

	for index := range patterns {
		var input [8]byte

		binary.LittleEndian.PutUint64(input[:], uint64(index))
		digest := sha256.Sum256(input[:])
		text := encoding.EncodeToString(digest[:])

		patterns[index] = text[:10]

		if shared {
			patterns[index] = "aaa" + text[:7]
		}
	}

	return patterns
}
