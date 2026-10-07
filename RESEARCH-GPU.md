# Resident Vulkan Prefix Search for onino

## Abstract

This paper describes onino's resident Vulkan prefix-search backend and its measured optimization on the RTX 5090. Public elliptic-curve centers, offset tables, candidate positions and prefix filters remain on the device; only new public centers and compact potential-match records cross the host boundary. Private key material remains on the host, where each potential match is independently reconstructed and verified before persistence. GPU streams and CPU search workers advance independently.

On an NVIDIA RTX 5090 with a Ryzen 9 9950X3D host, sharing the candidate and center-jump inversion and introducing bounded operation-specific carries raise rare-prefix throughput from 168.97 to 271.95 million logically checked candidates per second at identical production dispatch settings. Ten alternating baseline/candidate pairs after sustained warm-up show a 60.9% median paired improvement. Three-prefix and 51-character-prefix workloads gain similarly; frequent-hit throughput remains approximately 1.64 million candidates per second. With exactly one pinned CPU worker, rare-prefix combined throughput rises from 232.89 to 330.19 million candidates per second. Fixed-index expansion and an explicit high/low 32-bit prototype lose complete-search performance and are not retained. A bounded dispatch sweep identifies further rare-prefix gains but also a frequent-hit and cancellation tradeoff, so the default remains 256 streams and four rounds.

## 1. Motivation and scope

The CPU engine described in RESEARCH.md already amortizes expensive curve operations over paired affine candidates. Moving isolated multiplications or individual candidate encodings to an accelerator would retain much of the host orchestration cost while introducing transfers and synchronization at precisely the wrong granularity. A useful first GPU backend must instead own a substantial, independently advancing search interval.

The objective here is correctness and measurable additional throughput alongside one CPU worker. The implementation uses one selected Vulkan device and one compute queue, with no vendor-specific shader instructions. Vulkan 1.3, shader 64-bit integer arithmetic, compute timestamps, suitable storage memory and explicit workgroup limits define the capability floor. This is a deliberately conservative portability contract rather than a claim that every conforming driver has been tested.

GPU builds accept one to eight anchored lowercase base32 prefixes of one to 51 characters. Suffix, interior, wildcard and other search plans are rejected explicitly. The restriction excludes the 52nd visible character, whose interpretation reaches beyond the simple prefix portion handled here. Prefixes longer than twelve characters use the same GPU necessary-condition filter and exact host-side matching; they are not truncated semantically.

## 2. Mathematical construction

### 2.1 Independent streams and disjoint intervals

Each stream starts from independent cryptographic entropy. The host expands and clamps an Ed25519 secret scalar, retains its private scalar and nonce material and uploads only an affine public center. Let the initial scalar be \(s\), let \(B\) be the Ed25519 base point and measure offsets in units of \(8B\). The initial center is \((s+8\cdot64)B\). Sixty-four lanes use offsets \(Q_j=8jB\), for \(1\leq j\leq64\) and evaluate both the positive and negative candidate around the center.

For a center at step \(k\), this covers steps \(k-64\) through \(k+64\), excluding \(k\) itself. Advancing the center by \(129\cdot8B\) makes successive intervals disjoint. There is no need to coordinate scalar ranges with CPU workers because their seeds are independent. The intentionally skipped center is a small, fixed coverage cost, not a repeated candidate or a gap in the accepted-match protocol.

Within a range, logical candidate order is lane zero's positive candidate, its negative sibling, then the corresponding pair from each successive lane. A resident cursor identifies the first candidate not yet accounted for. This ordering matters when a necessary-condition match fails complete host verification: resuming at the next logical position preserves the sibling and all later candidates without recounting earlier work.

The host reserves the entire 32-bit step budget before accepting a seed, rejecting a starting scalar if the largest permitted increment could cross the scalar's high-bit boundary. The device requests a replacement before range advancement could overflow that budget. Generation numbers distinguish successive seeds occupying the same stream slot.

### 2.2 Paired affine generation

Work is performed over \(\mathbb F_p\), where \(p=2^{255}-19\), on the twisted Edwards curve with \(d=-121665/121666\). For affine center \(C\) and offset \(Q\), define

\[
a=x_Cx_Q,\qquad b=y_Cy_Q,\qquad c=d\,x_Cy_Cx_Qy_Q.
\]

The paired ordinates are

\[
y_+=\frac{b+a}{1-c},\qquad y_-=\frac{b-a}{1+c}.
\]

