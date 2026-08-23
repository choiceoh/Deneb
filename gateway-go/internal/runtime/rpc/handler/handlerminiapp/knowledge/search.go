// search.go — miniapp.search.all unified search handler.
//
// One entry point that fans out to wiki, diary, people, files, and mail
// backends in parallel and returns a single merged result. The original
// wiki/diary/people fields stay unchanged for older clients; newer clients can
// also render files/mail and distinguish empty results from source failures.
//
// Per-domain failures are non-fatal and represented only by a bounded status
// enum. Raw dependency errors never cross the RPC boundary. The RPC only
// surfaces an error when every source is absent — i.e. nothing to search at all.

package knowledge

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

const (
	defaultUnifiedSearchLimit  = 10
	maxUnifiedSearchLimit      = 30
	maxUnifiedSearchQueryRunes = 500
	// People search scans the same recent-mail window the listing
	// handler did, then filters the aggregated rows by query
	// substring. The window is fixed (not a request parameter) so
	// search behavior stays predictable from one query to the next.
	unifiedPeopleWindowDays = 30
	// People search hits Gmail (a 100-message aggregate, uncached), which can
	// be far slower than the in-memory wiki/diary FTS; semantic file search and
	// archive mail search may also depend on optional services. Start every
	// source deadline at fan-out time so one slow source adds at most this much
	// wall time, rather than this duration once per source.
	unifiedSearchSourceTimeout = 12 * time.Second

	searchSourceOK          = "ok"
	searchSourcePartial     = "partial"
	searchSourceUnavailable = "unavailable"
	searchSourceError       = "error"
	searchSourceTimeout     = "timeout"
)

// ErrSearchSourceUnavailable lets composition callbacks distinguish an
// intentionally unconfigured source from a failed one without exposing the
// backend's error text to clients.
var (
	ErrSearchSourceUnavailable = errors.New("search source unavailable")
	// ErrSearchSourcePartial marks a source that returned useful hits but could
	// not complete every retrieval branch. Unlike a generic error, the bounded
	// hits are preserved so clients can use them without mistaking them for a
	// complete search.
	ErrSearchSourcePartial = errors.New("search source partial")
)

// SearchDeps wires the five underlying sources. Any dependency may be nil;
// nil means the source is unavailable, while a callback error is reported as
// the generic source state "error" without exposing the error text.
type SearchDeps struct {
	Store  func() (MemorySearcher, error)
	Client func() (PeopleClient, error)
	Files  func(context.Context, string, int) ([]SearchFileHit, error)
	Mail   func(context.Context, string, int) ([]SearchMailHit, error)
}

// SearchWikiHit is one wiki hit row — mirrors the per-domain
// memorySearch shape so the Mini App can reuse its memory-card renderer.
//
//deneb:wire
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
//
//deneb:wire
type SearchDiaryHit struct {
	File    string  `json:"file"`
	Header  string  `json:"header"`
	Content string  `json:"content"`
	At      int64   `json:"at,omitempty"`
	Score   float64 `json:"score"`
}

// SearchFileHit is one evidence-bearing file result. Locator fields identify
// the best matching chunk so clients can explain and eventually jump to the
// exact source region instead of showing file metadata alone.
//
//deneb:wire
type SearchFileHit struct {
	Path      string  `json:"path"`
	Name      string  `json:"name"`
	Size      int64   `json:"size,omitempty"`
	Snippet   string  `json:"snippet"`
	Score     float64 `json:"score"`
	StartLine int     `json:"startLine,omitempty"`
	EndLine   int     `json:"endLine,omitempty"`
	Kind      string  `json:"kind,omitempty"`
	Heading   string  `json:"heading,omitempty"`
}

// SearchMailHit is one archive-mail result. The body is deliberately reduced
// to a bounded caller-provided snippet; handlers never include backend errors.
//
//deneb:wire
type SearchMailHit struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId,omitempty"`
	From     string `json:"from,omitempty"`
	Subject  string `json:"subject,omitempty"`
	Date     string `json:"date,omitempty"`
	Snippet  string `json:"snippet,omitempty"`
	Mailbox  string `json:"mailbox,omitempty"`
}

// SearchSourceStatus distinguishes a real empty result (ok) from an absent,
// partially searched, failed, or timed-out source. Values are one of
// ok|partial|unavailable|error|timeout.
//
//deneb:wire
type SearchSourceStatus struct {
	Wiki   string `json:"wiki"`
	Diary  string `json:"diary"`
	People string `json:"people"`
	Files  string `json:"files"`
	Mail   string `json:"mail"`
}

