#!/usr/bin/env bash
# ci-check.sh — single pre-push gate that mirrors what CI enforces.
#
# Runs every fast CI gate in one go and prints a per-gate PASS/FAIL summary with
# offender detail for anything that fails. Unlike `make check` (which stops at the
# first failed target), this keeps going so a single run surfaces *everything*
# that would fail CI — closing the "fix one, push, watch CI find the next" loop
# that bit us when a gofmt-only failure slipped through partial local checks.
#
# Gates (each is the make target of the same name):
#   Go      generate-check  go-fmt  go-vet  go-lint  go-test
#   Kotlin  kotlin-spotless  kotlin-detekt
#
# The Go and Kotlin lanes run in parallel (gradle JVM startup is the long pole),
# so wall-clock is roughly max(Go suite, Kotlin suite), not their sum.
#
# This mirrors CI's *fast* gates only — no -race, coverage threshold, or
# integration-tagged tests. Those stay in CI (run them locally via go-test flags
# if needed). Driven by `make ci`; can also be run directly.
#
# Usage:
#   scripts/dev/ci-check.sh            # all gates (Go + Kotlin)
#   scripts/dev/ci-check.sh --go       # Go gates only
#   scripts/dev/ci-check.sh --kotlin   # Kotlin gates only
#
# Exit status: 0 if every run gate passed, 1 if any failed (or preflight failed).

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# Mirror the Makefile PATH so golangci-lint (and other ~/go/bin tools) resolve
# even when this script is invoked directly rather than via make.
export PATH="$HOME/go/bin:$PATH"

# Android SDK for the Kotlin gradle gate; mirrors the Makefile / scripts default.
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"

# --- Gate definitions (gate name == make target) -----------------------------
GO_GATES=(generate-check go-fmt go-vet go-lint go-test)
KOTLIN_GATES=(kotlin-spotless kotlin-detekt)

# --- Args --------------------------------------------------------------------
RUN_GO=true
RUN_KOTLIN=true
case "${1:-}" in
  --go)      RUN_KOTLIN=false ;;
  --kotlin)  RUN_GO=false ;;
  --all|"")  ;;
  -h|--help)
    sed -n '2,33p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
    exit 0 ;;
  *)
    echo "ci-check: unknown argument '$1' (use --go, --kotlin, or no argument)" >&2
    exit 2 ;;
esac

# --- Colors (only when stdout is a terminal) ---------------------------------
if [ -t 1 ]; then
  BOLD=$(tput bold 2>/dev/null || echo); DIM=$(tput dim 2>/dev/null || echo)
  RED=$(tput setaf 1 2>/dev/null || echo); GREEN=$(tput setaf 2 2>/dev/null || echo)
  YELLOW=$(tput setaf 3 2>/dev/null || echo); RESET=$(tput sgr0 2>/dev/null || echo)
else
  BOLD=; DIM=; RED=; GREEN=; YELLOW=; RESET=
fi

# --- Preflight: the Kotlin lane needs the Android SDK ------------------------
if $RUN_KOTLIN && [ ! -d "$ANDROID_HOME" ]; then
  echo "${RED}${BOLD}ci-check: ANDROID_HOME not found:${RESET} $ANDROID_HOME" >&2
  echo "  The Kotlin gate (spotless/detekt) needs the Android SDK + a JDK." >&2
  echo "  Install it, set ANDROID_HOME, or run Go gates only:  make ci ARGS=--go" >&2
  exit 1
fi

LOGDIR="$(mktemp -d "${TMPDIR:-/tmp}/deneb-ci-check.XXXXXX")"

now_ms() { date +%s%3N; }
fmt_dur() { printf '%d.%01ds' "$(( $1 / 1000 ))" "$(( ($1 % 1000) / 100 ))"; }

