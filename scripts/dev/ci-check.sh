#!/usr/bin/env bash
# ci-check.sh — local gate that mirrors what CI enforces.
#
# Runs every fast CI gate and prints a per-gate PASS/FAIL summary with offender
# detail for anything that fails. Unlike `make check` (which stops at the first
# failed target), this keeps going so a single run surfaces *everything* that
# would fail CI — closing the "fix one, push, watch CI find the next" loop that
# bit us when a gofmt-only failure slipped through partial local checks.
#
# Gates (each is the make target of the same name):
#   Go      generate-check  go-fmt  go-vet  go-lint  go-test
#   Kotlin  kotlin-spotless  kotlin-detekt
#
# The Go and Kotlin lanes run in parallel (gradle JVM startup is the long pole),
# so wall-clock is roughly max(Go suite, Kotlin suite), not their sum.
#
# Two modes:
#   (default)  full gate — every lane, faithful to CI. Run before pushing.
#   --fast     inner-loop gate — only the side that changed vs origin/main
#              (skip the Go lane if gateway-go/ is untouched, the Kotlin lane if
#              client-android/ is untouched) and use the Go test cache. Much
#              faster on single-side edits; NOT authoritative — run the full
#              `make ci` before the actual push.
#
# Mirrors CI's *fast* gates only — no -race, coverage threshold, or
# integration-tagged tests. Driven by `make ci` / `make ci/fast`.
#
# Usage:
#   scripts/dev/ci-check.sh            # full gate (Go + Kotlin)
#   scripts/dev/ci-check.sh --go       # Go gates only
#   scripts/dev/ci-check.sh --kotlin   # Kotlin gates only
#   scripts/dev/ci-check.sh --fast     # changed-side gates only, cached tests
#
# Exit status: 0 if every run gate passed (or nothing changed in --fast), 1 if
# any failed (or preflight failed).

set -uo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$REPO_ROOT"

# Mirror the Makefile PATH so golangci-lint (and other ~/go/bin tools) resolve
# even when this script is invoked directly rather than via make.
export PATH="$HOME/go/bin:$PATH"

# Android SDK for the Kotlin gradle gate; mirrors the Makefile / scripts default.
export ANDROID_HOME="${ANDROID_HOME:-$HOME/android-sdk}"

# Branch this fast-mode diffs against to decide which lanes changed.
BASE_REF="${CI_CHECK_BASE:-origin/main}"

# --- Gate definitions (gate name == make target) -----------------------------
GO_GATES=(generate-check go-fmt go-vet go-lint go-test)
KOTLIN_GATES=(kotlin-spotless kotlin-detekt)

# --- Args --------------------------------------------------------------------
RUN_GO=true
RUN_KOTLIN=true
FAST=false
case "${1:-}" in
  --go)      RUN_KOTLIN=false ;;
  --kotlin)  RUN_GO=false ;;
  --fast)    FAST=true ;;
  --all|"")  ;;
  -h|--help)
    echo "ci-check.sh — local mirror of CI's fast gates"
    echo
    echo "Usage:"
    echo "  scripts/dev/ci-check.sh           full gate (Go + Kotlin)"
    echo "  scripts/dev/ci-check.sh --go      Go gates only"
    echo "  scripts/dev/ci-check.sh --kotlin  Kotlin gates only"
    echo "  scripts/dev/ci-check.sh --fast    changed-side gates only, cached tests"
    echo
    echo "Via make:  make ci   |   make ci ARGS=--go   |   make ci/fast"
    exit 0 ;;
  *)
    echo "ci-check: unknown argument '$1' (use --go, --kotlin, --fast, or no argument)" >&2
    exit 2 ;;
esac

LABEL="ci"; $FAST && LABEL="ci/fast"

# --- Colors (only when stdout is a terminal) ---------------------------------
if [ -t 1 ]; then
  BOLD=$(tput bold 2>/dev/null || echo); DIM=$(tput dim 2>/dev/null || echo)
  RED=$(tput setaf 1 2>/dev/null || echo); GREEN=$(tput setaf 2 2>/dev/null || echo)
  YELLOW=$(tput setaf 3 2>/dev/null || echo); RESET=$(tput sgr0 2>/dev/null || echo)