One reciprocal serves both candidates. With \(r=(1-c^2)^{-1}\) and \(t=cr\), the two inverse factors are \(r+t=(1-c)^{-1}\) and \(r-t=(1+c)^{-1}\). The offset table stores the factor involving \(d\), while each resident center stores its ordinary \(xy\) product. The complete Edwards addition law on these valid points avoids exceptional denominators.

The sixty-four denominators in a workgroup are inverted together. A shared-memory product tree performs 63 upward multiplications and 126 downward multiplications. Let its root product be \(P\) and let \(J=1-c_{\mathrm{jump}}^2\) be the analogous denominator for center advancement. Instead of independently inverting the root and jump denominator, lane zero computes

\[
u=(PJ)^{-1},\qquad P^{-1}=Ju,\qquad J^{-1}=Pu.
\]

The recovered root reciprocal feeds the unchanged downward tree. Its otherwise unused node zero holds the jump reciprocal until advancement. This replaces one inversion with three multiplications, retaining the product-tree layout and synchronization. Center advancement uses the same affine identities with the fixed jump point, including the abscissa needed to form the next center. A hit pauses the stream without advancing; resumption recomputes intermediates for that same center and starts accounting at the saved cursor. No reciprocal persists across a center or generation change. The complete addition law makes both the candidate and jump denominators nonzero for the valid points used here.

### 2.3 Field representation and canonical filtering

Field elements use ten unsigned limbs with alternating 26- and 25-bit widths. Multiplication retains schoolbook accumulation in 64-bit integers, including the radix-alignment factors and reduction by \(2^{255}\equiv19\pmod p\). Squaring retains its dedicated symmetric implementation. A fixed addition chain computes inversion with 254 squarings and eleven multiplications. The useful reduction change is to carry only as much as each operation and its consumers require, rather than immediately normalizing every addition and subtraction.

Let \(r_i=2^{26-(i\bmod2)}\). A tight field has \(0\leq a_i<r_i\), while a supported lazy field has \(0\leq a_i<3r_i\). Tight does not mean canonical: the represented integer can still be between \(p\) and \(2^{255}-1\). The operation contracts are:

| Operation | Input bounds | Output bounds and accumulator limit |
| --- | --- | --- |
| Addition | Both tight | Each limb is below \(2r_i\); unsigned 32-bit addition suffices. |
| Subtraction | Both tight | Compute \(a_i+2p_i-b_i\), with \(p_0=r_0-19\), \(p_i=r_i-1\) otherwise. Every limb is nonnegative and below \(3r_i\), without unsigned overflow. |
| Multiplication | Both below \(3r_i\) | Tight output. The largest unreduced accumulator is 5,046,283,313,212,293,387; all accumulators including incoming carries remain below \(2^{63}\). |
| Dedicated square | Below \(3r_i\) | Tight output with the same accumulator bound, including doubled off-diagonal terms. |
| Repeated squares and inverse | Initially below \(3r_i\) | Every square/multiply restores tight bounds. Repetition does not grow the bound. |
| Product-tree multiplication | Lazy leaves, tight internal results | The first level accepts the lazy subtractions; every subsequent upward or downward product is tight. |
| Normalization | Below \(3r_i\) | Two 32-bit ripple/fold passes produce tight limbs. |
| Canonicalization | Tight | Subtract \(p\) at most once; output is in \([0,p)\). |

The accumulator bound follows by replacing each input limb by \(3r_i-1\) in the nonnegative schoolbook sums, with coefficient two for odd/odd radix alignment and nineteen for wraparound. The original three full 64-bit carry passes are replaced by one 64-bit ripple and top fold, one additional low-limb carry before narrowing and two 32-bit normalization passes. After the first ripple/fold, limb zero is below \(2^{37}\). Carrying it into limb one leaves limb zero tight, limb one below \(r_1+2048\) and every other limb tight; narrowing therefore loses no bits. For the general lazy normalization contract, incoming carries are at most three. The first pass leaves only limb zero potentially loose, below \(r_0+57\). A second top wrap can occur only if the low limb carried, leaving it at most 56 before the final addition of nineteen. Thus the second pass leaves every limb tight without a third pass. All intermediate 32-bit values remain below \(3\cdot2^{26}+3\).

Production additions and subtractions consume tight values and feed multiplication or squaring; arbitrary chains of lazy additions are outside the contract. The diagnostic shader normalizes addition/subtraction outputs before canonicalization. Production filtering already consumes tight multiplication results, so it incurs no extra generic normalization.

Canonicalization precedes every prefix decision. Filtering an equivalent but noncanonical field representation could reject an actual match and host verification cannot recover such false negatives. The GPU compares masks over the first twelve base32 characters, a 60-bit necessary condition contained in the first eight bytes of the encoded ordinate. It therefore does not need each candidate's abscissa or sign bit on the ordinary search path. A diagnostic shader calculates the full compressed point, including sign, for differential tests.

