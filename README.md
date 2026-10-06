<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/banner.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/banner-light.svg">
	<img alt="onino - CPU-only vanity .onion search. A racing onion wordmark; oni[on ⇄ no] flips onion's final two letters." src=".github/banner-light.svg">
</picture>

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

`--simd=auto` (the default) selects optional AVX-512 acceleration at startup when the CPU and operating system support the required instructions and register state. Use `--simd=avx2` to skip onino's AVX-512 detection and disable all its AVX-512 paths. Both modes use the same binary and compilation baseline; older CPUs retain their existing fallbacks.

Patterns are ORed together. Supported forms are `prefix.`, `.suffix`, `prefix.suffix`, `.interior.` and `anywhere`; see [the matcher documentation](internal/pattern/README.md) for precise rules and validation.

All patterns match the **first 52 characters actually printed in the hostname**. A suffix ends at character 52: `.aaa` produces a hostname shaped like `...aaa????.onion`, with four further checksum/version characters. Character 52 mixes one public-key bit with four checksum bits and can be any `a-z2-7` character. The matcher filters public-key bits first and computes the checksum only when needed to verify the visible spelling.

The search continues until Ctrl+C or an error. Workers send matching candidate snapshots and discovery timestamps to one saver through a 64-entry buffered queue, then reseed and resume searching. They wait for saving only when the queue fills. Cancellation finishes each worker's current batch of 512 checked candidates and drains the queue, so frequent matches or slow storage can delay shutdown. Successfully saved hostnames are printed to stdout with the time since the previous discovery (or search start for the first) and total search time at discovery, for example `example.onion in 12.34s (23.45s total)`. Queueing and filesystem delays do not inflate those timestamps; out-of-order discoveries across workers use zero for the inter-match interval. About every four seconds, one line on stderr shows total keys checked, elapsed time and overall average keys/second; intermediate counters are approximate and final counts are exact. Reporting waits for an available callback slot when a save is in progress.

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
3. **Save at most one key per seed.** After a hit, onino queues an owned snapshot of the public key, base secret and scalar offset, discards any pending relative from that seed and reseeds independently from `crypto/rand`. The saver finalizes the scalar, verifies the key pair and persists it. Discarded candidates are replaced and never counted as checked. Scalar offsets preserve clamping, reserve headroom and stay within bounded reseeding epochs.

Very frequent short patterns use an independently seeded projective `8B` walk instead, avoiding the cost of repeatedly rebuilding affine centers. This selection happens once at compilation/startup; both engines use the same save and validation contract.

Small pattern sets use specialized scalar and AVX2 filters. At 32 eligible unanchored literals, a strided triplet dictionary shares filtering work across the set. Large anchored sets use fixed-position indexes. All filters verify complete constraints before accepting a match.

Arithmetic, matching and checksum acceleration are selected independently. Existing BMI2/ADX and AVX2 paths remain available; `-tags purego` disables native assembly, including AVX-512. PACE supplies register-ABI calls and targeted inlining; stock Go remains supported and PGO is not required.

Steady-state search and queue handoff perform no heap allocations; filesystem persistence allocates separately. Each worker owns its generator, secure seeds, pending candidates, scratch and counters. Shared search tables are immutable and queued snapshots contain no references to worker state. The CLI uses `search.RunQueued`, with one saver even for a single search worker and sets `GOMAXPROCS` to the worker count once. `search.Run`, `RunWithProgress` and `RunWithOptions` retain their synchronous save contracts. Saved counters advance only after successful persistence; a save error stops workers and further saves, while a worker error still drains already accepted matches. See [research and implementation notes](RESEARCH.md) for formulas, invariants and measured tradeoffs.

## Benchmarks

CPU-only prefix searches on an **AMD Ryzen 9 9950X3D**, Windows 11, with **32 workers** and normal key-file output. The same onino binary was measured separately with `--simd avx2` and `--simd auto` (AVX-512 on this CPU). Onionloom used `--gpu off`. Rates are **million candidates/second**; higher is better.

