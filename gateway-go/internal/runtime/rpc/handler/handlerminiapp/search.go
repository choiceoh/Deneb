// search.go — miniapp.search.all unified search handler.
//
// One entry point that fans out to wiki, diary, and people backends in
// parallel and returns a single merged result. Replaces the trio of
// per-domain listing entries on the Mini App home (memory / diary /
// people) — there's now one search input and three result sections.
//
// Per-domain failures are non-fatal: if Gmail isn't configured the
// people section just comes back empty (the operator still sees wiki
// and diary hits). The RPC only surfaces an error when both factories
// are absent — i.e. nothing to search at all.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultUnifiedSearchLimit = 10
	maxUnifiedSearchLimit     = 30
	// People search scans the same recent-mail window the listing
	// handler did, then filters the aggregated rows by query
	// substring. The window is fixed (not a request parameter) so
	// search behavior stays predictable from one query to the next.
	unifiedPeopleWindowDays = 30
)

// SearchDeps wires the two underlying domains. Either factory may be
// nil (e.g. wiki disabled, or Gmail not configured); the handler
// gracefully degrades to whatever is available.
type SearchDeps struct {
	Store  func() (MemorySearcher, error)
	Client func() (PeopleClient, error)
}

// SearchWikiHit is one wiki hit row — mirrors the per-domain
// memorySearch shape so the Mini App can reuse its memory-card renderer.
type SearchWikiHit struct {
	Path     string  `json:"path"`
	Title    string  `json:"title,omitempty"`
	Summary  string  `json:"summary,omitempty"`
	Category string  `json:"category,omitempty"`
	Snippet  string  `json:"snippet"`
	Score    float64 `json:"score"`
}

// SearchDiaryHit is one diary hit row. Mirrors the recent-diary entry
// shape with a Score added (search ranks by BM25 × recency).
type SearchDiaryHit struct {
	File    string  `json:"file"`
	Header  string  `json:"header"`
	Content string  `json:"content"`
	At      int64   `json:"at,omitempty"`
	Score   float64 `json:"score"`
}

// SearchAllResult is the unified response shape.
type SearchAllResult struct {
	Wiki   []SearchWikiHit  `json:"wiki"`
	Diary  []SearchDiaryHit `json:"diary"`
	People []PersonRow      `json:"people"`
}

// SearchMethods returns the miniapp.search.* handler map. Returns nil
// if both factories are absent — there's nothing to search.
func SearchMethods(deps SearchDeps) map[string]rpcutil.HandlerFunc {
	if deps.Store == nil && deps.Client == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.search.all": searchAll(deps),
	}
}

func searchAll(deps SearchDeps) rpcutil.HandlerFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
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
			limit = defaultUnifiedSearchLimit
		}
		if limit > maxUnifiedSearchLimit {
			limit = maxUnifiedSearchLimit
		}

		result := SearchAllResult{
			Wiki:   []SearchWikiHit{},
			Diary:  []SearchDiaryHit{},
			People: []PersonRow{},
		}
		var wg sync.WaitGroup
		anyAttempt := false

		if deps.Store != nil {
			anyAttempt = true
			wg.Add(2)
			go func() {
				defer wg.Done()
				result.Wiki = runWikiSearch(ctx, deps, query, limit)
			}()
			go func() {
				defer wg.Done()
				result.Diary = runDiarySearch(ctx, deps, query, limit)
			}()
		}
		if deps.Client != nil {
			anyAttempt = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				result.People = runPeopleSearch(ctx, deps, query, limit)
			}()
		}
		wg.Wait()

		if !anyAttempt {
			return rpcerr.WrapUnavailable("search backends unavailable", nil).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, result)
	}
}

// runWikiSearch executes the wiki full-text search and enriches each
// hit with the page's title/summary/category. Failures (factory
// unavailable, search error) collapse to an empty slice — per-domain
// error surfacing is intentionally out of scope for v1.
func runWikiSearch(ctx context.Context, deps SearchDeps, query string, limit int) []SearchWikiHit {
	store, err := deps.Store()
	if err != nil || store == nil {
		return []SearchWikiHit{}
	}
	hits, err := store.Search(ctx, query, limit)
	if err != nil {
		return []SearchWikiHit{}
	}
	out := make([]SearchWikiHit, 0, len(hits))
	for _, h := range hits {
		row := SearchWikiHit{
			Path:    h.Path,
			Snippet: truncateRunes(h.Content, maxMemorySnippetChars),
			Score:   h.Score,
		}
		if page, perr := store.ReadPage(h.Path); perr == nil && page != nil {
			row.Title = page.Meta.Title
			row.Summary = page.Meta.Summary
			row.Category = page.Meta.Category
		}
		out = append(out, row)
	}
	return out
}

// runDiarySearch executes diary full-text search via the wiki store.
// Prefers the match-centered Snippet over raw Content so the Mini App
// can show the hit in context without re-extracting on the client.
func runDiarySearch(ctx context.Context, deps SearchDeps, query string, limit int) []SearchDiaryHit {
	store, err := deps.Store()
	if err != nil || store == nil {
		return []SearchDiaryHit{}
	}
	hits, err := store.SearchDiary(ctx, query, limit)
	if err != nil {
		return []SearchDiaryHit{}
	}
	out := make([]SearchDiaryHit, 0, len(hits))
	for _, h := range hits {
		content := h.Snippet
		if content == "" {
			content = h.Content
		}
		out = append(out, SearchDiaryHit{
			File:    h.File,
			Header:  h.Header,
			Content: truncateRunes(content, maxDiarySnippetChars),
			At:      h.At,
			Score:   h.Score,
		})
	}
	return out
}

// runPeopleSearch reuses the people listing's Gmail-aggregate path,
// then filters by case-insensitive substring match on name or email.
// Gmail-side `from:<query>` was rejected because `query` is free text
// (could be a partial display name, a domain, or a Korean alias) —
// post-aggregation filter handles all of these uniformly.
func runPeopleSearch(ctx context.Context, deps SearchDeps, query string, limit int) []PersonRow {
	client, err := deps.Client()
	if err != nil || client == nil {
		return []PersonRow{}
	}
	gmailQuery := "newer_than:" + strconv.Itoa(unifiedPeopleWindowDays) + "d -from:me"
	msgs, err := client.Search(ctx, gmailQuery, maxPeopleScanMessages)
	if err != nil {
		return []PersonRow{}
	}
	rows := aggregatePeople(msgs)
	filtered := make([]PersonRow, 0, len(rows))
	for _, r := range rows {
		if matchesPerson(r, query) {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered
}

// matchesPerson is case-insensitive substring match on email or
// display name. Both sides are lowered locally so callers can pass the
// raw query without worrying about normalization.
func matchesPerson(p PersonRow, query string) bool {
	needle := strings.ToLower(strings.TrimSpace(query))
	if needle == "" {
		return true
	}
	if strings.Contains(strings.ToLower(p.Email), needle) {
		return true
	}
	if p.Name != "" && strings.Contains(strings.ToLower(p.Name), needle) {
		return true
	}
	return false
}
