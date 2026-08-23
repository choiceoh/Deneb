// gmail_context.go — miniapp.mail.sender_context RPC.
//
// Given a Gmail sender ("Name <email>", raw email, or just a name), assemble
// what Deneb already knows about that person so the Mini App detail view
// can show a contextual card instead of treating each email as an anonymous
// arrival.
//
// Three sources combined:
//
//   1. Gmail itself — `from:<email> newer_than:30d` to count recent
//      messages and grab the timestamp of the last one. Fast (single API
//      call) and gives the freshness signal a busy operator actually
//      reads.
//
//   2. Wiki memory — `wiki.Store.Search` on the display name. Pulls back
//      the operator's hand-curated notes about this person/company
//      (frontmatter title/summary/category). Empty when the person isn't
//      in memory yet, which is itself useful information ("새로운 연락처").
//
//   3. Wiki graph — `graphify` relationship/context facts, but with a
//      short Mini App budget so a slow graph traversal never holds back
//      the fast Gmail/wiki card.

package handlermail

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// GmailContextDeps groups the factories the handler needs. Any of them
// can fail at construction (no OAuth / no wiki / no graphify) — the
// handler then surfaces a notice for that source and still returns
// whatever the others produced.
//
// SenderFacts is the wiki-graph traversal injected from
// mailanalysis.ExtractSenderFacts. It runs an external `graphify` CLI with
// a longer pipeline timeout, so this handler wraps it in a shorter Mini
// App budget: the handler stays testable, and slow/missing graphify only
// omits wikiFacts instead of delaying the whole sender card.
type GmailContextDeps struct {
	Client             func() (GmailClient, error)
	WikiStore          func() (MemorySearcher, error)
	SenderFacts        func(ctx context.Context, from string) string
	SenderFactsTimeout time.Duration
	RecentDays         int           // Lookback window for "from:<email> newer_than:..."; 0 → 30.
	MaxRecent          int           // Cap on Gmail.Search results; 0 → 50.
	MaxWikiHits        int           // Cap on wiki search results; 0 → 5.
	CacheTTL           time.Duration // Per-sender result cache window; 0 → 90s. Negative disables caching.
	CacheMax           int           // LRU bound for the result cache; 0 → 64.
}

const (
	defaultSenderFactsTimeout = 750 * time.Millisecond

	// Cache window for assembled sender context. The card mixes
	// metadata that changes slowly (wiki pages, graph facts) with a
	// 30-day recent-mail count that ticks every time a new message
	// arrives. 90 seconds is short enough that a freshly-arrived mail
	// shows up in the count on the next drill-in, but long enough to
	// absorb the burst of repeat reads that happen as the operator
	// scans through several mails from the same sender. Tunable via
	// GmailContextDeps.CacheTTL when wired.
	defaultSenderContextCacheTTL = 90 * time.Second

	// LRU bound: the operator works through dozens of senders per
	// session, not thousands. A small bounded cache avoids unbounded
	// memory growth on long-running gateways while still covering the
	// "I'll come back to this one in a minute" pattern.
	defaultSenderContextCacheMax = 64
)

// GmailContextMethods returns the miniapp.mail.sender_context handler.
// Returns nil if no source is wired — the gateway then skips registration
// cleanly.
func GmailContextMethods(deps GmailContextDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil && deps.WikiStore == nil && deps.SenderFacts == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.mail.sender_context": senderContext(deps),
	}
}

// senderContextOut is the miniapp.mail.sender_context response shape.
type senderContextOut struct {
	Sender      string             `json:"sender"`
	Email       string             `json:"email,omitempty"`
	DisplayName string             `json:"displayName,omitempty"`
	Recent      *senderRecentOut   `json:"recent,omitempty"`
	WikiHits    []senderWikiHitOut `json:"wikiHits"`
	// WikiFacts is the free-form graphify-CLI snapshot of what's
	// known about the sender (relationships, recent deals/decisions
	// in the wiki graph). Empty when graphify is unavailable, the
	// graph isn't built, or the sender isn't in the graph.
	WikiFacts string `json:"wikiFacts,omitempty"`
	// Notes the handler attaches when a source was unavailable so
	// the client can render "wiki not configured" hints instead of
	// silently empty cards.
	Notices []string `json:"notices,omitempty"`
}

// senderContextLimits is GmailContextDeps with the zero-value knobs resolved
// to their defaults.
type senderContextLimits struct {
	recentDays int
	maxRecent  int
	maxWiki    int
	cacheTTL   time.Duration
	cacheMax   int
}

