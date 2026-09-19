# Translation latency

## Unicode batching follow-up (2026-09-20)

Baseline: `05f2716649d5b1a94585c2e58269668e9a9f2aea`, including both cache
optimizations below. The only production change in this follow-up is counting
Unicode code points, instead of UTF-8 bytes, in `translateInputCost` for source
text and its discounted context. The 3,000-character budget, 50-text limit,
six-worker limit, provider parameters, and cache behavior are unchanged.

The browser budgets JavaScript string length. Charging UTF-8 bytes on the server
inflated Cyrillic/CJK text and unnecessarily added provider scheduling waves.
The browser counts supplementary characters as two UTF-16 units; the server now
counts one code point, so these are still not identical units. The ordinary
3,000-code-point batches remain comfortably below DeepL's separate
[128 KiB request-body limit](https://developers.deepl.com/api-reference/translate/request-translation),
including URL encoding and the capped context. A single oversized input retains
the existing handling; this change does not introduce a new hard byte limit.

### Prediction and live result

Before editing, the prediction was 10 requests / two waves becoming 6 requests /
one wave for a fixed Russian fixture, with at least 20% lower median cold RPC
latency if provider latency remained approximately flat. This prediction held.

Forty synthetic Russian paragraphs, 15,160 source characters / 27,800 UTF-8
bytes, target Korean, no explicit context hints. The test exercised the actual
`miniapp.web.translate` RPC and real DeepL on the Linux arm64 gateway host.
Each trial started an isolated development gateway with a fresh synthetic
50,000-entry disk cache; production state was not modified. A = baseline,
B = candidate; order was A B B A B A A B.

| Run | Variant | Cold RPC | Warm RPC | Provider requests |
| --- | --- | ---: | ---: | ---: |
| 1 | A | 2,666.85 ms | 2.68 ms | 10 |
| 2 | B | 2,121.02 ms | 1.80 ms | 6 |
| 3 | B | 2,011.68 ms | 2.15 ms | 6 |
| 4 | A | 2,602.22 ms | 1.38 ms | 10 |
| 5 | B | 2,200.71 ms | 2.34 ms | 6 |
| 6 | A | 2,819.24 ms | 3.22 ms | 10 |
| 7 | A | 2,493.49 ms | 2.88 ms | 10 |
| 8 | B | 1,952.44 ms | 1.69 ms | 6 |

The cold median fell from **2,634.54 ms to 2,066.35 ms (21.6%)**. Each variant
still sent exactly 15,160 billable source characters. This is an exploratory
four-sample-per-variant RPC comparison, not a device rendering measurement or
a guarantee for every page. ASCII batching is unchanged. Different batch
boundaries can also change the combined context hint for contextual envelopes;
these live samples exercise plain paragraphs, not every such context case.

All 320 live segments contained Korean and retained their numbered section
positions. Every warm result exactly matched its own cold result, and every
cache retained 50,000 entries. The eight response hashes differed, including
between unchanged-binary runs. Manual checks of each run's first and last
paragraph found wording differences such as equivalent terms for "guide";
an eight-keyword coverage check passed for every paragraph. These are limited
quality checks, not a general semantic equivalence claim.

### Regression coverage and artifacts

`TestTranslateSegmentsUnicodeBudgetPreservesResults` exercises the production
entrypoint with an exact stubbed provider response and 40 unique 400-character
segments. Before the change, provider counts were Latin 6, Cyrillic 14, CJK 20,
and supplementary-plane text 40. Afterward all four use 6 calls, preserving
every output by index. The test checks actual encoded body size and flattened
text counts. Boundary cases cover Unicode source/context costs.

```bash
go test -run 'TestTranslateSegmentsUnicodeBudgetPreservesResults|TestBoundaryTranslateInputCostContextDiscount' \
  -count=1 ./internal/pipeline/chat/tools/translateops
```

Live fixture SHA-256:
`10c60d0e23ab0548550882635dae6cd6afb0dcf9a34df8e2a52839b270d64d05`.
Linux binary SHA-256 values:
`b71c75d2e28ee23adbd3817464f1ee610a032e5cf20da4fe51c0e9f36286db6f` (A),
`ae42cb150ff00766037a8c0aaa62f54170e8f57d7d8e1ea458a1685235af3828` (B).
The harness, fixture, raw results and synthetic translations are retained in
`~/deneb-dev/codex-deepl-unicode-20260920/` on the test host, with a local copy in
`/tmp/deneb-deepl-unicode-evidence/`. Test gateways were stopped and their
temporary state removed after every run.

Validation: the targeted package tests, race tests for `translateops`,
`opstranslate` and `rpc/handler/chat/miniapp`, and
`TMPDIR=/private/tmp make check GO_PAR=2` passed. The full check includes
generation, formatting, vet, lint and all Go tests; its optional LLM-response
quality gate was skipped by default. Live translation quality was checked via
the actual RPC above. `DENEB_INSTANCE=deepl-unicode scripts/dev/live-test.sh
restart` and `smoke` also passed both health/readiness checks, and that local
development gateway was stopped afterward. Development logs included optional local-model probe
and embedding warmup warnings; no translation/deepl warning category occurred.

The patch-note fragment also triggered native gates: spotless, detekt, desktop
smoke tests, Android compilation and design lint passed on macOS. The golden
image gate differed for 74 images on both the unchanged baseline and candidate;
all 75 deterministic baseline/candidate renders were byte-identical. The same
candidate native sources and patch note then passed the golden image gate on
Linux (75 PNGs match), without changing or regenerating committed goldens.

## Historical cache optimization

Measured 2026-09-19 against baseline `35554e253` and integrated on `f936a2855`.

The production fix already landed in [PR #5130](https://github.com/choiceoh/Deneb/pull/5130),
including sorting and moving serialization/file writes outside the cache mutex.
PR #5131 added stricter eviction checks and reproducible benchmarks. Its
production code was identical to that upstream commit. The historical results
below isolate a sort-only candidate; they are not measurements of #5130's full
implementation or an additional speedup supplied by #5131.

## Cause and change

The running gateway's durable translation cache contained 50,000 entries
(11,174,299 bytes, 4,339 distinct insertion timestamps). At this limit, saving
fresh translations invokes `trimLocked` while holding the shared cache mutex.
The former insertion sort reordered the entire randomly iterated map in O(n²)
time. Concurrent DeepL batches therefore waited for repeated cache trims even
after their provider responses were ready.

The historical candidate's `slices.SortFunc` reduced that sort to O(n log n).
The capacity, oldest-first eviction, persisted format, synchronous durability, request batching, provider
parameters, and translation text were unchanged. Entries with identical timestamps
had no specified eviction order in that candidate; #5130 additionally orders
equal-age entries by ID and coalesces writes outside the cache mutex.

The pre-change prediction was at least an 80% reduction in local processing time
with a full cache and an immediate provider response.

## Historical controlled local comparison

Apple M5, macOS arm64, Go 1.26.2; five one-iteration samples per variant. Both
variants used the same benchmark, synthetic 50,000-entry cache, and temporary
file storage. Fixture construction was excluded from timing. No live provider
was called in these benchmarks.

| Measurement | Before median | After median | Reduction |
| --- | ---: | ---: | ---: |
| Trim 50,050 entries to 50,000 | 603.558 ms | 3.140 ms | 99.5% |
| `TranslateSegments`, 40 uncached 433-character segments | 3,750.780 ms | 166.722 ms | 95.6% |

The second benchmark exercises the production entrypoint, seven batches with up
to six concurrent calls, cache updates, real JSON serialization, and file writes.
Only DeepL HTTP is stubbed. Every output is checked against its input index. It measures local
overhead, not phone-to-gateway or DeepL network latency.

The full-path samples were 1,959.927 / 3,295.762 / 3,893.979 / 4,587.728 /
3,750.780 ms before, and 167.098 / 159.201 / 164.750 / 166.980 / 166.722 ms after.
Concurrent batch completion can change how many flushes encounter an overflow;
the prediction held despite that variation.

Reproduce from `gateway-go` (copy the benchmark file unchanged into a baseline
checkout for a before/after comparison):

```bash
go test -run '^$' \
  -bench 'BenchmarkTranslate(DiskCacheTrim|SegmentsFullDiskCache)$' \
  -benchtime=1x -count=5 -benchmem ./internal/pipeline/chat/tools/translateops
```

## Historical live DeepL comparison

Linux arm64 gateway host, isolated loopback development instances, separate
synthetic state directories, and real `miniapp.web.translate` RPCs. Production
services and cache were not modified. Each cold request used the same 40 English
paragraphs (14,440 characters), an initially full synthetic cache, five DeepL
requests, and the same target language. Each subsequent warm request reused
the just-returned translations. Order: A B B A B A A B (A = baseline).

| Run | Variant | Cold RPC | Warm RPC |
| --- | --- | ---: | ---: |
| 1 | Before | 6,568.43 ms | 1.16 ms |
| 2 | After | 9,760.50 ms | 1.35 ms |
| 3 | After | 2,437.45 ms | 1.49 ms |
| 4 | Before | 8,034.87 ms | 1.86 ms |
| 5 | After | 2,174.49 ms | 1.32 ms |
| 6 | Before | 7,263.39 ms | 1.40 ms |
| 7 | Before | 5,340.52 ms | 1.21 ms |
| 8 | After | 3,094.60 ms | 2.03 ms |

The four-sample median decreased from 6,915.91 to 2,766.03 ms (60.0%). This is
an exploratory RPC comparison, not an Android page-render benchmark or a tail
latency guarantee: the 9.76-second candidate sample remains in the results.
DeepL's timing and wording varied even within the same variant. All eight
responses contained 40 Korean segments; every warm result exactly matched its
own cold response, and every persisted cache retained exactly 50,000 entries.
The initial harness's cross-run output-hash assertion failed because live
DeepL responses varied on both unchanged and changed binaries; it is not used
as a deterministic quality gate. Fixed provider-response preservation is
instead checked by the benchmark and existing translation tests.

The fixture SHA-256 was
`2bebe84d11f4282ff435ca730999d26f44ee5432a9d6b38c9d6ee5cb93308b24`.
Binary SHA-256 values were
`069e94d81579d70eb5188947d73f33afdd2a4bc3d8a5368c95ed645d8faf3bab` (before) and
`d193110957c5567fda5c67f97c886a6acd895cef7a07025d83245dc5aa1e810f` (after).

Both development variants passed health/readiness checks. Development logs
contained warnings about the unavailable optional LLM endpoint and canceled
embedding warmup during shutdown. Neither subsystem is used by the DeepL
translation RPC.

## Integrated upstream measurement

The same benchmarks were rerun on `f936a2855` plus #5131's tests, using
the same Apple M5 environment and five one-iteration samples. Production files
were verified byte-for-byte identical to #5130.

| Measurement | Integrated median |
| --- | ---: |
| Trim 50,050 entries to 50,000 | 5.673 ms |
| `TranslateSegments`, 40 uncached 433-character segments | 84.984 ms |

The full-path samples were 72.506 / 90.668 / 101.779 / 84.984 / 83.660 ms.
This preserves the fix's low local overhead with the upstream write coalescing
and deterministic tie ordering. These are stubbed-provider measurements; the
historical live API samples above were not rerun for this tests-only follow-up.

## Verification

- `TMPDIR=/private/tmp make check GO_PAR=2`: passed generation checks, formatting,
  vet, lint, all Go tests, runtime-health tests, and Health Bench scorer tests.
  The opt-in LLM Korean-response quality gate was skipped by its default setting;
  live translation behavior was exercised directly above.
- `go test -race -count=1 ./internal/pipeline/chat/tools/translateops
  ./internal/runtime/opstranslate ./internal/runtime/rpc/handler/chat/miniapp`:
  passed all three packages (run from `gateway-go`).
- `DENEB_INSTANCE=deepl-speed-after scripts/dev/live-test.sh restart` followed
  by `smoke`: health and readiness passed; the development instance was stopped.
- The cache eviction regression verifies the exact retained count, all 50 oldest
  evictions, and every retained translation and timestamp, including tied ages.

The first plain `make check` run exposed the existing
`TestEnsureWorktreeIsIdempotent` macOS path-alias failure: Git reports canonical
`/private/var/...` paths while the default temporary directory uses `/var/...`.
The unchanged test failed independently with the default temp directory and
passed with canonical `TMPDIR=/private/tmp`; the full gate then passed with that
environment. No test exclusions or unrelated code changes were made.

After integration, the four-package parallel run hit the unchanged liteparse
test `TestBoundaryParsePartialOutputWinsOnCancellation` at its one-second
fake-process readiness deadline. Three isolated repetitions passed, followed by
the full gate at the repository's default two-package parallelism.
