// semantic.go — optional dense-vector (embedding) index over wiki pages.
//
// BM25 (search.go) finds pages by keyword overlap; it misses pages that are
// *about* the query but phrase it differently ("이 거래 위험요인" vs a page whose
// summary says "납기 지연 가능성"). This index embeds each page once (cached by
// content hash) and ranks by cosine similarity, so Search can blend lexical and
// semantic hits.
//
// Everything here degrades silently: no embedder, an unhealthy embedding
// server, or an embed error all fall back to pure BM25. The index is in-memory
// and lazy — it (re)embeds only pages whose content changed, on the first
// semantic query and whenever pages are touched.
package wiki

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// Embedder is the minimal embedding-server surface the wiki needs.
// *embedding.Client satisfies it; kept as an interface so the wiki package
// doesn't import the ai layer (and tests can inject a fake).
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	IsHealthy() bool
}

// semanticMinChars guards against embedding near-empty pages.
const semanticMinChars = 8

// semanticEmbedBatch bounds how many pages we embed per request. Kept small
// because the CPU embedding server drops (EOF) on large batches — empirically
// 32 and 64 texts return fine (~1.4s / ~3.3s) but a full ~110-page batch is
// refused, which silently failed the whole refresh and left search, related-
// link suggestion, and the graph embedding rerank with no vectors at all.
const semanticEmbedBatch = 32

// cachedVec is one page's embedding plus the content hash it was computed from.
type cachedVec struct {
	hash string
	vec  []float32
}

