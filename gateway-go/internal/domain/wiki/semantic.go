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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/embedindex"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/pkg/textchunk"
	"github.com/choiceoh/deneb/gateway-go/pkg/vectorutil"
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

type semanticChunk struct {
	snippet   string
	startLine int
	endLine   int
	kind      string
	vec       []float32
}

// cachedVec keeps a page centroid for graph operations and structure-aware
// chunks for retrieval. The centroid is derived from chunk vectors, so this
// adds no extra embedding request.
type cachedVec struct {
	hash   string
	vec    []float32
	chunks []semanticChunk
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
	forgetEpoch uint64               // bumped under mu on each forget; a refresh drops its write-back if this changed mid-flight so an in-flight embed can't resurrect a vector a concurrent forget deleted

	cacheFingerprint   string // embedding contract that produced vecs
	cacheDimensions    int
	cachePreprocessing string
	lastError          string

	// Lifecycle for the background refresh goroutine: baseCtx is cancelled by
	// shutdown() so an in-flight re-embed stops promptly, and wg lets Close wait
	// for it to fully exit — so its saveCache write cannot land after the store
	// is torn down (a truncated cache on real shutdown; a "directory not empty"
	// TempDir cleanup race in tests, since saveCache repopulates the wiki dir
	// after RemoveAll has enumerated it). closed (under mu) serializes wg.Add
	// against wg.Wait: once shutdown sets it, refreshAsync starts no new goroutine,
	// so a positive Add can never race a zero-counter Wait (the sync.WaitGroup
	// contract). All three are guarded by mu.
	baseCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
}

func newSemanticIndex(e Embedder) *semanticIndex {
	ctx, cancel := context.WithCancel(context.Background())
	return &semanticIndex{embedder: e, vecs: make(map[string]cachedVec), baseCtx: ctx, cancel: cancel}
}

// shutdown cancels any in-flight background refresh and waits for it to finish,
// guaranteeing no saveCache write happens after this returns. Called from
// Store.Close. Idempotent: safe to call repeatedly. Setting closed under mu
// happens-before any later wg.Add check, so wg.Wait below never races a starting
// Add (the goroutine is registered, or it never starts).
func (si *semanticIndex) shutdown() {
	si.mu.Lock()
	si.closed = true
	si.mu.Unlock()
	si.cancel()
	si.wg.Wait()
}

// semanticRefreshTimeout bounds a background re-embed. Generous because the CPU
// embedding server (BGE-M3) is slow under host load — this runs off the request
// path, so it can afford to wait rather than time out on a caller's recall budget.
const semanticRefreshTimeout = 3 * time.Minute

// semanticQueryTimeout bounds user-facing query embeddings. Page refresh owns a
// longer timeout; query embedding is a best-effort semantic signal and must
// fall back to BM25 before one tool call consumes the turn.
var semanticQueryTimeout = 800 * time.Millisecond

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
	// Register the goroutine under mu so a concurrent shutdown (which sets closed
	// under the same mu before wg.Wait) either sees it counted or stops it from
	// starting — never a positive Add racing a zero-counter Wait.
	si.mu.Lock()
	if si.closed {
		si.mu.Unlock()
		si.refreshing.Store(false) // not spawning; release the single-flight guard
		return
	}
	si.wg.Add(1)
	si.mu.Unlock()
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

// semanticCacheSchemaVersion 4 moved the vectors into a sidecar blob
// (semantic_cache_blob.go); the manifest keeps paths, hashes and chunk
// metadata. A v3 file still loads and is rewritten as v4 on the next save.
const semanticCacheSchemaVersion = 4

// semanticPreprocessingVersion identifies how a wiki page becomes embedding
// input. Bump it whenever semanticText normalization/chunking semantics change,
// even if the embedding model and dimensions stay the same.
const semanticPreprocessingVersion = "wiki-structure-chunk-v2"

// diarySemanticCacheFile is the diary vector cache, kept beside the diary files
// (leading dot so the diary walk never treats it as an entry file).
const diarySemanticCacheFile = ".diary-semantic-cache.json"

// SetEmbedder attaches semantic indexes (wiki pages + diary entries) backed by e.
// Passing nil disables them (search reverts to pure BM25). Safe to call once at
// wiring time.
func (s *Store) SetEmbedder(e Embedder) {
	if e == nil {
		s.sem = nil
		if s.diaryFTS != nil {
			s.diaryFTS.attachSemantic(nil, "")
		}
		return
	}
	si := newSemanticIndex(e)
	si.cachePath = filepath.Join(s.dir, semanticCacheFile)
	si.loadCache()
	s.sem = si
	if s.diaryFTS != nil && s.diaryDir != "" {
		s.diaryFTS.attachSemantic(e, filepath.Join(s.diaryDir, diarySemanticCacheFile))
	}
}

