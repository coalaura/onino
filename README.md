# onino

CPU-only vanity v3 `.onion` address search. onino uses one worker, matches public keys against compiled patterns and keeps searching after saving matches.

## Build and use

PACE is the primary build and release target. It enables register-ABI assembly calls and explicit inlining in the matcher and search engine. Use a toolchain compatible with the Go 1.27.1 version declared in `go.mod`:

```sh
pace build -o onino .
./onino --output matches 'hello.' 'onino.'
```

On Windows, build with `pace build -o onino.exe .` and invoke `./onino.exe`. Stock `go build` is supported as an alternative when PACE is unavailable.

Use `--help` for usage and `--output` / `-o` to select the destination directory; it defaults to `matches`.

Patterns are ORed together. Supported forms are `prefix.`, `.suffix`, `prefix.suffix`, `.interior.` and `anywhere`; see [the matcher documentation](internal/pattern/README.md) for precise rules and validation.

Matching uses the **standalone lowercase, unpadded base32 encoding of the 32-byte public key**, excluding the checksum and version bytes. That representation has 52 symbols and ends in `a` or `q`. Its last symbol contains zero padding, whereas character 52 of the complete onion address also contains checksum bits; suffix patterns refer to the standalone key representation.

The search continues until Ctrl+C or an error. Cancellation finishes the current batch of 512 checked candidates, including saving its matches. Successfully saved hostnames are printed to stdout. About every four seconds, a single line on stderr shows the total keys checked, total elapsed time and keys/second over the latest reporting interval, with comma-separated counts and rates; final counts are printed when the search stops.

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
2. **Match before completing the sign.** Candidates first have canonical Y bytes. A necessary-condition filter ignores only the unknown compressed-point sign; survivors get their exact X/sign using retained reciprocals, then pass the full matcher. The sign affects base32 character 50, one-based. Matching reads raw key bytes without constructing base32 strings.
3. **Save at most one key per seed.** After a hit, onino saves a value snapshot, discards any pending relative from that seed and reseeds independently from `crypto/rand`. Discarded candidates are replaced and never counted as checked. Scalar offsets preserve clamping, reserve headroom and stay within bounded reseeding epochs.

Very frequent short patterns use an independently seeded projective `8B` walk instead, avoiding the cost of repeatedly rebuilding affine centers. This selection happens once at compilation/startup; both engines use the same save and validation contract.

Small pattern sets use specialized scalar and AVX2 filters. At 32 eligible unanchored literals, a strided triplet dictionary shares filtering work across the set. Large anchored sets use fixed-position indexes. All filters verify complete constraints before accepting a match.

The field backend uses four 64-bit limbs with fused BMI2/ADX assembly where available. Arithmetic and AVX2 matching are detected independently; AVX2 dispatch also checks operating-system vector-state support. AVX2 is the SIMD ceiling and `-tags purego` disables both assembly paths. PACE supplies register-ABI calls and targeted inlining; PGO is not required.

The ordinary search path performs no heap allocations. The CLI sets `GOMAXPROCS(1)`; `search.Run` is a synchronous context/matcher/save-callback API. See [research and implementation notes](RESEARCH.md) for formulas, invariants, crossover decisions and evaluated alternatives.

## Benchmarks

Stock Go and PACE Go 1.27.1, built from the same current source, on Windows 11/amd64 and an AMD Ryzen 9 9950X3D. Both use `GOAMD64=v1`, `GOMAXPROCS=1`, no PGO and logical CPU 2 affinity. Values are medians from five alternating one-second runs per compiler; lower ns/key is better.

| Workload | Go, ns/key | PACE, ns/key | PACE keys/second |
| --- | ---: | ---: | ---: |
| Full search, rare prefix | 42.99 | 40.88 | 24.46 million |
| Full search, frequent `ab.` | 52.35 | 50.54 | 19.79 million |
| Full search, every candidate hits | 7900 | 7800 | 128,200 |
| Full search, 512 anywhere patterns | 88.57 | 70.66 | 14.15 million |
| Full search, 512 shared-triplet patterns | 83.23 | 66.32 | 15.08 million |
| Full search, 512 prefixes | 45.16 | 43.14 | 23.18 million |
| Full search, 512 suffixes | 45.21 | 43.30 | 23.09 million |

Both compilers report **0 B/op and 0 allocs/op** across all search samples. Rare-prefix samples ranged from 42.86-43.03 ns/key with Go and 40.69-42.15 with PACE. All-hit medians differ by about 1.3%, with overlapping ranges. These results describe one machine and use elapsed time on a pinned worker, rather than hardware-counter CPU accounting.

Full-search benchmarks include center transitions, matching, sign completion, statistics, discarded-candidate replenishment, immutable key snapshots and per-hit reseeding. They use reproducible SHAKE entropy and a cheap synchronous callback; startup, OS random acquisition, cancellation polling and disk persistence are outside the timed loop. Actual CLI throughput depends on hit rate and storage. With PACE, generation alone takes 38.73 ns/key for canonical Y or 62.52 ns/key for complete signed public keys.

To benchmark the production search loop:

```sh
go test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(FullSearch|DictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(FullSearch|DictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
```

Run benchmarks serially with `GOMAXPROCS=1`, `GOAMD64=v1` and consistent CPU affinity. For compiler comparisons, build the same source with both toolchains and alternate their test binaries to reduce run-order bias.

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

Tests cover independent scalar multiplication and Ed25519 signatures, scalar boundaries and table transitions, exceptional points, reseeding and saved-key independence, cancellation, Tor file validation and zero allocations. Field arithmetic is checked against `math/big`; matchers are compared with independent base32/string references. Bounded fuzzing and the detailed verification scope are documented in [RESEARCH.md](RESEARCH.md#verification).
