package knowledge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// Router federates multiple knowledge backends under one surface. Created
// with the set of adapters available in the current deployment; missing
// backends are skipped silently so the router degrades gracefully when, for
// example, a backend is not configured.
type Router struct {
	adapters          []Adapter
	recallGuards      []RecallResultGuard
	writer            Writer // first writable adapter wins (today: wiki)
	facts             FactWriter
	factObserverMu    sync.RWMutex
	factMutationEvent FactMutationObserver
}

// New constructs a Router from the given adapters. nil entries are ignored so
// callers can pass conditional constructors without nil-checking each one.
func New(adapters ...Adapter) *Router {
	r := &Router{}
	for _, a := range adapters {
		if a == nil {
			continue
		}
		r.adapters = append(r.adapters, a)
		if guard, ok := a.(RecallResultGuard); ok {
			r.recallGuards = append(r.recallGuards, guard)
		}
		if w, ok := a.(Writer); ok && r.writer == nil {
			r.writer = w
		}
		if f, ok := a.(FactWriter); ok && r.facts == nil {
			r.facts = f
		}
	}
	return r
}

// Layers reports which backends this router will dispatch to.
func (r *Router) Layers() []Layer {
	out := make([]Layer, 0, len(r.adapters))
	for _, a := range r.adapters {
		out = append(out, a.Layer())
	}
	return out
}

// layerRecallQuota caps how many hits any single layer may contribute to a
// merged Recall, expressed as a fraction of limit (rounded up, min 1). Without
// it a layer whose score band sits higher than the others sweeps the whole
// result set and buries the rest — the failure mode that retired hindsight
// (its synthetic 0.60–0.92 band always lost to wiki/diary BM25, so it was
// either invisible or, when it surfaced, duplicative). The bands here differ
// too: the wiki adapter returns BM25-normalized 0–1 scores while the files
// adapter returns raw embedding cosine on the embedder's own band (BGE-M3
// packed hits into 0.73–0.86; Nemotron admits from 0.33 up), so a naive
// score-sort would mis-rank layers against each other on a mixed query. The quota guarantees each configured layer a share of the
// window regardless of its raw score scale; within the window, score still
// orders the rows. A single-layer router is unaffected (quota ≥ limit).
const layerRecallQuota = 0.6

// mergeRRFK is the Reciprocal Rank Fusion damping constant for cross-layer
// merge (same default as wiki search rrfK). Layers use incomparable raw score
// bands; RRF orders by per-layer rank only.
const mergeRRFK = 20.0

// Recall queries every adapter in parallel and merges the results. Within each
// layer hits are ordered by score; across layers a per-layer quota
// (layerRecallQuota) prevents one score band from monopolizing the merged
// window, then kept rows are fused by per-layer rank RRF (not raw score).
// Per-adapter errors are swallowed so a single flaky backend does not block
// the call; callers see the successful subset. Prefer RecallWithMeta when the
// caller needs degrade notes (e.g. files timeout).
func (r *Router) Recall(ctx context.Context, query string, limit int) []Result {
	hits, _ := r.RecallWithMeta(ctx, query, limit)
	return hits
}

// RecallWithMeta is Recall plus human-readable degrade notes (e.g. files-layer
// timeout). Notes are empty when every layer completed normally.
func (r *Router) RecallWithMeta(ctx context.Context, query string, limit int) ([]Result, []string) {
	packet := r.RecallPacket(ctx, query, limit, RecallOptions{})
	return packet.Results, packet.Notes
}

