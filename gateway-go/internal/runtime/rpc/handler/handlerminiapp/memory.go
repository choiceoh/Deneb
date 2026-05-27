// memory.go — miniapp.memory.* RPC handlers (currently just search).
//
// Wraps the wiki package's full-text search and enriches each hit with the
// page's title and summary so the Mini App can show a useful card row
// without a follow-up RPC per result.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemorySearcher is the subset of *wiki.Store the handler needs. Defined
// here so tests can drop in a fake without spinning up the real store.
// Name kept for compatibility — it now covers listing/stats/diary AND
// writes too, not strictly searching. *wiki.Store satisfies all of
// these naturally; tests provide a fake.
type MemorySearcher interface {
	Search(ctx context.Context, query string, limit int) ([]wiki.SearchResult, error)
	ReadPage(relPath string) (*wiki.Page, error)
	WritePage(relPath string, page *wiki.Page) error
	Stats() wiki.StoreStats
	ListPages(category string) ([]string, error)
	RecentDiaryEntries(limit int) []wiki.DiaryHit
}

// MemoryDeps holds the wiki store and is consumed at registration time.
// Store is a lazy factory so the gateway boots cleanly when the wiki
// knowledge base is disabled (per-config `wiki.enabled=false`); the
// handlers then surface UNAVAILABLE per call instead of crashing at boot.
type MemoryDeps struct {
	Store func() (MemorySearcher, error)
}

const (
	defaultMemorySearchLimit = 10
	maxMemorySearchLimit     = 50
	maxMemorySnippetChars    = 240

	// Caps for the new listing endpoints. The Mini App is mobile-first
	// so default page sizes stay small; ceilings protect the gateway
	// from a misbehaving client asking for "everything".
	defaultMemoryListLimit = 50
	maxMemoryListLimit     = 200
	defaultDiaryRecent     = 20
	maxDiaryRecent         = 100
	maxDiarySnippetChars   = 200
)

// MemoryMethods returns the miniapp.memory.* handler map. Returns nil if
// no store factory is provided so method_registry can register
// conditionally.
func MemoryMethods(deps MemoryDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.memory.search":           memorySearch(deps),
		"miniapp.memory.get_page":         memoryGetPage(deps),
		"miniapp.memory.write_page":       memoryWritePage(deps),
		"miniapp.memory.categories":       memoryCategories(deps),
		"miniapp.memory.list_in_category": memoryListInCategory(deps),
		"miniapp.memory.diary_recent":     memoryDiaryRecent(deps),
	}
}

// memoryGetPage returns the full body + frontmatter of a single wiki page.
// Used by the Mini App's wiki detail view when a memory search hit or a
// sender-context chip is tapped.
func memoryGetPage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	type out struct {
		Path       string   `json:"path"`
		Title      string   `json:"title,omitempty"`
		Summary    string   `json:"summary,omitempty"`
		Category   string   `json:"category,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Related    []string `json:"related,omitempty"`
		Created    string   `json:"created,omitempty"`
		Updated    string   `json:"updated,omitempty"`
		Due        string   `json:"due,omitempty"`
		Importance float64  `json:"importance,omitempty"`
		Body       string   `json:"body"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		rel := strings.TrimSpace(p.Path)
		if rel == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		// Reject anything that could let the caller escape the wiki
		// root. Substring ".." alone is too weak — Store.ReadPage
		// does filepath.Join(s.dir, rel) so absolute paths and "../"
		// segments both need explicit guards. Backslash normalized to
		// forward slash before cleaning so Windows-style traversal
		// can't sneak past path.Clean.
		if err := validateWikiPath(rel); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		page, err := store.ReadPage(rel)
		if err != nil {
			// Distinguish "missing" from "unreadable" so the Mini App
			// surfaces the right banner — permission/IO errors used to
			// be misreported as NOT_FOUND and the client gave up.
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}
		if page == nil {
			return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, out{
			Path:       rel,
			Title:      page.Meta.Title,
			Summary:    page.Meta.Summary,
			Category:   page.Meta.Category,
			Tags:       page.Meta.Tags,
			Related:    page.Meta.Related,
			Created:    page.Meta.Created,
			Updated:    page.Meta.Updated,
			Due:        page.Meta.Due,
			Importance: page.Meta.Importance,
			Body:       page.Body,
		})
	}
}

