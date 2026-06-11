// memory_browse.go — miniapp.memory.* browse-side handlers: category
// listing with page counts, per-category page rows, and recent diary
// entries. Split from memory.go (deps, registration, read handlers,
// shared helpers).
package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemoryCategoryRow is a single category entry exposed via
// miniapp.memory.categories.
//
//deneb:wire
type MemoryCategoryRow struct {
	Name      string `json:"name"`
	PageCount int    `json:"pageCount"`
}

// MemoryPageRow is one row in a category page listing. Same scoping
// reason as MemoryCategoryRow.
//
//deneb:wire
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
