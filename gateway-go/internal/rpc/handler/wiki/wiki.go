// Package wiki provides RPC handlers for wiki.* methods.
//
// Exposes the wiki knowledge base as RPC operations: read, write, delete,
// list, search, index, and stats.
package wiki

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps holds dependencies for wiki.* RPC methods.
type Deps struct {
	Store *wiki.Store
}

// Methods returns all wiki.* RPC handler methods.
// Returns nil if the wiki store is not configured (feature-flagged off).
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"wiki.search": wikiSearch(deps),
		"wiki.read":   wikiRead(deps),
		"wiki.write":  wikiWrite(deps),
		"wiki.delete": wikiDelete(deps),
		"wiki.list":   wikiList(deps),
		"wiki.index":  wikiIndex(deps),
		"wiki.stats":  wikiStats(deps),
	}
}

func wikiSearch(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[params](req)
		if errResp != nil {
			return errResp
		}
		if p.Query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		if p.Limit <= 0 {
			p.Limit = 10
		}
		results, err := deps.Store.Search(ctx, p.Query, p.Limit)
		if err != nil {
			return rpcerr.Unavailable(err.Error()).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]any{"results": results})
	}
}

func wikiRead(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Path    string `json:"path"`
		Section string `json:"section,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Path == "" {
			return nil, rpcerr.MissingParam("path")
		}
		page, err := deps.Store.ReadPage(p.Path)
		if err != nil {
			return nil, rpcerr.NotFound(err.Error())
		}
		result := map[string]any{
			"path": p.Path,
			"meta": page.Meta,
			"body": page.Body,
		}
		if p.Section != "" {
			result["section"] = page.Section(p.Section)
		}
		return result, nil
	})
}

func wikiWrite(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Path       string   `json:"path"`
		Title      string   `json:"title"`
		Category   string   `json:"category,omitempty"`
		Body       string   `json:"body"`
		Tags       []string `json:"tags,omitempty"`
		Importance float64  `json:"importance,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Path == "" {
			return nil, rpcerr.MissingParam("path")
		}
		if p.Title == "" {
			return nil, rpcerr.MissingParam("title")
		}
		page := wiki.NewPage(p.Title, p.Category, p.Tags)
		page.Body = p.Body
		if p.Importance > 0 {
			page.Meta.Importance = p.Importance
		}
		if err := deps.Store.WritePage(p.Path, page); err != nil {
			return nil, rpcerr.Unavailable(err.Error())
		}
		return map[string]any{"ok": true, "path": p.Path}, nil
	})
}

func wikiDelete(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Path string `json:"path"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		if p.Path == "" {
			return nil, rpcerr.MissingParam("path")
		}
		if err := deps.Store.DeletePage(p.Path); err != nil {
			return nil, rpcerr.Unavailable(err.Error())
		}
		return map[string]any{"ok": true, "path": p.Path}, nil
	})
}

func wikiList(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Category string `json:"category,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		pages, err := deps.Store.ListPages(p.Category)
		if err != nil {
			return nil, rpcerr.Unavailable(err.Error())
		}
		return map[string]any{"pages": pages, "count": len(pages)}, nil
	})
}

func wikiIndex(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Category string `json:"category,omitempty"`
	}
	return rpcutil.BindHandler[params](func(p params) (any, error) {
		idx := deps.Store.GetIndex()
		if p.Category == "" {
			return map[string]any{
				"entries":       idx.Entries,
				"lastProcessed": idx.LastProcessed,
				"totalPages":    len(idx.Entries),
			}, nil
		}
		// Filter by category.
		filtered := make(map[string]wiki.IndexEntry)
		for path, entry := range idx.Entries {
			if entry.Category == p.Category {
				filtered[path] = entry
			}
		}
		return map[string]any{
			"entries":    filtered,
			"category":   p.Category,
			"totalPages": len(filtered),
		}, nil
	})
}

func wikiStats(deps Deps) rpcutil.HandlerFunc {
	return rpcutil.BindHandler[struct{}](func(_ struct{}) (any, error) {
		return deps.Store.Stats(), nil
	})
}
