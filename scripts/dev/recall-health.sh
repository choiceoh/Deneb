#!/usr/bin/env bash
# recall-health.sh — recall's evaluation-loop health, against the LIVE wiki.
#
# recall-metric.sh scores a synthetic corpus; this scores the REAL one: retrieval
# quality on the hand-curated gold set, PLUS the two loop-closing signals recall
# lacked — production ledger utility (what recall actually surfaced in real turns)
# and gold-set coverage of the live project roster (the eval-completeness gap that
# grows as the dreamer adds pages). Advisory, operator-run: it needs the embedding
# server and a copy of the prod wiki, so it is NOT a CI gate.
#
# recall-bench MUTATES its --wiki tree (index reconcile + semantic warm), so this
# always runs against a throwaway COPY — production stays untouched. Pass
# --emit-gold to also print deterministic gold candidates for uncovered projects.
set -euo pipefail
cd "$(dirname "$0")/../../gateway-go"

WIKI_SRC="${DENEB_WIKI_DIR:-$HOME/.deneb/wiki}"
DIARY_SRC="${DENEB_DIARY_DIR:-$HOME/.deneb/diary}"
GOLD="${DENEB_WIKI_GOLD:-$HOME/.deneb/wiki-qa-gold.jsonl}"

if [[ ! -d "$WIKI_SRC" ]]; then
    echo "recall-health: wiki not found at $WIKI_SRC (set DENEB_WIKI_DIR)" >&2
    exit 1
fi

scratch="$(mktemp -d "${TMPDIR:-/tmp}/recall-health.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
cp -a "$WIKI_SRC" "$scratch/wiki"
[[ -d "$DIARY_SRC" ]] && cp -a "$DIARY_SRC" "$scratch/diary" || mkdir -p "$scratch/diary"

go run ./cmd/recall-bench \
    --wiki "$scratch/wiki" --diary "$scratch/diary" \
    --gold "$GOLD" --health "$@"

# Optional repair worklist: re-run the analysis-path gold set verbosely and
# classify the misses into wiki repair categories (thin folder / sibling
# overlap / vocab gap — scripts/audit/wiki_repair_worklist.py). Advisory,
# enabled by RECALL_WORKLIST=1 (the nightly job sets it).
if [[ "${RECALL_WORKLIST:-}" == "1" ]]; then
    XL_GOLD="${DENEB_WIKI_GOLD_XL:-$HOME/.deneb/wiki-qa-gold-analysis-xl.jsonl}"
    if [[ -f "$XL_GOLD" ]]; then
        echo
        echo "== wiki repair worklist (analysis-xl misses) =="
        go run ./cmd/recall-bench \
            --wiki "$scratch/wiki" --diary "$scratch/diary" \
            --gold "$XL_GOLD" -v 2>/dev/null | grep "✗" > "$scratch/misses.txt" || true
        python3 ../scripts/audit/wiki_repair_worklist.py \
            --wiki "$scratch/wiki" --gold "$XL_GOLD" --misses "$scratch/misses.txt"
    else
        echo "recall-health: analysis-xl gold not found ($XL_GOLD) — worklist skipped" >&2
    fi
fi
