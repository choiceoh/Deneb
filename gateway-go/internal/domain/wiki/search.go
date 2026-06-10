// search.go — In-memory full-text search for wiki pages.
// Replaces SQLite FTS5 with a pure Go textsearch index.
package wiki

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/textsearch"
)

// SearchResult is a single search hit.
type SearchResult struct {
	Path    string  // relative path within wiki dir
	Line    int     // always 0 (line-level matching not available)
	Content string  // matching snippet
	Score   float64 // relevance score (0-1)
}

// searchDB manages the in-memory FTS index for wiki pages.
type searchDB struct {
	idx *textsearch.Index
	mu  sync.RWMutex
	// validity holds the per-page staleness factor (see validityFactor),
	// computed when the page is (re)indexed. Search multiplies scores by it
	// so archived/superseded/aging facts stop outranking current ones.
	validity map[string]float64
}

func newSearchDB() *searchDB {
	return &searchDB{idx: textsearch.New(), validity: make(map[string]float64)}
}

// indexPage upserts a page into the search index.
func (s *searchDB) indexPage(relPath string, page *Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Upsert(relPath, searchablePageFields(page)...)
	if page != nil {
		s.validity[relPath] = validityFactor(page.Meta, time.Now())
	}
}

// removePage removes a page from the search index.
func (s *searchDB) removePage(relPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Remove(relPath)
	delete(s.validity, relPath)
}

// validityFactor scores how current a page's facts are (0–1]. Archived and
// superseded pages keep working for direct reads but should not outrank
// living pages in recall; old "updated" stamps decay gently — operational
// facts (ports, prices, configs) rot, and recall presenting a year-old fact
// as current is exactly the failure this guards against.
func validityFactor(meta Frontmatter, now time.Time) float64 {
	f := 1.0
	if meta.Archived {
		f *= 0.3
	}
	if meta.SupersededBy != "" {
		f *= 0.5
	}
	if meta.Updated != "" {
		if t, err := time.Parse("2006-01-02", meta.Updated); err == nil {
			switch age := now.Sub(t); {
			case age > 365*24*time.Hour:
				f *= 0.7
			case age > 180*24*time.Hour:
				f *= 0.85
			}
		}
	}
	return f
}

// applyValidity multiplies result scores by each page's validity factor and
// re-sorts. Pages never indexed (factor missing) pass through unchanged.
func (s *searchDB) applyValidity(results []SearchResult) []SearchResult {
	if len(results) == 0 {
		return results
	}
	s.mu.RLock()
	for i := range results {
		if f, ok := s.validity[results[i].Path]; ok && f < 1.0 {
			results[i].Score *= f
		}
	}
	s.mu.RUnlock()
	sort.SliceStable(results, func(a, b int) bool {
		if results[a].Score != results[b].Score {
			return results[a].Score > results[b].Score
		}
		return results[a].Path < results[b].Path
	})
	return results
}

// search runs a full-text query and returns scored results.
func (s *searchDB) search(_ context.Context, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	hits := s.idx.Search(query, limit)
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{
			Path:    h.ID,
			Content: h.Snippet,
			Score:   scoreToNormalized(h.Score),
		}
	}
	return results, nil
}

// rebuildIndex clears and rebuilds the index from all .md files in dir.
func (s *searchDB) rebuildIndex(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.idx.Clear()

	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries and directories in walk
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		page, err := ParsePageFile(path)
		if err != nil {
			// An unparseable page is functionally deleted: it stays on disk but
			// never appears in search again. Surface it instead of hiding it.
			slog.Warn("wiki: unparseable page skipped during search index rebuild",
				"path", rel, "error", err)
			return nil //nolint:nilerr // skip unparseable files
		}
		s.idx.Upsert(rel, searchablePageFields(page)...)
		return nil
	})
}

func searchablePageFields(page *Page) []string {
	if page == nil {
		return nil
	}
	return []string{
		page.Meta.Title,
		page.Meta.Summary,
		page.Meta.ID,
		page.Meta.Category,
		strings.Join(page.Meta.Tags, " "),
		strings.Join(page.Meta.Related, " "),
		page.Body,
	}
}

// close is a no-op (in-memory index, nothing to close).
func (s *searchDB) close() error {
	return nil
}

// Search runs a search across wiki pages. With no embedder configured it is
// pure BM25 (exact prior behavior). When a semantic index is attached and the
// embedding server is healthy, it blends BM25 with dense-vector neighbors so a
// query also finds pages by meaning. Semantic degradation (server down, embed
// error) silently falls back to the BM25 result.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s.fts == nil {
		return nil, nil
	}
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	bm25, err := s.fts.search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	sem := s.searchSemantic(ctx, query, limit)
	if len(sem) == 0 {
		return s.fts.applyValidity(bm25), nil
	}
	return s.fts.applyValidity(mergeSearchResults(bm25, sem, limit)), nil
}

// mergeSearchResults blends lexical (BM25) and semantic hits. Each list's score
// is already 0-1; a page in both gets the higher single score plus a small
// agreement bonus, so pages found by *both* signals rise to the top. BM25
// snippets are preserved (semantic-only hits have no snippet). Order is by
// blended score, descending; ties broken by path for determinism.
func mergeSearchResults(bm25, sem []SearchResult, limit int) []SearchResult {
	type merged struct {
		res   SearchResult
		score float64
	}
	byPath := make(map[string]*merged, len(bm25)+len(sem))
	add := func(r SearchResult, bonus float64) {
		m := byPath[r.Path]
		if m == nil {
			byPath[r.Path] = &merged{res: r, score: r.Score}
			return
		}
		if r.Score > m.score {
			m.score = r.Score
		}
		m.score += bonus // agreement: seen by both signals
		if m.res.Content == "" && r.Content != "" {
			m.res.Content = r.Content
		}
	}
	for _, r := range bm25 {
		add(r, 0)
	}
	for _, r := range sem {
		add(r, 0.1)
	}

	out := make([]merged, 0, len(byPath))
	for _, m := range byPath {
		m.res.Score = m.score
		out = append(out, *m)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].score != out[b].score {
			return out[a].score > out[b].score
		}
		return out[a].res.Path < out[b].res.Path
	})
	if len(out) > limit {
		out = out[:limit]
	}
	results := make([]SearchResult, len(out))
	for i := range out {
		results[i] = out[i].res
	}
	return results
}

// SearchFiles returns wiki file paths matching a query.
func (s *Store) SearchFiles(ctx context.Context, query string, limit int) ([]string, error) {
	results, err := s.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(results))
	for i, r := range results {
		paths[i] = r.Path
	}
	return paths, nil
}

// scoreToNormalized converts a raw BM25 score to a 0-1 range.
func scoreToNormalized(score float64) float64 {
	if score <= 0 {
		return 0
	}
	// Sigmoid normalization: maps [0, +inf) to (0, 1).
	return score / (score + 1)
}
