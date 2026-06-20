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
# stale-belief disambiguation (supersede marker demotes a revised-away value the
# raw diary keeps with no ordering signal). Pure-Go, no GPU/LLM needed; lexical
# path only, so this is the wiki's LEXICAL gain floor (semantic gain needs a GPU).
set -euo pipefail
cd "$(dirname "$0")/../../gateway-go"

out=$(go test ./internal/pipeline/chat/ \
    -run 'TestRecallWikiGain|TestRecallStaleBeliefGuard' -count=1 -v 2>&1 || true)

gain_line=$(grep -o 'RECALL_GAIN both=[0-9]* diaryonly=[0-9]* wikionly=[0-9]* gain=-\?[0-9]* total=[0-9]*' <<<"$out" | tail -1)
stale_line=$(grep -o 'RECALL_STALELEAK diaryonly=\(true\|false\) both=\(true\|false\) new_surfaced=\(true\|false\)' <<<"$out" | tail -1)

if [[ -z "$gain_line" ]]; then
    echo "metric_value=0"
    echo "error: bench did not produce RECALL_GAIN (build failure?)" >&2
    echo "$out" >&2
    exit 1
fi

echo "$gain_line"
[[ -n "$stale_line" ]] && echo "$stale_line"

gain=${gain_line##*gain=}
gain=${gain%% *}
echo "metric_value=$gain"
