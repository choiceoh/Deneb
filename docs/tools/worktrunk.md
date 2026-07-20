# Worktrunk (wt)

> Operator guide for [Worktrunk](https://worktrunk.dev) — the `wt` CLI that makes git worktrees as cheap as branches. Adopted as the manual lane for parallel work in the dev checkout, alongside the hook-managed agent worktrees.

## Role in this repo

Worktrunk is the **operator's manual worktree lane**. It does not replace the
agent-harness isolation — each lane keeps its own namespace:

| Lane | Worktree path | Branch | Managed by |
|---|---|---|---|
| Operator / manual | `<repo>/.worktrees/<branch>` | free-form | `wt` (this guide) |
| ZCode | `~/.zcode/worktrees/Deneb/<id>` | `zcode/<id>` | `scripts/dev/zcode-worktree-init.sh` |
| Cursor | `~/.cursor/worktrees/Deneb/<id>` | `cursor/<id>` | `scripts/dev/cursor-worktree-init.sh` |
| Claude Code | `.claude/worktrees/<name>` | session branch | native `EnterWorktree` |
| Codex | `~/.codex/worktrees/` | `codex/<id>` | user-scope hooks |

Use `wt` from the dev checkout (`~/deneb-dev`) or any dev clone. Never create
worktrees or branches in `~/deneb` — that checkout is production, auto-deployed
from `main`. <!-- docref:ignore -->

## Install

Prebuilt binary (aarch64 musl, no toolchain needed):

```bash
curl -sL https://github.com/max-sixty/worktrunk/releases/latest/download/worktrunk-aarch64-unknown-linux-musl.tar.xz \
  | tar -xJ --strip-components=1 -C ~/.local/bin --wildcards '*/wt'
wt config shell install bash   # one line in ~/.bashrc: cd-on-switch + completions
```

(`cargo install worktrunk` also works but builds from source.)

## Configuration

- **User config** `~/.config/worktrunk/config.toml` — personal; pins worktrees
  to the repo convention and enables the LLM features: <!-- docref:ignore -->

  ```toml
  worktree-path = "{{ repo_path }}/.worktrees/{{ branch | sanitize }}"

  [list]
  summary = true                 # LLM one-line branch summaries in `wt list --full`

  [commit.generation]
  command = "wt-commit-msg"      # host-deployed from scripts/dev/wt-commit-msg.py
  ```

  `.worktrees/` is gitignored, and existing `~/deneb-dev/.worktrees/*` trees
  are picked up as-is.

- **Project config** `.config/wt.toml` — committed; defines hooks:
  - `post-start` seeds the CodeGraph index for a fresh worktree via
    `scripts/dev/codegraph-autoindex.py` (sibling-index copy + `codegraph sync`,
    backgrounded, fail-open) — same bootstrap the agent harnesses run.
  - `pre-merge` **blocks `wt merge`**: landing goes through PRs +
    `scripts/dev/pr.sh land` (green-gate → squash → landed-verify). For a
    throwaway branch that never touches `main`, `wt merge --no-hooks` is the
    escape hatch.
  - `[commit.generation] template-append` injects the Conventional Commits
    rule into LLM-generated commit messages (only relevant if you wire an LLM
    command in user config).

Worktrunk never runs project hooks until they are approved. Approvals are
keyed by repo identity (`github.com/choiceoh/deneb`), so one approval covers
every checkout and worktree of this repo:

```bash
wt config approvals list   # see pending hook commands
wt config approvals add    # approve them (interactive multi-select)
```

`approvals add` is **interactive-only** — in a non-interactive shell it fails
with "Cannot prompt". Agents therefore run `wt -y <cmd>` instead, which skips
the prompt **for that invocation only** (verified: `-y` does not persist an
approval). Re-approval is required whenever a hook command changes.

## LLM commit messages and branch summaries

`wt step commit` (stage + commit with a generated message), squash messages,
and the `wt list --full` per-branch summaries (`[list] summary = true`,
cached until the branch diff changes) are all powered by
`scripts/dev/wt-commit-msg.py`. It calls the wormhole (`127.0.0.1:18800`,
token read at runtime from `~/.wormhole/config.json`) with the local GPU <!-- docref:ignore -->
model — no external API key, unmetered. Override with
`WT_COMMIT_MODEL=glm-5.2` for higher quality. The Conventional Commits rule
is injected automatically via `template-append` in `.config/wt.toml`.

The script is deployed host-wide as `~/.local/bin/wt-commit-msg` (re-run <!-- docref:ignore -->
`install -m 755 scripts/dev/wt-commit-msg.py ~/.local/bin/wt-commit-msg` <!-- docref:ignore -->
after editing it) so it also works in checkouts whose branch predates the
repo file — a worktree-relative command would silently break there.

## Shell prompt statusline

`~/.bashrc` appends a guarded block that prints `wt list statusline` (~80ms) <!-- docref:ignore -->
above the prompt inside git repos and nothing elsewhere — branch, dirty
state, ahead/behind, and CI at a glance. Remove the block to disable. The
Claude Code statusline equivalent is wired separately (below).

## Claude Code plugin and statusline

Installed on the gateway host (both are user-level, repo-independent):

- `wt config plugins claude install` — the Worktrunk plugin (marketplace), so
  Claude Code sessions get wt-aware guidance. Requires the `claude` CLI
  (`npm i -g @anthropic-ai/claude-code`, lives in `~/.npm-global/bin`). <!-- docref:ignore -->
- `wt config plugins claude install-statusline` — sets the Claude Code
  statusline to `wt list statusline --format=claude-code`
  (branch / worktree / dirty-state at a glance during sessions).

## Daily commands

```bash
wt switch -c fix-thing     # create branch + worktree at .worktrees/fix-thing, cd into it
wt switch main             # jump back to the main tree
wt list                    # all worktrees: dirty state, ahead/behind, age
wt list --full             # + CI status and LLM summaries (needs gh)
wt step commit             # stage + commit with an LLM-generated message
wt land                    # alias: push -> ensure PR -> pr.sh land (green-gate, squash)
wt remove                  # delete current worktree; deletes branch if merged (-D forces)
```

Landing stays on the repo pipeline — `wt land` (alias in `.config/wt.toml`)
just chains push → `gh pr create --fill` → `scripts/dev/pr.sh land`. After a
land, `wt remove` the worktree, and periodically:

```bash
wt step prune              # remove all worktrees whose branch is merged into main
wt step for-each -- <cmd>  # run a command in every worktree (fleet ops)
```

(`scripts/dev/zcode-cleanup.sh` still handles the `zcode/*` namespace.)

## Automated consumers

worktrunk is not an operator-only surface — the fleet snapshot feeds the RSI
loop:

- **Branch-rot miner** (`scripts/audit/branch_rot_miner.py`,
  `deneb-branch-rot.timer` weekly): reads `wt list --format json` for the dev
  checkout and files stale ahead-of-main branches (no open PR, past the
  staleness bar) as propose-only scope=code recovery candidates through the
  self-correction review lane. wt's trees-match integration detection splits
  retire (content already in main — verify, then delete) from recover
  (rebase → land or retire) candidates, and the cached `--full` LLM branch
  summaries ride along as review evidence. Source namespace `branch-rot`
  starts Staged on the dispatch ladder — see
  [self-improvement](/agent-rules/self-improvement).
- The RSI **coding dispatch** lane keeps its own per-attempt worktree
  mechanics (`scripts/dev/coding-dispatch.sh` — unique branch + worktree per
  attempt with its own cleanup); its worktrees appear in `wt -C ~/deneb list` <!-- docref:ignore -->
  like any other, so the whole agent fleet is visible from one place.

## Parallel agents

`wt switch -x` creates a worktree and launches a command inside it:

```bash
wt switch -c -x claude perf-tuning -- '프롬프트 캐시 히트율을 조사해줘'
```

This is the documented Worktrunk pattern for running several Claude Code
sessions side by side without them stepping on each other's tree. The
`post-start` hook has already seeded CodeGraph by the time the agent starts.

## Troubleshooting

- **Hooks silently not running** — approvals missing; run `wt config approvals list` in the worktree's repo.
- **`wt switch -c` refuses to create the worktree** — same cause: an unapproved `post-start` hook aborts creation entirely. Approve, or one-shot `wt -y switch -c`.
- **`wt` not changing directory** — shell integration not active in this shell; `exec bash` or re-source `~/.bashrc`. <!-- docref:ignore -->
- **`wt merge` fails with a pr.sh message** — intentional; see `.config/wt.toml` `pre-merge`.
- **`wt step commit` fails** — read stderr from `scripts/dev/wt-commit-msg.py`: wormhole down or token unreadable; commit manually or via `scripts/committer`.
- Verbose diagnostics: `wt -v <cmd>` (hook/template vars) or `WORKTRUNK_VERBOSE=2`.
