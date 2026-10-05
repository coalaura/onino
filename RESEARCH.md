# Search design and optimization results

onino optimizes complete searches around a single-worker engine: a useful candidate must have canonical public-key bytes, an exact match decision and an independently valid expanded secret. Generating intermediate coordinates cheaply is insufficient if conversion, sign recovery or reseeding costs more afterward. Multicore execution gives each worker an independent instance of that engine. PACE is the performance target; stock Go and portable paths remain compatibility requirements.

## Paired affine generation

### Arithmetic

For Edwards25519, `p = 2^255-19`, `a = -1` and `d = -121665/121666`. Each affine center `P` caches `xP`, `yP` and `xP*yP`. Immutable offsets `Qj = j.8B`, for `j = 1...64`, cache `xQ`, `yQ` and `d*xQ*yQ`. A pair is formed as follows:

```text
a = xP*xQ
b = yP*yQ
c = d*(xP*yP)*(xQ*yQ)
r = 1/(1-c²)
t = c*r

plusInverse  = r+t = 1/(1-c)
minusInverse = r-t = 1/(1+c)

y(P+Q) = (b+a)*plusInverse
y(P-Q) = (b-a)*minusInverse
x(P+Q) = (xP*yQ + yP*xQ)*minusInverse
x(P-Q) = (xP*yQ - yP*xQ)*plusInverse
```

The 256 centers share one inversion of the product of `1-c²`. Both reconstructed reciprocals survive matching, so a filter survivor needs three multiplications to recover X and its exact sign, with no additional inversion. The two Y values are serialized canonically before filtering. The sign filter ignores only byte 31's high bit, which affects base32 character 50; the complete matcher runs after sign recovery.

These denominators are nonzero for valid affine points over this field: `a` is a square and `d` is a nonsquare, the complete twisted-Edwards addition case. Tests include identity order-two/order-four points, positive/negative offsets and independently multiplied search keys. The field-character assumptions are also checked with `math/big`.

Write `M`, `S` and `I` for field multiplication, squaring and inversion. One pair costs approximately **9M+1S**, plus **I/256 pairs**: three cached-coordinate products, one square, three multiplications for batch inversion, one shared reciprocal product and two Y products. The batch endpoints save three multiplications overall. This is 4.5M+0.5S per generated candidate before byte serialization and matching; sign recovery adds 3M only for filter survivors. BMI2/ADX denominator preparation uses a dedicated square; portable squaring uses multiplication.

Every 64 offsets, centers advance by `129.8B`, sharing another inversion. A transition costs approximately 12M+1S per center, amortized across 128 candidates. Full-search timings include these transitions, sign completion, statistics, copying saved keys, discarded relatives and independent reseeding. Reseeding includes entropy acquisition, SHA-512, clamping/headroom checks, base multiplication, affine normalization and rebuilding the cached product.

### Scalar and ownership invariants

Each center starts from an independent expanded secret at offset 64, so subtracting the largest table offset cannot move below its seed scalar. Advancing by 129 steps makes successive center windows disjoint; the center itself is skipped. Scalar offsets are multiples of eight, preserve the nonce prefix and reserve the existing `2^32`-step epoch headroom below the clamping boundary. Centers reseed before the next window would exceed that bound.

A saved key is a value snapshot. A hit on the plus side invalidates its pending minus relative before the center is reseeded; a minus-side hit has no remaining sibling. Later candidates from that seed are never exported. Skipped relatives do not increment `Checked` and generation replenishes them until the batch contains 512 actual checks. Other centers' pending candidates remain valid. Cancellation is observed between these bounded batches, retaining the existing synchronous save semantics.

The paired state occupies approximately 114 KiB on amd64, including secrets, scratch and public keys, with about 6 KiB of shared offset data. Only the selected engine is initialized. Matching data is separate and immutable.

### Native implementation

The portable Go engine was validated and benchmarked before assembly was added. Its complete rare-search screen measured roughly 48 ns/key; fused preparation and reverse reciprocal passes measured about 43 ns/key. These implementation screens establish why the fused passes were retained. Current compiler measurements are reported separately below.

The amd64 leaves reuse the four-limb BMI2/ADX multiply core. PACE's natural argument registers avoid adapters; the routines are zero-frame, NOSPLIT leaves without calls. X registers preserve pointers across the multiplication core; R14 and X15 are untouched. The emitted assembler listings were checked, including the absence of AVX instructions in these arithmetic leaves. Constant memory displacements and expired prefix-product scratch avoid unnecessary address updates and copies.

BMI2/ADX arithmetic dispatch is independent of AVX2 matcher dispatch. AVX2 is the SIMD ceiling and requires CPU, OSXSAVE and XGETBV support. `purego` selects real portable arithmetic and matching, not an assembly-backed simulation.

## Multicore search

### Ownership and coordination

This pass started from clean SHA `ccf7acd2ac359ee666b77ba997f4eb4e5ee93379`. The unchanged checkout was compiled and measured before edits; its test binary was retained for alternating comparisons. The arithmetic, matcher dispatch, AVX2 ceiling and existing `Run`/`RunWithProgress` implementations remain unchanged. `RunWithOptions` dispatches directly to the existing path for one unpinned worker. The CLI defaults to `--cpu 1`, resolves `all` against process availability, rejects excessive counts and sets `GOMAXPROCS` once. Libraries do not set it.

Parallel workers persist for the search lifetime. Each initializes after pinning and owns its generator, independent OS-secure seeds, pending candidates, scratch and ordinary counters. There are no candidate queues or recurring barriers. Workers check cancellation between existing 512-checked-candidate batches and publish cumulative counters every 128 batches into separate 256-byte atomic slots. Padding isolates writers even if the allocation is not cache-line aligned. A coordinator samples about every four seconds; publication can lag by up to 65,536 checks per active worker. Final publication and joining every worker make returned totals exact, including partially completed failed batches.

Only hits acquire the save mutex. Save callbacks and progress callbacks never overlap; a busy save makes the coordinator skip that reporting tick. Successful saves alone increment `Saved` and the existing value-snapshot, pending-sibling invalidation and independent-reseeding rules apply unchanged. Startup, worker, save and affinity-cleanup failures stop peers and retain substantive errors instead of replacing them with cancellation. Normal cancellation drains in-flight batches. Arbitrary synchronous callbacks cannot be interrupted: a callback must eventually return for shutdown to finish. The completion channel carries one notification per worker, never search work.

Windows uses CPU-set topology, process CPU-set/hard-affinity restrictions and group-aware thread affinity. Linux uses `sched_getaffinity` plus sysfs physical-core/cache topology. Missing topology leaves OS placement with an explicit startup message; discovery or requested pinning failures are errors. Other systems use `runtime.NumCPU` and report unsupported topology/affinity. Each pinned worker locks its OS thread before applying affinity and creating state. Cleanup restores prior affinity before unlocking; failed restoration retires the locked thread. Windows machines with multiple processor groups conservatively retire restored worker threads too, because a single `GROUP_AFFINITY` cannot represent an originally unconstrained all-group thread.

### Placement decision

The Windows 11 Ryzen 9950X3D exposes 16 physical cores, 32 logical CPUs and two last-level-cache groups. The OS-reported groups on this host were logical 0-15 and 16-31, with adjacent SMT pairs. Selection uses the discovered core/cache identities, not those numbering patterns. `packed` visits a cache's physical cores together; `spread` alternates caches before taking any SMT sibling. The CLI keeps one worker unpinned on the direct path and uses spread placement for multiple workers. Experimental placement/publication controls are not CLI flags.

Three alternating five-second production samples per placement, following a one-second screen, gave these median rare-prefix rates in million keys/s:

| Workers | OS placement | Pinned, packed caches | Pinned, spread caches |
| ---: | ---: | ---: | ---: |
| 2 | 42.99 | 46.36 | 46.57 |
| 8 | 117.64 | 189.99 | 193.01 |
| 16 | 230.56 | 374.12 | 377.57 |
| 32 | 418.34 | 417.47 | 417.54 |

Spread avoids the large scheduler-dependent losses seen at partial occupancy and performed slightly better than packing on this machine. It is not a universal cache/NUMA optimization: the initial screen's OS placement was faster at four/eight workers and there is little difference when every logical CPU is occupied. No hybrid-core or multi-socket performance claim follows from this result.

### Scaling and coordination results

Measurements used PACE Go 1.27.1, Windows/amd64, `GOAMD64=v1`, no PGO and `GOMAXPROCS` equal to workers. Each worker initialized and warmed for 200 ms before a one-time start gate. Three five-second samples per configuration alternated production/reference order; placement comparisons reversed placement order too. Initialization and warm-up are excluded; batch draining, affinity restoration and joining are included. Search-only runs use independent reproducible SHAKE streams and a concurrency-safe no-op save callback. The reference uses the same engine, pinning and cancellation polling with local counters, but no periodic publication, save serialization or progress coordinator. Progress-enabled runs use the real four-second timer and a no-op reporter; CLI status formatting has separate allocation tests and terminal I/O is not timed.

Rare-prefix results below are medians in million keys/s. "Per worker" is aggregate throughput divided by worker count, not a measurement of individual-worker skew. The one-worker row deliberately exercises the coordinated, pinned runner to compare like-for-like overhead; it is not the CLI's direct, unpinned one-worker default.

| Workers | Independent reference | Production | Production + progress | Production per worker |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 19.55 | 20.24 | 19.51 | 20.24 |
| 2 | 46.04 | 45.40 | 46.06 | 22.70 |
| 4 | 93.84 | 95.29 | 95.61 | 23.82 |
| 8 | 192.01 | 193.05 | 194.04 | 24.13 |
| 16 | 375.40 | 375.20 | 377.32 | 23.45 |
| 24 | 389.75 | 395.23 | 395.64 | 16.47 |
| 32 | 420.43 | 419.54 | 413.97 | 13.11 |

Overhead is `1 - production/reference`, using comparable medians. The largest positive rare-match overhead was 1.38% without progress and 1.54% with progress; negative values are run-to-run variation, not a claim that coordination accelerates the engine. The approximately 1-2% coordination goal was met in this matrix. Physical-core rates must not be extrapolated linearly across SMT siblings.

The same seven counts, reference and progress modes were measured for the other workloads. Production medians without reporting are below, also in million keys/s; divide by workers for the corresponding average per-worker rate. The ordinary/shared dictionaries contain 512 anywhere literals. All-hit search matches `.a` and `.q`, includes every successful callback and reseed and excludes disk I/O.

| Workers | Frequent `ab.` | Ordinary dictionary | Shared-triplet dictionary | Every candidate hits |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 18.179 | 14.104 | 14.992 | 0.119 |
| 2 | 39.065 | 30.289 | 31.373 | 0.246 |
| 4 | 79.966 | 62.135 | 63.364 | 0.498 |
| 8 | 159.179 | 123.506 | 123.562 | 0.968 |
| 16 | 304.626 | 235.914 | 237.038 | 1.853 |
| 24 | 320.869 | 254.323 | 255.684 | 1.948 |
| 32 | 337.065 | 273.999 | 275.065 | 2.015 |

Across these workloads the largest positive median overhead was about 1.1% without progress and 1.2% with progress. Steady-state batch/save-wrapper/publication tests and all direct full-search benchmarks report zero allocations. Whole timed rare-search runs typically recorded `workers + 1` allocations, with occasional runtime noise; these cover one-time timer/worker-transition/shutdown work and are not per-candidate allocations. Real persistence allocates separately.

Three alternating 30-second samples exposed sustained-run variation: at 16 workers the reference/production/progress medians were 373.16/374.36/376.98 million keys/s; at 32 they were 414.76/419.87/415.92. Samples varied by roughly 2% within a mode. Frequency and temperature counters were not collected, so timing variation cannot be attributed specifically to thermal behavior.

Simultaneously running pinned single-worker processes, synchronized to a common start and given the same CPU list, provide a second reference. Three alternating five-second samples gave production/process medians of 47.14/47.18, 198.29/198.56, 380.76/382.02 and 411.04/422.65 million keys/s at 2, 8, 16 and 32 workers respectively. The first three differ by less than 0.4%; at 32 workers production is about 2.75% below the process reference. That gap remains a limitation; the independent-loop coordination result is not a guarantee against every process/scheduler configuration.

### Persistence and single-worker acceptance

Real-persistence runs used OS entropy and `onion.Store.Save`, including validation, file flushes and directory rename, in temporary directories. They used progress, spread placement, three five-second cancellation deadlines and no hostname printing. Median successful saves/s were 410, 379 and 354 at 1, 4 and 16 workers, respectively, with roughly 28 allocations and 3.6 KiB allocated per saved key. Synchronous storage dominated. Total elapsed times were 5.18-6.24, 5.38-5.69 and 21.64-23.88 seconds: at 16 workers an all-hit in-flight batch drain required 8,192 saves. These numbers must not be compared with cheap-callback search rates as if they measured the same work.

The initial unchanged-engine screen used six one-second samples on logical CPU 2, including 39.57 ns/key for the rare-prefix median. Final single-worker comparisons used that preserved baseline binary, logical CPU 2 affinity, a warm-up of each binary, five alternating two-second samples and the ordinary test build without the opt-in multicore measurement harness. Medians below are ns/key, with lower values better:

| Workload | Starting binary | Final binary | Change |
| --- | ---: | ---: | ---: |
| Rare prefix | 39.86 | 39.72 | -0.35% |
| Frequent `ab.` | 48.39 | 48.43 | +0.08% |
| Every candidate hits | 7675 | 7696 | +0.27% |
| Ordinary 512 | 62.04 | 63.30 | +2.03% |
| Shared-triplet 512 | 61.72 | 63.03 | +2.12% |
| Mixed 512 | 65.03 | 66.07 | +1.60% |
| Short 512 | 480.2 | 482.5 | +0.48% |

The strict no-reproducible-single-worker-regression goal is **not fully met**: dictionary slowdowns repeated across comparisons. Hot engine source is unchanged and inspected worker/batch/reseed/base-multiply machine instructions were unchanged apart from relocation. Linking the larger experimental harness shifted which workloads were slower; keeping it behind `-tags measure` isolates it from ordinary tests, but does not eliminate the dictionary difference. Binary layout is a plausible contributor, not a proven cause. No arbitrary padding or arithmetic changes were introduced to tune a particular executable. All samples remained 0 B/op and 0 allocs/op.

### Reproducing the multicore pass

Create `measurements` if necessary. Before modifying a clean starting checkout, retain its ordinary test binary using `pace test -vet=off -c -pgo=off -o measurements/multicore-baseline.exe ./internal/search`. Build both versions with the same PACE toolchain and `GOAMD64=v1`; the starting SHA above identifies the baseline. Current-tree Windows commands are:

