---
title: "Deneb-Briefcase"
summary: "Implemented v1 casepack, isolated replay, device twin, deterministic grader, and three-role closed-loop contracts"
read_when:
  - Building or validating a Deneb-Briefcase casepack
  - Working on the isolated Briefcase runtime or deterministic grader
  - Checking which Briefcase capabilities are implemented versus gated for later
---

# Deneb-Briefcase

Deneb-Briefcase is Deneb's local, deterministic harness and grading substrate
for time-ordered knowledge work. The current implementation authenticates immutable
case inputs, creates a disposable Deneb filesystem, advances an explicit clock,
replays scripted events and device outcomes, and grades deterministic evidence.
It adapts [UniClawBench](https://arxiv.org/abs/2607.08768)'s task-package and
hidden-evidence isolation ideas to Deneb's own records, memory, and native-device
boundary; it is not a copy of UniClawBench's 400-task suite.

It is not yet a complete benchmark product. P2 adds a supported three-role
closed loop to the standalone `deneb-briefcase` CLI, but there is no
source-aware LLM judge, model-backed user simulator, dashboard, Vault operator
workflow, or public case publisher. The harness is deterministic around the
model; a remote inference provider is not guaranteed to be bit-for-bit
deterministic even when OpenAI-compatible seed forwarding is available.

## Implementation status

### P0: immutable case and deterministic grade

Implemented:

- `internal/domain/briefcase.LoadDir` loads a directory casepack whose root is a
  real directory, whose manifest is at most 4 MiB, and whose tree contains no
  symlink, special file, undeclared file, missing declared file, or hash mismatch.
  Individual assets are capped at 64 MiB and the pack at 512 MiB.
- `SetCanonicalDigest` and `CanonicalDigest` bind the typed manifest. The digest
  is SHA-256 over compact JSON with `manifestDigest` blank.
- `Pack.ReadFile` rechecks the declared digest on every read, detecting an asset
  changed after initial validation.
- `internal/eval/briefcase.Grade` supports `PASS`, `FAIL`, and `INVALID`; it does
  not turn malformed or unsupported checks into a pass.
- Deterministic checks are `exact_text`, `contains`, `contains_token`,
  `forbidden`, `artifact`, and `state_json_equal`.

Schemas:

- [manifest.schema.json](../../bench/briefcase/schema/manifest.schema.json)
- [grader-plan.schema.json](../../bench/briefcase/schema/grader-plan.schema.json)
- [device-plan.schema.json](../../bench/briefcase/schema/device-plan.schema.json)
- [supervisor-plan.schema.json](../../bench/briefcase/schema/supervisor-plan.schema.json)
- [user-simulator-plan.schema.json](../../bench/briefcase/schema/user-simulator-plan.schema.json)

### P1: isolated replay primitives

Implemented:

- `NewRunRoot` creates private `0700` home, state, files, workspace, artifact,
  log, and temp roots. Its generated Deneb config disables cron, Gmail polling,
  LMTP, and session auto-resume.
- `IsolatedEnviron` retains only locale, timezone, and terminal display settings;
  ambient credentials and proxy variables are not inherited. It sets
  `DENEB_PROFILE=briefcase` and enables secret redaction.
- `FrozenClock`, `ManualClock`, and `Timeline` provide deterministic time and
  event order. Events sort by time, explicit order, then ID. A handler failure
  does not consume the event.
- `DeviceTwin` accepts only predeclared action ID, kind, and canonical JSON
  payload combinations. Outcomes are `confirmed`, `failed`, `unconfirmed`, or
  `delayed`; duplicate action IDs cannot execute twice.
- `Policy` allows absolute, clean, symlink-safe reads and writes only below the
  disposable run root, allows only explicitly configured device kinds, and
  always denies network and process execution.
- The `briefcase` chat tool preset exposes only the case-local surface listed
  below. Network, arbitrary shell execution, external delivery, scheduling, and
  agent fan-out are absent.
- `World` loads snapshot sources, releases timeline sources only when due, and
  never loads sealed evidence into the agent-visible view. For paired runs it
  withholds sources explicitly marked `memory: true` from the `raw-primary` arm.
- `NewFixtureRegistry` exposes case-local record tools, production-shaped file
  tools with output-only mutation, and a pure-Go grep implementation.
- `NewChatHarness` binds the signed run/tool budgets, frozen clock, explicit
  workspace, fixture registry, transcript, and actual `chat.Handler`, then
  executes all episodes in one continuing `bench:` session.
- Briefcase mode disables slash dispatch, host context files and ancestor
  `AGENTS.md`/`SOUL.md`/`USER.md`/`MEMORY.md`, topic/calendar/goal/persona
  providers, skills, routing, link enrichment, auto-title, post-turn diary and
  coding hooks, global recall caches, learned token calibration, retry/fallback
  ladders, budget grace, and output-recovery calls.
- The harness transcript is a RunRoot-local production `polaris.Bridge`, not a
  file-only approximation. Follow-up turns therefore receive prior user and
  assistant messages as ordinary role history in both benchmark arms.
- Transcript context-load or append failures are fatal in Briefcase mode; a run
  cannot be scored after silently losing a user, assistant, or tool-result turn.
- Agent-visible `wiki`, `knowledge`, `polaris`, and related record tools are
  bounded, read-only case-local adapters. The production `wiki.Store` is used
  only behind automatic recall in the assisted arm. Visible `memory: true`
  records are projected into that RunRoot-local index after their signed access
  time; no production Wiki status, index, or mutation action is exposed.

The harness records only tool-result hashes, not raw tool results. A separate
`score` invocation opens the sealed grader plan, preserving the agent/grader
boundary.

The current harness is in-process, so `IsolatedEnviron` is an available
subprocess contract rather than a process-global mutation performed by `run`.
The scored tool surface has no process launcher and its live-store dependencies
are nil or RunRoot-local. A future container/child-process runner must apply the
returned environment explicitly before claiming OS-level environment isolation.

### P2: three-role closed loop

Implemented:

- The executor is the same isolated `ChatHarness`. Its initial timeline runs
  once; follow-ups reuse the same handler, session key, Polaris transcript,
  memory store, tool budget, workspace, and Device Twin. Polaris migration and
  dual-write failures are fatal in Briefcase even though production mirrors
  remain best-effort.
- A supervisor plan is a sealed signed source with its own canonical
  `planDigest`. It evaluates one deterministic checkpoint per cycle and returns
  `PASS`, `CONTINUE`, or `FAIL` while retaining grader-private diagnostics.
- The signed manifest authorizes `maxFollowUps` from 0 through 2 and an optional
  `perTurnTimeoutSeconds`. A supervisor plan must contain exactly
  `maxFollowUps + 1` checkpoints.
- The user simulator is a separate sealed scripted plan. It receives only a
  construction-locked handoff containing a coarse verdict, recoverability,
  score band, and public trajectory/artifact summaries. It never receives the
  supervisor plan, check results, exact threshold, or hidden rationale.
- A fail-closed feedback firewall checks simulator output before it reaches the
  executor. It rejects sealed IDs and paths, hidden references, rubric and
  checkpoint IDs, expected answers, supervisor reasoning markers, invalid
  UTF-8, control characters, and oversized text without echoing the rejected
  secret into errors. Scannable sealed inputs are capped at 16 MiB total. This
  is a lexical and numeric DLP boundary, not a semantic noninterference proof.
- One signed deadline covers executor replay, continuations, supervisor checks,
  and the scripted simulator after construction. Case loading, hash validation,
  and runner construction happen before that deadline starts. Each handler call
  also uses the signed per-turn timeout.
- Every completed cycle snapshots declared artifacts before supervisor work.
  `BestRun` points to the isolated snapshot for the highest valid score (earliest
  cycle on ties), while `Run` is the latest completed executor state. A later
  failed continuation cannot overwrite the preserved best artifact bytes.
- The loop result includes trusted `supervisorAudit` diagnostics for evaluator
  inspection. The runner never feeds that field to the simulator or executor;
  result files must remain grader-private.

The P2 simulator is deterministic and scripted so regression tests remain
offline and reproducible. A future model-backed simulator may implement the
same `UserSimulator` interface, but it must receive the same coarse handoff and
pass the same feedback firewall. The deterministic supervisor checks exact
text, state, and artifact bytes; it is not yet a source-aware qualitative LLM
judge.

Cancellation is cooperative at model streaming, tool, hashing, copy, and
grading boundaries. A kernel call already in progress may return only when the
operating system returns control; the harness does not claim hard process-level
preemption.

## Casepack layout

```text
case-root/
  manifest.json
  snapshot/       # visible at cutoff
  timeline/       # episode input and sources released later
  sealed/         # grader-only assets; never released by an episode
```

Artifacts are declarations under logical `output/` paths in the manifest; they
are not immutable input files and must not appear in the casepack tree.

The manifest has these required top-level fields:

| Field | Implemented contract |
|---|---|
| `schemaVersion` | Exactly `deneb.briefcase/v1` |
| `manifestDigest` | Lowercase 64-character SHA-256 of the typed manifest with this field blank |
| `caseId`, `familyId` | `[A-Za-z0-9][A-Za-z0-9._:-]{0,127}` |
| `split` | `dev`, `calibration`, or `holdout` |
| `privacyMode` | `portable` or `vault`; operational Vault use is not enabled yet |
| `seed` | Positive signed 64-bit integer; zero means unset |
| `cutoffAt`, `frozenNow` | RFC 3339 times; `frozenNow` cannot precede `cutoffAt` |
| `timezone` | Nonblank IANA timezone accepted by Go's timezone database |
| `locale` | Exactly `ko-KR` in v1 |
| `sources` | At most 512 immutable snapshot, timeline, or sealed assets with provenance and SHA-256 |
| `episodes` | 1 through 500 chronological `user-turn`, `event`, or `heartbeat` records |
| `artifacts` | At most 128 unique logical paths below `output/`; each is capped at 64 MiB |
| `runPolicy` | Case-wide `maxTurns` 1..500 and output-token budget 1..250,000; execution timeout 1..86,400 seconds; `maxFollowUps` 0..2; optional per-turn timeout no larger than the execution timeout |
| `toolPolicy` | Default is `deny`; at most 32 rules; v1 decisions are only `deny` or `allow`; total attempt budget is at most 10,000 |
| `networkPolicy` | Exactly `deny` in v1; `allowedHosts` must be absent or empty |

### Source visibility

- `snapshot`: its path is below `snapshot/`; `availableAt` must not be later
  than the case cutoff.
- `timeline`: its path is below `timeline/`; exactly one episode must release it,
  not earlier than `availableAt`.
- `sealed`: its path is below `sealed/`; no episode may release it.

`eventAt` is when the represented fact or event occurred. `availableAt` is when
Deneb could first know it. `capturedAt` is provenance and cannot make information
visible early. `supersedes` must name existing source IDs, cannot point to self,
and must form an acyclic graph.

Executable sealed roles are explicit `sourceRef` values and must be unique when
used: `briefcase:grader-plan`, `briefcase:supervisor-plan`,
`briefcase:user-simulator-plan`, and `briefcase:device-plan`. Gold/firewall-only
content uses `briefcase:gold` or `briefcase:gold:<label>`. Arbitrary sealed files
are not silently treated as grader plans, and an unrecognized scannable role
causes closed-loop construction to fail.

### Two-arm contract

The same signed casepack supports a paired comparison:

| Arm | Source visibility | Recall behavior |
|---|---|---|
| `raw-primary` | Withholds every source whose manifest entry has `memory: true`; ordinary snapshot and due timeline sources remain available | Long-term recall preflight is forced off |
| `memory-assisted` | Includes `memory: true` sources under their normal snapshot/timeline timing | Run-local Deneb Wiki recall is attached unless the caller separately requests `SkipRecall` |

`memory` is an explicit provenance role, not an inference from `kind`. A wiki,
diary, notebook, transcript, or workfeed record is marked only when it represents
derived durable memory being tested. Primary mail, calendar, file, capture, and
device evidence normally remains unmarked. Sealed evidence stays unavailable in
both arms regardless of the flag.

For a valid causal comparison, run both arms with the same manifest digest,
model, seed, prompt/tool versions, budgets, and episode schedule, but with fresh
RunRoots. The run result records its `arm`. The measured delta is
`memory-assisted - raw-primary`; it must not also change the primary evidence or
model configuration.

`NewChatHarness` defaults an omitted arm to `memory-assisted`. Set
`ChatHarnessConfig.Arm` explicitly to `ArmRawPrimary` or `ArmMemoryAssisted` in a
paired runner. The standalone `run` command exposes the same contract through
`--arm memory-assisted` and `--arm raw-primary`; its default is
`memory-assisted`. `raw-primary` automatically disables recall and withholds
`memory: true` sources. `--skip-recall` by itself only changes recall preflight
and must not be reported as a raw-primary result.

### Run provenance

`deneb-briefcase-run/v1` records the casepack, requested model,
provider-reported model, API mode, seed-forwarding status, arm, recall mode,
tool schema, normalized endpoint, running build, execution profile, fixed
sampling values, and ordered finalized system-prompt hashes. A provider model
must be nonblank and stable within and across every turn. Unknown or missing
finish reasons are incomplete, not silently mapped to `end_turn`.
Score-visible episode text uses the selected raw model output before interactive
surface substitutions, so process-global market-token caches cannot alter a run.

`buildSha256` is the SHA-256 of the running executable when that file is
readable. The in-process test fallback hashes Go/module/VCS build metadata if an
executable file cannot be read. `executionProfileSha256` also binds fixed
context budgets, one-attempt behavior, no grace/recovery, cumulative case-wide
turn/output budgets, explicit stop reasons, stream byte caps, a 180-second
stream-idle watchdog, sequential tool execution, and disabled endpoint-specific
prompt-cache metadata. Dirty source is therefore distinguished by the actual
test binary rather than only a `vcs.modified` boolean.

### Validation beyond JSON Schema

JSON Schema catches object shape, unknown fields, enums, local path form, and
numeric bounds. `LoadDir` remains authoritative for invariants that JSON Schema
cannot express completely:

- canonical manifest digest and every asset's actual digest
- valid IANA timezone and temporal ordering
- unique IDs and output paths by a named property
- source/artifact cross-references
- complete, acyclic supersession graph
- exactly-once release of every timeline source
- rule `maxCalls` not exceeding global `maxCalls`
- absence of unreferenced files, symlinks, and special files
- asset immutability between load and read

Do not treat schema validation alone as case authentication.

## Execution surface

`DENEB_PROFILE=briefcase` selects the current fixed preset:

```text
mail_archive  contacts  files  calendar  todo
phone_read    phone_write
wiki          knowledge polaris notebook
read          grep      write edit
```

Only tools named by signed `allow` rules are advertised, and every advertised
tool is eager. There is no `fetch_tools` path in Briefcase. Record adapters
return at most four records per page, 8 KiB per ID lookup, 8 KiB aggregate
content, and 20 KiB of JSON; `recordOffset`, `offsetBytes`, `limitBytes`, and
`nextOffset` provide deterministic paging. Binary or escape-heavy content uses
base64 when that is the smaller safe representation.

`write` and `edit` are present because a benchmark task may create artifacts;
the fixture registry restricts them to exact manifest-declared paths below
`workspace/output` and their signed byte limits. Regex, batch, replace-all, and
whitespace-tolerant edit fallback are disabled. `grep` is implemented in-process
and does not launch ripgrep. `exec`,
`web`, Gmail send, cron mutation, external messaging, and subagent spawning are
not in the preset.

Mail, calendar, files, contacts, todo, wiki, knowledge, notebook, Polaris, and
phone reads are deterministic case-local adapters and never contact the
operator's live stores. `phone_write` reaches only the scripted Device Twin.
The production Wiki search index is internal to assisted-arm recall and is not
an agent-callable production Wiki tool.

Schema v1 is deny-only for network. A future allowlist requires a new reviewed
contract; it cannot be enabled by adding hosts to a v1 manifest.

Network denial applies to agent tools. The model API connection is the one
runner-controlled exception and is separately authorized by privacy mode and
`--allow-remote-model`.

`runPolicy.timeoutSeconds` begins when executor/closed-loop execution starts;
case loading, authentication, and constructor work are bounded separately by
file/count limits, not this timer.
`perTurnTimeoutSeconds`, when nonzero, bounds each handler call; otherwise the
execution timeout is also the per-turn ceiling. `maxTurns` and `maxTokens` are
cumulative across the complete timeline and every follow-up. A provider-reported
token count is charged against a fixed local estimate over every generated
text, thinking block, tool name, and tool argument, whichever is larger. If the
provider omits usage or reports zero, a conservative one-token-per-serialized
byte upper bound replaces that local fallback. The charge is checked before any
requested tool side effect. Both
raw SSE framing and translated content are capped, including a delimiter-free
multi-line SSE event. `toolPolicy` reserves every attempt before deciding allow/deny, so
denied calls, duplicate IDs, and retries cannot bypass the global budget. A
single model response larger than the remaining case-wide tool-call budget is
rejected before allocation or execution. Duplicate call IDs, invalid
tool/finish-reason combinations, and per-tool limits fail closed.

## Deterministic grader

A grader plan contains a provenance fingerprint, an optional pass threshold,
and one or more weighted checks. Omitted or zero `passThreshold` means `1.0`.

```json
{
  "fingerprint": {
    "runId": "run-20260711-001",
    "caseId": "case-alpha",
    "casepackSha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "model": "model-under-test",
    "providerModel": "provider-reported-model",
    "toolSchemaSha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
    "endpointSha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
    "buildSha256": "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
    "executionProfileSha256": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
    "systemPromptSequenceSha256": "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
    "arm": "memory-assisted",
    "apiMode": "openai",
    "recallMode": "enabled",
    "seed": 42001
  },
  "passThreshold": 0.85,
  "checks": [
    {
      "id": "revised-budget",
      "type": "contains",
      "critical": true,
      "weight": 3,
      "needle": "120"
    },
    {
      "id": "old-budget-forbidden",
      "type": "forbidden",
      "critical": true,
      "weight": 2,
      "needle": "approved budget: 100"
    },
    {
      "id": "state",
      "type": "state_json_equal",
      "weight": 2,
      "expectedState": {
        "approvedBudget": 120,
        "currency": "KRW"
      }
    }
  ]
}
```

Check behavior:

| Type | Evidence and verdict |
|---|---|
| `exact_text` | Full, case-sensitive equality with `Evidence.Text` |
| `contains` | Literal, case-sensitive `needle` must be present |
| `contains_token` | Literal token must be present; numeric needles reject signed, decimal, thousands, and digit-superset matches such as `1200` for `120` |
| `forbidden` | Literal, case-sensitive `needle` must be absent |
| `artifact` | Safe relative regular file must exist below `ArtifactRoot` and match `expectedSha256`; symlinks are invalid |
| `state_json_equal` | Whole JSON value must match semantically; object key order and numeric spelling are ignored, array order is not; duplicate keys and trailing values are invalid |

The score is passed weight divided by total valid weight. Check IDs are capped
at 128 characters, literal text at 8,192 characters, and artifact paths at
1,024 characters. Any invalid plan or
check makes the report `INVALID`. A failed critical check makes the report
`FAIL` even when the weighted score reaches the threshold. Otherwise the score
must meet the threshold for `PASS`. The pure `Grade` function hashes fingerprint
metadata but does not authenticate it; the standalone `score` command performs
the run/case/provenance checks described below before calling `Grade`.

The current artifact check proves byte identity only. It does not open a PDF,
recalculate a spreadsheet, render slides, inspect a video, or judge presentation
quality.

The CLI replaces optional plan wildcards with provenance verified from the run
before emitting the report. It verifies the signed case/seed/arm, exact complete
timeline order, normalized input hashes, release/withhold outcome, scripted
follow-up IDs and messages, requested and provider-reported models, explicit
terminal reasons, cumulative budgets, tool schema, normalized endpoint, running
executable bytes, fixed sampling/execution profile, and the exact finalized
system-prompt hash sequence. It records normalized input and finalized system
prompt hashes, not a byte-for-byte hash of the provider's complete HTTP request.

Manifest `required`, `maxBytes`, and episode `expectedArtifactIds` are signed
authoring metadata in P1; they do not yet create implicit grader checks. A case
author must include an explicit sealed `artifact` check for every scored output.

## CLI

Build or run the command from `gateway-go`:

```bash
go build ./cmd/deneb-briefcase

# Authenticate a casepack and all declared assets.
go run ./cmd/deneb-briefcase validate \
  --case ../bench/briefcase/portable/budget-supersession-v1

# Exercise disposable paths and verify network/exec denial.
go run ./cmd/deneb-briefcase doctor

# Run against a loopback OpenAI-compatible model endpoint.
DENEB_BRIEFCASE_MODEL_BASE_URL=http://127.0.0.1:8000/v1 \
DENEB_BRIEFCASE_MODEL=test-model \
go run ./cmd/deneb-briefcase run \
  --case ../bench/briefcase/portable/budget-supersession-v1 \
  --arm memory-assisted \
  --output /tmp/briefcase-run.json

# Run the sealed three-role loop. The scripted user plan is required because
# this sample signs maxFollowUps=2.
go run ./cmd/deneb-briefcase loop \
  --case ../bench/briefcase/portable/budget-supersession-v1 \
  --base-url http://127.0.0.1:8000/v1 \
  --model test-model \
  --arm memory-assisted \
  --supervisor-plan ../bench/briefcase/portable/budget-supersession-v1/sealed/supervisor-plan.json \
  --user-plan ../bench/briefcase/portable/budget-supersession-v1/sealed/user-simulator-plan.json \
  --output /tmp/briefcase-loop.json

# Grade after the agent process has finished. Declared artifacts were exported
# to /tmp/briefcase-run.json.artifacts; artifactRoot is stored relative to the
# run JSON so both can be moved together.
go run ./cmd/deneb-briefcase score \
  --case ../bench/briefcase/portable/budget-supersession-v1 \
  --plan ../bench/briefcase/portable/budget-supersession-v1/sealed/grader-plan.json \
  --run /tmp/briefcase-run.json
```

For a paired comparison, repeat `run` with `--arm raw-primary`, a separate
`--output`, and otherwise identical case, model, endpoint, and settings. Each
invocation creates a fresh RunRoot. `--skip-recall` alone is not equivalent to
`--arm raw-primary` because it does not withhold `memory: true` sources.

Remote model endpoints require HTTPS, a Portable case, and
`--allow-remote-model`; plain HTTP is accepted only for loopback. The `run` and `loop` commands reject every Vault manifest before
model endpoint authorization, including loopback endpoints; `validate` may
still authenticate its casepack without executing it. API keys are read from
`DENEB_BRIEFCASE_MODEL_API_KEY` (or the environment variable named by
`--api-key-env`), never from a command-line value.

The scorer accepts only a plan declared as a sealed source in the signed
casepack. It rejects a run whose case ID, manifest digest, seed, or arm does not
match; optional grader fingerprint fields are checked when present.

OpenAI-compatible mode forwards the signed seed on every model request.
Anthropic mode has no equivalent field, so `seedForwarded` is false. Both wire
modes explicitly send temperature 0 and top-p 1. OpenAI-compatible mode also
sends zero frequency and presence penalties; Anthropic Messages has no fields
for those penalties, so they remain nominal profile values rather than wire
parameters. Sampling therefore remains provider-best-effort. Each run records
the fixed profile, `apiMode`, `seedForwarded`, requested model, provider model,
endpoint hash, executable hash, and arm.

When a case declares the unique sealed `briefcase:device-plan` role, `run` and
`loop` load it automatically and the harness refuses to start without it.
`--device-plan` may name that same signed source explicitly; it cannot inject a
different plan. The run records both its source SHA-256 and a canonical digest
of the parsed plans, and `score` recomputes and requires both. Cases without the
role forbid device-plan digests.

`loop` likewise accepts only sealed supervisor and user-simulator plans. It
preflights every scripted feedback message against the hidden-token firewall,
checks that supervisor cycles equal the signed follow-up budget plus one, and
records both sealed source digests. A supervisor `FAIL` is written to the result
and then returned as a nonzero CLI outcome.

`run` exports only declared artifacts to `--artifact-dir`, or by default to
`<output>.artifacts`; stdout-only runs receive a durable temporary export.
`loop` exports separate `run/` and `best/` bundles. Export destinations must be
new, so stale files cannot affect scoring. When an output JSON and its export
share a directory tree, `artifactRoot` is relative to that JSON and `score`
resolves it from the run file location; the bundle can therefore be moved as a
unit. If execution fails after at least one completed episode, `run` writes a
`deneb-briefcase-partial/v1` envelope that cannot be decoded as a scoreable
RunResult and still exits nonzero.

The command deletes the plaintext RunRoot by default and treats cleanup failure
as a command error. `--keep-run-root` is a deliberate local-debug escape hatch,
applies even on execution/export failure, and prints the retained path to
stderr. Durable artifact exports contain declared outputs only, not records,
transcripts, memory indexes, or sealed inputs.

## Implemented verification commands

From the repository root:

```bash
cd gateway-go
go test ./cmd/deneb-briefcase ./internal/domain/briefcase ./internal/runtime/briefcase ./internal/eval/briefcase ./internal/eval/briefcase/closedloop
go test -race ./cmd/deneb-briefcase ./internal/domain/briefcase ./internal/runtime/briefcase ./internal/eval/briefcase ./internal/eval/briefcase/closedloop
go vet ./cmd/deneb-briefcase ./internal/domain/briefcase ./internal/runtime/briefcase ./internal/eval/briefcase ./internal/eval/briefcase/closedloop
```

Back at the repository root, the schema files are ordinary JSON and can be
syntax-checked without adding a repository dependency:

```bash
python3 -m json.tool bench/briefcase/schema/manifest.schema.json >/dev/null
python3 -m json.tool bench/briefcase/schema/grader-plan.schema.json >/dev/null
python3 -m json.tool bench/briefcase/schema/device-plan.schema.json >/dev/null
python3 -m json.tool bench/briefcase/schema/supervisor-plan.schema.json >/dev/null
python3 -m json.tool bench/briefcase/schema/user-simulator-plan.schema.json >/dev/null
```

The runtime package includes an end-to-end test in which an OpenAI-compatible
stub model calls `mail_archive`, receives timeline evidence through the real
Deneb handler tool loop, and returns a final answer under frozen semantic time.

## Future gates: Vault and public release

### Vault

`privacyMode: vault` is present in the v1 type and schema, but an operational
Vault run is not implemented or approved. Before enabling it, Deneb needs all of
the following as hard gates:

- encrypted storage and an explicit retention/deletion policy
- local-only model and grader routing, or separately approved data processing
- sealed-answer process isolation and benchmark-memory exclusion
- PII/secret audit and incident procedure
- operator-visible authorization and immutable run audit

Until those gates exist, accepting a Vault-shaped manifest must not launch a
run or send its sources to any provider.

### Public/portable publication

`privacyMode: portable` means the format can be moved; it does not certify that
the data is anonymous, licensed, or safe to publish. Public release requires a
separate exporter and approval gate covering deterministic pseudonymization,
PII and metadata scanning, rights review, answer/canary leakage checks, and a
fresh holdout split. No such publisher is implemented in P0/P1/P2.
