// Package embedindex is a corpus-agnostic in-memory dense-vector index.
//
// It generalizes the pattern that wiki (semantic.go) and filestore (semindex.go)
// each grew independently: embed a corpus of text items once (cached by content
// hash), keep the vectors in memory, and answer cosine-similarity queries so a
// caller can blend semantic hits with its lexical (BM25) index. Both of those
// predate this package and are left untouched; embedindex exists so NEW corpora
// (diary entries, session summaries) get the same background-refresh, on-disk
// cache, and degrade-to-empty behavior without a third and fourth copy.
//
// Everything degrades silently: no embedder, an unhealthy server, or an embed
// error all yield zero hits, so the caller falls back to its lexical index. The
// index is lazy — it (re)embeds only items whose content hash changed, in the
// background, off the caller's query deadline.
package embedindex

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

	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/pkg/vectorutil"
)

// Embedder is the minimal embedding-server surface the index needs. Both
// *embedding.Client and the wiki/filestore embedders satisfy it structurally;
// kept as an interface so this package imports no ai layer and tests can fake it.
type Embedder interface {
	TextEmbedder
	IsHealthy() bool
}

// Item is one corpus entry to embed. Hash is any stable digest of the embeddable
// content (ContentHash is provided); a changed hash triggers a re-embed. Text is
// what actually gets embedded — the caller decides what a semantic query should
// match against (e.g. summary + body).
type Item struct {
	ID   string
	Hash string
	Text string
}

// Supplier enumerates the corpus's current items. Called on each refresh, off
// the query path, so it may read files. Items shorter than minChars are skipped.
type Supplier func() []Item

// Hit is one scored result: the item ID and its cosine similarity to the query.
type Hit struct {
	ID    string
	Score float64
}

// cachedVec is one item's embedding plus the content hash it was computed from.
type cachedVec struct {
	hash string
	vec  []float32
}

// minEmbedChars guards against embedding near-empty items.
const minEmbedChars = 8

// Index is an in-memory, lazily-maintained vector index over one corpus.
type Index struct {
	name      string // log/label prefix, e.g. "diary" or "session"
	embedder  Embedder
	cachePath string // "" → persistence disabled (tests)
	batch     int    // items embedded per request
	refreshTO time.Duration
	// Lock hierarchy (acquire in this order; never reverse):
	//
	//	refreshMu -> mu
	//
	// refreshMu serializes synchronous warm and the background refresh worker;
	// mu protects vectors, lifecycle state, and the coalesced supplier slot.
	refreshMu       sync.Mutex
	mu              sync.Mutex
	vecs            map[string]cachedVec // id -> embedding
	pendingSupplier Supplier             // latest refresh request while one is running
	refreshing      atomic.Bool          // single-flight guard for RefreshAsync
	syncRefresh     bool                 // tests: run RefreshAsync inline
	lastRefreshAt   atomic.Int64
	refreshCount    atomic.Uint64
	refreshErrors   atomic.Uint64

	cacheFingerprint   string
	cacheDimensions    int
	cachePreprocessing string
	preprocessing      string

	// Lifecycle for the background refresh goroutine (mirrors wiki.semanticIndex):
	// baseCtx is cancelled by Close so an in-flight embed stops; wg lets Close wait
	// so no saveCache write lands after teardown; closed (under mu) serializes
	// wg.Add against wg.Wait.
	baseCtx context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	closed  bool
}

// Option configures an Index at construction.
type Option func(*Index)

// WithSyncRefresh runs RefreshAsync inline (tests only, deterministic).
func WithSyncRefresh() Option { return func(ix *Index) { ix.syncRefresh = true } }

// WithPreprocessingFingerprint identifies the caller's text construction and
// normalization contract. Change it when identical source items would be
// embedded differently under new preprocessing semantics.
func WithPreprocessingFingerprint(fingerprint string) Option {
	return func(ix *Index) {
		if fingerprint = strings.TrimSpace(fingerprint); fingerprint != "" {
			ix.preprocessing = fingerprint
		}
	}
}