```powershell
$env:GOAMD64 = "v1"
pace test -vet=off -c -pgo=off -o measurements/multicore-single-final.exe ./internal/search
pace test -vet=off -c -pgo=off -tags measure -o measurements/multicore-candidate.exe ./internal/search
./scripts/measure-single.ps1 -Baseline measurements/multicore-baseline.exe -Candidate measurements/multicore-single-final.exe -Name multicore-single-warm -Time 2s -Count 5 -Affinity 4
./scripts/measure-multicore.ps1 -Name placement -Workloads rare -Workers 2,8,16,32 -Modes parallel -Seconds 5 -Repeats 3
./scripts/measure-multicore.ps1 -Name scaling -Placements spread -Seconds 5 -Repeats 3
./scripts/measure-multicore.ps1 -Name sustained -Workloads rare -Workers 16,32 -Placements spread -Seconds 30 -Repeats 3
./scripts/measure-multicore.ps1 -Name processes -Workers 2,8,16,32 -Seconds 5 -Repeats 3 -Processes
./scripts/measure-multicore.ps1 -Name persistence -Workloads persistence -Workers 1,4,16 -Placements spread -Modes progress -Seconds 5 -Repeats 3
```

`-Affinity 4` means logical CPU 2 on this one-group measurement host; choose an allowed CPU on other machines. Run benchmarks serially. `measure-multicore.ps1` defaults to workers 1,2,4,8,16,24,32 and reference/parallel/progress modes, skips unavailable counts and imposes a three-hour test timeout. The default search-only matrix is bounded but lengthy. Portable invocation uses `ONINO_MEASURE=rare ONINO_WORKERS=1,2,4,8,16,24,32 ONINO_PLACEMENTS=spread ONINO_SECONDS=5 ONINO_REPEATS=3 pace test -vet=off -tags measure ./internal/search -run '^TestMeasureMulticore$' -v -timeout 3h`. Process comparisons use `ONINO_PROCESSES=2,8,16,32` and `TestMeasureProcesses` instead.

The harness emits `MEASURE` JSON with exact checked/saved totals, elapsed duration, aggregate/average per-worker rates, allocation deltas and actual group/CPU/core/cache selections. The recorded local artifacts are `measurements/multicore-{rare,frequent,512,shared512,all_hits}.txt`, `multicore-placement-{screen,sustained}.txt`, `multicore-sustained.txt`, `multicore-processes.txt`, `multicore-persistence.txt` and `multicore-single-warm-{before,after}.txt`. Raw measurements and test binaries are ignored; the tables above retain the conclusions. Measurement controls live only in test builds/scripts and ordinary tests never launch a search experiment.

### Multicore validation and limits

Native and `purego` suites passed with PACE and stock Go on Windows; stock-Go native/purego suites and both race variants passed on Linux under WSL. Custom `vet --tests` passed for native, purego and measure builds, plus Linux amd64/arm64, Windows arm64 and Darwin targets. New focused tests cover strict CPU parsing and excessive counts, irregular core/cache/group identities, process-mask restrictions, physical-core-before-SMT selection, real pinned execution, affinity restoration/retirement, independent secure worker state, concurrent signed-key snapshots, callback serialization, blocked saves/reports, exact final counters, cancellation, initialization/reseed/save failures and zero-allocation coordination. The existing sibling-invalidation tests remain in place.

Windows race binaries built with `builder test go --no-pace --cgo --dyn --compat --no-min --no-gen -vet=off -race`, but ThreadSanitizer failed before tests with a memory-allocation error (Windows error 87); they did not pass. WSL race commands were `go test -vet=off -race ./...` and `go test -vet=off -race -tags purego ./...`. Actual multi-group Windows hardware, hybrid cores and NUMA systems were unavailable; group-aware structures and selection are covered, but multi-group OS behavior is not hardware-validated. The portable fallback cannot discover restrictions beyond those reported by `runtime.NumCPU`. The single-worker dictionary and 32-process comparison gaps above remain open performance limits.

## Match probability estimates

The CLI estimates the probability of matching any supplied pattern once at startup, independently of the search engine. It reuses the parser and bit constraints, expands valid literal positions and forms their union under a model of 256 uniform public-key bits plus four independent checksum bits. This includes the visible character-52 boundary, overlapping anchors, contained patterns and self-overlapping literals; summing individual probabilities would overcount these cases. Fully specified public keys have their checksum validated at compilation, leaving 256 constrained bits. A bounded decision diagram computes the union exactly when practical. Complex dictionaries fall back to deterministic weighted union sampling, drawing a condition proportional to its probability and weighting a satisfying input by inverse coverage. Conditioning on matches retains extremely rare probabilities that naive random-key sampling would miss. This estimation PRNG is unrelated to secure search entropy.

For per-candidate probability `p` and confidence `c`, the precomputed count is `ceil(log(1-c) / log1p(-p))`, with explicit all-hit/zero-probability cases. A seven-character prefix such as `example.` has model probability `32^-7`: about 23,816,355,775 candidates for 50% and 102,932,577,139 for 95%. `32^7` is the mean waiting count, not certainty. There is no finite 100% wait unless every candidate matches.

Progress divides these two counts by the overall `checked / elapsed` rate. It estimates the next wait from now, without subtracting past checks or saves. The IID model is approximate: actual curve encodings and successive search candidates are not independent uniform 256-bit inputs. Large-union sampling adds estimation error. Formatting uses a reusable buffer and floating-point seconds, including waits beyond `time.Duration`'s range. The updated PACE status benchmark measured 88-93 ns/report with 0 B/op and 0 allocs/op; probability analysis and logarithms are outside reporting and search loops.

Before the visible-checksum change, one-time probability analysis on the same host measured about 107 ns/64 allocated bytes for `example.`, 7.4 ms/8.1 MB for `example` anywhere and 21.1 ms/29.5 MB for the 512-word test dictionary. Temporary diagram/sampling allocations belong to startup, not steady-state search. Reproduce with `pace test -vet=off -run '^$' -bench 'Benchmark(SearchStatus|EstimateProbability)' -benchmem . ./internal/pattern`. Analytical overlap/checksum cases, exhaustive small unions, rare-event sampling, confidence thresholds and zero-allocation formatting are tested.

## Visible character-52 matching

The search window is the first 52 visible hostname characters, not independently padded public-key base32. Character 52 has one public-key bit and the high four bits of the first SHA3-256 checksum byte. A suffix `.aaa` therefore constrains eleven public-key bits and four checksum bits, giving model probability `2^-15`; the public-key-only filter admits about `2^-11` of fully signed candidates. All 32 final symbols remain searchable and checksum work always uses the real completed point sign.

Compilation separates ordinary occurrences from occurrences ending at character 52. Ordinary matches return immediately. Boundary candidates pass raw public-key constraints before a single shared checksum calculation, then select the matching checksum-nibble constraints. Identical public constraints merge their accepted nibbles; all sixteen accepted nibbles eliminate the hash entirely. Fully specified keys have their checksum checked once during compilation. The existing 64-byte raw constraints, 16-byte scan plans, assembly layouts and candidate generation remain intact.

Sets consisting only of one-, two- or three-character suffixes use a direct table indexed by the final public-key bits. Their tables contain 2, 64 or 2,048 two-byte masks (4, 128 or 4,096 bytes). A zero mask rejects before hashing; a complete mask accepts without hashing. This keeps simple suffix matching allocation-free and avoids walking alternatives. A single one-character suffix still needs SHA3 for about half the candidates and reseeds after about one in 32; those costs are inherent in visible matching and the existing saved-key independence contract.

The following bounded crossover screen used PACE Go 1.27.1 on the same Windows/9950X3D host, one worker without affinity, three 400 ms samples per case. Values are median [minimum, maximum] ns/key. Both engines include matching, callbacks and reseeding using deterministic benchmark entropy, without disk persistence. All samples reported **0 B/op and 0 allocs/op**.

| Suffix | Independent walk | Paired engine | Selected engine, million keys/s |
| --- | ---: | ---: | ---: |
| `.a` | 448.2 [444.4, 450.0] | 487.6 [487.0, 492.6] | Walk, 2.23 |
| `.aa` | 81.89 [81.49, 82.13] | 52.06 [51.99, 52.17] | Paired, 19.21 |
| `.aaa` | 70.35 [70.20, 70.61] | 38.79 [38.71, 38.81] | Paired, 25.78 |
| `.aaaa` | 70.10 [70.05, 70.10] | 38.25 [38.21, 38.27] | Paired, 26.14 |

Reproduce with `pace test -vet=off ./internal/search -run '^$' -bench '^BenchmarkEngineCrossover$/suffix' -benchmem -benchtime=400ms -count=3 -cpu=1`. The existing engine-selection hints choose the faster engine in each tested suffix case. Older suffix/crossover measurements elsewhere in this document used the padded-key boundary and have different hit rates.

A separate three-sample 300 ms before/after `BenchmarkFullSearch` check showed no material regression in the existing workloads: rare prefix 38.70→38.80 ns/key, frequent prefix 47.90→47.68, all-hit 7,559→7,543 ordinary dictionary 63.55→62.45, shared dictionary 62.79→62.21, mixed 65.83→65.20 and short 480.3→470.1. These unpinned bounded screens are regression checks, not a statistical claim of improvement. The updated all-hit fixture includes all 32 one-character suffixes; `.a` and `.q` alone no longer cover every candidate.

Validation includes the originally reported padding-only `.aaa` false positive, every two-byte public tail against independent SHA3/base32 hostname references, scalar/vector short-suffix alternatives, merged checksum coverage, full-key checksum validation, exact saved keys from both engines and zero matcher allocations. PACE and stock-Go native/purego suites passed, as did Linux native/purego race suites. Custom vet passed for native/purego Windows, Linux amd64/arm64 and Darwin.

### No-save cost breakdown

`BenchmarkSuffixCosts` separates generation/matching from per-hit handling using the production-selected engine. `matching_only` generates real candidates, completes the necessary signs, verifies the actual checksum and counts matches, without exporting keys or reseeding on hits. `reseed_shake` uses the production search batch, immutable snapshots, a cheap callback and reproducible SHAKE entropy. `reseed_random` uses the same production path with `crypto/rand.Reader` for hit-triggered reseeding. None performs hostname formatting, console output or filesystem writes. Initial worker construction remains outside the timer; the matching-only path is a diagnostic, not the saved-key search path.

On the same Windows/9950X3D host with PACE Go 1.27.1, one worker without affinity, three 750 ms samples per case gave the following median [minimum, maximum] ns/key. All 54 samples reported **0 B/op and 0 allocs/op**. The two single-character cases use the independent walk; the other cases use the paired engine.

| Pattern | Generation/matching only | With SHAKE reseeding | With OS-random reseeding |
| --- | ---: | ---: | ---: |
| `somethingrare.` | 38.24 [38.17, 38.49] | 38.27 [38.11, 38.31] | 38.23 [38.18, 38.25] |
| `a.` | 83.63 [79.38, 84.68] | 320.0 [315.4, 325.6] | 320.9 [316.0, 321.4] |
| `.a` | 222.3 [214.5, 226.2] | 476.2 [473.0, 483.9] | 463.3 [462.2, 464.8] |
| `.aa` | 44.89 [44.52, 45.88] | 53.82 [53.80, 54.12] | 53.64 [52.84, 54.63] |
| `.aaa` | 39.96 [39.19, 41.17] | 41.15 [39.97, 41.40] | 39.32 [39.27, 39.49] |
| `.aaaa` | 39.20 [38.67, 39.81] | 39.64 [39.22, 39.95] | 39.40 [39.04, 39.74] |

Without per-hit handling, `.a` reaches 4.50 million keys/s; retaining normal OS-random reseeding gives 2.16 million keys/s, still without any saving. The corresponding `.aa` rates are 22.28 and 18.64 million keys/s. Both longer suffixes remain around 25 million keys/s; small reversals between modes are within the variability of these unpinned samples. The `a.` control has the same expected 1/32 hit rate as `.a` but no checksum calculation, reaching 11.96 million keys/s before hit handling.

Independent three-sample 750 ms microbenchmarks measured a checksum at 254.0 [253.0, 255.4] ns, independent-walk SHAKE reseeding at 7,492 [7,448, 7,544] ns, OS-random reseeding at 7,480 [7,463, 7,481] ns and snapshot/callback at 8.605 [8.571, 8.608] ns. All were allocation-free. For `.a`, hashing half the candidates contributes roughly 127 ns per candidate and reseeding one in 32 contributes roughly 234 ns per candidate. These costs explain the large slowdown despite a tiny lookup table; random acquisition and the benchmark callback are not the dominant costs. A table indexed by the final public bits cannot supply checksum bits, which depend on the entire public key. Longer suffixes reject much more work before hashing: `.aa` admits about 1/64 of candidates, `.aaa` about 1/2,048 and `.aaaa` about 1/65,536. Actual storage and output can add further overhead, which these measurements intentionally exclude.

Reproduce with:

```sh
pace test -vet=off ./internal/search -run '^$' -bench '^BenchmarkSuffixCosts$' -benchmem -benchtime=750ms -count=3 -cpu=1
pace test -vet=off ./internal/onion -run '^$' -bench '^BenchmarkHitCosts$/checksum$' -benchmem -benchtime=750ms -count=3 -cpu=1
pace test -vet=off ./internal/search -run '^$' -bench '^BenchmarkHitHandling$' -benchmem -benchtime=750ms -count=3 -cpu=1
```

## Sequential arithmetic experiments

These four experiments started from clean commit `d4def3e` and used PACE Go 1.27.1 on Windows 11, Ryzen 9 9950X3D, `GOAMD64=v1`, `GOMAXPROCS=1`, `-pgo=off` and logical CPU 2 affinity (mask 4). Each candidate was compared with the last accepted implementation, serially, with five alternating one-second samples per variant and reversed order on alternating pairs. Tables give median [minimum, maximum] ns/key and median throughput change, calculated as `baseline/candidate-1`. All timed search and arithmetic samples reported **0 B/op and 0 allocs/op**. The workload and timing exclusions are described under the historical compiler comparison below.

### 1. Shared reciprocal product: retained

Computing `t=c*r` and reconstructing `r+t` and `r-t` removes one multiplication per pair in both portable Go and the fused reverse pass. The assembly reuses the expired denominator slot for `t`; both final reciprocals remain available for exact sign recovery. Independent big-integer tests cover random full-width couplings and noncanonical boundaries in complete 256-center batches.

| Full search | Original | Shared product | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 43.61 [43.57, 43.61] | 40.56 [40.51, 40.64] | +7.52% |
| Frequent `ab.` | 52.76 [52.74, 52.86] | 49.76 [49.72, 49.89] | +6.03% |
| Every candidate hits | 7651 [7645, 7657] | 7650 [7648, 7659] | +0.01% |
| 512 anywhere patterns | 72.99 [72.93, 73.07] | 69.84 [69.78, 70.18] | +4.51% |
| 512 shared-triplet patterns | 67.96 [67.88, 68.03] | 64.84 [64.80, 64.99] | +4.81% |

### 2. Dedicated assembly square: retained for preparation

