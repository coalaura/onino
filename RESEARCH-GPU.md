# Resident Vulkan Search and Bounded Automatic Configuration

## Abstract

onino combines independent CPU search workers with a resident Vulkan prefix-search engine. This study optimizes complete search, including submission, accounting, verification, persistence and cancellation, rather than isolated field arithmetic. It replaces repeated full command uploads and command-buffer recording with dirty updates and reusable submissions, moves result allocation to device-local memory, bounds readback and separates preparation, packed root inversion and reconstruction into GPU-resident passes. Automatic configuration searches a small stream/round neighborhood while the requested CPU workers perform useful search. Calibration preserves matches, generations and completed counts.

On a Ryzen 9 9950X3D and RTX 5090, ten alternating comparisons measure **4.044 billion combined keys/s in automatic mode versus 4.099 billion for the best manually tested configuration**. GPU-only automatic mode measures 2.717 billion versus 2.786 billion keys/s manually. The respective gaps are 1.34% and 2.49%: inside the tuner's 2.5% practical indifference band, but not statistically indistinguishable in these repeated measurements. Warm initial selection takes approximately 4.4 seconds GPU-only and 5.1-5.5 seconds with all CPU workers; useful GPU work starts much earlier. These are bounded-search results on one device, not a global-optimum or universal deadline guarantee.

The most important shader change is packing independent inversions into adjacent invocations instead of leaving one active lane per stream during inversion. At identical 16,384-stream/four-round settings, complete GPU search rises from 788 to 2,788 million keys/s. Persistent scratch and extra GPU passes are included in that comparison. Ordinary no-hit submissions upload no commands, copy back 6,160 bounded bytes and allocate no Go memory on the tested submit/collect path. GPU drainage is short in rare-prefix runs, but occasional uninterruptible driver destruction calls take over a second. Frequent matches can legitimately take longer to save after submission stops.