// dropSemanticVector synchronously removes a page's cached embedding so a
// hard-deleted page stops surfacing in semantic recall immediately. Without it,
// the vector lingers in s.sem.vecs until the next async refresh prune, and
// searchSemanticWithVec ranks the live vecs — so a just-forgotten page could
// still be returned in the race window. No-op when no embedder is wired.
func (s *Store) dropSemanticVector(relPath string) {
	if s.sem == nil {
		return
	}
	s.sem.mu.Lock()
	_, existed := s.sem.vecs[relPath]
	delete(s.sem.vecs, relPath)
	// Bump the epoch so a refresh that snapshotted this page before the delete
	// and is embedding it outside the lock won't write the vector back after us.
	s.sem.forgetEpoch++
	s.sem.mu.Unlock()
	if existed {
		// Persist the removal: otherwise a gateway restart reloads the forgotten
		// vector from .semantic-cache.json in SetEmbedder and the first semantic
		// search ranks it before the async prune wins — resurfacing the page.
		// saveCache locks si.mu itself, so call it after releasing the lock.
		s.sem.saveCache()
	}
}

// SearchDiarySemanticBatch returns semantic (cosine-ranked) diary hits per query,
// index-aligned with queries — the dense-vector complement to SearchDiary's BM25.
// Score is the raw cosine (0–1); the recall layer applies the diary source prior.
// Empty when no embedder is wired, so callers keep pure BM25.
func (s *Store) SearchDiarySemanticBatch(ctx context.Context, queries []string, limit int) [][]DiaryHit {
	if s.diaryFTS == nil {
		return nil
	}
	return s.diaryFTS.searchSemanticBatch(ctx, queries, limit)
}

// WarmDiarySemantic eagerly embeds all diary entries so semantic diary recall is
// ready before the first query. Mirrors WarmSemanticIndex; no-op without an
// embedder. Intended to run once in the background at startup.
func (s *Store) WarmDiarySemantic(ctx context.Context) error {
	if s.diaryFTS == nil {
		return nil
	}
	return s.diaryFTS.warmSemantic(ctx)
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
	Hash   string              `json:"hash"`
	Vec    []float32           `json:"vec"`
	Chunks []semanticChunkWire `json:"chunks,omitempty"`
}

type semanticChunkWire struct {
	Snippet   string    `json:"snippet"`
	StartLine int       `json:"startLine"`
	EndLine   int       `json:"endLine"`
	Kind      string    `json:"kind,omitempty"`
	Vec       []float32 `json:"vec"`
}

type semanticCacheEnvelope struct {
	Version       int                      `json:"version"`
	Fingerprint   string                   `json:"fingerprint,omitempty"`
	Dimensions    int                      `json:"dimensions,omitempty"`
	Preprocessing string                   `json:"preprocessing,omitempty"`
	Entries       map[string]cachedVecWire `json:"entries"`
	// BlobVectors is how many vectors the sidecar must hold (v4+). It is the
	// integrity check between the two files: a manifest and blob that disagree
	// would slice someone else's embedding into a page, so a mismatch drops the
	// whole cache and re-embeds.
	BlobVectors int `json:"blobVectors,omitempty"`
}

// cachedVecWireV4 is one entry in a v4 manifest: the same metadata, with the
// vectors replaced by indices into the sidecar blob.
type cachedVecWireV4 struct {
	Hash     string                `json:"hash"`
	VecIndex int                   `json:"vecIndex"`
	Chunks   []semanticChunkWireV4 `json:"chunks,omitempty"`
}

type semanticChunkWireV4 struct {
	Snippet   string `json:"snippet"`
	StartLine int    `json:"startLine"`
	EndLine   int    `json:"endLine"`
	Kind      string `json:"kind,omitempty"`
	VecIndex  int    `json:"vecIndex"`
}

// semanticCacheEnvelopeV4 is the manifest actually written from v4 on.
type semanticCacheEnvelopeV4 struct {
	Version       int                        `json:"version"`
	Fingerprint   string                     `json:"fingerprint,omitempty"`
	Dimensions    int                        `json:"dimensions,omitempty"`
	Preprocessing string                     `json:"preprocessing,omitempty"`
	BlobVectors   int                        `json:"blobVectors"`
	Entries       map[string]cachedVecWireV4 `json:"entries"`
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
	// v4+: vectors live in the sidecar blob.
	var v4 semanticCacheEnvelopeV4
	if err := json.Unmarshal(data, &v4); err == nil && v4.Version >= 4 && v4.Entries != nil {
		si.loadCacheV4(v4)
		return
	}
	var envelope semanticCacheEnvelope
	var wire map[string]cachedVecWire
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Version > 0 && envelope.Entries != nil {
		wire = envelope.Entries
	} else if err := json.Unmarshal(data, &wire); err != nil {
		slog.Warn("wiki: semantic cache unreadable; re-embedding from scratch",
			"path", si.cachePath, "error", err)
		return
	}
	slog.Info("wiki: reading legacy semantic cache; it will be rewritten in the compact format",
		"path", si.cachePath, "entries", len(wire))
	si.mu.Lock()
	defer si.mu.Unlock()
	si.cacheFingerprint = envelope.Fingerprint
	si.cacheDimensions = envelope.Dimensions
	si.cachePreprocessing = envelope.Preprocessing
	for rp, cv := range wire {
		if cv.Hash == "" || len(cv.Vec) == 0 {
			continue
		}
		chunks := make([]semanticChunk, 0, len(cv.Chunks))
		for _, chunk := range cv.Chunks {
			if len(chunk.Vec) == 0 {
				continue
			}
			chunks = append(chunks, semanticChunk{
				snippet: chunk.Snippet, startLine: chunk.StartLine, endLine: chunk.EndLine,
				kind: chunk.Kind, vec: chunk.Vec,
			})
		}
		si.vecs[rp] = cachedVec{hash: cv.Hash, vec: cv.Vec, chunks: chunks}
	}
}

