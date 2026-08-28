# tp4-mem: TP=4 serving memory planner (Spark fleet)

> Internal research note (not in docs navigation). Fleet-specific by nature:
> the tool lives on the serving head node as `~/tp4-mem.sh`, next to the
> TP=4 launchers it serves. Companion memory: `tp4-mem-planner`.

## Problem

Multi-node TP serving on the GB10 fleet keeps failing at one spot: the
`gpu-memory-utilization` flag is a fraction of **total** device memory, but
what actually bounds a boot is the **floor node's live free memory**, which
moves with page cache, dead experiment containers, and sidecars. During the
Qwen3.8-Flash-Next TP=4 bring-up (2026-08-28) six boots died to this window:
0.62 starved the KV allocator ("No available memory for the cache blocks"),
0.70 failed the startup check ("Free memory on device ... is less than
desired"), and the workable 0.66 was found by burning ~12-minute boots.

Two platform facts make hand-picking unreliable:

- **Unified memory gap.** On GB10 the CUDA startup check sees less than
  `/proc/meminfo MemAvailable`; measured gap 4-12 GiB depending on node state.
- **A warm page cache starves the CUDA allocator mid-load** (independently
  reported for GB10 on vLLM PR #53896), so cache state changes the answer.

## Tool

`tp4-mem.sh` on the serving head node. Subcommands:

| command | does |
|---|---|
| `plan` | probe all 4 nodes in parallel, print per-node usable table, identify the floor node, compute `budget = floor - margin`, `KV = budget - static`, and either print the recommended `GPU_MEM` (plus the equivalent `--kv-cache-memory` bytes) or refuse with "free N GiB on node X" |
| `gmu` | print just the number — consumed by the launcher's `GPU_MEM=auto` (now the default), turning a 12-minute failed boot into a 2-second refusal |
| `reclaim` | remove **dead** experiment containers (`q38*`/`q38save*` only), reap orphan harness processes, drop caches on all nodes, print before/after; `--stop-gradle` also reaps idle gradle/kotlin build daemons |
| `doctor` | fleet health sweep (fabric link speed, clocks, container inventory); `--collect` writes a bundle |
| `clock` | `status` / `set MHZ` — application clock cap across all four nodes |
| `restore-prod` | put production back after an experiment: clear experiment containers, start the production model and OCR sidecar, then **prove it with a real chat call** |
| `preflight` · `record` · `history` | pre-boot check, boot-ledger append, past-boot table |
| `help` | usage text (also saved as a README next to the tool) |

Defaults: `--static 46` (measured per-rank: NVFP4 weights 31.5 + FP8 PLE
11.9 + runtime overhead), `--kv-min 6`, `--margin 2`. Fast probe mode uses
MemAvailable minus a conservative 12 GiB reserve; `--accurate` uses torch
`mem_get_info` but **refuses while any engine container runs**, because adding
a CUDA context can kill a boot holding a ~2 GiB margin.

## Safety design (each rule maps to a same-day incident)

- **Allowlist-only.** Only `q38*`/`q38save*` containers and our own harness
  scripts are reachable. Production (dsv4/hy4, the embedding sidecar, ERP,
  mail) is untouchable by construction — an earlier cleanup mistakenly stopped
  the production embedding server, so protection must not depend on a deny
  list being complete.
- **Running containers are skipped** unless `--include-running`; a booting
  engine is not a zombie.
- **A live engine also protects harness processes** — the sweep script driving
  a boot looks orphaned but is not; killing it orphans a wanted run.
- **SIGSTOPped downloads are reported, never killed** — a paused download is a
  paused job.

## Adjacent lessons recorded with the tool

- Experiment logs must live on a host mount, not container `/tmp` — a dead
  container otherwise destroys the evidence of why it died.
- Watchdogs must treat "container gone" as a wake condition; `docker exec`
  probes fail silently against a dead container and the watch never fires.
- Never combine `pkill -f <script>` and a relaunch of the same script in one
  remote shell: the shell's own cmdline contains the script path and pkill
  kills it (the bracket trick covers matching, not colocation).
- **A cleanup tool that kills harness scripts must exempt its own ancestry.**
  Wiring `reclaim` into the bench harness so every boot started from a clean
  background made each boot kill itself on its first line — the tool's kill
  list names the harness, and the harness was now the caller. The bracket
  trick does not help: the victim is a genuine ancestor, not a self-match.
  The fix walks `/proc/<pid>/status` up the parent chain and refuses to signal
  any PID in it, which covers every future caller rather than that one script.

## Static reserve is configuration-dependent

The `--static 46` default was measured on the plain serving configuration.
Turning on torch.compile fusion passes pushes the per-rank static footprint
past it: the planner cleared a boot at `GPU_MEM=0.66` and the engine then
computed **`Available KV cache memory: -5.14 GiB`** and aborted. The planner
cannot see this, because fusion allocates during compilation, after the
planning probe. Until the reserve is made configuration-aware, raise
`--static` by hand when enabling fusion, or treat a negative-KV abort as the
signal that the static term, not the budget, was wrong.