func memorySearch(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	type hitOut struct {
		Path     string  `json:"path"`
		Title    string  `json:"title,omitempty"`
		Summary  string  `json:"summary,omitempty"`
		Category string  `json:"category,omitempty"`
		Snippet  string  `json:"snippet"`
		Score    float64 `json:"score"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultMemorySearchLimit
		}
		if limit > maxMemorySearchLimit {
			limit = maxMemorySearchLimit
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory search unavailable", err).Response(req.ID)
		}
		results, err := store.Search(ctx, query, limit)
		if err != nil {
			return rpcerr.WrapUnavailable("memory search failed", err).Response(req.ID)
		}

		out := make([]hitOut, 0, len(results))
		for _, r := range results {
			row := hitOut{
				Path:    r.Path,
				Snippet: truncateRunes(r.Content, maxMemorySnippetChars),
				Score:   r.Score,
			}
			// Best-effort title/summary lookup. If reading the page
			// fails, fall through — Path + Snippet are still useful.
			if page, perr := store.ReadPage(r.Path); perr == nil && page != nil {
				row.Title = page.Meta.Title
				row.Summary = page.Meta.Summary
				row.Category = page.Meta.Category
			}
			out = append(out, row)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"results": out})
	}
}

// memoryWritePage replaces the body of an existing wiki page while
// preserving its frontmatter (title/summary/category/tags/related/...).
// Updated date is stamped to today so the Mini App's "갱신 N일 전"
// labels reflect the edit.
//
// Single-operator deployment, no versioning / optimistic locking —
// the underlying wiki.Store is the source of truth and last-write-wins.
// If we ever multi-tenant, this needs an etag-style guard; for now the
// risk is the operator clobbering their own pending edit, which is
// recoverable from git history.
func memoryWritePage(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
		Body string `json:"body"`
	}
	type out struct {
		Path       string   `json:"path"`
		Title      string   `json:"title,omitempty"`
		Summary    string   `json:"summary,omitempty"`
		Category   string   `json:"category,omitempty"`
		Tags       []string `json:"tags,omitempty"`
		Related    []string `json:"related,omitempty"`
		Created    string   `json:"created,omitempty"`
		Updated    string   `json:"updated,omitempty"`
		Due        string   `json:"due,omitempty"`
		Importance float64  `json:"importance,omitempty"`
		Body       string   `json:"body"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		rel := strings.TrimSpace(p.Path)
		if rel == "" {
			return rpcerr.MissingParam("path").Response(req.ID)
		}
		if err := validateWikiPath(rel); err != nil {
			return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}

		// ReadPage to preserve existing frontmatter. A miss → NOT_FOUND
		// (the Mini App doesn't expose page creation yet — that would
		// need title/category inputs).
		existing, err := store.ReadPage(rel)
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
			}
			return rpcerr.WrapUnavailable("wiki page read failed", err).Response(req.ID)
		}
		if existing == nil {
			return rpcerr.NotFound("wiki page " + rpcutil.TruncateForError(rel)).Response(req.ID)
		}

		// Replace body, bump Updated to today's local date. Frontmatter
		// otherwise untouched.
		existing.Body = p.Body
		existing.Meta.Updated = todayDateString()

		if err := store.WritePage(rel, existing); err != nil {
			return rpcerr.WrapUnavailable("wiki page write failed", err).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, out{
			Path:       rel,
			Title:      existing.Meta.Title,
			Summary:    existing.Meta.Summary,
			Category:   existing.Meta.Category,
			Tags:       existing.Meta.Tags,
			Related:    existing.Meta.Related,
			Created:    existing.Meta.Created,
			Updated:    existing.Meta.Updated,
			Due:        existing.Meta.Due,
			Importance: existing.Meta.Importance,
			Body:       existing.Body,
		})
	}
}

// todayDateString returns YYYY-MM-DD in the gateway's local zone.
// Pulled out of memoryWritePage so the test can swap in a fixed clock.
// Production reads the wall clock directly; tests inject by overriding
// the package-level `nowFunc` (see memory_test.go).
var nowFunc = time.Now

func todayDateString() string {
	return nowFunc().Format("2006-01-02")
}

// MemoryCategoryRow is a single category entry exposed via
// miniapp.memory.categories. Defined at package scope so the sort helper
// can refer to it without a generic wrapper.
type MemoryCategoryRow struct {
	Name      string `json:"name"`
	PageCount int    `json:"pageCount"`
}

// MemoryPageRow is one row in a category page listing. Same scoping
// reason as MemoryCategoryRow.
type MemoryPageRow struct {
	Path    string `json:"path"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary,omitempty"`
	Updated string `json:"updated,omitempty"`
}

