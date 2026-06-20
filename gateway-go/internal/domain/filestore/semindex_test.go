package filestore

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// fakeEmbedder maps text to a deterministic vector by counting occurrences of a
// fixed vocabulary. Texts sharing vocabulary get high cosine similarity, so
// ranking is predictable without a real embedding server.
type fakeEmbedder struct {
	healthy bool
	vocab   []string
	calls   int // number of Embed invocations (to assert incrementality)
	texts   int // total texts embedded across all calls
}

func newFakeEmbedder(vocab ...string) *fakeEmbedder {
	return &fakeEmbedder{healthy: true, vocab: vocab}
}

func (f *fakeEmbedder) IsHealthy() bool { return f.healthy }

func (f *fakeEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.texts += len(texts)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		lower := strings.ToLower(t)
		v := make([]float32, len(f.vocab))
		for j, w := range f.vocab {
			v[j] = float32(strings.Count(lower, w))
		}
		out[i] = v
	}
	return out, nil
}

// plainText is a trivial extractor: the file's bytes ARE its text.
func plainText(_ context.Context, data []byte, _ string) string { return string(data) }

func TestSemanticIndex_ReindexAndSearch(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// Two files with disjoint vocabulary so a query about one ranks it first.
	mustPut(t, store, "/계약/납기.txt", "delivery delay penalty 납기 지연 위약금 계약")
	mustPut(t, store, "/회의/메뉴.txt", "lunch menu coffee 점심 메뉴 커피")

	embed := newFakeEmbedder("delivery", "delay", "납기", "지연", "lunch", "menu", "점심", "커피")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))

	stats, err := idx.Reindex(ctx, store, plainText, embed)
	if err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	if stats.Scanned != 2 || stats.Embedded != 2 {
		t.Fatalf("stats = %+v, want Scanned=2 Embedded=2", stats)
	}

	// Query about delivery delay must rank the contract file first.
	hits, err := idx.Search(ctx, "납기 지연 delivery delay", 5, embed)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) == 0 {
		t.Fatal("Search returned no hits")
	}
	if hits[0].Entry.PathDisplay != "/계약/납기.txt" {
		t.Errorf("top hit = %q, want /계약/납기.txt (hits: %+v)", hits[0].Entry.PathDisplay, hits)
	}
	if hits[0].Snippet == "" {
		t.Error("top hit missing snippet")
	}
	if hits[0].Score <= 0 {
		t.Errorf("top hit score = %v, want > 0", hits[0].Score)
	}
}

func TestSemanticIndex_Incremental(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/a.txt", "delivery delay 납기")
	mustPut(t, store, "/b.txt", "lunch menu 점심")

	embed := newFakeEmbedder("delivery", "납기", "lunch", "점심")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))

	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex 1: %v", err)
	}
	firstTexts := embed.texts
	if firstTexts < 2 {
		t.Fatalf("first reindex embedded %d texts, want >=2", firstTexts)
	}

	// Second reindex with no changes must embed nothing new.
	stats, err := idx.Reindex(ctx, store, plainText, embed)
	if err != nil {
		t.Fatalf("Reindex 2: %v", err)
	}
	if stats.Embedded != 0 {
		t.Errorf("unchanged reindex Embedded = %d, want 0", stats.Embedded)
	}
	if embed.texts != firstTexts {
		t.Errorf("unchanged reindex embedded %d more texts, want 0", embed.texts-firstTexts)
	}

	// Modify one file → only it re-embeds.
	mustPut(t, store, "/a.txt", "delivery delay 납기 추가 내용 변경됨")
	before := embed.texts
	stats, err = idx.Reindex(ctx, store, plainText, embed)
	if err != nil {
		t.Fatalf("Reindex 3: %v", err)
	}
	if stats.Embedded != 1 {
		t.Errorf("changed-file reindex Embedded = %d, want 1", stats.Embedded)
	}
	if embed.texts <= before {
		t.Error("changed file was not re-embedded")
	}
}