The host reconstructs a reported scalar offset using an independent Ed25519 scalar-base multiplication. It checks the reported 64-bit ordinate fragment, evaluates the complete original matcher on the reconstructed public key and saves only a fully verified result. Longer prefixes and any sign-sensitive part of the complete matcher are resolved here. Normal operation consequently does no host work per rejected GPU candidate.

## 3. Resident execution and match preservation

### 3.1 Ownership and submission

The implementation is isolated in internal/gpu and selected by small startup adapters. Without the `gpu` build tag, neither CGO nor the Vulkan package nor embedded shaders participate in the build. GPU-enabled executables still default to `--gpu off`, which retains the CPU-only execution path. The CPU implementation packages do not contain GPU coordination code.

A focused C bridge statically includes pinned volk sources and Vulkan headers. Precompiled SPIR-V is embedded in the executable. Building requires a C compiler but no installed Vulkan SDK; running uses the installed loader and driver. Linux GPU builds use a compatible dynamic libc. The build and shader-generation procedure is specified in internal/gpu/BUILD.md.

The bridge owns all asynchronous memory and Vulkan objects. Go inputs are copied synchronously before a CGO call returns; the driver never retains Go memory. Public stream state and offset/filter tables live in device-local buffers. Two reusable slots hold host-coherent command and result buffers, command buffers, fences and timestamp-query positions. The default dispatch has 256 independent workgroups, 64 lanes per workgroup and four range rounds: up to 131,072 logically checked candidates when no stream pauses.

Submission and collection are coarse operations. The controller fills both slots, blocks on the oldest fence, consumes its compact output and reuses that slot. Queue-ordered barriers protect shared resident state between dispatches. There is no busy polling or steady-state queue-wide or device-wide wait. A device-wide wait is reserved for destruction and failed initialization. Reused CGO output fields and preallocated Go buffers keep the tested steady-state submit/collect path at zero Go allocations; initialization and successful seed replacement are outside that assertion.

### 3.2 Pending hits, acknowledgements and generations

A stream finding a potential match records its scalar step, generation and ordinate fragment, then pauses. It can be active, pending delivery or delivered and awaiting acknowledgement. A result record is six 32-bit words: stream index, generation, step, record kind and the two ordinate words. The record kind also permits an exhausted stream to request reseeding.

The bounded result array is not the authoritative storage for an undelivered match. If the array is full, the stream keeps its pending record and remains paused. Later dispatches retry publication; delivered records are not published twice. Overflow therefore reduces immediate progress but does not discard matches. Completion fences allow collecting a partially filled array immediately rather than waiting for a target hit count.

Host verification either resumes the same generation after a false positive or replaces the seed after a verified, successfully saved match. A replacement increments the generation and commands carry the generation they expect to modify. Stale acknowledgements cannot resume a replacement stream. Host-side generation checks reject stale results and the first exported key retires its seed, preserving the one-export-per-seed rule.

### 3.3 Persistence, cancellation and counters

Verification and saving run on a separate goroutine from Vulkan submission. In mixed operation, an adapter serializes CPU and GPU save callbacks and progress callbacks. The first save error is retained, cancels both engines and prevents further persistence attempts. Successful-save counts are distinct from checked-candidate counts.

Ordinary cancellation stops new search submissions, collects in-flight dispatches and performs collection-only dispatches until every resident pending hit has been delivered. It does not acknowledge or resume paused streams while draining. The verifier finishes accepted work before return. This contract covers cancellation while overflow records are waiting; it does not promise recovery after process termination, device loss or a failing persistence callback.

Checked counts refer to unique logical candidates consumed through the first potential match, inclusive or through the end of a range. A SIMD-style dispatch can physically calculate later ordinates before discovering which stream position wins and a resumed range may recompute intermediates. Those speculative calculations are not counted twice. Under the rare-prefix timing workload there are no such pauses, making the count directly comparable to the CPU candidate rate.

## 4. Correctness methodology

The permanent tests use the production arithmetic and search source, with small diagnostic shader variants where observation requires more information than ordinary compact results expose. Arithmetic vectors compare addition, subtraction, multiplication, squaring and inversion with an independent big-integer model. They include zero, random values and values immediately around the prime, including noncanonical encodings. An additional 1,024-case bounds test covers maximum lazy limbs, each radix boundary, zero/max combinations, 254 repeated squarings, six product-tree-depth self-products and a subtraction/addition product. Its reference sums weighted limbs as integers, rather than bit-packing overlapping lazy limbs. The diagnostic checks the operation-specific output bound before canonicalization and the host checks both tight output limbs and all 32 canonical bytes against the independent reference.

