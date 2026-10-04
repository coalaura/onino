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
- **Fused scalar arithmetic:** four full-width 64-bit limbs replace the general-purpose five-limb representation. On amd64 CPUs with BMI2 and ADX, assembly leaves fuse complete mixed addition and deferred-sign reverse normalization, reducing calls and temporary copies. They share a 16-product multiplication kernel using `MULX`, independent `ADCX`/`ADOX` carry chains and modular carry folding. Backend selection happens outside each fused pass; constant memory displacements avoid redundant address arithmetic. PACE uses the natural register-ABI argument assignments without extra adapters.
- **Batched inversion:** 512 independent search lanes share one field inversion per batch. Point advancement and inversion-product accumulation use one forward pass, followed by a reverse encoding pass.
- **Deferred sign completion:** the ordinary miss path produces canonical Y bytes and first checks a filter that ignores only the unknown sign bit. Surviving candidates get their real affine X/sign and pass exact matching before acceptance. The sign affects base32 character 50, one-based. Inverse Z reuses expired prefix-product scratch, while projective X stays intact for the next step. One-character alternatives retain eager sign completion.
- **Cache-conscious state:** hot coordinates, inversion products and public keys occupy 96 KiB. Expanded secrets and lane epochs are stored separately. Complete-search benchmarks screen batches from 128 through 2048; 512 remains the default to balance throughput, memory and cancellation latency.
- **Direct matching:** `internal/pattern` matches raw key bytes without constructing base32 strings. The ordinary miss path performs no SHA3 hashing, random reads or heap allocations. Small pattern sets retain specialized scalar and AVX2 fast paths.
- **Strided dictionaries:** sets with at least 128 eligible unanchored patterns use a compact 15-bit triplet membership bitmap and verification buckets. Eligible literals have at least six characters and nine possible positions. Scanning every fourth triplet is safe because each pattern contributes anchors covering every offset residue, sharing one exact verifier. Short literals retain their existing paths. PACE inlines the hot window and literal checks without requiring PGO.
- **Anchored dictionaries:** sets with at least 64 eligible anchored patterns use fixed-position 15-bit indexes, with complete bit-mask verification after a bucket hit. Prefixes need at least three symbols and suffixes four. Combined patterns choose the less-populated eligible fragment. Large unanchored dictionaries can absorb anchored alternatives into their shared triplet index.
- **Independent saved keys:** each lane is seeded independently from `crypto/rand` and gets a fresh seed immediately after a saved hit. This avoids exporting multiple related secret scalars from one walk while retaining every match in a batch. Walks also have a bounded reseeding epoch and reserve scalar headroom before starting.

BMI2/ADX arithmetic and AVX2 matching are detected independently. AVX2 dispatch checks CPU support, OSXSAVE and XGETBV vector-state support; SIMD uses AVX2 at most. Portable arithmetic and matching remain available and `-tags purego` disables both assembly paths.

The specialized point representation, fixed-step formula and field backend live in `internal/search`. Initial scalar multiplication, the once-per-batch inversion chain and independent saved-key validation use `filippo.io/edwards25519`. The synchronous `search.Run` API accepts a compiled matcher, context and save callback. The CLI sets `GOMAXPROCS(1)`.

## Measurements

PACE 1.27.1, Windows 11/amd64, AMD Ryzen 9 9950X3D with BMI2, ADX and AVX2; `GOAMD64=v1`, `GOMAXPROCS=1`, no PGO, one worker pinned to logical CPU 2. Measurements ran serially with five alternating one-second samples per workload. The baseline is revision `adb389f`, which already includes the first optimization pass. Both columns use the same PACE toolchain.

| Workload | Before, ns/key | Current, ns/key | Current keys/second |
| --- | ---: | ---: | ---: |
| Full search, rare prefix | 77.00 | 72.39 | 13.81 million |
| Full search, frequent `ab.` | 84.46 | 79.82 | 12.53 million |
| Full search, every candidate hits | 7685 | 7668 | 130,400 |
| Full search, 512 anywhere patterns | 138.20 | 101.80 | 9.82 million |
| Full search, 512 shared-triplet patterns | 130.30 | 96.81 | 10.33 million |
| Full search, 64 prefixes | 95.30 | 74.63 | 13.40 million |
| Full search, 512 prefixes | 224.20 | 74.77 | 13.37 million |
| Full search, 512 suffixes | 229.00 | 74.75 | 13.38 million |
| Full search, 512 combined anchors | 224.20 | 74.77 | 13.37 million |
| Generation + anywhere matching | 85.46 | 80.83 | 12.37 million |
| Generation + 64-pattern matching | 109.60 | 104.90 | 9.53 million |