// New builds an index. name labels logs; cachePath is the on-disk vector cache
// ("" disables persistence). A nil embedder yields an index that always returns
// zero hits (the caller degrades to its lexical index).
func New(name string, e Embedder, cachePath string, opts ...Option) *Index {
	ctx, cancel := context.WithCancel(context.Background())
	ix := &Index{
		name:          name,
		embedder:      e,
		cachePath:     cachePath,
		batch:         64,
		refreshTO:     3 * time.Minute,
		vecs:          make(map[string]cachedVec),
		preprocessing: name + ":v1",
		baseCtx:       ctx,
		cancel:        cancel,
	}
	for _, o := range opts {
		o(ix)
	}
	ix.loadCache()
	return ix
}

// Enabled reports whether semantic search can run (embedder present and healthy).
func (ix *Index) Enabled() bool {
	return ix != nil && ix.embedder != nil && ix.embedder.IsHealthy()
}

// Len returns the number of currently usable corpus vectors.
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	return len(ix.vecs)
}

// Calibration returns the model-aware semantic-only admission profile for one
// consuming surface.
func (ix *Index) Calibration(surface SemanticSurface) Calibration {
	if ix == nil {
		return CalibrationFor(nil, surface)
	}
	return CalibrationFor(ix.embedder, surface)
}

// Status is a cheap in-process diagnostic snapshot for health surfaces.
type Status struct {
	Enabled         bool   `json:"enabled"`
	Entries         int    `json:"entries"`
	Refreshing      bool   `json:"refreshing"`
	RefreshPending  bool   `json:"refreshPending"`
	LastRefreshAtMs int64  `json:"lastRefreshAtMs,omitempty"`
	RefreshCount    uint64 `json:"refreshCount"`
	RefreshErrors   uint64 `json:"refreshErrors"`
	Fingerprint     string `json:"fingerprint,omitempty"`
	Dimensions      int    `json:"dimensions,omitempty"`
	Preprocessing   string `json:"preprocessing,omitempty"`
}

func (ix *Index) Status() Status {
	if ix == nil {
		return Status{}
	}
	identity := IdentityOf(ix.embedder)
	enabled := ix.Enabled()
	ix.mu.Lock()
	status := Status{
		Enabled:         enabled,
		Entries:         len(ix.vecs),
		Refreshing:      ix.refreshing.Load(),
		RefreshPending:  ix.pendingSupplier != nil,
		LastRefreshAtMs: ix.lastRefreshAt.Load(),
		RefreshCount:    ix.refreshCount.Load(),
		RefreshErrors:   ix.refreshErrors.Load(),
		Fingerprint:     identity.Fingerprint,
		Dimensions:      identity.Dimensions,
		Preprocessing:   ix.preprocessing,
	}
	ix.mu.Unlock()
	return status
}

// Close cancels any in-flight refresh and waits for it, so no cache write happens
// after this returns. Idempotent.
func (ix *Index) Close() {
	if ix == nil {
		return
	}
	ix.mu.Lock()
	ix.closed = true
	ix.mu.Unlock()
	ix.cancel()
	ix.wg.Wait()
}

// RefreshAsync re-embeds changed items in the background, at most one at a time.
// Callers invoke it then read whatever vectors exist now (eventually consistent)
// instead of blocking on the embed under a tight query deadline.
func (ix *Index) RefreshAsync(supplier Supplier) {
	if ix == nil || ix.embedder == nil || supplier == nil {
		return
	}
	if ix.syncRefresh {
		ctx, cancel := context.WithTimeout(ix.baseCtx, ix.refreshTO)
		defer cancel()
		_ = ix.Warm(ctx, supplier)
		return
	}
	ix.mu.Lock()
	if ix.closed {
		ix.mu.Unlock()
		return
	}
	// One latest supplier slot is enough: corpus suppliers enumerate current
	// state, so ten mutations during one embed batch need one follow-up scan.
	ix.pendingSupplier = supplier
	ix.mu.Unlock()
	if !ix.refreshing.CompareAndSwap(false, true) {
		return // the running worker will consume pendingSupplier
	}
	ix.mu.Lock()
	if ix.closed {
		ix.mu.Unlock()
		ix.refreshing.Store(false)
		return
	}
	ix.wg.Add(1)
	ix.mu.Unlock()
	safego.GoWithSlog(slog.Default(), "embedindex-refresh-"+ix.name, func() {
		defer ix.wg.Done()
		ix.runRefreshLoop()
	})
}

