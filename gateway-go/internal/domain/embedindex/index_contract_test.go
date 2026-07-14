package embedindex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingEmbedder struct {
	mu      sync.Mutex
	healthy bool
	calls   [][]string
	err     error
	badLen  bool
	block   <-chan struct{}
	started chan<- struct{}
}

func (e *recordingEmbedder) IsHealthy() bool { return e != nil && e.healthy }

func (e *recordingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	e.mu.Lock()
	e.calls = append(e.calls, append([]string(nil), texts...))
	e.mu.Unlock()
	if e.started != nil {
		select {
		case e.started <- struct{}{}:
		default:
		}
	}
	if e.block != nil {
		select {
		case <-e.block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if e.err != nil {
		return nil, e.err
	}
	count := len(texts)
	if e.badLen && count > 0 {
		count--
	}
	out := make([][]float32, count)
	for i := range out {
		// Each text receives a deterministic non-zero vector. The first two
		// dimensions also make exact-text fixtures easy to rank deliberately.
		out[i] = []float32{float32(len(texts[i])), float32(i + 1), 1}
	}
	return out, nil
}

func (e *recordingEmbedder) snapshotCalls() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([][]string, len(e.calls))
	for i := range e.calls {
		out[i] = append([]string(nil), e.calls[i]...)
	}
	return out
}

func TestIndexOptionsAndNilSafety(t *testing.T) {
	if (*Index)(nil).Enabled() {
		t.Fatal("nil index reported enabled")
	}
	(*Index)(nil).Close()
	(*Index)(nil).RefreshAsync(func() []Item { t.Fatal("nil index called supplier"); return nil })
	if err := (*Index)(nil).Warm(context.Background(), nil); err != nil {
		t.Fatalf("nil Warm: %v", err)
	}
	if got := (*Index)(nil).SearchVec([]float32{1}, 1); got != nil {
		t.Fatalf("nil SearchVec = %v", got)
	}

	for _, tc := range []struct {
		name string
		n    int
		want int
	}{
		{name: "zero ignored", n: 0, want: 64},
		{name: "negative ignored", n: -1, want: 64},
		{name: "too large ignored", n: 257, want: 64},
		{name: "one accepted", n: 1, want: 1},
		{name: "maximum accepted", n: 256, want: 256},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix := New("options", nil, "", WithBatchSize(tc.n))
			defer ix.Close()
			if ix.batch != tc.want {
				t.Fatalf("batch = %d, want %d", ix.batch, tc.want)
			}
		})
	}
}

func TestWarmLoadsOnlyChangedItemsAndSkipsShortText(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true}
	ix := New("batch", embedder, "", WithBatchSize(2))
	defer ix.Close()
	items := []Item{
		{ID: "a", Hash: "ha", Text: "alpha long text"},
		{ID: "short", Hash: "hs", Text: " tiny "},
		{ID: "b", Hash: "hb", Text: "bravo long text"},
		{ID: "c", Hash: "hc", Text: "charlie long text"},
	}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("first Warm: %v", err)
	}
	wantFirst := [][]string{{"alpha long text", "bravo long text"}, {"charlie long text"}}
	if got := embedder.snapshotCalls(); !reflect.DeepEqual(got, wantFirst) {
		t.Fatalf("embed calls = %#v, want %#v", got, wantFirst)
	}
	if len(ix.vecs) != 3 {
		t.Fatalf("vectors = %d, want 3", len(ix.vecs))
	}

	// Identical hashes must not consume another embedding request.
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("second Warm: %v", err)
	}
	if got := embedder.snapshotCalls(); len(got) != 2 {
		t.Fatalf("unchanged corpus made %d calls, want 2 total", len(got))
	}

	// Changing b re-embeds only b, removing c drops it, and the too-short item
	// never enters the cache even if it has a hash.
	changed := []Item{
		items[0],
		{ID: "b", Hash: "hb2", Text: "bravo changed text"},
		items[1],
	}
	if err := ix.Warm(context.Background(), func() []Item { return changed }); err != nil {
		t.Fatalf("changed Warm: %v", err)
	}
	calls := embedder.snapshotCalls()
	if len(calls) != 3 || !reflect.DeepEqual(calls[2], []string{"bravo changed text"}) {
		t.Fatalf("changed calls = %#v", calls)
	}
	if _, ok := ix.vecs["c"]; ok {
		t.Fatal("removed item c remains cached")
	}
	if _, ok := ix.vecs["short"]; ok {
		t.Fatal("short item was cached")
	}
}

