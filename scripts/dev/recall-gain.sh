#!/usr/bin/env bash
# recall-gain.sh — does the curated wiki layer EARN its recall weight?
#
# CL-Bench (arXiv:2606.05661) measures a memory layer by its *gain* — the lift
# over running the same system without it — not its raw score. Deneb's diary +
# polaris are raw + query-time retrieval (the pattern CL-Bench favors); the WIKI
# is the one eagerly-curated layer. This wrapper runs recall_gain_test.go, which
# seeds the SAME facts into both diary and wiki, then scores recall with the wiki
# ON vs OFF:
#
#     gain = hit_rate(wiki+diary) - hit_rate(diary-only)
#
# A wiki that only restates what the raw diary already surfaces is redundancy
# (gain 0) — what retired Hindsight turned out to be. Positive gain shows up in
# terminology normalization (paraphrased query matches curated title/summary) and
# stale-belief disambiguation (the supersede marker flags a revised-away value and
# ranks the corrected one first, where the raw diary keeps both with no ordering
# signal). Pure-Go, no GPU/LLM needed; lexical path only, so this is the wiki's
# LEXICAL gain floor (semantic gain needs a GPU).
set -euo pipefail
cd "$(dirname "$0")/../../gateway-go"

# Capture go test's exit status separately — do NOT let `|| true` swallow it, or a
# failing stale-belief guard would still print the positive gain line and the
# iterate loop (metric>0 == pass) would count a broken run as a success.
out=$(go test ./internal/pipeline/chat/ \
    -run 'TestRecallWikiGain|TestRecallStaleBeliefGuard' -count=1 -v 2>&1) && rc=0 || rc=$?

gain_line=$(grep -oE 'RECALL_GAIN both=[0-9]+ diaryonly=[0-9]+ wikionly=[0-9]+ gain=-?[0-9]+ total=[0-9]+' <<<"$out" | tail -1 || true)
stale_line=$(grep -oE 'RECALL_STALELEAK [^[:cntrl:]]*new_surfaced=(true|false)' <<<"$out" | tail -1 || true)

[[ -n "$gain_line" ]] && echo "$gain_line"
[[ -n "$stale_line" ]] && echo "$stale_line"

# Any test failure (e.g. the stale-belief guard) ⇒ not a healthy run: report 0.
if [[ $rc -ne 0 ]]; then
    echo "metric_value=0"
    echo "error: recall gain tests failed (go test exit $rc) — not reporting a positive metric" >&2
    grep -E '^\s*--- FAIL|^FAIL' <<<"$out" >&2 || true
    exit 1
fi

if [[ -z "$gain_line" ]]; then
    echo "metric_value=0"
    echo "error: bench did not produce RECALL_GAIN (build failure?)" >&2
    echo "$out" >&2
    exit 1
fi

gain=${gain_line##*gain=}
gain=${gain%% *}
echo "metric_value=$gain"
