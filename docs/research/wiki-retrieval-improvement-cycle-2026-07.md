# Wiki retrieval improvement cycle — findings (2026-07-19/20)

> Snapshot of a multi-day investigation into improving wiki recall/ranking, run
> almost entirely by **measure-first + held-out/live validation**. Records what
> was tried, what shipped, and — more usefully — what was *rejected with data*,
> so the next cycle doesn't re-run dead ends. Numbers are from `cmd/recall-bench`
> (offline, prod wiki copy, nemotron :8002 embedder, production fusion weights
> `bm25:sem:graph = 1:10:3`) and from **live** `miniapp.memory.search` RPC.
>
> Not a current-state doc (see `docs/research/README.md`). The single source of
> truth for what is wired now is the module `CLAUDE.md`s + `docs/agent-rules/`.

## TL;DR

- **Ranking is not the bottleneck; retrieval recall is.** A cross-encoder
  reranker landed (#3992) and helps single-fact P@1 by a real but small +3.4pp,
  but on realistic multi-topic queries it moves recall@8 by **0 pp** (live-
  confirmed) — reranking only reorders the candidates fusion already returned; it
  cannot rescue a page fusion never retrieved.
- **The only lever that improved recall was LLM query expansion** (a tiny model
  bridging query vocabulary to the wiki's), but the gain is modest and
  high-variance on the current gold (+3–5pp robust, +10pp on a lucky prompt/seed)
  — not yet confidently wireable.
- **Everything else was rejected with data**: per-query-type fusion routing,
  pruning (xProvence), naive multi-query, candidate-window expansion, and
  swapping the reranker model.

## What shipped

| PR | What | Result |
|----|------|--------|
| #3982 | recall-bench `--by-category` + `--holdout` | diagnosis tooling |
| #3987 | recall-bench `--dump-signals` (per-mode rankings for offline re-fusion) | tooling |
| #3992 | **cross-encoder rerank wired live** — xProvence sidecar (:8004), page-head docs, `DENEB_RERANK_FORCE` | single-fact P@1 83.7→87.1 |
| #3995 | sidecar-models.md rerank row | docs |
| #4001 | recall-bench `--content` (must_contain hit — robust to folder renames) | tooling |
| — | Plaud meeting→wiki recall gold, 42 cases (`~/.deneb/wiki-qa-gold-meeting.jsonl`) | hard eval |

## Rejected with data

### Per-query-type fusion routing — REJECTED (held-out regression)
Per-category diagnostics showed value/identity lookups (거래/인물/집계/관계)
score far higher under BM25 than the sem-dominant global weight (거래 P@1 74 vs
48). But **every router built to exploit it regressed held-out**: a keyword
classifier −18.5pp, a signal-confidence router −3.7pp (both train-positive, test-
negative — textbook overfitting on a 101-case gold). The apparent per-category
headroom is a full-set fitting artifact; the global 1:10:3 is near-optimal for
the available cheap signal. **The binding constraint is validation data, not the
mechanism.**

### Reranker model swap — NO WINNER on our data
Six rerankers compared on identical candidates (xProvence / bge-reranker-v2-m3 /
jina-reranker-v3 / Qwen3-Reranker-0.6B / gte-multilingual / nemotron-rerank-1b):

- On the (saturated) single-fact gold they cluster inside noise (all ~93% content
  P@1) — the bench can't tell them apart.
- On the **hard meeting-recall** set they finally separate: nemotron-rerank-1b
  edges the field (recall@8 60.7 vs xProvence 60.3), and gte — a *leader* on the
  saturated set — comes **last** here. Lesson: a saturated bench can rank models
  backwards.
- But nemotron's edge (+0.4–0.6pp) costs **2.5× latency** (~325ms vs ~130ms; bge
  ~95ms). Not worth swapping. **Keep xProvence**; if the CC-BY-NC license ever
  matters, **bge** (Apache, fastest) is the pragmatic Apache pick — not nemotron.

### xProvence context pruning — REJECTED
Using xProvence's sentence pruning to shrink recall-note tokens: measured worse
answer-token retention than the current query-match snippet (81.5% vs 78.7%) and
the token saving is negligible (recall block already capped). Pruning picks
topically-relevant sentences but drops the numeric/answer sentence.

### Reranking does not help recall (live-confirmed)
Live `memory.search` rerank ON vs OFF on the meeting gold: **identical** recall@8
(all 58.3%, multigold 48.1%). Reranking reorders the top-10; it never adds a
page fusion missed. This is why the #4004 reranker fan-out to mail/workfeed/
codesearch/fetch surfaces (other agents) is unlikely to move recall there either
— and those surfaces were wired without a quality bench (only wiring tests).

### Naive multi-query — REJECTED (hurts, offline + live)
Fragmenting a query into signal terms and unioning (both an offline best-rank
merge and the production `SearchPlan` clause fusion, tested **live**) is worse at
**every** K — recall@8 multigold 48→39, recall@40 68.6→42.3. Splitting
"강진 90MW 도급계약 독소조항" into `[강진][90MW][도급계약][독소조항]` weakens each
sub-query below the coherent whole. Coverage does **not** improve; it degrades.

### Rerank candidate-window expansion (10→30) — marginal, noisy
The recall curve shows ~8pp of gold at fusion rank 9–20, so widening the
reranked window should help. Measured gain is small and non-monotonic (+2–4pp
recall@8 with a dip at 20) because the imperfect reranker also *demotes* correct
top-8 pages when given a wider window. Kept as an env knob experiment
(`DENEB_RERANK_CANDIDATES`), not landed.

## The one lever that worked: LLM query expansion

For each meeting query, a **tiny model** (deepseek-v4-flash via wormhole) emits
diverse retrieval angles that bridge the query's conversational vocabulary to the
wiki's (e.g. "독소조항" → "위약벌·지체상금·PF·대주단"; "화신" → "화신이엔지 LOA·
변압기 GIS 승계"); the sub-queries run through the real `memory.search` and are
**RRF-merged**. Unlike naive fragmentation, semantic expansion + RRF does not
flood top-K.

Measured live on the 26 multigold meeting cases (recall@8):

| | multigold | all(42) |
|---|---|---|
| original query only | 48.1% | 58.3% |
| + expansion (RRF), first run | **58.3%** | 60.3% |
| + expansion (RRF), tuning re-run | 51.3% | 60.3% |
| expansion only (drop original) | 53.2% | 59.1% |
| + expansion **+ HyDE** | 50.0% | — |

Honest caveats:
- **High variance.** The same technique scored 58.3% then 51.3% across runs —
  LLM nondeterminism + a 26-case set. The robust estimate is **+3–5pp**; the
  first-run +10pp was partly prompt/seed luck.
- **HyDE hurt** (a hypothetical page pulls in off-topic neighbors and demotes
  gold) — expansion queries only.
- Over-weighting the original query in RRF hurt (46.2%); on hard meeting queries
  the original is *less* reliable than the expansions.
- Cost: one tiny-LLM call (~hundreds of ms) + N extra searches per recall turn.

**Verdict:** the only positive recall lever, but modest and not yet confidently
wireable. Needs a larger meeting gold (100+) to nail the magnitude and stabilize
the prompt before wiring the expansion into the recall preflight's `searchQueries`.

## Methodology notes worth keeping

- **Path-based gold rots.** The wiki restructured project folders to code IDs
  (`대한전선-당진` → `pl2-tha-epc-001`) mid-investigation, collapsing path-based
  P@1 to 64% while true content P@1 was 92.7%. `recall-bench --content` (score a
  hit when the page body holds the answer tokens) is rename-robust and is now the
  right default for retrieval measurement.
- **Saturation hides signal.** The single-fact gold (P@1 ~93%) could not
  distinguish rerankers or reveal the recall gap. Only the hard meeting set
  (recall@8 ~50%) exposed both. When a bench stops moving, the bench is the
  problem — build a harder one.
- **Offline bench was faithful.** Live `memory.search` reproduced the offline
  single-query numbers exactly (58.3 / 48.1), validating the offline harness for
  relative comparisons.
- Freeze the wiki snapshot before dump+score — a live wiki drifts (dreamer / mail
  analysis rename pages) and desyncs dump paths from disk reads.

## Where a future cycle should start

1. **Grow the meeting/multi-topic gold to 100+** — every conclusion above is
   bounded by small-sample variance. This is the highest-leverage next step.
2. If the recall gap still matters after (1), wire **tiny-model query expansion +
   RRF** into the recall preflight and A/B it live. Skip HyDE.
3. Reranking, fusion-weight routing, model swaps, pruning, naive multi-query:
   **do not re-open** without new data — all measured negative or noise here.
4. Ask first whether recall@8 ~50% actually degrades answer quality: recall
   injects the whole top-8 and the model synthesizes, so a moderate recall gap
   may not be user-visible. Measure downstream answer quality before more
   retrieval investment.