// RefreshIfStale bounds supplier scans on read-heavy paths while still
// reconciling an index that was restored from an old cache. Mutations should
// continue to call RefreshAsync directly.
func (ix *Index) RefreshIfStale(supplier Supplier, maxAge time.Duration) {
	if ix == nil || supplier == nil {
		return
	}
	last := ix.lastRefreshAt.Load()
	if last > 0 && maxAge > 0 && time.Since(time.UnixMilli(last)) < maxAge {
		return
	}
	ix.RefreshAsync(supplier)
}

func (ix *Index) runRefreshLoop() {
	defer ix.refreshing.Store(false)
	ix.refreshMu.Lock()
	defer ix.refreshMu.Unlock()
	mutated := false
	defer func() {
		if mutated {
			ix.saveCache()
		}
	}()

	for {
		supplier := ix.takePendingSupplier()
		if supplier == nil {
			// Publish idle before rechecking the slot. A request that races this
			// edge either starts a new worker or is observed here and consumed by
			// this worker; it can no longer disappear behind single-flight.
			ix.refreshing.Store(false)
			ix.mu.Lock()
			pending := !ix.closed && ix.pendingSupplier != nil
			ix.mu.Unlock()
			if pending && ix.refreshing.CompareAndSwap(false, true) {
				continue
			}
			return
		}
		ctx, cancel := context.WithTimeout(ix.baseCtx, ix.refreshTO)
		changed, err := ix.refresh(ctx, supplier)
		cancel()
		mutated = mutated || changed
		ix.recordRefresh(err)
	}
}

func (ix *Index) takePendingSupplier() Supplier {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if ix.closed {
		ix.pendingSupplier = nil
		return nil
	}
	supplier := ix.pendingSupplier
	ix.pendingSupplier = nil
	return supplier
}

func (ix *Index) recordRefresh(err error) {
	ix.refreshCount.Add(1)
	if err != nil {
		ix.refreshErrors.Add(1)
		return
	}
	ix.lastRefreshAt.Store(time.Now().UnixMilli())
}

// Warm synchronously (re)embeds the corpus — for an eager startup warm so the
// first query has vectors instead of racing the background refresh, and for
// deterministic tests. No-op without an embedder.
func (ix *Index) Warm(ctx context.Context, supplier Supplier) error {
	if ix == nil || ix.embedder == nil || supplier == nil {
		return nil
	}
	ix.mu.Lock()
	if ix.closed {
		ix.mu.Unlock()
		return nil
	}
	ix.wg.Add(1)
	ix.mu.Unlock()
	defer ix.wg.Done()

	ix.refreshMu.Lock()
	defer ix.refreshMu.Unlock()
	mutated, err := ix.refresh(ctx, supplier)
	if mutated {
		ix.saveCache()
	}
	ix.recordRefresh(err)
	return err
}

