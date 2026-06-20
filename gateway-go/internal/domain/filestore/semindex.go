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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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

// contentHashPrefixBytes is how many leading bytes are hashed (together with the
// size) to form the content-change key. ServerModified is RFC3339 (1-second
// granularity), so a file overwritten within the same second at the same byte
// size would keep its stale vectors under an (mtime,size)-only key. A hash of
// the first 64 KiB catches the common overwrite (a re-export, a `cp` of
// different content) cheaply — reading a full multi-MB file every pass just to
// detect this rare collision would defeat the incremental design. The residual
// blind spot (two files identical in their first 64 KiB but differing later,
// same size, same second) is vanishingly rare and self-heals on the next mtime
// change.
const contentHashPrefixBytes = 64 << 10 // 64 KiB

// minSemanticScore is the cosine floor a file's best chunk must clear to count
// as a semantic hit. BGE-M3 packs *Korean* text into a high, narrow band: even
// a totally unrelated query scores ~0.58–0.69 against a Korean office document,
// and a genuinely relevant query scores ~0.77–0.86. A low floor therefore lets
// every Korean query "match" every file — measured on the live srv4 BGE-M3
// (:8001), the old 0.4 floor kept 15/15 irrelevant queries (e.g. "오늘 날씨
// 어때" returned the 개발행위허가 PDF). The floor must sit *inside* the
// Korean separation band, not at the generic-cosine band the wiki uses.
//
// Measured cosine distribution (srv4 BGE-M3, 20 relevant + 23 irrelevant
// Korean (query, file) pairs across two office-doc corpora):
//
//	relevant   : min 0.7722, mean 0.8137, max 0.8619
//	irrelevant : min 0.5847, mean 0.6307, max 0.6890   (best chunk over all files)
//	"오늘 날씨 어때" vs 개발행위허가 PDF: 0.5626
//
// Floor sweep (relevant kept / irrelevant kept):
//
//	0.40 → 20/20, 23/23   (the old floor — useless for Korean)
//	0.70 → 20/20,  0/23   (clean)
//	0.76 → 20/20,  0/23   (clean)
//	0.78 → drops real hits (토지 형질변경 0.772, 인사 발령 명단 0.772)
//
// The clean window is [0.689 irrelevant-max, 0.772 relevant-min]; 0.73 is its
// midpoint, keeping every relevant pair and rejecting every irrelevant one with
// ~0.04 margin on each side. An absolute floor separates cleanly, so no
// per-query normalization or BM25 hybrid is needed. Below it the match is noise
// and the query falls through to name/content search as intended.
const minSemanticScore = 0.73

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
// freshness keys used for incremental reindex (mtime/size/content hash), and its
// chunks.
type fileEntry struct {
	Path    string  `json:"path"`              // virtual store path ("/메일/foo.pdf")
	MTime   string  `json:"mtime"`             // ServerModified (RFC3339); changed file ⇒ re-embed
	Size    int64   `json:"size"`              // byte size; changed file ⇒ re-embed
	Content string  `json:"content,omitempty"` // content-prefix hash; disambiguates same-second+same-size overwrites
	Chunks  []chunk `json:"chunks"`            // empty allowed (e.g. extract returned text but all chunks too short)
}

// indexData is the on-disk JSON shape (version + path→entry map).
type indexData struct {
	Version int                   `json:"version"`
	Files   map[string]*fileEntry `json:"files"`
}

// SemanticIndex is a file-backed, incrementally-maintained vector index over the
// store. Concurrency: a single mutex guards the in-memory map and is held only
// around map reads/writes — never across the network embed call, the cosine
// scan, or disk I/O (marshal + write). The slow paths (save, Search) snapshot
// under the lock and do the heavy work outside it, so a large index never
// freezes a concurrent Search or write (see .claude/rules/concurrency.md: no
// network/disk I/O while holding a lock).
//
// Lock discipline: callers must hold mu when invoking a *Locked helper; public
// methods take/release mu themselves. Chunk slices in fileEntry are never
// mutated in place — an entry is always replaced wholesale — so a snapshot may
// retain entry pointers and read their chunks lock-free.
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

