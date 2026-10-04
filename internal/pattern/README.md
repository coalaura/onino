# Pattern matcher

The `pattern` package is onino's internal base32 pattern-matching component. It compiles simple patterns once, then matches directly against 32 raw bytes as though they were lowercase, unpadded RFC 4648 base32. Matching never constructs an encoded string and performs no heap allocations.

## API

Import `github.com/coalaura/onino/internal/pattern` from within this module:

```go
matcher, err := pattern.CompilePatterns([]string{"start.enda", ".middle.", "begin.", ".finisha", "anywhere"})
if err != nil {
	panic(err)
}

var input [32]byte
matched := matcher.Match(input)
```

Patterns are ORed: `Match` returns true if any pattern matches. A matcher is immutable and safe for concurrent use. An empty pattern list and the zero value of `pattern.Matcher` match nothing. Duplicate patterns are ignored.

| Form | Meaning | Valid example |
| --- | --- | --- |
| `text.` | Starts with the literal | `begin.` |
| `.text` | Ends with the literal | `.enda` |
| `pre.suf` | Starts and ends with the respective literals | `start.enda` |
| `.text.` | Contains the literal strictly inside the encoding; the occurrence touches neither endpoint | `.middle.` |
| `text` | Contains the literal anywhere, including either endpoint | `anywhere` |

Prefix and suffix constraints may overlap when their characters agree. An interior pattern may still match if there is also an occurrence at an endpoint, provided another occurrence is strictly interior.

The alphabet is exactly `abcdefghijklmnopqrstuvwxyz234567`. Empty strings, uppercase letters, other characters, malformed dot forms and patterns impossible for every 32-byte input return an error with the pattern's zero-based index and text. Compilation returns no matcher on error.

### Canonical padding matters

Exactly 32 bytes encode to 52 symbols. The final symbol has one input bit and four zero padding bits, so it can only be `a` or `q`. For example, `.end` is impossible and rejected, while `.enda` is valid. An unanchored `end` is valid because it can occur before the final symbol.

A full-width literal must respect the final symbol too. Interior literals can contain at most 50 symbols. Incompatible overlapping prefix/suffix constraints are rejected.

## Implementation

- Anchored patterns compile to raw-bit masks. A single short anchored pattern reduces to a 64-bit masked comparison. Wider constraints first test their most selective word, then verify the remaining bits only on a candidate hit.
- Single-character searches compare eight packed five-bit symbols at once with scalar word operations. Endpoint masks enforce strictly interior matches without borrow-related false positives.
- On AVX2-capable amd64, substring searches extract overlapping 15-bit windows once into vector registers, then reuse them across the pattern list. These are raw-bit windows, not ASCII or an encoded buffer. Two- and three-character filters are exact; longer literals verify their complete masks on candidate hits.
- Each AVX2 filter is 16 bytes with no struct padding. Four filters occupy 64 bytes; larger verification data is stored separately and accessed only after a filter hit. Scalar probes use the same hot/cold separation. Searches with very few possible positions use scalar probes to avoid vector setup cost.
- AVX2 detection checks both CPU support and operating-system vector-state support. Other CPUs and architectures use portable scalar matching. The vector routine loads exactly 32 input bytes without overreading and synthesizes the canonical final padding bits.
- PACE force-inlines the small dispatch paths and calls the assembly leaf through `//go:abiinternal`, passing arguments and results in registers. Stock Go is also supported through its normal assembly ABI. No external dependencies are needed.

## Measurements

PACE, Windows/amd64, AMD Ryzen 9 9950X3D, median of three 250 ms runs. Each benchmark reuses 256 deterministic inputs; these are warm-cache throughput measurements. Random inputs are mostly misses except for short literals. The baseline also allocates nothing and parses its patterns before timing.

| Pattern workload | Compiled, ns/input | Encode then search, ns/input |
| --- | ---: | ---: |
| Prefix | 0.93 | 14.71 |
| Suffix | 1.06 | 17.52 |
| Prefix and suffix | 1.03 | 16.13 |
| One-character substring | 4.06 | 22.23 |
| Two-character substring | 10.09 | 20.70 |
| Six-character substring | 9.18 | 20.75 |
| Strictly interior substring | 9.17 | 21.17 |
| Mixed five-pattern set | 10.35 | 34.58 |
| Eight substring patterns | 12.15 | 69.59 |

Guaranteed-hit benchmarks measured approximately 1.1-1.2 ns for a short prefix or suffix, 2.5 ns for prefix plus suffix, 13.2 ns for a longer substring and 15.7 ns when the guaranteed hit is the last of eight substring patterns. Random six-character pattern sets measured 9.6, 12.6, 36.0 and 230.4 ns/input for 1, 8, 64 and 512 patterns respectively. Every matching benchmark reported **0 B/op and 0 allocs/op**.

These results depend on the CPU, compiler, pattern set, hit rate and cache state; they are not a guarantee of the fastest possible implementation for every workload.

Run these commands from the repository root:

```sh
pace test ./internal/pattern
go test ./internal/pattern
vet --tests ./internal/pattern
pace test ./internal/pattern -run '^$' -bench . -benchmem -benchtime=250ms -count=3
pace test ./internal/pattern -run '^$' -fuzz '^FuzzMatcher$' -fuzztime=15s -parallel=4
```

Tests compare both scalar and AVX2 matching against the standard library's base32 encoder and independent string matching, including every possible literal length/alignment, canonical final-symbol constraints, all single-character positions, conflicting overlaps, false-candidate continuation, concurrent use and zero matching allocations. Assembly-facing data layouts are checked explicitly. The latest fuzz run passed 3,007,669 cases.