func TestWarmPreservesPriorVectorsOnEmbedFailureAndBadShape(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true}
	ix := New("failure", embedder, "")
	defer ix.Close()
	initial := []Item{{ID: "a", Hash: "one", Text: "initial long text"}}
	if err := ix.Warm(context.Background(), func() []Item { return initial }); err != nil {
		t.Fatalf("initial Warm: %v", err)
	}
	prior := append([]float32(nil), ix.vecs["a"].vec...)

	embedder.err = errors.New("embedding offline")
	changed := []Item{{ID: "a", Hash: "two", Text: "changed long text"}}
	if err := ix.Warm(context.Background(), func() []Item { return changed }); !errors.Is(err, embedder.err) {
		t.Fatalf("Warm error = %v", err)
	}
	if got := ix.vecs["a"]; got.hash != "one" || !reflect.DeepEqual(got.vec, prior) {
		t.Fatalf("failed embed replaced prior vector: %+v", got)
	}

	embedder.err = nil
	embedder.badLen = true
	if err := ix.Warm(context.Background(), func() []Item { return changed }); err != nil {
		t.Fatalf("bad-shape Warm returned error: %v", err)
	}
	if got := ix.vecs["a"]; got.hash != "one" || !reflect.DeepEqual(got.vec, prior) {
		t.Fatalf("bad shape replaced prior vector: %+v", got)
	}
}

func TestSearchVecRejectsInvalidInputsAndBreaksTiesStably(t *testing.T) {
	ix := New("search", &recordingEmbedder{healthy: true}, "")
	defer ix.Close()
	ix.vecs = map[string]cachedVec{
		"z": {hash: "z", vec: []float32{1, 0}},
		"a": {hash: "a", vec: []float32{1, 0}},
		"b": {hash: "b", vec: []float32{0, 1}},
		"n": {hash: "n", vec: []float32{-1, 0}},
	}

	if got := ix.SearchVec(nil, 3); got != nil {
		t.Fatalf("empty query vector = %v", got)
	}
	if got := ix.SearchVec([]float32{1, 0}, 0); got != nil {
		t.Fatalf("zero limit = %v", got)
	}
	if got := ix.SearchVec([]float32{1, 0}, -2); got != nil {
		t.Fatalf("negative limit = %v", got)
	}
	hits := ix.SearchVec([]float32{1, 0}, 10)
	if len(hits) != 2 {
		t.Fatalf("positive hits = %#v", hits)
	}
	if hits[0].ID != "a" || hits[1].ID != "z" {
		t.Fatalf("tie order = %#v, want a then z", hits)
	}
	if got := ix.SearchVec([]float32{1, 0}, 1); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("limited hits = %#v", got)
	}
}

func TestSearchAndSearchBatchReturnNilOnDegradedEmbedderOrShortInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		embedder *recordingEmbedder
	}{
		{name: "unhealthy", embedder: &recordingEmbedder{healthy: false}},
		{name: "error", embedder: &recordingEmbedder{healthy: true, err: errors.New("down")}},
		{name: "bad shape", embedder: &recordingEmbedder{healthy: true, badLen: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ix := New("degrade", tc.embedder, "")
			defer ix.Close()
			ix.vecs["a"] = cachedVec{hash: "a", vec: []float32{1, 0, 0}}
			if got := ix.Search(context.Background(), "query long enough", 5); got != nil {
				t.Fatalf("Search = %v", got)
			}
			if got := ix.SearchBatch(context.Background(), []string{"query long enough"}, 5); got != nil {
				t.Fatalf("SearchBatch = %v", got)
			}
		})
	}

	healthy := &recordingEmbedder{healthy: true}
	ix := New("guards", healthy, "")
	defer ix.Close()
	if got := ix.Search(context.Background(), "short", 5); got != nil {
		t.Fatalf("short Search = %v", got)
	}
	if got := ix.SearchBatch(context.Background(), nil, 5); got != nil {
		t.Fatalf("empty SearchBatch = %v", got)
	}
	if got := ix.SearchBatch(context.Background(), []string{"x", " tiny "}, 5); got != nil {
		t.Fatalf("all-short SearchBatch = %v", got)
	}
	if len(healthy.snapshotCalls()) != 0 {
		t.Fatal("guarded queries called embedder")
	}
}