// SearchAllResult is the unified response shape.
//
//deneb:wire
type SearchAllResult struct {
	Wiki    []SearchWikiHit    `json:"wiki"`
	Diary   []SearchDiaryHit   `json:"diary"`
	People  []PersonRow        `json:"people"`
	Files   []SearchFileHit    `json:"files"`
	Mail    []SearchMailHit    `json:"mail"`
	Sources SearchSourceStatus `json:"sources"`
}

// SearchMethods returns the miniapp.search.* handler map. Returns nil
// if every source is absent — there's nothing to search.
func SearchMethods(deps SearchDeps) map[string]rpcutil.HandlerFunc {
	if !hasSearchSource(deps) {
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
	return minibind.BindOptional[params](func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		query := strings.TrimSpace(p.Query)
		if query == "" {
			return rpcerr.MissingParam("query").Response(req.ID)
		}
		if utf8.RuneCountInString(query) > maxUnifiedSearchQueryRunes {
			return rpcerr.InvalidRequest("query must be at most 500 characters").Response(req.ID)
		}
		limit := p.Limit
		if limit <= 0 {
			limit = defaultUnifiedSearchLimit
		}
		if limit > maxUnifiedSearchLimit {
			limit = maxUnifiedSearchLimit
		}
		if !hasSearchSource(deps) {
			return rpcerr.WrapUnavailable("search backends unavailable", nil).Response(req.ID)
		}

		result := SearchAllResult{
			Wiki:   []SearchWikiHit{},
			Diary:  []SearchDiaryHit{},
			People: []PersonRow{},
			Files:  []SearchFileHit{},
			Mail:   []SearchMailHit{},
		}

		// All source deadlines start before the first wait. Results travel through
		// buffered channels, and only this request goroutine mutates result, so a
		// late source cannot race with response serialization.
		wikiJob := startSearchSource(ctx, "wiki", deps.Store != nil, func(sourceCtx context.Context) ([]SearchWikiHit, error) {
			return runWikiSearch(sourceCtx, deps, query, limit)
		})
		diaryJob := startSearchSource(ctx, "diary", deps.Store != nil, func(sourceCtx context.Context) ([]SearchDiaryHit, error) {
			return runDiarySearch(sourceCtx, deps, query, limit)
		})
		peopleJob := startSearchSource(ctx, "people", deps.Client != nil, func(sourceCtx context.Context) ([]PersonRow, error) {
			return runPeopleSearch(sourceCtx, deps, query, limit)
		})
		filesJob := startSearchSource(ctx, "files", deps.Files != nil, func(sourceCtx context.Context) ([]SearchFileHit, error) {
			return runFilesSearch(sourceCtx, deps, query, limit)
		})
		mailJob := startSearchSource(ctx, "mail", deps.Mail != nil, func(sourceCtx context.Context) ([]SearchMailHit, error) {
			return runMailSearch(sourceCtx, deps, query, limit)
		})

		if hits, status := awaitSearchSource(wikiJob); status == searchSourceOK {
			result.Wiki = hits
			result.Sources.Wiki = status
		} else {
			result.Sources.Wiki = status
		}
		if hits, status := awaitSearchSource(diaryJob); status == searchSourceOK {
			result.Diary = hits
			result.Sources.Diary = status
		} else {
			result.Sources.Diary = status
		}
		if hits, status := awaitSearchSource(peopleJob); status == searchSourceOK || status == searchSourcePartial {
			result.People = hits
			result.Sources.People = status
		} else {
			result.Sources.People = status
		}
		if hits, status := awaitSearchSource(filesJob); status == searchSourceOK || status == searchSourcePartial {
			result.Files = hits
			result.Sources.Files = status
		} else {
			result.Sources.Files = status
		}
		if hits, status := awaitSearchSource(mailJob); status == searchSourceOK || status == searchSourcePartial {
			result.Mail = hits
			result.Sources.Mail = status
		} else {
			result.Sources.Mail = status
		}
		return rpcutil.RespondOK(req.ID, result)
	})
}

func hasSearchSource(deps SearchDeps) bool {
	return deps.Store != nil || deps.Client != nil || deps.Files != nil || deps.Mail != nil
}

type searchSourceExecution[T any] struct {
	value  T
	status string
}

type searchSourceJob[T any] struct {
	available bool
	ctx       context.Context
	cancel    context.CancelFunc
	result    <-chan searchSourceExecution[T]
}