Values are medians; all listed benchmarks report **0 B/op and 0 allocs/op**. Rare-prefix throughput improves by about 6.4%, 512-anywhere throughput by 35.8% and 512-prefix throughput by about 3x in this comparison. Rare-search samples ranged from 76.90-77.05 ns/key before to 72.36-72.43 afterward. The 0.2% all-hit difference is practically flat and sensitive to binary layout. These are one-CPU elapsed-time measurements, not direct CPU-time or hardware-counter accounting.

Generation/matching benchmarks omit hit handling. Full-search benchmarks use the production batch loop with statistics, immutable key snapshots, a cheap synchronous callback and independent reseeding after every hit; reproducible SHAKE entropy replaces OS random acquisition. Both exclude startup and context polling between batches. Filesystem persistence is measured separately and took about 2.6 ms per saved match on this machine, including validation and synchronized file writes. CLI throughput therefore depends strongly on the hit rate and storage.

On Windows, reproduce the pinned measurements and compare the recorded distributions with:

```powershell
powershell.exe -NoProfile -File tools/measure.ps1 -Name local
bun tools/compare.mjs measurements/pass2-final-after.txt measurements/local.txt
# For a change, build before.exe and after.exe from the respective source versions:
pace test -vet=off -c -pgo=off -o measurements/after.exe ./internal/search
powershell.exe -NoProfile -File tools/measure-pair.ps1 -Baseline measurements/before.exe -Candidate measurements/after.exe -Name local-pair -Bench '^BenchmarkFullSearch$'
bun tools/compare.mjs measurements/local-pair-before.txt measurements/local-pair-after.txt
```

The comparison helper uses Bun; building and running onino does not require it. Compare runs only with matching compiler, environment and benchmark settings.

## Verification

```sh
pace test -vet=off ./...
go test -vet=off ./...
pace test -vet=off -tags purego ./...
go test -vet=off -tags purego ./...
vet --tests ./...
vet --tests --tags purego ./...
pace test -vet=off ./internal/search -run '^$' -bench '^(BenchmarkWorkloads|BenchmarkFullSearch|BenchmarkDictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test -vet=off ./internal/search -run '^$' -bench '^BenchmarkSearchSizes$' -benchmem -benchtime=250ms -count=2 -cpu=1
pace test -vet=off ./internal/search -run '^$' -fuzz '^FuzzFieldArithmetic$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/search -run '^$' -fuzz '^FuzzAdvancement$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/search -run '^$' -fuzz '^FuzzNormalization$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzSignFilter$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzDictionary$' -fuzztime=10s -parallel=1
pace test -vet=off ./internal/pattern -run '^$' -fuzz '^FuzzAnchoredDictionary$' -fuzztime=10s -parallel=1
```

Run benchmarks serially with `GOMAXPROCS=1`, `GOAMD64=v1` and consistent affinity when comparing results. Repeat the bounded fuzz commands with `-tags purego` except `FuzzNormalization`, which targets the amd64 assembly leaf. The `purego` test matrix covers both arithmetic and matching fallbacks. Use the custom `vet` tool for static checks; tests disable the built-in vet invocation.

Tests compare field arithmetic against `math/big`, including boundary values, noncanonical limbs, carry chains, aliasing and 20,000 deterministic random operand pairs. Fused advancement is checked against the existing formula; completed batches are checked against independent scalar multiplication and Go's standard Ed25519 signature verifier. Matcher tests cover both signs, prefixes of lengths 49-52, sign-crossing suffix/interior/anywhere patterns, overlapping anchors, final-symbol padding, shared triplets and false-positive continuation against independent base32/string references. Multiple-hit batches, per-hit reseeding, continuous saving, published Tor address vectors, exact key-file bytes and zero-allocation search assertions remain covered.
