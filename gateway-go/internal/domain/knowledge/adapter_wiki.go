package knowledge

import (
	"context"
	"fmt"
	"os"
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
	// Canonical spelling first (".md" appended when absent) — the layout guard
	// below detects the flat 대표페이지 form by its extension, and the store
	// would apply the same normalization on write anyway.
	path := wiki.NormalizePagePath(opts.Page)

	// Flat-creation guard (wiki-layout 불변식): a NEW flat 프로젝트/<name>.md
	// routes onto its 대표.md slot. Updates of an EXISTING legacy flat page keep
	// writing in place, so only the creation case normalizes — the pre-read
	// decides which case this is (only a definite not-exists counts; a
	// transient read error must not fork a slot sibling of a live flat page).
	// A racing creator is then caught by the atomic UpdatePage below, which
	// re-reads under the write lock.
	if np := wiki.NormalizeProjectPagePath(path); np != path {
		if _, rerr := a.store.ReadPage(path); rerr != nil && os.IsNotExist(rerr) {
			path = np
		}
	}

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		// Derive from the last path segment.
		if i := strings.LastIndexByte(path, '/'); i >= 0 {
			title = path[i+1:]
		} else {
			title = path
		}
	}

	// Atomic read-modify-write: a separate ReadPage→WritePage pair loses any
	// concurrent write landing between the two calls (the dreamer, mail
	// analysis, and the agent wiki tool all target the same pages).
	now := time.Now().Format("2006-01-02")
	err := a.store.UpdatePage(path, func(existing *wiki.Page) (*wiki.Page, error) {
		var page *wiki.Page
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
			return page, nil
		}
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
		return page, nil
	})
	if err != nil {
		return Ref{}, fmt.Errorf("write wiki page %q: %w", path, err)
	}
	for _, old := range opts.Supersedes {
		old = strings.TrimSpace(old)
		if old == "" {
			continue
		}
		if err := a.store.MarkSuperseded(old, path); err != nil {
			return Ref{}, fmt.Errorf("mark superseded %q -> %q: %w", old, path, err)
		}
	}
	return Ref{Layer: LayerWiki, ID: path}, nil
}
