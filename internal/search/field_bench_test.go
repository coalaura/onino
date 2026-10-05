package search

import "testing"

var benchmarkField fieldElement

func BenchmarkField(b *testing.B) {
	value := fieldElement{0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff, 0xffffffffffffffff}

	b.Run("multiply", func(b *testing.B) {
		result := value

		b.ReportAllocs()

		for b.Loop() {
			result.multiply(&result, &value)
		}

		benchmarkField = result
	})

	b.Run("square", func(b *testing.B) {
		result := value

		b.ReportAllocs()

		for b.Loop() {
			result.square(&result)
		}

		benchmarkField = result
	})

	b.Run("invert", func(b *testing.B) {
		result := value

		b.ReportAllocs()

		for b.Loop() {
			result.invert(&result)
		}

		benchmarkField = result
	})
}
