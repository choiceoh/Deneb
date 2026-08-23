# ZCode Development Environment

> Complete guide to the ZCode agent setup for the Deneb repo — worktree isolation, codegraph integration, helper scripts, and the hook pipeline shared with Claude Code and Codex.

## Architecture at a glance

```
SessionStart ──→ zcode-worktree-init.sh / cursor-worktree-init.sh
                                              auto-creates agent worktree + copies codegraph index

PreToolUse ────→ worktree-guard              blocks edits in the main checkout
                clash check                   cross-worktree conflict detection (ZCode)
                claude-rules-gate.py          path-scoped rule guidance (1st touch)
                codegraph-remind.py           CodeGraph nudge on source read/edit (1x/session)
                codegraph-nudge.py            symbol grep → CodeGraph redirect (1x/pattern)
                pre-commit-gate.sh            go-fmt/vet on committer calls (ZCode)

PostToolUse ───→ codegraph-sync               background codegraph sync after edits (<0.5s)

Stop ──────────→ zcode-worktree-status.sh    reports commit/uncommitted counts (ZCode)

MCP ───────────→ codegraph                    codegraph-serve.sh (worktree bind + explore→node proxy)
```

Every coding agent shares one CodeGraph stack (`codegraph_root.py` + `codegraph-serve.sh` + explore→node proxy + `codegraph-sync-run.sh`):

| Agent | Config file | Worktree location | Branch prefix |
|-------|-------------|-------------------|---------------|
| **ZCode** | `.zcode/config.json` | `~/.zcode/worktrees/Deneb/` | `zcode/` |
| **Cursor** | `.cursor/hooks.json` + `.cursor/mcp.json` | `~/.cursor/worktrees/Deneb/` | `cursor/` |
| **Claude Code** | `.claude/settings.json` + `.mcp.json` | native `EnterWorktree` | `claude/` |
| **Codex** | `.codex/config.toml` + `.codex/hooks.json` | `~/.codex/worktrees/Deneb/` | `codex/` <!-- docref:ignore --> |
| **Trae** | `.trae/mcp.json` | `~/.trae/worktrees/Deneb/` | `trae/` |
| **VS Code / Copilot** | `.vscode/mcp.json` | workspace folder | — |

Serve wrappers refuse the production checkout, bind that agent's `active-root` worktree, evict a stale daemon, and reroute a single-symbol `explore` to `node` (same precision as the runtime gateway). `codegraph upgrade` clobbers MCP configs — `codegraph_mcp_restore.py` rewrites only the fingerprint.

## Configuration files

### `.zcode/config.json` (workspace scope)

Defines:

- **MCP server**: `codegraph` via `bash scripts/dev/codegraph-serve.sh` (project-relative; portable across machines).
- **Hooks**: 8 hooks across SessionStart / PreToolUse / PostToolUse / Stop, with `hooks.enabled: true`.

### `.cursor/hooks.json` + `.cursor/mcp.json` (workspace scope)

Cursor's equivalent:

- **MCP**: `.cursor/mcp.json` → `cursor-codegraph-serve.sh` (binds to `~/.cursor/worktrees/Deneb/active-root` when set, else workspaceFolder).
- **Hooks**: `sessionStart` → `cursor-worktree-init.sh`; `preToolUse` → main-checkout guard (`failClosed: true`) + rules-gate + codegraph-nudge; `afterFileEdit` → codegraph sync; `postToolUse` → codegraph-remind.
- **Isolation**: session worktrees under `~/.cursor/worktrees/Deneb/<session-id>` on `cursor/<session-id>`. After SessionStart, enter with `move_agent_to_root` then `git checkout cursor/<session-id>` (restores branch if the move rewrote it to `main`, and rebinds MCP).
- **Rule**: `.cursor/rules/worktree-codegraph.mdc` (`alwaysApply`).

### `.claude/settings.json` (workspace scope)

Claude Code's equivalent: SessionStart codegraph-autoindex, PreToolUse clash+rules-gate+nudge+remind, PostToolUse codegraph-sync.

### `.codex/config.toml` + `.codex/hooks.json` (project scope)

Codex loads these in trusted checkouts (CLI + IDE extension). MCP goes through `codegraph-serve.sh`; hooks restore wrappers, seed the index, nudge symbol greps, and sync after edits. User-level `~/.codex/` stays untouched so operator hooks are not rewritten.

### `.trae/mcp.json` + `.vscode/mcp.json`

Trae and VS Code / Copilot use the same wrapper. `.vscode/` is gitignored except `mcp.json`.

## Worktree isolation

### How it works

1. **SessionStart** (`zcode-worktree-init.sh`): creates `~/.zcode/worktrees/Deneb/<session-id>` on branch `zcode/<session-id>` from `main`. Copies `.codegraph/` from the main checkout and runs `codegraph sync` in the background. Idempotent — reuses existing worktree on session resume.