func resolveSenderContextLimits(deps GmailContextDeps) senderContextLimits {
	lim := senderContextLimits{
		recentDays: deps.RecentDays,
		maxRecent:  deps.MaxRecent,
		maxWiki:    deps.MaxWikiHits,
		cacheTTL:   deps.CacheTTL,
		cacheMax:   deps.CacheMax,
	}
	if lim.recentDays <= 0 {
		lim.recentDays = 30
	}
	if lim.maxRecent <= 0 {
		lim.maxRecent = 50
	}
	if lim.maxWiki <= 0 {
		lim.maxWiki = 5
	}
	if lim.cacheTTL == 0 {
		lim.cacheTTL = defaultSenderContextCacheTTL
	}
	if lim.cacheMax <= 0 {
		lim.cacheMax = defaultSenderContextCacheMax
	}
	return lim
}

func senderContext(deps GmailContextDeps) rpcutil.HandlerFunc {
	type params struct {
		Sender string `json:"sender"`
	}
	lim := resolveSenderContextLimits(deps)
	var cache *senderContextCache
	if lim.cacheTTL > 0 {
		cache = newSenderContextCache(lim.cacheMax, lim.cacheTTL)
	}

	return bindOptional(func(ctx context.Context, req *protocol.RequestFrame, p params) *protocol.ResponseFrame {
		raw := strings.TrimSpace(p.Sender)
		if raw == "" {
			return rpcerr.MissingParam("sender").Response(req.ID)
		}

		email, displayName := parseSender(raw)
		cacheKey := senderContextCacheKey(email, raw)
		if cache != nil {
			if cached, ok := cache.get(cacheKey); ok {
				return rpcutil.RespondOK(req.ID, cached)
			}
		}

		resp := collectSenderContext(ctx, deps, lim, raw, email, displayName)

		// Cache the assembled response only when at least one source
		// actually contributed data — there's no point pinning an
		// all-empty result on the off chance a transient failure made
		// every source return nothing. The wikiHits slice is always
		// allocated, so check its length rather than nil.
		if cache != nil && (resp.Recent != nil || len(resp.WikiHits) > 0 || resp.WikiFacts != "") {
			cache.put(cacheKey, resp)
		}

		return rpcutil.RespondOK(req.ID, resp)
	})
}

// senderContextCacheKey is the lower-cased extracted email when we have
// one, otherwise the trimmed raw input. This collapses casing
// differences ("Alice@Foo.com" vs "alice@foo.com") and lets
// the same person hit cache across messages that label them
// differently in the From header.
func senderContextCacheKey(email, raw string) string {
	if key := strings.ToLower(email); key != "" {
		return key
	}
	return strings.ToLower(raw)
}

// collectSenderContext fans the three sources out in parallel. Each writes to
// its own slot of the response struct under the mutex below; notices
// accumulate in a slice the goroutines append to (also under the mutex).
// Wall-clock for the slowest source — graphify, bounded by
// SenderFactsTimeout — sets the response latency, instead of summing all
// three as the sequential version did.
func collectSenderContext(ctx context.Context, deps GmailContextDeps, lim senderContextLimits, raw, email, displayName string) senderContextOut {
	resp := senderContextOut{
		Sender:      raw,
		Email:       email,
		DisplayName: displayName,
		WikiHits:    []senderWikiHitOut{},
	}
	var mu sync.Mutex
	addNotice := func(s string) {
		mu.Lock()
		resp.Notices = append(resp.Notices, s)
		mu.Unlock()
	}

	var wg sync.WaitGroup

	// --- Gmail recent activity ---
	if deps.Client != nil && email != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := gmailRecentActivity(ctx, deps.Client, email, lim, addNotice)
			if rec == nil {
				return
			}
			mu.Lock()
			resp.Recent = rec
			mu.Unlock()
		}()
	}

	// --- Wiki hand-curated notes ---
	if deps.WikiStore != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rows, ok := senderWikiHits(ctx, deps.WikiStore, displayName, raw, lim.maxWiki, addNotice)
			if !ok {
				return
			}
			mu.Lock()
			resp.WikiHits = rows
			mu.Unlock()
		}()
	}

	// --- Wiki-graph traversal (graphify CLI) ---
	// Best-effort with a short UI budget. The underlying extractor
	// still owns graphify's longer subprocess timeout for analyze
	// pipelines, but this Mini App path should not make fast
	// Gmail/wiki context wait on graph traversal.
	if deps.SenderFacts != nil && raw != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			facts := senderFactsWithin(ctx, deps.SenderFacts, raw, deps.SenderFactsTimeout)
			mu.Lock()
			resp.WikiFacts = facts
			mu.Unlock()
		}()
	}

	wg.Wait()
	return resp
}