// save mirrors the in-memory map to disk atomically. It snapshots the map under
// the lock, then marshals and writes *without* the lock held — marshaling a
// large index can take tens of ms, and holding mu across it would stall every
// concurrent Search/write (the bug this fixes). The caller MUST NOT hold mu.
// Failures only cost a warm start, so they are returned for the caller to log
// (not fatal).
func (si *SemanticIndex) save() error {
	if si.path == "" {
		return nil
	}
	// Shallow-copy the path→entry map under the lock. Entries are immutable once
	// stored (replaced wholesale, never mutated in place), so sharing the
	// *fileEntry pointers with the marshaler is safe even as the map mutates.
	si.mu.Lock()
	snapshot := make(map[string]*fileEntry, len(si.files))
	for p, fe := range si.files {
		snapshot[p] = fe
	}
	si.mu.Unlock()

	raw, err := json.Marshal(indexData{Version: semindexVersion, Files: snapshot})
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

// Remove drops the index entry for path (a deleted/moved-away file) so a search
// stops returning a stale path that would 404 at download time, without waiting
// for the next 15-minute reindex. A no-op when the path isn't indexed. Persisted
// best-effort; a save failure only costs a warm start (the GC pass would drop it
// anyway). Safe to call even when persistence is disabled (path == "").
func (si *SemanticIndex) Remove(path string) {
	if si == nil || path == "" {
		return
	}
	si.mu.Lock()
	_, existed := si.files[path]
	if existed {
		delete(si.files, path)
	}
	si.mu.Unlock()
	if existed {
		_ = si.save()
	}
}

// Rename re-keys the index entry from oldPath to newPath after a move, so search
// returns the new path immediately (the vectors are unchanged — only the file's
// location moved). If newPath already has an entry it is overwritten (the moved
// file's content wins). A no-op when oldPath isn't indexed or the paths are
// equal. Persisted best-effort. The entry's Path field is updated so a later
// save/search reports the new location.
func (si *SemanticIndex) Rename(oldPath, newPath string) {
	if si == nil || oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}
	si.mu.Lock()
	fe, ok := si.files[oldPath]
	if ok {
		delete(si.files, oldPath)
		fe.Path = newPath
		si.files[newPath] = fe
	}
	si.mu.Unlock()
	if ok {
		_ = si.save()
	}
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

// freshKey returns the (mtime,size) pair that is the cheap first half of the
// change check. A change in either re-embeds the file without any read; the
// content hash (contentHashFor) only disambiguates the case where both match.
func freshKey(e Entry) (string, int64) { return e.ServerModified, e.Size }

// contentHashFor returns a hex SHA-256 over the file's size and first
// contentHashPrefixBytes bytes — the change key used to disambiguate a
// same-mtime, same-size overwrite. A read error returns "" (the caller then
// falls back to the (mtime,size) decision, i.e. treats it as unchanged), since
// an unreadable file will be re-evaluated next pass anyway.
func contentHashFor(ctx context.Context, store Store, path string, size int64) string {
	rc, _, err := store.Open(ctx, path)
	if err != nil {
		return ""
	}
	defer func() { _ = rc.Close() }()
	h := sha256.New()
	// Bind the size into the hash so a truncation/extension that keeps the same
	// prefix still changes the key.
	fmt.Fprintf(h, "%d:", size)
	if _, err := io.Copy(h, io.LimitReader(rc, contentHashPrefixBytes)); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

// contentHashOfBytes mirrors contentHashFor for already-in-memory bytes (the
// embed path has the full file in hand, so it hashes the same prefix without a
// second read).
func contentHashOfBytes(data []byte, size int64) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d:", size)
	if len(data) > contentHashPrefixBytes {
		data = data[:contentHashPrefixBytes]
	}
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

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

	// Snapshot which paths look current (cheap mtime+size match) under the lock,
	// then verify the ambiguous ones by content hash and do the slow
	// extract+embed work — all outside the lock.
	si.mu.Lock()
	var toEmbed []Entry
	var verify []Entry // (mtime,size) matched — confirm with a content-prefix hash
	prevContent := make(map[string]string)
	for p, e := range live {
		mtime, size := freshKey(e)
		if cur, ok := si.files[p]; ok && cur.MTime == mtime && cur.Size == size {
			// Looks unchanged by the cheap key. If we have a stored content hash,
			// re-verify it (catches a same-second, same-size overwrite). An entry
			// from before content hashing has Content=="" — treat it as current to
			// avoid a one-time full re-embed of the whole store on upgrade.
			if cur.Content != "" {
				verify = append(verify, e)
				prevContent[p] = cur.Content
			}
			continue
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

	// Content-hash the ambiguous set (lock-free reads). A changed hash promotes
	// the file into toEmbed; an unreadable file (hash "") is left as-is.
	for _, e := range verify {
		if cerr := ctx.Err(); cerr != nil {
			_ = si.persistIfMutated(removed > 0)
			return stats, cerr
		}
		h := contentHashFor(ctx, store, e.PathDisplay, e.Size)
		if h != "" && h != prevContent[e.PathDisplay] {
			toEmbed = append(toEmbed, e)
		}
	}

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
// returned (warm-start cost only). save() snapshots under the lock and writes
// outside it, so this must be called WITHOUT mu held.
func (si *SemanticIndex) persistIfMutated(mutated bool) error {
	if !mutated {
		return nil
	}
	return si.save()
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
	content := contentHashOfBytes(data, e.Size)
	text := extractFn(ctx, data, e.Name)
	chunks := chunkText(text)
	if len(chunks) == 0 {
		// No extractable text: record a fresh, empty entry so we don't re-extract
		// this unchanged file every pass (the mtime/size/content key marks it current).
		mtime, size := freshKey(e)
		return &fileEntry{Path: e.PathDisplay, MTime: mtime, Size: size, Content: content}, true
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
	return &fileEntry{Path: e.PathDisplay, MTime: mtime, Size: size, Content: content, Chunks: out}, true
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

	// Snapshot the entry pointers under the lock, then run the O(all chunks)
	// cosine scan OUTSIDE it — the scan is pure CPU over immutable chunk vectors,
	// so holding mu across it would needlessly block concurrent reindex writes.
	// Entries are replaced wholesale (never mutated in place), so the retained
	// pointers stay valid even if the map mutates after the snapshot.
	si.mu.Lock()
	entries := make([]*fileEntry, 0, len(si.files))
	for _, fe := range si.files {
		entries = append(entries, fe)
	}
	si.mu.Unlock()

	type scored struct {
		path    string
		size    int64
		mtime   string
		score   float64
		snippet string
	}
	hits := make([]scored, 0, len(entries))
	for _, fe := range entries {
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
		hits = append(hits, scored{path: fe.Path, size: fe.Size, mtime: fe.MTime, score: best, snippet: bestSnip})
	}

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
