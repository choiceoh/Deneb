// semindex.go — optional dense-vector (embedding) index over the file store.
//
// Name/content substring search (Search/SearchContent) finds files by literal
// overlap; it misses a file that is *about* the query but phrases it differently
// (a query "납기 지연 위험" vs a contract whose text says "delivery delay
// penalty"). This index extracts each file's text once, chunks it, embeds the
// chunks (BGE-M3), and ranks files by the best cosine similarity of any chunk to
// the query — so search can find files by meaning, not just by matching strings.
//
// Everything here degrades silently: no embedder, an unhealthy embedding server,
// or an embed error make Reindex a no-op and Search return an empty result (never
// an error). The chat tool / RPC layers fall back to name/content search.
//
// The index is a sidecar JSON file under the state dir (NOT inside the store
// root, so the user-facing listing never shows it), incrementally maintained by
// mtime+size so a background reindex re-embeds only new/changed files.
package filestore

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// semindexVersion is bumped when the on-disk shape or chunking changes, so a
// stale cache from an older layout is dropped and rebuilt rather than misread.
const semindexVersion = 1

// chunkRunes is the target size of one text chunk (~512 tokens for mixed
// Korean/English). Rune-based so a chunk boundary never splits a CJK character.
const chunkRunes = 2000

// maxChunksPerFile caps how many chunks a single file contributes, bounding both
// the embed cost and the index size for a pathologically large document.
const maxChunksPerFile = 20

// maxIndexFileBytes is the per-file payload cap for extraction during indexing.
// A file larger than this is skipped (it would dominate extract+embed latency);
// it remains findable via name/content search.
const maxIndexFileBytes = 5 << 20 // 5 MiB

// minChunkRunes guards against embedding near-empty chunks (e.g. a trailing
// whitespace-only tail), which carry no signal.
const minChunkRunes = 8

// minSemanticScore is the cosine floor a file's best chunk must clear to count
// as a semantic hit. BGE-M3 returns a non-trivial cosine (~0.3–0.6) even for
// unrelated text, so a bare best>0 filter would always return up to `max`
// results whenever the embedder is healthy — burying exact name/content matches
// under semantic noise and starving the caller's lexical fallback (which only
// triggers on an *empty* semantic result). 0.4 matches the wiki's BGE-M3
// support band (wiki/search.go semSupportThreshold); below it the match is
// noise, so the query falls through to name/content search as intended.
const minSemanticScore = 0.4

// embedBatchSize bounds how many chunks are embedded per request. Kept small
// because the CPU BGE-M3 server drops (EOF) on large batches — the wiki index
// learned the same lesson (semanticEmbedBatch=32).
const embedBatchSize = 32

// Embedder is the minimal embedding-server surface the index needs. *embedding.
// Client satisfies it; an interface keeps this package off the ai layer's import
// path and lets tests inject a deterministic fake.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	IsHealthy() bool
}

// ExtractFunc turns a file's bytes into searchable plain text. It is injected
// (the chat tools' document extractor) so the domain never imports the tools
// layer. Returning "" means "no extractable text" → the file is skipped.
type ExtractFunc func(ctx context.Context, data []byte, name string) string

// chunk is one embedded slice of a file's text.
type chunk struct {
	Snippet string    `json:"snippet"`
	Vector  []float32 `json:"vector"`
}

// fileEntry is the index record for one file: its identity (path), the
// freshness keys used for incremental reindex (mtime/size), and its chunks.
type fileEntry struct {
	Path   string  `json:"path"`   // virtual store path ("/메일/foo.pdf")
	MTime  string  `json:"mtime"`  // ServerModified (RFC3339); changed file ⇒ re-embed
	Size   int64   `json:"size"`   // byte size; changed file ⇒ re-embed
	Chunks []chunk `json:"chunks"` // empty allowed (e.g. extract returned text but all chunks too short)
}

// indexData is the on-disk JSON shape (version + path→entry map).
type indexData struct {
	Version int                   `json:"version"`
	Files   map[string]*fileEntry `json:"files"`
}

// SemanticIndex is a file-backed, incrementally-maintained vector index over the
// store. Concurrency: a single mutex guards the in-memory map and is held only
// around map reads/writes — never across the network embed call or disk I/O.
//
// Lock discipline (see .claude/rules/concurrency.md): callers must hold mu when
// invoking a *Locked helper; public methods take/release mu themselves.
type SemanticIndex struct {
	path string // sidecar JSON path; "" disables persistence (tests)

	mu    sync.Mutex
	files map[string]*fileEntry
}

// ScoredEntry is one search hit: the matched file plus its best chunk score and
// the snippet that scored highest (for display / agent context).
type ScoredEntry struct {
	Entry   Entry
	Score   float64
	Snippet string
}

// ReindexStats summarizes one Reindex pass for logging.
type ReindexStats struct {
	Scanned  int // files enumerated in the store
	Embedded int // files (re)embedded this pass (new or changed)
	Removed  int // index entries dropped because the file is gone
	Skipped  int // files skipped (too large, unreadable, or no extractable text)
}

