# onino

CPU-only vanity v3 `.onion` address search. The current engine uses one worker, checks every generated public key against compiled patterns and keeps searching after saving matches.

## Build and use

PACE is the primary build and release target. It enables register-ABI assembly calls and explicit inlining in the matcher and search engine. Use a toolchain compatible with the Go 1.27.1 version declared in `go.mod`:

```sh
pace build -o onino .
./onino --output matches 'hello.' 'onino.'
```

On Windows, build with `pace build -o onino.exe .` and invoke `./onino.exe`. Stock `go build` is supported as an alternative when PACE is unavailable.

The CLI uses `urfave/cli/v3`. Use `--help` for usage and `--output` / `-o` to select the destination directory; it defaults to `matches`.

Patterns are ORed together. Supported forms are `prefix.`, `.suffix`, `prefix.suffix`, `.interior.` and `anywhere`; see [the matcher documentation](internal/pattern/README.md) for precise rules and validation.

Matching uses the **standalone lowercase, unpadded base32 encoding of the 32-byte public key**, excluding the checksum and version bytes. That representation has 52 symbols and ends in `a` or `q`. Its last symbol contains zero padding, whereas character 52 of the complete onion address also contains checksum bits; suffix patterns refer to the standalone key representation.

The search continues until Ctrl+C or an error. Cancellation finishes the current 512-key batch, including saving its matches. Successfully saved hostnames are printed to stdout; status and final counts go to stderr.

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

- **Fixed-step Edwards addition:** each candidate advances an Edwards25519 point by `8*B`, using a targeted seven-multiplication mixed-addition formula instead of a full scalar multiplication. The stride preserves scalar clamping.
- **Fused scalar arithmetic:** four full-width 64-bit limbs replace the general-purpose five-limb representation. On amd64 CPUs with BMI2 and ADX, a fused assembly leaf performs the complete mixed addition, reducing calls and temporary copies. It shares a 16-product multiplication kernel using `MULX`, independent `ADCX`/`ADOX` carry chains and modular carry folding. Backend selection happens once for the forward batch pass; PACE calls the assembly through the register ABI.
- **Batched inversion:** 512 independent search lanes share one field inversion per batch. Point advancement and inversion-product accumulation use one forward pass, followed by a reverse encoding pass.
- **Deferred sign completion:** the ordinary miss path produces canonical Y bytes and first checks a filter that ignores only the unknown sign bit. Surviving candidates get their real affine X/sign and pass exact matching before acceptance. The sign affects base32 character 50, one-based. Inverse Z reuses expired prefix-product scratch, while projective X stays intact for the next step. One-character alternatives retain eager sign completion.
- **Cache-conscious state:** hot coordinates, inversion products and public keys occupy 96 KiB. Expanded secrets and lane epochs are stored separately. Complete-search benchmarks screen batches from 128 through 2048; 512 remains the default to balance throughput, memory and cancellation latency.
- **Direct matching:** `internal/pattern` matches raw key bytes without constructing base32 strings. The ordinary miss path performs no SHA3 hashing, random reads or heap allocations. Small pattern sets retain specialized scalar and AVX2 fast paths.
- **Large dictionaries:** sets with at least 128 eligible unanchored patterns use a compact 15-bit triplet membership bitmap and verification buckets. Eligible literals have at least three characters and nine possible positions. Shared triplets are filtered once, all associated exact verifiers are retained and candidate windows are reused after false positives. This avoids large per-pattern, per-position verification tables.
- **Independent saved keys:** each lane is seeded independently from `crypto/rand` and gets a fresh seed immediately after a saved hit. This avoids exporting multiple related secret scalars from one walk while retaining every match in a batch. Walks also have a bounded reseeding epoch and reserve scalar headroom before starting.

BMI2/ADX arithmetic and AVX2 matching are detected independently. AVX2 dispatch checks CPU support, OSXSAVE and XGETBV vector-state support; SIMD uses AVX2 at most. Portable arithmetic and matching remain available and `-tags purego` disables both assembly paths.

The specialized point representation, fixed-step formula and field backend live in `internal/search`. Initial scalar multiplication, the once-per-batch inversion chain and independent saved-key validation use `filippo.io/edwards25519`. The synchronous `search.Run` API accepts a compiled matcher, context and save callback. The CLI sets `GOMAXPROCS(1)`.

## Measurements