func TestSemanticIndex_GC(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/keep.txt", "delivery 납기")
	mustPut(t, store, "/gone.txt", "lunch 점심")

	embed := newFakeEmbedder("delivery", "납기", "lunch", "점심")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex 1: %v", err)
	}

	if err := store.Delete(ctx, "/gone.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	stats, err := idx.Reindex(ctx, store, plainText, embed)
	if err != nil {
		t.Fatalf("Reindex 2: %v", err)
	}
	if stats.Removed != 1 {
		t.Errorf("Removed = %d, want 1", stats.Removed)
	}

	idx.mu.Lock()
	_, stillThere := idx.files["/gone.txt"]
	n := len(idx.files)
	idx.mu.Unlock()
	if stillThere {
		t.Error("/gone.txt still in index after GC")
	}
	if n != 1 {
		t.Errorf("index has %d files, want 1", n)
	}
}

func TestSemanticIndex_Persistence(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/a.txt", "delivery 납기")
	embed := newFakeEmbedder("delivery", "납기")

	path := filepath.Join(t.TempDir(), "idx.json")
	idx := NewSemanticIndex(path)
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// A fresh index at the same path loads the persisted vectors and serves
	// search without re-embedding any files.
	idx2 := NewSemanticIndex(path)
	embed2 := newFakeEmbedder("delivery", "납기")
	hits, err := idx2.Search(ctx, "delivery 납기", 5, embed2)
	if err != nil {
		t.Fatalf("Search after reload: %v", err)
	}
	if len(hits) != 1 || hits[0].Entry.PathDisplay != "/a.txt" {
		t.Fatalf("reload search hits = %+v, want 1 hit /a.txt", hits)
	}
	if embed2.texts != 1 {
		// Only the query is embedded (1 text); files came from the loaded cache.
		t.Errorf("reload embedded %d texts, want 1 (query only)", embed2.texts)
	}
}

func TestSemanticIndex_DegradesWithoutEmbedder(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/a.txt", "delivery 납기")

	idx := NewSemanticIndex("")

	// Unhealthy embedder → Reindex is a no-op (no error), Search empty (no error).
	down := &fakeEmbedder{healthy: false, vocab: []string{"delivery"}}
	stats, err := idx.Reindex(ctx, store, plainText, down)
	if err != nil {
		t.Fatalf("Reindex with down embedder errored: %v", err)
	}
	if stats.Embedded != 0 {
		t.Errorf("down-embedder Reindex embedded %d, want 0", stats.Embedded)
	}
	hits, err := idx.Search(ctx, "delivery", 5, down)
	if err != nil {
		t.Fatalf("Search with down embedder errored: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("down-embedder Search returned %d hits, want 0", len(hits))
	}

	// nil embedder is also safe.
	if _, err := idx.Reindex(ctx, store, plainText, nil); err != nil {
		t.Errorf("Reindex(nil embed) errored: %v", err)
	}
	if hits, err := idx.Search(ctx, "delivery", 5, nil); err != nil || len(hits) != 0 {
		t.Errorf("Search(nil embed) = (%v, %v), want (empty, nil)", hits, err)
	}
}

func TestChunkText(t *testing.T) {
	// Short text → one chunk.
	if got := chunkText("hello world 안녕"); len(got) != 1 {
		t.Errorf("short text chunks = %d, want 1", len(got))
	}
	// Empty / whitespace → no chunks.
	if got := chunkText("   \n  "); got != nil {
		t.Errorf("blank text chunks = %v, want nil", got)
	}
	// Long text splits into multiple rune-bounded chunks, capped at maxChunksPerFile.
	long := strings.Repeat("가", chunkRunes*3+50)
	got := chunkText(long)
	if len(got) != 4 { // 3 full + 1 remainder
		t.Errorf("long text chunks = %d, want 4", len(got))
	}
	for i, c := range got {
		if rc := len([]rune(c)); rc > chunkRunes {
			t.Errorf("chunk %d has %d runes, exceeds cap %d", i, rc, chunkRunes)
		}
	}

	// Cap enforcement: text that would yield > maxChunksPerFile chunks is capped.
	huge := strings.Repeat("나", chunkRunes*(maxChunksPerFile+5))
	if got := chunkText(huge); len(got) != maxChunksPerFile {
		t.Errorf("huge text chunks = %d, want cap %d", len(got), maxChunksPerFile)
	}
}
