---
title: "Default AGENTS.md"
summary: "Default Deneb agent instructions and skills roster for the personal assistant setup"
read_when:
  - Starting a new Deneb agent session
  - Enabling or auditing default skills
---

# AGENTS.md - Deneb Personal Assistant (default)

## First run (recommended)

Deneb uses a dedicated workspace directory for the agent. Default: `~/.deneb/workspace` (configurable via `agents.defaults.workspace`).

1. Create the workspace (if it doesn’t already exist):

```bash
mkdir -p ~/.deneb/workspace
```

2. Copy the default workspace templates into the workspace:

```bash
cp docs/reference/templates/AGENTS.md ~/.deneb/workspace/AGENTS.md
cp docs/reference/templates/SOUL.md ~/.deneb/workspace/SOUL.md
cp docs/reference/templates/TOOLS.md ~/.deneb/workspace/TOOLS.md
```

3. Optional: if you want the personal assistant skill roster, replace AGENTS.md with this file:

```bash
cp docs/reference/AGENTS.default.md ~/.deneb/workspace/AGENTS.md
```

4. Optional: choose a different workspace by setting `agents.defaults.workspace` (supports `~`):

```json5
{
  agents: { defaults: { workspace: "~/.deneb/workspace" } },
}
```

## Safety defaults

- Don’t dump directories or secrets into chat.
- Don’t run destructive commands unless explicitly asked.
- Don’t send partial/streaming replies to external messaging surfaces (only final replies).

## Session start (required)

- Read `SOUL.md`, `USER.md`.
- Read recent diary via `wiki(action="daily")` (today + yesterday context).
- Read `MEMORY.md` when present; only fall back to lowercase `memory.md` when `MEMORY.md` is absent.
- Do it before responding.

## Soul (required)

- `SOUL.md` defines identity, tone, and boundaries. Keep it current.
- If you change `SOUL.md`, tell the user.
- You are a fresh instance each session; continuity lives in these files.

## Shared spaces (recommended)

- You’re not the user’s voice; be careful in group chats or public channels.
- Don’t share private data, contact info, or internal notes.

## Memory system (recommended)

- **Diary:** `wiki(action="log")` / `wiki(action="daily")` — day-by-day logs (not a separate `memory/` folder).
- **Knowledge pages:** `knowledge(op="record")` — curated people/projects/decisions.
- **Curated personal context:** `MEMORY.md` for durable facts, preferences, and decisions (loaded on main-session turns).
- Lowercase `memory.md` is legacy fallback only; do not keep both root files on purpose.
- Capture: decisions, preferences, constraints, open loops.
- Avoid secrets unless explicitly requested.

## Tools & skills

- Tools live in skills; follow each skill’s `SKILL.md` when you need it.
- Keep environment-specific notes in `TOOLS.md` (Notes for Skills).

## Deferred self-corrections

- Before coding, review, or skill-evolution work, inspect `skill_lifecycle` action `status` and read `selfCorrectionCandidates`.
- If you notice a plausible correction but cannot safely apply and validate it now, record it with `skill_lifecycle` action `self_correction` using `title`, `evidence`, `targetFiles`, `proposedChange`, and `risk`.
- Treat queued items as unapplied hypotheses. Apply them only after batch review and tests, then mark them with `skill_lifecycle` action `self_correction_review` as `accepted`, `rejected`, `superseded`, or `applied`.
- The append-only queue is stored at `~/.deneb/data/self_correction_candidates.jsonl` for agents that need direct inspection.

## Backup tip (recommended)

If you treat this workspace as the agent's memory, make it a git repo (ideally private) so `AGENTS.md` and your memory files are backed up.

```bash
cd ~/.deneb/workspace
git init
git add AGENTS.md
git commit -m "Add Deneb workspace"
# Optional: add a private remote + push
```

## What Deneb Does

- Runs the Go gateway on the DGX Spark host so the assistant can read/write chats, fetch context, and run skills.
- The native client's home conversation is the `client:main` session; explicit conversations live at `client:main:<suffix>`, and background work runs under `cron:<job>:<ts>` / `system:<name>` sessions. Heartbeats keep background tasks alive.

## Bundled Skills (`skills/`, filesystem-discovered)

Skill discovery is filesystem-driven — the gateway indexes `skills/` at startup
and the native client's Settings → 스킬 tab lists them read-only (no toggles).