A complete-range test forces every candidate to become observable and compares 260 consecutive full compressed public keys against independent scalar-base multiplication. It crosses two center transitions and verifies the paired ordering, so candidates normally rejected by the prefix filter are covered. A separate three-prefix test compares both hits and checked counts over 384 candidate positions, testing rejection and filtering rather than just the successful path. Host mask tests cover every supported prefix length.

State-machine tests deliberately use result capacity one with several active streams. They check lossless overflow, no duplicate delivery, paused-stream counters, stale acknowledgements, replacement generations and resumption of the paired sibling. An epoch diagnostic starts the step counter near exhaustion to test the control transition without traversing billions of candidates; it does not substitute for a full scalar-arithmetic test at that counter. Host tests reject corrupted records and replayed generations. Cancellation tests drain overflow, propagate save failures and write actual keys through the existing store's independent validity checks.

The eleven GPU-package tests passed on the RTX 5090 under both PACE and stock Go with the Khronos validation layer and synchronization validation enabled. The full GPU-enabled suite passed under both toolchains with `-vet=off`; vetting uses only the custom vet tool. Windows/Linux GPU-tagged checks and Windows/Linux/Darwin ordinary checks passed. CPU-only `CGO_ENABLED=0` builds and tests passed under both toolchains and dependency inspection found no GPU package, CGO source, runtime/cgo or embedded SPIR-V in the ordinary build graph. All four production/diagnostic SPIR-V assets were regenerated with the commands in internal/gpu/BUILD.md, validated with `spirv-val` and reproduced byte for byte on a second generation. Required builds and tests use committed sources and assets, without measurement fixtures. A Linux GPU binary was cross-built with the documented GNU libc target; Linux runtime validation was not performed.

An earlier AMD integrated Radeon portability probe passed arithmetic but exited during ordinary and diagnostic search initialization before reporting an opened device. Its cause remains unresolved and the optimized shader has not received cross-vendor runtime validation. Vendor-neutral source and successful cross-compilation do not establish runtime portability.

## 5. Performance methodology

### 5.1 Baseline and controls

The baseline is commit `7a3e6289dd576d88258e70b83213a2d39e51ad2f`. Its GPU sources, SPIR-V and executable were preserved before modification. Baseline and candidate measurements use identical host code and measurement boundaries, differing only in the embedded production search shader except where a dispatch-setting comparison is explicitly identified. The target is Windows/amd64, Ryzen 9 9950X3D, RTX 5090, NVIDIA driver 616.64, WDDM, with the display active and the reported 575 W power limit unchanged.

Both Go and PACE report Go 1.27.1 windows/amd64. PACE is the timing target; stock Go is a compatibility target. Builds use builder v0.5.1 with `--cgo --pace --dyn --compat --no-gen`, `-pgo=off`, `-tags gpu`, trimpath, buildvcs disabled and stripped binaries, targeting amd64 v1. Shader generation uses Vulkan SDK 1.4.328.1: glslc reports shaderc v2023.8 / v2025.3-10-gc7e73e8, glslang 11.1.0-1302-gd213562e and SPIRV-Tools v2025.4 / v2022.4-970-g19042c89; the validator reports SPIRV-Tools v2025.4-0-g7f2d9ee9. Compilation uses `--target-env=vulkan1.3 -O` throughout.

The measurement procedure invokes `gpu.Run` and `search.RunQueued`, fixes `GOMAXPROCS=3` and process affinity to logical processors 2 through 7 and pins the sole CPU search worker to logical processor 2. The remaining controller/runtime threads have the same allowed scheduling set in every variant. CPU SIMD selection stays automatic. GPU-only comparisons precede mixed comparisons; builds, correctness tests, profiling and timed comparisons are separate sequential activities. Validation is disabled during timing. No all-core or multiple-CPU-search-worker benchmark is used.

The four workloads are a single rare prefix (`somethingrare.`), a three-prefix set (`somethingrare.`, `anotherrare.`, `onionsearch.`), a long prefix (51 `a` characters followed by `.`) and frequent hits (`a.`). Workloads, affinity, build settings, result capacity and candidate accounting match within every comparison. Seeds come from the same production entropy path, not identical deterministic streams; candidate-rate distributions rather than identical keys are compared. The persistence callback is a no-op, but independent host reconstruction, exact matching, stream pausing, acknowledgement and seed replacement remain enabled. Frequent-hit results therefore include the production match protocol but not filesystem persistence latency.

### 5.2 Warm-up, stability and accounting

