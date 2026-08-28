# Qwen3.8-Flash-Next TP=4: what actually moved throughput

> Internal research note (not in docs navigation). Companion memory:
> `qwen38-flash-next-tp4-cutin`. The planner that gates these boots is
> [tp4-serving-memory-planner](/research/tp4-serving-memory-planner).

Bring-up of Qwen3.8-Flash-Next NVFP4 (125B-A6B MoE) across the four-node
Spark fleet, 2026-08-28. This note records the **verdicts**, so the next
session spends boots on new questions instead of re-running settled ones.
Each boot costs 8-14 minutes.

## Why TP=4 at all

The model does not fit on one GB10. Measured device usage is 81.0 GiB for the
body plus 47.5 GiB for the per-layer embedding table, against 119.7 GiB
usable. Unified memory means "offload the embedding to host RAM" buys nothing,
because the host pool and the device pool are the same memory. Sharding the
embedding across four ranks is what makes it fit.

## Adopted configuration

Speculative decoding at `n=3`, async scheduling off, fusion off, autotune on,
`GPU_MEM=auto` (planner-computed). Korean 5-prompt median **~49 tok/s**.

The configuration is adopted because it is the simplest, not because it won:
see the resolution limit below.

## Verdicts

| Knob | Verdict | Evidence |
|---|---|---|
| Speculative decoding `n=3` | **Adopt** | 32.7 off vs ~49 on — far outside noise |
| Application clock cap 2000 MHz | **Free** | 50.4 at 2418 MHz vs 50.8 at 2000 MHz |
| fastsafetensors loader | **Reject** | No inference effect (50.1 vs 50.8); boot 834s vs 466s |
| torch.compile fusion | **No measurable effect** | inside the resolution limit |
| async scheduling plus the n-gram fix | **No measurable effect** | inside the resolution limit |
| autotune off | **Not established** | one boot 3 tok/s low; a second was never run |

Rejected before measurement, with reasons worth keeping: `VLLM_ALL2ALL_BACKEND`
is not a real environment variable (the backend is a CLI flag, and an early
"+77%" result attributed to it was invalid); PLE CPU offload is hard-gated to
single-node in the engine; the sharded-state saver does not accept the
headless flag the workers need.

## The resolution limit (read this before running a comparison)

The Korean 5-prompt median has a **boot-to-boot spread of about ±2 tok/s**.
Repeats inside one boot are tighter and will fool you: one engine gave
48.2 / 49.5 / 49.3 / 48.4 (spread 1.3) while the same configuration rebooted
gave 47.7 / 49.2 / 51.7 / 48.8 (spread 4.0). Within-boot repeats measure the
sampler; the unit that carries CUDA graph capture, autotuning results and
memory layout is the **boot**.

Consequence: a single boot per configuration cannot resolve anything smaller
than roughly 4 tok/s. Two of today's knob comparisons were run that way and
have to be reported as "no measurable effect" rather than as wins or losses.
Chasing a 2 tok/s effect honestly needs several boots per arm.

## Throughput is a function of the workload, not just the config

The same engine, same boot, same n=3 speculative config, measured across the
copy-to-generate spectrum (2026-08-29):

| Task | tok/s | tokens/step | draft acceptance |
|---|---|---|---|
| Reproduce a file with one edit | 75.9 | 3.60 | 87% |
| Fix a bug in a file | 67.3 | 3.20 | 73% |
| Add a function to a file | 56.3 | 2.69 | 56% |
| Free Korean prose | 44.0 | 2.14 | 38% |

Three conclusions follow:

- **The Korean 5-prompt median (~49) is the floor, not the number.** Agent
  workloads — tool JSON, file edits, wiki updates — sit in the 56-76 band.
- **The MTP head is fine.** 87% acceptance on copy-heavy work; the low average
  seen on prose is the workload, not a weak drafter. On copy-heavy tasks
  tokens/step reaches 3.60 against n=3's hard cap of 4.0 — the draft length,
  not the head, is what saturates. A community single-node run reached 97.4
  tok/s on the same file-reproduce task using an n-gram (prompt-lookup)
  drafter with longer drafts; raising our n lifts that end too but breaks the
  prose median (n=5 measured 33), and n is fixed per boot. n=3 is the balance.
