<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/banner.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/banner-light.svg">
	<img alt="onino - vanity .onion search with optional GPU acceleration. A racing onion wordmark; oni[on ⇄ no] flips onion's final two letters." src=".github/banner-light.svg">
</picture>

onino searches for vanity Tor v3 `.onion` addresses using CPU workers with optional Vulkan GPU acceleration. It matches the first **52 visible hostname characters**, saves usable service keys and keeps searching. By default it uses one CPU search worker and no GPU.

## Build and use

PACE is the primary build and release target, enabling register-ABI assembly calls and targeted inlining. Use a toolchain compatible with Go 1.27.1, as declared in `go.mod`. Ordinary builds require neither CGO nor Vulkan:

```sh
pace build -o onino .
```

On Windows, build with `pace build -o onino.exe .` and invoke `./onino.exe`. Stock `go build` is also supported. For an accelerated build, see [GPU support](#gpu-support).

```sh
./onino --cpu all 'hello.' 'onino.'
./onino --patterns prefixes.txt -o onions

# GPU-enabled builds:
./onino --cpu all --gpu auto 'helloworld.'
./onino --cpu off --gpu auto 'helloworld.'
```

| Option | Default | Values and behavior |
| --- | --- | --- |
| `--cpu` | `1` | Positive decimal worker count up to the process's available logical CPUs or `all`. With GPU search enabled, `0` or `off` disables CPU search workers. |
| `--simd` | `auto` | `auto` selects supported CPU acceleration; `avx2` disables onino's optional AVX-512 paths. Both retain the binary's compilation baseline and existing fallbacks. |
| `--output`, `-o` | `matches` | Destination directory for saved matches. |
| `--patterns` | None | UTF-8 pattern-file path; see syntax below. |
| `--gpu` + | `off` | `off`, `auto` or a nonnegative Vulkan physical-device index. `auto` prefers a capable discrete GPU. |
| `--gpu-streams` + | Automatic if omitted | Explicit integer `1-16384`; independent resident search streams. Requires GPU search enabled. |
| `--gpu-rounds` + | Automatic if omitted | Explicit integer `1-64`; rounds per submission. Requires GPU search enabled. |
| `--gpu-diagnostics` + | `false` | Boolean; detailed GPU diagnostics on stderr when GPU search is enabled. |
| `--help`, `-h` | - | Show help for this build. |

+ Available only in builds made with `-tags gpu`. Selecting a device with `--gpu` and tuning its workload are separate operations; see [GPU support](#gpu-support).

Patterns are ORed together. Supported forms are `prefix.`, `.suffix`, `prefix.suffix`, `.interior.` and `anywhere`; see [the matcher documentation](internal/pattern/README.md) for precise rules and validation.

Patterns use lowercase `a-z2-7`. A suffix ends at visible character 52: `.aaa` produces a hostname shaped like `...aaa????.onion`, with four further checksum/version characters. Character 52 mixes one public-key bit with four checksum bits; exact matching includes those bits.

Supply positional patterns **or** `--patterns`, never both. A file contains one pattern per line, for example `hello.` and `onino.` on separate lines. Surrounding whitespace, blank lines and lines whose first non-whitespace character is `#` are ignored; inline comments are not supported. An initial UTF-8 BOM and CRLF line endings are accepted. Empty/comment-only files are rejected; errors in individual patterns identify the file and physical line. Setup reports the input count and source instead of dumping large lists; duplicate entries still count toward that input count.

Startup presents one selected-setup block on stderr, including input source/count, CPU/GPU configuration, startup timing and output location. A single-line progress report every **five seconds** shows elapsed time, combined checked work, recent throughput, successful saves and estimated waits **from now** for a 50%/95% chance of a match. Estimates account for overlapping patterns and checksum constraints but assume independent uniform candidates; they are guidance, not deadlines. Intermediate counters are approximate.

The search continues until Ctrl+C or an error. Cancellation stops new work at backend boundaries and drains accepted work and saves; frequent matches or slow storage can delay shutdown. One final stderr block gives exact CPU, GPU and total counts. All final **overall averages use the same full-run wall time**, from search-action entry before input preparation through accepted-save drainage and backend teardown, rather than backend-active time. Successful saves go to stdout with the hostname, time since the previous discovery and elapsed time at discovery; saving delays do not inflate these timestamps and out-of-order discoveries use a zero inter-match interval.

## GPU support

GPU builds require CGO, a C11 compiler and `-tags gpu`. The bridge vendors its Vulkan headers and embeds committed SPIR-V, so building does **not** require a Vulkan SDK or shader-development tools. On Windows with builder installed:

```sh
builder build go --cgo --pace --dyn --no-gen -tags gpu
```

Omit `--pace` for stock Go. Linux requires dynamic linking to a libc compatible with the installed Vulkan loader/driver; fully static musl GPU binaries are unsupported. See [GPU build instructions](internal/gpu/BUILD.md) for native Linux linking, cross-compilation, shader generation and validation.

At runtime, the system Vulkan loader and driver must support Vulkan 1.3, shader 64-bit integers, compute timestamps and the required workgroup/storage limits, with device-local storage and host-visible staging memory. The backend is designed for Vulkan portability, but runtime validation and performance measurements cover only the **RTX 5090 on Windows**; Linux cross-builds are not runtime validation. Initialization failures are errors, not silent CPU fallback. GPU-only search still uses the host CPU for orchestration, verification and saving.

GPU search accepts **1-8 literal anchored prefixes**, each **1-51 lowercase base32 characters followed by a dot**, such as `hello.`. Every input entry must qualify, including in combined CPU+GPU mode and pattern files; other forms or hundreds of file entries are not supported. The GPU filters at most the first twelve characters, then the host independently verifies the full pattern and key.

Automatic calibration makes a bounded selection during useful search with the requested CPU workers active: checked work and matches are retained. Its ten-second startup target includes input preparation and driver initialization, but cannot bound blocking driver calls. Advanced controls `--gpu-streams` and `--gpu-rounds` each fix only their own parameter; specifying one leaves the other automatic, while specifying both skips performance exploration but retains useful-work validation. Unsupported or unresponsive explicit settings fail rather than being silently changed; automatic settings can back off during search.

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

CPU workers usually generate paired candidates around 256 independent affine Edwards25519 centers, sharing inversions and completing signs only for filter survivors. Frequent short patterns use projective walks; specialized filters and dictionary indexes handle different pattern sets. CPU arithmetic, matching and checksum acceleration are selected independently; `-tags purego` disables native assembly. GPU search instead uses resident streams and separate preparation, packed inversion and reconstruction passes. Both backends independently verify saved keys and export at most one key per seed.

`--cpu` controls search workers, not total host threads. Windows/Linux process affinity limits are respected; discovered topology places workers on physical cores before SMT siblings and spreads them across last-level caches, with OS placement as the fallback. A sole CPU-only worker is unpinned; GPU mode also pins a single search worker when topology is available. The runtime uses `GOMAXPROCS=workers` without GPU search and `workers+2` with it. CPU workers use a 64-entry saver queue; GPU verification/save queues are bounded by allocated streams. Persistence is serialized across backends.

[RESEARCH.md](RESEARCH.md) covers CPU arithmetic, matching, SIMD, worker coordination and their historical experiments. [RESEARCH-GPU.md](RESEARCH-GPU.md) covers resident Vulkan execution, useful-search calibration, combined CPU/GPU measurements and portability limits. Each preserves its own measurement boundaries and baselines.

## Benchmarks

CPU-only prefix searches on an **AMD Ryzen 9 9950X3D**, Windows 11, with **32 workers** and normal key-file output. `--simd auto` (AVX-512 on this CPU) was refreshed for onino v0.2.1; forced AVX2 and competitor results are retained from the previous comparison. Onionloom used `--gpu off`. Rates are **million candidates/second**; higher is better.

<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/prefix-comparison.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/prefix-comparison-light.svg">
	<img alt="Median CPU-only throughput for onino AVX2, onino auto, onionloom and mkp224o across four prefix workloads, including a no-match case, on a shared zero-based scale." src=".github/prefix-comparison-light.svg">
</picture>

**Median throughput**

| Prefixes | onino AVX2 | onino auto | onionloom | mkp224o |
| --- | ---: | ---: | ---: | ---: |
| `hello` | 416.4 | **1,249.4** | 343.4 | 129.5 |
| `privacy` | 422.9 | **1,290.5** | 345.2 | 130.3 |
| `donate`, `mirror`, `secure` | 413.8 | **1,269.2** | 332.8 | 111.2 |
| `somethingrare` (no matches) | 422.9 | **1,289.0** | 345.1 | 130.4 |

**Min-max throughput**

| Prefixes | onino AVX2 | onino auto | onionloom | mkp224o |
| --- | ---: | ---: | ---: | ---: |
| `hello` | 409.4-422.0 | 1,229.6-1,268.5 | 338.6-345.2 | 129.0-130.3 |
| `privacy` | 419.2-424.8 | 1,242.8-1,303.0 | 341.7-345.8 | 129.5-131.4 |
| `donate`, `mirror`, `secure` | 408.1-416.2 | 1,264.4-1,286.6 | 330.1-334.6 | 110.6-112.5 |
| `somethingrare` (no matches) | 418.2-423.7 | 1,278.1-1,305.8 | 338.3-345.6 | 129.5-131.2 |

Each configuration has five samples per workload, run sequentially with all 32 logical CPUs available. The original comparison rotated tool order; the v0.2.1 refresh ran only auto mode. Timed windows lasted 20-32 seconds after warm-up, bounded by progress reports; rates use cumulative candidate-count deltas over wall time, not peak displayed rates. Multiple prefixes match any listed prefix. The 13-character `somethingrare` prefix produced **zero matches and zero key files in every run**, isolating search throughput from match-saving I/O. Incomplete trials were excluded and rerun; four refreshed `hello` samples used temporary key-output directories after filesystem rename failures. [Raw samples](.github/prefix-comparison.csv) are available.

Tested versions (2026-10-06):

- onino v0.2.1 auto / [v0.2.0](https://github.com/coalaura/onino/releases/tag/v0.2.0) AVX2 - PACE Go 1.27.1, `GOAMD64=v1`, PGO disabled.
- [onionloom v1.0.1](https://github.com/chrisch88dev/onionloom/releases/tag/v1.0.1) - official Windows release.
- [mkp224o v1.7.0](https://github.com/cathugger/mkp224o/releases/tag/v1.7.0) - official Windows release.

## Optimization history

In CPU-only single-worker measurements on the Ryzen 9 9950X3D under Windows 11, rare-prefix search improved from **125.5 to 14.37 ns/key (8.74x throughput)** from the first batched engine to v0.2.1 with AVX-512, while matching against 512 anywhere patterns improved from **348.3 to 41.32 ns/key (8.43x)**. Retained v0.2.0 forced-AVX2 measurements are **39.22 ns/key** and **62.45 ns/key**, respectively.

| Single-worker search | v0.2.0 `--simd avx2`, ns/key | v0.2.1 `--simd auto`, ns/key |
| --- | ---: | ---: |
| Rare prefix | 39.22 [39.19-40.22] | **14.37 [14.27-14.48]** |
| Frequent prefix | 48.49 [48.47-48.55] | **23.73 [23.69-23.75]** |
| 512 anywhere patterns | 62.45 [62.41-62.50] | **41.32 [41.23-41.52]** |

<picture>
	<source media="(prefers-color-scheme: dark)" srcset=".github/performance-history.svg">
	<source media="(prefers-color-scheme: light)" srcset=".github/performance-history-light.svg">
	<img alt="Single-worker search performance through v0.2.1 AVX-512, retaining the earlier milestones and v0.2.0 comparison, with median points and full-range whiskers." src=".github/performance-history-light.svg">
</picture>

Each point shows median single-worker performance; brackets and whiskers show the full measured range, including plateaus and regressions. The v0.2.1 auto refresh used ten samples of 51.2 million candidates per workload after warm-up, pinned to logical CPU 2 with `GOMAXPROCS=1`, `-cpu=1`, `-parallel=1` and PGO disabled. These in-process searches use deterministic entropy and a no-op save callback; all measured zero allocations. Historical points are preserved, including the ten alternating v0.2.0 AVX2/auto samples. The new single-worker results are slightly slower than those earlier auto samples; this refresh is not a paired version comparison. The [research notes](RESEARCH.md#appendix-d-cumulative-performance-history) cover the earlier experiments, with [raw samples](.github/performance-history.csv) available separately.

## Verification

```sh
pace test -vet=off ./...
go test -vet=off ./...
pace test -vet=off -tags purego ./...
go test -vet=off -tags purego ./...
vet --tests ./...
vet --tests --tags purego ./...
```

Use the custom `vet` tool for static checks; tests disable the built-in vet invocation. This CPU-only native/purego matrix exercises actual arithmetic and matcher fallbacks. GPU device checks and validation-layer requirements are in [internal/gpu/BUILD.md](internal/gpu/BUILD.md#device-correctness-checks).

Tests cover independent scalar multiplication and Ed25519 signatures, scalar boundaries and table transitions, reseeding and saved-key independence, concurrent hits, exact shutdown counters, callback serialization, CPU selection/affinity cleanup, cancellation, Tor file validation and zero allocations. Field arithmetic is checked against `math/big`; matchers are compared with independent base32/string references. Linux native/purego race tests pass; the Windows race runtime could not start on the measurement host. Detailed verification scope is documented in [RESEARCH.md](RESEARCH.md#verification).
