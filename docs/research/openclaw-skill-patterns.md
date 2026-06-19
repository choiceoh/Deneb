---
title: "OpenClaw Skill Patterns for Deneb"
summary: "Which OpenClaw skill patterns Deneb adopted, adapted, or rejected"
read_when:
  - Changing Deneb skill discovery, skill metadata, native Skills UI, or self-improvement coding queue
  - Adding external agent skills or reviewing ClawHub/OpenClaw skill imports
  - Designing background coding, PR review closeout, remote proof, or native connection diagnostics
---

# OpenClaw Skill Patterns for Deneb

## Sources Checked

- https://github.com/openclaw/agent-skills
- https://docs.openclaw.ai/tools/skills
- https://github.com/openclaw/openclaw
- https://github.com/Gen-Verse/OpenClaw-RL

## Adopted Patterns

### Skill eligibility and install visibility

OpenClaw's strongest product pattern is not any single skill; it is the way
skills declare host requirements, config gates, install hints, and visible
metadata. Deneb already parses `metadata.deneb.requires` and `install`, so the
adoption path is to expose that state in the native skill catalog instead of
importing OpenClaw's loader wholesale.

Deneb implementation direction:

- Keep filesystem discovery and prompt filtering as the source of truth.
- Show homepage, tags, related skills, dependency summary, and install summary
  in native skill rows/detail.
- Do not silently show retired or unavailable skills as usable.

### Agent transcript and session viewer provenance

OpenClaw's `agent-transcript` and `session-viewer` are useful as a pattern:
reviewers need compact, redacted provenance for why an agent changed code. Deneb
should not publish raw local logs. The Deneb-shaped version is:

- session key when available,
- changed files,
- evidence text,
- focused validation,
- PR/commit URL,
- no raw tool output, secrets, cookies, or unrelated turns.

This maps directly to `selfCorrectionCandidates` and Settings > `자가개선 코딩`.

### Review closeout scope governor

OpenClaw's `autoreview` skill has a strong closeout rule: review output is
advisory, every finding must be verified against real code, and review-triggered
fixes must stay inside the original PR scope.

Deneb adoption:

- New `review-closeout` skill owns PR review response and merge-readiness
  workflow.
- `github` remains the command/reference skill.
- Findings become blocker/follow-up/stop, not an invitation to rewrite the PR.

### Remote validation proof

OpenClaw's `crabbox` is too specific to import. The useful pattern is proof
reporting: actual provider/host/ref, command result, lease or listener identity,
and cleanup state.

Deneb adoption:

- New `remote-validation` skill uses Deneb surfaces: local, CI, `srv1`, DGX,
  native artifact, listener, or deploy checkout.
- Reports must name the proof surface and limitation.

### TaskFlow owner state

OpenClaw's `taskflow` captures one owner flow, child work, waiting state, and
revision-safe resume. Deneb does not need the same runtime API immediately, but
agents need the discipline.

Deneb adoption:

- New `taskflow` skill records owner/currentStep/state/wait/children/next.
- Long PR/review/deploy lanes should resume from live state first.

### Native node connection diagnostics

OpenClaw's `node-connect` is directly relevant to Deneb native app support.
Deneb's version should avoid mixing LAN/Tailscale/public routes and separate
reachability from pairing/auth.

Deneb adoption:

- New `node-connect` skill for Android/native app to gateway connection issues.
- One route, one diagnosis, one next action.

### Next-state feedback as self-improvement input

OpenClaw-RL's OPD idea treats next user/environment/tool feedback as hindsight
that can improve future behavior. Deneb should not jump to model training. The
useful product translation is:

external feedback -> structured hint -> queued candidate -> batch review ->
focused validation.

Signals:

- user correction,
- PR review comment,
- test failure,
- tool/runtime error,
- failed deploy/listener check,
- post-merge reality mismatch.

Targets:

- `validation_case_from_session` when replayable,
- `self_correction` when code/prompt/docs/config/tests need later inspection,
- no-op when evidence is weak.

## Rejected or Deferred

- Do not bulk-install ClawHub/community skills. Third-party skills are executable
  supply-chain artifacts and must be read, pinned, and reviewed first.
- Do not import OpenClaw macOS-local skills into Deneb by default; Deneb's main
  surface is Android native plus the gateway host.
- Do not inject plaintext env/API keys through skill metadata. Prefer existing
  Deneb secret-reference patterns.
- Do not add a new runtime TaskFlow database until existing session/cron/agentlog
  state proves a concrete gap.
