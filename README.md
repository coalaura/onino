# onino

CPU-only vanity v3 `.onion` address search across the first **52 visible hostname characters**. onino matches public keys against compiled patterns and keeps searching after saving matches. It defaults to one worker; use `--cpu` for multicore search.

## Build and use

PACE is the primary build and release target. It enables register-ABI assembly calls and explicit inlining in the matcher and search engine. Use a toolchain compatible with the Go 1.27.1 version declared in `go.mod`:

```sh
pace build -o onino .
./onino --output matches 'hello.' 'onino.'
./onino --cpu all 'hello.'
```

On Windows, build with `pace build -o onino.exe .` and invoke `./onino.exe`. Stock `go build` is supported as an alternative when PACE is unavailable.

Use `--help` for usage and `--output` / `-o` to select the destination directory; it defaults to `matches`.

`--cpu` accepts a positive integer or `all`, bounded by the logical CPUs available to the process. Windows/Linux affinity restrictions are respected. Multiple workers use discovered physical cores before SMT siblings and spread across last-level caches; unsupported topology uses OS placement and says so at startup. The flag selects search workers, not total runtime threads.

Patterns are ORed together. Supported forms are `prefix.`, `.suffix`, `prefix.suffix`, `.interior.` and `anywhere`; see [the matcher documentation](internal/pattern/README.md) for precise rules and validation.

All patterns match the **first 52 characters actually printed in the hostname**. A suffix ends at character 52: `.aaa` produces a hostname shaped like `...aaa????.onion`, with four further checksum/version characters. Character 52 mixes one public-key bit with four checksum bits and can be any `a-z2-7` character. The matcher filters public-key bits first and computes the checksum only when needed to verify the visible spelling.

The search continues until Ctrl+C or an error. Cancellation finishes each worker's current batch of 512 checked candidates, including synchronous saves, so frequent matches or slow storage can delay shutdown. Successfully saved hostnames are printed to stdout with the time since the previous match (or search start for the first) and total search time, for example `example.onion in 12.34s (23.45s total)`. About every four seconds, one line on stderr shows total keys checked, elapsed time and overall average keys/second; intermediate counters are approximate and final counts are exact. Reporting waits for an available callback slot when a save is in progress.

Startup shows approximate candidate counts for a **50% and 95% chance of at least one match** across all patterns. Progress converts those counts into estimated waits from now using the overall average rate. Estimates account for overlapping patterns and the four checksum bits in character 52, but assume independent uniform candidates; they are guidance, not deadlines or guarantees.

## Saved matches

Each match gets a separate directory:

```text
matches/<hostname>.onion/
	hostname
	hs_ed25519_public_key
	hs_ed25519_secret_key
```

These are Tor-compatible v3 service files, including Tor's tagged binary key headers. The secret contains the 64-byte **expanded Ed25519 scalar and nonce prefix**, rather than a seed or Go's `ed25519.PrivateKey` representation. The hostname includes the SHA3-256 checksum and version byte.

The key pair is checked before writing. Files are flushed inside a private staging directory and the completed directory is renamed to its hostname. Existing matches are refused; a failed write retains its staging directory and reports its path. Directories are created with mode `0700` and files with `0600` where those permission modes apply. A persistence error stops the search and is reported.

## Search engine

1. **Generate pairs around independent centers.** The usual engine starts 256 independently seeded affine Edwards25519 centers. A table of 64 offsets `j.8B` produces `P+Q` and `P-Q` together, sharing arithmetic and one batch inversion. Centers advance between table passes instead of being rebuilt for every candidate.
2. **Match before completing the sign.** Candidates first have canonical Y bytes. A necessary-condition filter ignores the unknown compressed-point sign and checksum constraints; survivors get their exact X/sign using retained reciprocals, then pass the full matcher. The sign affects base32 character 50, one-based and must be completed before hashing. Matching reads raw key bytes without constructing base32 strings.
3. **Save at most one key per seed.** After a hit, onino saves a value snapshot, discards any pending relative from that seed and reseeds independently from `crypto/rand`. Discarded candidates are replaced and never counted as checked. Scalar offsets preserve clamping, reserve headroom and stay within bounded reseeding epochs.

Very frequent short patterns use an independently seeded projective `8B` walk instead, avoiding the cost of repeatedly rebuilding affine centers. This selection happens once at compilation/startup; both engines use the same save and validation contract.

Small pattern sets use specialized scalar and AVX2 filters. At 32 eligible unanchored literals, a strided triplet dictionary shares filtering work across the set. Large anchored sets use fixed-position indexes. All filters verify complete constraints before accepting a match.

The field backend uses four 64-bit limbs with fused BMI2/ADX assembly where available. Arithmetic and AVX2 matching are detected independently; AVX2 dispatch also checks operating-system vector-state support. AVX2 is the SIMD ceiling and `-tags purego` disables both assembly paths. PACE supplies register-ABI calls and targeted inlining; PGO is not required.

Steady-state search performs no heap allocations. Each worker owns its generator, secure seeds, pending candidates, scratch and counters; only immutable search tables are shared. Saves are serialized. The CLI sets `GOMAXPROCS` to the worker count once; `search.Run` and `RunWithProgress` retain their direct single-worker paths, while `RunWithOptions` adds parallel execution. See [research and implementation notes](RESEARCH.md) for formulas, invariants and measured tradeoffs.

