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