A one-second warm-up proved insufficient on this machine; those exploratory measurements are excluded from the results below. For each primary ten-pair comparison, both variants in the first pair run at least 30 seconds before measurement. Subsequent sequential runs require at least six seconds and an independently checked plateau. The latest six approximately one-second progress-rate windows must have a full range no greater than 3% of their mean, with the first-three/last-three mean difference no greater than 1.5%. The accepted steady interval is five seconds followed by cancellation and draining. Mixed runs start their pinned CPU worker before GPU warm-up. Alternating baseline/candidate and candidate/baseline ordering reduces systematic order effects; minimum, median and maximum are reported rather than claiming confidence intervals.

Frequent mixed search did not reliably meet a one-second-window stability criterion. Its final comparisons and matched CPU-only controls instead use six ten-second windows, a 5% range limit, a 2.5% half-window drift limit and twenty-second steady intervals. Accepted mixed warm-ups lasted 61-151 seconds. Frequent search at 4,096 streams uses a separately identified, noisier 10% range / 5% drift gate and ten-second intervals. Runs failing their gate are excluded and are not evidence of an improvement. Stability gating does not eliminate between-run scheduling variation, especially in mixed and frequent-hit workloads.

For the historical measurements below, end-to-end GPU throughput divides the post-warm-up checked-count delta by host time through return, including final draining. CPU progress callbacks provide their own count/timestamp pair, so CPU throughput uses that exact CPU count window through worker completion. The GPU and CPU windows overlap but need not start at precisely the same instant; those combined rates sum separately normalized rates. CPU-only controls use the same API, affinity and workload, with ten runs per workload. GPU execution throughput divides all logical checked counts by all compute timestamp intervals, including warm-up. Execution and steady end-to-end rates consequently have different boundaries and are not algebraic complements. Gaps are measured from a dispatch's end timestamp to the next dispatch's start timestamp.

The CLI now uses one coordinator to sample all enabled backends every four seconds. Checked counts and runtime stay cumulative; the live `recent` rate is the combined completed-count delta divided by the actual monotonic elapsed time between snapshots. Each boundary reads the existing batched CPU publications and completed GPU dispatch counts directly, without callback caching, extra GPU synchronization or readbacks. The initial count/time baseline precedes backend setup; startup affects only the first interval. Live 50%/95% waits use this recent combined rate, with zero throughput and unavailable waits when no work completes. Periodic reporting stops on cancellation or backend shutdown, and one final summary uses authoritative totals after accepted work and saves drain. Its `overall avg` divides total checked keys by the full runtime, including backend setup and shutdown. Device-only GPU timestamp throughput remains a separate diagnostic; the historical tables have not been remeasured using the new CLI intervals.

Host CPU cost is the process user-plus-kernel CPU-time delta divided by steady wall time. For GPU-only operation it measures controller, verifier, Go runtime and process-attributed driver cost together, not an isolated controller-thread profile or system-wide driver cost. Logical upload/read counts are not PCIe bus measurements. Before/after telemetry accompanies the comparisons; a separate baseline/candidate run samples NVIDIA clock, power, temperature and utilization every 500 ms during load, without mixing those instrumented runs into the primary performance table.

### 5.3 Available production-shader profiling

The installed driver exposes `VK_KHR_pipeline_executable_properties`; querying the compiled production compute pipeline provides the following static statistics. Native binary size and shared memory are driver reports, not source estimates. The reported subgroup size is 32 for every variant.

| Shader | SPIR-V bytes | Native binary bytes | Registers/thread | Shared bytes | Stack bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| Original baseline | 176,988 | 535,936 | 168 | 6,944 | 0 |
| Shared inversion | 131,664 | 416,896 | 168 | 7,968 | 0 |
| Shared inversion + fixed-index multiply/square | 304,304 | 417,024 | 168 | 7,968 | 0 |
| Retained shared inversion + bounded carries | 157,464 | 350,208 | 168 | 7,968 | 0 |
| Retained + explicit high/low products | 182,936 | 515,968 | 255 | 7,968 | 0 |

The local-memory-size statistic returned implausible values near 68.7 billion bytes even though its format was explicitly `UINT64`; it is unusable for determining spills. The driver returned no internal representations and an installed Nsight runtime profiler was unavailable. Thus local-memory traffic, actual occupancy, native instruction mix and barrier/dependency stall counters were not measured. Source loops and array indices do not prove runtime overhead or spilling. Register and code-size observations support controlled comparisons but cannot by themselves attribute a throughput change to occupancy, instruction-cache pressure or a particular stall.

## 6. Component selection