// blobPath is the vector sidecar beside the manifest.
func (si *semanticIndex) blobPath() string {
	if si.cachePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(si.cachePath), semanticBlobFile)
}

// loadCacheV4 hydrates from a manifest plus its vector blob. Any inconsistency
// between the two — missing blob, wrong length, out-of-range index — drops the
// cache instead of hydrating a page with another page's vector.
func (si *semanticIndex) loadCacheV4(env semanticCacheEnvelopeV4) {
	reader, err := readSemanticBlob(si.blobPath(), env.Dimensions)
	if err != nil {
		slog.Warn("wiki: semantic vector blob unreadable; re-embedding from scratch",
			"path", si.blobPath(), "error", err)
		return
	}
	if reader.Count() != env.BlobVectors {
		slog.Warn("wiki: semantic manifest and vector blob disagree; re-embedding from scratch",
			"manifestVectors", env.BlobVectors, "blobVectors", reader.Count())
		return
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	si.cacheFingerprint = env.Fingerprint
	si.cacheDimensions = env.Dimensions
	si.cachePreprocessing = env.Preprocessing
	for rp, cv := range env.Entries {
		vec := reader.At(cv.VecIndex)
		if cv.Hash == "" || vec == nil {
			continue
		}
		chunks := make([]semanticChunk, 0, len(cv.Chunks))
		for _, chunk := range cv.Chunks {
			cvec := reader.At(chunk.VecIndex)
			if cvec == nil {
				continue
			}
			chunks = append(chunks, semanticChunk{
				snippet: chunk.Snippet, startLine: chunk.StartLine, endLine: chunk.EndLine,
				kind: chunk.Kind, vec: cvec,
			})
		}
		si.vecs[rp] = cachedVec{hash: cv.Hash, vec: vec, chunks: chunks}
	}
}

// saveCache mirrors vecs to disk: a JSON manifest plus the float32 vector blob,
// each written atomically (tmp+rename). The blob lands first so a crash between
// the two leaves the OLD manifest with a newer blob — caught on load by the
// vector-count check, which drops the cache and re-embeds rather than serving
// mis-sliced vectors. Failures only cost a warm start, so they are logged and
// otherwise ignored.
func (si *semanticIndex) saveCache() {
	if si.cachePath == "" {
		return
	}
	fingerprint := si.cacheFingerprint
	dimensions := si.cacheDimensions
	if identity := embedindex.IdentityOf(si.embedder); identity.Fingerprint != "" {
		fingerprint = identity.Fingerprint
		dimensions = identity.Dimensions
	}

	si.mu.Lock()
	if dimensions <= 0 {
		for _, cv := range si.vecs { // fall back to whatever the vectors carry
			if len(cv.vec) > 0 {
				dimensions = len(cv.vec)
				break
			}
		}
	}
	blob := newBlobWriter(dimensions, len(si.vecs)*6)
	entries := make(map[string]cachedVecWireV4, len(si.vecs))
	for rp, cv := range si.vecs {
		idx := blob.Add(cv.vec)
		if idx < 0 {
			continue // wrong-dimension vector: re-embedded on the next refresh
		}
		chunks := make([]semanticChunkWireV4, 0, len(cv.chunks))
		for _, chunk := range cv.chunks {
			cidx := blob.Add(chunk.vec)
			if cidx < 0 {
				continue
			}
			chunks = append(chunks, semanticChunkWireV4{
				Snippet: chunk.snippet, StartLine: chunk.startLine, EndLine: chunk.endLine,
				Kind: chunk.kind, VecIndex: cidx,
			})
		}
		entries[rp] = cachedVecWireV4{Hash: cv.hash, VecIndex: idx, Chunks: chunks}
	}
	preprocessing := semanticPreprocessingVersion
	si.mu.Unlock()

	data, err := json.Marshal(semanticCacheEnvelopeV4{
		Version:       semanticCacheSchemaVersion,
		Fingerprint:   fingerprint,
		Dimensions:    dimensions,
		Preprocessing: preprocessing,
		BlobVectors:   blob.Len(),
		Entries:       entries,
	})
	if err != nil {
		return
	}
	blobPath := si.blobPath()
	blobTmp := blobPath + ".tmp"
	if err := writeFileSync(blobTmp, blob.Bytes(), 0o644); err != nil {
		slog.Warn("wiki: semantic vector blob write failed", "path", blobPath, "error", err)
		return
	}
	if err := os.Rename(blobTmp, blobPath); err != nil {
		os.Remove(blobTmp)
		slog.Warn("wiki: semantic vector blob rename failed", "path", blobPath, "error", err)
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

// SemanticIndexStatus is the operator-facing health of the wiki vector cache.
// Search still degrades to BM25 when this is incomplete; this status makes that
// fallback visible instead of allowing a partial/stale cache to look healthy.
type SemanticIndexStatus struct {
	Enabled               bool   `json:"enabled"`
	Healthy               bool   `json:"healthy"`
	EmbedderHealthy       bool   `json:"embedderHealthy"`
	Refreshing            bool   `json:"refreshing"`
	IdentityMismatch      bool   `json:"identityMismatch"`
	PreprocessingMismatch bool   `json:"preprocessingMismatch"`
	Fingerprint           string `json:"fingerprint,omitempty"`
	CacheFingerprint      string `json:"cacheFingerprint,omitempty"`
	Dimensions            int    `json:"dimensions,omitempty"`
	Preprocessing         string `json:"preprocessing"`
	CachePreprocessing    string `json:"cachePreprocessing,omitempty"`
	Expected              int    `json:"expected"`
	Indexed               int    `json:"indexed"`
	Pending               int    `json:"pending"`
	Stale                 int    `json:"stale"`
	CorpusErrors          int    `json:"corpusErrors"`
	LastError             string `json:"lastError,omitempty"`
	DegradedReason        string `json:"degradedReason,omitempty"`
}

// SemanticStatus compares the live wiki corpus with the vector cache and the
// active embedding identity. It performs only local reads and is intended for
// status/diagnostic surfaces, not the per-turn search hot path.
func (s *Store) SemanticStatus() SemanticIndexStatus {
	if s == nil || s.sem == nil || s.sem.embedder == nil {
		return SemanticIndexStatus{}
	}
	si := s.sem
	identity := embedindex.IdentityOf(si.embedder)
	status := SemanticIndexStatus{
		Enabled:         true,
		EmbedderHealthy: si.embedder.IsHealthy(),
		Refreshing:      si.refreshing.Load(),
		Fingerprint:     identity.Fingerprint,
		Dimensions:      identity.Dimensions,
		Preprocessing:   semanticPreprocessingVersion,
	}

	si.mu.Lock()
	vecs := make(map[string]cachedVec, len(si.vecs))
	for path, vector := range si.vecs {
		vecs[path] = vector
	}
	status.CacheFingerprint = si.cacheFingerprint
	status.CachePreprocessing = si.cachePreprocessing
	cacheDimensions := si.cacheDimensions
	if status.Dimensions == 0 {
		status.Dimensions = cacheDimensions
	}
	status.LastError = si.lastError
	si.mu.Unlock()

	identityMismatch := len(vecs) > 0 && identity.Fingerprint != "" &&
		(status.CacheFingerprint == "" || status.CacheFingerprint != identity.Fingerprint ||
			(cacheDimensions > 0 && identity.Dimensions > 0 && cacheDimensions != identity.Dimensions))
	status.IdentityMismatch = identityMismatch
	status.PreprocessingMismatch = len(vecs) > 0 && status.CachePreprocessing != semanticPreprocessingVersion
	live := make(map[string]struct{})
	paths, listErr := s.ListPages("")
	if listErr != nil {
		status.CorpusErrors++
		if status.LastError == "" {
			status.LastError = listErr.Error()
		}
	}
	for _, path := range paths {
		page, err := s.ReadPage(path)
		if err != nil {
			status.CorpusErrors++
			if status.LastError == "" {
				status.LastError = err.Error()
			}
			continue
		}
		if page == nil {
			status.CorpusErrors++
			continue
		}
		if !semanticPageAdmitted(path, page) {
			continue
		}
		text := semanticText(page)
		if len(text) < semanticMinChars {
			continue
		}
		status.Expected++
		live[path] = struct{}{}
		cached, ok := vecs[path]
		if ok && !identityMismatch && !status.PreprocessingMismatch && cached.hash == contentHash(text) &&
			validCachedSemanticPage(cached, status.Dimensions) {
			status.Indexed++
			continue
		}
		if ok {
			status.Stale++
		}
	}
	for path := range vecs {
		if _, ok := live[path]; !ok {
			status.Stale++
		}
	}
	status.Pending = status.Expected - status.Indexed
	switch {
	case !status.EmbedderHealthy:
		status.DegradedReason = "embedding_unhealthy"
	case status.IdentityMismatch:
		status.DegradedReason = "embedding_identity_mismatch"
	case status.PreprocessingMismatch:
		status.DegradedReason = "preprocessing_mismatch"
	case status.CorpusErrors > 0:
		status.DegradedReason = "corpus_read_error"
	case status.LastError != "":
		status.DegradedReason = "refresh_error"
	case status.Pending > 0:
		status.DegradedReason = "incomplete_cache"
	case status.Stale > 0:
		status.DegradedReason = "stale_cache"
	}
	status.Healthy = status.DegradedReason == ""
	return status
}

// semanticText is the text embedded for a page: title + summary + cue anchors +
// facet identity metadata + body, which is what a meaning-based query should
// match against. Cues are the alternate phrasings a future query may use, so
// folding them into the embedding pulls the page's vector toward the question
// vocabulary; a page whose cues change re-embeds automatically via the
// contentHash cache key. Facet metadata (facetText: 거래처/현장/코드) folds in
// behind the same DENEB_WIKI_FACET_BOOST gate as the lexical facet field —
// the RRF semantic arm carries 10x the BM25 weight, so counterparty/site
// vocabulary reachable only lexically stayed half-covered (facet probe:
// client-arm recovery 1/45 with the BM25 field alone). Toggling the knob
// changes this text and therefore re-embeds facet pages on the next warm.
func semanticText(page *Page) string {
	if page == nil {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(page.Meta.Title)
	if page.Meta.Summary != "" {
		sb.WriteString("\n" + page.Meta.Summary)
	}
	if len(page.Meta.Cues) > 0 {
		sb.WriteString("\n" + strings.Join(page.Meta.Cues, " · "))
	}
	if facet := facetText(page); facet != "" && wikiFacetBoostValue() > 0 {
		sb.WriteString("\n" + facet)
	}
	if page.Body != "" {
		sb.WriteString("\n" + page.Body)
	}
	return strings.TrimSpace(sb.String())
}

// semanticPageAdmitted is the single corpus predicate shared by refresh and
// health accounting. Counting a page that refresh intentionally excludes
// leaves SemanticStatus permanently pending after a fact projection or page
// supersession.
func semanticPageAdmitted(relPath string, page *Page) bool {
	if page == nil || isGeneratedFactProjectionPage(relPath, page) {
		return false
	}
	return !IsEffectivelySuperseded(relPath, page.Meta)
}

const wikiSemanticMaxChunks = textchunk.DefaultMaxChunks

type semanticChunkInput struct {
	text      string
	snippet   string
	startLine int
	endLine   int
	kind      string
}

// semanticChunkInputs prefixes each body chunk with stable page identity while
// keeping the displayed snippet source-only. The identity prefix gives a small
// section enough context to remain meaningful without exposing hidden cues.
func semanticChunkInputs(relPath string, page *Page) []semanticChunkInput {
	if page == nil {
		return nil
	}
	identityParts := []string{
		page.Meta.Title,
		page.Meta.Summary,
		strings.Join(page.Meta.Cues, " · "),
	}
	// Same facet fold (and gate) as semanticText: every body chunk carries the
	// page's counterparty/site/code identity so a facet-vocabulary query can
	// reach any chunk, not just the identity chunk.
	if facet := facetText(page); facet != "" && wikiFacetBoostValue() > 0 {
		identityParts = append(identityParts, facet)
	}
	identity := strings.TrimSpace(strings.Join(identityParts, "\n"))
	bodyStart := pageBodyStartLine(page)
	chunks := textchunk.Split(relPath, page.Body, textchunk.Options{
		TargetRunes: textchunk.DefaultTargetRunes,
		MaxChunks:   wikiSemanticMaxChunks,
	})
	if len(chunks) == 0 {
		if len(identity) < semanticMinChars {
			return nil
		}
		return []semanticChunkInput{{text: identity, snippet: visiblePageIdentity(page), startLine: 1, endLine: 1, kind: "identity"}}
	}
	out := make([]semanticChunkInput, 0, len(chunks))
	for _, chunk := range chunks {
		input := strings.TrimSpace(identity + "\n" + chunk.Text)
		if len(input) < semanticMinChars {
			continue
		}
		out = append(out, semanticChunkInput{
			text: input, snippet: chunk.Text,
			startLine: bodyStart + chunk.StartLine - 1,
			endLine:   bodyStart + chunk.EndLine - 1,
			kind:      chunk.Kind,
		})
	}
	return out
}

func pageBodyStartLine(page *Page) int {
	if page == nil || page.Body == "" {
		return 1
	}
	rendered := string(page.Render())
	prefixLength := len(rendered) - len(page.Body)
	if prefixLength < 0 || !strings.HasSuffix(rendered, page.Body) {
		return 1
	}
	return strings.Count(rendered[:prefixLength], "\n") + 1
}

func visiblePageIdentity(page *Page) string {
	if page == nil {
		return ""
	}
	return strings.TrimSpace(strings.Join([]string{page.Meta.Title, page.Meta.Summary}, " — "))
}

func validCachedSemanticPage(cached cachedVec, dimensions int) bool {
	if len(cached.vec) == 0 || len(cached.chunks) == 0 {
		return false
	}
	expectedDimensions := dimensions
	if expectedDimensions <= 0 {
		expectedDimensions = len(cached.vec)
	}
	if len(cached.vec) != expectedDimensions {
		return false
	}
	for _, chunk := range cached.chunks {
		if len(chunk.vec) != expectedDimensions {
			return false
		}
	}
	return true
}

func centroid(vectors [][]float32) []float32 {
	if len(vectors) == 0 || len(vectors[0]) == 0 {
		return nil
	}
	out := make([]float32, len(vectors[0]))
	valid := 0
	for _, vector := range vectors {
		if len(vector) != len(out) {
			continue
		}
		for i, value := range vector {
			out[i] += value
		}
		valid++
	}
	if valid == 0 {
		return nil
	}
	for i := range out {
		out[i] /= float32(valid)
	}
	return out
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

	qvecs, err := embedSemanticQueries(ctx, s.sem.embedder, []string{query})
	if err != nil || len(qvecs) == 0 {
		return nil
	}
	return s.searchSemanticWithVec(ctx, qvecs[0], limit)
}

// searchSemanticWithVec ranks pages by cosine to a PRE-COMPUTED query vector —
// the scan half of searchSemantic, split out so SearchBatch can embed every
// query in one request (fanned across the server's context pool) and reuse each
// vector here. Returns nil for an empty vector or a disabled index.
func (s *Store) searchSemanticWithVec(ctx context.Context, qv []float32, limit int) []SearchResult {
	if s.sem == nil || len(qv) == 0 {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}
	s.sem.mu.Lock()
	identity := embedindex.IdentityOf(s.sem.embedder)
	identityMismatch := identity.Fingerprint != "" &&
		(s.sem.cacheFingerprint == "" || s.sem.cacheFingerprint != identity.Fingerprint ||
			(s.sem.cacheDimensions > 0 && identity.Dimensions > 0 && s.sem.cacheDimensions != identity.Dimensions))
	if s.sem.cachePreprocessing != semanticPreprocessingVersion || identityMismatch {
		s.sem.mu.Unlock()
		return nil
	}
	type semanticEntry struct {
		path string
		vec  cachedVec
	}
	entries := make([]semanticEntry, 0, len(s.sem.vecs))
	for path, cv := range s.sem.vecs {
		// cachedVec is immutable after publication: refresh replaces the map
		// entry wholesale. A shallow snapshot therefore keeps the backing
		// vectors alive while letting searches scan outside the global mutex.
		entries = append(entries, semanticEntry{path: path, vec: cv})
	}
	s.sem.mu.Unlock()

	type scored struct {
		path      string
		score     float64
		snippet   string
		startLine int
		endLine   int
	}
	hits := make([]scored, 0, len(entries))
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil
		}
		best := scored{path: entry.path, score: -1}
		cv := entry.vec
		for _, chunk := range cv.chunks {
			if score := cosine(qv, chunk.vec); score > best.score {
				best.score = score
				best.snippet = chunk.snippet
				best.startLine = chunk.startLine
				best.endLine = chunk.endLine
			}
		}
		if best.score < 0 {
			best.score = cosine(qv, cv.vec)
		}
		hits = append(hits, best)
	}
	if ctx.Err() != nil {
		return nil
	}

	// Tie-break equal cosines by path: hits is built by ranging s.sem.vecs (a map,
	// arbitrary order), and RRF turns any tie order into distinct rank scores —
	// left map-arbitrary it made recall flaky for equal-cosine embeddings.
	sort.Slice(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].path < hits[b].path
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]SearchResult, 0, len(hits))
	for _, h := range hits {
		if h.score <= 0 {
			continue
		}
		out = append(out, SearchResult{
			Path: h.path, Score: h.score, Content: h.snippet,
			Line: h.startLine, EndLine: h.endLine,
		})
	}
	return out
}

