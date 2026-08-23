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

# The srv4 self-hosted runner's PATH has no Go toolchain (it lives in
# ~/go-sdk/go/bin), so every scheduled run of this script died on "go: command
# not found" — and because the nightly job pipes us through `tee`, the pipeline
# returned tee's success and the job stayed green. The trend line this script
# exists to produce was empty from its first run (2026-07-20) to 2026-08-23.
for candidate in "$HOME/go-sdk/go/bin" "/usr/local/go/bin"; do
    if [[ -x "$candidate/go" ]]; then
        export PATH="$candidate:$PATH"
        break
    fi
done
if ! command -v go >/dev/null 2>&1; then
    echo "recall-health: go toolchain not found (checked PATH, ~/go-sdk/go/bin, /usr/local/go/bin)" >&2
    exit 1
fi

WIKI_SRC="${DENEB_WIKI_DIR:-$HOME/.deneb/wiki}"
DIARY_SRC="${DENEB_DIARY_DIR:-$HOME/.deneb/diary}"
GOLD="${DENEB_WIKI_GOLD:-$HOME/.deneb/wiki-qa-gold.jsonl}"

# Production-parity retrieval defaults, in ONE place. Callers used to hardcode
# these (the nightly job carried 1:10:3 with no reranker); production moved to
# 1:3:4 + xprovence FORCE=on on 2026-07-20 and the copies drifted, so the nightly
# trend line was scoring a configuration that no longer exists — measured 2026-07-25
# at p@1 41.3% vs 53.8% at true parity, a 12.5pp understatement.
#
# Source of truth: the gateway's systemd drop-in (~/.config/systemd/user/
# deneb-gateway.service.d/nemotron-embed.conf). Keep these in step with it; every
# value stays overridable from the environment for sweeps.
export DENEB_EMBEDDING_URL="${DENEB_EMBEDDING_URL:-http://127.0.0.1:8002}"
export DENEB_WIKI_SEM_FLOOR="${DENEB_WIKI_SEM_FLOOR:-0.44}"
export DENEB_WIKI_RRF_SEM_WEIGHT="${DENEB_WIKI_RRF_SEM_WEIGHT:-3}"
export DENEB_WIKI_RRF_GRAPH_WEIGHT="${DENEB_WIKI_RRF_GRAPH_WEIGHT:-4}"
export DENEB_RERANK_FORCE="${DENEB_RERANK_FORCE:-on}"
export DENEB_RERANK_URL="${DENEB_RERANK_URL:-http://127.0.0.1:8004}"
export DENEB_RERANK_MODEL="${DENEB_RERANK_MODEL:-xprovence-bgem3-v2}"

# Vocabulary-gap expansion backfill went live in production on 2026-08-08
# (#4473; gateway drop-in sets DENEB_WIKI_QUERY_EXPANSION=backfill), so parity
# needs the bench arm too — the exact drift class this block exists to prevent.
# The bench expander mirrors the production tiny role (qwen via wormhole);
# without the wormhole token the expander calls fail per-query and the run is
# effectively expansion-off, so warn loudly rather than measure silently.
export DENEB_WIKI_QUERY_EXPANSION="${DENEB_WIKI_QUERY_EXPANSION:-backfill}"
export DENEB_EXPANSION_MODEL="${DENEB_EXPANSION_MODEL:-qwen3.6-35b-a3b}"
if [[ -z "${DENEB_EXPANSION_API_KEY:-}" && -f "$HOME/.wormhole/config.json" ]]; then
    DENEB_EXPANSION_API_KEY="$(python3 -c 'import json,os;print(os.path.expandvars(json.load(open(os.path.expanduser("~/.wormhole/config.json")))["token"]))' 2>/dev/null || true)"
    export DENEB_EXPANSION_API_KEY
fi
if [[ "$DENEB_WIKI_QUERY_EXPANSION" == "backfill" && -z "${DENEB_EXPANSION_API_KEY:-}" ]]; then
    echo "recall-health: WARNING — expansion=backfill but no wormhole token; expander calls will fail and the run scores expansion-OFF (not production parity)" >&2
fi

echo "recall-health: fusion 1:${DENEB_WIKI_RRF_SEM_WEIGHT}:${DENEB_WIKI_RRF_GRAPH_WEIGHT}" \
     "· rerank=${DENEB_RERANK_FORCE} (${DENEB_RERANK_MODEL})" \
     "· embed=${DENEB_EMBEDDING_URL} · floor=${DENEB_WIKI_SEM_FLOOR}" \
     "· expansion=${DENEB_WIKI_QUERY_EXPANSION} (${DENEB_EXPANSION_MODEL})"

if [[ ! -d "$WIKI_SRC" ]]; then
    echo "recall-health: wiki not found at $WIKI_SRC (set DENEB_WIKI_DIR)" >&2
    exit 1
fi

scratch="$(mktemp -d "${TMPDIR:-/tmp}/recall-health.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT
cp -a "$WIKI_SRC" "$scratch/wiki"
[[ -d "$DIARY_SRC" ]] && cp -a "$DIARY_SRC" "$scratch/diary" || mkdir -p "$scratch/diary"

bench_out="$scratch/bench.txt"
go run ./cmd/recall-bench \
    --wiki "$scratch/wiki" --diary "$scratch/diary" \
    --gold "$GOLD" --health "$@" | tee "$bench_out"

# Self-verification: a run that produces no score did not measure anything, and
# an advisory job that reports "green" for a run that never happened is worse
# than a red one — that is exactly how 45 consecutive nightly runs hid a broken
# harness. Fail loudly instead.
if ! grep -q "RECALL_HEALTH score=" "$bench_out"; then
    echo "recall-health: no RECALL_HEALTH line in the bench output — the run did not measure anything" >&2
    exit 1
fi

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
