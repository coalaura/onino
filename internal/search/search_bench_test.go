package search

import (
	"crypto/ed25519"
	"encoding/binary"
	"strconv"
	"testing"

	"filippo.io/edwards25519"
	"filippo.io/edwards25519/field"

	"github.com/coalaura/onino/internal/pattern"
)

type benchmarkCase struct {
	name     string
	patterns []string
}

var benchmarkHits uint64

func BenchmarkSearch(b *testing.B) {
	cases := []benchmarkCase{
		{name: "prefix", patterns: []string{"somethingrare."}},
		{name: "anywhere", patterns: []string{"somethingrare"}},
		{name: "eight_patterns", patterns: []string{"helloworld", "onionworld", "oninosayshi", "middleearth", "searchingfor", "patternmatch", "vanityaddress", "addressfound"}},
	}

	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			state := testGenerator(b)

			matcher, err := pattern.CompilePatterns(test.patterns)
			if err != nil {
				b.Fatal(err)
			}

			var hits uint64

			b.ReportAllocs()

			for b.Loop() {
				state.next()

				for index := range state.publicKeys {
					if matcher.Match(state.publicKeys[index]) {
						hits++
					}
				}
			}

			benchmarkHits = hits

			b.ReportMetric(float64(b.N)*batchSize/b.Elapsed().Seconds(), "keys/s")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/batchSize, "ns/key")
		})
	}
}

func BenchmarkBatchSizes(b *testing.B) {
	sizes := []int{1, 8, 16, 32, 64, 128, 256, 512, 1024, 2048}

	for _, size := range sizes {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			state := testGenerator(b)

			points := make([]extendedPoint, size)
			products := make([]field.Element, size)
			publicKeys := make([][32]byte, size)

			for index := range points {
				points[index] = state.points[index%batchSize]
			}

			b.ReportAllocs()

			for b.Loop() {
				generatePoints(points, products, publicKeys)
			}

			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(size), "ns/key")
		})
	}
}

func BenchmarkCurveStep(b *testing.B) {
	b.Run("general", func(b *testing.B) {
		point := edwards25519.NewGeneratorPoint()

		step := new(edwards25519.Point).MultByCofactor(point)

		b.ReportAllocs()

		for b.Loop() {
			point.Add(point, step)
		}
	})

	b.Run("targeted", func(b *testing.B) {
		var point extendedPoint

		point.set(edwards25519.NewGeneratorPoint())

		b.ReportAllocs()

		for b.Loop() {
			point.advance()
		}
	})

	b.Run("general_with_encoding", func(b *testing.B) {
		point := edwards25519.NewGeneratorPoint()

		step := new(edwards25519.Point).MultByCofactor(point)

		b.ReportAllocs()

		for b.Loop() {
			point.Add(point, step)
			point.Bytes()
		}
	})
}

func BenchmarkSeedKeyGeneration(b *testing.B) {
	var (
		seed     [32]byte
		sequence uint64
	)

	b.ReportAllocs()

	for b.Loop() {
		binary.LittleEndian.PutUint64(seed[:], sequence)

		ed25519.NewKeyFromSeed(seed[:])

		sequence++
	}
}
