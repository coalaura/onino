# Resident Vulkan Prefix Search for onino

## Abstract

This paper describes the first working GPU backend for onino: an opt-in Vulkan compute engine that searches independently of the existing CPU workers. The central design choice is residency. Public elliptic-curve centers, offset tables, candidate positions and prefix filters remain on the device; only new public centers and compact potential-match records cross the host boundary. Private key material remains on the host, where each potential match is independently reconstructed and verified before persistence.

On an NVIDIA RTX 5090 with a Ryzen 9 9950X3D host, the initial implementation achieved a median 166.3 million candidates per second within GPU execution intervals and 164.0 million candidates per second end to end. With one pinned CPU worker, the median combined rate was 221.9 million candidates per second, retaining 97.2% of the separately measured CPU rate. These are short, validation-disabled measurements of rare-prefix search, not persistence benchmarks or a broad tuning study. Differential tests cover arithmetic, complete candidate encodings, filtering, range transitions, generation handling, overflow, cancellation and saved-key validity. Execution portability remains narrower than source portability: Windows/RTX 5090 search is validated, Linux is cross-built and a second vendor's driver did not complete search-engine initialization.

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

The sixty-four denominators in a workgroup are inverted together. A shared-memory product tree performs 63 upward multiplications, one field inversion at the root and 126 downward multiplications to recover the individual reciprocals. Intermediate values never leave the GPU. Center advancement uses the same affine identities with the fixed jump point, including the abscissa needed to form the next center. This first implementation uses a separate inversion for that jump; it does not attempt to merge or aggressively optimize the two inversion phases.

### 2.3 Field representation and canonical filtering

Field elements use ten unsigned limbs with alternating 26- and 25-bit widths. Multiplication uses straightforward schoolbook accumulation in 64-bit integers, including the radix-alignment factors and reduction by \(2^{255}\equiv19\pmod p\). Squaring has a dedicated symmetric implementation. A fixed addition chain computes inversion with 254 squarings and eleven multiplications. Carry propagation returns bounded limbs and a final conditional subtraction produces the canonical representative.

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

The permanent tests use the production arithmetic and search source, with small diagnostic shader variants where observation requires more information than ordinary compact results expose. Arithmetic vectors compare addition, subtraction, multiplication, squaring and inversion with an independent big-integer model. They include zero, random values and values immediately around the prime, including noncanonical encodings.

A complete-range test forces every candidate to become observable and compares 260 consecutive full compressed public keys against independent scalar-base multiplication. It crosses two center transitions and verifies the paired ordering, so candidates normally rejected by the prefix filter are covered. A separate three-prefix test compares both hits and checked counts over 384 candidate positions, testing rejection and filtering rather than just the successful path. Host mask tests cover every supported prefix length.

State-machine tests deliberately use result capacity one with several active streams. They check lossless overflow, no duplicate delivery, paused-stream counters, stale acknowledgements, replacement generations and resumption of the paired sibling. An epoch diagnostic starts the step counter near exhaustion to test the control transition without traversing billions of candidates; it does not substitute for a full scalar-arithmetic test at that counter. Host tests reject corrupted records and replayed generations. Cancellation tests drain overflow, propagate save failures and write actual keys through the existing store's independent validity checks.

The RTX 5090 device tests passed with the Khronos validation layer and synchronization validation enabled. The ten focused GPU-package tests also passed under PACE and stock-Go compatibility was checked separately. CPU-only CGO-disabled builds and tests remain part of verification. Windows and Linux static analysis uses the repository's custom vet tool. A Linux GPU binary was cross-built with an explicit GNU libc target and its dynamic interpreter verified, but Linux runtime validation was not performed.

An AMD integrated Radeon provided an additional portability probe. Its arithmetic test passed, while both ordinary and diagnostic search tests exited during engine initialization before the test could report an opened device. The available output did not establish the cause. This result prevents claiming working cross-vendor search in this pass, despite the absence of vendor-specific shader features.

## 5. Performance methodology

Measurements used Windows/amd64 on a Ryzen 9 9950X3D with an RTX 5090, NVIDIA driver 616.64 and the WDDM driver model. The application was built with PACE, Go 1.27.1, CGO, dynamic linking, a baseline-compatible amd64 target and profile-guided optimization disabled. Shader generation used Vulkan SDK 1.4.328.1. Runtime SIMD selection remained automatic for the CPU worker. Validation was disabled throughout timing.

Three configurations were measured: GPU only; one CPU worker pinned to logical processor 2; and the GPU plus that same pinned worker. Each configuration had three sequential repetitions. There were no all-CPU-worker measurements or launch-parameter sweeps. The GPU used the production defaults of 256 streams, four rounds, two slots and result capacity equal to the stream count. All configurations searched `somethingrare.` with a no-op persistence callback; no matches occurred during these measurements.

The dedicated measurement procedure invokes gpu.Run and search.RunQueued directly, with `GOMAXPROCS=3`. For GPU-containing runs, the first GPU progress callback, approximately one second after search starts, marks the warm-up boundary. The mixed run starts its CPU worker at this boundary. A ten-second timer then cancels the shared context and the measurement includes final draining. CPU-only runs use the first CPU progress callback as the warm-up boundary and likewise measure the following ten seconds. Initialization, driver compilation and warm-up are excluded from steady end-to-end rates. These boundaries can be reproduced using the public options and progress callbacks of the two internal packages, without changing either search implementation.

