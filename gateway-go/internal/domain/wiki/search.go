// search.go — SQLite FTS5-based full-text search for wiki pages.
// Replaces ripgrep with a Go-native solution using modernc.org/sqlite.
package wiki

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // register sqlite3 driver
)

// SearchResult is a single search hit.
type SearchResult struct {
	Path    string  // relative path within wiki dir
	Line    int     // always 0 for FTS (line-level matching not available)
	Content string  // matching snippet
	Score   float64 // relevance score (0-1)
}

// searchDB manages the FTS5 index for wiki pages.
type searchDB struct {
	db   *sql.DB
	mu   sync.RWMutex
	done chan struct{} // closed on Close to stop checkpoint goroutine
}

const ftsSchema = `
CREATE TABLE IF NOT EXISTS wiki_pages (
	path    TEXT PRIMARY KEY,
	title   TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT ''
);
CREATE VIRTUAL TABLE IF NOT EXISTS wiki_fts USING fts5(
	path, title, content,
	content='wiki_pages',
	content_rowid='rowid',
	tokenize='unicode61 remove_diacritics 2'
);
CREATE TRIGGER IF NOT EXISTS wiki_pages_ai AFTER INSERT ON wiki_pages BEGIN
	INSERT INTO wiki_fts(rowid, path, title, content) VALUES (new.rowid, new.path, new.title, new.content);
END;
CREATE TRIGGER IF NOT EXISTS wiki_pages_ad AFTER DELETE ON wiki_pages BEGIN
	INSERT INTO wiki_fts(wiki_fts, rowid, path, title, content) VALUES('delete', old.rowid, old.path, old.title, old.content);
END;
CREATE TRIGGER IF NOT EXISTS wiki_pages_au AFTER UPDATE ON wiki_pages BEGIN
	INSERT INTO wiki_fts(wiki_fts, rowid, path, title, content) VALUES('delete', old.rowid, old.path, old.title, old.content);
	INSERT INTO wiki_fts(rowid, path, title, content) VALUES (new.rowid, new.path, new.title, new.content);
END;
`

func newSearchDB(dir string) (*searchDB, error) {
	dbPath := filepath.Join(dir, ".wiki.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("wiki: open search db: %w", err)
	}
	if _, err := db.ExecContext(context.Background(), ftsSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("wiki: init fts schema: %w", err)
	}
	s := &searchDB{db: db, done: make(chan struct{})}
	go s.periodicCheckpoint()
	return s, nil
}

// indexPage upserts a page into the FTS index.
func (s *searchDB) indexPage(relPath string, page *Page) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO wiki_pages (path, title, content) VALUES (?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET title=excluded.title, content=excluded.content`,
		relPath, page.Meta.Title, page.Body,
	)
	return err
}

// removePage removes a page from the FTS index.
func (s *searchDB) removePage(relPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM wiki_pages WHERE path = ?`, relPath)
	return err
}

// search runs an FTS5 query and returns scored results.
func (s *searchDB) search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tokens := splitTokens(query)
	if len(tokens) == 0 {
		return nil, nil
	}

	// Try AND first for precision, fall back to OR for recall.
	ftsQuery := buildFTSQuery(tokens, "AND")
	results, err := s.runQuery(ctx, ftsQuery, limit)
	if err != nil || len(results) == 0 {
		ftsQuery = buildFTSQuery(tokens, "OR")
		results, err = s.runQuery(ctx, ftsQuery, limit)
	}
	return results, err
}

func (s *searchDB) runQuery(ctx context.Context, ftsQuery string, limit int) ([]SearchResult, error) {
	if ftsQuery == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT path, snippet(wiki_fts, 2, '', '', '...', 40), rank
		 FROM wiki_fts WHERE wiki_fts MATCH ?
		 ORDER BY rank LIMIT ?`,
		ftsQuery, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		var rank float64
		if err := rows.Scan(&r.Path, &r.Content, &rank); err != nil {
			continue
		}
		r.Score = rankToScore(rank)
		results = append(results, r)
	}
	return results, rows.Err()
}

func (s *searchDB) close() error {
	close(s.done)
	// Final checkpoint before closing to reclaim WAL space.
	s.checkpoint()
	return s.db.Close()
}

// checkpoint runs a WAL TRUNCATE checkpoint to reclaim disk space.
func (s *searchDB) checkpoint() {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)") // best-effort: WAL compaction is non-critical
}

// periodicCheckpoint runs a WAL checkpoint every 30 minutes.
func (s *searchDB) periodicCheckpoint() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.checkpoint()
		}
	}
}

// rebuildIndex scans all wiki pages and rebuilds the FTS index from scratch.
func (s *searchDB) rebuildIndex(dir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing data.
	if _, err := s.db.ExecContext(context.Background(), `DELETE FROM wiki_pages`); err != nil {
		return err
	}

	// Walk all .md files.
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil //nolint:nilerr // skip inaccessible entries and directories in walk
		}
		if filepath.Ext(path) != ".md" {
			return nil
		}
		base := filepath.Base(path)
		if base == "index.md" || base == "_index.md" || base == ".wiki.db" || base == "log.md" {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		page, err := ParsePageFile(path)
		if err != nil {
			return nil //nolint:nilerr // skip unparseable files
		}
		_, err = s.db.ExecContext(context.Background(),
			`INSERT INTO wiki_pages (path, title, content) VALUES (?, ?, ?)
			 ON CONFLICT(path) DO UPDATE SET title=excluded.title, content=excluded.content`,
			rel, page.Meta.Title, page.Body,
		)
		return err
	})
}

// Search runs a full-text search across wiki pages.
func (s *Store) Search(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s.fts == nil {
		return nil, fmt.Errorf("wiki: search not available")
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

// FTS5 query building (adapted from memory/search.go).

func buildFTSQuery(tokens []string, op string) string {
	if len(tokens) == 0 {
		return ""
	}
	var escaped []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if containsHangul(t) {
			escaped = append(escaped, t+"*")
		} else {
			escaped = append(escaped, `"`+t+`"`)
		}
	}
	if len(escaped) == 0 {
		return ""
	}
	return strings.Join(escaped, " "+op+" ")
}

func splitTokens(s string) []string {
	fields := strings.Fields(s)
	var tokens []string
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" {
			tokens = append(tokens, f)
		}
	}
	return tokens
}

func containsHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

func rankToScore(rank float64) float64 {
	if rank >= 0 {
		return 0
	}
	return 1.0 / (1.0 + math.Exp(rank))
}
