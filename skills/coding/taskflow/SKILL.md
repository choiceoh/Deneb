---
name: taskflow
version: "1.0.0"
category: coding
description: "Coordinate multi-step Deneb work as one durable owner flow. Use when: work spans background agents, PR review loops, waits for CI/human input, or needs resumable state. NOT for: one-shot edits, simple shell commands, or cron schedule authoring."
metadata:
  {
    "deneb":
      {
        "emoji": "🪝",
        "tags": ["background", "workflow", "subagent", "state", "resume", "review"],
        "related_skills": ["review-closeout", "github", "tmux", "session-logs"],
      },
  }
---

# TaskFlow

Use this to keep long Deneb coding/ops work coherent across waits, background
agents, PR reviews, CI, and follow-up turns.

## When to Use

- A task has one owner request but multiple child work items.
- Work waits on CI, PR review, a human answer, or a remote host.
- A coding agent must continue after context compaction or app restart.
- The user asked for "진행", "계속", "리뷰 달림", "머지", or similar lifecycle
  steps on an existing lane.

## Flow Shape

Keep one compact state block in your working notes or final handoff:

```text
TaskFlow:
- owner: <user request / PR / issue>
- currentStep: <inspect | patch | test | review | wait-ci | merge | verify>
- state: <branch, PR, changed files, blockers, proof>
- wait: <none | CI | review | user | remote host>
- children: <background agents / sessions / tmux panes>
- next: <single next action>
```

## Procedure

1. Identify the owner: user request, PR number, issue, branch, or deploy lane.
2. Record the current step and the single next action before starting child work.
3. When spawning or supervising a worker, give it the owner, branch/PR, expected
   proof, and one completion route. Do not rely on silent background success.
4. When waiting, say what is awaited and what evidence will unblock it.
5. On resume, read the latest real state first: git, PR, CI, logs, or remote
   listener. Do not continue from stale memory when live state is cheap to check.
6. Finish with proof tied to the owner: tests, PR/merge commit, deployed route,
   or explicit blocked reason.

## State Discipline

- Store only state needed to resume; avoid transcript dumps.
- Keep child work linked by stable handles: PR URL, branch, session key, tmux
  session, check run, remote host.
- If a child agent proposes a source-level improvement but cannot apply it,
  queue it as `skill_lifecycle` `self_correction`; do not bury it in chat.
- If a task changes scope, create a new owner flow or stop and ask.

## Closeout

Every flow closeout should answer:

- What changed?
- What proof ran?
- What is still waiting, if anything?
- What exact live state proves completion?
