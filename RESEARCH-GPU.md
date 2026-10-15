# Developing onino's GPU engine: resident search, exact filtering and bounded coordination

onino searches for Tor v3 onion addresses while producing independently valid, usable keys for every saved match. Its GPU engine must do more than generate curve coordinates quickly: it must preserve candidate order, verify complete keys, account for completed work, handle bounded output without losing matches and cooperate with CPU workers and durable storage. This paper develops that complete-search design. Arithmetic, work distribution and host coordination are considered together because an improvement to one can make the complete search slower.

The current engine uses GPU-resident paired affine generation, eight-word field arithmetic, batched half-delta inversion, a compact product tree and a conservative partial-product filter followed by exact verification. Three specialized Vulkan passes separate preparation, packed inversion and reconstruction. Automatic configuration measures useful search alongside the requested CPU workers, retaining calibration counts and matches. The current implementation is explained first; earlier configurations and rejected alternatives retain their own baselines and measurement boundaries.

**Navigation:** [Foundations](#foundations-and-measurement-boundaries) . [Current architecture](#current-architecture-and-development-map) . [Arithmetic and filtering](#arithmetic-inversion-and-filtering) . [Work distribution](#work-distribution-and-reconstruction) . [Configuration and saving](#automatic-configuration-cpu-coordination-and-saving) . [Complete-search results](#complete-search-results) . [Verification and compilers](#correctness-portability-and-compiler-findings) . [Limitations](#limitations-and-conclusions) . [Extended evidence](#appendix-a-experimental-protocols-and-extended-results) . [Profiling and resources](#appendix-b-profiling-startup-and-resource-observations) . [Reproduction](#appendix-c-artifacts-and-reproduction) . [Experiment index](#appendix-d-experiment-index-and-historical-navigation) . [References](#references)

<a id="abstract"></a>
<a id="1-objective-and-experimental-scope"></a>

## Abstract and scope

The organizing unit is an independently seeded resident stream. Each round tests 128 positive/negative offset pairs around an affine center. A product tree shares one inversion across the pair denominators and center advancement; adjacent invocations invert independent stream roots instead of leaving most lanes idle during inversion. Quadratic denominator preparation, batched divsteps, selective recomputation, an internal-node-only tree and exact-verified partial products reduce the cost of maintaining that stream. Dirty command ranges, reusable submissions, device-local results and bounded readback keep host coordination small without dropping overflow or counting speculative candidates.

On the recorded Ryzen 9 9950X3D/RTX 5090 system, the final exact-counter comparison measured mean automatic GPU-only throughput of **12.77415 billion checked candidates/s**, versus 12.45193 for its immediate control and **14.16478 billion/s combined**, versus 13.82561. All five paired differences were positive in each mode. These are warmed 30-second complete-search windows, not arithmetic-kernel rates or full-process averages. The same-window combined CPU contribution remained approximately 1.324 billion/s. Multiple-prefix results also improved; long-prefix GPU results were unresolved. Frequent `hello` GPU-only search improved, but automatic combined throughput was about 1.86% lower and longer fixed-configuration confirmation remained mixed. Faster candidate generation does not establish faster saving-limited search.

The evidence is predominantly Windows Vulkan execution on one discrete GPU. Integrated-Radeon arithmetic and small-search validation succeeded, but a large cancellation test still exceeded a conservative execution guard. Shader compilation and SPIR-V validation did not establish runtime correctness for the tested Slang route. Historical command-reset and destruction stalls remain unexplained. Neither the tuning budget nor observed short submissions guarantees a deadline around blocking driver calls.

<a id="2-mathematical-and-counting-invariants"></a>

## Foundations and measurement boundaries

### Address semantics and independent keys

A Tor v3 hostname encodes a compressed Edwards25519 public key, checksum and version using lowercase base32. The public key stores Y and the sign of X. The GPU plan accepts one to eight literal anchored prefixes, each one to 51 characters followed by a dot. Only the first twelve characters, at most 60 public-key bits, enter the GPU necessary-condition filter. The complete original matcher runs on an independently reconstructed public key before saving, so longer patterns retain their meaning. Positional patterns and `--patterns` input are mutually exclusive; the same GPU form, length and count limits apply to files and combined search. The more general CPU matcher and its separate development history are described in [RESEARCH.md](RESEARCH.md).

Each GPU stream and CPU worker starts with independent cryptographic entropy. The host expands and clamps an Ed25519 scalar, retains its nonce prefix and private scalar and uploads only a public affine center. Scalar steps are multiples of eight. Related candidates may be searched, but **only one key from a seed may be exported**. A successful save retires that seed and triggers independent reseeding; false necessary-condition matches resume the same stream. Generation checks reject stale results and acknowledgements. The host reserves the entire 32-bit step range before accepting a seed, rejecting scalars whose permitted boundary could be crossed.

For initial scalar `s`, the current center is `(s + 8*128)B`; offsets are `Qj = 8jB`, `j = 1...128`. A center at step `k` covers `k-128...k+128`, excluding `k`. Advancing by `257*8B` gives disjoint intervals. Logical order is each lane's positive candidate followed by its negative sibling, then the next lane. A cursor identifies the first unconsumed position. A potential match pauses the stream without committing its provisional next center. If full matching rejects it, the acknowledgement resumes after that candidate; recomputing earlier arithmetic does not recount it. Before advancing, the shader checks `step > UINT32_MAX - 385`, allowing for both the 257-step jump and the next positive offset of 128.

### Paired affine generation and shared inversion

The field is `p = 2^255 - 19`; the twisted-Edwards parameters are `a = -1` and `d = -121665/121666`. For center `C` and offset `Q`, define temporary products `a = xC*xQ`, `b = yC*yQ` and `c = d*xC*yC*xQ*yQ`. The paired formulas are:

```text
r = 1/(1-c^2)
t = c*r
y(C+Q) = (b+a)*(r+t)
y(C-Q) = (b-a)*(r-t)
r+t = 1/(1-c)
r-t = 1/(1+c)
```

The addition law is complete for valid affine points over this field: the curve's `a` is a square and `d` a nonsquare. Tests include exceptional points and independently multiplied search keys. If the candidate product-tree root is `P` and the jump denominator is `J`, one inverse serves both:

```text
u = 1/(P*J)
inverseP = J*u
inverseJ = P*u
```

The tree expands `inverseP` to the pair reciprocals; `inverseJ` supports the next center. Neither can be reused after a center or generation changes. `M`, `S` and `I` below denote field multiplication, squaring and inversion. Source operation counts explain an algorithm's work but are not native instruction counts or throughput predictions.

### Checked candidates, publication and timing

A no-hit round checks exactly 256 candidates per active stream. With an earliest winner, the increment is `min(winner + 1, 256) - cursor`. Later physically computed candidates are speculative and excluded. Paused streams and collection-only submissions contribute zero. Each stream accumulates its count across the submitted rounds, resets that accumulator in the first preparation and adds it once at the last reconstruction. The host adds the result exactly once after fence completion, including calibration and cancellation drainage. Dispatch dimensions are never substituted for observed counts.

At the explicit maximum of 32768 streams and 64 rounds, a no-hit sequence checks 536,870,912 candidates, within the 32-bit device counter. Host totals are 64-bit. Earlier limits were 134,217,728 for the 64-pair/16384-stream engine and 268,435,456 after moving to 128 pairs but before explicit 32768-stream support. Those historical counts must not be applied to the current geometry.

`Saved` means a successful persistence callback, not discovery or queue insertion. GPU-only disables CPU search workers but still requires host orchestration, independent scalar-base verification and persistence. Combined throughput uses CPU and GPU deltas over the **same** wall-clock endpoints; separately timed backend rates cannot be added. The paper distinguishes these boundaries:

| Measurement | Numerator and denominator |
| --- | --- |
| Warmed complete-search window | Published checked-counter deltas over common monotonic endpoints; includes verification, actual saving, pauses and coordination during that window. |
| Device-only rate | GPU checked candidates divided by summed timestamped sequence execution, including recorded transfers; excludes host gaps, initialization and teardown. |
| Backend-active rate | GPU checked candidates divided by controller time after device initialization through accepted-work/save drainage; includes calibration and gaps, excludes driver teardown. |
| Final overall average | Exact returned CPU, GPU or total count divided by the same search-action-to-return interval, including preparation, initialization, calibration, drainage and teardown. |
| Arithmetic or phase diagnostic | The named kernel or timestamp interval, with its stated dispatch, conversion and synchronization overhead; not complete-search throughput. |

M/s means millions and B/s billions of checked candidates per second, not bytes. Memory sizes explicitly use bytes, MiB or MB. Ranges are observed minimum/maximum values unless labeled otherwise. Historical mean-plus/minus tables distinguish sample standard deviations from nominal confidence intervals. No percentages from successive studies are added.

The final exact reporter removes rounded-display error, not asynchronous publication error. It reads CPU/GPU checked and saved counters with monotonic timestamps bracketing the reads, every five seconds. It does not drain either engine. CPU batches and already-executing GPU work can straddle endpoints; the 120 ms flight guard is not a zero-error timing bound. A recorded zero read interval reflects clock resolution, not an atomic cross-engine snapshot. Final returned counts after graceful drainage have a different contract from these live snapshots.

<a id="3-retained-resident-execution"></a>

## Current architecture and development map

### Resident passes and storage

The production shader creates three specialized pipelines. Preparation and reconstruction use 128 lanes per stream; packed inversion uses 64 invocations per workgroup, each handling an independent stream. Phase and local size are specialization IDs 0 and 1. The runtime requires Vulkan 1.3, `maintenance4` for `LocalSizeId`, shader 64-bit integers, compute timestamps and sufficient workgroup, shared-memory and storage-buffer limits. Ordinary CPU builds require neither CGO nor Vulkan; GPU-enabled builds default to `--gpu off`. Device preference and stream/round tuning are separate decisions. [Vulkan execution requirements](#ref-vulkan) define capability and ownership constraints, not optimal sizing.

1. **Preparation** applies resident commands, resets per-submission accounting, computes quadratic denominator parents and builds the product tree only as far as eight frontier roots.
2. **Packed inversion** privately completes that tree, combines its root with the jump denominator, performs one batched inverse, expands inverse products back to the eight-root frontier and computes a provisional next affine center.
3. **Reconstruction and filtering** resumes the reverse tree, recomputes inexpensive leaf intermediates, tests both signs in logical order, then pauses on a winner or commits the provisional next center. Only the last reconstruction publishes results and checked counts.

All rounds and passes are recorded in one command buffer, with compute-write to compute-read/write dependencies between passes. Bounds checks protect partial packed workgroups; paused streams are skipped. Full-point diagnostics retain Y and sign data across passes so an early-round hit remains inspectable at final publication. Scratch slots unused by the production frontier are not treated as live production inputs merely because diagnostics use them.

Current state is 4,324 bytes per stream: a 96-byte current affine center, nine metadata words, 128 eight-word scratch fields and a 96-byte provisional center. The stream array is 67.5625 MiB at 16384 streams and 135.125 MiB at 32768. The table contains 129 affine offsets, eight probes and their count, 64 quadratic sums, 64 products, a jump square and one filter-mode word: 16,648 logical bytes before allocation alignment. Commands are 108 bytes; result records are 24 bytes. These are current ABI sizes, distinct from historical allocations in [Appendix B](#appendix-b-profiling-startup-and-resource-observations).

Two bounded slots retain command buffers, descriptors, fences, timestamps and mapped upload/readback storage. Centers, scratch, tables, commands and results are device-local. Go memory is copied synchronously at the C boundary and never retained by Vulkan. A bounded dirty range carries new seeds or acknowledgements; zero-action holes are harmless. Dirty entries for temporarily inactive streams survive until reactivation. Ordinary no-hit submissions upload zero command bytes. Recorded commands are reused when active range, rounds and collection mode match and no dirty upload requires recording.

Default result capacity is `min(allocated streams, 256)`. A fixed copy of the 16-byte header plus the bounded array is recorded with the search: normally 6,160 bytes. No second host trip is needed to discover a size. Device allocation is limited by dispatch/storage capabilities and a conservative 1/64 share of the smallest participating heap, using a 12,288-byte per-stream budget. Automatic allocation is capped at 16384; explicit support extends to 32768. This is a capacity allowance, not a query of free memory. Allocation failure remains an error.

Memory selection uses properties, not assumptions that host-visible means system memory. Coherent staging is preferred. Noncoherent fallback flushes upload and invalidates readback over whole mapped allocations after fence completion, avoiding atom-alignment mistakes. Discrete and unified layouts can differ. Allocation sizes or memory flags alone establish neither cache behavior nor runtime portability.

### Delivery, verification and cancellation

A potential match remains resident until delivered. If the append array is full, pending-overflow accounting increases and the stream stays paused; no key is dropped. A delivered stream awaits acknowledgement and cannot publish twice. Host verification checks stream, generation, record kind and the reported low 64 bits against independent Ed25519 scalar-base multiplication, then applies the full matcher. A false full match resumes the cursor. A successful save permits reseeding. Verification/save channels are bounded by allocated streams and CPU/GPU callbacks share serialized persistence.

Cancellation stops new useful submissions, drains accepted work and issues collection-only submissions until undelivered overflow is collected, without applying acknowledgements that could resume search during drainage. Accepted verification and successful saves finish before return. Storage failure is propagated as an error. Device loss, process death and failed storage cannot promise lossless delivery; slow successful storage may legitimately extend shutdown.

At most two submissions are in flight. A trial's first observation runs alone and overlap is allowed only when the observed tail fits the 120 ms aggregate-flight budget. A validation sequence exceeding 150 ms is rejected. Normal search shrinks an automatic dimension when a tail exceeds 60 ms or backlog warrants it; an explicit setting is rejected rather than silently changed. Fence timeout does not cancel GPU work. Sudden slowdowns, first execution and uninterruptible driver calls can exceed predictions.

### Development landmarks

The following labels identify evidence, not a shared benchmark series. They replace dated report sections while preserving their provenance. Exact artifacts and protocols are in [Appendix A](#appendix-a-experimental-protocols-and-extended-results) and [Appendix C](#appendix-c-artifacts-and-reproduction).

| Label | Recorded reference | Design/evidence boundary |
| --- | --- | --- |
| R: resident execution | `a87def5300bd8bd99061bd2f206fd86486ea6446` | Ten alternating pairs; ten 25/26-bit limbs, 64 pairs original monolithic-to-packed comparison. |
| W: word radix and batches, October 12 | `8e5ad93333c316d9686030ec6b1819ccbc12741f` | Eight 32-bit limbs, 128 pairs, phase specialization and limb-major scratch. Requested `87353dd2085dd02b49c47d651a949cc8c5c97596` was absent and is not the measured baseline. |
| P: projection and reconstruction, October 13 | `cd0192e2b9d55f5a0178a7a6f91895d414326cf6` | Exact prefix projection, provisional center, child-per-lane reverse tree and internal-node storage. |
| A-O: systematic alternatives, October 13 | `bcd43a8d77c812f2672b1b2f2904f36115d41027`; work began at production-identical `0ad90f74c219e806b74a3336edef2ecdc3ac5565` | Fixed-window observer; conditional corrections and workgroup probe caching retained. |
| T: tuning stability, October 14 | `b45cdaa` plus recorded tuning changes | Combined-only benchmark refresh; repeated finalists and fixed noise band. |
| E01-E14: arithmetic/execution, October 14 | `cf413508cc27781d5f00b17265201c82ee854ead` | Quadratic parents, partial products, depth-two frontier, batched inversion, square complement and calibration-backlog stop. Shaders match earlier analysis reference `a4fadc66271d41d6869bc1f0980275f4e177ec13`. |
| F01-F08: reconstruction/measurement, October 15 | `51af262` | Same exact reporter in both builds; uniform filter mode, one probe traversal, low32 route, bounded column sums and depth-three/WG64 frontier. |

<a id="6-component-experiments"></a>

## Arithmetic, inversion and filtering

### Full-width eight-word arithmetic

The current representation consists of eight little-endian unsigned 32-bit words and accepts **every** value in `[0, 2^256)`. Operations use the redundant modulus `2p = 2^256 - 38`, then project canonically modulo `p` where required. Multiplication, dedicated squaring and reduction were adapted from the pinned [Prefix32 v2.2.0 implementation](#ref-prefix32), with its MIT notice retained in `field32.glsl`. Prefix32's seed policy, result-capacity truncation, center candidate, exhaustive tuning and device-name-only cache were not adopted. Its published performance was contextual evidence, not a newly reproduced competitor result.

Let `R = 2^32`. In schoolbook multiplication, a cell contains one product, the existing output word and carry. Its maximum is `(R-1)^2 + 2(R-1) = R^2-1`, fitting `uint64`; the row's high destination is still unused when its final carry is written. Dedicated squaring computes 28 cross-products and eight diagonal products, doubles cross terms into an exact 512-bit result and propagates carries without assuming canonical input. Its relevant carry bound is below `2^33`.

Reduction folds the high half by 38. A cell is bounded by `39(R-1)+38`, with outgoing carry at most 38. The first final fold adds at most 1,444. If that wraps, the wrapped low word is at most 1,443, so the final addition of 38 is at most 1,481 and cannot require another full ripple. Addition cells are at most `2^33-1`. Subtraction obtains borrow from bit 63 of the unsigned wrapped difference, corrects a 256-bit wrap by subtracting 38 and handles the possible second borrow with a safe low-word correction: that case leaves a low word at least `R-38`, hence at least `R-76` after the correction. These are onino's full-width bounds, not borrowed bounds for a canonical or unsaturated representation. [Nath and Sarkar](#ref-nath) provide related representation/carry analysis, not Vulkan timing predictions.

Two refinements reduce work while preserving this contract. First, correction chains touch words 1...7 only if adjusting word zero overflows or underflows. In the A-O B experiment, unbiased instrumentation over 78,798,389,248 candidates observed 45,868 long fold corrections in 637,466,656,768 opportunities and 1,038 long subtraction corrections in 158,212,390,912: approximately `7.20e-8` and `6.56e-9`. An earlier instrumented version made a dead preparation numerator live and was discarded as biased. Second, E09's `squareComplement` folds the wide square directly into `1-square(value)`: signed carry starts at one and subtracts each low word and 38 times its high partner before the small final correction. Signed 64-bit intermediates suffice for arbitrary words. It avoids materializing a positive reduction followed by subtraction; the analogous multiplication-complement variant tied and was removed.

Canonicalization cannot be skipped merely because a candidate is likely to fail. A noncanonical encoding can create a false negative which host verification cannot recover. Since the input is below `2p+38`, at most two subtractions of `p` suffice. In the carry case bit 255 must be toggled rather than unconditionally cleared. Production filtering only needs the canonical low 64 bits. For representative `v`, define:

```text
q = [v >= p] + [v >= 2p]
canonicalLow64 = low64(v) + 19*q       (mod 2^64)
h = bit255(v)
upper = (word7 | 0x80000000) & word6 & ... & word1
q = h + [upper == 0xffffffff && word0 >= 0xffffffed - 19*h]
```

This exact projection replaces full serialization in ordinary search; diagnostics still check full canonical Y and X sign. It is different from the subsequent partial-product necessary filter: projection itself has no quotient uncertainty.

### Batched half-delta inversion

Packed inversion currently performs twenty batches of thirty half-delta steps: 600 fixed steps, exceeding the cited 590-step bound for 256-bit inputs. The algorithm adapts the local CPU implementation and [libsecp256k1's signed-30-bit implementation and explanation](#ref-safegcd); the applicable MIT notice remains in `divsteps.glsl`. The [constant-time inversion literature](#ref-inversion) supplies algorithmic bounds, not a GPU speed forecast. A reviewed [BitcoinAddressFinder port](#ref-bitcoinaddressfinder) was secondary implementation context, not the source of Curve25519 constants.

Nine signed radix-`2^30` limbs encode `p`: low limb `2^30-19`, seven middle limbs `2^30-1` and top limb 32,767. The independently derived inverse is `(-19)^-1 mod 2^30 = 0x179435e5`; `zeta = -(delta+1/2)` starts at -1. Canonicalization and conversion occur once on entry and once on exit. Each batch builds a signed 2x2 transition matrix with wrapping small-word arithmetic, then updates the full integers and modular coefficients once.

Induction on elementary transitions bounds each matrix row's absolute sum by `2^30`. The coefficient correction, formed from sign-selected row entries minus its low-thirty-bit cancellation residue, lies in `[-2^31+1, 2^30]`. For ordinary limbs, row products, correction times a modulus limb and incoming carry remain below `2^62` in absolute magnitude; the signed top limb is smaller. The inherited invariant `-2p < d,e < p` permits normalization by two conditional modulus additions around gcd-sign normalization. Zero follows the same schedule and returns zero. No secp256k1 modulus constants are reused.

This batching distinction explains an apparent reversal in the evidence. A-O F tested full-width updates **on every divstep**, with ordinary 741-step and half-delta 590-step recurrences. Both were correct but slower than Fermat inversion. E04 updates full-width state only once per thirty-step batch and improved complete search. F07's subsequent limb/transition unrolling and lifetime experiments gave no repeatable gain; the current batching schedule remains.

### Quadratic preparation

Write `u = xC*yC`, `v_i = d*xQi*yQi`, `t = u^2` and `c_i = v_i^2`. Adjacent denominator leaves can be multiplied without separately forming both squares:

```text
S = c_a + c_b
C = c_a*c_b
(1-t*c_a)*(1-t*c_b) = 1 + t*(t*C-S)
```

This integer polynomial identity holds modulo both `2p` and `p`; canonical table coefficients do not require canonical intermediates. Sixty-four direct parents cost 128M, the upper tree 63M and the jump denominator 1M: **192M+1S instead of 256M+129S**, before moving frontier levels. Lane zero shares the center square through a barrier. The immutable table adds 64 sums, 64 products and one jump square, 4,128 bytes. It removes no candidate, reconstruction product or exact check.

Independent table checks and 2,048 compiled arithmetic cases validate the coefficients and identity against separately formed denominators. E01's complete-search screen rose from about 9.60 to 10.00 B/s; a real 64-lane preparation variant passed correctness but fell from 10.10 to 9.60. The retained 128-lane preparation includes coefficient loads and synchronization in its timing. Its five-run remove-one ablation reduced the combination median from 12.550 to 11.750 B/s.

### Conservative partial products and exact fallback

Most selective candidates can be rejected before a complete Y product. The filter must include the true canonical word for all full-width inputs, not just with high probability. Let `H = 2^255`, `P = A*B` and `p = H-19`. Exact division gives:

```text
P/p = P/H + 19P/H^2 + 361P/[H^2(H-19)]
```

For low64 projection, let `Ck = sum(a_i*b_j, i+j=k)` denote an unreduced product column, set `K = 38*a7*b7 + C6 + C7*R + C8*R^2 + C9*R^3` and define `qhat = floor(K/2^63) mod 2^64` and `u = low64(P)+19*qhat mod 2^64`. This retains columns 6...9 and the top correction. Columns 10 and above contribute integer multiples of `2^65` and disappear modulo `2^64`. Omitted lower columns contribute less than `12/R`; the remaining correction after removing the top product contributes less than `152/R+76/R^2`; because `P < 4H^2`, the final remainder is below `1444/(H-19)`. Thus the nonnegative omitted error is below `164/R+76/R^2+1444/(H-19) < 1`. The true quotient modulo `2^64` is `qhat` or `qhat+1`. Since `p == -19 mod 2^64`, the canonical product's word is **`u` or `u+19`, including wraparound**.

The original E02 implementation collected four columns with explicit unsigned carries: 28 middle products, one top product and three low products, 32 limb products altogether. Its weighted first accumulator is below `45R^2 < 2^70`; a 64-bit accumulator plus a 32-bit overflow word suffices. Four output words preserve exactly the bits for `floor(K/2^63) mod 2^64`. F05 instead sums bounded low and high halves and combines carry once per column. Ordinary sums stay below `9R`; the weighted first sum below `46R`. This removes repeated overflow bookkeeping without changing the quotient proof.

F03 specializes the arithmetic, not the pipeline, when every actual probe mask fits low32. With `C_i` the full convolution column:

```text
qhat = floor((38*a7*b7 + C6 + C7*R + C8*R^2)/2^63) mod R
u = a0*b0 + 19*qhat mod R
```

Columns 9 and above vanish modulo `R` because their quotient contributions are multiples of `2^33`. The omitted lower contribution is below `12/(R-1)`, the remaining correction below `152/R` and the geometric remainder below `1444/(H-19)`; their sum is below one. Column 9 and low cross-products may therefore be omitted **only for low32**. The same two-word inclusion rule holds.

Initialization chooses a uniform mode from the actual masks: exact products if any probe constrains fewer than twelve bits; low32 if all high masks are zero; otherwise low64. The choice does not infer eligibility from textual length and adds no pipeline portfolio. One traversal checks both signs and both provisional alternatives. If either sign could match, the shader computes **both complete products**, applies exact canonical projection and exact GPU probes, then selects the earliest winner. Provisional alternatives are never separate candidates. With eight probes and at least twelve constrained bits, `16/4096` is a conservative uniform-word acceptance heuristic, not a correctness assumption.

Reference tests cover quotient uncertainty and residue inclusion on 8,192 full-width cases, modulus aliases, long carries and near-zero residues; the later low32 gate expands this to 8,448 including 256 one-bit cases, carry boundaries and `+19` wrap. Compiled search tests cover arbitrary/overlapping masks, both signs, resumption and three centers; independent full keys remain the final check. The arithmetic identity and uncertainty bounds are onino derivations, distinct from borrowed field multiplication.

### Arithmetic alternatives and what they establish

The following screens use their own immediate controls. A-O and E/F rates are B/s; R/W/P rows explicitly use M/s. Small or single-screen differences are not promoted to significance claims.

| Experiment | Mechanism and observed result | Interpretation |
| --- | --- | --- |
| R scheduling | Delayed numerator products 786.5 -> 592.5 M/s; sequential plus/minus 789.7 -> 642.8. Earlier fixed-index unrolling and explicit high/low arithmetic increased code/register cost and lost throughput, without a preserved quantitative estimate. | Shorter source lifetimes and fewer apparent wide operations were insufficient. |
| W radix | Eight-word arithmetic 2937.795 +/- 2.160 -> 4188.168 +/- 1.153 M/s, five pairs, sample SD. | Retained; full-width proof accompanies the implementation. |
| W extended products | Explicit extended multiply 4203.2 -> 2928.6 M/s. | Rejected code-generation route. |
| P multiplication | Karatsuba 9273.184 -> 8590.637 M/s; Comba 9279.276 -> 5143.630, two pairs each. | Fewer products or column layout did not improve complete search. |
| A-O B | Conditional corrections 9.277 -> 9.437. | Retained, with unbiased correction-frequency evidence above. |
| A-O E | Keep square temporaries 9.297 -> 9.290; full unroll 9.275 -> 9.139; groups-of-five unroll 9.288 -> 9.185. | No retained gain. SPIR-V growth is not proof of native unrolling. |
| A-O F | Ordinary per-step divsteps 9.314 -> 5.799; half-delta per-step 9.284 -> 6.123. Inversion phases 76.410 -> 361.156 us and 77.040 -> 322.620 us, including conversion. | Correct but rejected; not the later batched E04 algorithm. |
| E02 | Initial loop and explicit unroll lost 11-12%; workload fallback changed compiled behavior, 10.60 -> 11.10. Remove-one medians 11.650 -> 12.349, including a slow combination sample. | Retained only after complete-search confirmation, not from product count. |
| E04 | Batched inversion screen 11.30 -> 12.50; ablation 11.250 -> 12.550. | Retained despite increased inversion resources. |
| E08 | Radix-29 inversion: dynamic indexing 2.58-3.52 versus about 11.40; explicit convolution 10.00 versus 11.20. | Correct, rejected phase-specific implementation; no all-phase radix-29 result. |
| E09 | Square-complement ablation 12.401 -> 12.550; multiplication complement tied. | Retain square fusion only. |
| F01 | Delay X/Y products about -2.7%; reload/recompute mixed about -9.2% despite one fewer register; moving numerator sums unresolved, native resources unchanged. | Original scheduling restored. |
| F02 | Uniform mode plus one traversal about +0.88% in initial three-window rare test; each remove-one ablation positive in all three rare/multiple pairs. Specialization raised reconstruction registers 96 -> 168 and lost three of four screens; per-sign survivor branches lost every workload screen. | Retain uniform mode and traversal, reject the two alternatives. |
| F03/F05 | Low32 multiple-prefix confirmation positive in all three pairs, final ablation about +1.75%; bounded sums rare ablation about +0.65%, multiple effects mixed. Extended multiply slower; explicit diagonals and unroll hints no repeatable extra gain. | Retain eligible low32 and bounded sums; do not add isolated percentages. |
| F07 | Limb/transition unrolling and matrix/frontier lifetime changes gave no repeatable complete-search gain; transition unrolling grew native code. | Preserve 600-step schedule. |

Karatsuba used three four-by-four products, 48 limb products; its nonnegative middle term is below `2^257` and needs nine words. Comba's columns are below `8R^2`, requiring 67 bits; carry is below `8R` and at most seven product-add overflows are possible. Its factor-38 reduction still has carry at most 38. Both passed independent arithmetic checks. Related multiword work such as [MoMA](#ref-moma) motivates testing multiplication choices, not assuming its NTT results transfer to this width and shader.

The rejected per-step inversions used nine signed radix-30 limbs with `(f,g,d,e) = (p,a,0,1)`, starting delta at one for ordinary divsteps and one-half for half-delta divsteps. A positive-delta odd step swaps and half-subtracts with `delta <- 1-delta`; the other step half-adds the odd multiple with `delta <- 1+delta`. Bounds `|f|,|g| <= p`, pre-half magnitude at most `2p` and coefficient half-update at most `2p-2` establish safety; zero returns zero. The problem was work distribution and full-width update cost, not correctness.

E08's nine radix-29 limbs have a 23-bit top limb, 81-product multiply and 45-product square. Convolution plus carry is below `9(2^29-1)^2+carry < 2^62`. Here `R^9 = 2^261`, so the high-fold constant is 1,216, with the top six bits folded by 19. Canonical conversion, fixed folds and Fermat inversion passed actual shader tests. A separate five-interleaved-sample diagnostic used 8,192 inputs through 64 dependent operations, one warmup and input/output conversion where needed: median dispatches were 0.608 ms radix-32 multiply, 0.965 ms radix-29 multiply, 0.407 ms radix-32 square and 0.740 ms radix-29 square. Dispatch/output overhead is included. The [GPU X25519/448 work](#ref-x25519) supplies related reduced-radix context, not an expected onino speedup.

## Work distribution and reconstruction

### Packing independent inversions

The original monolithic shader inverted a root in lane zero while the rest of its stream workgroup waited. Separating passes allows adjacent invocations to invert independent stream roots, leaving numerator recomputation to reconstruction. R's ten-pair comparison at 16384/4 improved complete GPU search from 788.03 +/- 1.13 to 2788.31 +/- 4.60 M/s and combined search from 2094.60 +/- 8.67 to 3514.31 +/- 63.75. These nominal 95% interval half-widths include the extra resident scratch, barriers and host work. Packing, not an isolated faster inverse, produced the large gain.

Subsequent phase specialization let preparation, inversion and reconstruction compile independently. Moving from 64 to 128 pairs amortized inversion further; removing tree padding and storing scratch in limb-major order improved cooperative access. Larger batches did not continue that trend. The current engine therefore combines coarse independent streams with a 128-pair cooperative tree rather than maximizing private work per invocation.

### Provisional centers and compact reverse trees

The packed invocation also computes the next center while it owns the shared jump inverse. This removes approximately nine multiplications from a serial reconstruction tail, but the result remains provisional: a hit or exhausted epoch must not commit it. P's five-pair component comparison improved 6862.440 +/- 28.466 to 8006.487 +/- 7.309 M/s.

Reverse reconstruction assigns a child to each lane. Sibling products must be read before in-place overwrite, requiring a read-before-write barrier and then a store-completion barrier at each level. P's child-per-lane change improved 7982.480 +/- 27.461 to 8660.304 +/- 26.904 M/s. Storing only 128 internal nodes instead of 256 leaf-inclusive nodes, then recomputing each leaf with a square/complement, improved 8668.326 +/- 36.453 to 9226.305 +/- 67.322. At 16384 streams, state fell from 136,380,416 to 70,844,416 bytes: a net 62.5 MiB reduction despite adding 1.5 MiB for next centers. These are P sample SDs, not R confidence intervals.

The current depth-three frontier moves sparse tree levels into the packed invocation. Preparation stops at eight existing products; the invocation uses a private sixteen-field tree, inverts once and expands back to that frontier. For depth `k`, this relocates exactly `3(2^k-1)` multiplications without adding arithmetic or dispatches. Depths one, two and three move 3, 9 and 21M and remove three, six and nine workgroup barriers. Inter-dispatch dependencies remain unchanged. E03 retained depth two; F08's full matrix and combination evidence selected depth three with inversion WG64. The greater packed-inversion cost nearly offsets the removed preparation/reconstruction levels, so fewer barriers alone cannot predict a win.

### Trees, private batches and alternative walks

| Experiment | Complete-search result and tested mechanism | Disposition |
| --- | --- | --- |
| R extra storage | Store numerator/mixed arrays: 2747.1 -> 2516.7 M/s; stride 5276 -> 12952 bytes. | Recompute inexpensive values. |
| W independent roots | Against about 2950 M/s, 8/16/32 roots gave 2855.6/2694.9/1574.9; eight-root global-prefix variant 2830.7. Later eight roots 6221.3 versus 6514.7. | Rejected at both tested stages. |
| W batch geometry | 256/512/1024 candidates: 4880.3/4161.8/4391 M/s. Two serial pairs/lane 6365.6 versus 6514.7; shared transpose 6511.5 versus 6514.7. | Retain 256 candidates and original shared arrangement. |
| P subgroup reverse tree | 9286.565 -> 9197.080 M/s, two pairs. Queried subgroup size 32, explicit mapping. | Correct on tested mapping, slower. |
| P fused dispatch | `3R -> 2R+1` dispatches, 9285.794 -> 8795.020 M/s; reconstruction registers stayed 96, native bytes 64512 -> 94336, SPIR-V 150112 -> 169156. | Reduced dispatch count did not win. |
| A-O A | Immutable products at nodes 1...127, inverse slots 129...255: seven reverse barriers, 254M, 256 slots. 9.313 -> 9.216. Delayed numerators with A 9.200 -> 9.383, but delay alone 9.320 -> 9.081. | Ownership proved; no retained combination. |
| A-O C | Pad metadata to 160 bytes: stride 4352, +448 KiB; 9.293 -> 8.886. | Reject; no cache-sector measurements. |
| A-O D | Two/four-lane inverse: 9.311 -> 5.188 / 9.278 -> 5.106. | Correct queried-subgroup mapping and fallback, but 48 shuffle components/product and replicated carry work lost. Other subgroup widths untested. |
| A-O G | Two/four-pair weighted batches: 9.307 -> 7.922 / 9.306 -> 7.810. Alternative-Y two-pair batch 9.295 -> 7.023. | All rejected with correct exceptional fallback. |
| A-O H | 64/32-node checkpoints: 9.273 -> 9.104 / 9.283 -> 8.939. A 64-node plus 128 mixed-product array: 9.679 -> 9.324. | Extra recomputation/storage did not win. |
| A-O J | Include center as 257th candidate: 9.303 -> 9.230, maximum candidate gain only 0.390625%. | Removed; original enumeration and tests restored. |
| A-O K | Complement products: +128M and 4096 bytes read/written per stream, stride 8420; 9.302 -> 9.082. | Reject; not concurrent overlap of preparation and inverse. |
| E03 | E01-based depth1/2/3 screens about 10.40/10.80/10.70 versus 10.00-10.10; depth-two ablation 11.750 -> 12.450. | Initially retain depth two; no independent full depth-by-algebra factorial. |
| E06 | Sixteen/thirty-two pairs per invocation, private prefix arrays, 8/4 active invocations: 1.48/1.26 versus 12.80. | Correct GPU-resident implementations, rejected. |
| E07 | Thirty-two-pair scaled Montgomery differential walk: 0.862 versus 12.599. | Reject this complete engine, not every differential walk. |
| E11 | Extra 452-byte/stream roots/metadata buffer, about 7.1 MiB, one binding, existing holes retained. No-cut 11.40 -> 11.90; depth-two combination tied at 12.60. | Promising isolated screen, unconfirmed combination; removed. |
| F04 | Immutable siblings: shared 8212 -> 16404 bytes, registers 92 -> 126, all screens slower. Subgroup leaves: eight limb shuffles plus mapping/admission/fallback, about 1-2% slower in all pairs. | Neither retained; no production subgroup requirement. |
| F08 | All nine depth1/2/3 x WG32/64/128 variants passed. Depth one clearly lost; depth3/WG64 only promising structure. Three automatic GPU pairs favored depth three; combined effects small/mixed. | Retain depth3/WG64 with final combination evidence, not from isolated phase sums. |

For A-O G, private storage is `128*b` bytes for `b` pairs per lane. Preparation takes `3b` products, prefix/weight construction `2b-1`, reverse expansion `4b-1`; a tree of `L=128/b` leaves costs `L-1` upward and `2L-2` reverse products plus one inverse. Amortization does not eliminate serial dependencies. A-O H's 64/32-node versions use 2048/1024 scratch bytes and add 64M/one barrier or 96M/two barriers. The mixed-product version uses 6144 scratch bytes and stride 6372, avoids 128M but adds 64M. These resource changes were included in complete search.

An alternative paired-Y identity, reviewed alongside [Prefix32](#ref-prefix32), is `(b +/- a)*(x*v -/+ y*u) = (x*y -/+ u*v)*(1 -/+ c)`. Its replacement denominators can vanish at valid exceptional pairs such as `P = +/-Q`. The W prototype used a unit leaf and exact complete-formula inversion fallback; it passed exceptional tests but delivered 2587.5 versus about 2950 M/s. A-O G's conservative low64-zero test could cause extra fallback work, never a dropped candidate. Algebraic simplification alone does not establish a complete-law replacement.

E07 adapted the recurrence in [GPUonion's kernel and optimization notes](#ref-gpuonion). With `u=(1+y)/(1-y)`, `kappa=(u(step)-1)/(u(step)+1)=y(step)`, it forms `N=U-W`, `D=U+W`, `T=kappa*D`, then `U'=Wprevious*(N+T)^2` and `W'=Uprevious*(N-T)^2`. Neighbor initialization, both signed walks and 64 weighted numerator/denominator entries per invocation stayed GPU-resident. Exact probes retained even-plus/odd-minus cursor order; full diagnostics recovered X sign from the paired formula. A zero conversion denominator triggered exact neighbor reinitialization. Its reported 0.862 B/s includes initialization inverses, conversion, batch inversion, filtering and advancement. A nominal differential-step cost of 3M+2S is not comparable to that complete cost; only this configuration was screened.

### Cooperative arithmetic and bounded code search

A-O M tested real integer tensor operations inspired by [TensorZKP](#ref-tensorzkp), not floating-point approximation. Byte Toeplitz packing used `M16N16K32` unsigned 8x8-to-32 operations: sixteen offset pairs, four degree blocks and eight operations per subgroup over two tiles. Coefficients were at most 2,080,800 and carry 8,160; shared storage grew by about 20 KiB. The first capability gate omitted the required Vulkan memory-model feature; fixing it allowed correctness testing. The supported implementation passed, then fell from 9.332 to 3.938 B/s. It is a measured rejection, not an unsupported-hardware result.

A-O L used seed 20261013 and Fisher-Yates ordering to screen six source variants, following the correctness-first bounded-search idea of [CryptOpt](#ref-cryptopt), without importing its optimizer. Against controls around 9.26-9.32 B/s, delayed numerators gave 9.081, early sums 9.446, swapped products 9.320, limb unrolling 9.430, retained temporaries 9.373 and extra temporaries 9.312. Early sums did not improve the B+I combination; a 60-second B+I comparison gave 9.593 versus 9.569 with unrolling, so the latter was removed. Source-level search supplied hypotheses, not a native instruction model.

<a id="4-automatic-selection-during-useful-search"></a>

## Automatic configuration, CPU coordination and saving

### Useful-search calibration

The ten-second startup budget begins at search-action entry, after argument parsing but before input preparation and Vulkan initialization. CPU workers start while Vulkan initializes; first useful GPU submission and final initial selection are reported separately. Blocking pipeline creation is outside the controller's ability to enforce a deadline. Trials use the same device, resident streams and saver as normal search, preserving their counts, matches, generations and cursors.

Automatic growth starts with at most 256 streams and one round, doubles conservatively under observed tail bounds and retains the best validated fallback rather than the last attempted population. Explicit dimensions are fixed and validated; setting both disables timed performance exploration but not responsiveness checks. Growth considers the union of actual GPU masks, removing covered/duplicate prefixes and respecting the twelve-character filter limit. It stops when expected accepted candidates or observed backlog make more streams unhelpful. Frequent acceptance (`acceptance*256 > 0.05`) forces one automatic round. A paused trial cannot establish that all requested no-hit rounds would fit: extrapolation accounts for hidden work.

Streams provide independent work and determine resident memory. Rounds amortize host overhead while increasing submission duration. The bounded candidate set includes nearby populations and round counts aimed at approximately 20, 10, 5 and 2 ms, longest useful target first. It is not an exhaustive cross-product and uses no device-name rule or cache. Comparisons keep all requested CPU workers active and score completed CPU-plus-GPU deltas over common monotonic windows.

The current finalist stage uses 100 ms settling and 300 ms scoring. Two leaders are measured in runner/best/best/runner/best/runner order, producing three scores each; medians are compared with a fixed 1% indifference band. The budget reserves six settle/score/flight intervals and a 500 ms transition margin before entering this stage. Effective ties favor shorter tails and then fewer streams. The old drift-expanded 2.5% band and shorter repetition schedule belong to R, not current behavior.

Backpressure is a measurement condition as well as a runtime constraint. Trials now stop after collection if verification backlog exceeds eight and initial selection returns a validated incumbent or fallback instead of launching more growth through a saving backlog. E13 reduced five-run `ab` GPU selection from 7.76-15.99 s to 0.22-0.76 s and combined from 11.10-15.89 s to 0.39-4.54 s. All sixty candidate attempts across rare/hello/ab selected within 9.32 s. Rare medians were essentially unchanged, 12.600 -> 12.549 B/s GPU and 13.851 -> 13.902 combined. This establishes an application-controlled startup improvement, not a universal throughput or driver deadline guarantee.

### Why the combined optimum differs

R's original 4096/2 shader produced 634.18 M/s GPU-only with 1.644 ms execution and 0.009 ms gaps. With all CPU workers, GPU contribution fell to 230.03 M/s while CPU remained 1306.23; gaps rose to 2.928 ms and recording wall time from about 17.7 to 998 us. At 16384/4, combined CPU/GPU contributions were 1277.95/627.47 M/s with 11.997 ms execution and 1.393 ms gaps. Scheduling delay belongs in these host timers. It does not prove that reserving a CPU core would improve the total.

Equal-work R screens separated population from submitted work: 4096/16, 8192/8 and 16384/4 each checked 8,388,608 candidates under the old 128-candidate geometry. Packed GPU rates rose 2047.070 -> 2527.541 -> 2793.359 M/s; corresponding combined rates were 3038.843/3388.115/3592.589. More rounds then brought combined throughput near 4.10 B/s even though GPU-only was already near its four-round plateau. P later measured GPU gap fractions at 1/4/16 rounds of 1.69%/0.38%/0.09%, versus 71.38%/39.11%/1.50% combined; automatic combined gaps were 0.11%. GPU-only settings cannot be transferred blindly to all-core operation.

Larger populations also ceased to help after arithmetic changes. A-O D's explicit 32768 support was retained from its local evidence, but E05's automatic expansion to that population did not reproducibly improve selection. Its twenty-attempt comparison gave GPU medians 12.550 -> 12.450 and combined 13.699 -> 13.799 B/s with substantial outliers. All five enlarged GPU trials still selected 16384; four combined trials did too, while the sole 32768 choice reached 13.432. Selection finished within 8.88 s. F08 fixed 32768/4 and /2 gave 12.42/12.33 B/s or less against roughly 12.8 at 16384/4; 8192/8 lost and 16384/8 versus /4 was unresolved. Automatic allocation remains 16384.

### Sustainable saving and adverse outcomes

GPU discovery can greatly exceed storage capacity. An independent P diagnostic verified 10,000 results in 180.172 ms, about 55,502/s or 18 us each, while 1,000 actual saves took 2.1843 s, about 457.813/s or 2184 us each. This was idle capacity, not all-core capacity; overlapping work changed results by less than 1% and did not justify a new admission rule. Real durable writes, callback waiting and independent reseeding matter more than verifier arithmetic for frequent prefixes.

W's backlog stop deliberately traded candidate throughput for drainage. Five combined `abcde` pairs fell from 7725.596 +/- 47.970 to 2533.971 +/- 187.109 M/s, but mean drainage fell from 5546.170 ms (5376-5712) to 84.981 ms (61.540-125.558); exit fell from 5.4-5.76 s to 200-544 ms. Retained CPU/GPU contributions were about 1257.926/1276.045 M/s. This is not an unexplained shader regression. Later persistence experiments likewise preserve durability rather than redefining saved work.

| Coordination experiment | Observation | Decision/limit |
| --- | --- | --- |
| R host residency | Ten-pair bundle improved both modes; independent device-local commands/results 2737.36 +/- 2.91 -> 2804.40 +/- 2.93 M/s. | Retain explicit staging with device-local working buffers. |
| R bounded readback | Full array 2802.79 +/- 1.85 versus bounded 2805.39 +/- 3.03 M/s; 393232 -> 6160 bytes/submission, 63.8-fold payload reduction. | Throughput essentially tied; retain bounded copy without size-discovery pass. |
| R count reductions | One atomic/workgroup helped; final once/sequence atomic helped. A separate linear GPU reduction 785.4 -> 592.5 M/s. | Keep final per-stream accumulation, not that reduction prototype. |
| A-O I | Shared probes 9.299 -> 9.408; single-round initialization 9.303 -> 9.488; single-prefix specialization 9.446 -> 9.398. | First two retained, specialization removed as neutral. |
| A-O O saver changes | Two 40-second thread-pinning pairs 2.425 -> 2.510 B/s, noisy; parallel flush 2.468 -> 2.482 with one GPU stall. Later degraded-group pinning 0.910 +/- 0.014 -> 0.945 +/- 0.015, but save rates unresolved. | Correct durability, no established sustained saving gain; both removed. |
| E12 counter shards | Two header shards by stream parity, no allocation/dispatch: 12.800 -> 12.400 B/s; frequent `ab` storage-bound. | Rejected despite low structural cost; no direct contention measurement. |
| E13 hysteresis | Wait for zero after backlog exceeds eight: GPU about 0.4 -> 0.1 M/s, combined 0.2 -> 0.1 M/s. | Rejected. Units here are millions, not billions. |
| E13 calibration stop | Stop a backlogged trial and return validated selection; startup ranges above, rare throughput tied. | Retained for bounded application-controlled calibration. |

In audited E diagnostics at 256/1, GPU-only verification/reseed means were 15.96/25.15 us over 6144 hits, discovery-to-verification 186.88 ms and callback 5.954 ms. Over 4096 saves, validation/preparation/three-file work/publication averaged 12.76 us/0.647 ms/4.126 ms/1.126 ms. File opens, writes and syncs summed to 1.214/0.353/2.014 ms; remaining file time includes closing, encoding, paths and instrumentation. Callback-lock waiting was 0.14 us.

With all 32 CPU workers, the 4096-save store means were 19.27 us/0.638 ms/3.765 ms/1.140 ms, including 1.944 ms sync time; callback serialization waited 5.468 ms. A separate 3072-hit GPU-verifier sample measured 14.36 us verification, 27.61 us reseeding, 424.17 ms discovery-to-verification and 12.932 ms callback including CPU-save waiting. These differently scoped means cannot be summed into one latency breakdown. GPU drainage was 550/606 ms and process cancellation 621/756 ms. Instrumentation was removed before final timing.

The E `ab` full-procedure measurements delivered about 450 durable saves/s in both modes: initial before/after medians 449.99/445.01 GPU and 450.00/455.00 combined; startup-fix medians 435.00/449.50 and 445.00/448.51. Variation was broad. Forced Windows harness termination can leave one incomplete staging directory, so these are durable rates at measured endpoints, not drained whole-process rates. Separate cancellation tests establish graceful delivery.

<a id="7-automatic-configuration-results"></a>

## Complete-search results

### Latest paired comparison

F used a starting algorithm rebuilt with the same exact reporter as the candidate. Automatic rare acceptance had five interleaved 30-second windows per build and mode after fifteen seconds of warmup. All requested 32 CPU workers remained active and independent in combined mode. Every paired difference was positive:

| Mode | Baseline mean and range, B/s | Retained mean and range, B/s | Mean change |
| --- | --- | --- | ---: |
| GPU-only | 12.45193 (12.31275-12.53319) | 12.77415 (12.65134-12.86462) | +2.59% |
| Combined | 13.82561 (13.78429-13.84672) | 14.16478 (14.11576-14.21918) | +2.45% |

Same-window CPU means were 1.32338 -> 1.32382 B/s; GPU contributions 12.50223 -> 12.84096. Separate three-window CPU-only ranges overlapped, 1.3166-1.3257 versus 1.3204-1.3274, with no resolved regression. Rare stream selection stayed at 16384. GPU choices included initial baseline nine/candidate three rounds, then four; combined choices ranged 29-58. Slow choices were not discarded.

| Secondary workload | Evidence | Supported conclusion |
| --- | --- | --- |
| `donate+mirror+secure`, three windows | GPU 12.224-12.261 -> 12.799-12.967 B/s; combined 13.514-13.572 -> 13.981-14.193; every pair positive. | Retained combination improved this workload. |
| `privacy` | Combined every pair positive. Automatic GPU had slow seven-/nine-round choices; fixed 16384/4 repeat overlapped, 12.435-12.667 versus 12.513-12.897. | No resolved long-prefix GPU gain. |
| `hello`, automatic | GPU 12.380-12.528 -> 12.855-13.024; saves about 373-375 -> 385-389/s. Combined about 2.53-2.62 -> 2.50-2.58 B/s, consistently about 1.86% lower with different configurations and backpressure. | GPU gain and adverse combined outcome both retained. |
| `hello`, fixed 16384/4, no diagnostics | Five 60-second windows after fifteen-second warmup: 2.489-2.576 versus 2.497-2.596 B/s; saves 71.93-77.50 versus 75.16-77.23/s; CPU about 1.29 B/s. | Mixed overlap; no frequent-combined speedup. |
| `hello`, attempted fixed 16384/32 | Seven of ten starts exceeded the unchanged submission guard, leaving unpaired windows. | Unsuitable comparison, not ten successful runs. |

### Earlier complete-search evidence

The principal E arithmetic combination, before attributing E13's separate startup correction, used five interleaved external-counter runs, twenty-second windows after at least fifteen seconds warmup. Rounded reporting makes it less suitable for small effects than F. It nevertheless shows the larger joint arithmetic change against `cf413508`, with all 32 CPU workers in CPU-only/combined modes:

| Workload | GPU baseline -> combination, M/s | Combined baseline -> combination, M/s | CPU-only baseline -> combination, M/s |
| --- | ---: | ---: | ---: |
| `somethingrare` | 9650.061 -> 12549.919 | 10949.151 -> 13851.013 | 1314.579 -> 1310.382 |
| `hello` | 9649.858 -> 12350.406 | 2502.271 -> 2515.174 | 1295.237 -> 1300.065 |
| `privacy` | 9649.963 -> 12549.807 | 10998.644 -> 13759.871 | 1304.923 -> 1305.845 |
| `donate+mirror+secure` | 9600.199 -> 12250.224 | 10855.035 -> 13603.329 | 1286.171 -> 1295.530 |
| `helloeveryone` | 9649.982 -> 12599.938 | 10997.790 -> 13900.388 | Not measured in this extension |

The rare median changes are 30.05% GPU and 26.50% combined; CPU control changes are below 0.8%. These joint changes cannot be reconstructed by summing component ablations or adding E13's startup effect. The full earlier W/P/A-O results, including full-run averages and incomplete samples, remain in [Appendix A](#appendix-a-experimental-protocols-and-extended-results).

R directly compared automatic configuration with the best settings it had tested manually. Ten pairs gave 2716.56 +/- 29.31 versus 2785.94 +/- 2.88 M/s GPU at manual 16384/4 and 4044.02 +/- 19.04 versus 4099.03 +/- 9.91 combined at manual 16384/16. The 2.49% and 1.34% gaps were inside the then-current 2.5% practical band but larger than the nominal confidence intervals. A cold/driver-active GPU sample about 6% behind was retained. This was near-plateau evidence on that engine, not exact optimality or a current speed figure.

T's targeted combined rerun tightened rare selection: minimum/median/maximum changed from 9599.3/10543.3/10804.8 to 10753.5/10945.0/10994.3 M/s. Width fell from 1205.5 to 240.7 M/s, about 80% and median rose 3.8%. All five chose 16384 streams with 32/22/31/44/44 rounds. This did not solve every workload: `hello` ranged 1100.2-2504.2 and multiple prefixes 1840.6-10908.3 M/s. Two one-round cases showed 8.4/8.6 s save-log latency before later 0.04-0.05 s intervals. `privacy` had one 600-second startup timeout while CPU/save activity continued; four of five valid results remained an incomplete cell, not a survivor median.

### Separately scoped published release refresh

Available evidence after F includes the v0.4.1 release refresh in `.github/benchmarks.csv`, suite `bench-2026-10-15T16-20-41-374Z`. It uses the release binary, Go 1.27.2 and **GOAMD64 v3**, rather than the studies' v1 target, with automatic configuration, all 32 requested CPU workers, normal saving and the rounded external twenty-second procedure after fifteen-second warmup. It is not another paired F result.

| Workload | CPU-only median M/s | GPU-only median M/s | Combined median M/s |
| --- | ---: | ---: | ---: |
| `somethingrare` | 1314.1 | 12900.2 | 14292.1 |
| `hello` | 1293.7 | 12950.1 | Incomplete: 4/5 valid |
| `privacy` | 1316.8 | 12900.3 | 14200.8 |
| `donate+mirror+secure` | 1301.9 | 11850.1 | 13863.0 |

Of sixty attempts, 59 were valid. Combined `hello` round three reached the 600-second startup/mode timeout while saving, without a post-setup counter interval. No retry or four-survivor median fills that cell. The other 120 comparison rows were unchanged. The incomplete result remains a limit on the published refresh rather than being hidden by the stronger rare-prefix result.

<a id="8-correctness-compatibility-and-reproducibility"></a>

## Correctness, portability and compiler findings

### Independent gates

Arithmetic tests compare production shader operations with an independent big-integer model, including maximal words, modulus aliases, noncanonical encodings, carries, repeated squares and product-tree chains. The historical ten-limb contract had its own maximal-lazy tests. Full-key diagnostics compare consecutive compressed public keys with independent scalar-base multiplication across center transitions: initially 260 keys, later 516. Projection tests cover 2048 random/full-width cases; quadratic preparation 2048; complement fusion 4096; batched inversion 8192 against `big.Int.ModInverse`; partial products the 8192/8448 cases described above. These tests check compiled arithmetic, not just a host translation.

State-machine tests cover earliest-hit counts, both signs, all supported prefix lengths, overlapping masks, capacity-one overflow, no duplicate delivery, pause/resume, stale generations/acknowledgements, reseeding, epoch exhaustion and 4096 accepted matching streams at cancellation. Calibration tests cover activation/deactivation, cached command reuse, zero-count collection, slow successful saves during cancellation, explicit overrides, best-validated fallback, noise thresholds, repeated finalists, injected cancellation and first/subsequent backlogged trials. A per-pass publication prototype double-counted persistent overflow and was caught; final-only publication is essential. F02's initial host table-size panic was fixed before timing, not treated as a completed benchmark.

| Checkpoint | Recorded result and boundary |
| --- | --- |
| R | GPU-enabled suites 325 records: 322 pass, three skip; subsequent fallback regression brought GPU package to 21 passes under PACE and stock Go with synchronization validation. CPU CGO-disabled suites 285: 284 pass, one skip. |
| W | 403 records: 400 pass, three skip; 24 GPU tests, also checked with phase instrumentation; 516 complete keys and near-exhaustion fixtures. |
| P | 26 GPU tests; 2048 raw random arithmetic and 2048 projection cases plus full-key diagnostics. All four shader assets reproduced byte-for-byte. |
| A-O | 406 records: 403 pass, three skip; 26 GPU tests. Integrated Radeon passed 24 top-level tests except the large cancellation/budget case, despite accounting for all 4096 saves. |
| T | 28 GPU tests and synchronization checks passed; targeted custom vet passed. A repository-wide command timed out and is not a pass. |
| E | Final 35 GPU tests; prototypes had 30-33. Host/internal/root checks recorded 316/77 passes with optional device cases skipped; pure-CPU checks 248/51 with stated skips. Broad `./...` timed out traversing ignored generated-key directories; explicit root/internal checks passed. |
| F | PACE and stock-Go suites on RTX 5090 each 416 pass and three timed-CLI opt-in skips. Separate exact-enabled integration passed all four tests, covering those skips and graceful stop. Vulkan and synchronization validation enabled. |

The integrated Radeon remains a partial portability result. With driver 32.0.21045.5002, E's 4096 cancellation case failed its conservative submission budget despite correct delivered keys; F recorded 34 passes and two failed parent/subtest records for that limitation. Arithmetic, masks ordinary search and small cancellation passed. Earlier integrated-device initialization failure and later execution-budget failure are different observations. Neither is erased by cross-compilation success.

Windows/Linux/Darwin and ordinary arm64 checks were performed at their recorded checkpoints. Later Linux amd64/arm64 GPU cross-builds and target-specific custom vet passed, establishing compilation only. CPU-only builds had no GPU package or `runtime/cgo` dependency and worked with CGO disabled; no new Go dependencies were required. A Linux GNU-libc cross-build had the expected ELF interpreter; an earlier musl dynamic target did not, so the compatible GNU target was used. Windows race instrumentation failed before tests because ThreadSanitizer could not allocate its region: race validation was unavailable, not passed.

### Compiler routes and reproducible assets

Production uses precompiled embedded SPIR-V, with no shader compiler required at runtime or for builds from committed assets. E10 retained installed glslang 16.0.0 `-V --target-env vulkan1.3 -Os` over glslc `--target-env=vulkan1.3 -O`; both share frontend ancestry. Its five-run median was 12.300 -> 12.450 B/s, but one field-valued conditional was also changed to an explicit branch, so that comparison was not strictly compiler-only. A faster glslc outlier was retained.

F06 repeated identical-source controls. Rare glslang rates 12.707/12.646 versus glslc 12.505/12.508 B/s and both multiple-prefix pairs favored glslang. Separate conditional variants were unresolved with glslang and slower with glslc. With partial filtering disabled, glslang still gave 11.930/11.921 versus 11.740/11.736. The old apparent reversal was not reproduced. These support a practical generation route, not a diagnosed compiler defect or an independent-language comparison.

The direct Slang route has two distinct failures. E10 reported invalid storage-buffer atomic destinations and an internal select failure. F's minimal tests did **not** reproduce the atomic problem: native `RWStructuredBuffer`, GLSL storage and shared atomics passed on installed and pinned compilers, including used return values. Installed Slang 2025.17.2's error 99999, unhandled select, instead came from specialization-sized shared arrays; explicit fixed sizes with specialization elsewhere compiled. [Slang PR #10361](#ref-slang-select) adds specialization Select emission and [#10694](#ref-slang-rate) fixes specialization-rate propagation. Both were verified ancestors of the side-by-side 2026.19 compiler.

Pinned 2026.19 compiled and validated all four original modules, fixing the shared-array compilation failure, but failed **14 of 35 actual-device tests**: raw arithmetic, full keys, filtering/resumption and exceptional points among them. Isolated inverse and partial-product tests passed. Bindings 0-3, specialization IDs 0/1, structure strides/offsets and Shader/Int64 capabilities were checked. Root cause remains unresolved; that route was neither timed nor retained. The pinned tool was tested before the subsequent no-download constraint; installed SDK and driver were unchanged.

Generation now compiles all four modules into temporary outputs, checks each compiler and validator exit and publishes only after all succeed. An injected failure left existing hashes unchanged. The manifest records tool paths/versions, arguments, diagnostics and source/output hashes. Historical regeneration also established byte-identical shader assets; a P executable rebuild differed by 22 metadata bytes while code/assets were identical. Compiler success alone is not the correctness gate.

### Native-call stalls and lifecycle experiments

Some historical runs stalled in native calls after short GPU work or successful drainage. P's main matrix completed 69/73 baseline and 71/73 candidate attempts. Six initial baseline attempts succeeded; nine traced diagnostic runs, including three with validation, did not reproduce a stall, followed by another untraced failure. Native return addresses and Go pclntab information identified calls rather than guessing from nearby symbols.

One 4096/64 transition stalled in `vkResetCommandBuffer` after a 2.8353 s transition point, last sequence 14.654 ms and zero backlog; the caller had about 44.3 CPU-seconds, 44.2 native, at 47 s elapsed. Another stalled in `vkDestroyCommandPool` **after** wait-idle returned and after 20.255 ms drainage. Four frequent runs on baseline and candidate also blocked in reset. Of five further diagnostic runs, one steady 16384/4 reset stalled after 77.722 billion GPU candidates and 8721 submissions, last tail 14.358 ms, backlog eight. Two verbose runs perturbed behavior and were excluded from throughput evidence.

The actual blocked call's fence status was not captured. Successful program-order traces showed collection/fence completion before reuse, but that does not prove the failing call had no ownership defect. Loaded `graphics-hook64` and `amdihk64` modules establish presence, not cause. No speculative workaround or watchdog relaxation was retained.

A-O N tested per-slot individual reset and pool reset: 9.315 -> 9.346 and 9.277 -> 9.327 B/s, inconclusive. Unused unsignaled fences are legal; successful traces had 26,868/27,051 operations with `pending=false`, while failed captures did not resolve every API. E14 tried legal `VK_COMMAND_BUFFER_RESET_RELEASE_RESOURCES_BIT` after completed-slot checks, preserving reuse. Four sixty-second frequent runs had matched reset calls 189/192 GPU-only and 97/99 combined; maxima 1.48/1.68 and 2.55/1.57 ms, pool destruction 0.11-0.16 ms, cancellation/drainage 497-532 and 932-1290 ms. No hang occurred, but that small success sample cannot establish a fix. The flag was removed; `ONINO_GPU_NATIVE_TRACE` remains for reset/pool-destruction entry/return evidence. [Vulkan lifecycle requirements](#ref-command-reset) remain binding independently of measured performance.

<a id="9-conclusions-and-limitations"></a>

## Limitations and conclusions

Resident packing made shared inversion useful at GPU scale; selective recomputation and compact reconstruction then reduced its surrounding costs. The current arithmetic combines a full-width representation with exact canonical projection, conservative partial-product rejection and independent full-key verification. These mechanisms preserve ordering, exact counting and one-export-per-seed ownership even when candidates pause or overflow. Automatic tuning exploits the distinction between independent streams and rounds while keeping requested CPU workers active.

The measured gains are conditional on workload, compiler, configuration and timing boundary. Rare-prefix final comparisons favor the retained combination; long-prefix GPU changes remain unresolved, frequent combined automatic execution regressed and the published release refresh contains an incomplete cell. More private work, fewer barriers, fewer products, lower registers and fewer dispatches repeatedly failed as standalone predictors. Rejected implementations are evidence about those implementations, not universal impossibility results.

The largest remaining measured phase is reconstruction, but its native bottleneck is not established. No supported Nsight capture was available; an attempted `ngfx` launch returned ENOENT and further downloads were prohibited. Static executable properties, source counts and timestamp regions cannot substitute for active lanes, dependency/barrier stalls, native multiply forms, actual spills or cache traffic. [ZKProphet](#ref-zkprophet) motivates separating integer dependencies from occupancy; [Roofline](#ref-roofline) requires measured integer and traffic ceilings per checked candidate, not advertised floating-point TFLOPS. Those ceilings were not measured.

The remaining questions are specific: how native reconstruction work divides among instruction/dependency, barrier and memory costs; how durable-write latency distributions and callback waiting affect combined tuning; how publication lag and thermal/storage variation limit small-effect comparisons; why the corrected Slang compile route still fails arithmetic; how safe bounded configurations can be selected on the integrated GPU; and what caused historical reset/destruction stalls. Exact reporting removes display quantization, not these uncertainties. Current evidence establishes neither a global tuning optimum, universal shutdown/startup deadline, cross-vendor sustained performance nor energy efficiency.

The complete-search contract is the stable result: preserve independently verifiable keys, count only consumed candidates, retain overflow and accepted saves and compare useful work over explicit boundaries. The extended evidence below records the costs and failures that qualify the throughput gains.

<a id="5-measurement-method-and-baseline-attribution"></a>

## Appendix A: Experimental protocols and extended results

### A.1 Common environment and incompatible windows

The studies used the same local Ryzen 9 9950X3D, 16 cores/32 logical CPUs, RTX 5090, 32,607 MiB reported VRAM, PCI `00000000:01:00.0`, Windows amd64/WDDM and an active display. CPU workers used physical cores first, spread across caches, with affinity `0xffffffff`, normal priority and ordinary durable saving. R used NVIDIA 616.64, Go/PACE 1.27.1, Vulkan SDK 1.4.328.1 and glslc optimization for Vulkan 1.3. W/P/A-O used Go/PACE and stock Go 1.27.2, builder 0.5.5 and Zig 0.17.0, with driver 616.64. E/F recorded Windows 11 Pro build 26200 and driver 617.42; F used builder 0.5.6. Bun was 1.4.3 in E/F. PACE build metadata reported `go1.27.2-X:newinliner,runtimefreegc`. Study executables used CGO, dynamic linking, trimpath, amd64 v1, compatibility mode and PGO disabled. The separately labeled release refresh used v3. Validation was enabled for correctness and disabled for timing. No CPU worker was reserved for the GPU controller; R used `GOMAXPROCS = workers + 2`.

| Evidence family | Timing and aggregation | Qualification |
| --- | --- | --- |
| R | Ten alternating baseline/candidate pairs, nominal sixteen-second processes, rare `chopperepic.`; final two eligible exact snapshots with left endpoint after eight seconds, normally a four-second window. Mean +/- `t(9,0.975)*sampleSD/sqrt(10)`. | Descriptive nominal 95% intervals for each mean, not paired-difference intervals. Historical reporter interval was four seconds, current default five. |
| W/P component confirmation | Five interleaved pairs, generally 22-second processes, rare `somethingrare.`, exact diagnostic endpoints after at least eight seconds, about thirteen seconds steady. Mean +/- sample SD. | Later eighteen-second confirmation and two-pair sixteen-second screens are labeled separately. |
| A-O | Final three-pair rare comparisons used a fixed monitor window from fifteen to 75 seconds, with 78-second processes; frequent-auto used 150-second windows and 168-second processes. Exact common CPU/GPU endpoints, CPU obtained as total minus GPU. | Initial two twenty-second screens warmed for six seconds; compilation sometimes left only seven usable seconds. Do not pool them with final windows. 309 heterogeneous records, 304 complete and five failed. |
| T | Five attempts per cell; at least fifteen post-setup warmup seconds and twenty-second external rounded-counter windows. | Replaced twenty combined rows; other 160 rows unchanged. Suites `2026-10-14T04-31...` and `2026-10-14T10-49...`; incomplete cell retained. |
| E | Five interleaved runs, fifteen-second warmup and twenty-second external rounded-counter window. Two-run five-second screens only for large effects. | Short-screen quantization around 0.2 B/s cannot resolve sub-percent effects. 300 accepted rows: 120 combination, 40 long/frequent, 60 component, 60 startup and twenty population. |
| F | Fixed 16384/4 screens, five-second warmup and ten-to-twenty-second exact windows, then three thirty-second windows; final automatic rare five thirty-second windows after fifteen-second warmup. | Same exact reporter in both builds. Slower selections and outliers retained. Separate five sixty-second fixed frequent confirmation. |

Rounded-counter reporting reads cumulative display values, so endpoint error grows with the display unit. E's larger changes survive this limitation; the shortest screens are directional, not precise estimates. F checked reporter overhead with rounded fixed GPU runs: about 12.60/12.60 B/s without versus 12.50/12.55 with exact reporting, unresolved at roughly 0.1 B/s error. Automatic controls included low five-round selections and were retained. An interrupted external suite was excluded and restarted rather than mixed with completed exact windows.

E also had an execution-audit failure. An accidental full competitor harness ran on October 14 from 20:43:59 to 22:40:04 UTC; a local `node --check` compatibility shim relaunched the script at 22:47 and was stopped. All 300 accepted search rows preceded both accidental launches and had no overlap. Initial benchmark publications were restored. Overlapping arithmetic and persistence diagnostics were discarded, then repeated sequentially; only the replacements are reported here. This is why non-executing Bun parser checks, rather than that shim, were used later. The accepted evidence does not turn the accidental competitor execution into a reproduced comparison.

### A.2 Host residency and inversion packing

R's initial screens measured CPU-only 1284.681 M/s, GPU 256/4 at 273.496, one-CPU combined 338.988 and all-worker combined 1321.621. GPU 4096/2 reached 637.975; combined 8192/8 and 16384/4 reached 1942.157 and 1945.642. These identify the original scheduling problem, not current performance.

| R retained step | GPU before -> after, M/s, nominal 95% half-width | Combined before -> after, M/s, nominal 95% half-width |
| --- | --- | --- |
| Resident host bundle, 16384/4 | 691.97 +/- 1.34 -> 699.51 +/- 1.38 | 1987.95 +/- 7.41 -> 2012.00 +/- 4.98 |
| One checked atomic/workgroup, 16384/4 | 698.81 +/- 1.58 -> 787.60 +/- 1.43 | 2007.32 +/- 6.62 -> 2098.83 +/- 8.17 |
| Packed inverse, 16384/4 | 788.03 +/- 1.13 -> 2788.31 +/- 4.60 | 2094.60 +/- 8.67 -> 3514.31 +/- 63.75 |
| Final-only sequence counter, 16384/16 | 2747.32 +/- 5.31 -> 2799.30 +/- 2.55 | 4053.81 +/- 8.85 -> 4103.84 +/- 9.49 |

Equal-work old GPU rates at 4096/16, 8192/8 and 16384/4 were 621.478/667.586/691.359 M/s, compared with packed 2047.070/2527.541/2793.359. The resident host bundle is deliberately attributed as a bundle: it combined allocation/reuse, staging, bounded copies and persistent commands; only the separate device-local and bounded-readback ablations isolate those pieces.

Always-record/always-upload combined screens gave about 4096/4064 versus retained 4107/4112 M/s, while host orchestration used roughly 0.9-1.0 versus 0.02-0.04 CPU cores. The intended ten-pair confirmation did not complete because of execution guards and teardown failures. Its throughput difference is unresolved; it supports the host-work observation only. A retained steady `BenchmarkSubmitCollect` at 16384/16 measured 12.026 ms/op, 0 B/op and zero Go allocations/op, with initialization excluded and exact counters asserted. This does not measure native allocator activity.

### A.3 Word radix, batch geometry and reconstruction

| W component, five pairs | Before M/s, mean +/- SD | After M/s, mean +/- SD |
| --- | ---: | ---: |
| Phase specialization | 2811.718 +/- 6.059 | 2920.379 +/- 23.549 |
| Eight-word radix | 2937.795 +/- 2.160 | 4188.168 +/- 1.153 |
| 128 pairs/stream | 4159.145 +/- 59.790 | 4788.228 +/- 77.704 |
| Remove padding | 4848.001 +/- 10.734 | 5190.616 +/- 9.168 |
| Limb-major storage | 5143.095 +/- 71.728 | 6362.386 +/- 83.570 |

These components have different immediate controls. The first W full matrix preceded the saving-backpressure change:

| W mode, five 22-second pairs | Baseline -> candidate M/s, mean +/- SD | Same-window CPU / GPU components where applicable, M/s |
| --- | --- | --- |
| GPU manual | 2816.156 +/- 0.422 -> 6717.286 +/- 25.683 | GPU-only |
| GPU automatic | 2716.071 +/- 27.510 -> 6260.917 +/- 58.664 | GPU-only |
| Combined manual | 3536.507 +/- 103.949 -> 6285.347 +/- 151.768 | CPU 1327.051 +/- 4.358 -> 1324.086 +/- 2.201; GPU 2209.456 +/- 103.333 -> 4961.261 +/- 152.399 |
| Combined automatic | 4127.113 +/- 21.046 -> 7987.623 +/- 115.570 | CPU 1327.008 +/- 3.974 -> 1321.876 +/- 4.866; GPU 2800.105 +/- 20.890 -> 6665.747 +/- 118.799 |

A final five-pair eighteen-second manual confirmation, including the host change, gave GPU 2793.715 +/- 9.948 -> 6744.478 +/- 7.931 M/s and combined 3607.555 +/- 30.950 -> 6302.743 +/- 392.746, comprising CPU 1327.065 -> 1320.663 and GPU 2280.490 -> 4982.081. The candidate's 5642.107 M/s low combined sample was retained. A separate CPU control gave 1321.928 +/- 8.920 -> 1323.370 +/- 1.619. Two automatic follow-up pairs stopped after native stalls: pre-backpressure GPU 6211.712/6240.259 versus retained 6216.712/6211.879; combined 7999.747/8034.290 versus retained 8005.629/7977.541. They do not replace the larger earlier matrix.

Eight-prefix two-pair W controls were 2729.527/2859.520 -> 6037.143/6224.046 M/s GPU and 3508.117/3518.979 -> 7220.822/7261.903 combined, CPU contribution about 752 M/s. A GPU frequent screen before backpressure saved 1253 -> 3020 keys in sixteen seconds with 54/46 ms exit latency. For the five-pair combined backpressure comparison described in the body, full-run averages fell from 5073.855 +/- 38.935 to 2288.247 +/- 100.167 M/s alongside the deliberately reduced drainage time. Drainage ranged 5376.534-5711.509 -> 61.540-125.558 ms; exit latency ranged 5424.85-5755.64 -> 200.56-544.07 ms.

| P component, five pairs | Before M/s, mean +/- SD | After M/s, mean +/- SD |
| --- | ---: | ---: |
| Carry simplification | 6729.558 +/- 23.482 | 6799.942 +/- 17.502 |
| Exact canonical low-word projection | 6778.407 +/- 38.923 | 6875.328 +/- 33.556 |
| Provisional center in packed pass | 6862.440 +/- 28.466 | 8006.487 +/- 7.309 |
| Child-per-lane reverse tree | 7982.480 +/- 27.461 | 8660.304 +/- 26.904 |
| Internal-node-only scratch | 8668.326 +/- 36.453 | 9226.305 +/- 67.322 |

| P rare mode | Warmed baseline -> retained M/s, mean +/- SD | Full-run baseline -> retained M/s, mean +/- SD |
| --- | ---: | ---: |
| GPU automatic | 6257.305 +/- 74.713 -> 9238.235 +/- 18.232 | 6116.430 +/- 66.184 -> 8758.790 +/- 324.309 |
| GPU 16384/4 | 6710.120 +/- 18.094 -> 9234.282 +/- 42.080 | 6608.711 +/- 16.063 -> 9096.142 +/- 39.755 |
| GPU 16384/16 | 6734.008 +/- 4.204 -> 9239.360 +/- 18.481 | 6630.880 +/- 3.739 -> 9108.845 +/- 17.929 |
| Combined automatic | 8027.188 +/- 13.624 -> 10540.360 +/- 64.625 | 7324.904 +/- 50.258 -> 9544.737 +/- 91.866 |
| Combined 16384/4 | 6337.075 +/- 134.227 -> 7469.548 +/- 109.052 | 6066.703 +/- 104.475 -> 7043.432 +/- 106.709 |
| Combined 16384/16 | 7996.547 +/- 48.942 -> 10390.265 +/- 62.692 | 7724.577 +/- 61.422 -> 9938.522 +/- 52.814 |

P's automatic GPU aggregate includes five baseline and six completed candidate observations. Restricting to five complete pairs gives candidate 9238.120 +/- 20.381 M/s. The extra completed run is disclosed rather than silently discarded; failures have no throughput estimate. Same-window CPU and GPU components, also mean +/- SD, were:

| P combined mode | CPU baseline -> retained M/s | GPU baseline -> retained M/s |
| --- | --- | --- |
| Automatic | 1323.718 +/- 4.198 -> 1323.068 +/- 2.767 | 6703.470 +/- 10.259 -> 9217.292 +/- 65.096 |
| 16384/4 | 1319.925 +/- 1.604 -> 1319.688 +/- 2.081 | 5017.151 +/- 133.094 -> 6149.859 +/- 108.998 |
| 16384/16 | 1322.199 +/- 3.401 -> 1324.850 +/- 2.571 | 6674.348 +/- 45.833 -> 9065.415 +/- 64.149 |

Five separate eighteen-second CPU-only pairs gave 1318.496 +/- 2.663 -> 1319.540 +/- 1.828 M/s.

P's five-pair secondary automatic comparisons used `somethingrare.`, `anotherprefix.`, `vanityaddress.` and `hellothereab.` together or the long prefix `somethingreallyrareabcdefghijklmnopqrstuvwxyz234567.`. No run failed or saved a key in these two workloads. The following table separates warmed mean +/- SD from full-run means:

| P secondary workload/mode | Warmed baseline -> retained M/s | Full-run baseline -> retained M/s |
| --- | --- | --- |
| Four prefixes, GPU | 6178.860 +/- 102.007 -> 9089.936 +/- 34.669 | 6031.595 -> 8750.137 |
| Four prefixes, combined | 7394.494 +/- 31.846 -> 9822.532 +/- 105.092 | 6715.712 -> 8823.338 |
| Long prefix, GPU | 6218.689 +/- 18.013 -> 9253.713 +/- 29.197 | 6070.862 -> 8903.404 |
| Long prefix, combined | 7979.964 +/- 89.232 -> 10387.157 +/- 287.782 | 7303.165 -> 9445.185 |

Four-prefix CPU contribution was 800.113 -> 800.924 M/s; long-prefix CPU contribution was 1323.211 -> 1321.833. A 9891.937 M/s long-prefix combined candidate stayed in the aggregate. Frequent GPU completed five of seven attempts for each build, with four complete pairs; combined frequent completed five/five. Their mean +/- SD results were:

| P frequent mode | Warmed baseline -> retained M/s | Full-run baseline -> retained M/s | Saves/s, baseline -> retained |
| --- | --- | --- | --- |
| GPU | 6068.022 +/- 320.846 -> 8512.348 +/- 798.593 | 5930.870 +/- 230.585 -> 8301.591 +/- 557.424 | 175.721 +/- 8.078 -> 245.704 +/- 17.579 |
| Combined | 2430.016 +/- 83.038 -> 2455.783 +/- 66.213 | 2321.892 +/- 59.992 -> 2355.838 +/- 41.146 | 68.816 +/- 2.840 -> 70.781 +/- 3.695 |

Combined frequent CPU contribution was 1259.791 -> 1258.976 M/s. GPU exit latency ranged 39.842-50.477 -> 43.628-50.299 ms; combined exit latency 194.528-255.321 -> 211.871-276.223 ms.

P also compared tuner changes separately. Extending finalist scores to 750 ms added about 1.35 s startup without useful benefit and was reverted. Tightening the then-current practical band from 2.5% to 1% gave five GPU-auto means 9080.514 +/- 113.350 -> 9228.907 +/- 36.818 M/s, four wins; combined 10538.840 -> 10536.123 was tied. These precede T's repeated-finalist policy.

### A.4 Conditional corrections and cached probes

The A-O B+I combination was compared with a baseline-engine-equivalent observer build, not the original byte-identical executable. Before that observer, fresh 24-second baseline runs gave GPU 16384/4 at 9.356/9.236 B/s and combined automatic 10.537/10.426. Explicit combined 16384/4 gave 7.178/7.054. This reproduced the configuration sensitivity before any candidate claim.

| B+I confirmation | Pairs | Warmed baseline -> retained B/s, mean +/- SD | Full-run baseline -> retained B/s |
| --- | ---: | --- | ---: |
| Rare GPU automatic | 3 | 9.328 +/- 0.028 -> 9.656 +/- 0.049 | 9.236 -> 9.480 |
| Rare combined automatic | 3 | 10.655 +/- 0.013 -> 10.972 +/- 0.024 | 10.374 -> 10.686 |
| Rare GPU 16384/4 -> 32768/4 | 3 | 9.278 +/- 0.026 -> 9.782 +/- 0.029 | 9.240 -> 9.723 |
| Four prefixes GPU automatic | 2 | 9.203 +/- 0.084 -> 9.572 +/- 0.028 | 9.113 -> 9.472 |
| Four prefixes combined automatic | 2 | 10.004 +/- 0.039 -> 10.359 +/- 0.011 | 9.709 -> 10.062 |
| Long prefix GPU automatic | 2 | 9.307 +/- 0.025 -> 9.639 +/- 0.104 | 9.208 -> 9.537 |
| Long prefix combined automatic | 2 | 10.686 +/- 0.030 -> 11.012 +/- 0.001 | 10.387 -> 10.697 |
| Frequent GPU 16384/4 | 2 | 9.258 +/- 0.006 -> 9.637 +/- 0.020 | 9.183 -> 9.573 |
| Frequent combined 16384/4 | 2 | 2.428 +/- 0.056 -> 2.461 +/- 0.034 | 2.414 -> 2.425 |
| Frequent GPU automatic, 150 s | 3 | 8.959 +/- 0.255 -> 9.030 +/- 0.661 | 8.907 -> 8.983 |
| CPU-only rare, all workers | 3 | 1.325 +/- 0.002, engine unchanged | Separate control |

Paired rare automatic percentage changes were +3.520%, nominal 95% interval [2.362%, 4.678%], GPU and +2.975%, [2.146%, 3.804%], combined. CPU means were 1.323283 -> 1.324003 B/s and GPU contributions 9.332 -> 9.648. The manual +5.434% includes the population change; approximately 2.1% was the isolated population increment at that stage. Explicit combined comparisons remained unresolved: controls 7.504/7.209/7.239 versus candidates 7.201/7.362/7.275 B/s.

Population screens A-O D gave 24576 streams 9.298 -> 9.316, 32768 9.222 -> 9.467 and 49152 9.232 -> 9.463 B/s. A 65535-stream count bound of 1,073,725,440 was checked but not timed. Inversion workgroups 32 and 128 gave 9.293 -> 9.336 and 9.286 -> 9.278, inconclusive. Explicit 32768 support remained, automatic cap did not expand.

Frequent automatic GPU controls were 9.174/9.026/8.677 versus candidates 9.445/9.377/8.267 B/s. The low 8.267 sample was retained. An earlier 6.211 B/s case selected three rounds, kept the GPU almost entirely busy and had backlog eight; an idle-GPU explanation was unsupported. Saves averaged 266.618 +/- 8.583 -> 269.626 +/- 19.740/s. Fixed GPU saves were 277.122 +/- 0.152 -> 289.256 +/- 1.192/s in-window and 274.471 -> 286.858 whole-run. Fixed combined saves were 72.660 +/- 1.723 -> 73.065 +/- 0.620 in-window but 72.289 -> 71.908 whole-run, no improvement. Its completed-candidate paired-gain interval was approximately 1.382% +/- 8.552 percentage points, also inconclusive. Integrated combined drainage took 74.599-83.065 ms and exit 232.794-292.246 ms; GPU-only drainage took 4.639-14.079 ms. Combined gaps consumed 67.5-68 of 78 seconds; verification was about 16.5 us, reseeding 20-30 us, saving 2.78 ms GPU-only versus 13.27 ms combined, with a 26.88 ms combined callback.

The later thread-pinning confirmation belongs to a degraded entire run group: three 60-second pairs, 0.910 +/- 0.014 -> 0.945 +/- 0.015 B/s, full-run 0.899 -> 0.925. Paired improvement was 3.868 percentage points with 2.970-point nominal 95% half-width, but saves 27.450 +/- 0.539 -> 27.866 +/- 0.636/s did not resolve a gain. CPU activity was only nine to ten cores despite 32 requested, exit 1.78-2.37 s. The cause was unresolved; these runs were not pooled with healthier groups.

Five A-O records failed: baseline automatic, two pool-reset automatic, one parallel-file and one B+I fixed 78-second run. Their missing final numbers remain missing and an unmatched completed control was retained. A successful subset does not repair those attempts.

## Appendix B: Profiling, startup and resource observations

### B.1 What timestamp and executable properties measure

`ONINO_GPU_PROFILE=1` adds phase queries separately from throughput confirmation; ordinary execution retains whole-sequence timestamps only. Phase intervals include their dependency boundaries, not isolated ALU time. [Pipeline executable properties](#ref-executable-properties) report compiler resources; [Nsight's shader-profiler documentation](#ref-nsight) explains the stronger active/stall measurements that were unavailable. Utilization is not occupancy, reported local memory was sometimes implausible and SPIR-V operation names do not establish native multiply instructions.

| Checkpoint | Preparation / inversion / reconstruction shares | Registers/thread | Shared bytes | Native code bytes |
| --- | --- | --- | --- | --- |
| W specialized ten-limb comparison | 22.6% / 13.2% / 64.3% | 96 / 96 / 128 | Historical comparison | Historical comparison |
| W retained word-radix | 19.25% / 12.55% / 68.20% | 63 / 72 / 96 | 8212 / 3392 / 9760 | 36992 / 131456 / 129280 |
| P control | 19.295% / 12.598% / 68.107% | 63 / 72 / 96 | 8212 / 3392 / 9760 | W boundary |
| P retained | 23.607% / 17.246% / 59.147% | 63 / 72 / 96 | 8212 / 3392 / 9760 | 34688 / 183552 / 64512 |
| A-O control | 23.598% / 17.370% / 59.032% | 63 / 72 / 96 | 8212 / 3392 / 9760 | 34688 / 183552 / 64512 |
| A-O B+I | 24.145% / 17.676% / 58.179% | 63 / 64 / 92 | 8212 / 2880 / 8212 | 35712 / 189824 / 64512 |
| E retained | Per-round times below | 63 / 128 / 96 | 8212 / 5952 / 8212 | 39424 / 138624 / 83072 |

R's packed pipeline reported SPIR-V 159,520 bytes, native 351,104, 128 registers and 5920 shared bytes, versus the monolithic 168 registers and 7968 shared bytes. Its roughly 68.7 GB local-memory statistic was implausible and excluded from interpretation. W search SPIR-V was 153,860 bytes; P 150,112. A-O unrolling grew SPIR-V from 150,112 bytes/9331 instructions/nine loop-control sites to 249,904/15568 for a groups-of-five variant with eight loops. Native scheduling and unrolling cannot be inferred from that alone.

| Per completed population-round phase time, us | Preparation | Inversion | Reconstruction |
| --- | ---: | ---: | ---: |
| A-O control | 104.695 | 77.066 | 261.904 |
| A-O B+I | 104.329 | 76.375 | 251.390 |
| E control, 36,003 measured rounds | 104.96 | 76.13 | 252.82 |
| E combination, 58,167 measured rounds | 75.18 | 38.99 | 214.77 |
| F starting depth2, 804 rounds | 72.021 | 36.669 | 204.149 |
| F retained filtering depth2/WG64, 804 rounds | 72.366 | 36.790 | 199.164 |
| F retained depth3/WG64, 804 rounds | 67.004 | 49.665 | 190.485 |

E total run lengths differ, hence the normalization by completed rounds. The individual baseline/combination runs initialized in 125.07/283.50 ms and created pipelines in 2.19/3.53 ms; these are not cold-cache distributions. P reported zero stack bytes for all three phases. F reconstruction changed from 96 registers/83072 native bytes/8212 shared bytes to 92/73984/8212. Its complete frontier matrix follows; all nine variants passed correctness:

| Frontier depth | Inversion workgroup | Preparation us | Inversion us | Reconstruction us |
| ---: | ---: | ---: | ---: | ---: |
| 1 | 32 | 77.504 | 35.026 | 207.969 |
| 1 | 64 | 77.863 | 33.878 | 207.972 |
| 1 | 128 | 77.837 | 34.897 | 207.662 |
| 2 | 32 | 71.897 | 37.700 | 199.861 |
| 2 | 64 | 72.366 | 36.790 | 199.164 |
| 2 | 128 | 72.148 | 38.233 | 199.541 |
| 3 | 32 | 66.378 | 52.799 | 189.818 |
| 3 | 64 | 67.004 | 49.665 | 190.485 |
| 3 | 128 | 66.159 | 52.990 | 191.258 |

Inversion registers/native bytes for depths one/two/three were 96/109952, 128/138624 and 168/193152; WG64 shared bytes 4672/5952/8000. Filtering disabled increased reconstruction to 220.542 us. Preserved siblings and delayed values correlated with slower reconstruction, but F02-F05 native resources often remained identical. These controls locate effects without identifying native dependencies. F's driver-local-memory report near 64 GiB was not accepted as spill volume. `OpIMul` on uint64 versus `OpUMulExtended` in SPIR-V does not establish how many native 32x32 operations executed.

### B.2 Host gaps, transfers and CPU activity

R's exact diagnostic snapshot distinguishes genuine combined work from adding independent rates. CPU-only was 1313.746 M/s. Over 6.002 s GPU-only, 7.004 s with one CPU worker and 7.031 s with all workers:

| R quantity | GPU-only | One CPU worker + GPU | All CPU workers + GPU |
| --- | ---: | ---: | ---: |
| CPU M/s | 0 | 63.589 | 1311.966 |
| GPU M/s | 2676.187 | 2678.584 | 2812.562 |
| Combined M/s | 2676.187 | 2742.173 | 4124.528 |
| Submissions/s | 1276.1 | 1277.2 | 63.9 |
| Mean device sequence ms | 0.777 | 0.776 | 15.657 |
| Mean inter-sequence gap ms | 0.00681 | 0.00667 | 0.00636 |
| Mean host-copy us | 0.049 | 0.054 | 0.129 |
| Mean record us | 0.497 | 0.547 | 1.135 |
| Mean submit us | 13.72 | 14.51 | 42.68 |
| Explicit readback bytes/s | 7860808 | 7867850 | 393399 |
| Logically consumed header bytes/s | 20418 | 20436 | 1022 |

Those rates describe explicit recorded copies, not PCIe traffic. Lifetime activity was 45 command recordings and 2,162,688 uploaded bytes in the reported no-hit sequence. P's two additional warmed 45-second pairs per setting, sampled only after 25 controller seconds, measured automatic GPU orchestration 0.039 -> 0.016 cores (candidate range 0.006-0.027), but manual 0.022 -> 0.035. Corresponding steady rates were 6198.884 -> 9239.612 and 6688.141 -> 9178.940 M/s. Driver workers consumed 6.4-8.2 CPU-seconds over eleven seconds; that was not controller CPU time. Successful frequent GPU windows averaged 0.494 host cores. F GPU gap fractions were about 0.50-0.52% for rare/long/multiple and 1.40-3.90% for `hello`; host backpressure is not shader execution.

W's whole-process manual GPU CPU use was 0.028-0.115 cores, including a cold-compilation high sample; manual and automatic combined used 31.30-31.44 and 30.88-31.23. A-O's CPU-only check used about 31.35 cores and combined rare 31.30-31.34. These full-process observations are distinct from P's warmed controller windows.

### B.3 Startup, drainage and allocation

| Checkpoint | First useful work / selection / tail observations | Drainage and exit |
| --- | --- | --- |
| R warmed | First GPU 0.13-0.15 s, combined 0.30-0.63; selection 4.35-4.70 / 5.06-5.49 s. New shader variant first 1.86-2.13, selection 6.1-6.4; an earlier observation 2.54/6.82. No cache flush established. Max sequences 17.2-18 ms, startup gap 159 ms. | Typical drain 0.5/0.75/11.3 ms across GPU/one/all CPU, exits 36-84 ms; some 1.2-1.55 s device/instance destruction, one unresolved 30-second watchdog. |
| W | Cold pipeline 1.916 s, initialization 2.053, first 2.206; warm first GPU 133-140 ms, combined 318-367. Selection about 4.62/5.1 s, frequent 5.422-5.987. Manual tails 3-3.2 ms, auto 14.9-22.9; combined manual gaps 20-26 ms, startup gap 253. | Manual exits 38-48 ms, automatic GPU/combined 405/278; baseline once 1.52 s. Three follow-ups killed at 30 s after wait-idle marker or 4096/64 tail 14.8 ms. |
| P | Max first useful 1.796 s, selection 6.296, sequence 21.012 ms. | Frequent GPU drain 0.519-6.835 ms, combined 66.419-126.623; exits 43.628-50.299 / 211.871-276.223. Native stalls separately reported. |
| A-O | Max sequence 20.613 ms; warm first 138 ms/selection 4.62 s, cold 1.775/6.281 s; combined first 352-404 ms, selection 4.843-4.898. | Population comparison exits about 40 vs 38 ms; degraded saver group 1.78-2.37 s. |
| F | First uncached initialization 1.59 s, selection 8.60; warm init 0.13-0.14, selection about 7.15. Combined init 0.27-0.31, selection 7.68-7.88. Uniform/specialized cold pipeline observations 1.24/1.29 s versus cached 1.8-2.1 ms. | Observations do not guarantee cache state or deadlines; dedicated stop tests, rather than forced harness exit, establish drainage. |

R's frequent `a,ab,b` one-second run checked 12,638 and saved 795, draining in 1.082 s with observed backlogs 255/511. A two-second long-prefix run (51 characters plus a twelve-character probe) checked 4,366,761,984 with no hits and drained in 15.8 ms. These exercise state semantics, not sustained rare throughput.

The finer W automatic selection ranges were 4.619-4.637 s GPU and 5.109-5.217 s combined. Manual GPU gaps were 0.22-0.35 ms; rare automatic drainage was 0.52-0.54 ms GPU and 5.94-22.04 ms combined. P's slowest first useful work included 1.656 s pipeline creation. Warm rare first work was about 0.133 s GPU and 0.284-0.441 s combined, with selection 4.61-4.65 and 4.79-5.10 s; frequent combined reached 5.928 s. P rare explicit exits ranged 36.918-51.299 ms; automatic GPU exits 41.854-359.119 ms and combined 67.495-269.184 ms, with respective drainage 1.020-2.789 and 6.811-18.963 ms.

R allocations at 16384 streams were state 86,441,984 bytes, table 7,936, two command/upload slots of 2,162,688 each and two 6,160-byte readbacks. W state grew to 136,380,416, table 12,528 aligned and commands 1,769,472; P compacted state to 70,844,416. The old 8,192-byte allocation allowance and 64-lane capability bound are historical; current allowance and geometry are in the architecture section. Device heap observations of roughly 33.75/33.11 GB and memory flags 1/6 describe that device, not a portable host/device split.

A-O's explicit 32768 versus 16384 comparison had first work 434-447 versus 288-297 ms. Its resource ranges were:

| A-O manual quantity | Control 16384/4 | Retained 32768/4 |
| --- | --- | --- |
| Peak working set, MiB | 85.945-86.281 | 93.004-93.211 |
| Private process memory, MiB | 206.383-208.414 | 291.438-294.402 |
| Initial uploaded MB | 1.769472 | 3.538944 |
| Lifetime readback over 78 s, MB | 264.252-265.681 | 139.019-139.764 |
| Maximum sequence, ms | 2.217-2.387 | 4.200-4.288 |
| Accumulated gaps, ms | 289.120-294.103 | 156.566-161.004 |
| Process-exit latency, ms | 38.142-39.698 | 39.904-41.713 |

Readback fell because submissions were less frequent. Warm GPU automatic first work ranged 138.421-145.003 -> 138.379-138.820 ms and selection 4.631-4.635 -> 4.616-4.647 s. Integrated automatic GPU readback was 286-553 MB per 78 seconds versus about 53 MB combined, with 1.769 MB uploads. GPU automatic gaps totaled 474-763 ms, combined rare gaps 2.027-2.244 s. Automatic exits ranged 43.944-51.894 ms GPU and 92.522-238.346 ms combined. Allocation changes do not prove cache hits or memory-bandwidth limits.

Externally sampled process memory varied independently of exact stream-state allocation:

| Checkpoint/mode | Peak working set, MiB | Private process memory, MiB |
| --- | --- | --- |
| W warm manual GPU | About 86; cold sample 278 | About 291, versus control 252-254 |
| W warm manual combined | About 97 | 314-317, versus control 273-276 |
| W automatic GPU | 249-283 | 530-637 |
| W automatic combined | 99-100 | 320-324 |
| P rare explicit GPU, mean | 85.765 | 219.941 |
| P rare explicit combined, mean | 96.602 | 243.462 |
| P automatic GPU | 99.496-332.504 | 407.422-554.504 |
| P automatic combined, mean | 98.834 | 250.818 |
| A-O integrated automatic GPU | 265-363 | 494-547 |
| A-O integrated automatic combined | About 99-100 | About 238-242 |

R telemetry showed GPU clocks 2895-2910 MHz, memory 14201 MHz, 61-65 C, 432-450 W against a 575 W limit and 99-100% utilization. Device-global memory was 1406-1648 MiB; process working/private peaks varied about 336/446, 393/790 and 97/280 MiB across modes. A-O observations ranged 45-70 C, 2430-2887 MHz and 63.47-579.21 W, with throttle masks 400/4. These were not fixed-clock experiments. No CPU power, energy per candidate, measured PCIe traffic, sustained integer roof or hardware occupancy was established.

## Appendix C: Artifacts and reproduction

### C.1 Revision and artifact identity

Revision identity is separate from binary identity, instrumentation and generated assets. The original W requested reference was unavailable; the measured control is explicitly listed in the development map. Only the search module is embedded in the production CLI; arithmetic, diagnostic and epoch modules are test assets. Historic filenames under `measurements/` identify recorded artifacts even where cleanup subsequently archived them.

| Artifact | SHA-256 |
| --- | --- |
| W baseline `gpu20261012-baseline.exe` | `6663a4a9f8f1d8db7ab6f34740d2bbe0383677ea5b021770a8179216d3c679a7` |
| W search | `a69f279d5af3f87bbd0e685dc1a34bf36ce472a8baf21bd650d647aa1e92de7d` |
| W arithmetic | `a9525fb7c9790dd52e8d35c6dfafaecba9aa9a3b20d20ce756460953f56addb6` |
| W diagnostic | `02f4f6c7c25fe2b7a620f23f14db9f2fa045eb789adeae0fc5afeb5d6f54700f` |
| W epoch | `c4ca15ad969f447931bae206a93cd7a77afc24241c65446e39f5d210d5004358` |
| P baseline `gpu20261013-baseline.exe` | `931f308a0ff4163363077b51bf2155e7c755faaea43735e817e526cbe30f56e2` |
| P retained `gpu20261013-final.exe` | `479502e3c43fdc1655bdaa447e818f78c6a73d3ec41ab1cfb51dd4855878ab6c` |
| P search | `95751da9e2251d81757e7099864d53192c6720aca2cecea6af458e1e09db9d7b` |
| P arithmetic | `6ae18467b2fd1b6dac9e3366a12b16e6344edaeeb14470092bf730952f1e82c6` |
| P diagnostic | `52220c2eddeb7b5f7181a60b698d79034dc7bc35cffa4ea8c694cdc4a24825b5` |
| P epoch | `388ef96074c572a469a3bc7280dfb1840ec19c76867001073a5a2aaf03339c69` |
| A-O original `study-baseline.exe`, 4,995,072 bytes | `5c474d7d1ad7db22e60cc630b8c9bbd4202fbc6a63c1b04e9993cdb088dd922a` |
| A-O observer control | `da2afffe20d185bb5404a9c2a562397982e4b062047a7f577b349a60165e52fc` |
| A-O instrumented integrated executable | `e488d7a34c808c971b0ed377c95c403aac97eec3b4c8054d46ebd017fa648041` |
| A-O production executable, 5,006,336 bytes | `af59c79187f63ffaa5d972b484ef5ae7b1ecc3ed358a09234c8f930062510a91` |
| E baseline | `aada319afc2a10aae00bd839e72486e45c765d22833169705d3c4b8f66d5b640` |
| E arithmetic combination before E13 | `4ed6d31f7b2dea8c46f83c43c47534dc1c354a1e5158394c8bfa2da487b9c2e4` |
| E cleaned delivery, 4,981,248 bytes | `41280b47bba4a560b9be3ee8165d11b468748d672c7454bde51f5af4eb78a21f` |
| E production/F starting search, 123,588 bytes | `0c47c0edf46d2f2ebed5918705990e508d59c7e9728d83a4a1cba82cff95f553` |
| F exact baseline | `541ac1b7815e8e4660f6ca5f3ac2dcd5f259c93f7890523b549a9c641b833685` |
| F final executable | `5bbe33739ee9494fafb70a26b07a17bce6202d1d568f411f3afd144448233669` |
| F search | `8c2f8c176f170d0f41b9aba51a46d28745690eca6ccf806867df55b0c41cf869` |
| F arithmetic | `513d4121f2f3231d5932b4edfa00e69f2b378b0d8496ab7b5356508122e1e2ba` |
| F diagnostic | `ed8e209f54e33e708ea31d312f8f14db079528d2c64d1df962d372617b42ce46` |
| F epoch | `bb45da7ae56d202059cedb4d6c65e6c5edd4c271b31481c37fd59b748b4667e2` |
| v0.4.1 release refresh executable | `18b551777a0bdf6a032f066af30cbb69f3711d12b8c76761454bea8ed691bbba` |

F's artifact ledger covers 39 executables and 56 benchmark suites; `measurements/f-study/final-generation.json` records tool and module identities. The evidence archive `measurements/2026-10-15-evidence.tar.zst` and its inventory retain earlier local provenance, including original paths, hashes, removed top-level entries and verification status. These are research evidence, not normal build dependencies. A reproduction should preserve its own artifact identities rather than overwrite the recorded names. Current source comparison was made against checkout `db2bc74`; no performance experiment was run for this document rewrite.

### C.2 Recorded build and validation procedures

The following are **recorded reproduction procedures**, not commands executed as part of this editorial change. Paths assume the repository root, existing output directories and the stated tool versions. POSIX environment assignments need equivalent `$env:` settings in PowerShell. Builds, tests, profiles and timing must be sequential to reproduce the recorded uncontended procedure.

F's fail-fast shader generation and executable build were:

```sh
bun internal/gpu/generate.mjs tools/vulkan-sdk/Bin/glslang.exe tools/vulkan-sdk/Bin/spirv-val.exe measurements/f-study/final-generation.json
builder build go --cgo --pace --dyn --compat --no-gen --output measurements/f-study/final.exe -tags gpu -pgo=off
```

The control used the same build command and reporter instrumentation at its recorded baseline revision. Normal builds consume embedded shaders and need no generation step. W/P used glslc `--target-env=vulkan1.3 -O`; E/F retained glslang 16.0.0 `-V --target-env vulkan1.3 -Os`, followed by SPIRV-Tools validation. Recorded tools included shaderc `v2023.8 v2025.3-10-gc7e73e8`, glslang frontend `11.1.0-1302-gd213562e` and SPIRV-Tools `v2025.4-0-g7f2d9ee9`. See [the shader build instructions](internal/gpu/BUILD.md) and [generation script](internal/gpu/generate.mjs) for the checked-in route.

Representative device and exact-progress gates were:

```sh
ONINO_VULKAN_TEST=1 VK_LAYER_PATH="C:/Users/Laura/onino/tools/vulkan-sdk/Bin" VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT builder test go --cgo --pace --dyn --compat --no-gen -tags gpu -vet=off -count=1 -p=1 ./internal/gpu
ONINO_VULKAN_TEST=1 VK_LAYER_PATH="C:/Users/Laura/onino/tools/vulkan-sdk/Bin" VK_LAYER_ENABLES=VK_VALIDATION_FEATURE_ENABLE_SYNCHRONIZATION_VALIDATION_EXT builder test go --cgo --pace --dyn --compat --no-gen -tags gpu -vet=off -count=1 -p=1 .
ONINO_PROGRESS_TEST=1 ONINO_EXACT_COUNTERS=1 builder test go --cgo --pace --dyn --compat --no-gen -tags gpu -vet=off -count=1 -p=1 -run 'Test(CPU|GPU)ProgressIntegration' .
```

Omitting `--pace` selects the recorded stock-Go control; `ONINO_VULKAN_DEVICE=1` selected this host's integrated device. CPU-only/purego checks omit CGO/GPU flags and optionally add `-tags purego`. Custom `vet` was run separately for relevant targets; the `-vet=off` above prevents duplicate ordinary vet during tests, not exemption from checking. Historical broad `./...` commands were later replaced by explicit root/internal targets because ignored generated-key directories caused traversal timeouts. Linux amd64 GPU builds used builder's GNU dynamic-CGO route; later Linux arm64 GPU builds also used CGO, while earlier Linux/Darwin arm64 checks were ordinary CPU builds. Cross-builds are not device runtime validation.

### C.3 Complete-search and diagnostic reproduction

The retained F rare comparison used:

```sh
bun bench/run.mjs --only onino-gpu,onino-combined --onino measurements/f-study/final.exe --compare measurements/f-study/exact.exe --experiment final --exact 1 --workloads somethingrare --runs 5 --warmup 15 --seconds 30
```

E's earlier rounded-counter combination used:

```sh
bun bench/run.mjs --only onino-gpu,onino-combined,onino-cpu --onino measurements/e-release.exe --compare measurements/e-study-baseline.exe --experiment E-final --workloads somethingrare,hello,privacy,donate+mirror+secure --runs 5 --warmup 15 --seconds 20
bun bench/run.mjs --only onino-gpu,onino-combined --onino measurements/e-release.exe --compare measurements/e-study-baseline.exe --experiment E-long-frequent --workloads helloeveryone,ab --runs 5 --warmup 15 --seconds 20
```

Earlier W/P local runners created a separate Windows console process group, set affinity `0xffffffff`, alternated pair order, kept verification/saving enabled and sent Ctrl-Break at the requested duration. They recorded exact diagnostic samples, final counters, process CPU time, memory peaks and exit latency under `measurements/gpu20261012-{name}-{pair}-{cpu}-{configuration}-{binary}.{json,log}`. W `final.exe` means the arithmetic/layout combination before backpressure; `backpressure.exe` and fresh `retained.exe` include the host change. Prototype executables may require their archived experiment flags; production requires none.

Without those local runners, the fixed phase arrangement is `onino --cpu off --gpu auto --gpu-streams 16384 --gpu-rounds 4 --gpu-diagnostics somethingrare.` with `ONINO_GPU_PROFILE=1`. Graceful termination yields accumulated phase statistics. Replace `--cpu off` with the requested worker count for combined runs and omit explicit dimensions for automatic selection. `ONINO_GPU_NATIVE_TRACE=1` is a separate lifecycle diagnostic and perturbs timing. R reproductions require its old 64-pair revision; using its settings on the current 128-pair engine is not the same workload. A-O fixed snapshots require its matching observer builds. E/F external harness endpoints do not include graceful drainage, whereas earlier Ctrl-Break final averages do.

### C.4 Historical arithmetic contracts and source scope

R's ten alternating 25/26-bit limbs distinguished tight limbs below their radix and lazy limbs below three times it. The maximum multiplication accumulator was 5,046,283,313,212,293,387, below `2^63`; arbitrary chains of lazy additions were not permitted. After the first fold, the low limb was below `2^37` and limb one below its radix plus 2048. Normalization left the low limb below its radix plus 57, making a second pass safe. Fermat inversion used 254S+11M. These bounds explain the historical implementation; current eight-word arithmetic deliberately accepts a different, full-width domain.

The A-O pre-partial-filter, Fermat round census was 1301M, 511S, 259 additions and 514 subtractions, with 2071 fold and 514 subtraction opportunities. It is not a current operation count after quadratic preparation, partial filtering and batched inversion. Likewise, a theoretical independent multiply reduction does not account for shared-memory synchronization, native scheduling, conversion, filtering or saving.

The contemporary README figures of 2.810 B/s onino, 10.392 B/s Prefix32 and 4.115 B/s combined were historical published context, not a fresh reproduction in W/P. Direct implementation attribution applies to Prefix32's field operations and libsecp256k1's inversion method; onino's quotient-error filter proof and quadratic preparation derivation were independently established. GPUonion informed a rejected differential-walk prototype. [CUB BlockScan](#ref-cub), [Decoupled Fallback](#ref-fallback), [cuPQC 0.6](#ref-cupqc), [global-memory guidance](#ref-memory) and [subgroup documentation](#ref-subgroups) supplied design/methodology context only: their addition scans, PQC kernels, caches and forward-progress results are not multiplication-tree or onino speed measurements. [Simultaneous-inversion dataflow](#ref-simultaneous) informed A-O K's comparison, without importing a measured gain.

## Appendix D: Experiment index and historical navigation

The index preserves identifiers while organizing the evidence by mechanism. A-O retained B and I; E retained E01-E04, square-complement E09, the qualified compiler route E10 and calibration stop E13; F retained the confirmed F02/F03/F05 combination and F08 depth3/WG64. This does not imply that every variant under a retained identifier won.

| IDs | Topic and location | Distinct rejected or unresolved variants retained in the record |
| --- | --- | --- |
| R host, counter, packing | Architecture, coordination, A.2 | Always-record/upload incomplete confirmation; linear reduction; stored intermediates; delayed/sequential products; overflow duplication; stalls. |
| W arithmetic/layout | Arithmetic, reconstruction, A.3 | Multiple roots, global prefix, extended multiply, larger batches, serial pairs, shared transpose, exceptional alternate-Y; partial follow-up matrix. |
| P reconstruction/tuning | Reconstruction, tuning, A.3 | Subgroup reverse, fused dispatch, Karatsuba, Comba, 750 ms finalists; unmatched successes and reset/destruction failures. |
| A, C, H, K | Reconstruction | Immutable products, padded metadata, smaller checkpoints, mixed-product storage and complement products. |
| B, E, F | Arithmetic | Conditional corrections retained; square scheduling/unrolling and per-step ordinary/half-delta inversion rejected. |
| D | Work distribution and A.4 | Workgroups, population extension and two/four-lane inversions; automatic cap unchanged. |
| G, J | Reconstruction | Weighted private batches, alternate-Y batch, center candidate. |
| I, L | Coordination and bounded source search | Shared probes/single-round initialization retained; prefix specialization and unroll combinations not retained. |
| M | Cooperative arithmetic | Correct supported tensor-core prototype slower; initial capability gate corrected. |
| N, O | Lifecycle and saving | Reset policies inconclusive, thread pinning/parallel flush no resolved saving gain, degraded group distinct. |
| T | Tuning and complete-search results | Rare stability improved; multiple/frequent variation and incomplete privacy startup remain. |
| E01-E04 | Arithmetic and frontier | 64-lane preparation, first partial-product compilations and alternative depths qualified; batched inversion distinguished from A-O F. |
| E05-E08 | Population, private batches and arithmetic | Automatic 32768 expansion, deep private batching, differential walk and phase-specific radix29 rejected. |
| E09-E12 | Arithmetic, compilers and coordination | Multiply-complement neutral; E10 source/compiler confound; metadata separation unconfirmed in combination; counter shards slower. |
| E13-E14 | Calibration and lifecycle | Zero-backlog hysteresis slower; stop-on-backlog retained; release-resources reset not a demonstrated fix. |
| F01-F03 | Reconstruction/filtering | Lifetime changes, specialization and per-sign branching rejected; uniform mode, shared traversal and eligible low32 retained. |
| F04-F06 | Reconstruction, arithmetic and compiler | Immutable siblings/subgroup leaves/extended multiply rejected; identical-source compiler controls separate from conditional variants; Slang runtime failures. |
| F07-F08 | Inversion/frontier | No repeated unroll gain; full depth/workgroup matrix; population alternatives and frequent-combined regression qualified. |

<a id="10-radix-232-and-cooperative-batches-2026-10-12"></a>
<a id="11-reconstruction-and-exact-prefix-projection-2026-10-13"></a>
<a id="12-systematic-throughput-study-2026-10-13"></a>
<a id="13-autotuning-stability-and-targeted-benchmark-rerun-2026-10-14"></a>
<a id="14-gpu-arithmetic-and-execution-study-2026-10-14"></a>
<a id="15-reconstruction-efficiency-and-exact-measurement-2026-10-15"></a>

Historical report sections 1-9 map to R; 10 to W; 11 to P; 12 to A-O; 13 to T; 14 to E01-E14; 15 to F01-F08. Their conclusions are integrated into the relevant thematic sections above. The revision/artifact tables preserve the provenance needed to distinguish an earlier result from current behavior.

## References

<a id="ref-prefix32"></a>
**Prefix32.** 0xROOTPLS, *Prefix32 v2.2.0*, commit `fc077b3cb6df9364d755a0dfcfffff76cfd7579e`. Direct sources: [`kernel/vanity.cl`](https://github.com/0xROOTPLS/Prefix32/blob/fc077b3cb6df9364d755a0dfcfffff76cfd7579e/kernel/vanity.cl) and [`src/gpu.rs`](https://github.com/0xROOTPLS/Prefix32/blob/fc077b3cb6df9364d755a0dfcfffff76cfd7579e/src/gpu.rs). MIT-licensed arithmetic adapted in onino; other engine policies and benchmark figures are separately scoped.

<a id="ref-safegcd"></a>
**libsecp256k1.** Bitcoin Core contributors, *The safegcd implementation in libsecp256k1* and *32-bit modular inversion implementation*, commit `04b91ce295dad7b827ae18eeeed062135a918dae`: [`doc/safegcd_implementation.md`](https://github.com/bitcoin-core/secp256k1/blob/04b91ce295dad7b827ae18eeeed062135a918dae/doc/safegcd_implementation.md), [`src/modinv32_impl.h`](https://github.com/bitcoin-core/secp256k1/blob/04b91ce295dad7b827ae18eeeed062135a918dae/src/modinv32_impl.h). MIT-licensed primary implementation attribution; onino substitutes independently derived Curve25519 constants and follows its local CPU adaptation.

<a id="ref-inversion"></a>
**Constant-time inversion.** Daniel J. Bernstein, Han-Ting Chen, John R. Harrison, Cesare Huang, Gregory Maxwell, Bow-Yaw Wang, Pieter Wuille and Bo-Yin Yang. *Accelerating and verifying constant-time modular inversion*. EUROCRYPT 2026, pp. 598-627. [DOI: 10.1007/978-3-032-25336-1_21](https://doi.org/10.1007/978-3-032-25336-1_21). Algorithmic and verification context, not GPU performance evidence.

<a id="ref-bitcoinaddressfinder"></a>
**BitcoinAddressFinder.** Bernard Ladenthin and contributors, *OpenCL secp256k1 implementation*, [`src/main/resources/copyfromhashcat/inc_ecc_secp256k1.cl`](https://github.com/bernardladenthin/BitcoinAddressFinder/blob/main/src/main/resources/copyfromhashcat/inc_ecc_secp256k1.cl). Secondary reviewed port; historical review did not record a pinned revision. It is not the primary attribution for the retained inversion.

<a id="ref-gpuonion"></a>
**GPUonion.** ActiveTK, *GPUonion CUDA search and optimization notes*, [`src/main.cu`](https://github.com/ActiveTK/GPUonion/blob/main/src/main.cu) and [`Optimize.md`](https://github.com/ActiveTK/GPUonion/blob/main/Optimize.md). Direct implementation context for rejected E07. The original review did not record a commit, so the moving links do not establish an exact historical source snapshot.

<a id="ref-nath"></a>
**Prime-field arithmetic.** Kaushik Nath and Palash Sarkar. *Efficient arithmetic in (pseudo-)mersenne prime order fields*. Advances in Mathematics of Communications, 2022. [DOI: 10.3934/amc.2020113](https://doi.org/10.3934/amc.2020113). Related representation and carry-bound analysis.

<a id="ref-moma"></a>
**MoMA.** Naifeng Zhang and Franz Franchetti. *Code Generation for Cryptographic Kernels using Multi-word Modular Arithmetic on GPU*. IEEE/ACM International Symposium on Code Generation and Optimization (CGO), 2025. [Author manuscript](https://users.ece.cmu.edu/~franzf/papers/2025_CGO_MoMA.pdf). Related multiword code generation; no transferred NTT throughput claim.

<a id="ref-x25519"></a>
**GPU X25519/448.** Jiankuo Dong, Fangyu Zheng, Juanjuan Cheng, Jingqiang Lin, Wuqiong Pan and Ziyang Wang. *Towards High-performance X25519/448 Key Agreement in General Purpose GPUs*. IEEE Conference on Communications and Network Security (CNS), 2018. [Author manuscript](https://zhengfangyu.github.io/assets/papers/CNS2018-X25519.pdf). Related reduced-radix GPU arithmetic, distinct from the complete onion search.

<a id="ref-cryptopt"></a>
**CryptOpt.** Joel Kuepper Andres Erbsen, Jason Gross, Owen Conoly, Chuyue Sun, Samuel Tian, David Wu, Adam Chlipala, Chitchanok Chuengsatiansup, Daniel Genkin, Markus Wagner and Yuval Yarom. *CryptOpt: Verified Compilation with Random Program Search for Cryptographic Primitives*. 44th ACM SIGPLAN Conference on Programming Language Design and Implementation (PLDI), 2023. [Paper and artifact page](https://adam.chlipala.net/papers/CryptoptPLDI23/). Methodological context for bounded correctness-gated source search.

<a id="ref-tensorzkp"></a>
**TensorZKP.** Tao Lu, Jipeng Zhang, Yanpei Guo, Xuanming Liu, Wenjie Qu, Zonghui Wang, Wenzhi Chen and Jiaheng Zhang. *TensorZKP: Repurposing GPU Tensor Cores for High-Performance Zero-Knowledge Proofs*. 35th USENIX Security Symposium, 2026, pp. 5849-5867. [Proceedings entry](https://www.usenix.org/conference/usenixsecurity26/presentation/lu-tao). Related exact integer tensor packing; A-O M's performance was independently measured.

<a id="ref-simultaneous"></a>
**Simultaneous inversion.** Tamas Visegrady. *Low latency data-flow graphs for simultaneous modular inversion of many inputs*. arXiv, 2026. [arXiv:2607.11337](https://arxiv.org/abs/2607.11337). Related complement/dataflow organization, not evidence of a win for A-O K.

<a id="ref-zkprophet"></a>
**ZKProphet.** Tarunesh Verma, Yichao Yuan, Nishil Talati and Todd Austin. *ZKProphet: Understanding Performance of Zero-Knowledge Proofs on GPUs*. arXiv, 2025, version 1. [arXiv:2509.22684v1](https://arxiv.org/abs/2509.22684v1). Methodological context for integer instruction/dependency and occupancy analysis; workload differs from onino.

<a id="ref-roofline"></a>
**Roofline.** Samuel Williams Andrew Waterman and David Patterson. *Roofline: An Insightful Visual Performance Model for Multicore Architectures*. Communications of the ACM, 2009. [DOI: 10.1145/1498765.1498785](https://doi.org/10.1145/1498765.1498785). Analysis framework; no onino integer/traffic ceiling was measured.

<a id="ref-vulkan"></a>
**Vulkan execution and memory.** Khronos Group, *Vulkan Specification*, version 1.3.290: [versioned specification source](https://github.com/KhronosGroup/Vulkan-Docs/tree/v1.3.290), [synchronization chapter](https://github.com/KhronosGroup/Vulkan-Docs/blob/v1.3.290/chapters/synchronization.adoc) and [memory chapter](https://github.com/KhronosGroup/Vulkan-Docs/blob/v1.3.290/chapters/memory.adoc). Normative execution, dependency and mapped-memory coherence requirements; the current online specification redirects older 1.3 HTML URLs to newer versions.

<a id="ref-command-reset"></a>
**Command-buffer lifecycle.** Khronos Group, *vkResetCommandBuffer* and *vkResetCommandPool*, [buffer reset reference](https://registry.khronos.org/vulkan/specs/latest/man/html/vkResetCommandBuffer.html), [pool reset reference](https://registry.khronos.org/vulkan/specs/latest/man/html/vkResetCommandPool.html). Applicable Vulkan rules require non-pending buffers; these documents do not diagnose the recorded stalls.

<a id="ref-subgroups"></a>
**Subgroups.** Khronos Group, *Vulkan Guide: Subgroups*, [direct guide](https://docs.vulkan.org/guide/latest/subgroups.html). Capability, mapping and subgroup-size constraints for the rejected subgroup prototypes.

<a id="ref-executable-properties"></a>
**Executable properties.** Khronos Group, *VK_KHR_pipeline_executable_properties*, [extension reference](https://registry.khronos.org/vulkan/specs/latest/man/html/VK_KHR_pipeline_executable_properties.html). Source of the optional executable-statistics interface, not validation of implausible driver values.

<a id="ref-nsight"></a>
**Shader profiling.** NVIDIA, *Nsight Graphics 2025.4 User Guide: Shader Profiler*, [versioned documentation](https://docs.nvidia.com/nsight-graphics/2025.4/UserGuide/index.html#shader-profiler). Describes native profiling measurements unavailable in this study.

<a id="ref-slang-select"></a>
**Slang specialization Select.** shader-slang contributors, *SPIR-V specialization Select support*, [pull request #10361](https://github.com/shader-slang/slang/pull/10361). Reviewed fix present in tested 2026.19; compilation repair is separate from runtime arithmetic correctness.

<a id="ref-slang-rate"></a>
**Slang specialization rates.** shader-slang contributors, *Specialization-constant rate propagation*, [pull request #10694](https://github.com/shader-slang/slang/pull/10694). Reviewed fix present in tested 2026.19; direct compiler-issue attribution.

<a id="ref-cub"></a>
**CUB BlockScan.** NVIDIA CCCL contributors, *cub::BlockScan*, [implementation and embedded API documentation](https://github.com/NVIDIA/cccl/blob/main/cub/cub/block/block_scan.cuh). Related work-efficient/raking block scan organization; addition scan results do not establish field-product costs. No historical source revision was recorded.

<a id="ref-fallback"></a>
**Decoupled Fallback.** Decoupled Fallback project, *Decoupled Fallback: A Portable Single-Pass GPU Scan*, SPAA 2025 artifact, [source and artifact documentation](https://github.com/b0nes164/Decoupled-Fallback-Paper). Related subgroup-size/forward-progress portability context, not an imported cross-workgroup algorithm.

<a id="ref-cupqc"></a>
**cuPQC.** NVIDIA, *cuPQC 0.6 release documentation*, [versioned release](https://nvidia.github.io/cuPQC/releases/v0.6/). Library context for modular arithmetic/batching; PQC benchmarks are not transferred to this workload.

<a id="ref-memory"></a>
**GPU memory methodology.** NVIDIA, *Unlock GPU Performance: Global Memory Access in CUDA*, [technical article](https://developer.nvidia.com/blog/unlock-gpu-performance-global-memory-access-in-cuda/). Related coalescing/traffic methodology. The Vulkan layout experiments require their own measurements and do not establish cache behavior from allocation size alone.