// NewSemanticIndex creates an index persisted at path (pass "" to disable
// persistence). Any existing on-disk index at a matching version is loaded so a
// gateway restart doesn't force a full re-embed.
func NewSemanticIndex(path string) *SemanticIndex {
	si := &SemanticIndex{path: path, files: make(map[string]*fileEntry)}
	si.load()
	return si
}

// load hydrates files from the on-disk index. A missing file is the normal
// first-boot case; a corrupt file or version mismatch is dropped (rebuilt by the
// next Reindex).
func (si *SemanticIndex) load() {
	if si.path == "" {
		return
	}
	data, err := os.ReadFile(si.path)
	if err != nil {
		return // missing → first boot
	}
	var d indexData
	if err := json.Unmarshal(data, &d); err != nil || d.Version != semindexVersion {
		return // corrupt or stale layout → rebuild lazily
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	for p, fe := range d.Files {
		if fe == nil {
			continue
		}
		si.files[p] = fe
	}
}

// saveLocked mirrors the in-memory map to disk atomically. Caller holds mu.
// Failures only cost a warm start, so they are returned for the caller to log
// (not fatal).
func (si *SemanticIndex) saveLocked() error {
	if si.path == "" {
		return nil
	}
	d := indexData{Version: semindexVersion, Files: si.files}
	raw, err := json.Marshal(d)
	if err != nil {
		return fmt.Errorf("filestore semindex: marshal: %w", err)
	}
	// The index lives in the state dir, not the user-facing store root, so the
	// atomicfile .lock sidecar is harmless here (unlike the store, where a stray
	// lock would surface as a bogus listing entry).
	if err := atomicfile.WriteFile(si.path, raw, &atomicfile.Options{Perm: 0o600}); err != nil {
		return fmt.Errorf("filestore semindex: write %s: %w", si.path, err)
	}
	return nil
}

// chunkText splits s into <=chunkRunes-rune chunks on rune boundaries, dropping
// chunks shorter than minChunkRunes, and caps the count at maxChunksPerFile.
func chunkText(s string) []string {
	r := []rune(strings.TrimSpace(s))
	if len(r) == 0 {
		return nil
	}
	var out []string
	for start := 0; start < len(r) && len(out) < maxChunksPerFile; start += chunkRunes {
		end := start + chunkRunes
		if end > len(r) {
			end = len(r)
		}
		piece := strings.TrimSpace(string(r[start:end]))
		if len([]rune(piece)) >= minChunkRunes {
			out = append(out, piece)
		}
	}
	return out
}

// freshKey returns the (mtime,size) pair that decides whether a file needs
// re-embedding. A change in either re-embeds the file.
func freshKey(e Entry) (string, int64) { return e.ServerModified, e.Size }

// Reindex brings the index up to date with the store: it embeds new/changed
// files, drops entries for deleted files, and persists the result. It is
// incremental (only files whose mtime/size changed are re-embedded) so repeated
// runs are cheap.
//
// Degradation: a nil/unhealthy embed makes this a no-op (no error). Oversized,
// unreadable, or text-less files are skipped (they stay name/content searchable).
// ctx cancellation is honored between files and between embed batches.
func (si *SemanticIndex) Reindex(ctx context.Context, store Store, extractFn ExtractFunc, embed Embedder) (ReindexStats, error) {
	var stats ReindexStats
	if store == nil || embed == nil || !embed.IsHealthy() || extractFn == nil {
		return stats, nil // nothing to do; caller-side guard, not an error
	}

	// Enumerate every file once. defaultListCap bounds the candidate set.
	all, err := store.List(ctx, "/", true, defaultListCap)
	if err != nil {
		return stats, err
	}

	// Build the set of live file paths and decide which need (re)embedding.
	live := make(map[string]Entry, len(all))
	for _, e := range all {
		if e.IsFolder() {
			continue
		}
		live[e.PathDisplay] = e
		stats.Scanned++
	}

	// Snapshot which paths are already current under the lock, then do the slow
	// extract+embed work outside it.
	si.mu.Lock()
	var toEmbed []Entry
	for p, e := range live {
		mtime, size := freshKey(e)
		if cur, ok := si.files[p]; ok && cur.MTime == mtime && cur.Size == size {
			continue // unchanged
		}
		toEmbed = append(toEmbed, e)
	}
	// Drop entries for files that no longer exist (GC).
	removed := 0
	for p := range si.files {
		if _, ok := live[p]; !ok {
			delete(si.files, p)
			removed++
		}
	}
	si.mu.Unlock()
	stats.Removed = removed

	// Stable order so a partial run (ctx cancel) is deterministic and resumable.
	sort.Slice(toEmbed, func(a, b int) bool { return toEmbed[a].PathDisplay < toEmbed[b].PathDisplay })

	mutated := removed > 0
	for _, e := range toEmbed {
		if cerr := ctx.Err(); cerr != nil {
			// Persist progress so far; a save error here is secondary to the
			// cancellation we're returning (it only costs a warm start next time).
			_ = si.persistIfMutated(mutated)
			return stats, cerr
		}
		entry, ok := si.embedFile(ctx, store, extractFn, embed, e)
		if !ok {
			stats.Skipped++
			continue
		}
		si.mu.Lock()
		si.files[e.PathDisplay] = entry
		si.mu.Unlock()
		mutated = true
		stats.Embedded++
	}

	if err := si.persistIfMutated(mutated); err != nil {
		return stats, err
	}
	return stats, nil
}

// persistIfMutated saves the index when something changed; a save error is
// returned (warm-start cost only).
func (si *SemanticIndex) persistIfMutated(mutated bool) error {
	if !mutated {
		return nil
	}
	si.mu.Lock()
	defer si.mu.Unlock()
	return si.saveLocked()
}

// embedFile extracts, chunks, and embeds one file. Returns (entry, true) on
// success; (nil, false) when the file is skipped (oversized, unreadable, no
// text, or an embed failure). An embed failure for one file does not abort the
// whole reindex — the file is simply left out and retried next pass.
func (si *SemanticIndex) embedFile(ctx context.Context, store Store, extractFn ExtractFunc, embed Embedder, e Entry) (*fileEntry, bool) {
	if e.Size > maxIndexFileBytes {
		return nil, false
	}
	data, _, err := store.Get(ctx, e.PathDisplay)
	if err != nil {
		return nil, false // vanished/unreadable mid-walk
	}
	text := extractFn(ctx, data, e.Name)
	chunks := chunkText(text)
	if len(chunks) == 0 {
		// No extractable text: record a fresh, empty entry so we don't re-extract
		// this unchanged file every pass (the mtime/size key marks it current).
		mtime, size := freshKey(e)
		return &fileEntry{Path: e.PathDisplay, MTime: mtime, Size: size}, true
	}

	out := make([]chunk, 0, len(chunks))
	for start := 0; start < len(chunks); start += embedBatchSize {
		end := start + embedBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}
		vecs, eerr := embed.Embed(ctx, chunks[start:end])
		if eerr != nil || len(vecs) != end-start {
			return nil, false // skip this file; retried next pass
		}
		for i, v := range vecs {
			out = append(out, chunk{Snippet: chunks[start+i], Vector: v})
		}
	}
	mtime, size := freshKey(e)
	return &fileEntry{Path: e.PathDisplay, MTime: mtime, Size: size, Chunks: out}, true
}

