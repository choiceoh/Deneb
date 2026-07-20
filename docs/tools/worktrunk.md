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

- **User config** `~/.config/worktrunk/config.toml` — personal; holds the path
  template that pins worktrees to the repo convention: <!-- docref:ignore -->

  ```toml
  worktree-path = "{{ repo_path }}/.worktrees/{{ branch | sanitize }}"
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

Worktrunk never runs project hooks until they are approved once per checkout:

```bash
wt config approvals list   # see pending hook commands
wt config approvals add    # approve them (one-shot gate)
```

## Daily commands

```bash
wt switch -c fix-thing     # create branch + worktree at .worktrees/fix-thing, cd into it
wt switch main             # jump back to the main tree
wt list                    # all worktrees: dirty state, ahead/behind, age
wt list --full             # + CI status and LLM summaries (needs gh)
wt remove                  # delete current worktree; deletes branch if merged
```

Landing stays unchanged: push the branch, open a PR, `scripts/dev/pr.sh land <pr>`.
After a land, `wt remove` the worktree (or `wt list` to spot stale ones —
`scripts/dev/zcode-cleanup.sh` still handles the `zcode/*` namespace).

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
- **`wt` not changing directory** — shell integration not active in this shell; `exec bash` or re-source `~/.bashrc`. <!-- docref:ignore -->
- **`wt merge` fails with a pr.sh message** — intentional; see `.config/wt.toml` `pre-merge`.
- Verbose diagnostics: `wt -v <cmd>` (hook/template vars) or `WORKTRUNK_VERBOSE=2`.
