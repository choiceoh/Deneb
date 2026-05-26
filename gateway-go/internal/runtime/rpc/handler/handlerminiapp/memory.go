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
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// MemorySearcher is the subset of *wiki.Store the handler needs. Defined
// here so tests can drop in a fake without spinning up the real store.
type MemorySearcher interface {
	Search(ctx context.Context, query string, limit int) ([]wiki.SearchResult, error)
	ReadPage(relPath string) (*wiki.Page, error)
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
)

// MemoryMethods returns the miniapp.memory.* handler map. Returns nil if
// no store factory is provided so method_registry can register
// conditionally.
func MemoryMethods(deps MemoryDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.memory.search":   memorySearch(deps),
		"miniapp.memory.get_page": memoryGetPage(deps),
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
