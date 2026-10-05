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