// gmailRecentActivity counts the sender's recent messages via one Gmail
// search. Returns nil after adding a notice when the client or the search
// fails, so the caller leaves the Recent slot empty.
func gmailRecentActivity(ctx context.Context, clientFn func() (GmailClient, error), email string, lim senderContextLimits, addNotice func(string)) *senderRecentOut {
	client, err := clientFn()
	if err != nil {
		addNotice("gmail unavailable: " + err.Error())
		return nil
	}
	// Quote the email so any operator characters (`-`, `:`,
	// space-equivalents) in the local part are treated as
	// part of the address, not as Gmail search syntax.
	query := fmt.Sprintf("from:%q newer_than:%dd", email, lim.recentDays)
	results, qerr := client.Search(ctx, query, lim.maxRecent)
	if qerr != nil {
		addNotice("gmail search failed: " + qerr.Error())
		return nil
	}
	rec := &senderRecentOut{
		Count:      len(results),
		WindowDays: lim.recentDays,
		Truncated:  len(results) == lim.maxRecent,
	}
	// Pick the first non-empty Date — Search can stub
	// summaries with an empty Date when metadata fetch
	// failed, so index 0 alone is unreliable.
	for _, r := range results {
		if strings.TrimSpace(r.Date) == "" {
			continue
		}
		rec.LastReceivedAt = normalizeDate(r.Date)
		break
	}
	return rec
}

// senderWikiHits searches the operator's hand-curated wiki notes for the
// sender. ok is false after adding a notice when the store or the search
// fails, so the caller keeps the pre-allocated empty WikiHits slice.
func senderWikiHits(ctx context.Context, storeFn func() (MemorySearcher, error), displayName, raw string, maxWiki int, addNotice func(string)) (rows []senderWikiHitOut, ok bool) {
	store, err := storeFn()
	if err != nil {
		addNotice("memory unavailable: " + err.Error())
		return nil, false
	}
	// Prefer the display name for the query (matches title
	// field in person/company pages); fall back to the raw
	// input if we couldn't parse one out.
	wikiQuery := displayName
	if wikiQuery == "" {
		wikiQuery = raw
	}
	hits, werr := store.Search(ctx, wikiQuery, maxWiki)
	if werr != nil {
		addNotice("memory search failed: " + werr.Error())
		return nil, false
	}
	rows = make([]senderWikiHitOut, 0, len(hits))
	for _, h := range hits {
		// Fact-plane hits carry synthetic paths and are not editable wiki pages.
		// This page-only context card must never expose them as openable rows.
		if h.FactID != "" {
			continue
		}
		row := senderWikiHitOut{Path: h.Path}
		if page, perr := store.ReadPage(h.Path); perr == nil && page != nil {
			row.Title = page.Meta.Title
			row.Summary = page.Meta.Summary
			row.Category = page.Meta.Category
		}
		rows = append(rows, row)
	}
	return rows, true
}

// senderContextCache is a small TTL-bounded LRU keyed by normalized
// sender. Reads + writes are mutex-protected; the value is the full
// `out` struct (cheap to copy — ~5 small fields and a sub-slice).
//
// Expiry is opportunistic: a get() past the TTL evicts the entry and
// reports a miss; there's no background sweep. With a 64-entry bound
// and 90-second TTL the worst-case footprint is a few KB.
type senderContextCache struct {
	mu      sync.Mutex
	entries map[string]senderContextEntry
	max     int
	ttl     time.Duration
}

type senderContextEntry struct {
	value    senderContextResp
	insertAt time.Time
}

// senderContextResp captures every field the handler returns. Kept as
// a named type alias of the inline `out` struct via interface so the
// cache doesn't have to re-declare the whole response shape — see
// the type assertion in get/put.
type senderContextResp = any

func newSenderContextCache(max int, ttl time.Duration) *senderContextCache {
	return &senderContextCache{
		entries: make(map[string]senderContextEntry, max),
		max:     max,
		ttl:     ttl,
	}
}

func (c *senderContextCache) get(key string) (senderContextResp, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	if time.Since(entry.insertAt) > c.ttl {
		delete(c.entries, key)
		return nil, false
	}
	return entry.value, true
}

func (c *senderContextCache) put(key string, value senderContextResp) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Crude LRU: when at capacity, evict the oldest by insertAt. The
	// cache turns over slowly (one entry per unique sender per ~90s)
	// so the linear scan stays cheap up to ~64 entries.
	if len(c.entries) >= c.max {
		var oldestKey string
		var oldestAt time.Time
		for k, e := range c.entries {
			if oldestKey == "" || e.insertAt.Before(oldestAt) {
				oldestKey = k
				oldestAt = e.insertAt
			}
		}
		if oldestKey != "" {
			delete(c.entries, oldestKey)
		}
	}
	c.entries[key] = senderContextEntry{value: value, insertAt: time.Now()}
}

func senderFactsWithin(
	ctx context.Context,
	fn func(context.Context, string) string,
	raw string,
	timeout time.Duration,
) string {
	if fn == nil || strings.TrimSpace(raw) == "" {
		return ""
	}
	if timeout <= 0 {
		timeout = defaultSenderFactsTimeout
	}

	factsCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ch := make(chan string, 1)
	go func() {
		ch <- strings.TrimSpace(fn(factsCtx, raw))
	}()

	select {
	case facts := <-ch:
		if factsCtx.Err() != nil {
			return ""
		}
		return facts
	case <-factsCtx.Done():
		return ""
	}
}