- **Chasing a single headline number is a category error for this model.**
  Any configuration, including the community single-node stacks, lands at
  22-50 on pure generation and 2-4x that on n-gram-friendly output.

Speculative-decode acceptance decomposition (why ~50 and not 86 like the
production model): per-step forward time is actually faster than the
production model's (30.6 ms vs 36.8 ms no-spec), but tokens/step is 2.2 vs
3.16 - the entire gap is draft acceptance. The checkpoint's MTP head is one
layer and carries **no PLE tensors** (0 of 31 mtp.* tensors), so the draft
path runs without the model's signature n-gram table by design - the
suspected wiring bug there was investigated and refuted against the
checkpoint. Sampling costs only ~0.1 tokens/step (greedy A/B), so the
acceptance is intrinsic.

Also rejected on memory grounds: `--all2all-backend deepep_low_latency`
(DeepEP kernels are present in the image and CX-7 is its target NIC, but its
per-rank buffers left KV at -8.39 GiB; clearing that needs GPU_MEM ~0.71 on
the floor node, above the startup-check ceiling). Fusion failed the same way
at -5.96 GiB. Both are the planner's static-term blind spot, not engine bugs.

Upstream watch (NVIDIA forum, 2026-08-28): the PLE quantization-detection bug
our `DENEB_PLE_FORCE_FP8_EMBED` overlay works around was fixed upstream via a
dtype check instead of isinstance - retire that overlay on the next image
update. An INT4 PLE-offload path is "merging soon", which would shrink the
PLE's per-rank share without third-party checkpoints. Community cross-check:
an independent single-node run scored 87/100 on the same tool-eval harness
with the identical weak category (autonomous planning 67%), matching our
corrected ~85 and pinning the multi-step early-exit signature on the model,
not our wiring.

## Quality (2026-08-28/29)

Measured with the real gateway persona steering (scripts/eval/depth_probe.py,
five trap scenarios). Verdicts:

- **Depth is acceptable and the operator accepted it without a side-by-side.**
  The probe's traps were mostly passed: contradiction-crossing (D1), signal
  triangulation (D3), and commit-then-question prioritization (D5, the best
  answer of the set).
- **`reasoning_effort` must be pinned to `medium` in deployment.** The model
  rejects "high" (its tiers are xhigh-default/medium/low), and at its xhigh
  default it spent 22K reasoning characters on a five-line business question
  and returned an empty answer at a 6K budget. At medium: 1.3-3.9K reasoning,
  every scenario finishes, same trap-passing quality, ~30s per question
  instead of ~5min.
- tool-eval hardmode 84 scored 80 with two wiring artifacts (a 52-tool
  scenario class overflowing an 8K bench context, -9 points; the unsupported
  "high" effort). Corrected estimate ~85 vs the production model's 91; the
  residual gap is a real multi-step early-exit pattern (looks up, plans, then
  skips the final send/create step), cross-validated by the community run
  above.

## Background load is a first-class experimental variable

Idle gradle and kotlin build daemons left by a Kotlin gate run cost **32%** of
decode throughput: 38.5 vs 49.4 tok/s, same configuration, difference entirely
in what else was resident. This is the largest single effect measured all day,
larger than every serving knob combined, and it is invisible in the engine
logs.

Two consequences, both now enforced rather than remembered:

- The bench harness runs a reclaim pass before every boot, so a run cannot
  silently measure the background.
- Any bench that shares the fleet with build work is suspect until it does.

An earlier round of measurements was taken while UI work ran on the same
fleet, and every number from it had to be discarded — including a "regression"
that turned out to be daemons.

## Engine patches carried as overlays

Four overlay files, opt-in by environment variable, none of them upstream:

- **n-gram deferred correction.** Upstream reads the CPU token mirror while it
  still holds optimistic placeholders under async spec decode, so every decode
  step sees wrong context. The overlay runs the deferred correction before the
  read, mirroring the pattern the mamba path already uses.
- **PLE hash-chain width bound.** The hash chain was built to the static
  batched-token width rather than the live token count, costing roughly three
  orders of magnitude of wasted work per decode step.
- **QSA split cap.** The kernel's split heuristic assumes a ~180-SM part; GB10
  has 48.
- **fastsafetensors single-node path.** Three patches (skip cross-node NCCL
  redistribution, force the no-GDS path, stage shards through host memory).
  Kept only as a knob — the loader is rejected above.