// Search embeds the query and returns up to max files ranked by the best cosine
// similarity of any of their chunks. Each hit carries the highest-scoring
// snippet. Returns an empty slice (never an error) on any degradation path —
// no/unhealthy embedder, an empty index, a too-short query, or an embed failure
// — so callers fall back to name/content search.
func (si *SemanticIndex) Search(ctx context.Context, query string, max int, embed Embedder) ([]ScoredEntry, error) {
	if embed == nil || !embed.IsHealthy() {
		return nil, nil
	}
	q := strings.TrimSpace(query)
	if len([]rune(q)) < minChunkRunes {
		return nil, nil // too short to embed meaningfully
	}
	if max <= 0 {
		max = 20
	}

	// An embed failure or empty result is not surfaced as an error: Search is a
	// best-effort enhancement, and the caller falls back to name/content search
	// on an empty slice. (Reindex, the writer, does propagate embed errors.)
	qvecs, err := embed.Embed(ctx, []string{q})
	if err != nil || len(qvecs) == 0 {
		return nil, nil //nolint:nilerr // intentional graceful degradation to lexical search
	}
	qv := qvecs[0]

	si.mu.Lock()
	type scored struct {
		path    string
		size    int64
		mtime   string
		score   float64
		snippet string
	}
	hits := make([]scored, 0, len(si.files))
	for p, fe := range si.files {
		best := -1.0
		bestSnip := ""
		for i := range fe.Chunks {
			s := cosine(qv, fe.Chunks[i].Vector)
			if s > best {
				best = s
				bestSnip = fe.Chunks[i].Snippet
			}
		}
		if best < minSemanticScore {
			continue // below the cosine floor (empty entry, or only noise-level
			// similarity) ⇒ not a semantic hit; the caller's lexical fallback handles it
		}
		hits = append(hits, scored{path: p, size: fe.Size, mtime: fe.MTime, score: best, snippet: bestSnip})
	}
	si.mu.Unlock()

	sort.Slice(hits, func(a, b int) bool {
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return hits[a].path < hits[b].path // stable tiebreak
	})
	if len(hits) > max {
		hits = hits[:max]
	}

	out := make([]ScoredEntry, 0, len(hits))
	for _, h := range hits {
		out = append(out, ScoredEntry{
			Entry: Entry{
				Tag:            "file",
				Name:           pathBase(h.path),
				PathDisplay:    h.path,
				PathLower:      strings.ToLower(h.path),
				ID:             h.path,
				Size:           h.size,
				ServerModified: h.mtime,
			},
			Score:   h.score,
			Snippet: h.snippet,
		})
	}
	return out, nil
}

// pathBase returns the last "/"-segment of a virtual path (its file name).
func pathBase(p string) string {
	p = strings.TrimRight(p, "/")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// cosine returns the cosine similarity of two equal-length vectors (0 when
// either is empty or their lengths differ). Mirrors wiki/semantic.go's cosine.
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