// embedQueriesBatch embeds every query in ONE Embed request so the embedding
// server fans them across its context pool (a per-query loop serializes them —
// wasted now that the server embeds a batch in parallel). Returns a slice
// aligned with queries; entries for degraded paths (no/unhealthy embedder, a
// query too short to embed) are nil so the caller falls back to BM25 for that
// query. Kicks the background page re-embed once, like searchSemantic. Returns
// nil (whole slice) only when the embedder is unavailable — a healthy embed with
// some short queries still returns per-query vectors for the embeddable ones.
func (s *Store) embedQueriesBatch(ctx context.Context, queries []string) [][]float32 {
	if s.sem == nil || s.sem.embedder == nil || !s.sem.embedder.IsHealthy() {
		return nil
	}
	s.sem.refreshAsync(s)

	// Embed only the long-enough queries; remember each one's original index so
	// the returned vectors realign with the caller's query slice.
	idx := make([]int, 0, len(queries))
	texts := make([]string, 0, len(queries))
	for i, q := range queries {
		if len(strings.TrimSpace(q)) >= semanticMinChars {
			idx = append(idx, i)
			texts = append(texts, q)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	vecs, err := embedSemanticQueries(ctx, s.sem.embedder, texts)
	if err != nil || len(vecs) != len(texts) {
		return nil
	}
	out := make([][]float32, len(queries))
	for j, i := range idx {
		out[i] = vecs[j]
	}
	return out
}

func embedSemanticQueries(ctx context.Context, embedder embedindex.TextEmbedder, texts []string) ([][]float32, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := semanticQueryTimeout
	if timeout <= 0 {
		return embedindex.EmbedQueries(ctx, embedder, texts)
	}
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return embedindex.EmbedQueries(queryCtx, embedder, texts)
}

// refresh re-embeds pages whose content changed and drops deleted ones. Holds
// the index mutex only around map mutations, not around the network call.
// Any mutation (even partial progress before a batch error) is mirrored to the
// on-disk cache so the work survives the next restart.
func (si *semanticIndex) refresh(ctx context.Context, store *Store) (err error) {
	defer func() {
		si.mu.Lock()
		if err != nil {
			si.lastError = err.Error()
		} else {
			si.lastError = ""
		}
		si.mu.Unlock()
	}()

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

	// Read and chunk pages without holding the semantic mutex. The cache lock is
	// a leaf and must never cover disk I/O or embedding network calls.
	want := make(map[string]string, len(relPaths))
	inputs := make(map[string][]semanticChunkInput, len(relPaths))
	for _, rp := range relPaths {
		page, perr := store.ReadPage(rp)
		if perr != nil || !semanticPageAdmitted(rp, page) {
			continue
		}
		text := semanticText(page)
		if len(text) < semanticMinChars {
			continue
		}
		chunks := semanticChunkInputs(rp, page)
		if len(chunks) == 0 {
			continue
		}
		want[rp] = contentHash(text)
		inputs[rp] = chunks
	}
	var toEmbed []string

	si.mu.Lock()
	identity := embedindex.IdentityOf(si.embedder)
	identityChanged := identity.Fingerprint != "" && (si.cacheFingerprint == "" || si.cacheFingerprint != identity.Fingerprint ||
		(si.cacheDimensions > 0 && identity.Dimensions > 0 && si.cacheDimensions != identity.Dimensions))
	preprocessingChanged := si.cachePreprocessing != semanticPreprocessingVersion
	if identityChanged || preprocessingChanged {
		if len(si.vecs) > 0 {
			slog.Warn("wiki: semantic cache contract changed; rebuilding",
				"cached", si.cacheFingerprint, "active", identity.Fingerprint,
				"cachedDimensions", si.cacheDimensions, "activeDimensions", identity.Dimensions,
				"cachedPreprocessing", si.cachePreprocessing, "activePreprocessing", semanticPreprocessingVersion)
		}
		clear(si.vecs)
		mutated = true
	}
	if identity.Fingerprint != "" {
		si.cacheFingerprint = identity.Fingerprint
		si.cacheDimensions = identity.Dimensions
	}
	si.cachePreprocessing = semanticPreprocessingVersion
	// A moved page arrives as a new path with no cache entry, so a pure
	// path-keyed comparison re-embeds text that is byte-identical to what the
	// old path already holds. Chunk offsets are body-relative, so the cached
	// vectors transfer exactly. This matters because relocation comes in
	// batches — deal-ledger normalization, orphan-mail refiles, layout repairs —
	// and each batch otherwise costs a full re-embed plus a whole-cache rewrite.
	byHash := make(map[string]cachedVec, len(si.vecs))
	for rp, cur := range si.vecs {
		if _, stillThere := want[rp]; stillThere {
			continue // only vanished paths are relocation candidates
		}
		if validCachedSemanticPage(cur, identity.Dimensions) {
			byHash[cur.hash] = cur
		}
	}
	for rp, hash := range want {
		if cur, ok := si.vecs[rp]; ok && cur.hash == hash && validCachedSemanticPage(cur, identity.Dimensions) {
			continue
		}
		if moved, ok := byHash[hash]; ok {
			si.vecs[rp] = moved
			mutated = true
			continue
		}
		toEmbed = append(toEmbed, rp)
	}
	// Drop entries for pages that no longer exist.
	for rp := range si.vecs {
		if _, ok := want[rp]; !ok {
			delete(si.vecs, rp)
			mutated = true
		}
	}
	// Snapshot the forget epoch under the same lock as the page snapshot: if it
	// advances before a write-back below, a forget deleted a page we are still
	// embedding and the write-back must be abandoned to avoid resurrecting it.
	startEpoch := si.forgetEpoch
	si.mu.Unlock()

	// Stable order makes a partial refresh deterministic and resumable.
	sort.Strings(toEmbed)
	for pageIndex, rp := range toEmbed {
		pageInputs := inputs[rp]
		pageChunks := make([]semanticChunk, 0, len(pageInputs))
		vectors := make([][]float32, 0, len(pageInputs))
		for start := 0; start < len(pageInputs); start += semanticEmbedBatch {
			end := min(start+semanticEmbedBatch, len(pageInputs))
			texts := make([]string, end-start)
			for i := start; i < end; i++ {
				texts[i-start] = pageInputs[i].text
			}
			vecs, eerr := si.embedder.Embed(ctx, texts)
			if eerr != nil {
				slog.Warn("wiki: semantic embed batch failed; keeping prior pages",
					"pageIndex", pageIndex, "path", rp, "batchStart", start, "batchSize", end-start, "error", eerr)
				return eerr
			}
			if len(vecs) != end-start {
				return fmt.Errorf("wiki: semantic embed batch returned %d vectors for %d texts", len(vecs), end-start)
			}
			for i, vector := range vecs {
				input := pageInputs[start+i]
				pageChunks = append(pageChunks, semanticChunk{
					snippet: input.snippet, startLine: input.startLine, endLine: input.endLine,
					kind: input.kind, vec: vector,
				})
				vectors = append(vectors, vector)
			}
		}
		si.mu.Lock()
		if si.forgetEpoch != startEpoch {
			// A forget raced this refresh; its deletion must win. Abandon the
			// write-back (the next refresh re-embeds any legitimately changed
			// pages) so an in-flight embed never resurrects a forgotten vector.
			si.mu.Unlock()
			return nil
		}
		si.vecs[rp] = cachedVec{hash: want[rp], vec: centroid(vectors), chunks: pageChunks}
		si.mu.Unlock()
		mutated = true
	}
	return nil
}

// relatedSuggestMinScore is the cosine floor for a suggested `related` link.
// High on purpose: a sparse, trustworthy graph beats a dense, noisy one.
const relatedSuggestMinScore = 0.6

// A cosine floor filters weak neighbours; it cannot filter confident ones that
// belong to a different world. 시스템/ pages are gateway operating config and
// 기타/ holds personal miscellany — neither is business context for a project.
// Embedding proximity nonetheless linked 기타/lr458313 (차량 핫스팟) and
// 기타/stsjhouse-wifi (집 와이파이) into EPC contract mail, and 시스템/톤-규칙 into a
// 가배치 mail: short pages crowd together in vector space regardless of subject,
// the same effect isPersonStubPage guards against for person stubs.
//
// Those become graph edges the RRF ranker trusts, so recall on a contract
// question can surface the operator's home wifi page. The 2026-08-25 corpus
// audit found five such edges and zero legitimate ones, while the business
// cross-category edges the graph depends on (프로젝트↔인물 64, 프로젝트↔업무 67)
// are untouched by this rule.
func relatedDomainCompatible(src, dst string) bool {
	if !strings.HasPrefix(src, "프로젝트/") {
		return true
	}
	return !strings.HasPrefix(dst, "시스템/") && !strings.HasPrefix(dst, "기타/")
}

// suggestRelated returns the wiki paths most semantically similar to the page
// at relPath, excluding itself and any page already in its Related[]. Only
// neighbors above relatedSuggestMinScore are returned, best first. Returns nil
// when no embedder is configured/healthy or the page isn't embeddable — so
// callers can densify the graph opportunistically without ever forcing a link.
func (s *Store) suggestRelated(ctx context.Context, relPath string, limit int) []string {
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
	// A page with no prose of its own has nothing to be similar ABOUT, and the
	// empty scaffolding it does carry is what it matches on. See HasOwnProse.
	if !HasOwnProse(page.Body) {
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
		if !relatedDomainCompatible(relPath, path) {
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
	// Drop prose-less neighbours too, so an empty template cannot be linked TO
	// either. Reading pages is only affordable because this runs on the handful
	// that already cleared the cosine floor, in score order, until limit is met.
	out := make([]string, 0, limit)
	for _, c := range cands {
		if len(out) == limit {
			break
		}
		cand, err := s.ReadPage(c.path)
		if err != nil || cand == nil || !HasOwnProse(cand.Body) {
			continue
		}
		out = append(out, c.path)
	}
	return out
}

// HasOwnProse reports whether a body says anything of its own — any line that
// is not a heading, a provenance blockquote, or a horizontal rule.
//
// Empty scaffolding is not merely uninformative here, it is actively harmful:
// the 현장 template is byte-identical across projects apart from the place name,
// so those pages are each other's NEAREST neighbours and the cosine floor
// cannot separate them. The 2026-08-25 corpus audit found 30 of 36 현장 pages
// with zero prose, wired to each other across unrelated projects — 광명시→광주,
// 울주군→완도군, 영광군→광주 — edges that then mislead recall by region. The
// duplicate-folding pass had already made it worse, concatenating four empty
// templates into one page with four identical heading blocks.
//
// This is the category-agnostic form of what isPersonStubPage does for 인물,
// which needs a marker list because a synced person skeleton carries real
// value lines; a page that is nothing but headings needs no such list.
func HasOwnProse(body string) bool {
	for _, line := range strings.Split(body, "\n") {
		t := strings.TrimSpace(line)
		if t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ">") || strings.HasPrefix(t, "---") {
			continue
		}
		return true
	}
	return false
}

// cosine returns the cosine similarity of two equal-length vectors (0 when
// either is empty or their lengths differ).
func cosine(a, b []float32) float64 {
	return vectorutil.Cosine(a, b)
}
