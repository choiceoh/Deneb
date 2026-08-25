package embedindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type recordingEmbedder struct {
	mu          sync.Mutex
	healthy     bool
	calls       [][]string
	err         error
	badLen      bool
	block       <-chan struct{}
	started     chan<- struct{}
	fingerprint string
	dimensions  int
}

func (e *recordingEmbedder) IsHealthy() bool { return e != nil && e.healthy }

func (e *recordingEmbedder) EmbeddingFingerprint() string { return e.fingerprint }

func (e *recordingEmbedder) EmbeddingDimensions() int { return e.dimensions }

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

func TestIndexNilSafety(t *testing.T) {
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
}

func TestWarmUsesDefaultEmbedBatchLimit(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true}
	ix := New("batch-limit", embedder, "")
	defer ix.Close()
	items := make([]Item, 65)
	for i := range items {
		id := fmt.Sprintf("item-%02d", i)
		items[i] = Item{ID: id, Hash: id, Text: "long text " + id}
	}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	calls := embedder.snapshotCalls()
	if len(calls) != 2 {
		t.Fatalf("embed calls = %d, want 2", len(calls))
	}
	if len(calls[0]) != 64 || len(calls[1]) != 1 {
		t.Fatalf("embed batch sizes = [%d %d], want [64 1]", len(calls[0]), len(calls[1]))
	}
}

func TestWarmLoadsOnlyChangedItemsAndSkipsShortText(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true}
	ix := New("batch", embedder, "")
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
	wantFirst := [][]string{{"alpha long text", "bravo long text", "charlie long text"}}
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
	if got := embedder.snapshotCalls(); len(got) != 1 {
		t.Fatalf("unchanged corpus made %d calls, want 1 total", len(got))
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
	if len(calls) != 2 || !reflect.DeepEqual(calls[1], []string{"bravo changed text"}) {
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
	if err := ix.Warm(context.Background(), func() []Item { return changed }); err == nil {
		t.Fatal("bad-shape Warm returned nil error")
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
	ix.cachePreprocessing = ix.preprocessing

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
	var raw cacheEnvelope
	if err := json.Unmarshal(data, &raw); err != nil || raw.Version != cacheSchemaVersion || raw.Preprocessing != "persist:v1" || raw.Entries["a"].Hash != "hash-a" {
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

func TestEmbeddingIdentityMismatchSuppressesAndRebuildsCache(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true, fingerprint: "model-a:3", dimensions: 3}
	ix := New("identity", embedder, "")
	defer ix.Close()
	items := []Item{{ID: "a", Hash: "ha", Text: "identity protected text"}}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("Warm: %v", err)
	}
	if hits := ix.SearchVec([]float32{1, 0, 0}, 5); len(hits) != 1 {
		t.Fatalf("baseline hits = %+v", hits)
	}

	embedder.fingerprint = "model-b:3"
	if hits := ix.SearchVec([]float32{1, 0, 0}, 5); len(hits) != 0 {
		t.Fatalf("mismatched cache served hits = %+v", hits)
	}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("rebuild Warm: %v", err)
	}
	if ix.cacheFingerprint != "model-b:3" || len(ix.vecs) != 1 {
		t.Fatalf("rebuilt identity=%q vectors=%d", ix.cacheFingerprint, len(ix.vecs))
	}
	ix.preprocessing = "identity:v2"
	if hits := ix.SearchVec([]float32{1, 0, 0}, 5); len(hits) != 0 {
		t.Fatalf("preprocessing-mismatched cache served hits = %+v", hits)
	}
	if err := ix.Warm(context.Background(), func() []Item { return items }); err != nil {
		t.Fatalf("preprocessing rebuild Warm: %v", err)
	}
	if ix.cachePreprocessing != "identity:v2" || len(ix.vecs) != 1 {
		t.Fatalf("rebuilt preprocessing=%q vectors=%d", ix.cachePreprocessing, len(ix.vecs))
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

func TestRefreshAsyncReplaysLatestSupplierQueuedDuringFlight(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	embedder := &recordingEmbedder{healthy: true, block: release, started: started}
	ix := New("pending", embedder, "")
	defer ix.Close()

	ix.RefreshAsync(func() []Item {
		return []Item{{ID: "a", Hash: "a1", Text: "alpha first version"}}
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("first refresh did not start")
	}
	ix.RefreshAsync(func() []Item {
		return []Item{
			{ID: "a", Hash: "a2", Text: "alpha second version"},
			{ID: "b", Hash: "b1", Text: "bravo newly arrived"},
		}
	})
	close(release)

	deadline := time.Now().Add(2 * time.Second)
	var a cachedVec
	var aOK, bOK bool
	for time.Now().Before(deadline) {
		ix.mu.Lock()
		a, aOK = ix.vecs["a"]
		_, bOK = ix.vecs["b"]
		ix.mu.Unlock()
		if aOK && a.hash == "a2" && bOK && !ix.refreshing.Load() {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !aOK || a.hash != "a2" || !bOK {
		t.Fatalf("queued corpus not applied: a=%+v aOK=%v bOK=%v", a, aOK, bOK)
	}
	if ix.refreshing.Load() {
		t.Fatal("queued refresh did not finish")
	}
	if calls := embedder.snapshotCalls(); len(calls) != 2 {
		t.Fatalf("embed calls = %#v, want first plus one coalesced follow-up", calls)
	}
}

func TestRefreshIfStaleBoundsReadPathScansAndReportsFreshness(t *testing.T) {
	embedder := &recordingEmbedder{healthy: true, fingerprint: "model-a:3", dimensions: 3}
	ix := New("stale", embedder, "")
	defer ix.Close()
	supplierCalls := atomic.Int32{}
	supplier := func() []Item {
		supplierCalls.Add(1)
		return []Item{{ID: "a", Hash: "a1", Text: "alpha stable corpus"}}
	}

	if err := ix.Warm(context.Background(), supplier); err != nil {
		t.Fatalf("initial Warm: %v", err)
	}
	ix.RefreshIfStale(supplier, time.Hour)
	if got := supplierCalls.Load(); got != 1 {
		t.Fatalf("fresh index supplier calls = %d, want 1", got)
	}
	status := ix.Status()
	if !status.Enabled || status.Entries != 1 || status.RefreshCount != 1 || status.LastRefreshAtMs == 0 || status.Fingerprint != "model-a:3" {
		t.Fatalf("status = %+v", status)
	}

	ix.RefreshIfStale(supplier, 0)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (supplierCalls.Load() < 2 || ix.Status().RefreshCount < 2) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := supplierCalls.Load(); got != 2 {
		t.Fatalf("forced refresh supplier calls = %d, want 2", got)
	}
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