PACE 1.27.1, Windows 11/amd64, AMD Ryzen 9 9950X3D with BMI2, ADX and AVX2; `GOAMD64=v1`, `GOMAXPROCS=1`, one worker pinned to logical CPU 2. Measurements ran serially, with five one-second samples per workload. The baseline is revision `06aca2c`, before deferred signs, fused advancement and dictionary buckets. Both columns use the same PACE toolchain.

| Workload | Before, ns/key | Current, ns/key | Current keys/second |
| --- | ---: | ---: | ---: |
| Full search, rare prefix | 89.14 | 79.23 | 12.62 million |
| Full search, frequent `ab.` | 97.70 | 86.36 | 11.58 million |
| Full search, every candidate hits | 7915 | 7931 | 126,100 |
| Generation, complete signed keys | 88.19 | 85.06 | 11.76 million |
| Generation + prefix matching | 88.32 | 77.83 | 12.85 million |
| Generation + anywhere matching | 97.48 | 87.18 | 11.47 million |
| Generation + 64-pattern matching | 122.60 | 114.20 | 8.76 million |
| Generation + 512-pattern matching | 327.50 | 144.20 | 6.93 million |
| Generation + 512 shared-triplet patterns | 309.40 | 134.80 | 7.42 million |

Values are medians; all listed benchmarks report **0 B/op and 0 allocs/op**. Rare-prefix full-search throughput improves by about 12.5%, while 512-pattern generation/matching improves by 2.27x. Rare-search samples ranged from 88.99-91.75 ns/key before to 78.71-79.46 afterward. All-hit full-search results overlap and are unchanged within noise.

Generation/matching benchmarks omit hit handling. Full-search benchmarks use the production batch loop with statistics, immutable key snapshots, a cheap synchronous callback and independent reseeding after every hit; reproducible SHAKE entropy replaces OS random acquisition. Both exclude startup and context polling between batches. Filesystem persistence is measured separately and took about 2.6 ms per saved match on this machine, including validation and synchronized file writes. CLI throughput therefore depends strongly on the hit rate and storage.

See [the experiment notes](measurements/NOTES.md) for the complete workload matrix, sample ranges, raw results, reproduction commands and accepted/rejected experiments. Larger dictionaries trade a small first-pattern-hit cost for faster misses and false-positive handling. Batch-size optima and matcher crossover points vary by machine; these results are not compiler-independent throughput guarantees.

On Windows, reproduce the pinned measurements and compare the recorded distributions with:

```powershell
powershell.exe -NoProfile -File tools/measure.ps1 -Name local
bun tools/compare.mjs measurements/baseline.txt measurements/local.txt
```

The comparison helper uses Bun; building and running onino does not require it. Compare runs only with matching compiler, environment and benchmark settings.

## Verification

```sh
pace test ./...
go test ./...
pace test -tags purego ./...
go test -tags purego ./...
vet --tests ./...
vet --tests --tags purego ./...
pace test ./internal/search -run '^$' -bench '^(BenchmarkWorkloads|BenchmarkGeneration|BenchmarkFullSearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test ./internal/search -run '^$' -bench '^BenchmarkSearchSizes$' -benchmem -benchtime=250ms -count=2 -cpu=1
pace test ./internal/search -run '^$' -fuzz '^FuzzFieldArithmetic$' -fuzztime=10s -parallel=1
pace test ./internal/search -run '^$' -fuzz '^FuzzAdvancement$' -fuzztime=10s -parallel=1
pace test ./internal/pattern -run '^$' -fuzz '^FuzzSignFilter$' -fuzztime=10s -parallel=1
pace test ./internal/pattern -run '^$' -fuzz '^FuzzDictionary$' -fuzztime=10s -parallel=1
```

Run benchmarks serially with `GOMAXPROCS=1`, `GOAMD64=v1` and consistent affinity when comparing results. Repeat the bounded fuzz commands with `-tags purego` to exercise portable paths. The `purego` test matrix covers both arithmetic and matching fallbacks.

Tests compare field arithmetic against `math/big`, including boundary values, noncanonical limbs, carry chains, aliasing and 20,000 deterministic random operand pairs. Fused advancement is checked against the existing formula; completed batches are checked against independent scalar multiplication and Go's standard Ed25519 signature verifier. Matcher tests cover both signs, prefixes of lengths 49-52, sign-crossing suffix/interior/anywhere patterns, overlapping anchors, final-symbol padding, shared triplets and false-positive continuation against independent base32/string references. Multiple-hit batches, per-hit reseeding, continuous saving, published Tor address vectors, exact key-file bytes and zero-allocation search assertions remain covered.