// refresh embeds items whose content hash changed and drops vanished ones. It
// holds the mutex only around map mutations, not the network call. The owning
// warm/worker operation persists accumulated mutations before it returns.
func (ix *Index) refresh(ctx context.Context, supplier Supplier) (bool, error) {
	items := supplier()

	mutated := false

	want := make(map[string]string, len(items))
	var toEmbedID []string
	var toEmbedText []string

	ix.mu.Lock()
	identity := IdentityOf(ix.embedder)
	identityChanged := identity.Fingerprint != "" && (ix.cacheFingerprint == "" || ix.cacheFingerprint != identity.Fingerprint ||
		(ix.cacheDimensions > 0 && identity.Dimensions > 0 && ix.cacheDimensions != identity.Dimensions))
	preprocessingChanged := ix.cachePreprocessing != ix.preprocessing
	if identityChanged || preprocessingChanged {
		if len(ix.vecs) > 0 {
			slog.Warn("embedindex: cache contract changed; rebuilding",
				"index", ix.name, "cached", ix.cacheFingerprint, "active", identity.Fingerprint,
				"cachedDimensions", ix.cacheDimensions, "activeDimensions", identity.Dimensions,
				"cachedPreprocessing", ix.cachePreprocessing, "activePreprocessing", ix.preprocessing)
		}
		clear(ix.vecs)
		mutated = true
	}
	if identity.Fingerprint != "" {
		ix.cacheFingerprint = identity.Fingerprint
		ix.cacheDimensions = identity.Dimensions
	}
	ix.cachePreprocessing = ix.preprocessing
	for _, it := range items {
		if len(strings.TrimSpace(it.Text)) < minEmbedChars {
			continue
		}
		want[it.ID] = it.Hash
		if cur, ok := ix.vecs[it.ID]; !ok || cur.hash != it.Hash {
			toEmbedID = append(toEmbedID, it.ID)
			toEmbedText = append(toEmbedText, it.Text)
		}
	}
	for id := range ix.vecs {
		if _, ok := want[id]; !ok {
			delete(ix.vecs, id)
			mutated = true
		}
	}
	ix.mu.Unlock()

	for start := 0; start < len(toEmbedID); start += ix.batch {
		if ctx.Err() != nil {
			return mutated, ctx.Err()
		}
		end := min(start+ix.batch, len(toEmbedID))
		vecs, err := ix.embedder.Embed(ctx, toEmbedText[start:end])
		if err != nil {
			slog.Warn("embedindex: embed batch failed; keeping prior vectors",
				"index", ix.name, "batchStart", start, "batchSize", end-start, "error", err)
			return mutated, err
		}
		if len(vecs) != end-start {
			return mutated, fmt.Errorf("embedindex: %s embed batch returned %d vectors for %d texts", ix.name, len(vecs), end-start)
		}
		ix.mu.Lock()
		for i, id := range toEmbedID[start:end] {
			ix.vecs[id] = cachedVec{hash: want[id], vec: vecs[i]}
		}
		ix.mu.Unlock()
		mutated = true
	}
	return mutated, nil
}

// Search embeds the query and returns the top-limit items by cosine similarity.
// Returns nil on any degradation path so the caller falls back to its lexical
// index. Callers embedding several queries per turn should prefer SearchBatch.
func (ix *Index) Search(ctx context.Context, query string, limit int) []Hit {
	if !ix.Enabled() || len(strings.TrimSpace(query)) < minEmbedChars {
		return nil
	}
	qvecs, err := EmbedQueries(ctx, ix.embedder, []string{query})
	if err != nil || len(qvecs) == 0 {
		return nil
	}
	return ix.SearchVec(qvecs[0], limit)
}

// SearchBatch embeds every query in ONE request (the server fans them across its
// context pool) and returns per-query hits, index-aligned with queries. A query
// too short to embed, or a down embedder, yields nil for that slot.
func (ix *Index) SearchBatch(ctx context.Context, queries []string, limit int) [][]Hit {
	if !ix.Enabled() || len(queries) == 0 {
		return nil
	}
	idx := make([]int, 0, len(queries))
	texts := make([]string, 0, len(queries))
	for i, q := range queries {
		if len(strings.TrimSpace(q)) >= minEmbedChars {
			idx = append(idx, i)
			texts = append(texts, q)
		}
	}
	if len(texts) == 0 {
		return nil
	}
	vecs, err := EmbedQueries(ctx, ix.embedder, texts)
	if err != nil || len(vecs) != len(texts) {
		return nil
	}
	out := make([][]Hit, len(queries))
	for j, i := range idx {
		out[i] = ix.SearchVec(vecs[j], limit)
	}
	return out
}