2. **PreToolUse guard** (`zcode-worktree-guard.sh`): blocks Write/Edit/MultiEdit in the main checkout when on the `main` branch. Passes inside a linked worktree or when the user explicitly checked out a different branch. Exit 2 = deny with guidance to `cd` into the worktree.

3. **Guard fallback chain**: `CLAUDE_SESSION_ID` env → stdin JSON `session_id` → most-recently-modified `~/.zcode/worktrees/Deneb/*/` (robust even when the session ID is unavailable in the PreToolUse environment).

4. **Stop** (`zcode-worktree-status.sh`): reports commit count and uncommitted file count as `additionalContext`. No PR/push — those remain explicit-only per `CLAUDE.md` safety rules.

### Cleanup

```bash
scripts/dev/zcode-cleanup.sh           # dry-run — show what would be removed
scripts/dev/zcode-cleanup.sh --apply      # actually remove stale/merged worktrees
```

Removes worktrees whose branch is merged into `main` or older than N days (default 7) with no uncommitted changes. Preserves dirty worktrees.

## CodeGraph integration

### Prerequisites

```bash
npm i -g @colbymchenry/codegraph     # install (v1.5.0+)
codegraph init                        # build index (~10s, ~55K nodes)
```

### Index quality

The index excludes generated code, XML resources, and other noise via `codegraph.json`:

| Exclude pattern | What it filters |
|-----------------|-----------------|
| `**/*_gen.go` | Go generated code (tool_classification_gen.go, etc.) |
| `**/generated/**` | Kotlin wire types (MiniappWireTypes.kt) |
| `**/src/gen/**` | TS wire types (miniappWire.ts) |
| `**/.run/**` | IntelliJ run configs |
| `**/AndroidManifest.xml` | Android manifests |
| `**/composeResources/**` | Vector drawables, string resources |
| `**/mockServiceWorker.js` | MSW test mock |
| `**/config/detekt-baseline.xml` | Detekt lint baseline |
| `scripts/audit/**` | Audit scripts (CAPS constants collide with symbols) |
| `scripts/dev/**` | Dev hooks/helpers (not product source) |
| `**/testdata/**` | Test fixtures |
| `**/changelog.d/**` | Native changelog fragments |

After cleanup (post-precision excludes): ~**54.6K nodes**; audit/dev script constants removed from the graph.

Precision notes (Deneb):

- CLI/MCP `query`/`node`/`callers` pin exact symbols; `explore` may camelCase-split (`GatewayHub` → `Hub`/`GatewayTab`) — prefer `node` for a single known symbol.
- MCP exposes `explore,node,search,impact,callers,callees` via `CODEGRAPH_MCP_TOOLS`. `codegraph_mcp_proxy.py` reroutes a single-symbol explore to node (miss → original explore).
- `codegraph.json` excludes audit/dev scripts and generated/resource noise; re-index after changes.
- Runtime: CodeGraph **1.5.0+** (`npm i -g @colbymchenry/codegraph@1.5.0`). `codegraph upgrade` rewrites MCP configs — `codegraph_mcp_restore.py` (SessionStart) rewrites Cursor / Claude / ZCode / Codex / Trae / VS Code wrappers if the upgrade fingerprint is present. Serve wrappers evict a stale-version daemon and bind `codegraph_root.py`'s worktree pin (never the production checkout).

### Auto-sync

The **PostToolUse / afterFileEdit hooks** (`zcode-codegraph-sync.sh`, `cursor-codegraph-sync.sh`, Codex `.codex/hooks.json`) run the shared `codegraph-sync-run.sh` pipeline in the background: `codegraph sync` → rpcmap synthetic edges → optional semantic index refresh. Worktree seeds go through `codegraph-seed-index.sh` so daemon pid/sock files are not copied.

### rpcmap — the string-key edge filler

CodeGraph connects symbol→symbol edges but **not** string-keyed indirection. `scripts/dev/rpcmap.py` fills three gaps:

```bash
rpcmap miniapp.people.list           # RPC method → handler
rpcmap wiki                          # chat tool → handler
rpcmap chat.delivery_failed          # event broadcast → event type
rpcmap --handler peopleList          # reverse lookup
rpcmap --list                        # dump everything (~270 mappings)
```

Feed the resolved handler to `codegraph node <handler>` for full source + callers/callees.

### Known limitation: route false positives