Rates throughout are millions of logically checked candidates per second. Brackets give the full observed range. Component comparisons use rare-prefix GPU-only search at 256 streams/four rounds, with the warm-up procedure above. A and C are confirmed with ten alternating pairs each; clear losing prototypes use two-pair screens, not ten-pair precision claims.

| Component comparison | Control median [range] | Candidate median [range] | Decision |
| --- | ---: | ---: | --- |
| A: shared inversion versus original | 169.43 [168.53-169.97] | 252.24 [250.78-253.40] | Retain; approximately 48.9% faster. |
| B: fixed-index multiply and dedicated square versus A | 252.90 [252.80-253.00] | 220.22 [214.68-225.76] | Reject expanded arithmetic. |
| B: fixed-index multiply only versus A | 252.38 [251.33-253.44] | 181.69 [154.86-208.52] | Reject isolated multiply expansion. |
| B: fixed-index square only versus A | 252.62 [252.31-252.92] | 190.06 [166.92-213.20] | Reject isolated square expansion. |
| C: lazy add/subtract alone versus A | 252.84 [252.83-252.85] | 222.69 [222.32-223.06] | Insufficient alone. |
| C: split carry alone original add/subtract, versus A | 252.29 [251.48-253.10] | 253.54 [253.53-253.55] | Small screen difference, not an independently established gain. |
| C: lazy add/subtract plus split carry versus A | 252.68 [251.35-252.92] | 272.92 [271.37-273.17] | Retain the useful combination; approximately 8.0% faster. |
| C: immediately normalize add/subtract versus retained lazy version | 272.43 [271.57-273.28] | 260.25 [259.99-260.52] | Retain lazy outputs. |
| D: explicit high/low products versus retained shader | 271.53 [271.28-271.78] | 204.30 [203.91-204.68] | Reject 32-bit product prototype. |

The fixed-index prototype uses constant limb indices and coefficients with one scalar 64-bit accumulator per output limb, preserving the radix and dedicated 55-term symmetric square. Both isolated expansions and the combined variant lose after sustained warm-up. The isolated variants have unusually long cancellation/teardown tails in their first runs (1.66 seconds for multiply, 1.45 seconds for square), included in their end-to-end ranges rather than discarded. Even their timestamp-only execution rates, approximately 214 and 219 respectively, are below the matched A controls' 257-260. Larger SPIR-V did not reduce the reported register count or improve complete search. These experiments compare loop-based and explicit scalar schedules, not every possible partial-unrolling policy.

The bounded-carry result illustrates why useful subcomponents must be evaluated together after isolation: lazy addition alone loses, while lazy addition paired with split reduction wins reliably. Retaining only individually positive screens would discard the winning combination. No unmeasured radix change is needed.

The 32-bit prototype uses `umulExtended` for full high/low products, preserves the high part of coefficient scaling, uses `uaddCarry` plus explicit high-word accumulation and reconstructs a 64-bit value only for reduction. It passes the differential tests, including maximum lazy inputs and repeated chains. Nevertheless, register allocation rises from 168 to 255 and native code grows from 350,208 to 515,968 bytes while complete-search throughput falls. This is evidence against that bounded prototype, not evidence that every explicit 32-bit schedule is inferior. The retained implementation uses 32-bit bounded addition/subtraction and late carries, with 64-bit products and wide reduction where they measured better.

## 7. Complete-search results

### 7.1 GPU only at production defaults

Each row represents ten alternating original/retained pairs. GPU execution values are medians with the broader timestamp boundary described above. Gains are medians of per-pair ratios, rather than ratios of independently rounded table entries.

| Workload | Original end to end [range] | Retained end to end [range] | GPU execution original ? retained | Paired gain [range] |
| --- | ---: | ---: | ---: | ---: |
| Single rare prefix | 168.967 [168.701-169.489] | 271.955 [271.330-272.478] | 171.975 ? 279.020 | 60.87% [60.45-61.29%] |
| Three-prefix set | 168.770 [166.492-169.248] | 271.274 [270.540-272.262] | 171.413 ? 278.104 | 60.78% [60.02-62.54%] |
| Long prefix | 169.431 [168.573-169.574] | 272.832 [271.378-273.063] | 172.131 ? 279.636 | 61.00% [60.95-61.46%] |
| Frequent hits | 1.641 [1.638-1.652] | 1.641 [1.637-1.648] | 1.837 ? 1.821 | -0.10% [-0.58-0.56%] |

The arithmetic gain survives the complete pipeline for rare candidates, including long prefixes that require exact host matching if the necessary condition succeeds. It does not improve frequent-hit throughput. Logical candidate counting stops at the first pending match, so the frequent-hit execution rate is not a raw measure of how many affine ordinates the shader physically computes.