- **productivity/** — contract-review, decision-premortem, deep-research, email-analysis, fact-check, meeting-minutes, morning-letter, proactive-gate, retrieval-plan, session-logs, weekly-report
- **coding/** — evolution-proposal, github, self-evolve, skill-creator, skill-evolution, skill-factory
- **knowledge/** — kb-interview
- (devops/, integration/, operations/, security/ are empty placeholder categories — `DESCRIPTION.md` only, no skills yet)

## Usage Notes

- Keep heartbeats enabled so the assistant can schedule reminders and monitor inboxes.
- Skills are read via each skill's `SKILL.md` on demand; there is no install step — adding a directory under `skills/` is the install.

## ZCode development agent rules

When this repository is opened in **ZCode** (not the Deneb personal assistant), the development rules live in **`CLAUDE.md`** at the repo root. Read it at session start — it carries the authoritative safety gates, Git/PR conventions, style rules, CodeGraph usage, and the `docs/agent-rules/` conditional-loading index that the ZCode hooks reference.

- **Safety** (CLAUDE.md "안전" section): production paths, multi-agent worktree isolation, generated-code rules, security CODEOWNERS — always apply.
- **ZCode worktree isolation**: the SessionStart hook auto-creates `~/.zcode/worktrees/Deneb/<session-id>`. `cd` into it before any edit; the guard blocks main-checkout edits.
- **CodeGraph**: the `codegraph_explore` MCP tool is wired via `.zcode/config.json`. Use it for structural/relational queries; hooks nudge you there on symbol greps and source edits.
- **Rules gate**: editing a path under `docs/agent-rules/*.md` globs triggers a one-time pointer (via `zcode-hook-bridge.sh` → `claude-rules-gate.py`). Read the flagged rule, then retry.

## Cursor Cloud specific instructions

Durable, non-obvious notes for cloud agents. The startup update script already runs `go mod download` (in `gateway-go/`) and `corepack pnpm install` (in `andromeda/`); toolchains below are pre-installed in the VM snapshot, so this section is about *running* things, not installing them. Standard build/test commands live in `README.md`, the root `Makefile`, and `andromeda/CLAUDE.md` — use those; only the caveats below are non-obvious.

### Toolchains (pre-installed in snapshot)
- **Go 1.25** is required (`gateway-go/go.mod` = `go 1.25.0`); the base image's system Go is older. A 1.25 toolchain lives at `/usr/local/go` and is symlinked into `/usr/local/bin/go`. Verify with `go version`.
- **`golangci-lint` v2.5** (matches CI pin) is in `~/go/bin`. The root `Makefile` prepends `~/go/bin` to `PATH`, so `make check` / `make go-lint` resolve it automatically. A bare `golangci-lint` invocation needs `export PATH="$HOME/go/bin:$PATH"` first.
- **`gofumpt`** is not installed as a binary — the Makefile runs it via `go run mvdan.cc/gofumpt@v0.10.0` (`make go-fmt` / `make fmt`).
- **`pnpm` 11.7.0** is provided via corepack (activated + `corepack enable` done). In `andromeda/`, plain `pnpm …` uses 11.7.0; `corepack pnpm …` also works and is the robust form.

### Running the gateway (primary runtime) without prod config or an LLM key
- `scripts/dev/live-test.sh start` builds and boots a dev gateway on `127.0.0.1:18790` (prod on 18789). When `~/.deneb/deneb.json` is absent, `config-gen.sh` writes a minimal dev-safe config, so it boots standalone. `stop` / `restart` / `status` / `smoke` / `logs` as documented in the script header.
- Expected without an LLM API key + GPU sidecars: `/health` shows `providers: 0` and `embedding`/`local_ai` = `unhealthy`. This is normal — `/health` still reports `status: ok`, `/ready` returns 200, and ~57 tools + 4 models load. Chat won't produce replies (needs a provider key), but the RPC surface and local stores work.
- Exercise the RPC surface directly: `POST /api/v1/miniapp/rpc` with body `{"type":"req","id":"1","method":"<m>","params":{…}}` and header `X-Deneb-Client-Token: <token>`, where the token is in the dev state dir at `/tmp/deneb-dev-state/client_token`. Non-LLM methods like `miniapp.todo.create` / `miniapp.todo.list` are a good end-to-end smoke of dispatch + auth + local persistence. Map an RPC name to its handler with `python3 scripts/dev/rpcmap.py <method>`.
- `golangci-lint run ./...` on the full tree reports 2 pre-existing findings (an `SA4000` in a chat test + one `unused` func); CI runs `--new` diff mode so they don't block. Don't "fix" them as part of unrelated work.

### andromeda (desktop client)
- The sandbox is network-isolated and **cannot reach a gateway even on the same host**, so for UI work run `pnpm dev:mock` (Vite on `:1420` with MSW mock data) rather than `pnpm dev`.
- `pnpm verify` = typecheck + lint + format:check + test + build. `format:check` currently fails on a pre-existing committed file (`src/gateway.recovery.test.ts` is not prettier-clean) — a repo issue, not an environment problem; typecheck, lint, the full test suite (~1993 tests), and build all pass.
- `pnpm install` regenerates `andromeda/public/mockServiceWorker.js` (MSW postinstall). That shows up as a modified file — it's an install artifact; do not commit it.
- Full Tauri desktop build (`pnpm tauri:dev` / `tauri:build`) needs system GUI libs (`webkit2gtk-4.1`) not present here; only the web/`dev:mock` path is verifiable in this environment.

### client-android (mobile Kotlin)
- Not set up in this environment: the Gradle build needs Android SDK 37 (`~/android-sdk`), which is not installed. Out of scope for the current cloud setup; add the SDK if a Kotlin lane is needed.
