package knowledge

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// Router federates multiple knowledge backends under one surface. Created
// with the set of adapters available in the current deployment; missing
// backends are skipped silently so the router degrades gracefully when, for
// example, a backend is not configured.
type Router struct {
	adapters []Adapter
	writer   Writer // first writable adapter wins (today: wiki)
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
		if w, ok := a.(Writer); ok && r.writer == nil {
			r.writer = w
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
// adapter returns BGE-M3 cosine packed into a high 0.73–0.86 band, so a naive
// score-sort would let files crowd out genuinely-relevant wiki pages on a
// mixed query. The quota guarantees each configured layer a share of the
// window regardless of its raw score scale; within the window, score still
// orders the rows. A single-layer router is unaffected (quota ≥ limit).
const layerRecallQuota = 0.6

// Recall queries every adapter in parallel and merges the results. Within each
// layer hits are ordered by score; across layers a per-layer quota
// (layerRecallQuota) prevents one score band from monopolizing the merged
// window, then the kept rows are returned in global score order. Per-adapter
// errors are swallowed so a single flaky backend (e.g. one unreachable) does
// not block the call; callers see the successful subset.
func (r *Router) Recall(ctx context.Context, query string, limit int) []Result {
	if limit <= 0 {
		limit = 10
	}
	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		byHits = make(map[Layer][]Result, len(r.adapters))
	)
	for _, a := range r.adapters {
		wg.Add(1)
		go func(a Adapter) {
			defer wg.Done()
			hits, err := a.Recall(ctx, query, limit)
			if err != nil {
				return
			}
			mu.Lock()
			byHits[a.Layer()] = append(byHits[a.Layer()], hits...)
			mu.Unlock()
		}(a)
	}
	wg.Wait()

	// Per-layer quota: at most quota hits from any one layer (a single-layer
	// router is unaffected because quota is computed against limit). Each layer's
	// own hits are ranked by score first so the quota keeps that layer's best.
	quota := int(float64(limit)*layerRecallQuota + 0.999)
	if quota < 1 {
		quota = 1
	}
	var all []Result
	for _, hits := range byHits {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		if len(byHits) > 1 && len(hits) > quota {
			hits = hits[:quota]
		}
		all = append(all, hits...)
	}

	sort.SliceStable(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > limit {
		all = all[:limit]
	}
	return all
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
