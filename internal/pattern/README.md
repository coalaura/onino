# Pattern matcher

The `pattern` package is onino's internal 52-character hostname matcher. It compiles simple patterns once, then matches the first 52 visible lowercase base32 characters using raw public-key bytes and, when necessary, four SHA3-256 checksum bits. Matching never constructs an encoded string and performs no heap allocations.

## API

Import `github.com/coalaura/onino/internal/pattern` from within this module:

```go
matcher, err := pattern.CompilePatterns([]string{"start.end", ".middle.", "begin.", ".finish", "anywhere"})
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
| `.text` | Ends with the literal | `.end` |
| `pre.suf` | Starts and ends with the respective literals | `start.end` |
| `.text.` | Contains the literal strictly inside the encoding; the occurrence touches neither endpoint | `.middle.` |
| `text` | Contains the literal anywhere, including either endpoint | `anywhere` |

Prefix and suffix constraints may overlap when their characters agree. An interior pattern may still match if there is also an occurrence at an endpoint, provided another occurrence is strictly interior.

The alphabet is exactly `abcdefghijklmnopqrstuvwxyz234567`. Empty strings, uppercase letters, other characters, malformed dot forms and patterns impossible for every 32-byte input return an error with the pattern's zero-based index and text. Compilation returns no matcher on error.

### The visible 52-character boundary

The first 51 symbols contain public-key bits only. Symbol 52 has one public-key bit and four checksum bits, so every `a-z2-7` character is permitted. For example, `.end` matches visible `end` at characters 50-52, followed by four more checksum/version characters and `.onion`. All pattern forms use this same 52-character window, including full-width prefixes and the last possible unanchored occurrence.

Interior literals can contain at most 50 symbols. Incompatible overlapping prefix/suffix constraints are rejected, including conflicting checksum bits. When constraints determine all 32 public-key bytes, compilation verifies the checksum once and rejects impossible spelling; successful full-width constraints then need no runtime hash.

## Implementation

- Occurrences touching character 52 are separated from ordinary public-key matches. Ordinary matches win without hashing; boundary candidates first pass a raw-bit filter, then share at most one checksum computation across all alternatives. Alternatives with identical public-key constraints merge their allowed checksum nibbles, eliminating hashing when every nibble is accepted.
- Sets consisting solely of one- to three-character suffixes use a public-bit-indexed table of accepted checksum nibbles: 2, 64 or 2,048 entries, at two bytes per entry. A missing entry rejects without hashing; an entry accepting all nibbles succeeds without hashing. Short suffixes keep the existing independent/paired search-engine selection.
- Anchored patterns compile to raw-bit masks. A single short anchored pattern reduces to a 64-bit masked comparison. Wider constraints first test their most selective word, then verify the remaining bits only on a candidate hit.
- At 64 eligible anchored patterns, fixed-position 15-bit membership indexes select exact-verification buckets. Prefixes need three symbols, suffixes four; combined anchors choose the less-populated fragment. The suffix fragment excludes the compressed-point sign so the index can also serve deferred-sign filtering.
- Single-character searches compare eight packed five-bit symbols at once with scalar word operations. Endpoint masks enforce strictly interior matches without borrow-related false positives.
- On AVX2-capable amd64, substring searches extract overlapping 15-bit windows once into vector registers, then reuse them across the pattern list. These are raw-bit windows, not ASCII or an encoded buffer. Two- and three-character filters are exact; longer literals verify their complete masks on candidate hits.
- Each AVX2 filter is 16 bytes with no struct padding. Four filters occupy 64 bytes; larger verification data is stored separately and accessed only after a filter hit. Scalar probes use the same hot/cold separation. Searches with very few possible positions use scalar probes to avoid vector setup cost.
- At 32 eligible unanchored patterns, a portable triplet dictionary replaces per-pattern scanning. Eligible literals have at least six symbols and nine permitted starts. One anchor per offset residue covers every start while scanning only every fourth triplet; anchors share their exact parsed verifier. Short alternatives retain existing paths. The bitmap and rank directory use 5 KiB, with compact buckets for occupied triplets.
- `SignFilter` provides a necessary-condition matcher for incomplete Y encodings. It ignores byte 31's high bit and all checksum constraints. That bit directly affects base32 character 50 and also affects the checksum. Callers complete the actual sign before running exact `Match`; the filter never hashes an incomplete key. Raw exact and signless dictionary matchers share immutable indexes.
- `PreferIndependent` supplies a search-engine hint for frequent short patterns: one-symbol alternatives and two-symbol unanchored/interior literals. It preserves `SignFilter` behavior and does not estimate the combined probability of an arbitrary pattern list.
- AVX2 detection checks both CPU support and operating-system vector-state support. Other CPUs and architectures use portable scalar matching. The vector routine loads exactly 32 input bytes without overreading; its synthetic padding is used only by raw public-key filters, never to accept visible character 52.
- PACE force-inlines the small dispatch paths and calls the assembly leaf through `//go:abiinternal`, passing arguments and results in registers. Stock Go is also supported through its normal assembly ABI. No external dependencies are needed.

## Measurements

Thresholds 16, 32, 64 and 128 were screened using ordinary/shared literals, short fallbacks and mixed anchors. With the paired engine held fixed, lowering the threshold from 128 to 32 reduced full search from 63.14 to 59.17 ns/key for 32 ordinary patterns and from 75.38 to 60.01 for 64. Shared and mixed-anchor sets also improved; short-fallback differences were inconclusive. These are five-sample alternating PACE runs on one pinned Ryzen 9 9950X3D Windows worker, not matcher-only timings.

At the newly indexed sizes, compilation also became cheaper: ordinary 32-pattern compilation fell from approximately 292 to 30 µs and 228.5 to 47.3 KiB allocated, avoiding per-position vector verifiers. Sets below 32 retain the previous paths; existing larger dictionaries retain their algorithm. See [the research notes](../../RESEARCH.md#dictionary-crossover) for the crossover and memory tradeoffs and [the main README](../../README.md#benchmarks) for complete production-search results. Thresholds remain dependent on the machine, compiler and pattern distribution.

Run these commands from the repository root:

```sh
pace test -vet=off ./internal/pattern
go test -vet=off ./internal/pattern
vet --tests ./internal/pattern
pace test -vet=off ./internal/pattern -run '^$' -bench . -benchmem -benchtime=250ms -count=3 -cpu=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzMatcher$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzDictionary$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzAnchoredDictionary$' -fuzztime=10s -parallel=1
```

Tests compare scalar, AVX2 and indexed matching against independently checksummed hostname encodings and string matching, including every possible literal length/alignment, both signs, all 32 final characters, conflicting overlaps, false-candidate continuation, concurrent use and zero matching allocations. Deferred-sign filters are checked against the union of both signs and all checksum nibbles. Assembly-facing data layouts are checked explicitly. Use `GOMAXPROCS=1` for bounded single-worker runs and repeat with `-tags=purego` for real portable fallback coverage.