The four-limb square accumulates six off-diagonal products once, doubles the entire cross-product sum including its top carry, then adds four diagonal products. It shares the existing full-width reduction with multiplication. All 256 input bits are supported, including noncanonical representatives and every input is consumed before stores, preserving in-place operation. The emitted square has 14 `MULX` instructions including reduction, versus multiplication's 20; both are zero-frame, call-free PACE ABIInternal leaves preserving R14 and X15. In the dependent arithmetic diagnostic, square measured 6.169 [6.158, 6.172] ns/op versus multiply's 6.997 [6.979, 7.001].

| Full search | Shared reciprocal product | Plus dedicated square | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 40.54 [40.54, 40.56] | 40.16 [40.10, 40.20] | +0.95% |
| Frequent `ab.` | 49.77 [49.74, 49.87] | 49.43 [49.41, 49.47] | +0.69% |
| Every candidate hits | 7650 [7648, 7661] | 7650 [7646, 7663] | 0.00% |
| 512 anywhere patterns | 69.85 [69.82, 69.95] | 69.47 [69.38, 69.58] | +0.55% |
| 512 shared-triplet patterns | 64.83 [64.80, 64.95] | 64.38 [64.34, 64.53] | +0.70% |

The square was also evaluated in a four-limb implementation of the established inversion chain, using 254 squares and 11 multiplies. Inversion measured 1708 [1686, 1708] versus 1703 [1693, 1716] ns/op, an inconclusive difference. Complete searches also failed to improve reproducibly, so that prototype was removed and this pass left the dependency's inversion unchanged. The later divsteps experiment below uses a different algorithm. Timing variation was wider in this screen:

| Full search | Accepted preparation square | Four-limb inversion | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 40.98 [40.13, 41.22] | 41.00 [40.25, 42.20] | -0.05% |
| Frequent `ab.` | 50.47 [49.41, 51.77] | 50.48 [49.41, 51.82] | -0.02% |
| Every candidate hits | 7792 [7646, 8004] | 7783 [7653, 8114] | +0.12% |
| 512 anywhere patterns | 71.11 [69.36, 72.74] | 71.13 [69.49, 72.60] | -0.03% |
| 512 shared-triplet patterns | 65.77 [64.37, 67.86] | 66.11 [64.46, 71.31] | -0.51% |

### 3. Multiplication scheduling and reduction: rejected

Rare-search CPU profiles put the original preparation, reverse reciprocal pass and standalone multiplication at 36.49%, 28.92% and 14.83% of sampled CPU time. After experiments 1-2 they accounted for 39.16%, 21.47% and 15.18%; dependency squaring was another 6.91%. These are shares, not per-kernel speedups. Emitted assembler listings confirmed the expected dual carry chains, register-only products and absence of calls, stack operands or AVX in the integer leaves. Windows exposed performance-counter sources, but capture failed for lack of system-profiling permission; no hardware-counter bottleneck claim is made.

Two reference families informed bounded changes:

- **s2n-bignum**, commit `4d1356a7470663c752660a59375dc3a9ef548428`, `x86/curve25519/bignum_mul_p25519.S`: four full-width limbs, dual carry chains and quotient-estimated canonical reduction. Its representation is applicable, but canonicalizing every product adds work that onino normally defers until serialization.
- **CryptOpt**, commit `c089d8ce3cace748a0a22e25d7adfbf2cfc6a883`, `generated/fiat-amd64`: the inspected `fiat_curve25519_solinas_mul/seed0000000356490115_ratio18494.asm` had 21 `MULX` instructions and a 144-byte stack frame; the radix-51 `fiat_curve25519_carry_mul/seed0000000879783339_ratio12750.asm` had 25 `MULX` instructions. Their scheduling illustrates the register-pressure tradeoff, but stack traffic, reserved-register use and differing bounds prevent a direct substitution into onino's zero-frame ABI. No automated search was run.

First, replacing three row-end `MOV $0`/`ADCX` flushes with `ADC $0` shortened the instruction sequence but did not produce a consistent search win. Dependent multiplication worsened from 7.001 [6.985, 7.005] to 7.056 [7.036, 7.058] ns/op. The change was removed.

| Full search | Accepted square | Shorter carry flush | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 41.18 [40.98, 44.68] | 41.02 [40.99, 41.82] | +0.39% |
| Frequent `ab.` | 50.51 [50.32, 51.06] | 50.65 [50.36, 51.75] | -0.28% |
| Every candidate hits | 7798 [7781, 7879] | 7869 [7863, 8062] | -0.90% |
| 512 anywhere patterns | 71.23 [70.85, 72.02] | 71.23 [70.84, 72.66] | 0.00% |
| 512 shared-triplet patterns | 65.97 [65.61, 66.78] | 65.95 [65.88, 66.98] | +0.03% |

Second, an s2n-style quotient-estimated reduction was adapted to the existing full-width product registers and onino's aliasing/ABI requirements. It passed independent arithmetic and paired-key checks but slowed multiplication from 7.002 [6.983, 7.005] to 7.745 [7.726, 7.767] ns/op and regressed all paired workloads. It was removed; the original multiplication schedule and reduction remain. The earlier unsuccessful fused inverse/encoding experiment was not repeated.

| Full search | Accepted square | Quotient reduction | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 40.82 [40.74, 40.99] | 44.65 [44.48, 44.98] | -8.58% |
| Frequent `ab.` | 50.18 [50.02, 50.31] | 53.99 [53.88, 68.06] | -7.06% |
| Every candidate hits | 7772 [7734, 7880] | 7780 [7741, 7803] | -0.10% |
| 512 anywhere patterns | 70.60 [70.33, 71.49] | 74.47 [74.27, 74.69] | -5.20% |
| 512 shared-triplet patterns | 65.40 [65.22, 65.64] | 69.38 [69.06, 69.64] | -5.74% |

### 4. Four-way AVX2/FMA: rejected at the arithmetic screen

