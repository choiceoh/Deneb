package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

// wikiAdapter exposes the curated wiki Store as a knowledge backend.
type wikiAdapter struct {
	store *wiki.Store
}

// NewWikiAdapter wraps an initialized wiki.Store. Returns nil when store is
// nil so Router can ignore an unconfigured backend.
func NewWikiAdapter(store *wiki.Store) Adapter {
	if store == nil {
		return nil
	}
	return &wikiAdapter{store: store}
}

func (a *wikiAdapter) Layer() Layer { return LayerWiki }

func (a *wikiAdapter) Recall(ctx context.Context, query string, limit int) ([]Result, error) {
	hits, err := a.store.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		out = append(out, Result{
			Ref:     Ref{Layer: LayerWiki, ID: h.Path},
			Snippet: h.Content,
			Score:   h.Score,
		})
	}
	return out, nil
}

func (a *wikiAdapter) Read(_ context.Context, id string) (*Document, error) {
	page, err := a.store.ReadPage(id)
	if err != nil {
		return nil, fmt.Errorf("read wiki page %q: %w", id, err)
	}
	meta := map[string]string{}
	if page.Meta.Category != "" {
		meta["category"] = page.Meta.Category
	}
	if page.Meta.Type != "" {
		meta["type"] = page.Meta.Type
	}
	if page.Meta.Summary != "" {
		meta["summary"] = page.Meta.Summary
	}
	if len(page.Meta.Tags) > 0 {
		meta["tags"] = strings.Join(page.Meta.Tags, ", ")
	}
	if len(page.Meta.Related) > 0 {
		meta["related"] = strings.Join(page.Meta.Related, ", ")
	}
	if page.Meta.Updated != "" {
		meta["updated"] = page.Meta.Updated
	}
	return &Document{
		Ref:     Ref{Layer: LayerWiki, ID: id},
		Title:   page.Meta.Title,
		Content: page.Body,
		Meta:    meta,
	}, nil
}

func (a *wikiAdapter) Record(_ context.Context, opts RecordOptions) (Ref, error) {
	if strings.TrimSpace(opts.Page) == "" {
		return Ref{}, fmt.Errorf("page is required for knowledge.record")
	}
	path := opts.Page

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		// Derive from the last path segment.
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			title = path[i+1:]
		} else {
			title = path
		}
	}

	existing, _ := a.store.ReadPage(path)
	var page *wiki.Page
	now := time.Now().Format("2006-01-02")
	if existing != nil {
		page = existing
		page.Meta.Title = title
		if opts.Summary != "" {
			page.Meta.Summary = opts.Summary
		}
		if len(opts.Tags) > 0 {
			page.Meta.Tags = opts.Tags
		}
		if len(opts.Related) > 0 {
			page.Meta.Related = opts.Related
		}
		if opts.Importance > 0 {
			page.Meta.Importance = opts.Importance
		}
		if opts.Category != "" {
			page.Meta.Category = opts.Category
		}
		page.Meta.Updated = now
		if opts.Body != "" {
			page.Body = opts.Body
		}
	} else {
		page = wiki.NewPage(title, opts.Category, opts.Tags)
		page.Meta.Summary = opts.Summary
		page.Meta.Related = opts.Related
		if opts.Importance > 0 {
			page.Meta.Importance = opts.Importance
		}
		if opts.Body != "" {
			page.Body = opts.Body
		} else {
			page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성\n",
				title, now)
		}
	}

	if err := a.store.WritePage(path, page); err != nil {
		return Ref{}, fmt.Errorf("write wiki page %q: %w", path, err)
	}
	return Ref{Layer: LayerWiki, ID: path}, nil
}