// RecallPacket plans, fans out, normalizes, and fuses one retrieval request.
// Existing Recall/RecallWithMeta callers retain their behavior through the
// wrappers above; new consumers get the typed plan and evidence provenance.
func (r *Router) RecallPacket(ctx context.Context, query string, limit int, options RecallOptions) EvidencePacket {
	if limit <= 0 {
		limit = 10
	}
	started := time.Now()
	retrievedAt := started.UnixMilli()
	plan := r.PlanRecall(query, limit, options)
	packet := EvidencePacket{Query: strings.TrimSpace(query), Plan: plan, RetrievedAt: retrievedAt}
	if len(plan.Sources) == 0 {
		packet.Notes = []string{"선택 조건에 맞는 지식 소스가 없음"}
		return packet
	}
	adapterByLayer := make(map[Layer]Adapter, len(r.adapters))
	for _, adapter := range r.adapters {
		adapterByLayer[adapter.Layer()] = adapter
	}
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		byHits  = make(map[Layer][]Result, len(plan.Sources))
		layerMs = make(map[Layer]int64, len(plan.Sources))
		notes   []string
	)
	for _, source := range plan.Sources {
		a := adapterByLayer[source.Source.Layer]
		if a == nil {
			continue
		}
		wg.Add(1)
		go func(a Adapter, source PlannedSource) {
			defer wg.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					mu.Lock()
					notes = append(notes, fmt.Sprintf("%s 소스 오류로 제외됨", source.Source.Name))
					mu.Unlock()
					slog.Warn("knowledge recall source panicked", "source", source.Source.Name, "panic", recovered)
				}
			}()
			layerStart := time.Now()
			hits, err := a.Recall(ctx, query, source.FetchLimit)
			elapsed := time.Since(layerStart).Milliseconds()
			mu.Lock()
			defer mu.Unlock()
			layerMs[a.Layer()] = elapsed
			if err == nil {
				for _, hit := range hits {
					if !inRecallScopes(hit.Ref, plan.Scopes) {
						continue
					}
					byHits[a.Layer()] = append(byHits[a.Layer()], normalizeEvidence(hit, source.Source, retrievedAt))
				}
				if _, ok := byHits[a.Layer()]; !ok {
					byHits[a.Layer()] = nil
				}
				return
			}
			if errors.Is(err, ErrFilesRecallTimeout) {
				notes = append(notes, "files 레이어 타임아웃 — 위키 결과만 포함")
				return
			}
			// Other adapter errors: swallow (same as before) so one flaky
			// backend cannot fail the whole recall.
		}(a, source)
	}
	wg.Wait()
	// Canonical lifecycle guards run after every source finishes but before
	// quotas and fusion. This prevents a corrected value from re-entering via a
	// non-wiki connector and prevents a now-superseded synthetic hit from
	// surviving a mutation that committed while retrieval was in flight.
	for _, guard := range r.recallGuards {
		for layer, hits := range byHits {
			byHits[layer] = guard.FilterRecallResults(hits)
		}
	}

	// Per-layer quota: at most quota hits from any one layer (a single-layer
	// router is unaffected because quota is computed against limit). Each layer's
	// own hits are ranked by score first so the quota keeps that layer's best.
	quota := int(float64(limit)*layerRecallQuota + 0.999)
	if quota < 1 {
		quota = 1
	}
	type rankedHit struct {
		res  Result
		rank int // 1-based within its layer after quota trim
	}
	var ranked []rankedHit
	contributingLayers := 0
	for _, hits := range byHits {
		if len(hits) > 0 {
			contributingLayers++
		}
	}
	for _, hits := range byHits {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		if contributingLayers > 1 && len(hits) > quota {
			hits = hits[:quota]
		}
		for i, h := range hits {
			ranked = append(ranked, rankedHit{res: h, rank: i + 1})
		}
	}

	// Cross-layer RRF: fused score depends only on per-layer rank so wiki BM25
	// and files cosine never share an axis. Scale matches wiki mergeSearchResultsRRF
	// (0.4*(k+1)) so a rank-1 hit lands near 0.4 on the 0–1 band.
	scale := 0.4 * (mergeRRFK + 1)
	for i := range ranked {
		rrf := 1.0 / (mergeRRFK + float64(ranked[i].rank))
		ranked[i].res.Score = rrf * scale
	}
	priorityByLayer := make(map[Layer]int, len(plan.Sources))
	for _, source := range plan.Sources {
		priorityByLayer[source.Source.Layer] = source.Priority
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].res.Score != ranked[j].res.Score {
			return ranked[i].res.Score > ranked[j].res.Score
		}
		// Same rank across layers: use the auditable planner priority (for
		// example files on a code/path query), then the curated-wiki default and
		// stable ref order. Never fall back to incomparable raw backend scores.
		if ranked[i].res.Ref.Layer != ranked[j].res.Ref.Layer {
			leftPriority := priorityByLayer[ranked[i].res.Ref.Layer]
			rightPriority := priorityByLayer[ranked[j].res.Ref.Layer]
			if leftPriority != rightPriority {
				return leftPriority > rightPriority
			}
			if ranked[i].res.Ref.Layer == LayerWiki {
				return true
			}
			if ranked[j].res.Ref.Layer == LayerWiki {
				return false
			}
		}
		return ranked[i].res.Ref.String() < ranked[j].res.Ref.String()
	})

	all := make([]Result, 0, len(ranked))
	for _, rh := range ranked {
		all = append(all, rh.res)
	}
	if len(all) > limit {
		all = all[:limit]
	}

	filesDegraded := ""
	for _, n := range notes {
		if strings.Contains(n, "files 레이어 타임아웃") {
			filesDegraded = "timeout"
			break
		}
	}
	slog.Info(
		"knowledge recall",
		"query_len", len(query),
		"limit", limit,
		"hit_count", len(all),
		"layers", plannedLayers(plan),
		"scopes", plan.Scopes,
		"wiki_ms", layerMs[LayerWiki],
		"files_ms", layerMs[LayerFiles],
		"files_degraded", filesDegraded,
		"total_ms", time.Since(started).Milliseconds(),
	)
	packet.Results = all
	packet.Notes = notes
	return packet
}