### 7.2 GPU plus exactly one pinned CPU worker

The same ten-pair comparison is repeated with the single pinned worker active throughout warm-up and measurement. Combined columns are medians of each run's sum, not sums of separately rounded medians.

| Workload | GPU end to end original ? retained | GPU execution original ? retained | Original combined [range] | Retained combined [range] |
| --- | ---: | ---: | ---: | ---: |
| Single rare prefix | 169.295 ? 272.130 | 172.062 ? 279.275 | 232.885 [225.001-236.979] | 330.185 [327.560-336.292] |
| Three-prefix set | 169.138 ? 271.999 | 171.942 ? 278.801 | 208.748 [207.684-214.747] | 311.427 [309.607-317.554] |
| Long prefix | 168.477 ? 270.795 | 171.247 ? 277.935 | 232.535 [232.342-237.266] | 334.550 [334.451-340.612] |
| Frequent hits | 1.028 ? 1.025 | 1.214 ? 1.192 | 2.946 [2.622-2.964] | 2.951 [2.644-2.969] |

| Workload | CPU-only control [range] | CPU alongside original [range] | CPU alongside retained [range] | CPU retention original ? retained |
| --- | ---: | ---: | ---: | ---: |
| Single rare prefix | 61.988 [61.802-62.246] | 64.014 [55.778-68.402] | 57.876 [55.575-65.242] | 103.3% ? 93.4% |
| Three-prefix set | 46.331 [45.853-46.570] | 39.604 [39.445-45.431] | 39.363 [39.272-45.180] | 85.5% ? 85.0% |
| Long prefix | 66.835 [66.726-66.978] | 64.048 [63.912-67.831] | 63.815 [63.724-67.930] | 95.8% ? 95.5% |
| Frequent hits | 2.036 [1.914-2.787] | 1.924 [1.515-1.940] | 1.929 [1.542-1.946] | 94.5% ? 94.7% |

Retention is the ratio of mixed and CPU-only medians. The rare workload's original retention exceeding 100%, its wide mixed CPU range and the frequent workload's broad control range show that host scheduling/system state remains material despite fixed affinity and warm-up. The retained rare median is below the original mixed CPU median and the data do not justify attributing that difference solely to shader execution or promising a fixed CPU-retention percentage. GPU rate gains are much more consistent than CPU rates in these mixed runs.

### 7.3 Dispatches, host cost and telemetry

For single-prefix GPU-only search, the median dispatch interval falls from 762.16 to 469.76 microseconds. Average inter-dispatch gaps rise slightly from 9.36 to 10.09 microseconds; their fraction of execution-plus-gap time rises from 1.21% to 2.10% because compute is faster. Mixed rare search has 761.77 ? 469.33 microsecond dispatches and 9.32 ? 10.32 microsecond gaps (1.21% ? 2.15%). Thus the retained rare workload still spends about 98% of timestamped queue time inside dispatches. This is observed timing, not attribution to a particular arithmetic instruction or barrier.

| Workload/mode | Original process core equivalents [range] | Retained process core equivalents [range] |
| --- | ---: | ---: |
| Rare GPU only | 0.512 [0.019-0.523] | 0.527 [0.485-0.641] |
| Rare mixed | 1.448 [0.899-1.521] | 1.410 [0.909-1.537] |
| Three-prefix GPU only | 0.510 [0.501-0.607] | 0.524 [0.047-0.545] |
| Long-prefix GPU only | 0.513 [0.022-0.529] | 0.521 [0.022-0.532] |
| Frequent GPU only | 1.615 [1.421-1.666] | 1.620 [1.119-1.664] |
| Frequent mixed | 2.139 [1.922-2.429] | 2.125 [1.919-2.145] |

The unusually low process-accounting samples are retained in the ranges; process accounting is not a reliable per-thread attribution instrument. The medians show nonzero controller/runtime/driver cost and substantially greater host work for frequent hits. Frequent GPU-only dispatches average 116.52 ? 123.33 microseconds, with 12.91 ? 12.65 microsecond gaps (9.97% ? 9.28%). Streams waiting for verification can be paused inside otherwise short dispatches, so queue-gap percentages alone do not describe their utilization.

At production defaults, the maximum observed cancellation-to-return interval across these GPU-only and mixed tables is 28.03 ms, including pending-hit drainage and final verification with the no-op save callback. This is an observed bound for these workloads, not a deadline guarantee for arbitrary storage or driver delays.