// SearchVec ranks the corpus by cosine to a pre-computed query vector.
func (ix *Index) SearchVec(qv []float32, limit int) []Hit {
	if ix == nil || len(qv) == 0 || limit <= 0 {
		return nil
	}
	ix.mu.Lock()
	if identity := IdentityOf(ix.embedder); identity.Fingerprint != "" &&
		(ix.cacheFingerprint == "" || ix.cacheFingerprint != identity.Fingerprint ||
			(ix.cacheDimensions > 0 && identity.Dimensions > 0 && ix.cacheDimensions != identity.Dimensions)) {
		ix.mu.Unlock()
		return nil
	}
	if ix.cachePreprocessing != ix.preprocessing {
		ix.mu.Unlock()
		return nil
	}
	type vectorSnapshot struct {
		id  string
		vec []float32
	}
	vectors := make([]vectorSnapshot, 0, len(ix.vecs))
	for id, cv := range ix.vecs {
		// Vectors are immutable after insertion; retaining the slice reference is
		// safe and keeps the O(entries*dimensions) cosine scan off the index lock.
		vectors = append(vectors, vectorSnapshot{id: id, vec: cv.vec})
	}
	ix.mu.Unlock()
	hits := make([]Hit, 0, len(vectors))
	for _, vector := range vectors {
		hits = append(hits, Hit{ID: vector.id, Score: cosine(qv, vector.vec)})
	}

	sort.Slice(hits, func(a, b int) bool {
		if hits[a].Score == hits[b].Score {
			return hits[a].ID < hits[b].ID
		}
		return hits[a].Score > hits[b].Score
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := hits[:0]
	for _, h := range hits {
		if h.Score > 0 {
			out = append(out, h)
		}
	}
	return out
}

// cachedVecWire is the JSON shape of one cached embedding.
type cachedVecWire struct {
	Hash string    `json:"hash"`
	Vec  []float32 `json:"vec"`
}

const cacheSchemaVersion = 2

type cacheEnvelope struct {
	Version       int                      `json:"version"`
	Fingerprint   string                   `json:"fingerprint,omitempty"`
	Dimensions    int                      `json:"dimensions,omitempty"`
	Preprocessing string                   `json:"preprocessing,omitempty"`
	Entries       map[string]cachedVecWire `json:"entries"`
}

func (ix *Index) loadCache() {
	if ix.cachePath == "" {
		return
	}
	data, err := os.ReadFile(ix.cachePath)
	if err != nil {
		return
	}
	var envelope cacheEnvelope
	var wire map[string]cachedVecWire
	if err := json.Unmarshal(data, &envelope); err == nil && envelope.Version > 0 && envelope.Entries != nil {
		wire = envelope.Entries
	} else if err := json.Unmarshal(data, &wire); err != nil {
		slog.Warn("embedindex: cache unreadable; re-embedding from scratch",
			"index", ix.name, "path", ix.cachePath, "error", err)
		return
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ix.cacheFingerprint = envelope.Fingerprint
	ix.cacheDimensions = envelope.Dimensions
	ix.cachePreprocessing = envelope.Preprocessing
	for id, cv := range wire {
		if cv.Hash == "" || len(cv.Vec) == 0 {
			continue
		}
		ix.vecs[id] = cachedVec{hash: cv.Hash, vec: cv.Vec}
	}
}

func (ix *Index) saveCache() {
	if ix.cachePath == "" {
		return
	}
	ix.mu.Lock()
	wire := make(map[string]cachedVecWire, len(ix.vecs))
	for id, cv := range ix.vecs {
		wire[id] = cachedVecWire{Hash: cv.hash, Vec: cv.vec}
	}
	fingerprint := ix.cacheFingerprint
	dimensions := ix.cacheDimensions
	preprocessing := ix.preprocessing
	ix.mu.Unlock()
	if identity := IdentityOf(ix.embedder); identity.Fingerprint != "" {
		fingerprint = identity.Fingerprint
		dimensions = identity.Dimensions
	}

	data, err := json.Marshal(cacheEnvelope{
		Version:       cacheSchemaVersion,
		Fingerprint:   fingerprint,
		Dimensions:    dimensions,
		Preprocessing: preprocessing,
		Entries:       wire,
	})
	if err != nil {
		return
	}
	tmp := ix.cachePath + ".tmp"
	// A fresh state dir may not have the corpus subdir yet (e.g. memory/diary
	// before the first entry) — the cache must not fail on that.
	if err := os.MkdirAll(filepath.Dir(tmp), 0o755); err != nil {
		slog.Warn("embedindex: cache dir create failed", "index", ix.name, "path", ix.cachePath, "error", err)
		return
	}
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		slog.Warn("embedindex: cache write failed", "index", ix.name, "path", ix.cachePath, "error", err)
		return
	}
	if err := os.Rename(tmp, ix.cachePath); err != nil {
		os.Remove(tmp)
		slog.Warn("embedindex: cache rename failed", "index", ix.name, "path", ix.cachePath, "error", err)
	}
}

// ContentHash is a short stable digest of embeddable text; suppliers use it for
// Item.Hash so an unchanged item is not re-embedded.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:8])
}

// cosine returns the cosine similarity of two equal-length vectors (0 when either
// is empty, zero-norm, or length-mismatched).
func cosine(a, b []float32) float64 {
	return vectorutil.Cosine(a, b)
}