func plannedLayers(plan RecallPlan) []Layer {
	layers := make([]Layer, 0, len(plan.Sources))
	for _, source := range plan.Sources {
		layers = append(layers, source.Source.Layer)
	}
	return layers
}

// Read dispatches to the adapter that owns the ref's layer.
func (r *Router) Read(ctx context.Context, ref Ref) (*Document, error) {
	for _, a := range r.adapters {
		if a.Layer() == ref.Layer {
			return a.Read(ctx, ref.ID)
		}
	}
	return nil, fmt.Errorf("no adapter for layer %q", ref.Layer)
}

// Record writes a new entry through the writable adapter — today the wiki
// knowledge base.
func (r *Router) Record(ctx context.Context, opts RecordOptions) (Ref, error) {
	if r.writer == nil {
		return Ref{}, fmt.Errorf("knowledge router has no writable adapter")
	}
	return r.writer.Record(ctx, opts)
}

func (r *Router) RecordFact(ctx context.Context, opts FactRecordOptions) (FactMutationResult, error) {
	if r == nil || r.facts == nil {
		return FactMutationResult{}, fmt.Errorf("knowledge router has no fact writer")
	}
	result, err := r.facts.RecordFact(ctx, opts)
	r.notifyFactMutation(result)
	return result, err
}

func (r *Router) ForgetFact(ctx context.Context, opts FactForgetOptions) (FactMutationResult, error) {
	if r == nil || r.facts == nil {
		return FactMutationResult{}, fmt.Errorf("knowledge router has no fact writer")
	}
	result, err := r.facts.ForgetFact(ctx, opts)
	r.notifyFactMutation(result)
	return result, err
}

func (r *Router) Facts(ctx context.Context, subject, key string) ([]FactView, error) {
	if r == nil || r.facts == nil {
		return nil, fmt.Errorf("knowledge router has no fact reader")
	}
	return r.facts.Facts(ctx, subject, key)
}

// SetFactMutationObserver replaces the optional post-commit hook. Passing nil
// clears it. It is safe to call while the router serves concurrent requests.
func (r *Router) SetFactMutationObserver(observer FactMutationObserver) {
	if r == nil {
		return
	}
	r.factObserverMu.Lock()
	r.factMutationEvent = observer
	r.factObserverMu.Unlock()
}

func (r *Router) notifyFactMutation(result FactMutationResult) {
	if r == nil || !result.Committed {
		return
	}
	r.factObserverMu.RLock()
	observer := r.factMutationEvent
	r.factObserverMu.RUnlock()
	if observer == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("knowledge fact mutation observer panicked",
				"revision", result.Revision, "panic", recovered)
		}
	}()
	observer(result)
}