<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/prefix-comparison.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/prefix-comparison-light.svg">
	<img alt="Median CPU-only throughput for onino AVX2, onino auto, onionloom and mkp224o across four prefix workloads, including a no-match case, on a shared zero-based scale." src=".github/prefix-comparison-light.svg">
</picture>

**Median throughput**

| Prefixes | onino AVX2 | onino auto | onionloom | mkp224o |
| --- | ---: | ---: | ---: | ---: |
| `hello` | 416.4 | **1,211.1** | 343.4 | 129.5 |
| `privacy` | 422.9 | **1,247.7** | 345.2 | 130.3 |
| `donate`, `mirror`, `secure` | 413.8 | **1,237.4** | 332.8 | 111.2 |
| `somethingrare` (no matches) | 422.9 | **1,243.2** | 345.1 | 130.4 |

**Min-max throughput**

| Prefixes | onino AVX2 | onino auto | onionloom | mkp224o |
| --- | ---: | ---: | ---: | ---: |
| `hello` | 409.4-422.0 | 1,208.2-1,221.2 | 338.6-345.2 | 129.0-130.3 |
| `privacy` | 419.2-424.8 | 1,240.1-1,252.2 | 341.7-345.8 | 129.5-131.4 |
| `donate`, `mirror`, `secure` | 408.1-416.2 | 1,230.0-1,240.3 | 330.1-334.6 | 110.6-112.5 |
| `somethingrare` (no matches) | 418.2-423.7 | 1,238.5-1,248.0 | 338.3-345.6 | 129.5-131.2 |

Each configuration ran five samples per workload, sequentially with rotating tool order and all 32 logical CPUs available. Timed windows lasted 20-32 seconds after warm-up, bounded by progress reports; rates use cumulative candidate-count deltas over wall time, not peak displayed rates. Multiple prefixes match any listed prefix. The 13-character `somethingrare` prefix produced **zero matches and zero key files in every run**, isolating search throughput from match-saving I/O. One trial without a complete timed window was retained locally and rerun. [Raw samples](.github/prefix-comparison.csv) are available.

Tested versions (2026-10-06):

- onino v0.2.0 - current repository build ([`35b17e5`](https://github.com/coalaura/onino/commit/35b17e5)), PACE Go 1.27.1, `GOAMD64=v1`, PGO disabled.
- [onionloom v1.0.1](https://github.com/chrisch88dev/onionloom) - official Windows release.
- [mkp224o v1.7.0](https://github.com/cathugger/mkp224o) - official Windows release.

## Optimization history

From the first batched engine to v0.2.0, rare-prefix search improved from **125.5 to 14.21 ns/key (8.83x throughput)** with AVX-512, while matching against 512 anywhere patterns improved from **348.3 to 40.73 ns/key (8.55x)**. Fresh measurements of the same binary with forced AVX2 give **39.22 ns/key** and **62.45 ns/key**, respectively.

| Single-worker search | `--simd avx2`, ns/key | `--simd auto`, ns/key |
| --- | ---: | ---: |
| Rare prefix | 39.22 [39.19-40.22] | **14.21 [14.20-14.22]** |
| Frequent prefix | 48.49 [48.47-48.55] | **23.46 [23.44-23.46]** |
| 512 anywhere patterns | 62.45 [62.41-62.50] | **40.73 [40.70-40.82]** |

<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/performance-history.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/performance-history-light.svg">
	<img alt="Single-worker search performance across eighteen historical milestones plus fresh v0.2.0 AVX2 and AVX-512 measurements, with median points and full-range whiskers." src=".github/performance-history-light.svg">
</picture>

Each point shows median single-worker performance; brackets and whiskers show the full measured range, including plateaus and regressions. The two current modes used ten alternating samples of 51.2 million candidates each, pinned to logical CPU 2 with `GOMAXPROCS=1`, `-cpu=1` and PGO disabled. These in-process searches use deterministic entropy and a no-op save callback; all measured zero allocations. Historical points are preserved, not rerun. The [research notes](RESEARCH.md#cumulative-performance-history) cover the earlier experiments, with [raw samples](.github/performance-history.csv) available separately.

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
