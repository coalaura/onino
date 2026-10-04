# onino

CPU-only vanity v3 `.onion` address search. The current engine uses one worker, checks every generated public key against compiled patterns and keeps searching after saving matches.

## Build and use

Build with PACE to enable register-ABI assembly calls and explicit inlining in the matcher and search engine:

```sh
pace build -o onino .
./onino --output matches 'hello.' 'onino.'
```

On Windows, build with `pace build -o onino.exe .` and invoke `./onino.exe`. Stock `go build` is also supported.

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
- **Targeted field arithmetic:** four full-width 64-bit limbs replace the general-purpose five-limb representation. On amd64 CPUs with BMI2 and ADX, a 16-product multiplication kernel uses `MULX`, independent `ADCX`/`ADOX` carry chains and modular carry folding. PACE calls it directly through the register ABI. CPU detection selects a portable `math/bits` implementation otherwise; `-tags purego` also selects that fallback.
- **Batched inversion:** 512 independent search lanes share one field inversion per batch. Point advancement and inversion-product accumulation use one forward pass, followed by a reverse encoding pass.
- **Cache-conscious state:** hot coordinates, inversion products and public keys occupy 96 KiB, down from 116 KiB with five-limb fields. Expanded secrets and lane epochs are stored separately from those hot arrays. Benchmarks sweep batch sizes from 1 through 2048.
- **Direct matching:** every compressed 32-byte public key goes straight to `internal/pattern`. The candidate loop performs no base32 encoding, SHA3 hashing, random reads or heap allocations. The matcher uses AVX2 when supported by both the CPU and OS, with a portable fallback.
- **Independent saved keys:** each lane is seeded independently from `crypto/rand` and gets a fresh seed immediately after a saved hit. This avoids exporting multiple related secret scalars from one walk while retaining every match in a batch. Walks also have a bounded reseeding epoch and reserve scalar headroom before starting.

The specialized point representation, fixed-step formula and field backend live in `internal/search`. Initial scalar multiplication, the once-per-batch inversion chain and independent saved-key validation use `filippo.io/edwards25519`. The synchronous `search.Run` API accepts a compiled matcher, context and save callback. The CLI sets `GOMAXPROCS(1)`.

## Measurements

PACE, Windows/amd64, AMD Ryzen 9 9950X3D, BMI2/ADX enabled, one worker, medians of five 1-second runs:

| Generation and matching workload | Keys/second | ns/key | Allocations |
| --- | ---: | ---: | ---: |
| One rare prefix | 11.23 million | 89.08 | 0 |
| One rare substring | 10.05 million | 99.48 | 0 |
| Eight rare substrings | 9.61 million | 104.0 | 0 |

The previous five-limb implementation measured 8.09, 7.56 and 7.45 million keys/s respectively on this machine using three 300 ms runs. The new prefix result is approximately 39% higher. The targeted point step measured 49.2 ns against 99.5 ns for general point addition; general addition with individual encoding measured 1,967 ns and seed-based key generation measured 9,542 ns excluding random-seed acquisition.

Search measurements include generation and matching of every candidate, with warmed state and rare-match patterns. Startup seeding and the match-saving/reseeding path are outside the timed loop. Frequent matches include filesystem costs and have lower end-to-end throughput. Results depend on CPU, compiler and workload.

## Verification

```sh
pace test ./...
go test ./...
pace test -tags purego ./internal/search ./internal/onion
vet --tests ./...
pace test ./internal/search -run '^$' -bench '^(BenchmarkSearch|BenchmarkCurveStep|BenchmarkSeedKeyGeneration)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test ./internal/search -run '^$' -bench '^BenchmarkBatchSizes$' -benchmem -cpu=1
pace test ./internal/search -run '^$' -fuzz '^FuzzFieldArithmetic$' -fuzztime=30s -parallel=4
```

Tests compare both field multiplication backends, addition, subtraction, canonical encoding, sign extraction, inversion and aliased operations against `math/big`, including boundary values and 20,000 deterministic random operand pairs. A fuzz target exercises the same differential checks. Search tests compare batches against independent scalar multiplication, verify signatures with Go's standard Ed25519 verifier, exercise exceptional curve points and scalar carries, check per-hit reseeding and continuous saving, validate published Tor address vectors and exact key-file bytes and assert zero allocations in the candidate loop.