func TestCacheRoundTripAndInvalidEntries(t *testing.T) {
	cachePath := filepath.Join(t.TempDir(), "nested", "vectors.json")
	if err := os.MkdirAll(filepath.Dir(cachePath), 0o755); err != nil {
		t.Fatal(err)
	}
	embedder := &recordingEmbedder{healthy: true}
	ix := New("persist", embedder, cachePath)
	items := []Item{{ID: "a", Hash: "hash-a", Text: "alpha persisted text"}}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	ix.Close()

	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]cachedVecWire
	if err := json.Unmarshal(data, &raw); err != nil || raw["a"].Hash != "hash-a" {
		t.Fatalf("cache JSON = %#v, err=%v", raw, err)
	}

	loaded := New("loaded", nil, cachePath)
	defer loaded.Close()
	if got, ok := loaded.vecs["a"]; !ok || got.hash != "hash-a" || len(got.vec) == 0 {
		t.Fatalf("loaded vector = %+v, ok=%v", got, ok)
	}

	invalidPath := filepath.Join(t.TempDir(), "invalid.json")
	invalid := map[string]cachedVecWire{
		"valid":     {Hash: "h", Vec: []float32{1}},
		"emptyHash": {Vec: []float32{1}},
		"emptyVec":  {Hash: "h"},
	}
	encoded, _ := json.Marshal(invalid)
	if err := os.WriteFile(invalidPath, encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	filtered := New("filtered", nil, invalidPath)
	defer filtered.Close()
	if len(filtered.vecs) != 1 {
		t.Fatalf("filtered cache = %#v", filtered.vecs)
	}

	corruptPath := filepath.Join(t.TempDir(), "corrupt.json")
	if err := os.WriteFile(corruptPath, []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	corrupt := New("corrupt", nil, corruptPath)
	defer corrupt.Close()
	if len(corrupt.vecs) != 0 {
		t.Fatalf("corrupt cache loaded vectors: %#v", corrupt.vecs)
	}
}

func TestRefreshAsyncSingleFlightAndCloseCancellation(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	embedder := &recordingEmbedder{healthy: true, block: release, started: started}
	ix := New("async", embedder, "")
	supplierCalls := atomic.Int32{}
	supplier := func() []Item {
		supplierCalls.Add(1)
		return []Item{{ID: "a", Hash: "a", Text: "alpha asynchronous text"}}
	}

	ix.RefreshAsync(supplier)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("background embed did not start")
	}
	for i := 0; i < 10; i++ {
		ix.RefreshAsync(supplier)
	}
	if got := supplierCalls.Load(); got != 1 {
		t.Fatalf("single-flight supplier calls = %d", got)
	}

	closed := make(chan struct{})
	go func() {
		ix.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not cancel and join refresh")
	}
	if ix.refreshing.Load() {
		t.Fatal("refreshing flag remains set after Close")
	}
	ix.RefreshAsync(supplier)
	if got := supplierCalls.Load(); got != 1 {
		t.Fatalf("closed index refreshed again; supplier calls = %d", got)
	}
	close(release)
	ix.Close() // idempotent
}

func TestContentHashPreservesDeterminismAndSeparatesInputs(t *testing.T) {
	a := ContentHash("same text")
	b := ContentHash("same text")
	c := ContentHash("different text")
	if a == "" || len(a) != 16 {
		t.Fatalf("hash shape = %q", a)
	}
	if a != b {
		t.Fatalf("same input hashes differ: %q != %q", a, b)
	}
	if a == c {
		t.Fatalf("different inputs collided: %q", a)
	}
}