# run_gate <gate-name>: run `make <gate-name>`, capture output + timing + rc,
# and print a one-line completion marker (lanes run in parallel, so these appear
# in completion order — live feedback while the slow gradle gate churns).
run_gate() {
  local name="$1"
  local log="$LOGDIR/$name.log"
  local start end dur rc
  start=$(now_ms)
  make "$name" >"$log" 2>&1
  rc=$?
  end=$(now_ms); dur=$(( end - start ))
  printf '%s %s\n' "$rc" "$dur" > "$LOGDIR/$name.meta"
  if [ "$rc" -eq 0 ]; then
    printf '  %s✓%s %-16s %s%s%s\n' "$GREEN" "$RESET" "$name" "$DIM" "$(fmt_dur "$dur")" "$RESET"
  else
    printf '  %s✗%s %-16s %s%s%s\n' "$RED" "$RESET" "$name" "$DIM" "$(fmt_dur "$dur")" "$RESET"
  fi
}

go_lane()     { local g; for g in "${GO_GATES[@]}";     do run_gate "$g"; done; }
kotlin_lane() { local g; for g in "${KOTLIN_GATES[@]}"; do run_gate "$g"; done; }

# --- Run lanes in parallel ---------------------------------------------------
SELECTED=()
$RUN_GO     && SELECTED+=("${GO_GATES[@]}")
$RUN_KOTLIN && SELECTED+=("${KOTLIN_GATES[@]}")

echo "${BOLD}make ci${RESET} — CI gate mirror  ${DIM}(${#SELECTED[@]} gates$( $RUN_GO && $RUN_KOTLIN && printf ', Go ∥ Kotlin in parallel'))${RESET}"
echo

wall_start=$(now_ms)
pids=()
$RUN_GO     && { go_lane &     pids+=($!); }
$RUN_KOTLIN && { kotlin_lane & pids+=($!); }
for p in "${pids[@]}"; do wait "$p"; done
wall_ms=$(( $(now_ms) - wall_start ))

# --- Summary -----------------------------------------------------------------
echo
echo "${BOLD}Summary${RESET}"
passed=0; failed=0; failed_names=()
for g in "${SELECTED[@]}"; do
  read -r rc dur < "$LOGDIR/$g.meta"
  if [ "$rc" -eq 0 ]; then
    printf '  %s%-16s PASS%s  %s%s%s\n' "$GREEN" "$g" "$RESET" "$DIM" "$(fmt_dur "$dur")" "$RESET"
    passed=$((passed + 1))
  else
    printf '  %s%-16s FAIL%s  %s%s%s\n' "$RED" "$g" "$RESET" "$DIM" "$(fmt_dur "$dur")" "$RESET"
    failed=$((failed + 1)); failed_names+=("$g")
  fi
done
echo "  ${DIM}$(printf '%.0s-' {1..38})${RESET}"
printf '  %d passed, %d failed  %s·  wall %s%s\n' \
  "$passed" "$failed" "$DIM" "$(fmt_dur "$wall_ms")" "$RESET"

# --- Offender detail for failures --------------------------------------------
if [ "$failed" -gt 0 ]; then
  for g in "${failed_names[@]}"; do
    echo
    echo "${RED}${BOLD}▼ $g${RESET} ${DIM}(make $g)${RESET}"
    local_lines=$(wc -l < "$LOGDIR/$g.log" 2>/dev/null || echo 0)
    if [ "${local_lines:-0}" -gt 200 ]; then
      echo "  ${DIM}(last 200 of $local_lines lines — full log: $LOGDIR/$g.log)${RESET}"
      tail -n 200 "$LOGDIR/$g.log" | sed 's/^/  /'
    else
      sed 's/^/  /' "$LOGDIR/$g.log"
    fi
  done
  echo
  echo "${RED}${BOLD}make ci FAILED${RESET} — $failed gate(s) above would fail CI. Logs: $LOGDIR"
  exit 1
fi

# All green — clean up the scratch logs.
rm -rf "$LOGDIR"
echo
echo "${GREEN}${BOLD}make ci PASSED${RESET} — all gates green; safe to push."
