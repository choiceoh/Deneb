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
// example, hindsight is not configured.
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

// Recall queries every adapter in parallel and merges the results, sorted by
// score descending. Per-adapter errors are swallowed so a single flaky backend
// (e.g. hindsight unreachable) does not block the call; callers see the
// successful subset.
func (r *Router) Recall(ctx context.Context, query string, limit int) []Result {
	if limit <= 0 {
		limit = 10
	}
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		all []Result
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
			all = append(all, hits...)
			mu.Unlock()
		}(a)
	}
	wg.Wait()

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

// Record writes a new entry through the writable adapter. Today the only
// writable adapter is wiki; hindsight memories are retained automatically
// from completed turns, not by explicit agent action.
func (r *Router) Record(ctx context.Context, opts RecordOptions) (Ref, error) {
	if r.writer == nil {
		return Ref{}, fmt.Errorf("knowledge router has no writable adapter")
	}
	return r.writer.Record(ctx, opts)
}