In the separate sampled rare-prefix telemetry check, both shaders were in P0 with a 14,201 MHz reported memory clock and 99-100% device utilization. Sixteen 500 ms samples selected from an eight-second interior steady interval showed 2,940 MHz graphics clock and 174.35 W median board power for the original (174.09-174.75 W, 52\UffffffffC), versus 2,925 MHz and 175.09 W for the retained shader (174.41-175.27 W, 54-55\UffffffffC). These are one instrumented run per variant at default settings, not energy-efficiency confidence intervals or telemetry for the full sweep. Utilization reporting also does not establish shader occupancy.

## 8. Bounded stream/round sweep

The retained shader was screened over the following small settings grid, with three warmed repetitions per setting. Rates and dispatch durations are medians; result capacity follows stream count and the queue retains two slots.

| Streams | Rounds | End-to-end rate | Dispatch milliseconds |
| ---: | ---: | ---: | ---: |
| 128 | 4 | 137.06 | 0.467 |
| 256 | 2 | 267.20 | 0.234 |
| 256 | 4 | 273.07 | 0.469 |
| 256 | 8 | 275.95 | 0.937 |
| 512 | 2 | 423.09 | 0.298 |
| 512 | 4 | 434.42 | 0.591 |
| 1,024 | 4 | 451.15 | 1.148 |
| 1,024 | 8 | 452.57 | 2.296 |
| 2,048 | 2 | 552.55 | 0.936 |
| 2,048 | 4 | 561.65 | 1.851 |
| 4,096 | 2 | 632.17 | 1.646 |
| 4,096 | 4 | 630.72 | 3.313 |

Ten alternating retained-shader pairs confirm 256/four at 272.10 [270.62-273.67] against 4,096/two at 631.87 [628.52-633.93]. Maximum rare-prefix cancellation latency is 22.74 versus 24.69 ms. Increasing rounds at 4,096 streams doubles dispatch duration without raising throughput, so two rounds is the better rare-prefix setting within this grid. This establishes dispatch-parallelism headroom at the defaults, not a measured occupancy explanation.

To separate shader gains from setting gains, ten original/retained pairs also use identical 4,096/two settings for each rare workload: single-prefix medians 334.92 ? 631.73, three-prefix 334.66 ? 630.69 and long-prefix 335.94 ? 630.45. Single-prefix ranges are [333.88-336.47] and [628.88-633.86], with dispatch durations 3.107 ? 1.644 ms. Thus the approximately 89% same-setting shader gain is distinct from the larger ratio obtained by comparing differently configured binaries.

Frequent search does not benefit from that stream count. Under its separately relaxed stability gate, ten pairs give 1.355 [1.348-1.412] ? 1.383 [1.344-1.409], both below the approximately 1.64 at the default stream count. Maximum cancellation latency grows to approximately 108 ms because more streams can hold work requiring verification/drainage. The production default therefore remains 256/four. The larger rare-prefix setting was exercised through existing GPU options, not introduced as a universal CLI default or a new adaptive host policy. Its mixed-CPU behavior was not separately characterized.

## 9. Interpretation and limitations

Two localized changes survive full-search validation and repeated measurement: sharing the tree/jump inversion and combining lazy add/subtract with bounded split reduction. They preserve the independent CPU/GPU architecture, product tree, canonical filtering and resident-stream protocol. Generic 64-bit products and dedicated squaring outperform the tested fixed-index and explicit 32-bit alternatives on this driver. Neither source-level operation counts nor static register statistics alone explain those results.

The next measured rare-prefix limitation is dispatch configuration: 256 streams leave substantial complete-search throughput available at larger stream counts. At the retained default, queue gaps account for only about 2% of timestamped time, making shader/dispatch work the larger measured interval. Within that interval, inversion scheduling, dependencies, barrier stalls, actual occupancy and local-memory traffic remain hypotheses until suitable runtime counters are available. Broader inversion-layout and host-interface redesigns are deferred.

For frequent hits, unchanged end-to-end throughput despite the rare-search arithmetic gain, increased process CPU cost and worsening behavior with many pending streams identify the match-handling path as the next measured regime to investigate. A host profile is needed to distinguish reconstruction, reseeding, queue coordination and scheduling costs. The no-op persistence callback prevents inferring real disk save rates. Faster arithmetic does not remove stream pause/verification constraints.

These results apply to this Windows RTX 5090/driver combination with PACE and controlled single-worker coexistence. They do not establish all-core scaling, stock-Go performance parity, cross-vendor runtime support or Linux execution support. Initialization, shader-cache latency, arbitrary slow persistence and crash/device-loss recovery are outside the throughput claims. Committed shaders, focused tests and documented generation commands remain sufficient for ordinary builds and correctness checks without the temporary measurement machinery.