// startSearchSource begins one request-scoped source call. The result channel is
// buffered so a dependency that returns just after its deadline never blocks on
// delivery after the handler has already moved on.
func startSearchSource[T any](
	parent context.Context,
	name string,
	available bool,
	run func(context.Context) (T, error),
) searchSourceJob[T] {
	if !available {
		return searchSourceJob[T]{}
	}
	sourceCtx, cancel := context.WithTimeout(parent, unifiedSearchSourceTimeout)
	result := make(chan searchSourceExecution[T], 1)
	go func() {
		execution := searchSourceExecution[T]{status: searchSourceError}
		defer func() {
			if recover() != nil {
				// A callback panic must not take down the gateway or leak its value
				// into the response/log. The source's generic error state is enough.
				slog.Error("unified search source panicked", "source", name)
				execution.status = searchSourceError
			}
			result <- execution
		}()
		value, err := run(sourceCtx)
		execution.value = value
		execution.status = classifySearchSource(sourceCtx, err)
	}()
	return searchSourceJob[T]{
		available: true,
		ctx:       sourceCtx,
		cancel:    cancel,
		result:    result,
	}
}

func awaitSearchSource[T any](job searchSourceJob[T]) (T, string) {
	var zero T
	if !job.available {
		return zero, searchSourceUnavailable
	}
	defer job.cancel()
	select {
	case execution := <-job.result:
		return execution.value, execution.status
	case <-job.ctx.Done():
		// Prefer a concurrently completed result over the timer edge. Its status
		// was classified at completion time, before later cancellation can alter it.
		select {
		case execution := <-job.result:
			return execution.value, execution.status
		default:
		}
		if errors.Is(job.ctx.Err(), context.DeadlineExceeded) {
			return zero, searchSourceTimeout
		}
		return zero, searchSourceError
	}
}

func classifySearchSource(ctx context.Context, err error) string {
	if errors.Is(err, ErrSearchSourcePartial) {
		return searchSourcePartial
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return searchSourceTimeout
	}
	if errors.Is(err, ErrSearchSourceUnavailable) {
		return searchSourceUnavailable
	}
	if err != nil || ctx.Err() != nil {
		return searchSourceError
	}
	return searchSourceOK
}

// runWikiSearch executes the wiki full-text search and enriches each
// hit with the page's title/summary/category.
func runWikiSearch(ctx context.Context, deps SearchDeps, query string, limit int) ([]SearchWikiHit, error) {
	store, err := deps.Store()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrSearchSourceUnavailable
	}
	hits, err := store.Search(ctx, query, limit)
	if err != nil {
		return nil, err
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
	return out, nil
}

// runDiarySearch executes diary full-text search via the wiki store.
// Prefers the match-centered Snippet over raw Content so the Mini App
// can show the hit in context without re-extracting on the client.
func runDiarySearch(ctx context.Context, deps SearchDeps, query string, limit int) ([]SearchDiaryHit, error) {
	store, err := deps.Store()
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrSearchSourceUnavailable
	}
	hits, err := store.SearchDiary(ctx, query, limit)
	if err != nil {
		return nil, err
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
	return out, nil
}

// runPeopleSearch reuses the people listing's Gmail-aggregate path,
// then filters by case-insensitive substring match on name or email.
// Gmail-side `from:<query>` was rejected because `query` is free text
// (could be a partial display name, a domain, or a Korean alias) —
// post-aggregation filter handles all of these uniformly.
func runPeopleSearch(ctx context.Context, deps SearchDeps, query string, limit int) ([]PersonRow, error) {
	client, err := deps.Client()
	if err != nil {
		return nil, err
	}
	if client == nil {
		return nil, ErrSearchSourceUnavailable
	}
	gmailQuery := "newer_than:" + strconv.Itoa(unifiedPeopleWindowDays) + "d -from:me"
	msgs, err := client.Search(ctx, gmailQuery, maxPeopleScanMessages)
	if err != nil {
		return nil, err
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
	// This legacy aggregation sees at most 100 senders from a 30-day Gmail
	// window, not the full address book or historical correspondence. Preserve
	// useful hits but keep the completeness contract honest until those durable
	// people sources are merged here.
	return filtered, ErrSearchSourcePartial
}

func runFilesSearch(ctx context.Context, deps SearchDeps, query string, limit int) ([]SearchFileHit, error) {
	hits, err := deps.Files(ctx, query, limit)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return append([]SearchFileHit{}, hits...), err
}

func runMailSearch(ctx context.Context, deps SearchDeps, query string, limit int) ([]SearchMailHit, error) {
	hits, err := deps.Mail(ctx, query, limit)
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return append([]SearchMailHit{}, hits...), err
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
