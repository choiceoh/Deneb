// diary_search.go — In-memory full-text search over diary entries.
//
// Each diary file (`diary-YYYY-MM-DD.md`) is split into per-timestamp
// entries (the "## HH:MM" sections written by AppendDiary). Each entry
// becomes one document in a textsearch.Index, so BM25 ranking — not naive
// substring scan — drives recall preflight diary evidence.
//
// Why per-entry instead of per-file: a single diary file may cover dozens
// of unrelated topics. BM25 over the whole-file blob hides relevant entries
// behind the noise of the rest of the day. Per-entry indexing also lets us
// time-weight scores: a 13-minute-old note about topic X should outrank a
// 60-day-old note that matched a few more keywords.
//
// Recency weighting uses a 14-day half-life with a 50% score floor so even
// very old entries remain reachable for a strong textual match.
package wiki

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// DiaryHit is a single matching diary entry.
type DiaryHit struct {
	File    string  // e.g., "diary-2026-05-16.md"
	Header  string  // section header (typically "HH:MM")
	Content string  // entry body
	At      int64   // unix millis derived from filename + header (0 if unparseable)
	Score   float64 // BM25 × recency weight
	Snippet string  // text excerpt with match context
}

// diaryEntryMeta is the cached metadata for one indexed entry.
type diaryEntryMeta struct {
	File    string
	Header  string
	Content string
	At      int64
}

type diarySearchDB struct {
	idx *textsearch.Index

	mu   sync.RWMutex
	meta map[string]*diaryEntryMeta // docID -> entry metadata
}

func newDiarySearchDB() *diarySearchDB {
	return &diarySearchDB{
		idx:  textsearch.New(),
		meta: make(map[string]*diaryEntryMeta),
	}
}

// diaryDocID encodes filename + header into a unique doc ID. Two entries
// in the same minute would collide; AppendDiaryTo uses HH:MM precision so
// collisions are possible in theory but rare in practice (single user,
// sequential writes).
func diaryDocID(file, header string) string {
	return file + "#" + header
}

// upsertEntry indexes (or replaces) one diary entry. Safe for concurrent
// use because both meta and idx have their own internal locking.
func (d *diarySearchDB) upsertEntry(file, header, content string, at int64) {
	if content == "" {
		return
	}
	id := diaryDocID(file, header)
	d.mu.Lock()
	d.meta[id] = &diaryEntryMeta{File: file, Header: header, Content: content, At: at}
	d.mu.Unlock()
	d.idx.Upsert(id, content)
}

// rebuildFromDir scans every diary-YYYY-MM-DD.md file in dir and reindexes
// every entry. Safe to call repeatedly.
func (d *diarySearchDB) rebuildFromDir(dir string) error {
	if dir == "" {
		return nil
	}
	d.mu.Lock()
	d.meta = make(map[string]*diaryEntryMeta)
	d.mu.Unlock()
	d.idx.Clear()

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, "diary-") || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		for _, entry := range parseDiaryFile(name, string(data)) {
			d.upsertEntry(entry.File, entry.Header, entry.Content, entry.At)
		}
	}
	return nil
}

// search runs a BM25 query and returns recency-weighted hits.
func (d *diarySearchDB) search(ctx context.Context, query string, limit int) ([]DiaryHit, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if ctx != nil {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
	}
	// Overfetch so recency re-ranking has something to work with.
	rawHits := d.idx.Search(query, limit*3)
	if len(rawHits) == 0 {
		return nil, nil
	}

	now := time.Now().UnixMilli()
	d.mu.RLock()
	out := make([]DiaryHit, 0, len(rawHits))
	for _, h := range rawHits {
		m, ok := d.meta[h.ID]
		if !ok {
			continue
		}
		out = append(out, DiaryHit{
			File:    m.File,
			Header:  m.Header,
			Content: m.Content,
			At:      m.At,
			Score:   recencyWeightedScore(h.Score, m.At, now),
			Snippet: h.Snippet,
		})
	}
	d.mu.RUnlock()

	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// recentEntries returns the N most recent diary entries, ignoring query
// content. Used as a "vague-cue" fallback when the user message has no
// signal terms ("그거 뭐였지?") so recall preflight has something to show.
func (d *diarySearchDB) recentEntries(limit int) []DiaryHit {
	if limit <= 0 {
		return nil
	}
	d.mu.RLock()
	out := make([]DiaryHit, 0, len(d.meta))
	for _, m := range d.meta {
		out = append(out, DiaryHit{
			File:    m.File,
			Header:  m.Header,
			Content: m.Content,
			At:      m.At,
		})
	}
	d.mu.RUnlock()

	sort.Slice(out, func(i, j int) bool { return out[i].At > out[j].At })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// parseDiaryFile splits a diary file body on "## " section headers and
// returns one entry per section. Empty sections are dropped.
func parseDiaryFile(fileName, content string) []*diaryEntryMeta {
	// Prefix with a newline so the first section also splits cleanly.
	chunks := strings.Split("\n"+content, "\n## ")
	var entries []*diaryEntryMeta
	for _, chunk := range chunks[1:] {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		header, body, _ := strings.Cut(chunk, "\n")
		body = strings.TrimSpace(body)
		if body == "" {
			continue
		}
		entries = append(entries, &diaryEntryMeta{
			File:    fileName,
			Header:  strings.TrimSpace(header),
			Content: body,
			At:      diaryEntryUnixMillis(fileName, header),
		})
	}
	return entries
}

// diaryEntryUnixMillis resolves filename + header into a unix-millis
// timestamp. Returns 0 if the inputs are not parseable.
func diaryEntryUnixMillis(fileName, header string) int64 {
	date := strings.TrimSuffix(strings.TrimPrefix(fileName, "diary-"), ".md")
	ts, err := time.ParseInLocation("2006-01-02 15:04", date+" "+strings.TrimSpace(header), time.Local)
	if err != nil {
		return 0
	}
	return ts.UnixMilli()
}

// recencyWeightedScore boosts recent entries vs older ones. 14-day half-life
// keeps a generous tail — even a month-old strong match still ranks above a
// fresh weak match.
func recencyWeightedScore(bm25 float64, entryAt, nowMillis int64) float64 {
	if entryAt <= 0 {
		// Unknown timestamps are not penalized below the floor — they
		// probably indicate a parsing edge case, not stale content.
		return bm25 * 0.75
	}
	days := float64(nowMillis-entryAt) / float64(24*60*60*1000)
	if days < 0 {
		days = 0
	}
	recency := math.Pow(0.5, days/14.0)
	// Floor at 50% so very old entries are still reachable for a strong match.
	return bm25 * (0.5 + 0.5*recency)
}
