#!/usr/bin/env bash
# Route an RSI Bench ratchet verdict to the operator.
#
# WHY: refresh-bench-snapshots.sh has run `--check` on every daily deep pass
# since the timer was installed, and it deliberately exits 1 on a breach so the
# oneshot goes `failed` instead of decaying quietly. But nothing ever READ that
# verdict. The report lands in ~/.deneb/data/rsi-bench-check-latest.txt, which
# no code opens and no human opens either (grep the tree: the only reference is
# the line that writes it), and `systemctl --user status` was the sole other
# trace. Result: 12 of 12 retained runs (2026-08-20 → 08-31) went red on
# process.anti-collapse and process.timescale-turn, and the first party to
# notice was an agent that was asked to look. A check whose verdict reaches
# nobody is not a gate — it is a log line.
#
# Mirrors the health-bench idiom in .github/workflows/nightly-drift.yml
# (report-health), because that one already works and the operator already
# reads it: ONE labelled issue, body refreshed in place with a streak counter
# instead of a fresh comment every night (a persistent breach is the NORMAL
# case here — identical daily comments bury the issue rather than inform), and
# auto-closed when the ratchet goes green. The issue's open/closed state IS the
# live ratchet state.
#
# Usage:
#   bench-ratchet-notify.sh breach   # OnFailure= path (deneb-bench-refresh-notify.service)
#   bench-ratchet-notify.sh green    # success path, called by refresh-bench-snapshots.sh
#
# Best-effort by construction: every path exits 0. This is a notification, not
# a gate — it must never turn a green refresh red, and it must never mask the
# breach exit code that refresh-bench-snapshots.sh already raises on its own.
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
STATE_DIR="${DENEB_STATE_DIR:-$HOME/.deneb}"
REPORT="$STATE_DIR/data/rsi-bench-check-latest.txt"
LABEL="rsi-bench"
TITLE="rsi bench: main 래칫 회귀"

mode="${1:-}"
if [[ "$mode" != "breach" && "$mode" != "green" ]]; then
  echo "usage: $(basename "$0") breach|green" >&2
  exit 0
fi

# gh reads the repo from the working tree's remote; anchor to the repo that
# ships this script so the unit's WorkingDirectory cannot silently retarget it.
cd "$REPO_DIR" || exit 0

if ! command -v gh >/dev/null 2>&1; then
  echo "bench-ratchet-notify: gh not installed — skipping ($mode)" >&2
  exit 0
fi
if ! gh auth status >/dev/null 2>&1; then
  echo "bench-ratchet-notify: gh not authenticated — skipping ($mode)" >&2
  exit 0
fi

existing="$(gh issue list --label "$LABEL" --state open --json number --jq '.[0].number // empty' 2>/dev/null)"

if [[ "$mode" == "green" ]]; then
  if [[ -z "$existing" ]]; then
    exit 0
  fi
  gh issue comment "$existing" \
    --body "RSI Bench 래칫이 그린으로 복귀 — 자동 종결. ($(date '+%Y-%m-%d %H:%M %Z'))" >/dev/null 2>&1
  gh issue close "$existing" >/dev/null 2>&1
  echo "bench-ratchet-notify: closed #$existing (ratchet green)"
  exit 0
fi

# --- breach ---------------------------------------------------------------
# OnFailure= fires for ANY failure of the refresh unit, not only a ratchet
# breach (a health-v3 crash and the 45min timeout land here too). Report what
# the file actually says rather than asserting a breach that may not have
# happened.
summary=""
if [[ -r "$REPORT" ]]; then
  summary="$(grep -E '^(REGRESSION|UNMEASURED|DENEB_RSI_BENCH):?' "$REPORT" 2>/dev/null)"
  if [[ -z "$summary" ]]; then
    summary="$(tail -n 20 "$REPORT" 2>/dev/null)"
  fi
else
  summary="(no report at $REPORT — the refresh failed before the RSI check ran)"
fi

body="RSI Bench 래칫이 baseline(\`scripts/audit/rsi-bench-baseline.json\`) 대비 회귀했습니다.
\`deneb-bench-refresh.service\` (매일 04:30) 가 스냅샷은 정상 기록하고 마지막에 exit 1 합니다 — 리프레시는 멈추지 않습니다.

\`\`\`
${summary}
\`\`\`

전체 리포트: \`${REPORT}\` · 재현: \`make rsi-bench-check\`

판독 순서 — \`REGRESSION\` 만 코드/루프 회귀입니다. \`UNMEASURED\` 는 표본이 굶어(\`resolved<3\`) 측정 불가라는 뜻이라 회귀가 아닙니다 (docs/agent-rules/rsi-bench.md).

**베이스라인 플로어를 낮춰 통과시키지 마세요 — 일방향 래칫이고, \`--migrate-rubric\` 은 회귀검사를 통째로 건너뜁니다.**"

if [[ -n "$existing" ]]; then
  prev="$(gh issue view "$existing" --json body --jq .body 2>/dev/null)"
  days="$(printf '%s' "$prev" | sed -n 's/^연속 회귀: \([0-9]\{1,\}\)일차.*/\1/p' | head -1)"
  days=$(( ${days:-1} + 1 ))
  if gh issue edit "$existing" --body "연속 회귀: ${days}일차

${body}" >/dev/null 2>&1; then
    echo "bench-ratchet-notify: refreshed #$existing (streak ${days}d)"
  else
    echo "bench-ratchet-notify: could not refresh #$existing" >&2
  fi
  exit 0
fi

gh label create "$LABEL" \
  --description "RSI Bench ratchet regressions on main" \
  --color 5319E7 --force >/dev/null 2>&1
if num="$(gh issue create --title "$TITLE" --label "$LABEL" --body "연속 회귀: 1일차

${body}" 2>&1)"; then
  echo "bench-ratchet-notify: opened ${num##*/}"
else
  echo "bench-ratchet-notify: could not open issue — $num" >&2
fi
exit 0
