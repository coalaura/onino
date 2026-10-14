# Resident Vulkan Search and Bounded Automatic Configuration

**Current implementation and measurements:** [Section 11](#11-reconstruction-and-exact-prefix-projection-2026-10-13) records exact prefix projection, compact internal-node storage, cooperative reverse reconstruction, provisional center advancement and the current automatic-selection results, including unresolved native-call stalls. [Section 10](#10-radix-232-and-cooperative-batches-2026-10-12) preserves the preceding eight-limb/256-candidate study. Sections 1-9 describe the earlier ten-limb/128-candidate resident engine. Each study's allocation sizes, tuning policy and measurements belong to its own implementation; the abstract below is the original study's abstract.

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

## 10. Radix-2^32 and cooperative batches, 2026-10-12

### 10.1 Baseline, sources and experimental boundaries

The checkout was clean at `8e5ad93333c316d9686030ec6b1819ccbc12741f`. Its unchanged executable was preserved as `measurements/gpu20261012-baseline.exe` before edits (SHA-256 `6663a4a9f8f1d8db7ab6f34740d2bbe0383677ea5b021770a8179216d3c679a7`). The supplied reference `87353dd2085dd02b49c47d651a949cc8c5c97596` was absent from the local object database; it is not silently substituted for the measured baseline. All experiments ran sequentially on this development machine: Windows amd64, Ryzen 9 9950X3D, 16 cores/32 logical CPUs, NVIDIA RTX 5090, driver 616.64, 32,607 MiB reported VRAM, PCI `00000000:01:00.0`, 575 W power limit. All logical CPUs remained available in every mode. No remote machine, Prefix32 executable or onionloom search was run during this study.

Both matched executables used PACE `go1.27.2 windows/amd64`, builder 0.5.5, Zig 0.17.0, CGO, dynamic linking, `--compat` and `-pgo=off`. Stock Go was also 1.27.2. The existing Vulkan SDK directory was 1.4.328.1; the tools reported shaderc `v2023.8 v2025.3-10-gc7e73e8`, glslang `11.1.0-1302-gd213562e` and SPIRV-Tools `v2025.4`. Normal executable builds use embedded SPIR-V and do not need these shader tools.

Prefix32 source was unavailable locally, so its repository was fetched into ignored `tools/prefix32-review` and checked out at [v2.2.0, fc077b3](https://github.com/0xROOTPLS/Prefix32/tree/fc077b3cb6df9364d755a0dfcfffff76cfd7579e). The complete [`kernel/vanity.cl`](https://github.com/0xROOTPLS/Prefix32/blob/fc077b3cb6df9364d755a0dfcfffff76cfd7579e/kernel/vanity.cl) and [`src/gpu.rs`](https://github.com/0xROOTPLS/Prefix32/blob/fc077b3cb6df9364d755a0dfcfffff76cfd7579e/src/gpu.rs) were reviewed together. Its field multiplication splits four 64-bit words into eight 32-bit words, performs exact schoolbook cells and reduces using `2^256 == 38 mod p`; its dedicated square computes 28 off-diagonal and eight diagonal products. Its large private numerator/denominator arrays, shared offset table, reverse Montgomery traversal and alternative paired-Y identities motivated separate experiments. Its center candidate, per-dispatch seed policy, result-capacity truncation, exhaustive autotuning and device-name-only cache were not transferred into onino. The adapted multiplication, squaring and reduction retain the full MIT copyright/permission notice in `field32.glsl`.

The published 2.810 G/s onino GPU, 10.392 G/s Prefix32 GPU and 4.115 G/s onino combined figures remain historical comparisons with rounded counters and different binaries. They are not fresh results or targets achieved by this study. Relevant mathematical context remains the Montgomery batch-inversion discussion and Hosseini/Farashahi paired-coordinate research cited in [RESEARCH.md](RESEARCH.md); the earlier CPU differential/direct-Y recurrence is a different algorithm from the paired formula tested here.

### 10.2 Retained arithmetic and bounds

Production `Field` now contains eight little-endian `uint32` limbs. Every integer in `[0, 2^256)` is a valid representation modulo `p = 2^255 - 19`, including noncanonical values and the final 38 integers at or above `2p`. Arithmetic folds modulo `2p = 2^256 - 38`; canonical serialization reduces modulo `p`. GLSL operands are passed by value, so calls such as `value = mul(value, value)` and repeated squaring preserve aliases. Host points are serialized directly into eight little-endian words; ten alternating 25/26-bit limbs remain only as a diagnostic test-output format, not in production storage or hot arithmetic.

The bounds used by the implementation are:

- Addition cells are at most `2*(2^32-1)+1 = 2^33-1`; the outgoing carry is at most one. Subtraction uses intentional unsigned 64-bit wrap to obtain a borrow from bit 63: a nonwrapped cell is at most `2^32-1` and a negative cell has magnitude at most `2^32`. Subtracting `38*borrow` corrects a wrapped 256-bit subtraction. If this correction itself borrows, the resulting value is near `2^256`, so the second correction cannot borrow again.
- Each multiplication cell is bounded by `(2^32-1)^2 + 2*(2^32-1) = 2^64-1`. The row-end carry occupies a previously unused high cell. Thus the sixteen-word schoolbook result is exact, with no overflowing 64-bit accumulator.
- Squaring accumulates the 28 distinct off-diagonal products, doubles that sixteen-word integer, then adds eight diagonal squares with explicit carries. The doubled partial sum is at most the final square and the final square is below `2^512`. Each diagonal addition is bounded by two 32-bit words plus a carry, below `2^33`; it does not require a 65-bit product accumulator.
- Reduction cells satisfy `low + 38*high + carry <= 39*(2^32-1)+38`, with outgoing carry at most 38. Folding that carry adds at most 1,444. If the first fold wraps the full number, its low word is at most 1,443; the final `+38` cannot wrap the full number again. All outputs are again valid eight-word representations.
- Canonicalization conditionally subtracts `p` twice. Adding 19 and inspecting both the outgoing 256th carry and bit 255 detects the subtraction condition. Toggling bit 255, rather than simply clearing it, handles the carry case for values near `2^256`. Two subtractions suffice because every permitted input is below `2p+38`. Prefix filtering always uses the resulting canonical low word; there is no speculative noncanonical prefilter.
- Inversion retains the established `p-2` addition chain, 254 squares and 11 multiplications. Zero maps to zero in arithmetic tests; production batch denominators are nonzero by completeness of the retained Edwards formulas. No division by a possibly zero alternative denominator is introduced.

The old ten-limb bounds in Section 2 remain the control representation's bounds. Fewer source products alone were not the acceptance criterion: an explicit `umulExtended`/`uaddCarry` eight-limb prototype was materially slower than wide-intermediate arithmetic on this compiler/driver.

### 10.3 Resident phases, batches and indexing

One SPIR-V module creates three specialized pipelines: specialization constant 0 selects preparation, inversion or reconstruction and constant 1 selects local size 128, 64 or 128 respectively. `maintenance4` is checked and enabled for `LocalSizeId`. Each preparation/reconstruction workgroup cooperates on 128 pairs, hence 256 candidates. Adjacent inversion invocations still handle independent stream roots, preserving enough independent work. A root is multiplied by its stream's jump denominator, inverted once and split back into the two reciprocals.

The 128-leaf tree needs 127 forward and 254 reverse multiplications. Its 256 field nodes are resident as `scratch[8][256]`: adjacent lanes access adjacent node words of the same limb. Shared memory retains the simpler array-of-fields layout; changing shared memory to limb-major storage did not improve complete search. Reconstruction selectively recomputes numerators instead of storing more intermediates. Preparation, inversion and reconstruction remain separated by compute-write/read barriers; the two reusable command-buffer/fence/descriptor/staging slots and no-hit zero-upload path are preserved.

The first center is `(s + 128*8)B`; lane `j-1` visits center `+j*8B` then `-j*8B`, for `1 <= j <= 128`. Centers advance by `257*8B`, so windows remain disjoint and center candidates remain skipped. Cursor accounting is `min(winner+1,256)-cursor`, never speculative lane count. A center advances only when its step counter is at most `UINT32_MAX-385`, ensuring the next center plus 128 is representable; the initial center minus 128 is zero. Existing seed clamping/reserved-step checks, generation acknowledgements, complete X-sign recovery, host scalar multiplication and independent saved-key verification remain in force.

The stream stride is `96 + 9*4 + 8*256*4 = 8,324` bytes. A command is `96+3*4 = 108` bytes. The table is `129*96+8*16+4 = 12,516` bytes before allocation alignment. At the explicit maxima, one submitted sequence checks at most `16384*64*256 = 268,435,456` candidates, fitting the 32-bit device counter; host totals remain 64-bit. Indices are bounded by 16,384 streams, 256 nodes and eight limbs. Profile query storage is at most 195 queries per slot: two whole-sequence timestamps plus `1+3*64` phase timestamps. Device storage-range, workgroup and heap checks are recalculated; a device exposing only the minimum 128 MiB storage range cannot accept 16,384 of these streams and automatic capacity is clamped accordingly. The 12,288-byte per-stream heap budget includes staging/control overhead. Required 128-lane groups and the checked shared-memory floor fit Vulkan's minimum limits, but runtime portability was tested only on this NVIDIA device.

Streams, candidates per inversion, workgroup size and submission rounds are separate quantities. Doubling rounds does not amortize inversion. The useful logical batch found here is 256 candidates, while automatic rounds continue to adapt submission duration and all-core scheduling. Explicit stream/round overrides retain their existing meanings and limits.

### 10.4 Screens and confirmed component contributions

Cheap screens used 16-20 second real searches at 16,384 streams/four rounds, followed by five alternating pairs for each promising component. Rates below are millions of exact completed candidates per monotonic second; neither kernel-only benchmarks nor displayed rounded rates determined retention. The staged prototypes deliberately changed one arithmetic, batch or storage choice at a time. Intermediate padded ABIs differ from the final native-eight-word ABI, so component gains must not be added together.

| Confirmed component | Before mean +/- sample SD, M/s | After mean +/- sample SD, M/s |
|---|---:|---:|
| Runtime phase branch → specialized pipelines | 2811.718 +/- 6.059 | 2920.379 +/- 23.549 |
| Ten limbs → eight limbs, wide intermediates | 2937.795 +/- 2.160 | 4188.168 +/- 1.153 |
| 128 → 256 cooperative candidates | 4159.145 +/- 59.790 | 4788.228 +/- 77.704 |
| Remove scratch's two padding words | 4848.001 +/- 10.734 | 5190.616 +/- 9.168 |
| Array-of-fields → limb-major global scratch | 5143.095 +/- 71.728 | 6362.386 +/- 83.570 |

Individual before → after samples in pair order:

- Phase: `[2807.639,2806.314,2807.998,2818.240,2818.397]` → `[2926.051,2935.324,2921.741,2880.070,2938.708]`.
- Radix: `[2934.667,2939.416,2940.147,2937.792,2936.955]` → `[4189.352,4188.228,4187.736,4189.067,4186.459]`.
- Batch: `[4185.443,4182.297,4052.270,4187.165,4188.550]` → `[4855.607,4854.871,4695.226,4714.429,4821.010]`.
- Compact scratch: `[4837.474,4854.343,4835.163,4856.741,4856.285]` → `[5193.366,5193.177,5174.421,5196.613,5195.502]`.
- Coalescing: `[5195.132,5196.078,5193.918,5052.168,5078.179]` → `[6431.218,6429.723,6407.940,6277.749,6265.301]`.

Rejected single-screen experiments, rather than precise estimates: serially batching 8/16/32 ten-limb roots in private arrays achieved 2855.6/2694.9/1574.9 M/s versus about 2950 specialized; eight roots with resident global prefixes achieved 2830.7. Repeating eight-root batching after the different radix/layout achieved 6221.3 versus 6514.7. This did not justify reducing independent inversion work further. Explicit extended multiplication/carries achieved 2928.6 versus 4203.2 for the initial wide-eight-limb screen. At the same intermediate layout, 256/512/1024 cooperative candidates achieved 4880.3/4161.8/4391.0; larger batches were rejected before production integration. Two serial pairs per invocation achieved 6365.6 versus 6514.7 for one pair per cooperative invocation. Limb-major shared memory achieved 6511.5 versus 6514.7 and was rejected as neutral. These controls include scratch traffic, center advancement, synchronization, filtering and result handling. Larger batches passed focused full-key checks, but rejected prototypes were not subjected to the final full compatibility matrix.

### 10.5 Alternative paired Y formula

The requested identity was evaluated independently on the ten-limb specialized engine, before combining arithmetic or batch changes. Let `a=xu`, `b=yv`, `c=dxyuv`. Edwards curve equations give `(b+a)(xv-yu)=(xy-uv)(1-c)` and `(b-a)(xv+yu)=(xy+uv)(1+c)`. Consequently `y(P+Q)=(xy-uv)/(xv-yu)` and `y(P-Q)=(xy+uv)/(xv+yu)` agree with the retained complete formulas wherever the new denominators are nonzero. Offset `uv` was cached instead of `duv`; the jump retained complete arithmetic.

The actual subgroup sequence does not supply a proof excluding `P=Q` or `P=-Q`, which can produce a zero alternative denominator. The prototype detected a zero denominator product modulo `p`, substituted a unit leaf into the inversion tree and reconstructed both exceptional candidates using the original complete formula and its own nonzero-denominator inversion. A zero leaf therefore could not poison neighboring candidates. Explicit positive/negative equal-offset point tests compared every candidate with independent Edwards addition/subtraction, including full Y and sign and passed synchronization validation. Ordinary sequence tests also passed. Complete search measured only 2587.5 M/s versus about 2950 for the original formula, so the alternative and its exceptional-case production complexity were removed. The independent exceptional-point tests remain valuable coverage for the retained complete formulas. This result does not reuse the rejected CPU differential recurrence as evidence.

### 10.6 Complete-search results and exact accounting

The main confirmation used five alternating pairs, 22 seconds per run, `somethingrare.`, normal verification/saving, validation disabled and all 32 logical CPUs available. GPU diagnostic cumulative integer counts and controller monotonic timestamps bound each window: first sample at or after eight controller seconds through the final available sample, normally about 13 seconds. Initialization, calibration and cancellation are measured separately. Lightweight one-second diagnostics were enabled in both builds; phase profiling was disabled. CPU-only control uses exact final counts divided by the full process wall interval because it has no GPU diagnostic samples. No fine-grained number is derived from rounded five-second console counters.

| Configuration | Baseline GPU | New GPU | Baseline CPU | New CPU | Baseline total | New total |
|---|---:|---:|---:|---:|---:|---:|
| GPU, manual 16384/4 | 2816.156 +/- 0.422 | 6717.286 +/- 25.683 | - | - | 2816.156 | 6717.286 |
| GPU, automatic | 2716.071 +/- 27.510 | 6260.917 +/- 58.664 | - | - | 2716.071 | 6260.917 |
| Combined, manual 16384/4 | 2209.456 +/- 103.333 | 4961.261 +/- 152.399 | 1327.051 +/- 4.358 | 1324.086 +/- 2.201 | 3536.507 +/- 103.949 | 6285.347 +/- 151.768 |
| Combined, automatic | 2800.105 +/- 20.890 | 6665.747 +/- 118.799 | 1327.008 +/- 3.974 | 1321.876 +/- 4.866 | 4127.113 +/- 21.046 | 7987.623 +/- 115.570 |

Units are M/s, means with sample SD where shown. Individual GPU-only and combined-total samples, baseline → new:

- Manual GPU: `[2816.539,2815.500,2816.453,2816.299,2815.990]` → `[6716.649,6673.988,6731.084,6724.646,6740.064]`.
- Automatic GPU: `[2678.780,2696.357,2730.650,2745.512,2729.057]` → `[6330.662,6286.270,6199.526,6199.781,6288.345]`.
- Manual combined: `[3525.425,3587.785,3648.276,3369.887,3551.162]` → `[6100.978,6176.346,6400.356,6467.069,6281.985]`.
- Automatic combined: `[4144.850,4137.615,4096.312,4142.446,4114.342]` → `[7850.757,8064.214,8083.042,7872.313,8067.787]`.
- CPU-only full-run control: `[1327.966,1306.260,1327.034,1324.302,1324.081]` → `[1323.887,1322.263,1325.203,1321.189,1324.306]`, means `1321.928 +/- 8.920` → `1323.370 +/- 1.619`; no CPU improvement is claimed.

These measurements preceded the final saving-backpressure change described below. The exact retained binary was subsequently confirmed with another five alternating 18-second manual pairs: GPU `2793.715 +/- 9.948` → `6744.478 +/- 7.931` M/s; combined `3607.555 +/- 30.950` → `6302.743 +/- 392.746`, comprising CPU `1327.065` → `1320.663` and GPU `2280.490` → `4982.081`. Individual final GPU samples were `[6757.371,6742.294,6744.172,6742.907,6735.643]` against `[2806.424,2778.534,2794.866,2795.287,2793.462]`; combined totals were `[6236.833,6532.330,5642.107,6548.198,6554.249]` against `[3597.980,3593.445,3633.137,3644.583,3568.628]`. The combined manual result is scheduling-sensitive; its low sample is retained. Two completed automatic backpressure-check pairs gave new GPU `[6216.712,6211.879]` versus pre-backpressure `[6211.712,6240.259]` and combined `[8005.629,7977.541]` versus `[7999.747,8034.290]`. That automatic follow-up stopped after unresolved stalls; it is not represented as a second five-pair confirmation.

Automatic GPU-only commonly preferred one round through its latency tie-break, leaving a repeatable gap from the manual four-round result. Combined automatic operation selected longer, still bounded sequences (roughly 15-32 rounds in these runs), greatly reducing scheduling gaps while all 32 CPU workers remained active. No exhaustive runtime sweep, tuning cache, CPU-core reservation or submission-limit increase was added.

Eight simultaneous prefixes, including two 51-character prefixes, were screened with two alternating 20-second pairs. GPU-only baseline `[2729.527,2859.520]` became `[6037.143,6224.046]` M/s; combined totals `[3508.117,3518.979]` became `[7220.822,7261.903]`, with CPU contributions about 752 M/s in both builds. This is a workload check, not a five-pair estimate. Compile-time filter-count specialization, a noncanonical low-word prefilter and a backend/compiler rewrite were not pursued: the measured arithmetic/layout gains did not require them and native-instruction evidence did not identify a compiler limitation demanding another backend.

### 10.7 Saving backpressure, startup, memory and responsiveness

Frequent matches exposed a distinct complete-search bottleneck. With `abcde.` and all CPU workers, the faster shader accumulated seconds of pending host verification and serialized durable saves. Five alternating pairs compared that version with the retained host change: before each new noncollection submission, a candidate backlog above eight waits for verifier feedback or cancellation. Dirty acknowledgements remain resident until submitted; collection-only cancellation drainage never waits for feedback or resumes paused streams. The channel, GPU paused state and in-flight results remain bounded, but eight is a submission backpressure threshold, not a strict cap on all accepted results. Saved matches are never dropped and counts are never retroactively invented.

The tradeoff is explicit. Pre-backpressure frequent-match combined steady throughput was `7725.596 +/- 47.970` M/s; retained throughput was `2533.971 +/- 187.109` (GPU 1276.045, CPU 1257.926). Full-process completed throughput including saving/drain was `5073.855 +/- 38.935` → `2288.247 +/- 100.167` M/s. This host change is retained for demonstrated responsiveness, not advertised as a throughput improvement. Drain fell from a 5376.534-5711.509 ms range, mean 5546.170 ms, to a 61.540-125.558 ms range, mean 84.981 ms. Exact process-exit delays were `[5648.14,5755.64,5634.55,5510.53,5424.85]` → `[200.56,519.17,544.07,205.37,516.35]` ms; driver teardown accounts for much of the remaining delay. Individual retained combined steady samples were `[2431.307,2853.566,2391.845,2543.262,2449.877]` M/s. Normal saving was enabled throughout. A separate GPU-only frequent screen, before host backpressure, saved 1,253 baseline and 3,020 new independent keys in 16 seconds, with 54/46 ms process-exit delays.

The 150 ms measured submission guard, at most two in-flight submissions and approximately 120 ms aggregate flight budget are unchanged. In the main rare-prefix confirmation, maximum whole-sequence GPU times (including calibration) were 3.0-3.2 ms for new manual GPU runs and 14.9-22.9 ms for new automatic GPU/combined runs. Maximum gaps were 0.22-0.35 ms manual GPU, 20-26 ms manual combined and up to 253 ms during combined calibration; these gaps are host scheduling/idle intervals, not long GPU kernels. Retained final manual process cancellation was 38-48 ms. Main automatic rare-prefix drainage was 0.52-0.54 ms GPU-only and 5.94-22.04 ms combined, but process exit reached 405 ms GPU-only and 278 ms combined. The older baseline also exhibited a 1.52 s automatic teardown outlier. Consequently the requested 150-200 ms envelope is satisfied by measured bounded GPU work, not guaranteed for process exit, durable saving or blocking driver calls.

Warm new automatic first useful submission was 133-140 ms GPU-only and 318-367 ms combined; selection completed at 4.619-4.637 s and 5.109-5.217 s. The first new manual confirmation incurred pipeline creation 1.916 s, initialization 2.053 s and first work 2.206 s; warm pipeline creation was a few milliseconds. A frequent-match retained selection took 5.422-5.987 s. These normal completed tests stayed within ten seconds, including initialization. The ten-second target cannot interrupt a blocking driver call.

At 16,384 streams, resident state increased from 86,441,984 to 136,380,416 bytes because the logical batch doubled. The aligned table is 12,528 bytes; each of two device command buffers and two upload buffers is 1,769,472 bytes, down from 2,162,688; each result/readback buffer remains 6,160 bytes. The selected state/table/commands/results use device-local memory and staging uses host-visible coherent memory on this device. A no-hit submission uploads zero command bytes and performs bounded readback; four recorded command buffers sufficed for the manual steady workload. Increased state memory is a measured cost of the winning batch, not a goal in itself.

Whole-process CPU time divided by wall time was 0.028-0.115 cores for main new manual GPU runs (the high sample includes cold compilation), 31.30-31.44 for manual combined and 30.88-31.23 for automatic combined. Warm manual working set was about 86 MiB GPU-only and 97 MiB combined; the cold GPU sample reached 278 MiB. Private memory was about 291 MiB warm GPU-only and 314-317 MiB combined, versus about 252-254 and 273-276 MiB baseline. Automatic driver allocations varied more: new GPU-only peak working sets 249-283 MiB/private 530-637 MiB; combined 99-100 MiB/private 320-324 MiB. These are externally sampled process peaks, not device-memory totals or steady-only host-cost estimates.

Three follow-up runs did not complete and remain part of the record. One retained run stopped submitting, drained in 13.858 ms, then stopped after the `teardown wait_idle` diagnostic. Two pre-backpressure new-binary runs stopped making progress after automatic 4096/64 trials; their last reported GPU sequence times were about 14.8 ms. The runner terminated each after its 30-second cancellation grace. Their logs are `gpu20261012-pressure-rare-0-all-auto-gpu20261012-backpressure.exe.log`, `gpu20261012-pressure-rare-retry-0-off-auto-gpu20261012-final.exe.log` and `gpu20261012-pressure-rare-validation-2-off-auto-gpu20261012-final.exe.log` under `measurements/`. The exact stalled call/cause was not established; absence of validation errors does not prove a driver bug. These failures are excluded from completed-throughput estimates, explicitly not counted as successful responsiveness/startup tests. Historical blocking-driver problems and these new unresolved stalls prevent a universal startup or cancellation guarantee. Watchdogs were not disabled and submission durations were not enlarged to hide failures.

### 10.8 Profiling and the remaining bottleneck

Nsight Graphics/CLI was not installed or discoverable in the available local tooling. Profiling therefore used Vulkan timestamps and `VK_KHR_pipeline_executable_properties`, separately from throughput confirmation. `ONINO_GPU_PROFILE=1` adds per-phase timestamp queries; the default path retains only whole-sequence timestamps. The optional path passed synchronization validation. Phase intervals include their inter-pass dependency boundary and should not be interpreted as isolated ALU times.

One 12-second final-arithmetic profile accumulated 18,659 rounds: preparation 2,247,421,792 ns (19.25%), inversion 1,464,575,680 ns (12.55%), reconstruction 7,960,494,176 ns (68.20%). The earlier specialized-ten-limb profile was approximately 22.6%/13.2%/64.3%. Reconstruction is the measured dominant phase; additional inversion amortization alone cannot explain or deliver the entire remaining opportunity. Combined fixed-four-round operation additionally leaves substantial host scheduling gaps; automatic longer sequences recover much of that throughput without changing the logical inversion batch.

| Native pipeline statistic | Old shared runtime-phase pipeline | New preparation | New inversion | New reconstruction |
|---|---:|---:|---:|---:|
| Registers per thread | 128 | 63 | 72 | 96 |
| Shared bytes | 5920 | 8212 | 3392 | 9760 |
| Native code bytes | 351104 | 36992 | 131456 | 129280 |
| Stack bytes | 0 | 0 | 0 | 0 |
| Subgroup size | 32 | 32 | 32 | 32 |

The old runtime-phase statistics are the preceding study's recorded control, not a newly collected baseline profile. Fresh ten-limb specialization reported preparation/inversion/reconstruction registers 96/96/128, shared bytes 5408/5408/5920 and code bytes 43648/164864/153856. Final SPIR-V is 153,860 bytes versus the original 159,520 bytes; three pipelines add creation work despite the smaller module. The driver returned no internal/native instruction representations. Its reported local-memory figures near 68.7 billion bytes are not credible spill statistics and are not used as evidence of spilling. Native multiply/carry/reduction/register-movement instructions, achieved occupancy, active-lane utilization, memory traffic, cache hit rates, bank conflicts and stall reasons remain unmeasured. Source multiplication counts are not substituted for those measurements.

The justified next investigations are reconstruction scheduling/data movement with real hardware counters; the unexplained automatic GPU-only gap; and saving/verification scheduling under all-core load without sacrificing independent verification, durable saves or CPU workers. Subgroup synchronization changes and filter specialization remain hypotheses, not retained gains. Cross-vendor runtime behavior and the unresolved blocking/stall failures need investigation before stronger portability or responsiveness claims.

### 10.9 Correctness and reproduction

PACE and stock-Go full GPU suites passed with Vulkan synchronization validation: 403 tests ran, 400 passed, three skipped. The GPU package's 24 tests include independent big-integer arithmetic on maximal limbs, carries, prime/`2p`/`2^256` boundaries, all permitted noncanonical representations sampled across repeated operations and aliases; 516 consecutive full encoded public keys across center transitions; positive/negative exceptional point pairs; multiple/long-prefix host checks and GPU false positives; capacity-one overflow, stale acknowledgements, generation changes, reseeding, exhaustion, exact submission accounting, cancellation drainage and calibration transitions. Backpressure cancellation has a focused regression. The full optional phase-profile GPU suite also passed all 24 tests. The epoch fixture starts at `UINT32_MAX-128`, so its highest candidate, not just its center, remains within the valid range.

PACE and stock-Go CPU-only suites passed both native and `purego` builds. Custom vet passed Windows GPU+CGO, Windows `purego`, Linux GPU+CGO and Darwin arm64 checks; neither standalone `go vet` nor staticcheck was run. Windows stock-Go GPU, Linux amd64 PACE GPU/GNU dynamic, Linux arm64 portable CPU and Darwin arm64 CPU builds passed. The Linux artifact contains `/lib64/ld-linux-x86-64.so.2` and `libc.so.6`; no Linux GPU runtime claim follows from that build. No dependency was added or upgraded.

All four embedded shaders were regenerated, validated and independently regenerated byte-for-byte using [BUILD.md](internal/gpu/BUILD.md). Search SPIR-V SHA-256 is `a69f279d5af3f87bbd0e685dc1a34bf36ce472a8baf21bd650d647aa1e92de7d`; arithmetic `a9525fb7c9790dd52e8d35c6dfafaecba9aa9a3b20d20ce756460953f56addb6`; diagnostic `02f4f6c7c25fe2b7a620f23f14db9f2fa045eb789adeae0fc5afeb5d6f54700f`; epoch `c4ca15ad969f447931bae206a93cd7a77afc24241c65446e39f5d210d5004358`. Production sources contain none of the rejected root/formula/layout/extended-carry variants and do not include files from ignored directories.

Exact build and local comparison commands (repository root, existing ignored `measurements/` and `scripts/`):

```sh
builder build go --cgo --pace --dyn --compat --no-gen --output measurements/gpu20261012-retained.exe -tags gpu -pgo=off
pace run scripts/gpu20261012-cli.go --exe measurements/gpu20261012-baseline.exe --compare measurements/gpu20261012-final.exe --settings 16384/4,auto --cpu off,all --duration 22s --pairs 5 --name confirm
pace run scripts/gpu20261012-cli.go --exe measurements/gpu20261012-baseline.exe --compare measurements/gpu20261012-backpressure.exe --settings 16384/4 --cpu off,all --duration 18s --pairs 5 --name final-manual
pace run scripts/gpu20261012-cli.go --exe measurements/gpu20261012-final.exe --compare measurements/gpu20261012-backpressure.exe --settings auto --cpu all --duration 16s --pairs 5 --name pressure-confirm --patterns abcde.
pace run scripts/gpu20261012-cli.go --exe measurements/gpu20261012-baseline.exe --compare measurements/gpu20261012-final.exe --settings auto --cpu only --duration 22s --pairs 5 --name cpucontrol
ONINO_GPU_PROFILE=1 pace run scripts/gpu20261012-cli.go --exe measurements/gpu20261012-retained.exe --settings 16384/4 --duration 12s --name profile
```

The first baseline build used the identical builder command with output `gpu20261012-baseline.exe` before edits. `final.exe` denotes the confirmed arithmetic/layout combination before saving backpressure; `backpressure.exe` and a fresh `retained.exe` contain the final host change. The runner and raw JSON/logs are intentionally local ignored development artifacts, not build dependencies. It creates a separate Windows console process group, gives the child affinity `0xffffffff`, sends Ctrl-Break after the requested wall duration, keeps verification and normal key saving enabled, alternates pair order and records exact diagnostic samples plus final counters, process CPU time, memory peaks and exit latency. Raw results use `measurements/gpu20261012-{name}-{pair}-{cpu}-{configuration}-{binary}.{json,log}`. Reuse new names to preserve previous runs. Intermediate experiment binaries require their archived prototype environment flags; production builds require none of them.

For reproduction without that local runner, launch `onino --cpu off --gpu auto --gpu-diagnostics --gpu-streams 16384 --gpu-rounds 4 --output measurements/reproduction somethingrare.`; use `--cpu all` for combined and omit both size flags for automatic mode. Capture stderr, take two `cumulative sample` records after selection and at least eight controller seconds and divide each exact CPU/GPU count delta by the `controller_wall_s` delta. Cancel normally and retain all final counters/saves. Repeat in alternating matched-build order at least five times. Measure startup and process exit separately; do not use displayed compact counters for these calculations. Phase profiling and validation must be disabled for throughput runs.

```sh
ONINO_VULKAN_TEST=1 VK_LAYER_PATH="$PWD/tools/vulkan-sdk/Bin" VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT builder test go --cgo --pace --dyn --compat --no-gen -tags gpu -vet=off -p=1 ./... -count=1
go test -vet=off -p=1 ./...
go test -vet=off -p=1 -tags purego ./...
pace test -vet=off -p=1 ./...
pace test -vet=off -p=1 -tags purego ./...
vet --tests --tags gpu --cgo
vet --tests --tags purego
vet --tests --os linux --tags gpu --cgo
vet --tests --os darwin --arch arm64
builder build go linux --arch amd64 --cgo --pace --gnu --dyn --compat --no-gen --output measurements/onino-linux -tags gpu -pgo=off
builder build go linux --arch arm64 --pace --no-gen --output measurements/onino-linux-arm64 -tags purego -pgo=off
builder build go darwin --arch arm64 --pace --no-gen --output measurements/onino-darwin-arm64 -pgo=off
```

Repeat the first test command without `--pace` for stock Go or with `ONINO_GPU_PROFILE=1` for optional timestamp instrumentation. These are POSIX-shell environment assignments; PowerShell users should set the equivalent `$env:` variables. Builds, correctness tests, profiles and timing experiments were run sequentially, not against competing benchmark jobs. The results demonstrate practical complete-search gains on this machine, not an absolute hardware maximum.

## 11. Reconstruction and exact prefix projection, 2026-10-13

### 11.1 Baseline and measurement contract

The current checkout was clean at the reviewed reference `cd0192e2b9d55f5a0178a7a6f91895d414326cf6`; there were no subsequent commits or local changes to account for. README, this research history and the relevant shader, bridge, controller, tuner, verifier and tests were reviewed before editing. The unchanged baseline was built first as `measurements/gpu20261013-baseline.exe`, SHA-256 `931f308a0ff4163363077b51bf2155e7c755faaea43735e817e526cbe30f56e2`. The measured retained executable is `measurements/gpu20261013-final.exe`, SHA-256 `479502e3c43fdc1655bdaa447e818f78c6a73d3ec41ab1cfb51dd4855878ab6c`.

Both use `builder build go --cgo --pace --dyn --compat --no-gen --output measurements/NAME.exe -tags gpu -pgo=off`: builder 0.5.5, PACE/Go 1.27.2, Zig 0.17.0, Windows amd64 v1, trimpath, dynamic CGO, PGO disabled. Embedded build information reports `go1.27.2-X:newinliner,runtimefreegc`. Shader tools and hardware are the same as Section 10.1: Vulkan SDK 1.4.328.1, Ryzen 9 9950X3D, RTX 5090, Windows 11, NVIDIA 616.64, 32,607 MiB VRAM, 575 W power limit. Every run used this machine, affinity `0xffffffff`, normal priority and normal durable saving. No remote machine, Prefix32 executable or onionloom search was used.

Historical attribution matters: Section 10's **6744.478 M/s manual GPU** result confirmed its final backpressure binary; **7987.623 M/s automatic combined, including 1321.876 M/s CPU**, came from the earlier pre-backpressure confirmation. Neither is substituted for this study's matched baseline. Prefix32's historical **10.392B/s GPU-only** remains a reference from a different measurement procedure, not a newly reproduced competitive result.

Real CLI searches were run sequentially, alternating baseline/candidate order within each comparison. The principal runs last 22 seconds; exact diagnostic CPU/GPU count deltas between the first sample at or after eight controller seconds and the last sample give normally 13-second steady windows. Monotonic controller timestamps, not rounded progress strings, define those windows. One-second diagnostics are enabled equally; validation, phase profiling and debug captures are disabled during throughput runs. Final exact counts divided by complete process wall time separately include launch, initialization, useful calibration, search, accepted-work drainage and teardown. CPU-only controls use this full-process denominator. Every completed sample, including unmatched partners of failed runs and low observations, contributes to the reported all-completed means. Failed processes have no successful final count and are listed separately.

The controller still runs all requested CPU workers independently, with no CPU arithmetic feed or default core reservation. The 150 ms submission guard, two-submission limit, approximately 120 ms aggregate flight budget, watchdogs, CLI overrides, progress/final reporting, full independent verification, durable saving and one export per seed remain in force.

### 11.2 Retained arithmetic: exact representations, not approximate filters

Let `N=2^256`, `p=2^255-19` and `B=2^32`. Every integer in `[0,N)` remains a permitted eight-limb representation. The schoolbook product and dedicated square are unchanged; their exact accumulator bounds in Section 10.2 still apply.

**Carry folding.** The only production callers of `fold` are addition, with outgoing carry at most one and product reduction, with carry at most 38. The first adjustment is therefore at most `38*38=1444`. If adding it wraps `N`, the entire wrapped value is at most 1443. The second correction is exactly `+38`, giving at most 1481, below `B`; only limb zero can change. If the first adjustment does not wrap, the second correction is zero. Replacing the second eight-limb pass with that low-limb operation preserves the previous bit-for-bit representation, including noncanonical inputs, rather than merely equality modulo `p`.

**Borrow correction.** The initial subtraction's outgoing borrow is at most one, so the first correction subtracts either zero or 38. A full-value borrow during that correction requires its input to be below 38. The wrapped low limb is then at least `B-38`; subtracting the second 38 leaves it at least `B-76`, so no further limb can borrow. The low-limb-only second correction is again bit-for-bit identical for all permitted operands. Actual-shader raw-output tests compare add/subtract/fold/canonicalization against independent `math/big` integer models, including every carry from zero through 38, wrap neighborhoods, noncanonical boundaries and 2,048 random operand pairs.

**Exact canonical prefix word.** For `0 <= v < N`, `q=[v>=p]+[v>=2p]` is zero, one or two and `low64(v mod p)=(low64(v)+19*q) mod 2^64`. Let `h` be bit 255. Away from the modulus boundaries, `q=h`. A further subtraction is necessary precisely when all remaining bits 32..254 are one and limb zero is at least `B-19-19*h`. Thus, with `upper=(limb[7] | 0x80000000) & limb[6] & ... & limb[1]`, the implementation is `q=h+[upper==0xffffffff && limb[0]>=0xffffffed-19*h]`. For `h=0` this detects the final 19 integers before `2^255`; for `h=1` it detects the final 38 integers before `N`. Unsigned 64-bit addition intentionally wraps to the exact low word. No speculative prefilter, changed false-positive rate or ignored comparison bit is involved.

Production filtering and the hit record use `canonicalFirstWord`; full point diagnostics still canonicalize complete Y and recover canonical X's sign. Host verification reconstructs the complete key as before. A separate actual-shader differential test checks neighborhoods of zero, `p`, `2p` and `N`, every individually set and cleared bit and 2,048 random full-width values against `math/big` reduction. This complements, rather than replaces, the full-encoding and state-machine tests.

### 11.3 Retained GPU scheduling and compact state

The packed inversion invocation now computes the provisional next affine center after splitting the joint tree/jump reciprocal. This transfers the approximately nine-multiplication serial reconstruction tail into the existing independent-per-stream inversion phase, entirely on the GPU. Reconstruction retains the old center for current candidates and commits the provisional value only on a no-hit, nonexhausted advance. A hit keeps the old center/cursor; continuation recomputes its reciprocal and provisional center. A reseed or generation change supplies a new center before preparation/inversion; collection-only cancellation does not advance streams. Exhaustion requests a replacement without committing the provisional value. Existing command acknowledgement and result publication boundaries are unchanged.

Reverse traversal assigns one child multiplication to each live lane, rather than two serial child multiplications to a parent lane. At level `size`, lane `i<2*size` reads parent `products[(2*size+i)/2]` and original sibling `products[(2*size+i)^1]` into a private result. **Every lane reaches a workgroup barrier before any child is overwritten**, then live lanes store their child and all lanes synchronize again. The original sibling values therefore survive both reads. Work remains 254 reverse multiplications, with more participating lanes at each level; there is no work-inefficient multiplicative scan or assumed subgroup layout.

Global scratch stores only 128 internal nodes, including slot zero for the jump denominator. Preparation no longer stores the 128 leaves. Reconstruction already computes each pair's mixed product and recreates its denominator with one square and subtraction, trading arithmetic for half the tree's global traffic. Shared `Field products[256]` remains unchanged. Diagnostic full-point/sign slots 2 and 3 are still internal slots and survive until publication.

The new stream stride is `96 + 9*4 + 8*128*4 + 96 = 4324` bytes, including the provisional center. At 16,384 streams, resident state is **70,844,416 bytes / 67.5625 MiB**, versus **136,380,416 bytes / 130.0625 MiB**. The center itself costs 1.5 MiB; net state savings are 62.5 MiB. All global node indices are below 128; shared leaf indices remain below 256. Storage-range checks use the new stride, so the maximum state now fits a 128 MiB storage-buffer range. The conservative 12,288-byte per-stream heap budget is retained and covers state plus commands/staging/control overhead; it may select fewer streams on a small heap than a more aggressive budget. Command/table/result sizes, submission counter bounds and query layouts are unchanged. This is not a claim that fitting in a particular L2 cache caused the gain.

Each retained shader step was screened independently with two 16-second pairs, then confirmed with three more alternating pairs using its preserved predecessor/candidate binaries. All five pairs improved for each step. Means with sample SD, M/s, GPU-only 16384/4:

| Isolated step | Before | After |
|---|---:|---:|
| Low-limb second carry/borrow correction | 6729.558 +/- 23.482 | 6799.942 +/- 17.502 |
| Exact canonical prefix projection | 6778.407 +/- 38.923 | 6875.328 +/- 33.556 |
| Provisional center in packed inversion | 6862.440 +/- 28.466 | 8006.487 +/- 7.309 |
| One child per reverse-tree lane | 7982.480 +/- 27.461 | 8660.304 +/- 26.904 |
| Internal-only global tree | 8668.326 +/- 36.453 | 9226.305 +/- 67.322 |

The stages were measured at different times; their percentages must not be added. The final binary has its own matched confirmation below.

### 11.4 Rejected bounded experiments

Each prototype passed the GPU arithmetic/full-key/state suite with synchronization validation before two alternating 16-second complete-search screens at 16384/4. These are rejection screens, not five-pair estimates. All prototypes and raw captures remain ignored development artifacts; none creates a production variant portfolio.

| Experiment | Matched compact-tree control, M/s | Prototype, M/s | Decision |
|---|---:|---:|---|
| Subgroup-local reverse segments / register sibling exchange | 9286.565 | 9197.080 | Rejected |
| Reconstruction + next-round preparation fusion | 9285.794 | 8795.020 | Rejected |
| One-level four-limb-half Karatsuba | 9273.184 | 8590.637 | Rejected |
| Comba columns with integrated high-word folding | 9279.276 | 5143.630 | Rejected |

The subgroup prototype explicitly mapped logical lanes as `gl_SubgroupID*gl_SubgroupSize+gl_SubgroupInvocationID`; it did not equate local invocation ID with subgroup position. Original sibling values were loaded before XOR register exchange, shared memory/barriers handled cross-subgroup parents and recomputed final leaves stayed in registers. Queried device support was compute-capable basic/shuffle operations, subgroup size/min/max 32 (supported operation mask `0x7ff`). Only that mapping was exercised on hardware. Because the variant lost, production retains the shared-memory implementation with no subgroup requirement, width rule or runtime selection framework. Other subgroup sizes were not hardware-tested or claimed as validated. Moving the center already won decisively, so a second cooperative-center variant was not added without profiling evidence favoring it.

Fusion bootstrapped preparation once, kept packed inversion separate and omitted preparation after the last round: `3R` dispatches became `2R+1`. Paused streams, acknowledgements, generations and last-round counts passed tests, but reconstruction native code grew from 64,512 to 94,336 bytes, with 96 reported registers in both. SPIR-V grew from 150,112 to 169,156 bytes. Fewer dispatches did not offset the complete-search regression.

Karatsuba used three exact four-by-four products (48 limb products): low, high and the product of absolute signed half-differences. The middle term `lowProduct+highProduct-signedDifferenceProduct` equals the two cross products, is nonnegative and below `2^257`; a nine-word accumulator preserves the extra bit. Exact sixteen-word recombination precedes the existing reduction. Product-cell bounds remain those of schoolbook arithmetic, but difference/carry/recombination work erased the source-product saving.

Comba used a 64-bit low accumulator plus an explicit overflow word, never a naive sum of full products in one `uint64`. A column contains at most eight products and incoming carry below `8*B`, giving a total below `8*B^2` (67 bits); at most seven 64-bit overflows occur and the next carry remains below `8*B`. High product words were folded with factor 38 directly into the low eight words; each reduction carry remains at most 38. This bounded implementation was correct but much slower. Dedicated squaring was kept separate and unchanged. Previously rejected larger batches, additional root batching, serial pairs, extended-carry primitives, shared-memory relayout and the alternative paired-Y formula were not restarted. FP64/tensor/residue/backend rewrites lacked a competitive cost model or native-code evidence and were not pursued.

### 11.5 Automatic selection and full-search confirmation

The GPU-only gap was reproducible even after faster reconstruction. With the compact kernel, 300 ms trials rated 16384/1 around 9249 M/s and 16384/4 around 9427; a 2.5% indifference band deliberately retained the shorter configuration. Finalist repeats still preferred that tie despite sustained one-round rates around 9095 versus four-round 9219 and sixteen-round 9286. In the corresponding screens, GPU idle-gap fractions were approximately 1.69%/0.38%/0.09%. Under all CPU workers, one/four/sixteen-round gap fractions were approximately 71.38%/39.11%/1.50%; combined automatic operation used longer sequences and about 0.11% gaps. These are observed scheduling intervals, not shader stall counters.

An experiment lengthening finalist trials to 750 ms added about 1.35 seconds to selection without a repeatable combined full-process benefit and was reverted. The retained change narrows the minimum indifference band to **1%**, preserving 300 ms useful trials, repeated finalists, the measured-drift expansion of the band, latency tie-breaking inside it and deadline reserves. Five isolated automatic pairs measured GPU-only **9080.514 +/- 113.350 → 9228.907 +/- 36.818 M/s** (four of five wins), while combined throughput was effectively unchanged, **10538.840 +/- 125.405 → 10536.123 +/- 126.445**. Tests distinguish a stable 2% gain, a sub-percent tie and a larger measured-drift band. No GPU-name rule, longer startup matrix or extra pipeline is introduced.

Final rare-prefix results below include every completed main/retry observation. Units are M/s, mean +/- sample SD. Explicit configurations have five complete pairs. Automatic configurations have five complete pairs plus one completed candidate whose baseline partner stalled; therefore the all-completed candidate means use six observations and the baseline means five. Initial automatic failure counts are one baseline GPU-only transition and one baseline combined teardown; the paired retries completed.

| Configuration | Baseline GPU | Final GPU | Baseline CPU | Final CPU | Baseline total | Final total |
|---|---:|---:|---:|---:|---:|---:|
| GPU automatic | 6257.305 +/- 74.713 | 9238.235 +/- 18.232 | - | - | 6257.305 | 9238.235 |
| GPU 16384/4 | 6710.120 +/- 18.094 | 9234.282 +/- 42.080 | - | - | 6710.120 | 9234.282 |
| GPU 16384/16 | 6734.008 +/- 4.204 | 9239.360 +/- 18.481 | - | - | 6734.008 | 9239.360 |
| Combined automatic | 6703.470 +/- 10.259 | 9217.292 +/- 65.096 | 1323.718 +/- 4.198 | 1323.068 +/- 2.767 | 8027.188 +/- 13.624 | 10540.360 +/- 64.625 |
| Combined 16384/4 | 5017.151 +/- 133.094 | 6149.859 +/- 108.998 | 1319.925 +/- 1.604 | 1319.688 +/- 2.081 | 6337.075 +/- 134.227 | 7469.548 +/- 109.052 |
| Combined 16384/16 | 6674.348 +/- 45.833 | 9065.415 +/- 64.149 | 1322.199 +/- 3.401 | 1324.850 +/- 2.571 | 7996.547 +/- 48.942 | 10390.265 +/- 62.692 |

For just the five fully matched automatic pairs, final GPU-only is **9238.120 +/- 20.381** and combined **10541.898 +/- 72.130**. The extra completed observations do not drive the result. Final automatic GPU-only selected 16384/3 or /4; combined selected 16384/15, /21 or /42. There is no material rare-prefix automatic/manual gap in this confirmation, but no claim of a global optimum. Four explicit rounds remain strongly scheduling-limited with all CPU workers; preserving the override exposes that cost instead of silently changing it.

| Complete-process rate, M/s | Baseline | Final |
|---|---:|---:|
| GPU automatic | 6116.430 +/- 66.184 | 8758.790 +/- 324.309 |
| GPU 16384/4 | 6608.711 +/- 16.063 | 9096.142 +/- 39.755 |
| GPU 16384/16 | 6630.880 +/- 3.739 | 9108.845 +/- 17.929 |
| Combined automatic | 7324.904 +/- 50.258 | 9544.737 +/- 91.866 |
| Combined 16384/4 | 6066.703 +/- 104.475 | 7043.432 +/- 106.709 |
| Combined 16384/16 | 7724.577 +/- 61.422 | 9938.522 +/- 52.814 |
| CPU-only, five 18-second pairs | 1318.496 +/- 2.663 | 1319.540 +/- 1.828 |

CPU code and requested worker counts are unchanged. The CPU-only full-run denominator differs from the combined steady window; their small difference is not evidence that GPU work accelerates CPU arithmetic. Likewise, 10.540B/s combined exceeds the historical Prefix32 GPU-only reference numerically but is not an equivalent comparison. Onino GPU-only remains about 9.24B/s, below that historical target.

Five further automatic pairs per workload/mode exercised four prefixes (`somethingrare.`, `anotherprefix.`, `vanityaddress.`, `hellothereab.`) and one long prefix (`somethingreallyrareabcdefghijklmnopqrstuvwxyz234567.`), with no failures or saves:

| Workload / mode | Baseline steady total, M/s | Final steady total, M/s | Baseline → final full-process, M/s |
|---|---:|---:|---:|
| Four prefixes / GPU | 6178.860 +/- 102.007 | 9089.936 +/- 34.669 | 6031.595 → 8750.137 |
| Four prefixes / combined | 7394.494 +/- 31.846 | 9822.532 +/- 105.092 | 6715.712 → 8823.338 |
| Long prefix / GPU | 6218.689 +/- 18.013 | 9253.713 +/- 29.197 | 6070.862 → 8903.404 |
| Long prefix / combined | 7979.964 +/- 89.232 | 10387.157 +/- 287.782 | 7303.165 → 9445.185 |

Four-prefix CPU throughput was 800.113 → 800.924 M/s; long-prefix CPU was 1323.211 → 1321.833. The final long-prefix combined minimum, 9891.937 M/s, is included. These workloads do not promise the same gain on unmeasured patterns or devices.

### 11.6 Frequent matches and sustainable saving

A separate local capacity experiment precomputed independent valid candidates: 10,000 real verifications/reseeds with a no-op save took 180.172 ms (**55,502/s**), while 1,000 normal independent `Store.Save` calls took 2.1843 seconds (**457.813 durable saves/s**). The latter includes key validation, file flushes and rename. These are idle-machine stage-capacity measurements, not capacities under 32-worker contention. Verification is about 18 microseconds versus 2,184 microseconds for saving; overlapping those two isolated stages offers less than 1% theoretical benefit and does not explain the all-core loss. They do not justify a new admission formula or queue expansion from idle measurements alone.

Final `abcde.` searches kept normal durable saving and the existing backlog-above-eight submission backpressure. Five combined pairs completed. GPU-only encountered stalls: seven attempts per executable produced five completions each, but only four fully completed pairs after bounded retries. Its successful-run estimates are explicitly not a five-pair reliability/throughput confirmation.

| Frequent-match metric | GPU baseline | GPU final | Combined baseline | Combined final |
|---|---:|---:|---:|---:|
| Completed/attempted runs | 5/7 | 5/7 | 5/5 | 5/5 |
| Steady completed M/s | 6068.022 +/- 320.846 | 8512.348 +/- 798.593 | 2430.016 +/- 83.038 | 2455.783 +/- 66.213 |
| Full-process completed M/s | 5930.870 +/- 230.585 | 8301.591 +/- 557.424 | 2321.892 +/- 59.992 | 2355.838 +/- 41.146 |
| Verified durable saves / full-process second | 175.721 +/- 8.078 | 245.704 +/- 17.579 | 68.816 +/- 2.840 | 70.781 +/- 3.695 |
| Process-exit latency range, ms | 39.842-50.477 | 43.628-50.299 | 194.528-255.321 | 211.871-276.223 |

Combined frequent throughput did not materially improve: CPU contribution was 1259.791 → 1258.976 M/s. The final successful GPU-only run maxima sampled backlog 7-8 and combined 11-15; these one-second samples are not instantaneous backlog bounds. Accepted in-flight results may exceed eight. Final drainage was 0.519-6.835 ms GPU-only and 66.419-126.623 ms combined. Sustainable saves and whole-process rates, rather than a growing queue, determine this result. The responsiveness benefit from Section 10's backpressure is preserved; no accepted match is dropped, no persistence step weakened and no CPU search worker repurposed. Sustainable capacity under all-core contention remains a separate optimization opportunity.

### 11.7 Startup, host use, memory and profiling

Across completed final workload runs, the slowest first useful GPU work was **1.796 seconds**, including a **1.656-second pipeline creation**; the slowest automatic selection was **6.296 seconds**. Warm rare-prefix automatic first work was about 0.133 seconds GPU-only and 0.284-0.441 seconds combined, with selection 4.61-4.65 and 4.79-5.10 seconds respectively. Frequent combined selection reached 5.928 seconds. These are completed-run observations, not a deadline guarantee for the stalled native calls below.

Two additional 45-second alternating pairs per GPU-only setting sampled process CPU counters only after 25 controller seconds, separating warm orchestration from startup/compiler activity. Automatic baseline/final means were **0.039/0.016 cores** (final observations 0.006-0.027); explicit 16384/4 used **0.022/0.035 cores**. Their steady GPU rates were 6198.884 → 9239.612 and 6688.141 → 9178.940 M/s. Short automatic runs can include roughly one core of background driver compilation; native diagnostic captures found an `nvgpucomp64` worker with 6.4-8.2 CPU-seconds by eleven seconds. Those costs are not charged to the warmed controller or concealed from full-process measurements. Frequent verification/saving necessarily uses more host CPU: successful GPU-only frequent windows averaged 0.494 cores in the final binary.

Final rare-prefix explicit GPU peak working set averaged 85.765 MiB, private memory 219.941 MiB; combined averaged 96.602/243.462 MiB. Automatic GPU process peaks varied substantially with driver activity: working set 99.496-332.504 MiB and private memory 407.422-554.504 MiB. Automatic combined averaged 98.834/250.818 MiB. These externally sampled process allocations are distinct from the exact 67.5625 MiB device stream state. No memory-cache-residency inference is made.

Maximum sampled sequence time over all completed final workloads was **21.012 ms**, including calibration, well below the unchanged submission guard. Rare explicit process exit was 36.918-51.299 ms; rare automatic was 41.854-359.119 ms GPU-only and 67.495-269.184 ms combined. Rare automatic drain was 1.020-2.789 and 6.811-18.963 ms respectively. Host teardown and durable-save tails remain outside the GPU flight budget.

Nsight Graphics GPU Trace/Shader Profiler and CLI tooling were not installed locally; no tool or dependency was installed. Separate 12-second phase-timestamp profiles at 16384/4 reproduced the old preparation/inversion/reconstruction split as **19.295% / 12.598% / 68.107%** over 18,607 rounds and measured **23.607% / 17.246% / 59.147%** over 25,827 compact-kernel rounds. These intervals include dependency boundaries. Reconstruction remains dominant despite its substantial reduction; the larger inversion percentage does not mean its absolute per-round time grew.

| Final native executable statistic | Preparation | Inversion | Reconstruction |
|---|---:|---:|---:|
| Registers per thread | 63 | 72 | 96 |
| Shared bytes | 8212 | 3392 | 9760 |
| Native code bytes | 34688 | 183552 | 64512 |
| Stack bytes | 0 | 0 | 0 |

The provisional-center code increased inversion code size from 131,456 bytes without increasing the reported register count; reconstruction shrank from 129,280 bytes. Production SPIR-V is 150,112 bytes versus 153,860 baseline. The driver exposed no native instruction representation and its local-memory statistics near 68.7 billion bytes remain unusable as spill evidence. Active-lane utilization, integer instruction throughput/dependencies, achieved occupancy, genuine spills, shared-bank traffic, cache hit rates and global transaction counts remain unmeasured. The one-child scheduling and compact-tree conclusions rest on controlled complete-search comparisons, not source multiplication counts masquerading as native profiling.

### 11.8 Bounded stall investigation: blocked calls identified, cause unresolved

Six initial unchanged-baseline automatic reproductions completed. A later noise-band experiment stalled after the 4096/64 trial. Nine bounded diagnostic runs (full call tracing, transition-only tracing, native captures, Go all-goroutine snapshots; three with synchronization validation) completed without reproducing that failure or reporting a validation error. Uninstrumented final confirmation subsequently captured two baseline stalls and four frequent-match stalls, including two in the final binary. Across the final workload/control matrix, baseline completed 69 of 73 attempts and final completed 71 of 73. The runner captures native threads/minidumps five seconds before its unchanged 30-second cancellation-grace kill. Failed 22-second runs ended around 52.03 seconds without final accounting and are never successful throughput/startup/cancellation tests.

The native helper enumerates/suspends/resumes each process thread, records contexts, CPU times and module-relative stacks and writes a minidump. Stripped Go frames were resolved from the executable's `pclntab` with `debug/gosym`. The exact Vulkan call was determined from the saved return address and PE indirect-call target, matched to the loader's stored function pointer; a nearby exported driver symbol is not treated as its internal function name.

- **Automatic transition:** `gpu20261012-13-final-rare-0-off-auto-gpu20261013-baseline.exe.log` last completed a 4096/64 trial at 2.835309 seconds, tail 14.654 ms. At the last cumulative sample, 2.011457 controller seconds, 11,326,193,664 GPU candidates and 2,899 submissions were collected, with zero pending hits/backlog; progress subsequently stopped around 15.1G. At 47 seconds the Go chain was `Run → calibration.selectInitial → execution.measure → execution.submit → device.dispatch → _Cfunc_onino_submit`, blocked inside **`vkResetCommandBuffer`**, return offset `+0x207954`, not fence waiting. The caller had approximately 44.3 CPU-seconds and a native worker 44.2 seconds, consistent with prolonged spinning/yielding rather than a long measured GPU sequence.
- **Teardown:** `gpu20261012-13-final-rare-1-all-auto-gpu20261013-baseline.exe.log` reported stop at 22.008282 seconds and completed drain in 20.255 ms, 1,835 lifetime submissions, before the last `teardown wait_idle` marker. The captured chain was `Run`'s deferred `device.close → _Cfunc_onino_close`, blocked at **`vkDestroyCommandPool`**, return offset `+0x2071d9`. `vkDeviceWaitIdle` had already returned. Thus the old last log line did not identify the blocked call. The last cumulative sample had CPU 27,983,478,784 and GPU 127,674,155,008, 1,806 collected submissions, zero pending hits/backlog; no final CLI count was printed.
- **Frequent matches:** main `13-final-frequent` GPU-only pair 3 failed for both executables and pair 4 for baseline; `13-final-frequent-retry` pair 1 failed for final. All four stopped after a 4096/64 automatic trial and native stacks again identified `vkResetCommandBuffer`. Accepted hits already reported were saved before progress stopped. These are the two failures per binary in Section 11.6, not four omitted outliers.

Five further bounded frequent-match diagnostic runs were made: three minimally traced and two with every command-buffer reset logged. One minimal run stalled during steady 16384/4 operation, also in `vkResetCommandBuffer`; its last cumulative sample at 9.018799 seconds had GPU 77,722,628,294, 8,721 collected submissions, tail 14.358 ms, pending zero and backlog eight. The ten-second Go dump preceded the stall and therefore is not an all-goroutine snapshot of the blocked state. Two verbose runs completed but substantially perturbed throughput and are not benchmark samples. Loaded `graphics-hook64` and `amdihk64` modules were visible in diagnostic captures; their presence is not evidence of causation.

Instrumentation recorded both slot `submitted` flags and actual `vkGetFenceStatus` results around resets in successful traces (`VK_SUCCESS`/`VK_NOT_READY`). **The numeric fence states at the actual blocked calls were not captured**: transition-only logging missed the later same-configuration reset and the baseline minidump did not include the C heap objects. Program order establishes that collected work was fence-waited before a reusable slot was reset and that calibration drains before switching configuration; this inference is not a substitute for a live fence snapshot. Submitted/completed counters and native/Go call chains narrow the failure, but do not prove either an application synchronization defect or a driver defect.

No attributable application defect was established, so no speculative command-pool workaround or weakened watchdog was retained and no regression is claimed fixed. All temporary trace hooks were removed from shipping code. The next focused investigation is a low-perturbation full-state capture at command-pool reset/destruction, including live fence/slot state and graphics-layer interactions. The failure remains a reliability limitation of both the baseline and new binary despite successful synchronization-validation tests.

### 11.9 Correctness, sources and reproduction

PACE and stock-Go full GPU-enabled suites passed with Vulkan synchronization validation after the retained changes. The GPU package has **26 passing tests**, including the two new actual-shader raw-correction/projection tests; the optional phase-profile path also passed all 26 with synchronization validation. Existing coverage checks multiplication/squaring/reduction, noncanonical values and aliases, 516 complete encodings with sign recovery, candidate order, cursor continuation, center/tree transitions, exceptional point pairs, long/multiple-prefix false positives, reseeding/generations, capacity-one overflow, epoch exhaustion, independent saved keys, exact counts, cached submissions, calibration transitions and cancellation with 4,096 accepted streams/slow saves. Assertions were not weakened. Rejected subgroup/fusion/multiply prototypes passed this same GPU suite before screening; only subgroup size 32 was exercised.

PACE and stock-Go CPU-only suites passed native and `purego`. Custom vet passed Windows GPU+CGO, Windows `purego`, Linux GPU+CGO and Darwin arm64; the first Darwin invocation exceeded the tool timeout and a bounded retry completed. No standalone `go vet` or staticcheck was run. Stock-Go GPU tests/building, Linux amd64 PACE GPU/GNU dynamic, Linux arm64 stock-Go portable CPU and Darwin arm64 stock-Go CPU cross-builds passed. Linux GPU runtime and other GPUs remain untested. No dependencies or CPU algorithms were changed.

All four shaders were independently regenerated into ignored measurement files, passed `spirv-val` and matched the retained assets byte-for-byte:

| Asset | SHA-256 |
|---|---|
| `search.spv` | `95751da9e2251d81757e7099864d53192c6720aca2cecea6af458e1e09db9d7b` |
| `arithmetic.spv` | `6ae18467b2fd1b6dac9e3366a12b16e6344edaeeb14470092bf730952f1e82c6` |
| `diagnostic.spv` | `52220c2eddeb7b5f7181a60b698d79034dc7bc35cffa4ea8c694cdc4a24825b5` |
| `epoch.spv` | `388ef96074c572a469a3bc7280dfb1840ec19c76867001073a5a2aaf03339c69` |

A fresh executable rebuild differed from the preserved measured binary only in 22 bytes of PE timestamp/Go build-ID metadata; code/data and embedded production SPIR-V were identical. The preserved measured hash above, not a later timestamped rebuild's hash, identifies all final results. Test-only arithmetic/diagnostic/epoch modules are not embedded in the production CLI. The existing MIT notice for adapted arithmetic remains intact.

The requested external material was consulted for transferable methods, not predicted onino speedups:

- [Reviewed onino reference](https://github.com/coalaura/onino/tree/cd0192e2b9d55f5a0178a7a6f91895d414326cf6): local implementation/history, arithmetic contracts and prior negative experiments.
- [MoMA, CGO 2025](https://users.ece.cmu.edu/~franzf/papers/CGO_2025_Naifeng.pdf), multiword arithmetic and multiplication-selection sections: Karatsuba trades products for additions/comparisons/carries; its NTT results are for different workloads and widths.
- [Nath and Sarkar](https://www.aimsciences.org/article/doi/10.3934/amc.2020113), abstract, saturated/unsaturated representation tables and carry-chain discussion: formal accumulator reasoning motivates the local proofs; Haswell/Skylake CPU timings and carry-chain choices are not Vulkan forecasts. The bounds in Section 11.2 are independently derived for onino's full 256-bit input contract, not copied from a different representation's proof.
- [ZKProphet, IISWC 2025](https://arxiv.org/html/2509.22684v1), integer execution and occupancy analysis: distinguish dependency/throughput limits and register-limited occupancy instead of assuming higher occupancy always wins.
- [Decoupled Fallback](https://github.com/b0nes164/Decoupled-Fallback-Paper), portability and intra-workgroup scan discussion: subgroup-size-agnostic mappings and forward-progress requirements matter; bandwidth-bound addition-scan gains do not justify extra field multiplications or cross-workgroup spinning here.
- [CUB block scans](https://nvidia.github.io/cccl/unstable/cub/api/namespacecub_1abec44bba36037c547e7e84906d0d23ab.html): register memoization versus shared traffic and latency versus throughput motivate controlled experiments, not a direct multiplication-tree substitution.
- [Vulkan subgroup requirements](https://docs.vulkan.org/guide/latest/subgroups.html): query stages, operation support and sizes; do not infer subgroup membership from local invocation indices.
- [Nsight Shader Profiler](https://docs.nvidia.com/nsight-graphics/UserGuide/shader-profiler.html): native source correlation and achieved-utilization counters would address the remaining profiling gap; the unavailable tool's metrics are not fabricated from executable statistics.

Reproduction commands, from the repository root, with existing ignored directories and local runner (replace names to preserve raw runs):

```sh
builder build go --cgo --pace --dyn --compat --no-gen --output measurements/gpu20261013-rebuild.exe -tags gpu -pgo=off
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto,16384/4 --cpu off,all --duration 22s --pairs 5 --name 13-final-rare
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings 16384/16 --cpu off,all --duration 22s --pairs 5 --name 13-final-16
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto --cpu only --duration 18s --pairs 5 --name 13-final-cpu
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto --cpu off,all --duration 22s --pairs 5 --name 13-final-multiple --patterns somethingrare.,anotherprefix.,vanityaddress.,hellothereab.
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto --cpu off,all --duration 22s --pairs 5 --name 13-final-long --patterns somethingreallyrareabcdefghijklmnopqrstuvwxyz234567.
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto --cpu off,all --duration 22s --pairs 5 --name 13-final-frequent --patterns abcde.
go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-baseline.exe --compare measurements/gpu20261013-final.exe --settings auto,16384/4 --cpu off --duration 45s --warmup 25 --pairs 2 --name 13-final-warm
ONINO_GPU_PROFILE=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-final.exe --settings 16384/4 --cpu off --duration 12s --pairs 1 --name 13-profile
node scripts/gpu20261013-summary.js
```

Raw final records use `measurements/gpu20261012-13-final-{rare,rare-retry,16,cpu,multiple,long,frequent,frequent-retry,warm}-...{json,log}`. Retry groups contain one rare automatic pair per CPU mode and two frequent GPU-only attempts. Component groups are `13-{carry,prefix,center,child,compact}` plus their `-confirm` groups; rejection groups are `13-{subgroup,fusion,karatsuba,comba}`, with tuner confirmation `13-tie-confirm`. Native captures, minidumps, trace binaries, capacity fixtures and rejected prototypes stay in ignored `measurements/` and `scripts/`. The enhanced local runner preserves a `Failure` record and continues after watchdog termination instead of discarding later pairs. None of these fixtures is a build dependency.

For reproduction without local fixtures, use the real CLI/counter procedure in Section 10.9, the shader commands in [BUILD.md](internal/gpu/BUILD.md) and its validation opt-in. Use `--cpu all` with 32 available workers for combined, `--cpu off` for GPU-only, `--gpu off --cpu all` for CPU-only and omit size flags for automatic selection. The test/vet commands in Section 10.9 were rerun with the retained code; stock-Go cross-builds omit `--pace` for the two arm64 CPU targets. Builds, tests, profiles and throughput jobs ran sequentially. Retention is based on complete-search gains on this host; native-call reliability, all-core saving capacity, detailed GPU counters and cross-vendor runtime behavior remain open.

## 12. Systematic throughput study, 2026-10-13

### 12.1 Question, reference and retained result

Can the resident, three-phase Vulkan engine sustain more complete searches without transferring point generation to the CPU, compromising key independence, expanding the startup budget or weakening submission and persistence guarantees? This study implemented the A–O experiment program below. Its retained production changes are conditional carry/borrow correction (B), shared plus/minus prefix probes and single-round control flow (I), and a higher **explicit** stream limit (D). Automatic allocation still stops at 16,384 streams. No device-name selection, subgroup requirement, optimizer, alternative inversion, microbatch framework or tensor dependency was added to production. Optional native-call diagnostics from N aid investigation; they are not a stall fix.

The research reference is [`bcd43a8d77c812f2672b1b2f2904f36115d41027`](https://github.com/coalaura/onino/commit/bcd43a8d77c812f2672b1b2f2904f36115d41027). Work began at `0ad90f74c219e806b74a3336edef2ecdc3ac5565`, with a clean checkout. Relative to the reference, only research prose and blank lines in a test differed; production sources and embedded shaders were identical. Those subsequent changes were preserved. Section 11's approximately 9.238B/s GPU-only and 10.540B/s combined, and Prefix32's historical approximately 10.392B/s GPU-mode result, are context with different measurement procedures. They are not newly reproduced competitor measurements. Neither Prefix32 nor onionloom was run; no remote machine was accessed.

The preserved unchanged executable, `measurements/study-baseline.exe`, has SHA-256 `5c474d7d1ad7db22e60cc630b8c9bbd4202fbc6a63c1b04e9993cdb088dd922a` and size 4,995,072 bytes. Its four source-tree shader hashes are recorded in Section 11.9; only `search.spv` is embedded in the production CLI. Initial fresh 24-second baseline runs reproduced 9.356/9.236B/s at GPU-only 16384/4 and 10.537/10.426B/s combined with automatic configuration. Explicit 16384/4 combined was only 7.178/7.054B/s: a useful warning that GPU-only settings need not maximize combined throughput.

**Final automatic rare-prefix confirmation:** GPU-only improved from **9.328 to 9.656B/s** and combined from **10.655 to 10.972B/s**. Three paired runs give mean paired gains of **3.520%** and **2.975%**, with approximate 95% paired Student-t intervals of **[2.362%, 4.678%]** and **[2.146%, 3.804%]**. These small-sample intervals describe these runs, not universal hardware gains. Automatic frequent-match performance remains unresolved; both baseline and experimental executables exhibited native-call stalls elsewhere in the study.

### 12.2 Machine, method and measurement boundaries

All work used the same Windows 11 development machine: Ryzen 9 9950X3D, 32 logical processors, display-attached NVIDIA GeForce RTX 5090, driver **616.64**, 32,607 MiB reported device memory. The Windows Balanced power scheme, 575 W GPU power limit and existing display/driver configuration were left unchanged. The runner allowed all 32 logical CPUs; combined searches requested all 32 CPU workers throughout tuning and measurement. CPU-only used the existing automatic backend. Builds used Go/PACE **1.27.2**, PACE `newinliner,runtimefreegc`, `GOAMD64=v1`, CGO, `-tags gpu`, `-pgo=off` and builder's reproducible dynamic/compatibility settings. The shader compiler was SDK 1.4.328.1's glslc, reporting shaderc `v2023.8 v2025.3-10-gc7e73e8`, glslang `11.1.0-1302-gd213562e` and SPIRV-Tools `v2025.4 v2022.4-970-g19042c89`; every module used `--target-env=vulkan1.3 -O` and passed `spirv-val`.

Each prototype had a named parent, actual compiled-shader correctness gate, then alternating complete-CLI comparisons. Unless specified otherwise, the parent was the unchanged baseline, GPU-only 16384/4, `somethingrare.`, normal durable output, two pairs in AB/BA order, 20-second processes and a six-second warm-up. Early A used 24/8 seconds. These short screens use exact diagnostic counter deltas over the observed post-warm-up interval, usually 11–13 seconds; first-use compilation sometimes shortened it to seven seconds. Their intervals are **not identical**, so they reject large losses and nominate finalists rather than resolve sub-percent gains. Every completed sample is retained, including unmatched controls, cold observations and slower candidates.

Finalists used identical development-only snapshot instrumentation in both executables: after 15 seconds from search-action entry, read each registered monitor once under the monitor lock, collecting exact total/GPU completed and saved counters, then repeat after a target 60 seconds. CPU contribution is total minus that same GPU sample. Rates use the actual monotonic timestamp difference, not rounded progress output or rates from different windows. Processes received CTRL_BREAK at 78 seconds; final exact counts divided by process wall time include startup, cancellation, accepted-save drainage and teardown. The long automatic frequent-match check instead used 150-second windows and 168-second processes. Timer jitter is retained; CPU-only intervals were 60.008–60.113 seconds. Counter reads are snapshots, not a device-wide stop-the-world operation. The instrumentation was removed from shipping sources after building the measurement pair.

The fixed-window control is baseline-engine-equivalent, not byte-identical to the original preserved executable: it adds the same snapshot observer and disabled native-trace branch as the candidate. Its SHA-256 is `da2afffe20d185bb5404a9c2a562397982e4b062047a7f577b349a60165e52fc`. The clean integrated shader's instrumented executable is `e488d7a34c808c971b0ed377c95c403aac97eec3b4c8054d46ebd017fa648041`; its full name is `gpu20261013-study-integrated.exe`. The final production executable without snapshots is `study-production.exe`, 5,006,336 bytes, SHA-256 `af59c79187f63ffaa5d972b484ef5ae7b1ecc3ed358a09234c8f930062510a91`. The intended final `search.spv` was found byte-for-byte in both integrated and production executables, at offsets 4,703,360 and 4,694,656 respectively.

Builds, tests, profiles and timing jobs ran sequentially. Validation, detailed phase profiling, correction counters and native tracing were disabled for throughput comparisons. No dependency was installed. Independent longer confirmations followed the short screens; percentages from isolated changes were never added together. Error records and bounded watchdog terminations are not successful throughput samples. The local runner preserved 309 study JSON records: 304 completed and five failed, spanning screens, profiles and confirmations rather than one homogeneous reliability population.

### 12.3 Arithmetic, state and work model

Field values are eight little-endian 32-bit limbs, permitting the full interval `[0,2^256)`, with arithmetic reduced modulo `2p = 2^256-38`, then projected canonically modulo `p = 2^255-19` when required. A multiplication uses 64 source limb products; symmetric squaring uses 36. These are source operation counts, not native instructions. Folding receives carry at most 38, so its first adjustment is at most 1,444. The Fermat inversion chain remains **254 squares and 11 multiplications**, including zero-to-zero behavior. Canonical prefix projection and the complete host matcher are unchanged.

Each stream owns an independently generated seed, generation, cursor and affine center; its 128 offsets yield 256 surrounding candidates. Preparation stores the 127 internal denominator products plus the center-advance denominator, inversion processes independent roots and computes a provisional next center, and reconstruction recovers/filter-checks candidates. Only a no-hit round commits the next center. A hit pauses the stream until a generation-checked acknowledgement resumes or reseeds it. At most one saved key is exported per seed. Scalar headroom, full-key reconstruction, long-prefix false-positive resumption, bounded overflow publication and exact completed counts remain the existing contracts. Speculative inversion/next-center work is not counted as completed search.

The retained layout remains a 4,324-byte stream with a 4,096-byte, limb-major tree starting at byte 132, an 8 KiB logical shared field tree and two bounded in-flight slots. Per full no-hit stream-round, the useful scalar program has **1,301 field multiplications, 511 squares, 259 adds and 514 subtracts**, including inversion and center advancement. Consequently there are 2,071 calls to the small-carry fold and 514 subtraction corrections per 256 completed candidates. Compiler dead-code elimination removes unused preparation numerators; instrumentation must not accidentally make them live.

At the retained explicit maximum, `32768 * 64 * 256 = 536870912` candidates fit the 32-bit per-submission counter. A 32,768-stream state buffer is exactly **135.125 MiB**, versus 67.5625 MiB at 16,384. Existing storage-range, workgroup-count, heap-budget and duration checks still apply; results default to a bounded 256-entry capacity, with paused overflow streams drained rather than truncated. Explicit population does not enlarge automatic allocation or the 150 ms submission/120 ms aggregate-flight budgets.

### 12.4 Experiment dispositions and independent screens

Rates below are arithmetic means of two completed GPU-only screen samples in **B/s**, rounded to three decimals. They are screening evidence, not the final precise gain estimates. Each implemented variant passed the 26-test actual-Vulkan gate before these runs; compile/validation failures that preceded a corrected prototype are described below. “Inconclusive” means no improvement resolved, not a measured rejection of every possible implementation of the idea.

| Experiment | Parent and evaluated variants | Screen control → candidate | Disposition |
|---|---|---:|---|
| A: preserved-product reverse | Baseline → seven-barrier layout | 9.313 → 9.216 | Rejected alone |
| A: scheduling interaction | A → delayed X/Y numerators | 9.200 → 9.383 | Improved A, but not retained in the final combination |
| B: small correction | Baseline → overflow/underflow-conditional propagation | 9.277 → 9.437 | **Retained**, then confirmed with I |
| C: global alignment | Baseline → 160-byte tree offset, 4,352-byte stride | 9.293 → 8.886 | Rejected |
| D: inversion local size | Baseline 64 → 32 / 128 | 9.293 → 9.336 / 9.286 → 9.278 | Inconclusive; retain 64 |
| D: stream population | 16384 → 24576 / 32768 / 49152, four rounds | 9.298 → 9.316 / 9.222 → 9.467 / 9.232 → 9.463 | 32768 **retained as explicit limit** after confirmation; no higher default |
| D: cooperative inverse | Baseline → two / four lanes per inverse | 9.311 → 5.188 / 9.278 → 5.106 | Rejected |
| E: repeated-square loops | Baseline → retained / full / groups-of-five unrolling | 9.297 → 9.290 / 9.275 → 9.139 / 9.288 → 9.185 | Retained-loop hint inconclusive; full/partial variants rejected |
| F: modular inversion | Baseline → 741 ordinary / 590 half-delta divsteps | 9.314 → 5.799 / 9.284 → 6.123 | Rejected for throughput |
| G: weighted microbatches | Baseline → two / four serial pairs per invocation | 9.307 → 7.922 / 9.306 → 7.810 | Rejected |
| G: paired-coordinate identity | Baseline → two-pair alternative with exceptional fallback | 9.295 → 7.023 | Rejected |
| H: checkpoints | Baseline → retain 64 / 32 global tree nodes | 9.273 → 9.104 / 9.283 → 8.939 | Rejected |
| H: selected intermediate | Integrated B+I → 64 nodes plus 128 mixed products | 9.679 → 9.324 | Rejected |
| I: shared filtering | Baseline → one probe loop for plus/minus | 9.299 → 9.408 | **Retained** |
| I: production contract | Baseline → single-round control | 9.303 → 9.488 | **Retained** |
| I: single-prefix specialization | Shared-filter parent → specialization constant for count one | 9.446 → 9.398 | No improvement resolved; removed |
| J: center candidate | Baseline → 257 candidates per center | 9.303 → 9.230 | Rejected |
| K: complement products | Baseline → precompute all leaf complements | 9.302 → 9.082 | Rejected; no overlap claimed |
| L: schedule search | Six bounded candidates, details below | Best isolated early-numerator screen 9.281 → 9.446 | No additional variant survived combination confirmation |
| M: integer matrices | Baseline → complete tensor-assisted preparation multiplication | 9.332 → 3.938 | Rejected; hardware/compiler supported the prototype |
| N: command recycling | Baseline → per-slot pools with individual / pool reset | 9.315 → 9.346 / 9.277 → 9.327 | Throughput inconclusive; failed reliability confirmation; reset changes removed |
| O: sustainable saving | Baseline → temporary OS-thread pin / parallel file flushes | See saving measurements below | Inconclusive; production changes removed |

**A — read/write separation and scheduling.** Original internal products occupy shared slots 1–127; reconstructed internal inverses occupy 129–255. Copy the already inverted root to 129. For levels with `size = 1,2,4,8,16,32`, child `node = 2*size+lane` reads its parent inverse at `128+node/2` and original sibling at `node^1`, writes `128+node`, then all lanes barrier. Destinations at each level are disjoint from both read sets. After the sixth barrier no original internal product is needed. Overwrite lower slots 0–127 with recomputed leaves, barrier, then produce each final inverse directly in registers from upper parent `128+(128+lane)/2` and lower sibling `lane^1`. This is 126 internal plus 128 final multiplications, seven reverse barriers and the same 256 field slots. Paused-stream participation is uniform; diagnostic global nodes and hit publication are separate. The layout was slower alone. Delaying X/Y multiplication until after the reverse tree helped this layout, whereas the same delay alone was slower (9.320 → 9.081B/s). This is a measured scheduling interaction, distinct from the previous subgroup reconstruction experiment.

**B — bounded conditional correction.** Add the adjustment to limb zero first; propagate through limbs 1–7 only if that addition overflows. For subtraction, subtract the initial borrow times 38 from limb zero, and propagate only its underflow. The condition is not the ordinary field add/subtract carry. If the whole fold wraps, its value is at most 1,443, so the existing final `+38` is bounded to the low limb. If the whole subtraction correction borrows, the wrapped low limb is at least `2^32-38`, so its final `-38` cannot borrow again. Full-range noncanonical inputs and aliases therefore retain the previous contract. Actual shader tests now cover every 32-bit carry boundary, all fold carries 0–38, subtraction boundaries and 64-operation dependent chains against `math/big`.

Separate rare-path counters observed **45,868 fold propagations / 637,466,656,768 fold calls** and **1,038 subtraction propagations / 158,212,390,912 subtraction calls**, over 78,798,389,248 completed candidates: approximately `7.20e-8` and `6.56e-9`. Counts were packed into a per-submission diagnostic word, with observed values safely below each 16-bit field's capacity. This instrumented run is not a throughput sample. An earlier counter prototype made otherwise-dead preparation arithmetic observable; its biased denominator was discarded explicitly and the raw record preserved. SPIR-V contains the conditional path. Native instruction representations were unavailable, so whether the driver branches or predicates every particular correction is unmeasured; source savings are not an emitted-instruction count.

**C — alignment.** Seven padding words move the tree from byte 132 to 160 and stride from 4,324 to 4,352, adding exactly 448 KiB at 16,384 streams. Shader layout, C allocation/range constants and diagnostics changed together and passed the compiled tests. The approximately 4.4% loss removes this layout. No sector/cache counters were available, so fewer sector requests were a hypothesis, not an observation. A separate metadata/tree allocation was not additionally implemented: the conditional follow-up required profiling support that was unavailable. This is unrelated to the previously neutral shared-memory transpose.

**D — independent work and cooperation.** At 16,384 streams there are 512 NVIDIA warps of scalar inversion work in total; changing 32/64/128-thread workgroups changes distribution, not that total. All three legal sizes were actually tested. Larger populations were separately tested at 24,576, 32,768 and 49,152; an experimental 65,535 bound still limited worst-case work to 1,073,725,440 candidates, but 65,535 itself was not timed. The final 32,768 limit is explicit-only, with measured initialization/memory costs below. Two- and four-lane prototypes distributed product columns across contiguous subgroup tiles, summed each column in three 32-bit words, shuffled them and reconstructed carries in each participating lane. At most eight 64-bit products fit below `2^67`; square cross terms explicitly retain the doubled high bit. The same Fermat chain was used, including conversion/communication costs, with only the tile leader publishing the result. Each product required 48 scalar shuffle components plus replicated carry/reduction work. Subgroup-size divisibility was checked in the shader with scalar fallback; the experimental module required shuffle-capable hardware, verified on this device. No such optional module ships, and its capability fallback was not runtime-tested on another subgroup width. The 44–45% losses reject these implementations, not all cooperative big-integer algorithms.

**E and L — instruction footprint and schedule search.** Repeated-square hints and short limb-loop hints were independent experiments. `GL_EXT_control_flow_attributes` is supported by the installed compiler; no draft extension was introduced. The retained-loop and full-unroll square-chain modules each had 150,112 SPIR-V bytes and 9,331 instructions, with nine `DontUnroll` or `Unroll` loop controls replacing `None`. Groups of five squares grew this to 249,904 bytes/15,568 instructions, with eight `DontUnroll` loops. These facts establish that hints survived translation, not that native execution was unrolled as intended. Native hot instruction delivery and cache stalls were unavailable.

The development-only L search enumerated six justified transformations with seed **20261013** and a fixed budget of two alternating pairs plus the full correctness gate per candidate: delayed numerators, early numerator sums, swapped final multiply operands, explicit short-loop unroll, explicit short-loop retention, and addition-chain temporary reuse. Fisher–Yates ordering used a seeded `System.Random`. Their candidate means were respectively 9.081, 9.446, 9.320, 9.430, 9.373 and 9.312B/s against fresh controls near 9.26–9.32. All passed correctness; none was accepted merely for fewer source temporaries. Early numerators and limb hints nominated finalists. Early numerators added to B+I did not beat B+I; a fresh 60-second comparison of B+I against B+I plus limb unroll measured 9.593 versus 9.569B/s. No extra schedule variant was retained. The search objective was complete-search throughput, not shader byte size; no optimizer or search runs at application startup.

**F — bounded divsteps.** Both prototypes used nine signed radix-`2^30` limbs for `f,g`, and field-representation modular coefficients `v,r`, initially `(p,a,0,1)` after normalizing `a`. Ordinary delta starts at 1 and uses 741 steps; half-delta starts at 1/2, represented as integer twice-delta, and uses 590 steps. With `odd = g&1`, the positive-delta odd case maps `(f,g)` to `(g,(g-f)/2)` and `(v,r)` to `(r,(r-v)/2 mod p)`; otherwise `g=(g+odd*f)/2` and `r=(r+odd*v)/2 mod p`. Delta changes by `1-delta` in the first case and `1+delta` otherwise. These are the exact algorithms described by the pinned safegcd explanation, whose 256-bit bounds safely cover this 255-bit prime; the 590 bound is not reused for ordinary delta. The invariant `|f|,|g|<=p` bounds pre-halving sums by `2p`; signed 64-bit digit temporaries safely hold normalization carries. Modular halving canonicalizes, adds `p` if odd, then shifts; the value is at most `2p-2`. Final sign adjustment yields the inverse, and zero returns zero.

These are explicit one-divstep matrix updates, not a claim to reproduce libsecp256k1's optimized batched 30-step inverter. Conversion, normalization, coefficient updates and output conversion all ran in the shader and are included in complete-search rates. They replace 254S+11M with hundreds of serial digit/normalization/halving updates and divergent decisions. Correctness passed, but complete-search throughput fell 34–38%; no throughput claim is inferred from CPU assembly results or the EUROCRYPT paper. The prototypes were written from the equations; the existing adapted-arithmetic MIT notice remains intact.

Separate 16-second phase profiles confirmed the cost in the complete inversion phase: ordinary divsteps averaged **361.156 microseconds/round** versus **76.410** for its Fermat control (21,791 versus 35,167 profiled rounds); half-delta averaged **322.620** versus **77.040** (22,931 versus 34,959). These phase boundaries include representation conversions, normalization, next-center work, synchronization and query overhead. They are profiling observations, not additional uninstrumented throughput samples.

**G — GPU-resident weighted microbatches.** Two and four pairs per invocation reduced the cooperating tree to 64 and 32 leaves. Each invocation formed `2b` denominators and weighted numerators, accumulated its local product, joined the workgroup tree, then walked its local candidates backwards after receiving the inverse. Centers and advancement stayed on the GPU. Two private arrays require `128b` bytes of logical field storage per invocation before other live values; no measured spill count is available. For `b` pairs, forming complete numerators/denominators takes `3b` multiplications; the local denominator prefix and numerator weights each need `2b-1`, and the reverse output/propagation needs `4b-1`. Preparation can eliminate unused numerator work. The group tree adds `L-1` forward and `2L-2` reverse products with `L=128/b`; there is still one inverse per stream, so serialization does not magically improve inversion amortization. Fewer global tree nodes and barriers traded against additional multiplication, private state and serial dependence. The measured two/four-pair losses bounded expansion; larger private arrays were not pursued.

The alternative identity used `y+ = (center.xy-offset.x*offset.y)/(center.x*offset.y-center.y*offset.x)` and the corresponding plus numerator/denominator for `y-`. A zero denominator must not enter a neighbor's product. The prototype conservatively checked canonical low 64 bits for zero, used the complete formula for those candidates, and substituted denominator one with the already recovered ordinate. This may do unnecessary work for a nonzero denominator with zero low word, but never skips or poisons candidates. Actual diagnostic shaders tested exceptional centers at both signs of an offset, every generated public key and sign. This organization, including the fallback and extra packing/recomputation, was slower; it is not a repetition of the earlier differently organized alternative-Y experiment.

**H — checkpoints and selective storage.** Storing only 64 or 32 global nodes shrank scratch from 4,096 to 2,048 or 1,024 bytes per stream. Reconstruction rebuilt omitted parents from recomputed leaves, costing 64M plus one barrier or 96M plus two barriers before ordinary reverse traversal. A further useful intermediate point kept 64 nodes plus all 128 mixed products: 6,144 scratch bytes, 6,372-byte stride, 2,048 extra bytes written and read per stream, 128 avoided mixed multiplications but 64 added parent multiplications and one barrier. Original nodes 0–63 and mixed values 64–191 are disjoint; inter-dispatch dependencies publish both. All three layouts lost, including the selective-storage variant tested against the integrated parent. None justified combination with A, which itself had not survived combination confirmation.

**I — filtering and production control.** One runtime probe load/loop now tests both canonical plus/minus words and accumulates two booleans. Probe values remain runtime data; multiple, overlapping, duplicate and long prefixes preserve the exact necessary-condition relation to host matching. A separate specialization constant for exactly one probe passed tests but did not improve its shared-filter parent and was removed. Preparation/reconstruction each receive one round from the bridge, so their redundant round loop became a uniform `if (running)`. The initial buffer barrier is needed only when preparation applies commands; reconstruction has no corresponding writes to publish there. Shared winner barriers, final global hit/diagnostic publication barriers and Vulkan inter-dispatch dependencies remain. Shader barriers and Vulkan dependencies were not conflated.

**J — the omitted center.** The prototype appended center position 256 after the existing 256 surrounding candidates; sentinel/cursor became 257, and center hits used the center's step and sign. Each center now enumerated the complete scalar interval `[center-128,center+128]`; centers advance by 257, so intervals are disjoint and scalar headroom is unchanged. Candidate/reference indexing, epoch counts, zero-remaining-candidate resumption and diagnostic full encodings were updated and tested together. The upper bound of `1/256 = 0.390625%` extra arithmetic-free candidates did not cover filtering/control costs: the screen was approximately 0.8% slower. All enumeration/state/test changes were removed rather than retaining complexity for an unresolved benefit.

**K — complements.** Preparation kept the original root and computed each leaf's product of all other denominators by a reverse traversal starting from one. Reconstruction then multiplied the stored complement by the root inverse. Relative to normal reconstruction this adds 128 field multiplications per stream and 4,096 bytes each of global storage, writes and reads; stride became 8,420 bytes. It uses legal bounded dispatches and ordinary dependencies. Complements were computed before inversion, not concurrently with it, so no actual overlap is claimed. A shorter inverse-dependent path did not improve complete search. The evaluated applicable idea is rejected; a concurrent-overlap implementation was not tested and has no measured benefit claim.

**M — exact tensor-assisted preparation.** This machine exposes `VK_KHR_cooperative_matrix`, shader int8 and unsigned-8×unsigned-8→unsigned-32 shapes including M16N16K32. The prototype therefore ran, rather than being declared unsupported. For 16 offsets sharing a center operand, a 16×32 Toeplitz matrix of center bytes times a 32×16 matrix of offset bytes computes 16 convolution coefficients per output column. Four degree blocks cover 64 coefficients; each 32-lane subgroup handles two offset tiles, eight matrix operations, and the workgroup handles all 128 mixed products. A coefficient is at most `32*255^2 = 2080800`; propagated byte carry is at most 8,160, so unsigned 32-bit accumulators are exact. All packing, subgroup synchronization, stores, base-256 carries, 512-bit product assembly and the existing reduction were included. Additional shared storage was 20 KiB, on top of the ordinary tree. The host checked/enabled matrix/int8/memory-model features; the first validation gate identified a missing Vulkan memory-model feature, corrected before the passing gate and timing. A scalar subgroup-size fallback existed, but the experimental module itself required the queried matrix capability. The complete-stage 58% loss removed it and all optional requirements. TensorZKP's different field/workload is methodological motivation, not an onino throughput forecast.

### 12.5 Combinations and complete-search confirmation

B+I's independent combination screen reached 9.648B/s versus 9.302 baseline. Adding A with delayed numerators reached 9.588, and adding early numerator sums reached 9.625, against their own fresh baseline controls. These did not establish an additional benefit over B+I. The clean integrated module was regenerated without experimental switches and confirmed afresh; the table below refers to that module, not an earlier prototype with dead branches.

Means and sample standard deviations are in B/s; `n` counts alternating pairs. “Full” divides exact final completed work by the whole process interval. The CPU-only row is a separate three-run observation, not a rate added to another window.

| Workload / setting | n | Steady control | Steady integrated | Full control → integrated |
|---|---:|---:|---:|---:|
| Rare, GPU-only auto | 3 | 9.328 ± 0.028 | 9.656 ± 0.049 | 9.236 → 9.480 |
| Rare, combined auto | 3 | 10.655 ± 0.013 | 10.972 ± 0.024 | 10.374 → 10.686 |
| Rare, GPU-only 16384/4 → 32768/4 | 3 | 9.278 ± 0.026 | 9.782 ± 0.029 | 9.240 → 9.723 |
| Four prefixes, GPU-only auto | 2 | 9.203 ± 0.084 | 9.572 ± 0.028 | 9.113 → 9.472 |
| Four prefixes, combined auto | 2 | 10.004 ± 0.039 | 10.359 ± 0.011 | 9.709 → 10.062 |
| Long prefix, GPU-only auto | 2 | 9.307 ± 0.025 | 9.639 ± 0.104 | 9.208 → 9.537 |
| Long prefix, combined auto | 2 | 10.686 ± 0.030 | 11.012 ± 0.001 | 10.387 → 10.697 |
| Frequent, GPU-only 16384/4 | 2 | 9.258 ± 0.006 | 9.637 ± 0.020 | 9.183 → 9.573 |
| Frequent, combined 16384/4 | 2 | 2.428 ± 0.056 | 2.461 ± 0.034 | 2.414 → 2.425 |
| Frequent, GPU-only auto, 150 s | 3 | 8.959 ± 0.255 | 9.030 ± 0.661 | 8.907 → 8.983 |
| CPU-only rare, all workers | 3 | 1.325 ± 0.002 | CPU engine unchanged | Separate measurement |

In rare combined auto, GPU contribution was 9.332 → 9.648B/s and CPU contribution 1.323283 → 1.324003B/s from the same snapshots. Earlier 60-second explicit-16384/4 combined confirmation did **not** resolve a gain: controls 7.504/7.209/7.239B/s versus B+I 7.201/7.362/7.275. Keep requested CPU workers and tune for combined work rather than presenting the GPU-only configuration as a combined optimum. The final manual-population row's paired gain was 5.434%, but includes both shader changes and population. An earlier B+I-versus-B+I 16384/32768 comparison isolated an approximately 2.1% population increment. The automatic default was not raised from that single-machine observation.

### 12.6 Frequent matches, persistence and native calls

**O — targeted host work.** Separate instrumentation measured GPU verification around 16.5 microseconds and reseeding around 20–30 microseconds. Real `Store.Save` averaged approximately 2.78 ms with GPU-only search and 13.27 ms under all-worker contention; the GPU save callback including serialization wait averaged 26.88 ms combined. Profile counters included 7,723 GPU-only saves and 1,086 combined GPU saves; these are stage-cost observations, not benchmark capacities. Output reporting was much smaller than persistence. No worker was reserved or moved into a point feeder.

Two actual prototypes followed: temporarily lock the saving goroutine to its OS thread, and concurrently flush the three key files with two helper goroutines plus the calling goroutine, joining all errors before rename. Queues stayed bounded and successful return still required all file syncs and durable normal publication. Both passed GPU and storage tests. In 40-second two-pair fixed frequent screens, thread pinning changed combined 2.425 → 2.510B/s with noisy controls; parallel files changed 2.468 → 2.482B/s and one GPU-only attempt stalled. Neither established sustainable benefit sufficient to retain a persistence/scheduling change. Both were removed; no larger saver queue or weaker saving guarantee was used to create apparent throughput.

The promising thread-pin screen received a separate three-pair, fixed-60-second combined confirmation against the integrated B+I parent. All six processes completed after passing GPU/storage tests. The entire later group operated in a substantially slower saving regime: control **0.910 ± 0.014B/s**, candidate **0.945 ± 0.015B/s**, and whole-process **0.899 → 0.925B/s**. The paired completed-candidate gain was `3.868% ± 2.970 percentage points` at approximate 95% confidence, but durable saves were only **27.450 ± 0.539 → 27.866 ± 0.636/s**, with no consistent paired saving gain. Warm process CPU was 9.12–9.95 versus 9.58–10.58 cores despite requesting all 32 workers; exit took 1.78–2.37 seconds. The cause of the whole group's degradation relative to earlier runs was not established, so these six records are preserved separately rather than pooled into the final shader comparison or silently excluded. The thread-pin result remains inconclusive for the intended sustainable-save objective and was removed.

In the clean final fixed frequent runs, GPU-only durable saves increased from **277.122 ± 0.152 to 289.256 ± 1.192/s** over the measurement window. Whole-process means were **274.471 → 286.858 saves/s**, including drainage. Combined was **72.660 ± 1.723 → 73.065 ± 0.620 saves/s**, with whole-process means **72.289 → 71.908/s**: no saving improvement resolved. Its completed-candidate paired-gain interval was roughly `1.382% ± 8.552 percentage points`, also inconclusive. Integrated combined drainage took 74.599–83.065 ms and exit 232.794–292.246 ms; GPU-only drainage took 4.639–14.079 ms. Approximately 67.5–68.0 of 78 seconds of combined GPU inter-submission gaps is consistent with host saving/backpressure dominating this workload.

Automatic frequent results must be kept separate. Earlier B+I GPU-only windows ranged from 6.211 to 9.599B/s; the 6.211 run selected 16384/3, spent nearly its entire controller lifetime in GPU execution and had sampled backlog eight, so host-idle speculation does not explain it. Fresh clean 150-second controls were 9.174/9.026/8.677B/s; candidates were 9.445/9.377/**8.267**. Durable save means were 266.618 ± 8.583 versus 269.626 ± 19.740/s. All six completed, but the third candidate lost and variation dwarfs the mean gain. This is **inconclusive automatic frequent-match behavior**, not a uniform improvement or a growing-backlog success.

**N — legal recycling, unresolved reliability.** One command pool per in-flight slot was implemented twice: retain individual command-buffer reset, then reset the whole slot pool when rerecording. No-hit recorded-command reuse was preserved. The slot cannot be submitted twice; collection waits its fence before clearing pending state, and initialization waits the first slot's fence. A new unused slot may legally have an unsignaled fence: fence status alone is not evidence that a command buffer is pending. No pool was reset with pending commands. Both variants passed synchronization validation. Short throughput differences were inconclusive, and extended frequent runs did not improve reliability or combined throughput.

Five unsuccessful attempts were preserved with logs, exact completed snapshots when available, native stacks and minidumps: one unchanged-baseline automatic frequent run, two per-slot-pool automatic frequent runs, one parallel-file GPU-only run, and one B+I fixed frequent confirmation. The 60-second pool runs were killed near 90.03 seconds after cancellation grace, the 40-second file run at 70.12 seconds, and the 78-second B+I run at 108.03 seconds. They produced no final accounting and are never classified as sustainable completed samples. The third B+I control remained in the record despite its unmatched failed candidate. Later clean final runs all completed; that does not prove the earlier issue fixed.

Section 11 had resolved blocked calls to `vkResetCommandBuffer` and `vkDestroyCommandPool`; this study investigated those separately from GPU timestamps. Successful verbose pool-reset traces contained 26,868 and 27,051 pre-reset records, all with pending false and either a completed fence or an unused unsignaled fence. Those traced attempts did not stall, and their throughput was excluded as perturbed diagnostics. New failed captures showed driver yielding/spinning frames but did not independently resolve every stripped C return address to a specific API call. Thus there is no newly captured ownership violation or demonstrated root cause. Loaded graphics hooks are not causal evidence. The per-slot reset changes were removed. Shipping opt-in `ONINO_GPU_NATIVE_TRACE=1` records slot/pending/fence state and reset entry/return; `--gpu-diagnostics` now brackets pool destruction separately. These additions expose future blocked calls without changing reset policy or claiming a repair.

### 12.7 Profiling, startup and resource costs

Nsight Graphics GPU Trace/Shader Profiler was not installed. No CUDA profiler was represented as profiling Vulkan. Active lanes, achieved occupancy, eligible warps, integer pipeline pressure, dependency stalls, bank conflicts, cache sectors, genuine spills and instruction-delivery stalls remain **unmeasured**. Available evidence is actual shader correctness, SPIR-V, Vulkan timestamps, host counters and native executable statistics. The driver returned zero internal/native instruction representations and implausible local-memory totals around 68.7 GB; those are not spill measurements.

Separate 24-second phase profiles at 16384/4 measured 53,079 control and 54,495 B+I rounds. Preparation/inversion/reconstruction shares were **23.598 / 17.370 / 59.032%** versus **24.145 / 17.676 / 58.179%**. Per-round times were **104.695 / 77.066 / 261.904 microseconds** versus **104.329 / 76.375 / 251.390 microseconds**; boundaries include synchronization and profile-query overhead. Reconstruction remains the largest measured phase, without proving a specific execution-pipeline bottleneck.

| Native executable statistic, control → integrated | Preparation | Inversion | Reconstruction |
|---|---:|---:|---:|
| Registers/thread | 63 → 63 | 72 → 64 | 96 → 92 |
| Shared bytes | 8212 → 8212 | 3392 → 2880 | 9760 → 8212 |
| Native code bytes | 34688 → 35712 | 183552 → 189824 | 64512 → 64512 |
| Reported stack bytes | 0 → 0 | 0 → 0 | 0 → 0 |

These are resource/compiler statistics, not measured occupancy or executed instruction mix. The larger inversion program did not establish instruction-cache pressure. One-hertz telemetry during profiling, kept separate from throughput, observed P0, 45–70°C, graphics clocks 2430–2887 MHz, memory 14201 MHz, power 63.47–579.21 W, and clock-event reason masks `0x400`/`0x4` under the unchanged 575 W limit. This is sampled operating context, not a guarantee that every throughput run had identical clocks or no throttling.

| Lifecycle/resource observation | Control | Integrated |
|---|---:|---:|
| Warm GPU-only auto first useful submission | 138.421–145.003 ms | 138.379–138.820 ms |
| Warm GPU-only auto selection complete | 4.631–4.635 s | 4.616–4.647 s |
| First-seen integrated module first work / selection | — | 1.775 / 6.281 s |
| That run's synchronous pipeline creation | — | 1.632 s |
| Combined auto first work / selection | Same bounded procedure | 352–404 ms / 4.843–4.898 s |
| Warm GPU-only auto host CPU | 0.022–0.037 cores | 0.026–0.037 cores |
| Manual 16384/4 → 32768/4 host CPU | 0.026–0.028 cores | 0.013–0.018 cores |
| Manual first work / selection | 288–297 / 291–301 ms | 434–447 / 440–454 ms |
| Manual peak working set | 85.945–86.281 MiB | 93.004–93.211 MiB |
| Manual private process memory | 206.383–208.414 MiB | 291.438–294.402 MiB |
| Manual command upload bytes | 1,769,472 | 3,538,944 |
| Manual lifetime readback, 78 s | 264.252–265.681 MB | 139.019–139.764 MB |
| Manual largest submission | 2.217–2.387 ms | 4.200–4.288 ms |
| Manual accumulated submission gaps | 289.120–294.103 ms | 156.566–161.004 ms |
| Manual process-exit latency | 38.142–39.698 ms | 39.904–41.713 ms |

“First work” is the first useful submission, not its completion. Cold means the observed first use of the newly generated module; no reboot or driver-cache flush was imposed, so fully OS-cold latency is untested. The first-seen candidate also consumed 0.359 host cores during its nominal steady window and 0.444 over the full process, versus the smaller warmed observations; this sample was included, not silently relabeled or excluded. Application-controlled selection stayed under ten seconds in completed confirmations, while previously observed native blocking calls remain outside a hard application deadline.

Automatic GPU-only peaks varied with driver activity: integrated working set 265–363 MiB and private memory 494–547 MiB; combined was about 99–100 and 238–242 MiB. Automatic GPU readback was 286–553 MB per 78 seconds versus about 53 MB combined, reflecting different selected rounds; uploads were 1.769 MB. GPU-only automatic gaps totaled 474–763 ms, combined rare gaps 2.027–2.244 seconds. The integrated matrix's maximum sampled submission was 20.613 ms, below the unchanged guards; integrated automatic exit ranged 43.944–51.894 ms GPU-only and 92.522–238.346 ms combined. These tails include host teardown, which is not bounded by a GPU timestamp. The CPU-only check consumed approximately 31.35 cores; combined rare approximately 31.30–31.34, with all 32 requested workers still active.

### 12.8 Correctness and portability

Each successful prototype ran the actual compiled search, arithmetic, full-encoding diagnostic and epoch shaders before timing. The 26-test Vulkan gate covers independent multiplication/squaring/reduction/inversion and canonical references; zero, modulus boundaries, all 256-bit representations and aliases; complete public keys and signs through center transitions and exceptional point pairs; ordinary/frequent matches; multiple, overlapping, duplicate and long prefixes; necessary-condition false positives and cursor resumption; capacity-one overflow; paused streams and stale generations; reseeding, exhaustion, scalar headroom and independent saved keys; calibration/speculative-work accounting, cancellation, slow saves and drainage. B adds partial/full carry chains at each limb boundary and 64-step arithmetic chains. J temporarily updated both enumeration and reference fixtures, then restored them when rejected. An initially missed J prefix reference and M's missing memory-model feature were corrected before the passing gate; no test was weakened.

Final Windows PACE and stock-Go GPU-enabled full suites passed with synchronization validation: 403 of 406 reported tests passed and three were skipped; all 26 GPU tests ran and passed on the RTX 5090. GPU-disabled native and actual `purego` suites passed under PACE and stock Go. Custom vet passed Windows GPU/CGO, Windows portable and `purego`, Linux GPU/CGO and Darwin arm64. No standalone `go vet` or staticcheck was run. Initial externally imposed test-tool timeouts were followed by waiting for owned processes to finish and sequential successful retries; they are not GPU correctness passes by themselves.

Linux amd64 GPU cross-builds passed with both PACE and stock Go, GNU dynamic linking, a verified ELF interpreter and libc dependency. Linux arm64 stock-Go `purego` and Darwin arm64 stock-Go CPU cross-builds passed. Those are compilation checks, not Linux/macOS GPU runtime validation. The second local Vulkan device, AMD Radeon integrated graphics, ran the suite: 24 top-level tests passed, but `TestCancellationDrainsOverflow/4096` returned the conservative submission-budget error despite saving and accounting for all 4,096 accepted candidates. That parent/subtest failure is retained; the guard and assertions were not weakened. AMD validation is therefore partial, and no AMD performance claim or full supported-hardware conclusion follows. Experimental subgroup/tensor variants were runtime-tested only at NVIDIA subgroup size 32; other sizes and fallback paths remain untested.

All four final modules were regenerated independently into ignored files, validated and matched byte-for-byte:

| Asset | SHA-256 |
|---|---|
| `search.spv` | `d67dd1ab2dceea64b6a30310d7089c62b413f7dddace4ce9d8027bbc8c390c86` |
| `arithmetic.spv` | `b8996d1fd1cb3403e25c29995292fa143385f468d9bcc9110cf736f7c7ec3358` |
| `diagnostic.spv` | `222fe68f86eebd04b8af3aa0fe490b8c4e8b1e4a89737c88eba19d9544bded3a` |
| `epoch.spv` | `dfb8fa2550aea7b084dee163ab72ea74d8371d07448467d35e07590f725a7d54` |

### 12.9 Reproduction and source applicability

Run [BUILD.md's shader-generation commands](internal/gpu/BUILD.md#shader-generation) first when editing GLSL. These are the final build/check commands, from the repository root, using the already installed SDK validation layer:

```sh
builder build go --cgo --pace --dyn --compat --no-gen --output measurements/study-production.exe -tags gpu -pgo=off
ONINO_VULKAN_TEST=1 VK_LAYER_PATH="C:/Users/Laura/onino/tools/vulkan-sdk/Bin" VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT builder test go --cgo --pace --dyn --compat --no-gen -tags gpu -vet=off -count=1 -p=1 ./...
ONINO_VULKAN_TEST=1 VK_LAYER_PATH="C:/Users/Laura/onino/tools/vulkan-sdk/Bin" VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT builder test go --cgo --dyn --compat --no-gen -tags gpu -vet=off -count=1 -p=1 ./...
pace test -vet=off -count=1 ./...
go test -vet=off -count=1 ./...
pace test -vet=off -tags purego -count=1 ./...
go test -vet=off -tags purego -count=1 ./...
vet --tests --tags gpu --cgo
vet --tests --tags purego
vet --tests --os linux --tags gpu --cgo
vet --tests --os darwin --arch arm64
builder build go linux --arch amd64 --cgo --pace --gnu --dyn --compat --no-gen --output measurements/study-linux-pace -tags gpu -pgo=off
builder build go linux --arch amd64 --cgo --gnu --dyn --compat --no-gen --output measurements/study-linux-stock -tags gpu -pgo=off
builder build go linux --arch arm64 --no-gen --output measurements/study-linux-arm64 -tags purego -pgo=off
builder build go darwin --arch arm64 --no-gen --output measurements/study-darwin-arm64 -pgo=off
```

The unchanged baseline build used the first command with `--output measurements/study-baseline.exe` at the starting revision. Preserve that file rather than overwriting it. Ordinary CLI reproduction needs no ignored fixture: run `measurements/study-production.exe --gpu auto --cpu off --gpu-diagnostics --output measurements/reproduction somethingrare.`, interrupt at the chosen process boundary, and use exact final counts with full wall time. Repeat with `--cpu all`; for CPU-only use `--gpu off --cpu all`; for manual population use `--gpu-streams 32768 --gpu-rounds 4`. For fixed-window reproduction, implement the two monitor snapshots described in Section 12.2 identically in control and candidate, or use the preserved local development executables. Do not estimate these rates from rounded five-second progress reports.

The local fixed-window and screen commands were:

```sh
go run scripts/gpu20261012-cli.go --exe measurements/study-baseline.exe --settings 16384/4,auto --cpu off,all --duration 24s --warmup 8 --pairs 2 --name study-baseline
ONINO_STUDY_FIXED=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-window-control.exe --compare measurements/gpu20261013-study-integrated.exe --settings auto --cpu off,all --duration 78s --warmup 14 --pairs 3 --name study-final-auto
ONINO_STUDY_FIXED=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-window-control.exe --compare measurements/gpu20261013-study-integrated.exe --settings 16384/4 --compare-settings 32768/4 --cpu off --duration 78s --warmup 14 --pairs 3 --name study-final-large
ONINO_STUDY_FIXED=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-window-control.exe --compare measurements/gpu20261013-study-integrated.exe --settings 16384/4 --cpu off,all --duration 78s --warmup 14 --pairs 2 --name study-final-frequent --patterns abcde.
ONINO_STUDY_FIXED=1 ONINO_STUDY_DURATION=150s go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-window-control.exe --compare measurements/gpu20261013-study-integrated.exe --settings auto --cpu off --duration 168s --warmup 14 --pairs 3 --name study-final-frequent-auto --patterns abcde.
ONINO_GPU_PROFILE=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-window-control.exe --compare measurements/gpu20261013-study-window-bi.exe --settings 16384/4 --cpu off --duration 24s --warmup 8 --pairs 1 --name study-phase-profile
ONINO_GPU_PROFILE=1 go run scripts/gpu20261012-cli.go --exe measurements/study-baseline.exe --compare measurements/gpu20261013-study-f1.exe --settings 16384/4 --cpu off --duration 16s --warmup 6 --pairs 1 --name study-phase-f1
ONINO_GPU_PROFILE=1 go run scripts/gpu20261012-cli.go --exe measurements/study-baseline.exe --compare measurements/gpu20261013-study-f2.exe --settings 16384/4 --cpu off --duration 16s --warmup 6 --pairs 1 --name study-phase-f2
ONINO_STUDY_FIXED=1 go run scripts/gpu20261012-cli.go --exe measurements/gpu20261013-study-integrated.exe --compare measurements/study-o-confirm.exe --settings 16384/4 --cpu all --duration 78s --warmup 14 --pairs 3 --name study-confirm-o-thread --patterns abcde.
```

Four-prefix and long-prefix checks use the automatic command with two pairs and respectively `--patterns somethingrare.,anotherprefix.,vanityaddress.,hellothereab.` and `--patterns somethingreallyrareabcdefghijklmnopqrstuvwxyz234567.`. CPU-only uses `--cpu only` in this development runner. Prototype screens used `powershell -NoProfile -File scripts/study-evaluate.ps1 -Name <variant> -Defines <comma-separated-defines>`; the retained combination was `-DSTUDY_B=1,-DSTUDY_I_FILTER=1,-DSTUDY_I_ROUND=1`. Those definitions are archived research inputs, not supported production switches. The development schedule search was `powershell -NoProfile -File scripts/study-search.ps1`. Native tracing and correction instrumentation were separate runs, never part of final throughput commands.

Ignored `scripts/` holds rejected shader sources, bounded search/build/check drivers and the removed snapshot helper; ignored `measurements/` holds executable hashes, JSON/log records, native captures and SPIR-V. Raw groups use `gpu20261012-study-<experiment>-...`, with `study-final-{auto,large,multiple,long,frequent,frequent-auto}` identifying the clean integrated confirmation. These artifacts are not dependencies of the application or its tests. The equations, layouts, bounds, parent configurations and decisions above are the durable specification of the study rather than a chronological terminal log.

Sources were used according to their scope:

| Source | Credibility and applicability |
|---|---|
| [Onino reference](https://github.com/coalaura/onino/commit/bcd43a8d77c812f2672b1b2f2904f36115d41027) | Primary implementation evidence, 10/10 for code facts; historical benchmark boundaries retained |
| [Prefix32 v2.2.0 pinned source](https://github.com/0xROOTPLS/Prefix32/tree/fc077b3cb6df9364d755a0dfcfffff76cfd7579e) | Primary implementation evidence, 10/10; weighted forward/reverse traversal reviewed locally, not its seed policy, large arrays or reported speed adopted |
| [Nsight Graphics Shader Profiler](https://docs.nvidia.com/nsight-graphics/UserGuide/shader-profiler.html) | Official Vulkan-capable tooling guidance, 9/10; desired counters unavailable locally |
| [NVIDIA memory access](https://developer.nvidia.com/blog/unlock-gpu-performance-global-memory-access-in-cuda/) and [instruction cache study](https://developer.nvidia.com/blog/?p=86868) | Vendor guidance, 9/10 within the shown workloads; CUDA observations motivated hypotheses, not Vulkan measurements |
| [cuPQC 0.6 execution models](https://nvidia.github.io/cuPQC/releases/v0.6/) | Official guidance, 9/10; thread/warp tradeoffs informed D, while its CUDA API and large-integer results were not transplanted |
| [EUROCRYPT 2026 inversion](https://doi.org/10.1007/978-3-032-25336-1_21) | Peer-reviewed, 9/10; exact bounds/algorithms matter, CPU performance is not a GPU forecast |
| [Pinned libsecp256k1 explanation](https://github.com/bitcoin-core/secp256k1/blob/04b91ce295dad7b827ae18eeeed062135a918dae/doc/safegcd_implementation.md) and [32-bit implementation](https://github.com/bitcoin-core/secp256k1/blob/04b91ce295dad7b827ae18eeeed062135a918dae/src/modinv32_impl.h) | Primary implementation references, 9/10; ordinary/half-delta bounds and small-word design, not a copied 64-bit assembly bound |
| [CryptOpt, PLDI 2023](https://adam.chlipala.net/papers/CryptoptPLDI23/) | Peer-reviewed, 9/10; bounded search plus correctness-first methodology, not a ready-made GPU optimizer |
| [Low-latency simultaneous inversion](https://arxiv.org/html/2607.11337) | Preprint, 6/10; complement dependency graphs motivated K, no published speedup transferred |
| [TensorZKP, USENIX Security 2026](https://www.usenix.org/conference/usenixsecurity26/presentation/lu-tao) | Peer-reviewed, 9/10 for its field/workload; exact packing and utilization had to be derived for onino |
| [Vulkan recycling sample](https://docs.vulkan.org/samples/latest/samples/performance/command_buffer_usage/README.html) and [pool-reset requirements](https://docs.vulkan.org/refpages/latest/refpages/source/vkResetCommandPool.html) | Official guidance 9/10 and normative API requirements 10/10; legal ownership is required independently of observed speed |

The practical limits established here are reconstruction's dominant measured phase, substantial all-core persistence contention, variable automatic frequent-match behavior and unresolved native-call stalls. Arithmetic and filtering improve this host's rare complete-search throughput with very low warmed orchestration cost, but these measurements neither prove a universal optimum nor predict performance on untested devices.

## 13. Autotuning stability and targeted benchmark rerun, 2026-10-14

The comparison suite's five Onino CPU+GPU `somethingrare` samples ranged from 9,599.3 to 10,804.8 M/s. Short 300 ms trials included command-recording, pipeline-fill and drainage costs. The finalist comparison also widened its throughput indifference band using the difference between two repeat samples, allowing transient scheduling variation to classify a meaningful speed difference as a tie and favor the shorter submission.

Timed performance trials now settle for 100 ms before scoring a 300 ms pipelined interval. Transition/drainage work remains accounted useful search, but is excluded from that interval's rate. When the startup budget permits, finalists receive three interleaved samples each and are compared by median with a fixed 1% indifference band. Longer bounded submissions are explored first; reserving repeat time begins after two distinct finalists exist, so cold initialization cannot consume the remaining exploration budget on short submissions alone. Explicit overrides, submission/flight limits, independent verification and normal saving remain in effect.

The targeted rerun is `measurements/bench-2026-10-14T10-49-58-867Z`, built from `b45cdaa99f848732da0be33e3f8950350d49b26b` plus the recorded working-tree changes. It replaces only the 20 Onino CPU+GPU attempts from `measurements/bench-2026-10-14T04-31-54-935Z`; all 160 other sample records are identical. Each workload has five attempts, at least 15 seconds of post-setup search warmup, approximately 20-second counter-delta windows and normal key saving. The manifest and CSV retain binary/source hashes and per-sample provenance.

| CPU+GPU `somethingrare`, M/s | Before | After |
|---|---:|---:|
| Minimum | 9,599.3 | 10,753.5 |
| Median | 10,543.3 | 10,945.0 |
| Maximum | 10,804.8 | 10,994.3 |
| Min-max width | 1,205.5 | 240.7 |

Median throughput increased 3.8% and the observed min-max width decreased 80.0%. The five selected round counts were 32, 22, 31, 44 and 44, all with 16,384 streams: stable throughput does not require an identical winning configuration. These are sequential same-machine measurements, not a cross-device guarantee.

The remaining workloads expose limits rather than universal stability. `hello` ranged from 1,100.2 to 2,504.2 M/s; the multi-prefix workload ranged from 1,840.6 to 10,908.3 M/s. Its first two attempts selected one round and had approximately 8.4/8.6-second median discovery-to-saved-log delays, versus approximately 0.04-0.05 seconds in the three later fast attempts. This demonstrates substantial save-path queueing during the slow runs; it does not identify the underlying filesystem or scheduling cause. All slow samples are retained. One `privacy` attempt remained in tuning until the runner's 600-second startup timeout, with CPU search continuing. Its exact blocking operation was not captured, so it is not attributed to a particular native call. The table reports `privacy` as incomplete (4/5), without substituting a retry or selectively averaging survivors. A runner-timeout-interrupted rare-prefix attempt was archived before resuming the suite.

Regression tests cover scheduling outliers and cold-start candidate ordering. All 28 GPU-package tests passed with Vulkan synchronization validation; targeted GPU-package vet checks passed for Windows, Linux and macOS. The repository-wide vet invocation timed out and is not reported as passing. The tuning correction improves the primary comparison's measured consistency; saving contention and startup stalls remain unresolved limitations.