// memoryCategories returns the list of wiki categories with page counts.
// Used by the Mini App's 더보기 > 📂 카테고리 explorer so the user can
// browse pages even when they don't have a search query in mind.
func memoryCategories(deps MemoryDeps) rpcutil.HandlerFunc {
	type out struct {
		Categories []MemoryCategoryRow `json:"categories"`
		TotalPages int                 `json:"totalPages"`
		TotalBytes int64               `json:"totalBytes"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		stats := store.Stats()
		cats := make([]MemoryCategoryRow, 0, len(stats.CategoryCount))
		for name, count := range stats.CategoryCount {
			cats = append(cats, MemoryCategoryRow{Name: name, PageCount: count})
		}
		// Sort: page count desc, then name asc for stability so the
		// largest buckets bubble up but ties stay deterministic.
		sort.Slice(cats, func(i, j int) bool {
			if cats[i].PageCount != cats[j].PageCount {
				return cats[i].PageCount > cats[j].PageCount
			}
			return cats[i].Name < cats[j].Name
		})
		return rpcutil.RespondOK(req.ID, out{
			Categories: cats,
			TotalPages: stats.TotalPages,
			TotalBytes: stats.TotalBytes,
		})
	}
}

// memoryListInCategory lists pages within a category, returning enough
// metadata (title, summary, updated) for the Mini App to render a list
// card without a per-row ReadPage round-trip. Empty category name lists
// all pages — used as a fallback when the user picks the (root) bucket.
func memoryListInCategory(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Category string `json:"category,omitempty"`
		Limit    int    `json:"limit,omitempty"`
	}
	type out struct {
		Category string          `json:"category"`
		Pages    []MemoryPageRow `json:"pages"`
		Total    int             `json:"total"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		// Category is optional but must validate as a relative subpath
		// when present — same threat model as get_page's path check.
		// Special-case "(root)" the way Stats reports the unrooted
		// bucket so the client can round-trip it back here.
		cat := strings.TrimSpace(p.Category)
		listArg := cat
		if cat == "(root)" {
			listArg = ""
		}
		if listArg != "" {
			if err := validateWikiPath(listArg); err != nil {
				return rpcerr.InvalidRequest(err.Error()).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultMemoryListLimit
		}
		if limit > maxMemoryListLimit {
			limit = maxMemoryListLimit
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		paths, err := store.ListPages(listArg)
		if err != nil {
			return rpcerr.WrapUnavailable("list pages failed", err).Response(req.ID)
		}
		total := len(paths)
		if len(paths) > limit {
			paths = paths[:limit]
		}

		pages := make([]MemoryPageRow, 0, len(paths))
		for _, rel := range paths {
			row := MemoryPageRow{Path: rel}
			// Best-effort enrich; on failure show path only.
			if page, perr := store.ReadPage(rel); perr == nil && page != nil {
				row.Title = page.Meta.Title
				row.Summary = page.Meta.Summary
				row.Updated = page.Meta.Updated
			}
			pages = append(pages, row)
		}
		// Stable order: updated desc (newest first), then title asc as
		// tiebreaker. Pages without `updated` sink to the bottom.
		sort.Slice(pages, func(i, j int) bool {
			ai, aj := pages[i].Updated, pages[j].Updated
			if ai == aj {
				return pages[i].Title < pages[j].Title
			}
			if ai == "" {
				return false
			}
			if aj == "" {
				return true
			}
			return ai > aj
		})
		return rpcutil.RespondOK(req.ID, out{
			Category: cat,
			Pages:    pages,
			Total:    total,
		})
	}
}

// memoryDiaryRecent returns the most recent diary entries. Mini App
// renders these as a vertical timeline — Deneb writes a daily diary as
// part of normal operation, so this is the "what's been happening lately
// in my world" view.
func memoryDiaryRecent(deps MemoryDeps) rpcutil.HandlerFunc {
	type params struct {
		Limit int `json:"limit,omitempty"`
	}
	type entryOut struct {
		File    string `json:"file"`
		Header  string `json:"header"`
		Content string `json:"content"`
		At      int64  `json:"at,omitempty"`
	}
	type out struct {
		Entries []entryOut `json:"entries"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		var p params
		if len(req.Params) > 0 {
			if err := json.Unmarshal(req.Params, &p); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultDiaryRecent
		}
		if limit > maxDiaryRecent {
			limit = maxDiaryRecent
		}

		store, err := deps.Store()
		if err != nil {
			return rpcerr.WrapUnavailable("memory store unavailable", err).Response(req.ID)
		}
		hits := store.RecentDiaryEntries(limit)
		entries := make([]entryOut, 0, len(hits))
		for _, h := range hits {
			entries = append(entries, entryOut{
				File:    h.File,
				Header:  h.Header,
				Content: truncateRunes(h.Content, maxDiarySnippetChars),
				At:      h.At,
			})
		}
		return rpcutil.RespondOK(req.ID, out{Entries: entries})
	}
}

// truncateRunes clips s to maxChars Unicode code points and appends an
// ellipsis when truncated. Rune-aware so a Korean snippet doesn't get cut
// mid-character.
func truncateRunes(s string, maxChars int) string {
	runes := []rune(s)
	if len(runes) <= maxChars {
		return s
	}
	return string(runes[:maxChars]) + "…"
}

// validateWikiPath rejects any rel value that could let a caller of
// miniapp.memory.get_page read outside the wiki directory. The wiki
// store does filepath.Join(dir, rel) which preserves an embedded
// absolute path on some platforms and joins relative paths normally —
// so we (a) reject absolute forms outright, (b) normalize backslashes
// to forward slashes, (c) Clean the result, and (d) ensure the cleaned
// path stays under the root.
func validateWikiPath(rel string) error {
	if strings.HasPrefix(rel, "/") || strings.HasPrefix(rel, "\\") {
		return errors.New("path must be relative to the wiki root")
	}
	// Windows-style C:\foo or D:\bar — reject up front so path.Clean
	// can't normalize the drive letter away.
	if len(rel) >= 2 && rel[1] == ':' {
		return errors.New("path must be relative to the wiki root")
	}
	normalized := strings.ReplaceAll(rel, "\\", "/")
	cleaned := path.Clean(normalized)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return errors.New("path must stay within the wiki root")
	}
	return nil
}