## Benchmarks

### Single-worker baseline

PACE Go 1.27.1 on Windows 11/amd64 and an AMD Ryzen 9 9950X3D, with `GOAMD64=v1`, `GOMAXPROCS=1`, no PGO and logical CPU 2 affinity. Values are medians from ten alternating one-second runs against baseline `d01aa81`; lower ns/key is better. These historical measurements predate visible character-52 matching; [visible-suffix measurements](RESEARCH.md#visible-character-52-matching) cover the updated boundary. Stock Go and `purego` remain tested compatibility targets.

| Workload | PACE, ns/key | PACE keys/second |
| --- | ---: | ---: |
| Full search, rare prefix | 39.27 | 25.47 million |
| Full search, frequent `ab.` | 48.55 | 20.60 million |
| Full search, every candidate hits | 7652 | 130,700 |
| Full search, 512 anywhere patterns | 62.10 | 16.10 million |
| Full search, 512 shared-triplet patterns | 61.69 | 16.21 million |
| Full search, 512 prefixes | 41.59 | 24.05 million |
| Full search, 512 suffixes | 41.56 | 24.06 million |

All search samples report **0 B/op and 0 allocs/op**. Rare-prefix samples ranged from 39.26-39.74 ns/key; ordinary 512-pattern searches ranged from 62.03-62.89. Fingerprints and constant-time divsteps improve their throughput by 2.5% and 12.1% over the baseline. These are elapsed timings on one pinned worker; [research notes](RESEARCH.md#final-bounded-pass-fingerprints-and-divsteps) give full ranges, mixed workloads, memory costs and the historical compiler comparison.

Full-search benchmarks include center transitions, matching, sign completion, statistics, discarded-candidate replenishment, immutable key snapshots and per-hit reseeding. They use reproducible SHAKE entropy and a cheap synchronous callback; startup, OS random acquisition, cancellation polling and disk persistence are outside the timed loop. Actual CLI throughput depends on hit rate and storage.

To benchmark the production search loop:

```sh
go test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(FullSearch|DictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(FullSearch|DictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
```

Run benchmarks serially with `GOMAXPROCS=1`, `GOAMD64=v1` and consistent CPU affinity. For compiler comparisons, build the same source with both toolchains and alternate their test binaries to reduce run-order bias.

### Multicore performance

On the same Ryzen 9950X3D, pinned physical-cores-first placement gave the following median rare-prefix throughput with cheap callbacks, three alternating five-second samples per configuration and a warm-up before each sample:

| Workers | Million keys/s | Million keys/s per worker |
| ---: | ---: | ---: |
| 2 | 45.40 | 22.70 |
| 4 | 95.29 | 23.82 |
| 8 | 193.05 | 24.13 |
| 16 | 375.20 | 23.45 |
| 24 | 395.23 | 16.47 |
| 32 | 419.54 | 13.11 |

Rare-match coordination overhead was within 1.6% of equivalent independent worker loops, including progress-enabled comparisons. SMT adds throughput without preserving physical-core per-worker speed. Real synchronous persistence was storage-bound. Single-worker hot-loop code is unchanged, but repeated final-binary dictionary measurements were about 2% slower than the starting binary; the strict no-regression goal remains unresolved. [Multicore research notes](RESEARCH.md#multicore-search) contain the full workload matrix, placement/process comparisons, limitations and bounded reproduction commands.

### Optimization history

Eighteen cumulative milestones were built with the same PACE toolchain and measured with one full-search harness, from the first batched engine through adjacent-symbol fingerprints and constant-time divsteps inversion. Rare-prefix search went from **125.5 to 39.21 ns/key (3.20x throughput)**; a 512-pattern anywhere dictionary went from **348.3 to 62.25 ns/key (5.60x)**.

![Full-search performance across eighteen milestones, from the first batched projective engine through dictionary fingerprints and constant-time divsteps inversion.](.github/performance-history.svg)

Each point is the median of five one-second samples for steps 00-15 and ten for steps 16-17 on the same pinned worker; whiskers show the full range. The graph preserves every earlier measurement, including plateaus and regressions and extends all five series with the two retained improvements.

The [performance history CSV](.github/performance-history.csv) contains every milestone's samples and the final-pass baseline comparison. The [measurement notes](RESEARCH.md#cumulative-performance-history) describe the later history run, including its progress-reporting check; the performance table above uses the separate final-pass comparison.

## Verification

```sh
pace test -vet=off ./...
go test -vet=off ./...
pace test -vet=off -tags purego ./...
go test -vet=off -tags purego ./...
vet --tests ./...
vet --tests --tags purego ./...
```

Use the custom `vet` tool for static checks; tests disable the built-in vet invocation. The native/purego matrix exercises actual arithmetic and matcher fallbacks.

Tests cover independent scalar multiplication and Ed25519 signatures, scalar boundaries and table transitions, reseeding and saved-key independence, concurrent hits, exact shutdown counters, callback serialization, CPU selection/affinity cleanup, cancellation, Tor file validation and zero allocations. Field arithmetic is checked against `math/big`; matchers are compared with independent base32/string references. Linux native/purego race tests pass; the Windows race runtime could not start on the measurement host. Detailed verification scope is documented in [RESEARCH.md](RESEARCH.md#verification).
