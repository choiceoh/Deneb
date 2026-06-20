package filestore

import (
	"context"
	"os"
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

// fixedEmbedder returns a caller-chosen vector per exact text, so a test can
// place a file's chunk at a precise cosine distance from the query — letting us
// exercise the minSemanticScore floor (a real BGE-M3 scores unrelated text in a
// non-zero band, which the plain vocab-count fakeEmbedder can't reproduce since
// disjoint vocab yields an exact-0 cosine).
type fixedEmbedder struct{ vecs map[string][]float32 }

func (f *fixedEmbedder) IsHealthy() bool { return true }

func (f *fixedEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, ok := f.vecs[t]
		if !ok {
			v = []float32{0, 0} // unknown text → zero vector (cosine 0)
		}
		out[i] = v
	}
	return out, nil
}

// An irrelevant query whose best chunk cosine falls below minSemanticScore must
// yield an EMPTY result (not a max-capped list of noise), so the caller's
// name/content fallback kicks in.
//
// The floor (0.73) is tuned to BGE-M3's *Korean* cosine distribution, measured
// live on srv4: unrelated Korean queries score ~0.58–0.69 against Korean office
// docs, relevant ones ~0.77–0.86. So the dangerous case isn't an exactly-0.30
// cosine (any floor catches that) — it's the ~0.6 "Korean noise band" the old
// 0.4 floor let straight through (e.g. "오늘 날씨" returning a 개발행위허가 PDF).
// This test pins both a 0.6-band noise hit (must be dropped) and a 0.8 relevant
// hit (must survive), so a regression that lowers the floor back into the noise
// band fails here.
func TestSemanticIndex_ScoreFloor(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// Bodies must clear minChunkRunes (8) to produce a chunk at all.
	const fileA = "계약 납기 지연 위약금 조항입니다"
	const fileB = "점심 메뉴 커피 음료 목록입니다"
	mustPut(t, store, "/계약/납기.txt", fileA)
	mustPut(t, store, "/회의/메뉴.txt", fileB)

	// fileA/fileB sit on orthogonal unit axes. Each query vector is hand-placed
	// at a known cosine to fileA ({1,0,0}):
	//   noiseQuery  → ~0.302 (well below any floor)
	//   bandQuery   → ~0.6   (in the Korean noise band: old 0.4 kept it, 0.73 drops it)
	//   matchQuery  → 1.0    (a real, relevant hit)
	const noiseQuery = "전혀 무관한 다른 질문입니다" // >= 8 runes (Search rejects shorter)
	const bandQuery = "애매하게 걸치는 한국어 질문입니다"
	const matchQuery = "계약 납기 위약금 질문입니다"
	embed := &fixedEmbedder{vecs: map[string][]float32{
		fileA:      {1, 0, 0},
		fileB:      {0, 1, 0},
		noiseQuery: {1, 1, 3},     // ~0.302 to each file
		bandQuery:  {0.6, 0, 0.8}, // cos to fileA = 0.6 → Korean-noise band
		matchQuery: {1, 0, 0},     // identical to fileA's chunk → cosine 1.0
	}}

	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Both the exact-noise and the 0.6-band query must return nothing.
	for _, q := range []string{noiseQuery, bandQuery} {
		hits, err := idx.Search(ctx, q, 5, embed)
		if err != nil {
			t.Fatalf("Search(%q): %v", q, err)
		}
		if len(hits) != 0 {
			t.Fatalf("below-floor query %q returned %d hits, want 0 (floor=%v): %+v",
				q, len(hits), minSemanticScore, hits)
		}
	}

	// Sanity: a query that DOES clear the floor still returns its file, proving
	// the floor only cuts noise, not real hits.
	good, err := idx.Search(ctx, matchQuery, 5, embed)
	if err != nil {
		t.Fatalf("Search (above floor): %v", err)
	}
	if len(good) != 1 || good[0].Entry.PathDisplay != "/계약/납기.txt" {
		t.Fatalf("above-floor query hits = %+v, want 1 hit /계약/납기.txt", good)
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

// A file overwritten with DIFFERENT content but the SAME byte size and SAME
// mtime (second granularity) must still re-embed. The (mtime,size) key alone
// can't see this — the content-prefix hash is what catches it.
func TestSemanticIndex_ContentHashReindex(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	// Two bodies of identical byte length but different content.
	const v1 = "delivery delay 납기 지연 위약금 AAAA"
	const v2 = "delivery delay 납기 지연 위약금 BBBB"
	if len(v1) != len(v2) {
		t.Fatalf("test setup: v1/v2 byte lengths differ (%d vs %d)", len(v1), len(v2))
	}
	mustPut(t, store, "/a.txt", v1)

	embed := newFakeEmbedder("aaaa", "bbbb", "납기")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex 1: %v", err)
	}
	// Capture the file's mtime so we can pin it back after the overwrite.
	abs := filepath.Join(store.Root(), "a.txt")
	fi, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	origMtime := fi.ModTime()

	// Overwrite with same-size, different content, then force mtime back to the
	// original — reproducing a same-second, same-size overwrite exactly.
	mustPut(t, store, "/a.txt", v2)
	if err := os.Chtimes(abs, origMtime, origMtime); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	fi2, err := os.Stat(abs)
	if err != nil {
		t.Fatalf("stat 2: %v", err)
	}
	if !fi2.ModTime().Equal(origMtime) || fi2.Size() != fi.Size() {
		t.Fatalf("test setup: mtime/size not equal after overwrite (mtime %v==%v? size %d==%d?)",
			fi2.ModTime(), origMtime, fi2.Size(), fi.Size())
	}

	before := embed.texts
	stats, err := idx.Reindex(ctx, store, plainText, embed)
	if err != nil {
		t.Fatalf("Reindex 2: %v", err)
	}
	if stats.Embedded != 1 {
		t.Errorf("same-mtime/size overwrite Embedded = %d, want 1 (content hash should detect it)", stats.Embedded)
	}
	if embed.texts <= before {
		t.Error("content-changed file was not re-embedded")
	}

	// The index now reflects v2: a query for the new content's vocab finds it.
	// (Query must be >= minChunkRunes (8) runes or Search rejects it as too short.)
	hits, err := idx.Search(ctx, "bbbb 납기 지연 검색", 5, embed)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Entry.PathDisplay != "/a.txt" {
		t.Fatalf("post-overwrite search hits = %+v, want 1 hit /a.txt", hits)
	}
}

// Remove drops a deleted file's entry immediately (no reindex), so search stops
// returning it. Rename re-keys a moved file so it's findable at the new path.
func TestSemanticIndex_RemoveAndRename(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	mustPut(t, store, "/계약/납기.txt", "delivery delay 납기 지연")
	embed := newFakeEmbedder("delivery", "납기", "지연")
	idx := NewSemanticIndex(filepath.Join(t.TempDir(), "idx.json"))
	if _, err := idx.Reindex(ctx, store, plainText, embed); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Baseline: the file is found. (Query must be >= minChunkRunes (8) runes.)
	const q = "납기 지연 위약금 검색"
	hits, _ := idx.Search(ctx, q, 5, embed)
	if len(hits) != 1 || hits[0].Entry.PathDisplay != "/계약/납기.txt" {
		t.Fatalf("baseline hits = %+v, want 1 hit /계약/납기.txt", hits)
	}

	// Rename re-keys the entry to the new path (vectors unchanged).
	idx.Rename("/계약/납기.txt", "/보관/납기-2025.txt")
	hits, _ = idx.Search(ctx, q, 5, embed)
	if len(hits) != 1 || hits[0].Entry.PathDisplay != "/보관/납기-2025.txt" {
		t.Fatalf("after Rename hits = %+v, want 1 hit /보관/납기-2025.txt", hits)
	}

	// Remove drops it entirely.
	idx.Remove("/보관/납기-2025.txt")
	hits, _ = idx.Search(ctx, q, 5, embed)
	if len(hits) != 0 {
		t.Fatalf("after Remove hits = %+v, want 0", hits)
	}

	// Remove/Rename of an unknown path is a safe no-op; nil receiver too.
	idx.Remove("/nope.txt")
	idx.Rename("/nope.txt", "/also-nope.txt")
	var nilIdx *SemanticIndex
	nilIdx.Remove("/x")
	nilIdx.Rename("/x", "/y")
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
