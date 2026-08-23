package knowledge

import (
	"context"
	"fmt"
	"strings"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
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

// Layer identifies the knowledge layer served by the adapter.
func (a *wikiAdapter) Layer() Layer { return LayerWiki }

func (a *wikiAdapter) Descriptor() SourceDescriptor {
	return SourceDescriptor{
		Layer:       LayerWiki,
		Name:        "wiki",
		Description: "curated wiki pages with lexical, semantic, graph, and rerank retrieval",
		Capabilities: []Capability{
			CapabilityLexical, CapabilitySemantic, CapabilityStructured,
			CapabilityGraph, CapabilityRerank, CapabilityLateContext,
		},
		Cost: 2,
		Sync: SyncContract{
			StableID: "workspace-relative page path", Cursor: "write-through index generation",
			ChangeDetection: "page content hash", DeletionDetection: "store delete plus audit tombstone",
			FreshnessTargetMillis: millis(180 * 24 * time.Hour), AuthorizationBoundary: "workspace wiki root",
		},
	}
}

// Recall searches the adapter for knowledge relevant to the query.
// Uses the full wiki pipeline (BM25/semantic/graph RRF + validity + gated
// model rerank). applyModelRerank only runs when top scores are ambiguous
// (or ForceRerank), so clear winners skip the ≤800ms cross-encoder.
func (a *wikiAdapter) Recall(ctx context.Context, query string, limit int) ([]Result, error) {
	report, err := a.store.SearchWithOptions(ctx, query, limit, wiki.QueryOptions{})
	if err != nil {
		return nil, err
	}
	hits := report.Results
	out := make([]Result, 0, len(hits))
	for _, h := range hits {
		meta := map[string]string{}
		if h.Line > 0 {
			meta["startLine"] = fmt.Sprintf("%d", h.Line)
			meta["endLine"] = fmt.Sprintf("%d", h.EndLine)
		}
		// 인물 페이지의 emails는 메일 From과 비교할 식별자라 Recall Meta에
		// 싣는다. mail_archive가 Read(회상-사용 원장) 없이 불일치를 표시하게.
		if strings.HasPrefix(h.Path, "인물/") {
			if page, rerr := a.store.ReadPage(h.Path); rerr == nil && page != nil && len(page.Meta.Emails) > 0 {
				meta["emails"] = strings.Join(page.Meta.Emails, ", ")
			}
		}
		out = append(out, Result{
			Ref:     Ref{Layer: LayerWiki, ID: h.Path},
			Snippet: h.Content,
			Context: h.ExpandedContent,
			Score:   h.Score,
			Provenance: Provenance{
				Locator: Locator{
					StartLine: h.Line, EndLine: h.EndLine,
					ContextStartLine: h.ExpandedLine, ContextEndLine: h.ExpandedEndLine,
				},
				Hierarchy: append([]string(nil), h.Context...),
			},
			Meta: meta,
			Time: h.UpdatedAt,
		})
	}
	return out, nil
}

// Read returns the wiki document identified by id.
func (a *wikiAdapter) Read(_ context.Context, id string) (*Document, error) {
	page, err := a.store.ReadPage(id)
	if err != nil {
		return nil, fmt.Errorf("read wiki page %q: %w", id, err)
	}
	// 효용 접지: Router.Read's only caller is the chat knowledge tool, so this
	// is a model-driven page open — observed USE for the recall-utility ledger
	// (bridge-evidence adoption). No session at this layer (the domain cannot
	// read chat context keys); path-level attribution is what the scoring uses.
	// Best-effort derived telemetry by ledger contract; must never fail a read.
	_ = a.store.RecordRecallEvents([]wiki.RecallEvent{{Path: id, Event: wiki.RecallEventRead}})
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
	if len(page.Meta.Emails) > 0 {
		meta["emails"] = strings.Join(page.Meta.Emails, ", ")
	}
	return &Document{
		Ref:     Ref{Layer: LayerWiki, ID: id},
		Title:   page.Meta.Title,
		Content: page.Body,
		Meta:    meta,
	}, nil
}

// Record persists a document through the adapter.
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