CodeGraph misidentifies Go method calls like `c.Put("a", 1)` as HTTP routes (`PUT a`). 38 of 82 route nodes are false positives from test code. Reported upstream: [colbymchenry/codegraph#1259](https://github.com/colbymchenry/codegraph/issues/1259).

## Helper scripts

All scripts live in `scripts/dev/` and are executable.

### `zcode-push.sh` — fetch + rebase + push

Solves multi-agent push collisions (other agents pushing to main between our commits):

```bash
scripts/dev/zcode-push.sh             # current branch
scripts/dev/zcode-push.sh main        # explicit branch
```

Auto-stashes uncommitted changes, rebases on `origin/<branch>`, restores stash. Refuses to force-push `main`.

### `zcode-commit.sh` — local validation + Docker fallback

Solves the OrbStack/Docker pre-commit hook hang (ShellCheck/golangci-lint stuck on Docker):

```bash
scripts/dev/zcode-commit.sh "message" file1 file2 ...
```

1. Runs **local** `shellcheck` and `golangci-lint` on staged files (no Docker, fast).
2. Tries `scripts/committer` with full pre-commit hooks.
3. If pre-commit fails (Docker stuck or pre-existing debt), retries with `--no-verify`.

Local validation failures (real code issues) always block — only infrastructure failures trigger the fallback.

### `zcode-cleanup.sh` — stale worktree removal

```bash
scripts/dev/zcode-cleanup.sh           # dry-run
scripts/dev/zcode-cleanup.sh --apply   # execute
scripts/dev/zcode-cleanup.sh --apply 14  # custom age threshold (days)
```

### Hook bridge: `zcode-hook-bridge.sh`

ZCode and Claude Code use the same hook scripts (codegraph-nudge, codegraph-remind, claude-rules-gate) but with slightly different env vars and stdin payloads. The bridge normalizes them:

- Ensures `CLAUDE_PROJECT_DIR` is set (falls back to `ZCODE_PROJECT_DIR`).
- Enriches stdin JSON with `cwd` and `session_id` if missing.
- Passes enriched payload to the target Python script.

## Hook pipeline detail

### PreToolUse matchers

| Matcher | Hooks | Purpose |
|---------|-------|---------|
| `Write\|Edit\|MultiEdit` | worktree-guard, clash check, rules-gate | block main-checkout edits, detect conflicts, guide rules |
| `Read\|Edit\|Write\|MultiEdit` | codegraph-remind | CodeGraph nudge on first source file access (1x/session) |
| `Grep\|Bash` | codegraph-nudge, pre-commit-gate | redirect symbol greps to CodeGraph, go-fmt/vet on committer |

### PostToolUse matchers

| Matcher | Hook | Purpose |
|---------|------|---------|
| `Write\|Edit\|MultiEdit` | codegraph-sync | background `codegraph sync` after edits |

## Setup checklist (new machine)

1. **Install codegraph**: `npm i -g @colbymchenry/codegraph && codegraph init` (in the repo root)
2. **Verify MCP**: restart ZCode/Cursor → Settings → MCP → codegraph should show "connected"
3. **Verify worktree isolation**: start a new ZCode session → `~/.zcode/worktrees/Deneb/<session-id>`; Cursor chat → `~/.cursor/worktrees/Deneb/<session-id>`
4. **Verify guard**: try editing in the main checkout → should be blocked with worktree path guidance
5. **Verify codegraph hooks**: grep a known symbol (e.g., `GatewayHub`) → should get CodeGraph nudge
6. **Cursor**: open the repo (or a linked worktree) so `.cursor/hooks.json` + `.cursor/mcp.json` load; new Agent chat should inject the worktree path via `sessionStart`
7. **Optional — install OrbStack**: for Docker-based pre-commit hooks (ShellCheck, golangci-lint). If OrbStack is unstable, use `zcode-commit.sh` which falls back to `--no-verify`.

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| Worktree not created on session start | SessionStart hook didn't fire or `CLAUDE_SESSION_ID` missing | Run `bash scripts/dev/zcode-worktree-init.sh` manually |
| Guard blocks with no `cd` path | No zcode worktree exists | Run `zcode-worktree-init.sh` or create one manually |
| `codegraph_explore` not available | MCP server not connected | Check Settings → MCP; verify `bash scripts/dev/cursor-codegraph-serve.sh` starts; reload Cursor window |
| Cursor main-checkout edit blocked | Guard fired (expected) | Retry under `~/.cursor/worktrees/Deneb/<session-id>`; enter via `move_agent_to_root` then `git checkout cursor/<id>` |
| Cursor worktree branch became `main` | `move_agent_to_root` rewrote the worktree | Immediately `git checkout cursor/<session-id>` in that worktree |
| CodeGraph answers look stale vs edits | MCP still bound to primary checkout | Confirm `active-root` points at the session worktree; reload MCP or re-enter via `move_agent_to_root` |
| Push fails with non-fast-forward | Another agent pushed to main | Use `scripts/dev/zcode-push.sh` (auto rebase) |
| Commit hangs on pre-commit | OrbStack/Docker stuck | Use `scripts/dev/zcode-commit.sh` (local validation + fallback) |
| CodeGraph index stale | PostToolUse sync didn't run | Run `codegraph sync` manually |
| Route nodes are false positives | CodeGraph parser bug (`c.Put` → `PUT`) | Known issue — [#1259](https://github.com/colbymchenry/codegraph/issues/1259) |