The ETH report [*Fast Implementations of Curve25519 on Intel Skylake*](https://famoser.ch/papers/Fast%20Implementation%20of%20Curve25519%20on%20Intel%20Skylake.pdf), by Goetschmann, Moser, Streun and Tobler, uses exact floating-point arithmetic with twelve small limbs. The prototype used weights `ceil(21.25*i)`, four independent fields in each AVX2 vector, fused multiply-add accumulation and wrap factor `19*2^-255`. Square reused symmetric products. Eighteen rounded carry transfers in six parallel three-link chains bounded the signed output limbs for reuse. Dispatch required the separate FMA CPUID bit as well as AVX2, OSXSAVE and XGETBV support.

Independent integer bounds checked that all products and partial sums fit double's 53-bit significand. Tests compared multiplication, squaring and canonical serialization with `math/big`, covering full-width/noncanonical inputs, positive and negative limb bounds, 20,000 random lane pairs, repeated signed outputs and supported input/output aliases. PACE and stock Go passed before measurement.

| Four-field operation | BMI2/ADX median [range], ns/op | AVX2/FMA median [range], ns/op | Throughput |
| --- | ---: | ---: | ---: |
| Multiply | 19.40 [19.39, 19.44] | 22.88 [22.82, 22.94] | -15.21% |
| Square | 15.11 [15.10, 15.16] | 17.29 [17.28, 17.35] | -12.61% |

These five alternating one-second samples compare four independent integer chains with four packed lanes and include FMA carry normalization. Even already-packed arithmetic lost. Separately, packing four integer fields took 110.2 [110.0, 110.5] ns/op and canonical serialization took 132.6 [132.5, 133.1], each over five one-second samples. Those conversion routines were straightforward portable prototypes, not an optimized lower bound. Packed state also grew from 128 to 384 bytes per four fields. Scalar prefix products/inversion and canonical key output would add further integration costs. The arithmetic screen therefore did not justify a paired-generation backend; all FMA prototype source was removed. The report's Montgomery-ladder results on older Intel CPUs do not establish a win for this search on Ryzen.

### Retained result against the original baseline

The final comparison used ten alternating one-second samples per variant, combining two five-pair runs, with all samples retained. Dictionary timings varied more than in the individual acceptance screens and their ranges overlap. Reciprocal simplification and preparation squaring are the only retained arithmetic changes from this pass; the all-hit projective fallback is unaffected.

| Full search | Original median [range], ns/key | Final median [range], ns/key | Throughput |
| --- | ---: | ---: | ---: |
| Rare prefix | 43.62 [43.59, 44.12] | 40.125 [40.11, 40.15] | +8.71% |
| Frequent `ab.` | 52.785 [52.76, 52.89] | 49.40 [49.29, 49.45] | +6.85% |
| Every candidate hits | 7732 [7669, 7805] | 7732.5 [7672, 7974] | -0.01% |
| 512 anywhere patterns | 74.29 [73.07, 74.98] | 70.575 [69.29, 74.43] | +5.26% |
| 512 shared-triplet patterns | 69.14 [67.94, 69.33] | 65.385 [64.43, 69.38] | +5.74% |

All 100 samples were allocation-free. This original-to-final experiment is separate from the same-source compiler comparison below; their timings should not be mixed to calculate gains.

## Final bounded pass: fingerprints and divsteps

This pass started from clean **`d01aa812524e6fe3b4d4a36246524075e047025e`**, including the shared reciprocal and dedicated square retained in `336671a`. It used PACE Go 1.27.1, Windows 11, Ryzen 9 9950X3D, `GOAMD64=v1`, `GOMAXPROCS=1`, no PGO and logical CPU 2 affinity (mask 4). Baseline and candidate binaries included the same added mixed/short benchmark fixtures. Experiments ran sequentially against the last accepted version, with at least five alternating one-second samples per variant and order reversed on alternating pairs. Ten samples confirmed divsteps, the scratch decision and the final baseline comparison. A batch-filter run affected by an unrelated heavy tool was discarded and repeated after the tool stopped; all samples from the replacement and final runs were retained.

### 1. Dictionary filtering

Separate five-second CPU profiles distinguished rare generation from ordinary/shared 512-pattern dictionaries. Before changes, preparation/reverse arithmetic/standalone multiplication occupied 36.7%/26.2%/15.4% of rare-search samples; matching was about 2%. Ordinary/shared dictionary matching occupied 40.9%/38.4% of complete searches. Window scanning plus triplet probing accounted for 29.8%/32.8%, versus 7.3%/2.2% for exact verification and symbol decoding. Membership tests, rank/bucket lookup and verifier-routing control collectively dominated, especially with shared triplets. Profiles and emitted assembly identify these paths, not branch-miss or cache-miss rates: Windows listed hardware-counter sources, but capture failed with profiling-policy error `0xc5585011`.

Two independent screens followed [Wang et al., *Hyperscan*, NSDI 2019](https://www.usenix.org/conference/nsdi19/presentation/wang-xiang): use cheap literal filters before exact verification and expose independent work where worthwhile. The paper is an architectural reference, not a speedup prediction or an integration proposal.

- **Four-candidate scalar membership batching: removed.** Thirteen stride-four probes per key produced survivor masks before bucket traversal, preserving candidate order, sign alternatives, pending-sibling invalidation and checked accounting. Ordinary/shared medians improved only 69.55→69.36 and 64.55→64.14 ns/key; frequent prefixes regressed 49.47→49.86. Mixed/short/anchored results did not justify the extra filtering pass and 512-byte decision array. This bounded screen stopped at scalar instruction parallelism; no new AVX2 backend was warranted.
- **Adjacent-symbol fingerprints: retained.** Each triplet bucket stores two 32-bit membership masks, with one five-bit neighbor contributed per verifier. A candidate is rejected only when neither side's union admits it; every survivor still undergoes exact packed-key verification. Following symbols are preferred, with the preceding symbol used at literal ends. Stride-four neighbors are always at position 3 modulo 4, never deferred sign position 49; final-position padding and suffix offsets remain exact. Against the original implementation, the five-sample screen improved ordinary/shared dictionaries 69.52→63.02 and 64.46→62.66 ns/key, mixed anchors 72.44→66.19 and short fallbacks 488.0→478.8, without a reproducible rare/frequent/all-hit regression.

Buckets grow from 16 to 24 bytes, adding exactly 8 bytes per occupied triplet: 16,368 bytes for the ordinary fixture and 16,208 for the shared fixture. Exact/signless matchers share these immutable tables; worker scratch does not grow. Final ordinary/shared profiles put total dictionary matching at 34.8%/34.6%, with exact verification below 0.4% of samples. Emitted code uses packed-byte extraction and scalar mask tests before verifier traversal; existing independent BMI2/ADX and AVX2 detection is preserved.

### 2. Five-field paired scratch: removed

The portable and assembly prototype reduced seven fields to five, **224→160 bytes per center and 56→40 KiB across 256 centers**. During preparation, the eventual plus-reciprocal slot held `1-c²` and the minus slot held its prefix product. Walking backward consumed the previous prefix and current denominator before replacing either, then reused expired `c` for `c*r`. Final `r+t` and `r-t` survived Y encoding, deferred X/sign recovery and transitions. This preserved the 9M+1S count and 512-candidate public batch and passed independent native/portable arithmetic and complete-key checks.

Five-sample screening and ten-sample confirmation against fingerprints showed only about 0.2% faster paired searches. Confirmation medians were 40.17→40.09 ns/key for rare prefixes, 63.03→62.94 for ordinary dictionaries and 62.63→62.54 for shared dictionaries. All-hit and short fallbacks regressed 7663→7696 and 478.5→480.9, despite not using the modified scratch path. Binary layout is a possible explanation, not a measured cause. The complete-search acceptance rule rejected the prototype; the final state retains seven fields and about 114 KiB per worker. Improved cache behavior was not established.

### 3. Constant-time divsteps: retained

The inversion in `divsteps.go` follows [Bernstein-Yang, *Fast constant-time gcd computation and modular inversion*](https://gcd.cr.yp.to/safegcd-20190413.pdf) and the established [libsecp256k1 constant-time implementation](https://github.com/bitcoin-core/secp256k1/blob/master/src/modinv64_impl.h), with its [half-delta bound and implementation notes](https://github.com/bitcoin-core/secp256k1/blob/master/doc/safegcd_implementation.md). Both requested peer-reviewed papers were treated as high-credibility references (9/10), not evidence of workload speedups. The port preserves the upstream MIT notice and adds no dependency.

Canonical four-limb inputs convert to five signed radix-62 limbs. Ten groups of 59 half-delta divsteps use matrices scaled for exact division by `2^62`; the established 590-step bound covers 256-bit inputs. Coefficients stay in `(-2p,p)` and two masked corrections normalize the output. Zero maps to zero, arbitrary 256-bit representatives reduce modulo `p`, output is canonical and in-place inversion is supported. Fixed loop counts, sign masks and fixed-index accesses replace input-dependent GCD control flow. PACE hot-loop assembly was inspected for secret-dependent branches/addresses and division instructions; only fixed-loop and runtime stack checks remained. This is algorithmically different from the rejected exponentiation-chain rewrite.

Including both representation conversions, the initial five-sample screen reduced inversion from 1705 to 1215 ns/op. Ten-sample confirmation against the retained fingerprint version measured **1704 [1702,1711]→1215 [1214,1219] ns/op**, with rare/frequent/ordinary/shared searches improving 40.25→39.28, 49.41→48.55, 63.06→62.10 and 62.66→61.70 ns/key. Anchored and long-literal searches also improved. Small fallback regressions in the initial binary disappeared in confirmation; allocation counts stayed zero. These complete-search gains justified retaining the bounded implementation. Inversion adds stack-local working state and a 40-byte modulus constant, with no per-worker persistent storage.

### Final comparison with the original checkout

The following ten-sample medians and full ranges include all retained changes. Throughput is calculated from unrounded medians; displayed times are rounded. The [performance history CSV](.github/performance-history.csv) preserves all 360 search/arithmetic measurements under `run=final_comparison`, each reporting **0 B/op and 0 allocs/op**. Mixed workloads combine the ordinary 512-pattern set with prefix, suffix and combined anchors; short workloads add short anywhere/interior/prefix alternatives and exercise the projective fallback. All-hit ranges overlap and its small median difference is not an optimization claim.

| Complete search | Baseline median [range], ns/key | Final median [range], ns/key | Final Mkeys/s | Throughput |
| --- | ---: | ---: | ---: | ---: |
| Rare prefix | 40.245 [40.15,40.80] | 39.270 [39.26,39.74] | 25.465 | +2.48% |
| Frequent `ab.` | 49.480 [49.31,50.09] | 48.550 [48.39,49.01] | 20.597 | +1.92% |
| Every candidate hits | 7672 [7667,7773] | 7652 [7646,7735] | 0.131 | +0.26% |
| 512 anywhere patterns | 69.605 [69.45,70.22] | 62.095 [62.03,62.89] | 16.104 | +12.09% |
| 512 shared-triplet patterns | 64.515 [64.38,65.25] | 61.690 [61.65,62.45] | 16.210 | +4.58% |
| 512 patterns plus mixed anchors | 72.455 [72.27,73.50] | 65.060 [64.98,65.67] | 15.370 | +11.37% |
| 512 patterns plus short fallbacks | 487.900 [487.70,495.50] | 478.100 [478.00,484.80] | 2.092 | +2.05% |
| 64 prefixes | 42.390 [42.36,42.93] | 41.405 [41.32,41.47] | 24.152 | +2.38% |
| 512 prefixes | 42.550 [42.52,43.24] | 41.585 [41.49,41.63] | 24.047 | +2.32% |
| 64 suffixes | 42.400 [42.39,42.91] | 41.410 [41.33,41.69] | 24.149 | +2.39% |
| 512 suffixes | 42.565 [42.52,43.15] | 41.560 [41.48,42.53] | 24.062 | +2.42% |
| 64 combined anchors | 42.405 [42.39,43.06] | 41.365 [41.33,41.84] | 24.175 | +2.51% |
| 512 combined anchors | 42.560 [42.53,43.16] | 41.510 [41.49,42.35] | 24.091 | +2.53% |
| 64 long literals | 55.260 [55.23,56.16] | 54.110 [54.08,55.11] | 18.481 | +2.13% |
| 512 long literals | 63.705 [63.61,64.33] | 61.280 [61.19,62.29] | 16.319 | +3.96% |

In the same final comparison, inversion measured 1685 [1685,1693]→1215.5 [1214,1217] ns/op, including conversion. Its amortized reduction is about 0.92 ns per candidate in a 512-key batch, consistent with complete-search improvements. Multiply/square remained about 7.00/6.18 ns/op. Timing and profile shares do not establish hardware-counter causes. Neither rejected prototype remains in production source.

### Cumulative performance history

The [history graph](.github/performance-history.svg) retains steps 00-15 and their original 480 samples, then adds **16: adjacent-symbol fingerprints** and **17: constant-time divsteps inversion**. The new steps reuse the original six-workload history harness, including 64 anywhere patterns and the periodic `progressReporter.update` check. Literals are the first ten lowercase base32 symbols of SHA-256 of a little-endian 64-bit index; prefix dictionaries append `.`. Worker initialization, deterministic SHAKE entropy, save callback, checked-key accounting and timer boundaries match the original history run.

Steps 16-17 were measured in a later session with the same PACE 1.27.1, Windows 11, Ryzen 9 9950X3D, logical CPU 2 affinity, `GOAMD64=v1`, `GOMAXPROCS=1` and no PGO. Both builds passed their search-package tests. Ten serial one-second samples per workload per build alternated order, reversing it on each pair; all 120 samples were retained and reported zero allocations. Step 16 restores the baseline dependency inversion while retaining fingerprints; step 17 includes both accepted changes. The history harness and measurement session differ from the final-pass comparison above, so their samples are not pooled.

| Step | Rare prefix | Frequent `ab.` | All hits | 64 anywhere | 512 anywhere | 512 prefixes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 16 | 40.18 | 49.36 | 7663 | 55.045 | 63.08 | 42.605 |
| 17 | 39.21 | 48.52 | 7649 | 54.135 | 62.25 | 41.97 |

Values are median ns per checked key; the graph shows full ranges. The unified CSV has **960 samples**: `run=history` selects the 600 plotted-history measurements, including the unplotted all-hit workload, while `run=final_comparison` selects the 360 baseline/retained measurements above. In the latter, stage 15 identifies `d01aa81` and stage 17 the final implementation. The `unit` column distinguishes search ns/key from arithmetic ns/op; blank sample columns mean unmeasured, never zero.

## Frequent-hit crossover

Pairing saves work on misses but makes independent reseeding more expensive. A projective fixed-`8B` walk therefore remains useful for frequent hits: it initializes one independent seed per candidate and avoids affine normalization during reseeding.

The compiler supplies a static preference for one-symbol alternatives and two-symbol unanchored/interior literals. Search chooses the projective engine once at startup for these matchers. The two-symbol cases retain deferred sign filtering; engine choice does not alter `SignFilter` semantics. Other matchers use pairing.

| Workload | Projective walk, ns/key | Paired, ns/key | Evidence |
| --- | ---: | ---: | --- |
| One-symbol prefix | about 321 | about 364 | Bounded crossover screen |
| Two-symbol prefix `ab.` | about 80 | about 52 | Bounded crossover screen |
| Two-symbol suffix `.aa` | about 192 | about 191 | Approximately tied in screen |
| Two-symbol anywhere `bc` | 444.35 | 498.20 | Ten one-second samples per engine |
| Two-symbol interior `.bc.` | 441.85 | 497.10 | Ten one-second samples per engine |
| Three-symbol anywhere `abc` | about 92 | about 66 | Bounded crossover screen |

The final two-symbol anywhere ranges were 444.00-445.30 versus 497.70-498.80 ns/key; interior ranges were 441.10-443.80 versus 495.90-500.80. The selector does not estimate the union probability of arbitrary pattern lists. A large collection of individually selective alternatives can still be frequent enough to favor the projective engine; adaptive switching was not introduced.

## Dictionary crossover

The strided dictionary scans every fourth 15-bit window. Each eligible unanchored literal contributes an anchor for each offset residue and shares one exact verifier across those anchors. Eligibility requires at least six symbols and nine possible starts. Short literals keep their established scalar/vector paths and anchored alternatives retain complete position and overlap checks.

Thresholds 16, 32, 64 and 128 were screened with ordinary/shared literals, short fallbacks and mixed anchors, including startup allocations. Threshold 16 made 16-pattern ordinary/shared searches slower, approximately 57 to 59 ns/key. Threshold 64 missed the improvement already available at 32 patterns. **32 eligible unanchored patterns** was selected and confirmed against 128 with five alternating one-second samples:

| Pattern set | Threshold 128, ns/key | Threshold 32, ns/key |
| --- | ---: | ---: |
| Ordinary 32 | 63.14 | 59.17 |
| Ordinary 64 | 75.38 | 60.01 |
| Shared triplets 32 | 62.76 | 58.86 |
| Shared triplets 64 | 74.62 | 59.47 |
| Short fallbacks 32 | 525.40 | 526.50 |
| Short fallbacks 64 | 539.40 | 527.70 |
| Mixed anchors 32 | 63.68 | 62.17 |
| Mixed anchors 64 | 76.31 | 63.01 |

This experiment held the paired engine fixed to isolate the dictionary threshold; the later short-pattern engine selection is separate. Ordinary/shared/mixed improvements were reproducible. Short-fallback differences were inconclusive and reseed-dominated. Boundary tests at 31/32/33 and 63/64 patterns compare full and signless matching with independent base32/string references.

At threshold 128, the vector representation expands checks for many starts in smaller sets. Selecting the dictionary at 32 also reduces startup work at those sizes:

| Compilation | Threshold 128 | Threshold 32 | Allocated bytes, threshold 128 → 32 |
| --- | ---: | ---: | ---: |
| Ordinary 32 | about 292 µs | about 30 µs | 228.5 → 47.3 KiB |
| Ordinary 64 | about 586 µs | about 60 µs | 455.0 → 86.8 KiB |
| Shared 32 | about 293 µs | about 27 µs | 228.5 → 38.0 KiB |

These startup figures are bounded screens, not the five-sample search distributions. Dictionary storage is bounded by about 5 KiB fixed, 72 bytes per parsed pattern and 16 bytes per anchor, plus strings. The original 16-byte occupied bucket grows to 24 bytes with the final pass's fingerprints. Exact/signless matchers share that storage. Anchored-only indexing retains its separate threshold of 64 eligible patterns.

## Differential addition on twisted Edwards curves

### Source and applicability

The full accessible source evaluated was Hosseini and Farashahi's [*Differential Addition on Twisted Edwards Curves*, June 2026 manuscript](https://arxiv.org/html/2606.20831v1). Proposition/equation numbers below refer to that version; the paywalled 2017 proceedings edition was not independently checked. Its advertised operation counts usually combine **differential addition and doubling in a ladder**, not generation of a canonical search key.

For Edwards25519, independent quadratic-character and square-root checks give:

| Quantity | Quadratic character |
| --- | ---: |
| `a = -1` | +1 |
| `d`, `ad`, `d/a`, `a/d` | -1 |
| `(a-d)/a` | +1 |
| `(d-a)/d` | -1 |
| `A²-4`, with Montgomery `A = 486662` | -1 |

Consequently:

- **Propositions 1-4:** the invariant `w = d*x²*y²` and its differential identities apply; the complete-sum conditions hold. The root in equation 14 exists, but optimizes the ladder's doubling portion. The invariant identifies torsion cosets and does not directly encode the candidate's Y.
- **Propositions 5-6:** `w = a*x²/y²` has analogous identities, with a pole at order-four points where `y = 0`. Proposition 6's complete-sum conditions do not hold for Edwards25519. The corresponding equation-14 root is unavailable.
- **Propositions 7-8:** the required `sqrt(ad)` is absent.
- **Proposition 9, equation 22:** direct-Y differential addition applies and admits the fixed-step specialization below. Equation 26 is the Montgomery coordinate-change route to direct Y, not a recovery-free improvement over this walk.
- **Propositions 10-11 and 13-14:** scaled-Y variants require roots of `d/a` or different completeness assumptions. Those roots do not exist here. In particular, a fourth root cannot exist when even a square root is absent.
- **Proposition 12:** squaring the Y relation loses the sign of Y itself. Canonical public-key production would need an extra root/branch recovery, not merely the compressed X sign.
- **Propositions 15-16:** the Montgomery variants require `sqrt(A²-4)`, also absent.

### Direct Y is the previous recurrence, factored

Let `q = y(8B)`, `v = y(P-8B)` and `y = y(P)`. Proposition 9 uses `t = d/(a-d)`, which becomes exactly **121665**. Precompute:

```text
alpha = 121665*(q²-1)
beta  = 1-alpha
gamma = 121666*(q²-1)
u     = y²

y(P+8B) = (u-alpha*u+gamma) / (v*(alpha*u+beta))
```

This is algebraically the previous affine-Y recurrence. Using `beta = 1-alpha` removes one coefficient multiplication; it does not create a different map. Independent big-integer tests compare the specialization with Edwards additions.

The complete prototype maintains consecutive Y coordinates, batch-inverts the denominators and serializes canonical Y. That costs **6M+1S+I/512 per generated candidate**, before matching. A sign-filter survivor recovers X using:

```text
x = (q*y-v) / (xQ*(1+d*q*y*v))
```

Recovery adds **5M+I per survivor**, followed by exact matching. Initialization/reseeding computes an independent point and its predecessor, normalizing both with one shared inversion. A zero recurrence denominator is isolated from the batch product and that lane's two coordinates are rebuilt from its scalar; the exceptional lane, unaffected neighbors and following round were validated. The sign-recovery denominator is nonzero for valid points with this nonidentity fixed step.

Complete-key tests covered independent scalar multiplication, reseeds, low/high scalar boundaries and forced exceptional state under PACE and stock Go, native and portable. The bounded full-search screen included statistics, key snapshots, independent reseeding and every sign completion:

| Full search | Factored direct Y, ns/key | Paired production, ns/key |
| --- | ---: | ---: |
| Rare prefix | 64.48-64.82 | 43.34-43.41 |
| Frequent `ab.` | 75.10-75.48 | 52.43-52.46 |
| 512 literals | 94.35-94.58 | 73.32-73.52 |
| All hits | 11234-11253 | 7688-7702 |

Both engines were measured in the same PACE test binary and had zero search allocations. Direct Y lost clearly to pairing; hit-only inversion and reseeding worsened frequent-hit behavior. The experiment stopped before another assembly backend or production engine was added.

### Why the fourfold invariant was not pursued

Equation 18 recovers coordinates of **4P**, not P. A search could maintain quarter-scalar points, step by `2B`, then recover each `8B`-spaced candidate, but recovery belongs in the per-candidate cost.

For `w = d*x²*y²`, `e = 4a/d`, set `h = w²`, `H = h²+6h+1` and `R = w*(h+1)`. Equation 18's Y recovery simplifies to:

```text
y(4P) = (2*(e-2)*R-H) / (4*R+(16-4*e)*h-H)
```

This expression was independently checked against two Edwards doublings. Even with that polynomial factoring, a straightforward affine invariant walk costs 6M+2S plus its batch inversion; constructing and normalizing fourfold Y adds 7M+2S and another batch inversion. Constants such as `e` are full field multiplications in these counts. Thus the ordinate alone costs **13M+4S+2I/512**, before exact-sign recovery, matching or reseeding. This is a rejection bound for that construction, not a claim that every possible projective schedule is optimal. Projective state can trade away an inversion but adds homogeneous recovery work. The ladder's lower addition-plus-doubling count is not evidence of a faster search engine.

## Earlier approaches and retained components

| Approach | Outcome |
| --- | --- |
| Mixed projective addition and batch normalization | Retained for frequent patterns; fused four-limb arithmetic and expired-product scratch reduce call/copy costs. |
| Deferred compressed-point sign | Retained in both engines when useful; exact matching always follows sign completion. |
| Fixed-position anchored indexes | Retained; compact membership/rank tables avoid scanning every anchored pattern. |
| Stride-four dictionaries | Retained; shared exact verifiers compensate for storing four residue anchors per eligible literal. |
| Targeted PACE inlining | Retained for hot dictionary window/verification paths; whole-matcher inlining and mandatory PGO were unnecessary. |
| PGO-only optimization | A profile-dependent `field.Element.Bytes` inlining decision caused a 32-byte batch allocation. Diagnosis isolated that escape; ordinary builds remain allocation-free without PGO. |
| Earlier affine-Y recurrence | Rare misses improved, but per-hit recovery/reseeding lost. The paper specialization above was the lower-cost follow-up and still lost to pairing. |
| Dedicated Go four-limb square | Fewer products did not compensate for carry propagation; the measured implementation was slower than multiplication. The later assembly square above wins in denominator preparation. |
| Shared medium-set AVX2 register filter | Extraction and survivor-verification costs outweighed complete-search benefits. |
| Four independent prefix chains | Extra inversion/normalization work lost in complete searches. |
| Four-lane radix-29 AVX2 | Independent arithmetic tests passed, but packed multiplication was roughly four times slower than four BMI2 products; no point backend was added. |
| Larger projective batches | Small throughput gains did not justify increased state and cancellation work; the public batch remains 512 checked candidates. |

## Compiler comparison before the final pass

Stock Go and PACE Go 1.27.1 were built from the same pre-final-pass source on Windows 11/amd64, Ryzen 9 9950X3D, with `GOAMD64=v1`, `GOMAXPROCS=1`, no PGO and logical CPU 2 affinity. Five one-second samples per compiler were run serially, alternating compiler order. Both builds use the same production engine selection and native arithmetic/matching backends. These historical compiler timings precede fingerprints and divsteps; current PACE timings are above, while stock Go remains a compatibility target.

| Full search | Go median [range], ns/key | PACE median [range], ns/key |
| --- | ---: | ---: |
| Rare prefix | 42.99 [42.86, 43.03] | 40.88 [40.69, 42.15] |
| Frequent `ab.` | 52.35 [52.15, 54.11] | 50.54 [50.15, 51.66] |
| Every candidate hits | 7900 [7867, 7943] | 7800 [7762, 8051] |
| 512 anywhere patterns | 88.57 [88.14, 88.92] | 70.66 [70.33, 73.37] |
| 512 shared-triplet patterns | 83.23 [83.14, 83.50] | 66.32 [65.45, 67.95] |
| 64 prefixes | 44.62 [44.56, 44.69] | 42.65 [42.34, 43.24] |
| 512 prefixes | 45.16 [45.07, 45.31] | 43.14 [42.55, 44.27] |
| 64 suffixes | 45.04 [44.95, 49.73] | 42.97 [42.36, 44.19] |
| 512 suffixes | 45.21 [45.13, 46.67] | 43.30 [43.11, 44.46] |
| 64 combined anchors | 44.90 [44.85, 44.95] | 43.15 [42.95, 44.13] |
| 512 combined anchors | 45.27 [44.99, 45.41] | 43.27 [43.15, 44.40] |
| 64 long literals | 73.57 [73.21, 86.88] | 56.18 [56.02, 57.98] |
| 512 long literals | 82.30 [81.95, 82.53] | 64.92 [64.50, 66.37] |

All 130 full-search samples report 0 B/op and 0 allocs/op. PACE's median throughput is about 5.2% higher for rare-prefix search and 25-26% higher for the 512-pattern anywhere/shared dictionaries. All-hit medians differ by about 1.3%, with overlapping ranges. Five samples are a limited distribution estimate.

Generation-only runs give 40.32 [40.28, 40.33] ns/key with Go and 38.73 [38.72, 38.76] with PACE for paired Y; complete paired public keys take 65.60 [65.57, 65.63] and 62.52 [62.51, 62.53] respectively. All 20 generation samples also have zero allocations. They include table transitions but exclude matching and per-hit reseeding. `BenchmarkWorkloads`, `BenchmarkGeneration` and `BenchmarkSearchSizes` remain projective-engine diagnostics; `BenchmarkFullSearch` and `BenchmarkDictionarySearch` exercise production engine selection. `BenchmarkField` isolates dependent multiplication, squaring and inversion chains.

Full-search benchmarks replace OS entropy with deterministic SHAKE and saving with a cheap synchronous callback. Startup, context polling and filesystem validation/persistence are not timed. These are elapsed timings on a pinned worker, without hardware-counter or direct CPU-time measurements. They describe one AMD machine and a specific compiler pair. CPU scheduling, frequency, binary layout and pattern distributions remain relevant.

### Reproduction

Set `GOMAXPROCS=1` and `GOAMD64=v1`, use the same CPU affinity and run benchmarks serially. Build the same source with `go test -vet=off -c -pgo=off -o search-go.test.exe ./internal/search` and `pace test -vet=off -c -pgo=off -o search-pace.test.exe ./internal/search`. Alternate their binaries with `-test.run=^$ -test.bench=^BenchmarkFullSearch$ -test.benchmem -test.benchtime=1s -test.count=1 -test.cpu=1`, reversing compiler order on alternating samples. For current-tree checks, run these commands with either `go` or `pace`:

```sh
pace test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(FullSearch|DictionarySearch)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test -vet=off -pgo=off ./internal/search -run '^$' -bench '^Benchmark(PairedGeneration|EngineCrossover|Crossover)$' -benchmem -benchtime=1s -count=5 -cpu=1
pace test -vet=off -pgo=off ./internal/pattern -run '^$' -bench '^BenchmarkCompileCrossover$' -benchmem -benchtime=250ms -count=2 -cpu=1
```

## Verification

- Full `pace test -vet=off` and `go test -vet=off` suites passed, both native and `-tags purego`.
- Dedicated square and in-place square are checked against `math/big` across the existing boundary matrix and 20,000 random full-width pairs. Batched reciprocal tests independently check both reconstructed inverses for 2,048 couplings. These cover noncanonical inputs as well as production point-derived values; multiplication's existing aliasing tests remain intact.
- Divsteps inversion uses the same independent boundary/random references, including zero, `p`, `p+/-1`, `2p`, the maximum 256-bit input, canonical output, unchanged sources and in-place aliases. Another 1,024 inputs check every group's radix bounds, coefficient interval and modular congruences, then compare the terminal GCD with `math/big`.
- Dictionary collision tests cover both fingerprint directions, every placement, byte mutations, mixed anchors, short fallbacks and padded boundaries. Sign-filter expectations use independent base32/string matching over both possible signs, not the production matcher as their oracle.
- Complete paired keys were compared with independent scalar multiplication across two table transitions, interleaved reseeds, boundary scalars and epoch expiration. Formula tests include identity and torsion points. Saved-key tests verify signatures, nonce independence, immutable snapshots, both pending sides, exact-sign rejection, discarded-candidate accounting and zero allocations.
- Cancellation, save failures and entropy failures retain their batch/error contracts. Existing Tor address vectors, expanded-key validation and file-format tests pass.
- Final bounded PACE fuzzing used `-fuzztime=10s -parallel=1` and `GOMAXPROCS=1` for field arithmetic, paired generation, strided dictionaries, sign filtering and anchored dictionaries, each native and `purego`. All ten runs passed, processing over 2.8 million inputs including invalid-length rejections.
- Custom `vet` passed for native/purego and Linux amd64/arm64 and Darwin arm64 targets. A PACE Windows binary and stock-Go cross-builds succeeded; non-Windows binaries were not executed. Feature-poor hardware was not available, so the portable test matrix supplies fallback coverage.

For example, run `pace test -vet=off ./internal/search -run '^$' -fuzz '^FuzzPaired$' -fuzztime=10s -parallel=1`, then repeat with `-tags purego`. The corresponding matcher targets are `FuzzSignFilter`, `FuzzDictionary` and `FuzzAnchoredDictionary`; arithmetic uses `FuzzFieldArithmetic`. The custom `vet --tests ./...` and `vet --tests --tags purego ./...` commands provide static checks without invoking stock vet separately.

## Small anchored word sets

### Baseline and retained implementation

This pass starts at `15ccb81`. Relative to the requested reference `c08dd8d76e2f116419de824d685dc19828e7e311`, only README, release-workflow and SVG files changed; matcher and search code are identical. Before editing, the original search-test and CLI executables were preserved as `measurements/anchored-original-search.exe` and `measurements/anchored-original-cli.exe`. A second baseline, `anchored-baseline.exe`, includes the new benchmark fixtures but precedes the production optimization. `anchored-baseline-measure.exe` adds the existing opt-in `measure` harness. Candidate executables were preserved separately throughout the experiment.

Retained: `matcherSingleWordSet`, one little-endian word load and a scalar loop comparing `word & probe.mask == probe.value`. Compilation selects it only after the existing exclusions and empty handling, when there are multiple probes, one table holds every probe and that table has no residual checks. The selected table is copied into `tables[0]` and its byte offset uses the existing field. Matcher size does not grow. Each probe keeps its own mask. Single-probe, cross-word, scan, character, anchored-index and dictionary paths retain their constraints; boundary/signless children may specialize without bypassing their enclosing exact/checksum logic.

The original Linux EPYC/PACE screen reported 91.71 → 85.22 ns/key (+7.6%) for `donate. mirror. secure.` and 91.93 → 84.61 (+8.7%) for `donates. mirrors. secured.`. Those are screening observations, not Ryzen predictions. Here the same two workloads improve by 7.50-7.52% in longer Ryzen single-worker samples, 7.25-7.90% with the production coordinator at 16/32 workers and 8.69% in the persisted three-prefix CLI confirmation.

The single-pattern concern was investigated explicitly. Pinned two-second samples show a repeatable roughly 0.09 ns/key (0.21-0.24%) cost for the controls, rather than the VM's possible 1.6%. `pace tool objdump -s searchBatch` on baseline/scalar shows the same single-probe load/mask/compare sequence, registers and stack offsets. The dispatch remains a jump table; its bound changes from 9 to 10 and surrounding code/branch targets move. This is consistent with code-layout sensitivity, not an extra single-probe matching operation; it does not establish a hardware-counter explanation. Production-loop controls at 1/16/32 workers range from -0.12% to +0.06%. We accept this small measured cost against the repeatable multi-pattern gain.

### Individually compared variants and stopping decision

1. **Scalar word set: accepted.** The 350 ms screen improves useful 2-63-prefix sets by 5.13-8.02%, including differing masks and shared beginnings. Twelve-character prefixes fit; thirteen-character/cross-word patterns correctly keep the general path. The 64-prefix anchored index is essentially flat. Longer samples confirm the primary three-prefix wins.
2. **Shared mask: rejected against scalar.** A separate kind used `single.mask`, masking once before comparing values; unequal masks retained scalar matching. This avoided a new field, but the longer run gave only another 1.04% for three-prefix searches, with 1.41% all-hit and 1.63% short/mixed-dictionary regressions. Set63 gained 5.09%, insufficient to justify those regressions and larger hot code.
3. **Explicit two/three/four comparisons: rejected against scalar.** A length switch inside the scalar case used explicit masked OR comparisons for 2/3/4 probes and the loop otherwise. Longer three-prefix gains were another 1.34-1.49%, but cross-word fallback regressed 0.68% after a 0.92% screening regression. The extra hot code was not retained. This variant was tested independently of the rejected shared-mask change.

The scalar implementation already exceeds the >=3% complete-search target. No AVX2 word-set implementation, rejection index, arithmetic rewrite or dictionary project was pursued without new profiling evidence. Canonicalization remains required: its earlier roughly 4.5% VM ablation was intentionally invalid and is not an optimization candidate. Generation, candidate order, immediate match handling, reseeding, saved-key ownership/independence and persistence are unchanged.

### Fixtures, machine and protocol

Windows/amd64, AMD Ryzen 9 9950X3D (16 physical cores, 32 threads), PACE Go 1.27.1; compatibility checks also use stock Go 1.27.1. All performance binaries use `GOAMD64=v1`, `-pgo=off` and native dispatch capped at AVX2 with `GODEBUG=cpu.avx512f=off,cpu.avx512bw=off,cpu.avx512vl=off`. No dependencies or CPU tuning were changed.

`BenchmarkAnchoredSearch` compiles patterns and creates `testPairedGenerator` before `b.Loop`. Its deterministic SHAKE stream, `state.searchBatch`, synchronous `benchmarkSave`, exact `stats.Checked == uint64(b.N)*batchSize` assertion, allocation reporting and `ns/key`/`keys/s` metrics follow the existing full-search convention. Initialization and disk I/O are excluded. `three6` is `donate. mirror. secure.`; `three7` is `donates. mirrors. secured.`. Controls are `donate.`, `privacy.` and `somethingrare.`. Selective sets contain 2, 4, 8, 16, 32, 63 or 64 deterministic ten-character prefixes. Other fixtures cover mixed lengths/masks, shared beginnings, twelve-character word edges and cross-word patterns. Existing `BenchmarkFullSearch`, `BenchmarkSuffixCosts` and `BenchmarkDictionarySearch` provide frequent/all-hit, suffix, mixed-pattern and dictionary/index regressions. The suffix fixture's existing `reseed_random` diagnostic deliberately uses OS entropy; all new anchored fixtures and the ordinary full/dictionary search fixtures use deterministic entropy.

The first screen reproduces one pinned logical CPU (CPU 2, affinity mask 4), `GOMAXPROCS=1`, one-second warm-up per binary/workload, six alternating baseline/candidate pairs with reversed order on odd pairs and 350 ms per workload. Promising variants were then compared with two-second samples using the same ordering. Each change compares to the last accepted version, not to the initial baseline. The explicit-comparison confirmation hit the outer 360-second harness limit after five complete pairs because the `512` sub-benchmark regex also selected `shared512`, `mixed512` and `short512`; all five pairs are reported, with no partial or selected-out sample. Other screens/confirmations have six complete pairs. Outliers are retained.

Worker validation uses the existing production coordinator, deterministic per-worker streams, spread physical-core-first pinning, a 200 ms warm-up per worker and synchronized start. Four alternating pairs use three seconds per workload at 1, 16 and 32 workers. These are process-level throughput measurements, so they should not be compared directly with the pinned batch-loop timings. Final CLI validation uses four alternating ten-second pairs at 32 workers, normal secure entropy and normal filesystem persistence. The helper sends a Windows process-group CTRL_BREAK for graceful final counters. Rates use all checked keys over elapsed time, never time-to-first-match.

### Exact reproduction commands

Run from the repository root. The measurement scripts are preserved under `internal/search/testdata/anchored/`; during the measurements they ran from the ignored `scripts/` directory with identical behavior. `measure-single.ps1` is the existing single-core harness. Build the baseline after adding the fixture files and before changing `pattern.go`/`pattern_compile.go`; the original binaries precede even those fixture edits. The shared/explicit binaries refer to temporary variants described above, which were removed after measurement. Logs and executables remain in the ignored `measurements/` directory; the raw per-key samples are also recorded below.

```powershell
$env:GOAMD64 = 'v1'
$env:GODEBUG = 'cpu.avx512f=off,cpu.avx512bw=off,cpu.avx512vl=off'
New-Item -ItemType Directory -Force measurements | Out-Null
pace test -vet=off -pgo=off -c -o measurements/anchored-original-search.exe ./internal/search
pace build -pgo=off -o measurements/anchored-original-cli.exe .
# After adding fixtures, before modifying production matching:
pace test -vet=off -pgo=off -c -o measurements/anchored-baseline.exe ./internal/search
pace test -vet=off -pgo=off -tags measure -c -o measurements/anchored-baseline-measure.exe ./internal/search
# Build each temporary variant at its respective revision:
pace test -vet=off -pgo=off -c -o measurements/anchored-scalar.exe ./internal/search
pace test -vet=off -pgo=off -c -o measurements/anchored-shared.exe ./internal/search
pace test -vet=off -pgo=off -c -o measurements/anchored-explicit.exe ./internal/search
# Return to retained scalar code:
pace test -vet=off -pgo=off -tags measure -c -o measurements/anchored-scalar-measure.exe ./internal/search
pace build -pgo=off -o measurements/anchored-scalar-cli.exe .

$single = 'internal/search/testdata/anchored/measure-single.ps1'
& $single -Baseline measurements/anchored-baseline.exe -Candidate measurements/anchored-scalar.exe -Name anchored-scalar-screen -Bench '^Benchmark(AnchoredSearch|FullSearch|SuffixCosts)$' -Time 350ms -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-baseline.exe -Candidate measurements/anchored-scalar.exe -Name anchored-scalar-confirm -Bench '^BenchmarkAnchoredSearch$/(donate|privacy|rare|three6|three7)$' -Time 2s -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-scalar.exe -Candidate measurements/anchored-shared.exe -Name anchored-shared-screen -Bench '^Benchmark(AnchoredSearch|FullSearch)$' -Time 350ms -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-scalar.exe -Candidate measurements/anchored-shared.exe -Name anchored-shared-confirm -Bench '^Benchmark(AnchoredSearch|FullSearch)$/(donate|privacy|rare|three6|three7|cross_word|set63|all_hits|short512)$' -Time 2s -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-scalar.exe -Candidate measurements/anchored-explicit.exe -Name anchored-explicit-screen -Bench '^Benchmark(AnchoredSearch|FullSearch)$' -Time 350ms -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-scalar.exe -Candidate measurements/anchored-explicit.exe -Name anchored-explicit-confirm -Bench '^Benchmark(AnchoredSearch|FullSearch)$/(donate|privacy|rare|three6|three7|cross_word|set2|set4|512)$' -Time 2s -Count 6 -Affinity 4
& $single -Baseline measurements/anchored-baseline.exe -Candidate measurements/anchored-scalar.exe -Name anchored-dictionary-screen -Bench '^BenchmarkDictionarySearch$' -Time 350ms -Count 6 -Affinity 4

& internal/search/testdata/anchored/measure-workers.ps1 -Workers '16,32' -Name anchored-workers-many -Pairs 4
& internal/search/testdata/anchored/measure-workers.ps1 -Workers '1' -Name anchored-workers-one -Pairs 4
go build -pgo=off -o measurements/anchored-cli-runner.exe internal/search/testdata/anchored/measure-cli_windows.go
& measurements/anchored-cli-runner.exe
# The helper executes both baseline and scalar, four alternating pairs:
# <binary> --cpu 32 --output measurements/anchored-cli-<pair>-<side>-matches donate. mirror. secure.

pace tool objdump -s searchBatch measurements/anchored-baseline.exe
pace tool objdump -s searchBatch measurements/anchored-scalar.exe
pace test -vet=off -pgo=off ./...
pace test -vet=off -pgo=off -tags purego ./...
go test -vet=off -pgo=off ./...
go test -vet=off -pgo=off -tags purego ./...
vet --os windows
vet --os linux
vet --os darwin
vet --tests --tags purego --os windows ./...
vet --tests --tags measure --os windows ./...
vet --os windows ./internal/search/testdata/anchored
```

### Correctness and allocation results

All four full test suites passed: PACE/stock Go, native/`purego`, with `-vet=off -pgo=off`. Only custom `vet` was used; Windows, Linux and Darwin checks passed, as did Windows `purego` and `measure` checks. Initial house-rule blank-line diagnostics in the new tests were corrected before the successful runs.

`TestWordSetsAgainstStrings` checks independent visible-base32/string references, constructed positive matches, every single-bit mutation, random inputs, duplicates, shared beginnings, differing lengths/masks, cross-word, combined-anchor and suffix patterns, plus scan/character/dictionary mixtures. Counts span 2-64. Both scalar and available native compiler backends are exercised. The independent sign-filter oracle enumerates both signs and all checksum nibbles. Visible suffix/checksum wrappers and their signless children are covered. `TestWordSetOffsets` asserts specialization and checks independent base32 substrings at all four word offsets, including differing masks. Existing search tests continue to cover candidate accounting, immediate saves, reseeding and saved-key independence.

All 1,450 recorded batch-search benchmark samples below report **0 B/op, 0 allocs/op**, including the existing reseed/save regressions. `testing.AllocsPerRun` also confirms zero Match allocations. The separate process-level worker harness includes bounded startup-release/cancellation bookkeeping in its MemStats window: 2-3 allocations/136-616 bytes for one worker, 17-19/496-992 for 16 and 33-35/880-1392 for 32. Those are totals per run, not per-key steady-state allocations. CLI persistence naturally allocates and performs disk I/O; it is reported separately.

### Raw single-worker results

Each raw list is in pair order. Units are ns/key; summary columns are median [minimum-maximum]. Throughput change is `100 * (before median / after median - 1)`. `A`, `F`, `S` and `D` abbreviate `BenchmarkAnchoredSearch`, `BenchmarkFullSearch`, `BenchmarkSuffixCosts` and `BenchmarkDictionarySearch`. The preserved text logs also contain iteration counts, ns/op, keys/s and allocation columns. Every batch-search sample checks its exact candidate count internally.

#### Scalar screen: baseline → scalar, 350 ms x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.7, 39.5, 39.5, 39.52, 39.5, 39.52 | 39.59, 39.6, 39.58, 39.59, 39.6, 39.61 | 39.510 [39.5-39.7] | 39.595 [39.58-39.61] | -0.21% |
| A/privacy | 39.67, 39.49, 39.49, 39.5, 39.51, 39.51 | 39.58, 39.59, 39.58, 39.59, 39.61, 39.59 | 39.505 [39.49-39.67] | 39.590 [39.58-39.61] | -0.21% |
| A/rare | 39.66, 39.51, 39.53, 39.51, 39.52, 39.51 | 39.64, 39.62, 39.59, 39.6, 39.6, 39.58 | 39.515 [39.51-39.66] | 39.600 [39.58-39.64] | -0.21% |
| A/three6 | 43.66, 43.48, 43.67, 43.44, 43.66, 43.63 | 40.44, 40.82, 40.4, 40.41, 40.39, 40.39 | 43.645 [43.44-43.67] | 40.405 [40.39-40.82] | 8.02% |
| A/three7 | 43.92, 43.56, 43.71, 43.45, 43.74, 43.43 | 40.43, 40.51, 40.48, 40.42, 40.48, 40.41 | 43.635 [43.43-43.92] | 40.455 [40.41-40.51] | 7.86% |
| A/mixed_lengths | 43.9, 43.44, 43.49, 43.48, 43.73, 43.31 | 40.4, 40.41, 40.36, 40.44, 40.41, 40.54 | 43.485 [43.31-43.9] | 40.410 [40.36-40.54] | 7.61% |
| A/shared | 43.93, 43.6, 43.68, 43.62, 43.63, 43.64 | 40.64, 40.61, 40.58, 40.6, 40.57, 40.57 | 43.635 [43.6-43.93] | 40.590 [40.57-40.64] | 7.50% |
| A/word_edge | 44.35, 43.92, 44.04, 43.97, 44.01, 43.95 | 41.48, 40.92, 40.89, 41.06, 40.94, 40.85 | 43.990 [43.92-44.35] | 40.930 [40.85-41.48] | 7.48% |
| A/cross_word | 44.36, 43.92, 43.96, 43.83, 43.88, 44.17 | 45.28, 43.8, 43.92, 43.89, 43.98, 43.74 | 43.940 [43.83-44.36] | 43.905 [43.74-45.28] | 0.08% |
| A/set2 | 43.87, 43.39, 43.47, 43.5, 43.55, 43.47 | 41.32, 40.87, 40.66, 40.66, 40.69, 40.77 | 43.485 [43.39-43.87] | 40.730 [40.66-41.32] | 6.76% |
| A/set4 | 44.49, 44.17, 44.23, 43.91, 44.07, 44.05 | 42.99, 41.03, 41.02, 41.06, 41.16, 40.95 | 44.120 [43.91-44.49] | 41.045 [40.95-42.99] | 7.49% |
| A/set8 | 45.11, 44.69, 44.8, 44.84, 44.79, 44.95 | 42.21, 41.99, 42.06, 42.02, 41.89, 41.81 | 44.820 [44.69-45.11] | 42.005 [41.81-42.21] | 6.70% |
| A/set16 | 47.07, 46.45, 46.51, 46.68, 46.6, 46.7 | 43.79, 48.1, 43.81, 43.79, 43.79, 43.68 | 46.640 [46.45-47.07] | 43.790 [43.68-48.1] | 6.51% |
| A/set32 | 50.88, 50.16, 50.23, 50.24, 50.2, 50.18 | 47.56, 52.76, 47.59, 47.38, 47.62, 47.46 | 50.215 [50.16-50.88] | 47.575 [47.38-52.76] | 5.55% |
| A/set63 | 57.54, 57.31, 57.16, 59, 57.24, 57.25 | 54.65, 54.57, 54.4, 54.74, 54.24, 54.33 | 57.280 [57.16-59] | 54.485 [54.24-54.74] | 5.13% |
| A/set64 | 44.41, 42.17, 42.17, 42.05, 42.12, 42.05 | 42.25, 42.09, 42.15, 42.05, 42.09, 42.01 | 42.145 [42.05-44.41] | 42.090 [42.01-42.25] | 0.13% |
| F/rare | 41.63, 39.91, 40.12, 40.02, 40.15, 39.87 | 40.14, 40.07, 40.02, 39.94, 40.16, 40.16 | 40.070 [39.87-41.63] | 40.105 [39.94-40.16] | -0.09% |
| F/frequent | 50.6, 49.57, 49.44, 49.41, 49.49, 49.36 | 49.27, 49.56, 49.26, 49.22, 49.38, 49.25 | 49.465 [49.36-50.6] | 49.265 [49.22-49.56] | 0.41% |
| F/all_hits | 8100, 8250, 7880, 7939, 7872, 7906 | 7823, 7880, 8001, 7863, 7853, 7837 | 7922.500 [7872-8250] | 7858.000 [7823-8001] | 0.82% |
| F/512 | 66.22, 68.48, 65.07, 65.11, 65.08, 64.86 | 64.78, 64.75, 64.94, 64.51, 64.72, 64.85 | 65.095 [64.86-68.48] | 64.765 [64.51-64.94] | 0.51% |
| F/shared512 | 66.54, 78.65, 64.78, 64.57, 64.52, 64.58 | 64.22, 64.24, 64.3, 64.56, 64.77, 64.36 | 64.680 [64.52-78.65] | 64.330 [64.22-64.77] | 0.54% |
| F/mixed512 | 70.81, 69.18, 68.13, 67.96, 68.29, 68.01 | 67.8, 67.71, 68, 67.84, 68, 67.75 | 68.210 [67.96-70.81] | 67.820 [67.71-68] | 0.58% |
| F/short512 | 512.5, 492.3, 491.3, 494, 494.6, 493.2 | 491.3, 489.7, 491.3, 491.6, 491.2, 490.7 | 493.600 [491.3-512.5] | 491.250 [489.7-491.6] | 0.48% |
| S/rare_prefix/matching_only | 41.25, 39.99, 39.99, 39.99, 40.04, 40.07 | 39.89, 40.16, 40, 39.91, 40.2, 39.84 | 40.015 [39.99-41.25] | 39.955 [39.84-40.2] | 0.15% |
| S/rare_prefix/reseed_shake | 41.4, 39.95, 40.03, 40.03, 39.99, 39.95 | 39.97, 39.95, 40.09, 40.13, 40.01, 39.97 | 40.010 [39.95-41.4] | 39.990 [39.95-40.13] | 0.05% |
| S/rare_prefix/reseed_random | 42.1, 40.02, 39.96, 39.93, 39.9, 40.03 | 40.03, 40, 40.53, 39.93, 39.95, 40.11 | 39.990 [39.9-42.1] | 40.015 [39.93-40.53] | -0.06% |
| S/prefix1/matching_only | 84.52, 83.1, 82.85, 82.62, 82.88, 82.91 | 83.11, 83.31, 83.06, 83.02, 82.98, 83.19 | 82.895 [82.62-84.52] | 83.085 [82.98-83.31] | -0.23% |
| S/prefix1/reseed_shake | 339.2, 330.7, 330.2, 329.1, 330.4, 330 | 328.4, 329, 329.8, 328.4, 328.5, 329 | 330.300 [329.1-339.2] | 328.750 [328.4-329.8] | 0.47% |
| S/prefix1/reseed_random | 339.4, 328.4, 329.9, 328.4, 328.1, 331.3 | 327.6, 326.3, 326.5, 327.2, 328.1, 325.8 | 329.150 [328.1-339.4] | 326.850 [325.8-328.1] | 0.70% |
| S/suffix1/matching_only | 223.4, 220.7, 220.9, 220.6, 220.5, 220.4 | 220.7, 220.7, 220.4, 221.1, 220.3, 220.3 | 220.650 [220.4-223.4] | 220.550 [220.3-221.1] | 0.05% |
| S/suffix1/reseed_shake | 481, 466.3, 465.9, 465.4, 465.9, 464.3 | 462.1, 463.3, 461.3, 462.2, 465.4, 461.5 | 465.900 [464.3-481] | 462.150 [461.3-465.4] | 0.81% |
| S/suffix1/reseed_random | 481, 465.6, 467, 468.9, 468.2, 467.1 | 465, 462.3, 466, 463.1, 463.7, 463.5 | 467.650 [465.6-481] | 463.600 [462.3-466] | 0.87% |
| S/suffix2/matching_only | 47.1, 45.49, 45.49, 45.39, 45.41, 45.4 | 45.03, 45.05, 45.13, 45.04, 44.96, 45.01 | 45.450 [45.39-47.1] | 45.035 [44.96-45.13] | 0.92% |
| S/suffix2/reseed_shake | 55.6, 54.8, 55.04, 54.91, 54.8, 54.75 | 54.56, 54.96, 55.99, 54.29, 54.35, 54.38 | 54.855 [54.75-55.6] | 54.470 [54.29-55.99] | 0.71% |
| S/suffix2/reseed_random | 56.15, 54.89, 55.01, 54.66, 55, 54.99 | 54.51, 54.55, 70.12, 54.74, 54.62, 54.75 | 54.995 [54.66-56.15] | 54.680 [54.51-70.12] | 0.58% |
| S/suffix3/matching_only | 41.81, 40.22, 40.18, 40.24, 40.34, 40.22 | 40.15, 40.18, 44.37, 40.16, 40.15, 40.23 | 40.230 [40.18-41.81] | 40.170 [40.15-44.37] | 0.15% |
| S/suffix3/reseed_shake | 41.2, 40.7, 40.45, 40.64, 40.58, 40.44 | 40.61, 40.58, 40.57, 40.56, 40.45, 40.54 | 40.610 [40.44-41.2] | 40.565 [40.45-40.61] | 0.11% |
| S/suffix3/reseed_random | 42.21, 40.59, 40.49, 40.52, 40.55, 40.61 | 40.44, 40.58, 40.88, 40.58, 40.54, 40.48 | 40.570 [40.49-42.21] | 40.560 [40.44-40.88] | 0.02% |
| S/suffix4/matching_only | 41.57, 40.1, 40.04, 40.01, 40.2, 40.02 | 39.9, 40.09, 39.91, 39.98, 39.98, 39.9 | 40.070 [40.01-41.57] | 39.945 [39.9-40.09] | 0.31% |
| S/suffix4/reseed_shake | 41.24, 40, 40.05, 40.02, 40.03, 39.9 | 40.02, 40.01, 40.13, 40.3, 40.04, 40.04 | 40.025 [39.9-41.24] | 40.040 [40.01-40.3] | -0.04% |
| S/suffix4/reseed_random | 42.96, 40.03, 40.29, 39.97, 39.93, 40 | 40.13, 40, 40.06, 39.99, 40.07, 40.13 | 40.015 [39.93-42.96] | 40.065 [39.99-40.13] | -0.12% |

#### Scalar confirmation: baseline → scalar, two seconds x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.5, 39.49, 39.51, 39.55, 39.52, 39.52 | 39.6, 39.6, 39.6, 39.61, 39.61, 39.61 | 39.515 [39.49-39.55] | 39.605 [39.6-39.61] | -0.23% |
| A/privacy | 39.83, 39.86, 39.82, 40.75, 40.83, 39.82 | 39.94, 39.91, 39.94, 39.93, 39.97, 39.95 | 39.845 [39.82-40.83] | 39.940 [39.91-39.97] | -0.24% |
| A/rare | 39.94, 39.94, 39.99, 41.17, 40.1, 39.98 | 40.06, 40.08, 40.04, 40.08, 40.16, 40.03 | 39.985 [39.94-41.17] | 40.070 [40.03-40.16] | -0.21% |
| A/three6 | 44.15, 43.9, 43.99, 45.25, 44.04, 43.96 | 40.97, 40.94, 40.89, 41.62, 40.93, 40.85 | 44.015 [43.9-45.25] | 40.935 [40.85-41.62] | 7.52% |
| A/three7 | 44.02, 44.05, 43.96, 44.88, 43.99, 44.01 | 41.66, 40.92, 40.9, 42.03, 40.97, 40.84 | 44.015 [43.96-44.88] | 40.945 [40.84-42.03] | 7.50% |

#### Dictionary regression screen: baseline → scalar, 350 ms x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| D/prefix/64 | 41.65, 41.69, 41.67, 41.65, 41.64, 41.67 | 41.71, 41.67, 41.67, 41.72, 41.64, 41.66 | 41.660 [41.64-41.69] | 41.670 [41.64-41.72] | -0.02% |
| D/prefix/512 | 41.83, 41.84, 41.85, 41.8, 41.78, 41.81 | 41.85, 41.82, 41.8, 41.81, 41.81, 41.83 | 41.820 [41.78-41.85] | 41.815 [41.8-41.85] | 0.01% |
| D/suffix/64 | 41.65, 41.67, 41.66, 41.62, 41.62, 41.64 | 41.67, 41.69, 41.62, 41.64, 41.68, 41.64 | 41.645 [41.62-41.67] | 41.655 [41.62-41.69] | -0.02% |
| D/suffix/512 | 41.8, 41.8, 41.79, 41.79, 41.79, 41.81 | 41.87, 41.84, 41.8, 41.87, 41.8, 41.81 | 41.795 [41.79-41.81] | 41.825 [41.8-41.87] | -0.07% |
| D/combined/64 | 41.65, 41.68, 41.64, 41.63, 41.64, 41.65 | 41.64, 41.33, 41.65, 41.67, 41.65, 41.65 | 41.645 [41.63-41.68] | 41.650 [41.33-41.67] | -0.01% |
| D/combined/512 | 41.79, 41.79, 41.8, 41.77, 41.8, 41.8 | 41.85, 41.43, 41.82, 41.84, 41.83, 41.83 | 41.795 [41.77-41.8] | 41.830 [41.43-41.85] | -0.08% |
| D/long/64 | 55.86, 55.77, 55.75, 55.71, 55.8, 55.72 | 55.57, 55.6, 55.54, 55.48, 55.54, 55.54 | 55.760 [55.71-55.86] | 55.540 [55.48-55.6] | 0.40% |
| D/long/512 | 63.18, 63.05, 62.97, 63.21, 62.96, 63.29 | 62.79, 62.88, 62.73, 62.76, 62.73, 62.8 | 63.115 [62.96-63.29] | 62.775 [62.73-62.88] | 0.54% |

#### Shared-mask screen: scalar → shared, 350 ms x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.59, 39.59, 39.59, 39.59, 39.59, 39.63 | 39.5, 39.52, 39.5, 39.5, 39.49, 39.51 | 39.590 [39.59-39.63] | 39.500 [39.49-39.52] | 0.23% |
| A/privacy | 39.61, 39.59, 39.59, 39.59, 39.63, 39.63 | 39.51, 39.51, 39.49, 39.5, 39.46, 39.46 | 39.600 [39.59-39.63] | 39.495 [39.46-39.51] | 0.27% |
| A/rare | 39.6, 39.58, 39.61, 39.57, 39.6, 39.6 | 39.49, 39.51, 39.49, 39.47, 39.51, 39.5 | 39.600 [39.57-39.61] | 39.495 [39.47-39.51] | 0.27% |
| A/three6 | 40.39, 40.4, 40.41, 40.48, 40.53, 40.49 | 39.97, 40.25, 39.97, 39.99, 39.98, 39.98 | 40.445 [40.39-40.53] | 39.980 [39.97-40.25] | 1.16% |
| A/three7 | 40.38, 40.39, 40.41, 40.5, 40.49, 40.38 | 40, 39.96, 39.97, 40, 40, 40.02 | 40.400 [40.38-40.5] | 40.000 [39.96-40.02] | 1.00% |
| A/mixed_lengths | 40.41, 40.4, 40.48, 40.37, 40.54, 40.39 | 40.22, 40.22, 40.26, 40.25, 40.4, 40.25 | 40.405 [40.37-40.54] | 40.250 [40.22-40.4] | 0.39% |
| A/shared | 40.59, 40.58, 40.58, 40.58, 40.59, 40.59 | 40.1, 40.31, 40.16, 40.3, 40.14, 40.14 | 40.585 [40.58-40.59] | 40.150 [40.1-40.31] | 1.08% |
| A/word_edge | 40.79, 41.27, 40.9, 40.84, 40.86, 41.1 | 40.52, 40.67, 40.58, 40.43, 40.39, 40.59 | 40.880 [40.79-41.27] | 40.550 [40.39-40.67] | 0.81% |
| A/cross_word | 43.81, 43.81, 43.94, 44.26, 43.91, 43.87 | 43.85, 44.14, 44.08, 44.17, 44.05, 43.98 | 43.890 [43.81-44.26] | 44.065 [43.85-44.17] | -0.40% |
| A/set2 | 40.77, 40.92, 40.72, 40.6, 40.81, 40.78 | 40.41, 40.19, 40.83, 40.35, 40.38, 40.41 | 40.775 [40.6-40.92] | 40.395 [40.19-40.83] | 0.94% |
| A/set4 | 41.09, 41.23, 41.07, 41.11, 41.22, 41.3 | 40.55, 40.58, 40.78, 40.6, 40.72, 40.6 | 41.165 [41.07-41.3] | 40.600 [40.55-40.78] | 1.39% |
| A/set8 | 41.95, 41.92, 41.98, 42.09, 42.06, 42.11 | 41.46, 41.78, 41.47, 41.3, 41.41, 41.62 | 42.020 [41.92-42.11] | 41.465 [41.3-41.78] | 1.34% |
| A/set16 | 43.75, 43.98, 43.83, 43.78, 43.69, 43.84 | 43.06, 42.9, 42.96, 42.92, 43.53, 43.01 | 43.805 [43.69-43.98] | 42.985 [42.9-43.53] | 1.91% |
| A/set32 | 47.46, 47.68, 47.59, 47.44, 47.6, 47.75 | 46.08, 46.18, 46.01, 46.25, 46.37, 46.5 | 47.595 [47.44-47.75] | 46.215 [46.01-46.5] | 2.99% |
| A/set63 | 54.75, 54.48, 55.01, 54.88, 54.52, 54.76 | 51.91, 51.82, 52.02, 51.91, 52.08, 51.93 | 54.755 [54.48-55.01] | 51.920 [51.82-52.08] | 5.46% |
| A/set64 | 42.05, 42.27, 42.21, 42.38, 42.2, 42.32 | 42.11, 42.16, 42.5, 42.43, 42.18, 42.23 | 42.240 [42.05-42.38] | 42.205 [42.11-42.5] | 0.08% |
| F/rare | 39.98, 40.08, 40.09, 40.09, 40.1, 40.23 | 39.99, 39.95, 40.05, 39.88, 39.97, 40.07 | 40.090 [39.98-40.23] | 39.980 [39.88-40.07] | 0.28% |
| F/frequent | 49.18, 49.41, 49.28, 49.52, 49.37, 49.63 | 49.44, 49.43, 49.29, 49.45, 49.29, 49.73 | 49.390 [49.18-49.63] | 49.435 [49.29-49.73] | -0.09% |
| F/all_hits | 7877, 7882, 7883, 7859, 7854, 7896 | 7972, 7985, 7999, 7961, 7994, 8003 | 7879.500 [7854-7896] | 7989.500 [7961-8003] | -1.38% |
| F/512 | 64.91, 64.97, 64.95, 64.77, 64.94, 64.92 | 64.71, 64.96, 71.35, 64.97, 65.12, 65.21 | 64.930 [64.77-64.97] | 65.045 [64.71-71.35] | -0.18% |
| F/shared512 | 64.77, 64.64, 64.58, 64.38, 64.52, 64.64 | 64.42, 64.53, 71.94, 64.58, 64.46, 64.87 | 64.610 [64.38-64.77] | 64.555 [64.42-71.94] | 0.09% |
| F/mixed512 | 68.06, 68.13, 67.97, 67.81, 68.04, 68.1 | 67.91, 68.12, 68.24, 68.39, 68.16, 68.34 | 68.050 [67.81-68.13] | 68.200 [67.91-68.39] | -0.22% |
| F/short512 | 492, 491.1, 494.7, 492.4, 491.8, 492.5 | 497, 498, 500.5, 498.1, 499.5, 499.2 | 492.200 [491.1-494.7] | 498.650 [497-500.5] | -1.29% |

#### Shared-mask confirmation: scalar → shared, two seconds x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.6, 39.58, 39.6, 39.59, 39.59, 39.59 | 39.5, 39.51, 39.51, 39.49, 39.49, 39.49 | 39.590 [39.58-39.6] | 39.495 [39.49-39.51] | 0.24% |
| A/privacy | 40.04, 40.07, 40.11, 40.12, 39.92, 39.95 | 42.03, 39.96, 40.07, 41.84, 39.81, 39.79 | 40.055 [39.92-40.12] | 40.015 [39.79-42.03] | 0.10% |
| A/rare | 40.2, 40.24, 40.64, 40.33, 40.01, 41.25 | 40.17, 40.36, 40.3, 40.23, 39.95, 39.91 | 40.285 [40.01-41.25] | 40.200 [39.91-40.36] | 0.21% |
| A/three6 | 41.17, 41.12, 41.12, 41.15, 40.87, 40.91 | 40.68, 41.78, 40.71, 40.76, 40.43, 40.36 | 41.120 [40.87-41.17] | 40.695 [40.36-41.78] | 1.04% |
| A/three7 | 41.09, 41.15, 41.19, 41.22, 40.83, 40.99 | 40.61, 41.25, 40.78, 40.79, 40.42, 40.39 | 41.120 [40.83-41.22] | 40.695 [40.39-41.25] | 1.04% |
| A/cross_word | 44.05, 44.06, 44.14, 44.24, 43.8, 43.86 | 44.05, 45.18, 44.26, 44.26, 43.72, 43.8 | 44.055 [43.8-44.24] | 44.155 [43.72-45.18] | -0.23% |
| A/set63 | 54.74, 60.7, 55.03, 55.09, 54.36, 54.42 | 52.27, 53.35, 52.18, 52.34, 51.96, 51.9 | 54.885 [54.36-60.7] | 52.225 [51.9-53.35] | 5.09% |
| F/rare | 40.23, 40.77, 40.33, 40.45, 40.08, 40.09 | 40.06, 40.86, 40.22, 40.28, 40.09, 39.91 | 40.280 [40.08-40.77] | 40.155 [39.91-40.86] | 0.31% |
| F/all_hits | 7909, 8203, 7941, 7910, 7863, 7854 | 8014, 8239, 8050, 8031, 7973, 7968 | 7909.500 [7854-8203] | 8022.500 [7968-8239] | -1.41% |
| F/short512 | 501.7, 497.8, 497.8, 494, 494.4, 495.9 | 505.2, 511, 506.3, 505, 500.7, 500.6 | 496.850 [494-501.7] | 505.100 [500.6-511] | -1.63% |

#### Explicit-comparison screen: scalar → explicit, 350 ms x six pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.59, 39.59, 39.6, 39.59, 39.6, 39.6 | 39.47, 39.52, 39.51, 39.5, 39.52, 39.5 | 39.595 [39.59-39.6] | 39.505 [39.47-39.52] | 0.23% |
| A/privacy | 39.59, 39.6, 39.61, 39.61, 39.59, 39.64 | 39.48, 39.49, 39.5, 39.52, 39.5, 39.5 | 39.605 [39.59-39.64] | 39.500 [39.48-39.52] | 0.27% |
| A/rare | 39.59, 39.58, 39.58, 39.55, 39.61, 39.63 | 39.51, 39.48, 39.51, 39.55, 39.51, 39.48 | 39.585 [39.55-39.63] | 39.510 [39.48-39.55] | 0.19% |
| A/three6 | 40.39, 40.38, 40.42, 40.38, 40.38, 40.48 | 39.87, 39.89, 39.9, 39.93, 39.9, 39.89 | 40.385 [40.38-40.48] | 39.895 [39.87-39.93] | 1.23% |
| A/three7 | 40.39, 40.37, 40.4, 40.39, 40.41, 40.41 | 39.89, 39.88, 39.91, 39.93, 39.89, 39.89 | 40.395 [40.37-40.41] | 39.890 [39.88-39.93] | 1.27% |
| A/mixed_lengths | 40.54, 40.37, 40.41, 40.37, 40.52, 40.42 | 39.88, 39.89, 39.9, 39.91, 39.9, 39.9 | 40.415 [40.37-40.54] | 39.900 [39.88-39.91] | 1.29% |
| A/shared | 40.85, 40.58, 40.62, 40.57, 40.59, 40.55 | 40.05, 40.07, 40.07, 40.11, 40.06, 40.07 | 40.585 [40.55-40.85] | 40.070 [40.05-40.11] | 1.29% |
| A/word_edge | 40.86, 40.95, 40.93, 40.87, 40.91, 40.92 | 40.31, 40.26, 40.24, 40.37, 40.35, 40.35 | 40.915 [40.86-40.95] | 40.330 [40.24-40.37] | 1.45% |
| A/cross_word | 43.97, 43.74, 43.71, 43.75, 43.8, 43.9 | 44.1, 44.28, 44.11, 44.25, 44.05, 44.3 | 43.775 [43.71-43.97] | 44.180 [44.05-44.3] | -0.92% |
| A/set2 | 40.61, 40.65, 40.75, 40.53, 40.55, 40.79 | 40.1, 40.23, 40.33, 40.18, 40.17, 40.1 | 40.630 [40.53-40.79] | 40.175 [40.1-40.33] | 1.13% |
| A/set4 | 40.95, 41.13, 41, 40.97, 40.96, 41.16 | 40.43, 40.61, 40.56, 40.51, 40.47, 40.55 | 40.985 [40.95-41.16] | 40.530 [40.43-40.61] | 1.12% |
| A/set8 | 42.01, 41.95, 42.13, 41.8, 42.01, 42.01 | 42.11, 42.04, 42.04, 42.02, 42, 41.97 | 42.010 [41.8-42.13] | 42.030 [41.97-42.11] | -0.05% |
| A/set16 | 43.74, 43.73, 43.79, 43.88, 43.7, 43.81 | 43.84, 43.78, 43.74, 43.98, 43.73, 43.74 | 43.765 [43.7-43.88] | 43.760 [43.73-43.98] | 0.01% |
| A/set32 | 47.48, 47.5, 47.4, 47.31, 47.45, 47.59 | 47.43, 47.37, 47.5, 47.39, 47.29, 55.95 | 47.465 [47.31-47.59] | 47.410 [47.29-55.95] | 0.12% |
| A/set63 | 54.33, 54.57, 54.55, 54.67, 54.64, 54.65 | 54.09, 54.01, 54.16, 54.29, 54.16, 63.72 | 54.605 [54.33-54.67] | 54.160 [54.01-63.72] | 0.82% |
| A/set64 | 42.24, 42.09, 42.44, 42.08, 42.06, 42.16 | 42.15, 42.23, 42.34, 42.14, 42.19, 42.18 | 42.125 [42.06-42.44] | 42.185 [42.14-42.34] | -0.14% |
| F/rare | 40.15, 40.15, 40.04, 39.94, 40.05, 40.22 | 39.95, 39.88, 39.95, 40.03, 39.96, 39.94 | 40.100 [39.94-40.22] | 39.950 [39.88-40.03] | 0.38% |
| F/frequent | 49.23, 49.15, 49.56, 49.21, 49.21, 49.26 | 49.17, 49.14, 49.25, 49.18, 49.35, 49.11 | 49.220 [49.15-49.56] | 49.175 [49.11-49.35] | 0.09% |
| F/all_hits | 7875, 7848, 7853, 7844, 7848, 7868 | 7850, 7856, 7897, 7905, 7851, 7863 | 7850.500 [7844-7875] | 7859.500 [7850-7905] | -0.11% |
| F/512 | 65.15, 64.68, 64.7, 64.78, 64.6, 64.74 | 64.2, 64.1, 64.16, 64.03, 63.89, 63.7 | 64.720 [64.6-65.15] | 64.065 [63.7-64.2] | 1.02% |
| F/shared512 | 64.44, 64.65, 64.31, 64.44, 64.59, 64.37 | 63.55, 63.71, 63.72, 63.76, 63.27, 63.47 | 64.440 [64.31-64.65] | 63.630 [63.27-63.76] | 1.27% |
| F/mixed512 | 67.97, 67.65, 68.22, 67.72, 68.02, 68.13 | 67.31, 67.57, 67.33, 68.01, 67.03, 66.92 | 67.995 [67.65-68.22] | 67.320 [66.92-68.01] | 1.00% |
| F/short512 | 491.9, 491.1, 490, 490.4, 492.2, 492.2 | 492.5, 493.5, 493.3, 492.3, 491.8, 491.4 | 491.500 [490-492.2] | 492.400 [491.4-493.5] | -0.18% |

#### Explicit-comparison confirmation: scalar → explicit, two seconds x five complete pairs

| Workload | Before raw | After raw | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| A/donate | 39.59, 39.59, 39.64, 39.61, 39.58 | 39.5, 39.5, 39.49, 39.48, 39.51 | 39.590 [39.58-39.64] | 39.500 [39.48-39.51] | 0.23% |
| A/privacy | 39.93, 39.93, 39.99, 40.31, 39.97 | 39.84, 39.84, 39.86, 39.81, 39.88 | 39.970 [39.93-40.31] | 39.840 [39.81-39.88] | 0.33% |
| A/rare | 40.04, 40.1, 40.11, 40.32, 40.03 | 39.99, 39.94, 40.02, 39.95, 40.03 | 40.100 [40.03-40.32] | 39.990 [39.94-40.03] | 0.28% |
| A/three6 | 40.92, 40.95, 40.95, 42.4, 40.86 | 40.42, 40.32, 40.36, 40.41, 40.48 | 40.950 [40.86-42.4] | 40.410 [40.32-40.48] | 1.34% |
| A/three7 | 40.98, 40.87, 40.95, 42.39, 40.92 | 40.39, 40.32, 40.35, 40.33, 40.68 | 40.950 [40.87-42.39] | 40.350 [40.32-40.68] | 1.49% |
| A/cross_word | 44.01, 43.81, 43.88, 43.88, 43.95 | 44.18, 44.15, 44.14, 44.18, 44.19 | 43.880 [43.81-44.01] | 44.180 [44.14-44.19] | -0.68% |
| A/set2 | 40.72, 40.65, 40.62, 40.74, 40.69 | 40.2, 40.13, 40.23, 40.33, 40.29 | 40.690 [40.62-40.74] | 40.230 [40.13-40.33] | 1.14% |
| A/set4 | 41.07, 41.1, 41.24, 41.09, 41.18 | 40.56, 40.55, 40.49, 40.59, 40.64 | 41.100 [41.07-41.24] | 40.560 [40.49-40.64] | 1.33% |
| F/rare | 40.06, 40.91, 40.07, 40.03, 40.07 | 39.97, 40.04, 39.97, 40.25, 39.97 | 40.070 [40.03-40.91] | 39.970 [39.97-40.25] | 0.25% |
| F/512 | 65.26, 67, 64.98, 64.95, 64.92 | 64.03, 63.95, 67.68, 66.91, 63.95 | 64.980 [64.92-67] | 64.030 [63.95-67.68] | 1.48% |
| F/shared512 | 64.58, 66.16, 64.55, 64.5, 64.67 | 63.65, 63.57, 63.54, 63.74, 63.47 | 64.580 [64.5-66.16] | 63.570 [63.47-63.74] | 1.59% |
| F/mixed512 | 68.16, 69.72, 68.08, 69.42, 67.99 | 69.48, 67.21, 67.21, 67.28, 67.46 | 68.160 [67.99-69.72] | 67.280 [67.21-69.48] | 1.31% |
| F/short512 | 495, 507.5, 494.8, 495.5, 495.1 | 495.3, 494.4, 494.9, 496.3, 496.9 | 495.100 [494.8-507.5] | 495.300 [494.4-496.9] | -0.04% |

### Raw worker validation: baseline → scalar

Units are millions of checked keys/second; raw rates are rounded to six decimals. Four alternating pairs, three seconds each, with all samples retained. Summaries are median [minimum-maximum]; throughput change is the ratio of median rates. Original `anchored-workers-{one,many}-<pair>-{before,after}.txt` logs contain full-precision MEASURE JSON with exact Checked, Saved, elapsed Seconds, allocation totals and CPU placements. Spread placement uses logical CPU 0 for one worker; 16 uses `0,16,2,18,4,20,6,22,8,24,10,26,12,28,14,30`; 32 additionally uses their odd-numbered SMT siblings in that order.

| Workload/workers | Before raw Mkeys/s | After raw Mkeys/s | Before median [range] | After median [range] | Throughput |
| --- | --- | --- | ---: | ---: | ---: |
| donate/1 | 23.668582, 23.652045, 23.669837, 23.656928 | 23.640296, 23.661251, 23.659480, 23.651491 | 23.663 [23.652-23.670] | 23.655 [23.640-23.661] | -0.03% |
| privacy/1 | 23.611589, 23.575808, 23.599045, 23.581442 | 23.582085, 23.601159, 23.560887, 23.596369 | 23.590 [23.576-23.612] | 23.589 [23.561-23.601] | -0.00% |
| rare/1 | 23.591708, 23.577882, 23.614191, 23.583834 | 23.605734, 23.595307, 23.619526, 23.599905 | 23.588 [23.578-23.614] | 23.603 [23.595-23.620] | 0.06% |
| three6/1 | 21.572584, 21.536729, 21.571862, 21.521293 | 23.127461, 23.129463, 23.137178, 23.119387 | 21.554 [21.521-21.573] | 23.128 [23.119-23.137] | 7.30% |
| three7/1 | 21.550953, 21.536653, 21.568860, 21.545562 | 23.100942, 23.182294, 23.125703, 23.141445 | 21.548 [21.537-21.569] | 23.134 [23.101-23.182] | 7.36% |
| 512/1 | 14.508617, 14.515280, 14.527984, 14.511727 | 14.442014, 14.501070, 14.550862, 14.561888 | 14.514 [14.509-14.528] | 14.526 [14.442-14.562] | 0.09% |
| donate/16 | 349.398706, 358.519311, 358.965090, 358.603246 | 347.857775, 358.817129, 358.547761, 358.406850 | 358.561 [349.399-358.965] | 358.477 [347.858-358.817] | -0.02% |
| donate/32 | 416.819167, 419.629482, 418.450375, 419.688967 | 416.094638, 419.229533, 419.057903, 418.163619 | 419.040 [416.819-419.689] | 418.611 [416.095-419.230] | -0.10% |
| privacy/16 | 357.794695, 358.686382, 358.980025, 358.996274 | 358.004257, 359.313439, 358.864516, 358.581695 | 358.833 [357.795-358.996] | 358.723 [358.004-359.313] | -0.03% |
| privacy/32 | 415.606700, 418.996465, 418.095234, 419.286798 | 418.228192, 418.698335, 417.042366, 417.848194 | 418.546 [415.607-419.287] | 418.038 [417.042-418.698] | -0.12% |
| rare/16 | 357.954260, 358.474666, 358.951076, 359.029543 | 357.496179, 359.285124, 358.914183, 358.254735 | 358.713 [357.954-359.030] | 358.584 [357.496-359.285] | -0.04% |
| rare/32 | 416.953793, 419.604068, 417.296975, 419.331821 | 417.938330, 419.827666, 418.281373, 416.541484 | 418.314 [416.954-419.604] | 418.110 [416.541-419.828] | -0.05% |
| three6/16 | 321.665649, 324.780306, 325.224510, 324.790820 | 350.328259, 351.028064, 350.564993, 348.967093 | 324.786 [321.666-325.225] | 350.447 [348.967-351.028] | 7.90% |
| three6/32 | 377.959764, 378.281147, 377.396902, 379.383102 | 407.200494, 408.946958, 406.297434, 405.100444 | 378.120 [377.397-379.383] | 406.749 [405.100-408.947] | 7.57% |
| three7/16 | 324.645398, 324.409692, 324.714434, 325.001320 | 349.981923, 351.249756, 350.235175, 348.387937 | 324.680 [324.410-325.001] | 350.109 [348.388-351.250] | 7.83% |
| three7/32 | 378.155320, 379.904756, 378.312946, 379.358858 | 405.665411, 406.923246, 408.122849, 403.411759 | 378.836 [378.155-379.905] | 406.294 [403.412-408.123] | 7.25% |
| 512/16 | 217.522763, 218.345691, 217.707033, 218.105707 | 218.109043, 218.337393, 218.742348, 217.107305 | 217.906 [217.523-218.346] | 218.223 [217.107-218.742] | 0.15% |
| 512/32 | 268.572454, 269.989320, 268.552498, 268.685861 | 269.197087, 270.048263, 269.311237, 268.792108 | 268.629 [268.552-269.989] | 269.254 [268.792-270.048] | 0.23% |

### Persisted CLI confirmation: original → scalar

All eight runs exited successfully after graceful cancellation. Normal production storage wrote 55 baseline and 50 candidate match directories in total; each run's directory count equals its final Saved counter. Different saved counts reflect normal independent random entropy. Every run includes the complete search, synchronous match handling, reseeding, progress reporting and disk persistence.

| Pair | Version | Exact checked | Saved | Reported elapsed | Reported keys/s |
| ---: | --- | ---: | ---: | ---: | ---: |
| 0 | original | 3726819328 | 18 | 10.020 s | 371920474 |
| 0 | scalar | 4051836416 | 10 | 10.020 s | 404364637 |
| 1 | original | 3744892416 | 15 | 10.008 s | 374195536 |
| 1 | scalar | 4081601024 | 19 | 10.010 s | 407738981 |
| 2 | original | 3736879616 | 10 | 10.022 s | 372852504 |
| 2 | scalar | 4058835456 | 9 | 10.018 s | 405139082 |
| 3 | original | 3748519424 | 12 | 10.011 s | 374442056 |
| 3 | scalar | 4077923840 | 12 | 10.024 s | 406796002 |

Median throughput is **373.524 → 405.968 Mkeys/s (+8.69%)**. Full ranges are 371.920-374.442 and 404.365-407.739 Mkeys/s respectively. This independent persisted confirmation supports retaining the scalar specialization; it is not substituted for the controlled deterministic benchmarks.
