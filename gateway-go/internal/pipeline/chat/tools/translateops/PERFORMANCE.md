# Translation cache latency

Measured 2026-09-19 against baseline `35554e253`.

## Cause and change

The running gateway's durable translation cache contained 50,000 entries
(11,174,299 bytes, 4,339 distinct insertion timestamps). At this limit, saving
fresh translations invokes `trimLocked` while holding the shared cache mutex.
The former insertion sort reordered the entire randomly iterated map in O(n²)
time. Concurrent DeepL batches therefore waited for repeated cache trims even
after their provider responses were ready.

`slices.SortFunc` reduces that sort to O(n log n). The capacity, oldest-first
eviction, persisted format, synchronous durability, request batching, provider
parameters, and translation text are unchanged. Entries with identical timestamps
still have no specified eviction order.

The pre-change prediction was at least an 80% reduction in local processing time
with a full cache and an immediate provider response.

## Controlled local comparison

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

## Live DeepL comparison

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

## Verification

- `TMPDIR=/private/tmp make check GO_PAR=4`: passed generation checks, formatting,
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