GPU execution throughput divides the total checked count by the sum of compute-dispatch timestamp intervals. These device measurements include the initial search warm-up interval. End-to-end throughput uses the checked-count delta after the warm-up callback divided by host elapsed time through return. The two rates therefore have deliberately different boundaries and should not be treated as algebraically exact complements. Inter-dispatch gaps are measured between one dispatch's end timestamp and the next dispatch's start timestamp; their fraction uses total execution plus gaps, excluding initialization before the first timestamp.

Host CPU cost is the process user-plus-kernel CPU-time delta divided by steady wall time, using Windows process accounting. It includes Go, CGO, runtime activity and driver work attributed to the process. It is not an isolated controller-thread profile and it does not measure all system-wide driver activity. Transfer rates are logical payload counts: command bytes copied to coherent mapped memory and result bytes consumed by the host. They are not measured PCIe bus traffic and exclude implementation-dependent cache-line transactions.

For a preserved CPU comparison, `BenchmarkFullSearch/rare` from the pre-change CPU implementation was measured with `ONINO_BENCH_BACKEND=auto`, `GOMAXPROCS=1`, logical processor 2 affinity, three-second samples and three repetitions. That benchmark excludes setup and cancellation and uses deterministic entropy. Its median was 67.51 million candidates per second, with a 67.35-68.69 million range and zero allocations per operation. Automatic SIMD selection is important: leaving the benchmark-specific backend selector unset exercises a scalar implementation and would make an unfair primary CPU comparison. The production API measurements below supply the closest matched CPU-only control for mixed-worker retention.

## 6. Selected results

Rates are millions of logically checked candidates per second. Brackets show the full range of the three repetitions, not confidence intervals.

| Configuration | GPU execution | GPU end to end | CPU end to end | Combined end to end |
| --- | ---: | ---: | ---: | ---: |
| One pinned CPU worker | - | - | 69.35 [64.80-69.53] | 69.35 [64.80-69.53] |
| GPU only | 166.34 [166.31-167.36] | 163.97 [163.81-165.13] | - | 163.97 [163.81-165.13] |
| GPU plus one pinned CPU worker | 156.62 [154.90-169.06] | 154.48 [152.65-167.28] | 67.39 [65.17-67.46] | 221.87 [217.82-234.74] |

The GPU-only end-to-end median is about 2.36 times the production single-worker CPU control or 2.43 times the preserved pre-change microbenchmark median. The mixed median is about 3.20 times the production single-worker control. CPU throughput retained is the ratio of mixed and CPU-only medians: 97.2%. These ratios describe this device, host and workload; they do not predict scaling across all CPU cores.

The GPU-only median dispatch lasted about 788 microseconds, with roughly 1,254 submissions per second. Average inter-dispatch gaps were 9.56 microseconds, comprising 1.20% of execution-plus-gap time. The corresponding mixed medians were 837 microseconds, 1,180 submissions per second and 10.22-microsecond gaps, comprising 1.21%. Most of the difference between the two configurations' median GPU rates appears inside the dispatch intervals, rather than as a large increase in submission gaps. The sample count and lack of controlled clock telemetry do not justify assigning that variation a specific cause.

GPU-only logical command traffic was 42.37 MB/s, while host-consumed result traffic was only 20.06 kB/s. The latter consists of result headers because the rare workload produced no hits. The mixed medians were 39.89 MB/s and 18.89 kB/s. Command traffic includes a full 132-byte record per stream on every submission, even when no refill is needed. This is a measurable cost of the simple fixed-layout interface and a possible later improvement, not evidence that candidate data is being streamed through the host.

GPU-only process CPU cost had a median of 0.54 logical-core equivalents, with a 0.29-0.56 range. The CPU-only control used about 1.00 and mixed execution used 1.53, with a 1.51-1.54 range. Thus the controller and associated driver/runtime activity are not free even though the Go submit/collect path is allocation-free and blocks on fences. A thread-level profile would be needed to attribute that cost more precisely.

## 7. Interpretation and limitations

The first result is architectural: resident paired search can deliver useful additional throughput without turning the CPU into a per-candidate feeder. Compact results, bounded dispatches and GPU-local inversion keep end-to-end GPU throughput close to the measured execution rate. One pinned CPU worker remains productive alongside the device. The wider mixed-run range is also important; the reported medians are exploratory measurements, not evidence of a stable hardware ceiling or a regression explained by the implementation alone.

The arithmetic prioritizes understandable bounds and differential verification. It uses generic 64-bit integer operations, a workgroup product tree and a separate center-jump inversion. Register pressure, compiler behavior, subgroup scheduling, alternative radices and inversion scheduling have not been systematically optimized. Portability also depends on shader compilation and driver behavior, not merely on accepting the Vulkan capability checks. The unresolved AMD initialization failure and absence of Linux runtime measurements are concrete limits on the current support claim.

The throughput workload is deliberately rare. Frequent necessary-condition matches pause streams, invoke independent scalar reconstruction and can make verification or storage the bottleneck. Slow persistence naturally reduces active-stream occupancy. Correctness tests establish the preservation protocol under these conditions, but this paper does not claim a measured sustained save rate. Similarly, initialization and shader-cache behavior are outside the steady-rate table and ordinary cancellation guarantees should not be confused with durable recovery from a crash or device loss.

The committed implementation, precompiled shaders, generation procedure and focused correctness tests are sufficient to build and validate the backend without an SDK at ordinary build time. The remaining work is bounded: establish additional driver/runtime support, characterize controller CPU cost more precisely and measure targeted changes against this baseline. A broad optimization sweep or all-core CPU comparison would be premature before those uncertainties are reduced.
