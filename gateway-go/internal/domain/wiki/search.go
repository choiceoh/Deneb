// search.go — In-memory full-text search for wiki pages.
// Replaces SQLite FTS5 with a pure Go textsearch index.
package wiki

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

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
}

func newSearchDB() *searchDB {
	return &searchDB{idx: textsearch.New()}
}

// indexPage upserts a page into the search index.
func (s *searchDB) indexPage(relPath string, page *Page) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Upsert(relPath, searchablePageFields(page)...)
}

// removePage removes a page from the search index.
func (s *searchDB) removePage(relPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.idx.Remove(relPath)
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

// Search runs a full-text search across wiki pages.
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
	return s.fts.search(ctx, query, limit)
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