else
  BOLD=; DIM=; RED=; GREEN=; YELLOW=; RESET=
fi

# --- Fast mode: path-gate lanes by what changed vs BASE_REF ------------------
# Mirrors CI's own path-gating (kotlin-lint runs only on client-android/**), so
# this drops untouched lanes rather than weakening any gate that does run.
if $FAST; then
  base_sha="$(git merge-base HEAD "$BASE_REF" 2>/dev/null || true)"
  if [ -z "$base_sha" ]; then
    echo "${YELLOW}ci-check --fast:${RESET} can't resolve '$BASE_REF' merge-base — running all lanes." >&2
  else
    changed="$(
      { git diff --name-only "$base_sha" HEAD          # committed on this branch
        git diff --name-only                           # unstaged
        git diff --name-only --cached                  # staged
        git ls-files --others --exclude-standard        # untracked
      } 2>/dev/null | sort -u
    )"
    grep -q '^gateway-go/'     <<<"$changed" || RUN_GO=false
    grep -q '^client-android/' <<<"$changed" || RUN_KOTLIN=false
  fi
fi

# Fast mode with nothing relevant changed: nothing to gate.
if $FAST && ! $RUN_GO && ! $RUN_KOTLIN; then
  echo "${GREEN}${BOLD}make ci/fast${RESET} — no Go or Kotlin changes vs ${BASE_REF}; nothing to gate."
  echo "${DIM}(run the full ${RESET}${BOLD}make ci${RESET}${DIM} before pushing.)${RESET}"
  exit 0
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

# run_gate <gate-name>: run the gate's make target, capture output + timing + rc,
# and print a one-line completion marker (lanes run in parallel, so these appear
# in completion order — live feedback while the slow gradle gate churns).
run_gate() {
  local name="$1"
  local target="$name"
  # Fast mode swaps in the cached test target so unchanged packages don't rerun.
  if $FAST && [ "$name" = go-test ]; then target=go-test-cached; fi
  local log="$LOGDIR/$name.log"
  local start end dur rc
  start=$(now_ms)
  make "$target" >"$log" 2>&1
  rc=$?
  end=$(now_ms); dur=$(( end - start ))
  printf '%s %s %s\n' "$rc" "$dur" "$target" > "$LOGDIR/$name.meta"
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

if $FAST; then
  desc="vs ${BASE_REF} → Go:$($RUN_GO && echo run || echo skip)  Kotlin:$($RUN_KOTLIN && echo run || echo skip); tests cached"
else
  desc="${#SELECTED[@]} gates"
  $RUN_GO && $RUN_KOTLIN && desc="$desc, Go ∥ Kotlin in parallel"
fi
echo "${BOLD}make ${LABEL}${RESET} — $( $FAST && echo 'incremental gate' || echo 'CI gate mirror' )  ${DIM}(${desc})${RESET}"
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
  read -r rc dur _target < "$LOGDIR/$g.meta"
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
    read -r _rc _dur target < "$LOGDIR/$g.meta"
    echo
    echo "${RED}${BOLD}▼ $g${RESET} ${DIM}(make $target)${RESET}"
    lines=$(wc -l < "$LOGDIR/$g.log" 2>/dev/null || echo 0)
    if [ "${lines:-0}" -gt 200 ]; then
      echo "  ${DIM}(last 200 of $lines lines — full log: $LOGDIR/$g.log)${RESET}"
      tail -n 200 "$LOGDIR/$g.log" | sed 's/^/  /'
    else
      sed 's/^/  /' "$LOGDIR/$g.log"
    fi
  done
  echo
  echo "${RED}${BOLD}make ${LABEL} FAILED${RESET} — $failed gate(s) above would fail CI. Logs: $LOGDIR"
  exit 1
fi

# All green — clean up the scratch logs.
rm -rf "$LOGDIR"
echo
if $FAST; then
  echo "${GREEN}${BOLD}make ci/fast PASSED${RESET} — changed-side gates green. ${DIM}Run the full ${RESET}${BOLD}make ci${RESET}${DIM} before pushing.${RESET}"
else
  echo "${GREEN}${BOLD}make ci PASSED${RESET} — all gates green; safe to push."
fi