**Navigation:** [Scope](#1-objective-and-experimental-scope) . [Invariants](#2-mathematical-and-counting-invariants) . [Resident execution](#3-retained-resident-execution) . [Automatic selection](#4-automatic-selection-during-useful-search) . [Measurement and baselines](#5-measurement-method-and-baseline-attribution) . [Component experiments](#6-component-experiments) . [Automatic results](#7-automatic-configuration-results) . [Verification and reproducibility](#8-correctness-compatibility-and-reproducibility) . [Conclusions](#9-conclusions-and-limitations)

## 1. Objective and experimental scope

The configuration `onino --cpu all --gpu auto helloworld.` combines all available CPU search workers with a GPU. Device selection and workload calibration are distinct: `--gpu auto` prefers a capable discrete device, while omitted stream/round settings are calibrated independently as described in [Section 4](#4-automatic-selection-during-useful-search). Ordinary builds require neither CGO nor Vulkan and GPU-enabled builds default to `--gpu off`. GPU-only disables CPU search workers; orchestration, verification and persistence still require host CPU work.

This study begins from `a87def5300bd8bd99061bd2f206fd86486ea6446`, preserving its already-optimized monolithic shader, fixed 256-stream/four-round default and original host path as the baseline. The previously reported approximately 637 million GPU-only keys/s at 4096/2 and approximately 1.9 billion combined keys/s at 16384/4 were reproduction targets, not assumed baselines; the real-CLI screens in [Section 5.2](#52-reproducing-the-original-behavior) reproduced both. Earlier work had already selected shared tree/jump inversion and bounded carries; those arithmetic changes are retained rather than repeated. [RESEARCH.md](RESEARCH.md) describes the CPU engine and its separate measurement history.

GPU plans accept one to eight literal anchored lowercase base32 prefixes of one to 51 characters followed by a dot. Only the first twelve characters participate in the GPU necessary-condition filter; the complete original matcher independently verifies a reported key. Longer prefixes therefore retain their meaning. Overlapping prefixes and duplicate necessary-condition filters must not be counted as independent acceptance probabilities when sizing the workload. Current CLI input may be positional or loaded with `--patterns`, but not both; these form, length and count limits also apply to files and combined CPU+GPU search. The [README](README.md#build-and-use) documents file syntax and all current options.

The runtime capability floor is Vulkan 1.3, shader 64-bit integer arithmetic, compute timestamps, 64-invocation workgroups, sufficient shared/storage limits, device-local storage and host-visible staging. Coherent host memory is preferred but not required. Hardware limits constrain allocation and valid dispatches; they do not identify the fastest configuration. Runtime performance and validation in this study are limited to the RTX 5090 on Windows. Cross-compilation is a separate compatibility check.

## 2. Mathematical and counting invariants

### 2.1 Independent streams and candidate order

Each stream starts from independent cryptographic entropy. The host expands and clamps an Ed25519 secret scalar, retains private scalar and nonce material and uploads only an affine public center. If the scalar is \(s\), the initial center is \((s+8\cdot64)B\). Sixty-four lanes use offsets \(Q_j=8jB\), \(1\leq j\leq64\), evaluating both signs around that center. A center at step \(k\) covers \(k-64\) through \(k+64\), excluding \(k\); advancing by \(129\cdot8B\) gives disjoint successive intervals. CPU workers have independent seeds.

Logical order is lane zero's positive candidate, its negative sibling, then the pair from each successive lane. A resident cursor records the first unconsumed logical position. A potential match pauses the stream without advancing its center. If full host verification rejects the necessary-condition match, the acknowledgement resumes at the next cursor position. Recomputed arithmetic is not recounted. A successfully exported key retires that seed, preserving the existing one-export-per-seed guarantee.

The host reserves the entire 32-bit step budget before accepting a seed, rejecting a starting scalar that could cross the permitted scalar boundary. The shader requests replacement before advancement would overflow. Generation checks prevent stale acknowledgements or results from affecting a replacement seed.

### 2.2 Paired affine generation and shared inversion

Work is over \(\mathbb F_p\), \(p=2^{255}-19\), with Edwards parameter \(d=-121665/121666\). For center \(C\) and offset \(Q\), define \(a=x_Cx_Q\), \(b=y_Cy_Q\) and \(c=d x_Cy_Cx_Qy_Q\). The paired ordinates are

\[
y_+=\frac{b+a}{1-c},\qquad y_-=\frac{b-a}{1+c}.
\]

One reciprocal serves both candidates: if \(r=(1-c^2)^{-1}\) and \(t=cr\), then \(r+t=(1-c)^{-1}\) and \(r-t=(1+c)^{-1}\). A 64-leaf product tree needs 63 upward and 126 downward multiplications. If its root is \(P\) and the center-jump denominator is \(J\), one inversion recovers both reciprocals:

\[
u=(PJ)^{-1},\qquad P^{-1}=Ju,\qquad J^{-1}=Pu.
\]

The downward tree recovers candidate reciprocals; otherwise unused node zero retains the jump reciprocal for center advancement. The complete Edwards addition law makes these denominators nonzero for the valid points used here. Reciprocals are not reused across a changed center or generation.

### 2.3 Field bounds and canonical filtering

Field elements use ten unsigned limbs of alternating 26- and 25-bit widths. Let \(r_i=2^{26-(i\bmod2)}\). Tight limbs satisfy \(0\leq a_i<r_i\); supported lazy limbs satisfy \(0\leq a_i<3r_i\). Tight representation is not necessarily canonical. Addition of tight inputs stays below \(2r_i\); subtraction adds \(2p\) first and stays nonnegative and below \(3r_i\). Multiplication and dedicated squaring accept lazy inputs and restore tight output. Their largest unreduced accumulator is 5,046,283,313,212,293,387 and incoming carries remain below \(2^{63}\).

Reduction uses a 64-bit ripple/top fold, an additional low-limb carry before narrowing and two 32-bit normalization passes. After the first fold limb zero is below \(2^{37}\); carrying it leaves limb one below \(r_1+2048\), so narrowing loses no bits. For general lazy normalization, the first pass leaves only limb zero potentially loose, below \(r_0+57\); a second wrap can occur only after its low-limb carry, leaving sufficient room for the final addition of nineteen. Arbitrary chains of lazy additions are outside the contract. A fixed inversion chain uses 254 squarings and eleven multiplications.

Canonicalization precedes every prefix decision. Filtering an equivalent noncanonical representation could cause a false negative that host verification cannot recover. The GPU tests masks over the first 60 encoded bits. Host verification reconstructs the scalar offset with independent Ed25519 scalar-base multiplication, compares the reported 64-bit ordinate fragment, applies the complete matcher and saves only independently valid keys.

### 2.4 Exact completed work

A no-hit round accounts for exactly 128 candidates per active stream. Thus **4096/16, 8192/8 and 16384/4 each account for 8,388,608 candidates per submission**, not different amounts of work. With a hit, the increment is `min(winner + 1, 128) - cursor`. Physically computed later candidates are speculative and excluded. Paused streams contribute zero until resumed; collection-only work contributes zero.

The retained shader accumulates each stream's completed count across all rounds of the submitted sequence and adds it to the device result counter once, at the last reconstruction pass. Every first preparation resets that submission's accumulator. Counts are published to the host only after fence completion and added exactly once, including calibration and cancellation drainage. Dispatch dimensions are never substituted for observed counts. The maximum supported no-hit sequence, 16384/64, fits the 32-bit device counter at 134,217,728 candidates.

## 3. Retained resident execution

### 3.1 Three passes per round

The monolithic predecessor performs root inversion in lane zero while the other lanes wait. The retained sequence separates:

1. **Preparation:** one 64-lane workgroup per stream builds the denominator tree and stores its nodes and jump denominator in device-local scratch.
2. **Packed inversion:** adjacent invocations invert independent stream roots. The dispatch has `ceil(active streams / 64)` workgroups and bounds its final partial group. It skips paused streams.
3. **Reconstruction/filtering:** a 64-lane workgroup reloads the tree, reconstructs reciprocals, recomputes the inexpensive numerator intermediates, filters candidates and advances or pauses the stream.

All rounds and passes are recorded into one command buffer with compute-write to compute-read/write barriers between passes. Only the last reconstruction publishes results and the submission's checked count. Diagnostic full-point data also persists in scratch when an early-round hit must survive until the last pass.

Scratch contains 128 field elements per stream. The complete stream stride is 5,276 bytes; 16,384 streams allocate 86,441,984 bytes. Numerators and mixed products are recomputed rather than stored, a choice evaluated in [Section 6.2](#62-rejected-shader-alternatives). Complete-search comparisons include scratch traffic, barriers, repeated-round work, counters and host coordination.

### 3.2 Buffers, dirty updates and reuse

Public centers, generations, cursors, scratch, offset/filter tables, commands, allocation counters and result records are device-local. Two bounded slots retain command buffers, descriptor sets, fences, timestamp positions and mapped upload/readback storage. Go memory is copied synchronously at the C boundary and is never retained by Vulkan. Command buffers are reused when active streams, rounds and collection mode match and no dirty update must be recorded.

New seeds and verification acknowledgements mark a bounded dirty command range. Only that range is copied and cleared; holes inside the range are harmless zero-action commands. Ordinary no-hit submissions copy **zero command bytes**. Acknowledgements for temporarily inactive streams remain dirty until those streams reactivate. Configuration changes drain prior submissions and undelivered overflow before changing the active range; stream state, cursor and generation are retained rather than reset or replayed.

Default result capacity is `min(allocated streams, 256)`. Each submission copies a fixed 16-byte header and that bounded array of 24-byte records to mapped readback in the same command buffer: at the usual capacity this is **6,160 bytes**. A second CPU round trip to discover the record count is unnecessary. [Section 6.1](#61-repeated-complete-search-comparisons) evaluates this bounded append array against full-capacity readback.

Memory types are selected by properties and actual allocation properties are available with `--gpu-diagnostics`. Host-visible does not imply system memory. On this discrete GPU the selected staging type is host-visible/coherent system-heap memory; device-local buffers use the device heap. On unified-memory devices those properties can overlap. Non-coherent fallback flushes uploads and invalidates readback after fence completion using whole mapped allocations, avoiding atom-alignment mistakes. The same generic path supports such layouts, but it has not been performance-validated on an integrated GPU.

### 3.3 Overflow, verification and cancellation

A potential match remains in its stream until delivered. A full result array increments pending-overflow accounting and leaves that stream paused; it does not discard the record. Delivered streams await an acknowledgement and cannot publish twice. The verifier checks generation and record integrity independently, resumes false positives and reseeds only after successful save. Verification/save channels are bounded by allocated streams. CPU and GPU save callbacks share the existing serialized persistence adapter.

Cancellation is checked before every useful submission. The controller drains accepted submissions and then performs collection-only submissions until resident overflow is delivered, without applying acknowledgements during drainage. The verifier finishes accepted matches before return. A save failure cancels search and remains an error; device loss, process termination and failed storage cannot promise lossless persistence. Slow successful storage is allowed to delay final shutdown rather than discard accepted keys.

Two submissions are the maximum in flight. The first observation of each trial runs alone; overlap is enabled only when the observed sequence tail leaves room inside a 120 ms flight budget. Normal search reduces an automatic dimension when its observed tail exceeds 60 ms or verification backlog becomes large. Explicit settings are rejected with an explanatory error instead of silently changed. A first validation sequence exceeding 150 ms is rejected. These are conservative engineering policies based on observations, not portable watchdog guarantees: a first driver execution, sudden slowdown or uninterruptible driver call can exceed a prediction. Fence timeouts do not cancel work and the implementation does not pretend otherwise.

## 4. Automatic selection during useful search

The ten-second startup budget begins at search-action entry, after CLI argument parsing but before input preparation and backend initialization, rather than after device creation. CPU workers start while Vulkan initializes. GPU work begins after the device and an initial stream set are ready. Output distinguishes first useful GPU submission from final initial selection. Pipeline compilation and other blocking driver calls cannot be interrupted by the tuning deadline.

With stream count automatic, the initial probe uses at most 256 streams and one round; an explicit stream count instead fixes the initial size. Automatic streams grow conservatively by at most a factor of two, subject to measured tail bounds, acceptance/backlog and remaining startup time. Device allocation is bounded by workgroup limits, storage-buffer range and a conservative 1/64 share of the smallest applicable memory heap using an 8,192-byte per-stream allowance. This is a heap-capacity bound, not a query of currently free driver memory. Allocation failure remains an explicit error.

Stream count supplies independent work and controls resident state. Rounds separately amortize submission overhead and increase sequence duration. After growth, the small candidate set tests round counts targeting approximately 2, 5, 10 and 20 ms, nearby half/quarter stream counts and established successful 4096/2, 8192/8 and 16384/4 settings when supported. It does not exhaustively cross streams and rounds. No model-name or RTX-specific rule is used.

Short growth probes use approximately 50 ms; comparisons use approximately 300 ms plus bounded drainage. The requested CPU workers remain active. Scores are completed CPU-plus-GPU candidate deltas over identical monotonic wall-clock windows, naturally reducing to GPU throughput with CPU search disabled. GPU publication happens on each completed collection; CPU publication retains its existing batched 128-batch mechanism. The current five-second progress clock is independent of tuning; the historical measurement windows are defined below. There is no per-candidate synchronization or extra readback for reporting.

The two leading configurations receive a return-to-runner, repeat-winner, return-to-runner comparison. The required improvement is at least 2.5%, increased by observed before/after drift. Effective ties favor substantially shorter tails and then fewer streams. Exploration reserves time for repetitions and transition; if initialization or growth consumes that budget, the best validated probe is retained, not simply the last attempted size. No tuning cache is required.

Filter acceptance is the union of actual GPU masks: covered/duplicate prefixes are removed and long prefixes use the twelve-character GPU limit for this estimate. Frequent acceptance forces automatic rounds to one; observed hits, checked counts and verification backlog constrain growth. A paused dispatch is not evidence that all requested rounds fit: tail extrapolation conservatively allows for the work hidden by early pauses. During useful search, slow execution or growing backlog can reduce automatic rounds and then streams without changing the CPU worker count.

Each trial is real search using the same device, pipeline, allocations and resident states. Completed calibration counts remain in final totals, matches are processed immediately and retained cursors prevent replay. `--gpu-streams` or `--gpu-rounds` fixes only that dimension. Both fixed values still undergo conservative round validation, but no timed performance exploration. Manual configurations that cannot satisfy the observed responsiveness policy fail clearly.

## 5. Measurement method and baseline attribution

### 5.1 Controls and boundaries

Measurements use Windows/amd64, Ryzen 9 9950X3D, RTX 5090, NVIDIA 616.64, WDDM and an active display. Go/PACE report Go 1.27.1. Performance executables use PACE with builder's `--cgo --dyn --compat --no-gen`, `-pgo=off`, `-tags gpu` and the same amd64-v1 target. Stock Go is separately checked for compatibility. Shader compilation uses Vulkan SDK 1.4.328.1, `glslc --target-env=vulkan1.3 -O` and `spirv-val`. Builds, tests, profiles and timed runs execute sequentially. Vulkan validation is enabled for correctness and disabled for timing.

The real CLI runs in a separate console process group with affinity to all 32 logical processors. CPU-only, GPU-only, exactly one application-pinned CPU worker and all requested CPU workers are measured. The normal application `GOMAXPROCS` policy is unchanged: workers plus two in GPU mode. No worker reservation, affinity reinterpretation or CPU arithmetic change is adopted. The rare workload is `chopperepic.` with normal filesystem persistence; no matches occurred in its timing runs.

The historical CLI reported progress every four seconds. Primary comparisons use ten alternating baseline/candidate pairs. Each process runs sixteen seconds; the run statistic averages completed four-second recent windows after the first eight seconds. Short screens use twelve to twenty seconds and are labeled separately. This is a fixed warmed interval, not the older CPU paper's adaptive long warm-up gate. Selection normally finishes before the measured windows. GPU-only automatic runs show some continuing driver/startup variation, retained in their wider distribution. Development measurements are separate from the end-user startup budget. The current five-second reporter does not change these recorded endpoints, denominators or results.

Tables with uncertainty report a mean and nominal 95% Student-t interval half-width across ten run statistics, using nine degrees of freedom. These intervals are descriptive: serial system state can correlate runs and they are not paired-ratio confidence intervals. A one-second opt-in diagnostic separately exposes cumulative CPU and GPU completed counts, execution/gap times, submission/record/copy costs, explicit copy sizes, filter hits, overflow and verification backlog. Backend rates in attribution tables use the same diagnostic endpoints rather than adding separately timed rates.

Process CPU cost uses Windows user-plus-kernel process time divided by wall time. It includes initialization, runtime and process-attributed driver activity; it is not an isolated controller-thread measurement. CPU profiling attributes stacks, not reliable CPU occupancy while inside a blocking Windows CGO call. Explicit `vkCmdCopyBuffer` payloads, logical host memcpy bytes and logical consumed result bytes are reported separately. Hardware PCIe counters were unavailable, so none is labeled measured PCIe bus traffic.

The rate labels distinguish the following boundaries; device-only and backend-active rates must not be added to independently timed CPU rates:

| Rate | Numerator and timing boundary |
| --- | --- |
| CPU, GPU and combined recent throughput | Completed CPU, GPU or summed candidate deltas over the same wall-clock endpoints. Combined mode includes both search backends; GPU-only disables CPU search workers but retains host processing. |
| Device-only (`device_mps`) | Completed GPU candidates divided by summed GPU timestamp execution time for submitted sequences, including their transfers; excludes host gaps, initialization and teardown. |
| Backend-active (`backend_active_mps`) | Completed GPU candidates divided by controller wall time, starting after device initialization and ending after accepted GPU/verification/save drainage. Includes calibration and host gaps, excludes driver teardown. |
| Current final overall averages | Each backend's exact count or their sum, divided by the same full-run wall time from search-action entry through input preparation, initialization, calibration, search, accepted-save drainage and backend teardown. |

### 5.2 Reproducing the original behavior

These initial twenty-second CLI screens are single runs, not ten-pair estimates. Rates are millions of completed keys/s from warmed recent windows.

| Original implementation/mode | Streams/rounds | Combined Mkeys/s |
| --- | ---: | ---: |
| CPU only, all workers | - | 1,284.681 |
| GPU only original default | 256/4 | 273.496 |
| GPU + one pinned CPU original default | 256/4 | 338.988 |
| GPU + all CPU workers original default | 256/4 | 1,321.621 |
| GPU only, historical target | 4096/2 | 637.975 |
| GPU + all CPU workers | 8192/8 | 1,942.157 |
| GPU + all CPU workers | 16384/4 | 1,945.642 |

Instrumentation explains why GPU-only settings do not transfer directly to all-core operation. At original 4096/2, GPU-only search delivered 634.18 Mkeys/s with 1.644 ms execution and 0.009 ms mean gaps. With all CPU workers, GPU contribution fell to 230.03 Mkeys/s while CPU contribution remained 1,306.23 Mkeys/s; gaps averaged 2.928 ms. Measured command-recording wall time increased from approximately 17.7 microseconds to 998 microseconds per submission, including scheduling delay. At 16384/4, the original all-core window delivered 1,277.95 CPU plus 627.47 GPU Mkeys/s, with 11.997 ms execution and 1.393 ms gaps. This attributes the short-batch loss to coordination/scheduling as well as shader sizing, without assuming a particular core reservation would help.

### 5.3 Equal-work stream experiment

Every setting below completes 8,388,608 candidates per no-hit submission. These are brief screens; they distinguish useful independent-stream parallelism from simply multiplying submitted work.

| Streams/rounds | Original GPU-only Mkeys/s | Retained GPU-only Mkeys/s | Retained all-core combined Mkeys/s |
| ---: | ---: | ---: | ---: |
| 4096/16 | 621.478 | 2,047.070 | 3,038.843 |
| 8192/8 | 667.586 | 2,527.541 | 3,388.115 |
| 16384/4 | 691.359 | 2,793.359 | 3,592.589 |

Larger independent stream counts improve throughput at identical work size. More rounds can then amortize remaining coordination: the retained 16384/16 configuration reaches approximately 4.10 billion combined keys/s even though GPU-only throughput is already near its plateau at four rounds. Automatic selection must therefore be revalidated after the packed-pass shader change; the monolithic shader's successful four-round combined setting is no longer sufficient.

## 6. Component experiments

### 6.1 Repeated complete-search comparisons

To separate host coordination from shader execution gains, each increment is compared with its immediate control before evaluating the combined design. All rates below are Mkeys/s. Each row has ten alternating pairs for each listed mode; these are complete-search comparisons under the controls in Section 5, not isolated kernel rates.

| Change and settings | GPU-only control | GPU-only candidate | All-core combined control | All-core combined candidate |
| --- | ---: | ---: | ---: | ---: |
| Resident host path original shader, 16384/4 | 691.97 +/- 1.34 | 699.51 +/- 1.38 | 1,987.95 +/- 7.41 | 2,012.00 +/- 4.98 |
| One checked atomic per monolithic workgroup, 16384/4 | 698.81 +/- 1.58 | 787.60 +/- 1.43 | 2,007.32 +/- 6.62 | 2,098.83 +/- 8.17 |
| Packed passes versus monolithic local count, 16384/4 | 788.03 +/- 1.13 | 2,788.31 +/- 4.60 | 2,094.60 +/- 8.67 | 3,514.31 +/- 63.75 |
| Accumulate across packed rounds, 16384/16 | 2,747.32 +/- 5.31 | 2,799.30 +/- 2.55 | 4,053.81 +/- 8.85 | 4,103.84 +/- 9.49 |

The retained candidate wins every pair in these comparisons. The packed-pass improvement is not an isolated inversion benchmark. The counter improvement is small after packing but repeatable; per-stream accumulation plus one final atomic is retained instead of an extra reduction pass.

Independent host ablations on the packed implementation clarify the bundle. Ten GPU-only pairs at 16384/16 give 2,737.36 +/- 2.91 with host-visible command/results versus 2,804.40 +/- 2.93 with device-local command/results, leaving the same explicit staging copies in both variants. Full-capacity readback gives 2,802.79 +/- 1.85 versus bounded readback's 2,805.39 +/- 3.03: essentially tied throughput, but 393,232 versus 6,160 explicit bytes per submission, a 63.8-fold payload difference. The simpler bounded copy is retained without an additional scan/size-discovery protocol.

Always re-recording and always uploading full commands were separately screened. At 16384/16 their all-core rates were approximately 4,096 and 4,064 Mkeys/s versus corresponding retained controls of 4,107 and 4,112. Their warmed GPU-only process costs could approach 0.9-1.0 core instead of approximately 0.02-0.04. Attempts at ten-pair confirmation repeatedly stopped on the explicit responsiveness guard after unexpectedly long observed submissions; some also had long driver teardown. Those incomplete series are not presented as ten successful pairs. They reinforce the lifecycle/host-cost case for reuse, while the complete host bundle has its own successful ten-pair confirmation. Dirty uploads and reuse interact: a changed upload range necessarily changes recorded commands.

### 6.2 Rejected shader alternatives

The alternatives below test whether shorter value lifetimes, different counter reduction or greater reuse of intermediates improve complete search. These are short screens, not ten-pair precision claims; the losing prototypes are not part of the retained architecture.

| Prototype | Matched GPU-only control → candidate Mkeys/s | Decision |
| --- | ---: | --- |
| Move numerator products after inversion, monolithic 16384/4 | 786.5 → 592.5 | Reject shorter source lifetimes; complete search loses about 25%. |
| Generate/filter plus and minus sequentially, monolithic 16384/4 | 789.7 → 642.8 | Reject; approximately 19% slower. |
| Store per-workgroup counts and perform a separate linear GPU reduction | 785.4 → 592.5 | Reject this simple reduction prototype; no claim that every parallel reduction loses. |
| Store numerator/mixed intermediates across packed passes, 16384/16 | 2,747.1 → 2,516.7 | Reject extra scratch/traffic; selective recomputation wins. |

The stored-intermediate variant adds three 64-field arrays, increasing stream stride from 5,276 to 12,952 bytes. Its loss favors recomputation despite the extra arithmetic. A separate publication prototype emitted results after every pass, repeatedly counting persistent overflow; synchronization/state-machine tests caught this error. The retained sequence publishes only at the last reconstruction pass.

The existing product-tree layout is retained. Static register information and a source-visible inversion-lane imbalance justified the packed-root experiment, but runtime bank-conflict, active-lane and barrier counters were unavailable. A transpose, subgroup tree or cooperative field arithmetic is not adopted on an unmeasured occupancy hypothesis. Earlier rejected fixed-index unrolling and explicit high/low 32-bit arithmetic are not repeated: they increased code/register cost and lost complete-search throughput in the preceding study. The current field arithmetic remains the established bounded-limb implementation.

### 6.3 Available profiling and remaining bottleneck

The driver exposes `VK_KHR_pipeline_executable_properties`. The retained packed shader reports 159,520 SPIR-V bytes, 351,104 native bytes, 128 registers/thread, 5,920 shared bytes and zero stack bytes, versus 168 registers and 7,968 shared bytes for the monolithic predecessor. Subgroup size is 32. Its local-memory statistic is an implausible approximately 68.7 billion bytes and cannot establish spills. No internal representations or installed Nsight runtime profiler were available. Actual active lanes, occupancy, spills, instruction dependencies, shared-bank conflicts and barrier/bandwidth stall counters remain unmeasured. Lower registers support, but do not replace, the complete-search result.

The production submit/collect benchmark at 16384/16 reports 12.026 ms/op, **0 B/op and 0 allocs/op**, with initialization/seeding outside the measured loop and exact count assertions inside it. A CPU profile attributes most samples to CGO/fence collection; Windows sampling includes blocking C waits and must not be interpreted as a fully occupied controller core. Independent process-time observations establish the much lower steady controller/runtime cost of reusable manual configurations.

After adequate combined sizing, measured queue gaps become small and the remaining rare-search interval is predominantly the GPU-resident sequence. Storing more intermediates demonstrably makes that sequence slower. Distinguishing arithmetic dependencies from scratch bandwidth or barriers requires better runtime profiling, so none is asserted as the uniquely proven shader bottleneck. For short all-core submissions, coordination gaps remain a measured bottleneck; for frequent hits, verification and actual filesystem persistence are dominant practical constraints.

## 7. Automatic configuration results

### 7.1 Ten alternating automatic/manual pairs

The final comparison asks how closely bounded useful-search calibration approaches the best manually tested settings. It uses the alternating-pair method from Section 5 and evaluates the retained host and packed-pass design together.

| Mode | Automatic Mkeys/s | Best manually tested Mkeys/s | Automatic gap |
| --- | ---: | ---: | ---: |
| GPU only | 2,716.56 +/- 29.31 | 2,785.94 +/- 2.88 at 16384/4 | 2.49% |
| GPU + all requested CPU workers | 4,044.02 +/- 19.04 | 4,099.03 +/- 9.91 at 16384/16 | 1.34% |

GPU-only automatic selection often prefers 16384/1 because the short-tail result is within the practical tie band; four rounds gives a small repeatable throughput advantage. All-core automatic choices generally use 16,384 streams and approximately 13-26 rounds, depending on the observed tail and combined samples. There is no repeatable gap above 5%, but the mean differences are larger than these nominal confidence intervals. Automatic mode approaches the tested plateau with better responsiveness when effectively tied; it does not exactly reproduce the manual maximum. A cold/driver-active GPU-only sample was approximately 6% behind and remains in the reported distribution rather than being silently discarded.

### 7.2 Same-window CPU/GPU attribution

These separate diagnostic runs of the retained design are individual warmed windows, not the repeated means above. Each row's CPU and GPU deltas share precisely the same endpoints. The CPU-only control is a separate recent-window observation.

| Mode | Window seconds | CPU Mkeys/s | GPU Mkeys/s | Combined Mkeys/s |
| --- | ---: | ---: | ---: | ---: |
| CPU only, all workers | four-second reports (historical) | 1,313.746 | - | 1,313.746 |
| GPU only, automatic | 6.002 | 0 | 2,676.187 | 2,676.187 |
| GPU + one pinned CPU, automatic | 7.004 | 63.589 | 2,678.584 | 2,742.173 |
| GPU + all CPU workers, automatic | 7.031 | 1,311.966 | 2,812.562 | 4,124.528 |

The mixed CPU contribution remains approximately the CPU-only control in this screen. No CPU-core reservation is needed to explain or obtain the retained gain. This is not a ten-pair CPU-retention confidence claim.

| Same-window metric | GPU only | GPU + one CPU | GPU + all CPUs |
| --- | ---: | ---: | ---: |
| Submissions/s | 1,276.1 | 1,277.2 | 63.9 |
| Mean full sequence, ms | 0.777 | 0.776 | 15.657 |
| Mean inter-sequence gap, ms | 0.00681 | 0.00667 | 0.00636 |
| Host copy timer, microseconds/submission | 0.049 | 0.054 | 0.129 |
| Recording/reset timer, microseconds/submission | 0.497 | 0.547 | 1.135 |
| Queue-submit timer, microseconds/submission | 13.72 | 14.51 | 42.68 |
| Explicit command upload bytes/s | 0 | 0 | 0 |
| Explicit bounded readback bytes/s | 7,860,808 | 7,867,850 | 393,399 |
| Logical result bytes consumed/s | 20,418 | 20,436 | 1,022 |

Timers include wall-clock scheduling delay and timer overhead, not just exclusive CPU execution. The recording timer includes fence reset even when no command buffer is rebuilt. Across the entire final all-core run, only 45 recordings and 2,162,688 command-upload bytes were needed, including initial seeding and configuration transitions. Original 16384/4 GPU-only submissions performed roughly 179 MB/s of logical full-command memcpy; that number was not a hardware transfer measurement.

### 7.3 Startup, submission tails and cancellation

Warm GPU-only automatic runs typically reach first useful work in approximately 0.13-0.15 seconds and final selection in 4.35-4.70 seconds. Warm all-core first work is approximately 0.30-0.63 seconds, with selection in 5.06-5.49 seconds. First runs of new shader variants observed approximately 1.86-2.13 seconds to useful work and approximately 6.1-6.4 seconds to selection; an earlier variant took 2.54/6.82 seconds. These are observed colder driver/cache runs, **not controlled cache-flushed cold starts**. Initialization diagnostics separately expose total bridge and pipeline-creation time. No universal ten-second hard deadline is claimed around blocking initialization.

Final telemetry runs observed maximum full submitted-sequence times of approximately 17.2-18.0 ms, including calibration and transfers. The all-core steady mean was 15.66 ms. The maximum recorded gap was approximately 159 ms during startup/transitions, distinct from a long GPU submission. These are observed maxima, not p99 estimates; no duration histogram was collected. Entire multi-pass sequences, rather than only an inversion pass, feed the responsiveness guard. At most two such sequences can be queued.

Cancellation-to-process-exit is broader than GPU drainage. The final rare runs drained accepted GPU/verification work in approximately 0.5 ms GPU-only, 0.75 ms with one CPU and 11.3 ms with all CPUs; last useful submission timestamps precede drainage. Many controlled manual runs exited roughly 36-84 ms after interruption. Some automatic runs took 1.2-1.55 seconds: opt-in teardown timestamps locate delays in `vkDestroyDevice` or `vkDestroyInstance` after GPU drainage. One earlier uninstrumented run was terminated by the measurement harness's 30-second shutdown watchdog; its precise cause remains unresolved. It is not counted as a successful comparison and is not explained away as GPU work. No fence timeout or OS watchdog change is used to conceal these limitations.

### 7.4 Frequent hits, long prefixes and actual saves

A one-second GPU-only automatic run with overlapping `a.`, `ab.` and `b.` prefixes was interrupted during calibration. It completed 12,638 candidates and independently saved all 795 accepted matches. First GPU work began at 131 ms; new useful submissions stopped at approximately one second, but actual filesystem persistence took another 1.082 seconds to drain. Early trials reported backlogs of 255 and 511, which constrained growth. Hit handling proceeded during calibration rather than waiting for the then-current four-second progress tick. This is a correctness/backpressure example, not a steady frequent-hit throughput benchmark.

A separate two-second run with a 51-character prefix and another twelve-character prefix completed 4,366,761,984 candidates, no matches and approximately 15.8 ms drainage before teardown. Deterministic tests additionally exercise overlapping masks, the twelve-character acceptance limit, partial counts, frequent hits and near-epoch exhaustion. Diagnostics report delivered filter hits, undelivered overflow and verifier queue depth; the exact instantaneous population of all paused GPU streams is not separately read back. Paused-state semantics are tested rather than inferred from dispatch size.

### 7.5 Allocation and telemetry observations

At 16,384 streams, actual allocations are: 86,441,984 bytes for stream state/scratch, 7,936 for tables, two 2,162,688-byte command buffers, two 6,160-byte result buffers, two 2,162,688-byte upload buffers and two 6,160-byte readback buffers. State/table/commands/results use memory type 1, flags `0x1`, heap 0 of 33,750,515,712 bytes. Upload/readback use type 2, flags `0x6`, heap 1 of 33,113,260,032 bytes. These are actual selected allocation properties on this device, not assumptions about all host-visible memory.

Separate eight-second telemetry snapshots report GPU clocks of 2,895-2,910 MHz, memory clock 14,201 MHz, temperatures 61-65 °C and board power 432-450 W during retained automatic search, with an unchanged 575 W limit and 99-100% reported utilization. CPU-only GPU idle power was approximately 63 W. Windows process peak working sets were approximately 336 MiB GPU-only, 393 MiB with one CPU and 97 MiB with all CPUs; private bytes were 446, 790 and 280 MiB respectively. Driver/cache state makes these process footprints variable. NVIDIA's 1,406-1,648 MiB used-memory readings are device-wide, not per-process resident allocations. CPU package power/temperature and hardware PCIe traffic were unavailable. These snapshots are not sustained thermal or energy-efficiency confidence measurements and utilization is not occupancy.

## 8. Correctness, compatibility and reproducibility

The verification outcomes below are recorded development checkpoints, with their original test counts and platform boundaries. They do not represent new test runs for later CLI/reporting changes. Current build and device-validation commands are maintained in [internal/gpu/BUILD.md](internal/gpu/BUILD.md).

Permanent arithmetic tests compare production field operations with an independent big-integer model, including prime boundaries, noncanonical encodings, maximal lazy limbs, radix boundaries, repeated squaring and product-tree chains. A diagnostic shader compares 260 consecutive complete encoded public keys with independent scalar-base multiplication across center transitions. Filtering tests verify hits and checked counts over complete candidate ranges. Host tests cover all prefix lengths and reject corrupted or stale results.

State-machine tests cover capacity-one overflow, no duplicate delivery, paused/resumed counters, stale acknowledgements, reseeding, step exhaustion and cancellation with 4,096 accepted matching streams. Execution and calibration tests additionally cover exact counts across stream activation/deactivation and cached command reuse, collection-only zero counts, cancellation during calibration with deliberately slow successful saves, explicit tuning overrides, deadline fallback to the best validated probe, noise thresholds, repeated comparisons and injected trial cancellation. Diagnostic shader data survives the separate passes. The execution-path changes preserved existing test assertions.

PACE and stock-Go full GPU-enabled suites passed with Vulkan synchronization validation: 325 tests ran, 322 passed and three were skipped at the final compatibility checkpoint. A subsequent focused deadline-fallback regression brings the GPU package to 21 passing tests, verified under both PACE and stock Go with synchronization validation. Ordinary PACE and stock-Go CGO-disabled suites ran 285 tests, with 284 passing and one skipped. Tests use `-vet=off`; only the custom vet tool is used for vetting. Windows/Linux/Darwin GPU-tagged checks and ordinary checks, including an arm64 ordinary target, passed. Optional Windows race instrumentation failed before tests began because ThreadSanitizer could not allocate its required region; race validation is unavailable rather than reported as passed.

CPU-only builds passed with `CGO_ENABLED=0` and dependency inspection confirmed no GPU package or `runtime/cgo` in their graph. Stock-Go GPU and CPU builds passed. A GNU-libc Linux GPU cross-build had the expected ELF interpreter; Linux runtime testing was unavailable. Builder's musl-target dynamic cross-build produced an ELF without an interpreter, so the documented compatible GNU target was used instead. The GPU integration required no new or upgraded Go dependencies.

At the recorded checkpoint, production search, full-point diagnostic and epoch SPIR-V were regenerated from the same source with the commands in [internal/gpu/BUILD.md](internal/gpu/BUILD.md), then validated. A second independent generation reproduced all three assets byte for byte and passed `spirv-val`. Arithmetic SPIR-V and its source were unchanged. Builds and tests use committed source/assets and do not depend on temporary profiling or measurement fixtures; building from those assets does not require shader-development tools. Example production build and invocation on Windows:

```powershell
builder build go --pace --cgo --dyn --compat --no-gen --output onino.exe -pgo=off -tags gpu
.\onino.exe --cpu all --gpu auto helloworld.
```

Current reporting uses one coherent selected-setup block, one shared single-line progress report every five seconds and one final CPU/GPU/total breakdown. Setup summarizes pattern count/source, selected backends and workload, startup timing and output directory. Progress uses recent combined completed work and estimates waits from now; final counts include useful calibration and accepted-work drainage and all final averages share the full-run denominator defined in Section 5. Successful-save messages go to stdout with discovery-based intervals and elapsed times; setup, progress, final output and opt-in `--gpu-diagnostics` go to stderr. Diagnostics include allocation, initialization, trial, per-second cumulative accounting and teardown details. Private seeds remain host-side and saved keys remain independently verified.

## 9. Conclusions and limitations

The largest retained gain comes from placing independent inversion roots in adjacent GPU invocations while keeping all passes resident. Dirty command ranges, reusable submissions, device-local result allocation, bounded readback and once-per-sequence checked accounting make that faster shader useful alongside all requested CPU workers. Separating streams from rounds is essential: independent work improves equal-sized batches, while longer, but still short, sequences amortize host scheduling under all-core load.

Automatic mode finds a near-plateau configuration within the observed startup budget and preserves useful calibration work. Its 1.34% combined and 2.49% GPU-only gaps are explicit remaining performance costs, not evidence of exact optimality or statistical equivalence. Frequent-hit storage drainage, driver initialization/destruction outliers, unavailable runtime shader counters, unmeasured instantaneous paused population and lack of cross-vendor runtime validation remain limitations. An earlier integrated-Radeon initialization failure is unresolved; the generic memory-layout path is not a portability result.

Further work should first measure the remaining packed sequence with reliable runtime dependency, barrier and bandwidth counters and investigate the driver-call outliers independently of submission length. The data do not justify another unrolling/radix experiment, an unbounded persistent kernel, additional submission depth, automatic CPU-core reservation or a tuning cache. The retained design favors exact work accounting, bounded resident/backpressure state and short useful trials over an unsupported promise of a global maximum.