// semanticIndex is an in-memory, lazily-maintained vector index over wiki pages.
// Vectors are mirrored to an on-disk cache (semanticCacheFile) so the frequent
// gateway restarts don't force a full re-embed of the wiki on the first
// semantic query after every boot.
type semanticIndex struct {
	embedder    Embedder
	cachePath   string // "" → persistence disabled (tests)
	mu          sync.Mutex
	vecs        map[string]cachedVec // relPath -> embedding
	refreshing  atomic.Bool          // single-flight guard for refreshAsync
	syncRefresh bool                 // tests only: run refreshAsync inline for deterministic assertions

	// Lifecycle for the background refresh goroutine: baseCtx is cancelled by
	// shutdown() so an in-flight re-embed stops promptly, and wg lets Close wait
	// for it to fully exit — so its saveCache write cannot land after the store
	// is torn down (a truncated cache on real shutdown; a "directory not empty"
	// TempDir cleanup race in tests, since saveCache repopulates the wiki dir
	// after RemoveAll has enumerated it).
	baseCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newSemanticIndex(e Embedder) *semanticIndex {
	ctx, cancel := context.WithCancel(context.Background())
	return &semanticIndex{embedder: e, vecs: make(map[string]cachedVec), baseCtx: ctx, cancel: cancel}
}

// shutdown cancels any in-flight background refresh and waits for it to finish,
// guaranteeing no saveCache write happens after this returns. Called from
// Store.Close. Idempotent: cancel is safe to call repeatedly.
func (si *semanticIndex) shutdown() {
	si.cancel()
	si.wg.Wait()
}

// semanticRefreshTimeout bounds a background re-embed. Generous because the CPU
// embedding server (BGE-M3) is slow under host load — this runs off the request
// path, so it can afford to wait rather than time out on a caller's recall budget.
const semanticRefreshTimeout = 3 * time.Minute

// refreshAsync re-embeds changed pages in the background, at most one at a time.
// Request paths (search, related-link suggestion, graph rerank) call this and
// then read whatever vectors exist now — eventually consistent — instead of
// blocking on the embed under a caller's tight ctx. Re-embedding on the hot
// recall path (a ~1.5s preflight budget) was the source of repeated
// "context deadline exceeded" batch failures that dropped semantic search to
// BM25; the embed now owns its own generous deadline. Best-effort: a failed
// background refresh keeps prior vectors and is retried on the next trigger.
func (si *semanticIndex) refreshAsync(store *Store) {
	if si.syncRefresh {
		ctx, cancel := context.WithTimeout(context.Background(), semanticRefreshTimeout)
		defer cancel()
		_ = si.refresh(ctx, store)
		return
	}
	if !si.refreshing.CompareAndSwap(false, true) {
		return // a refresh is already in flight
	}
	si.wg.Add(1)
	safego.GoWithSlog(slog.Default(), "wiki-semantic-refresh", func() {
		defer si.wg.Done()
		defer si.refreshing.Store(false)
		// Derived from baseCtx so Store.Close can cancel it; still self-bounded so
		// it cannot wedge if Close is never called.
		ctx, cancel := context.WithTimeout(si.baseCtx, semanticRefreshTimeout)
		defer cancel()
		_ = si.refresh(ctx, store)
	})
}

// semanticCacheFile is the embedding cache inside the wiki dir. Hidden and
// non-.md so the FTS walk and ListPages never pick it up. Entries are keyed by
// content hash, so stale vectors for edited pages are re-embedded naturally.
const semanticCacheFile = ".semantic-cache.json"

// SetEmbedder attaches a semantic index backed by e. Passing nil disables it
// (Search reverts to pure BM25). Safe to call once at wiring time.
func (s *Store) SetEmbedder(e Embedder) {
	if e == nil {
		s.sem = nil
		return
	}
	si := newSemanticIndex(e)
	si.cachePath = filepath.Join(s.dir, semanticCacheFile)
	si.loadCache()
	s.sem = si
}

// WarmSemanticIndex eagerly (re)embeds any wiki pages missing from the on-disk
// vector cache so semantic Search is ready before the first query — instead of
// lazily refreshing under the caller's short recall deadline, where a large
// uncached page can time out on every query and silently degrade search to
// BM25-only. No-op without an embedder. Intended to run once in the background
// at startup; subsequent boots are cheap when the cache is already complete.
func (s *Store) WarmSemanticIndex(ctx context.Context) error {
	if s.sem == nil {
		return nil
	}
	return s.sem.refresh(ctx, s)
}

// cachedVecWire is the JSON shape of one cached embedding.
type cachedVecWire struct {
	Hash string    `json:"hash"`
	Vec  []float32 `json:"vec"`
}

// loadCache hydrates vecs from the on-disk cache. Missing file is the normal
// first-boot case; a corrupt file is dropped (vectors rebuild lazily).
func (si *semanticIndex) loadCache() {
	if si.cachePath == "" {
		return
	}
	data, err := os.ReadFile(si.cachePath)
	if err != nil {
		return
	}
	var wire map[string]cachedVecWire
	if err := json.Unmarshal(data, &wire); err != nil {
		slog.Warn("wiki: semantic cache unreadable; re-embedding from scratch",
			"path", si.cachePath, "error", err)
		return
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	for rp, cv := range wire {
		if cv.Hash == "" || len(cv.Vec) == 0 {
			continue
		}
		si.vecs[rp] = cachedVec{hash: cv.Hash, vec: cv.Vec}
	}
}

// saveCache mirrors vecs to disk (atomic tmp+rename). Failures only cost a
// warm start, so they are logged and otherwise ignored.
func (si *semanticIndex) saveCache() {
	if si.cachePath == "" {
		return
	}
	si.mu.Lock()
	wire := make(map[string]cachedVecWire, len(si.vecs))
	for rp, cv := range si.vecs {
		wire[rp] = cachedVecWire{Hash: cv.hash, Vec: cv.vec}
	}
	si.mu.Unlock()

	data, err := json.Marshal(wire)
	if err != nil {
		return
	}
	tmp := si.cachePath + ".tmp"
	if err := writeFileSync(tmp, data, 0o644); err != nil {
		slog.Warn("wiki: semantic cache write failed", "path", si.cachePath, "error", err)
		return
	}
	if err := os.Rename(tmp, si.cachePath); err != nil {
		os.Remove(tmp)
		slog.Warn("wiki: semantic cache rename failed", "path", si.cachePath, "error", err)
	}
}

// semanticText is the text embedded for a page: title + summary + body, which
// is what a meaning-based query should match against.
func semanticText(page *Page) string {
	if page == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(page.Meta.Title)
	if page.Meta.Summary != "" {
		sb.WriteString("\n" + page.Meta.Summary)
	}
	if page.Body != "" {
		sb.WriteString("\n" + page.Body)
	}
	return strings.TrimSpace(sb.String())
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// searchSemantic embeds the query and returns the top-`limit` pages by cosine
// similarity. Returns nil (not an error) on any degradation path so the caller
// falls back to BM25.
func (s *Store) searchSemantic(ctx context.Context, query string, limit int) []SearchResult {
	if s.sem == nil || s.sem.embedder == nil || !s.sem.embedder.IsHealthy() {
		return nil
	}
	if len(strings.TrimSpace(query)) < semanticMinChars {
		return nil // too short to embed meaningfully
	}
	// Re-embed changed pages in the background; search the current vectors now so
	// a stale page never stalls the recall budget. The query embed below (a single
	// short text) still runs on the request ctx — fast and necessary.
	s.sem.refreshAsync(s)

	qvecs, err := s.sem.embedder.Embed(ctx, []string{query})
	if err != nil || len(qvecs) == 0 {
		return nil
	}
	qv := qvecs[0]

	s.sem.mu.Lock()
	type scored struct {
		path  string
		score float64
	}
	hits := make([]scored, 0, len(s.sem.vecs))
	for path, cv := range s.sem.vecs {
		hits = append(hits, scored{path: path, score: cosine(qv, cv.vec)})
	}
	s.sem.mu.Unlock()

	sort.Slice(hits, func(a, b int) bool { return hits[a].score > hits[b].score })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		if h.score <= 0 {
			continue
		}
		out = append(out, SearchResult{Path: h.path, Score: h.score})
	}
	return out
}

// refresh re-embeds pages whose content changed and drops deleted ones. Holds
// the index mutex only around map mutations, not around the network call.
// Any mutation (even partial progress before a batch error) is mirrored to the
// on-disk cache so the work survives the next restart.
func (si *semanticIndex) refresh(ctx context.Context, store *Store) (err error) {
	relPaths, err := store.ListPages("")
	if err != nil {
		return err
	}

	mutated := false
	defer func() {
		if mutated {
			si.saveCache()
		}
	}()

	// Compute desired hashes and collect pages needing (re)embedding.
	want := make(map[string]string, len(relPaths))
	var toEmbed []string
	var toEmbedText []string

	si.mu.Lock()
	for _, rp := range relPaths {
		page, perr := store.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		text := semanticText(page)
		if len(text) < semanticMinChars {
			continue
		}
		h := contentHash(text)
		want[rp] = h
		if cur, ok := si.vecs[rp]; !ok || cur.hash != h {
			toEmbed = append(toEmbed, rp)
			toEmbedText = append(toEmbedText, text)
		}
	}
	// Drop entries for pages that no longer exist.
	for rp := range si.vecs {
		if _, ok := want[rp]; !ok {
			delete(si.vecs, rp)
			mutated = true
		}
	}
	si.mu.Unlock()

	// Embed changed pages in bounded batches (outside the lock).
	for start := 0; start < len(toEmbed); start += semanticEmbedBatch {
		end := min(start+semanticEmbedBatch, len(toEmbed))
		vecs, eerr := si.embedder.Embed(ctx, toEmbedText[start:end])
		if eerr != nil {
			// Prior batches already landed in vecs; keep them and surface the
			// failure (a healthy-looking server refusing batches was invisible
			// before and silently degraded search to BM25-only).
			slog.Warn("wiki: semantic embed batch failed; keeping prior vectors",
				"batchStart", start, "batchSize", end-start, "error", eerr)
			return eerr
		}
		if len(vecs) != end-start {
			return nil // unexpected shape; skip this refresh, keep prior vecs
		}
		si.mu.Lock()
		for i, rp := range toEmbed[start:end] {
			si.vecs[rp] = cachedVec{hash: want[rp], vec: vecs[i]}
		}
		si.mu.Unlock()
		mutated = true
	}
	return nil
}

// relatedSuggestMinScore is the cosine floor for a suggested `related` link.
// High on purpose: a sparse, trustworthy graph beats a dense, noisy one.
const relatedSuggestMinScore = 0.6

// SuggestRelated returns the wiki paths most semantically similar to the page
// at relPath, excluding itself and any page already in its Related[]. Only
// neighbors above relatedSuggestMinScore are returned, best first. Returns nil
// when no embedder is configured/healthy or the page isn't embeddable — so
// callers can densify the graph opportunistically without ever forcing a link.
func (s *Store) SuggestRelated(ctx context.Context, relPath string, limit int) []string {
	if s.sem == nil || s.sem.embedder == nil || !s.sem.embedder.IsHealthy() {
		return nil
	}
	if limit <= 0 {
		limit = 3
	}
	page, err := s.ReadPage(relPath)
	if err != nil || page == nil {
		return nil
	}
	s.sem.refreshAsync(s) // background re-embed; suggest from current vectors

	already := make(map[string]bool, len(page.Meta.Related))
	for _, r := range page.Meta.Related {
		already[strings.TrimSpace(r)] = true
	}

	s.sem.mu.Lock()
	self, ok := s.sem.vecs[relPath]
	if !ok {
		s.sem.mu.Unlock()
		return nil
	}
	type scored struct {
		path  string
		score float64
	}
	cands := make([]scored, 0, len(s.sem.vecs))
	for path, cv := range s.sem.vecs {
		if path == relPath || already[path] || already[strings.TrimSuffix(path, ".md")] {
			continue
		}
		if sc := cosine(self.vec, cv.vec); sc >= relatedSuggestMinScore {
			cands = append(cands, scored{path: path, score: sc})
		}
	}
	s.sem.mu.Unlock()

	sort.Slice(cands, func(a, b int) bool {
		if cands[a].score != cands[b].score {
			return cands[a].score > cands[b].score
		}
		return cands[a].path < cands[b].path
	})
	if len(cands) > limit {
		cands = cands[:limit]
	}
	out := make([]string, len(cands))
	for i := range cands {
		out[i] = cands[i].path
	}
	return out
}

// cosine returns the cosine similarity of two equal-length vectors (0 when
// either is empty or their lengths differ).
func cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
