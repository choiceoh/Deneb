// gmail_context.go — miniapp.gmail.sender_context RPC.
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

package handlerminiapp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// GmailContextDeps groups the factories the handler needs. Any of them
// can fail at construction (no OAuth / no wiki / no graphify) — the
// handler then surfaces a notice for that source and still returns
// whatever the others produced.
//
// SenderFacts is the wiki-graph traversal injected from
// gmailpoll.ExtractSenderFacts. It runs an external `graphify` CLI with
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

// GmailContextMethods returns the miniapp.gmail.sender_context handler.
// Returns nil if no source is wired — the gateway then skips registration
// cleanly.
func GmailContextMethods(deps GmailContextDeps) map[string]rpcutil.HandlerFunc {
	if deps.Client == nil && deps.WikiStore == nil && deps.SenderFacts == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.gmail.sender_context": senderContext(deps),
	}
}

func senderContext(deps GmailContextDeps) rpcutil.HandlerFunc {
	type params struct {
		Sender string `json:"sender"`
	}
	type wikiHitOut struct {
		Path     string `json:"path"`
		Title    string `json:"title,omitempty"`
		Summary  string `json:"summary,omitempty"`
		Category string `json:"category,omitempty"`
	}
	type recentOut struct {
		Count          int    `json:"count"`
		LastReceivedAt string `json:"lastReceivedAt,omitempty"` // ISO 8601
		WindowDays     int    `json:"windowDays"`
		// Truncated is true when `Count` equals the per-request cap —
		// there could be more matching messages than reported. UI uses
		// this to render "5+" instead of "5".
		Truncated bool `json:"truncated,omitempty"`
	}
	type out struct {
		Sender      string       `json:"sender"`
		Email       string       `json:"email,omitempty"`
		DisplayName string       `json:"displayName,omitempty"`
		Recent      *recentOut   `json:"recent,omitempty"`
		WikiHits    []wikiHitOut `json:"wikiHits"`
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
	recentDays := deps.RecentDays
	if recentDays <= 0 {
		recentDays = 30
	}
	maxRecent := deps.MaxRecent
	if maxRecent <= 0 {
		maxRecent = 50
	}
	maxWiki := deps.MaxWikiHits
	if maxWiki <= 0 {
		maxWiki = 5
	}
	cacheTTL := deps.CacheTTL
	if cacheTTL == 0 {
		cacheTTL = defaultSenderContextCacheTTL
	}
	cacheMax := deps.CacheMax
	if cacheMax <= 0 {
		cacheMax = defaultSenderContextCacheMax
	}
	var cache *senderContextCache
	if cacheTTL > 0 {
		cache = newSenderContextCache(cacheMax, cacheTTL)
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
		raw := strings.TrimSpace(p.Sender)
		if raw == "" {
			return rpcerr.MissingParam("sender").Response(req.ID)
		}

		// Cache key is the lower-cased extracted email when we have
		// one, otherwise the trimmed raw input. This collapses casing
		// differences ("Alice@Foo.com" vs "alice@foo.com") and lets
		// the same person hit cache across messages that label them
		// differently in the From header.
		email, displayName := parseSender(raw)
		cacheKey := strings.ToLower(email)
		if cacheKey == "" {
			cacheKey = strings.ToLower(raw)
		}
		if cache != nil {
			if cached, ok := cache.get(cacheKey); ok {
				return rpcutil.RespondOK(req.ID, cached)
			}
		}

		// Three sources fan out in parallel. Each writes to its own
		// slot of the response struct under the mutex below; notices
		// accumulate in a slice the goroutines append to (also under
		// the mutex). Wall-clock for the slowest source — graphify,
		// bounded by SenderFactsTimeout — sets the response latency,
		// instead of summing all three as the sequential version did.
		resp := out{
			Sender:      raw,
			Email:       email,
			DisplayName: displayName,
			WikiHits:    []wikiHitOut{},
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
				client, err := deps.Client()
				if err != nil {
					addNotice("gmail unavailable: " + err.Error())
					return
				}
				// Quote the email so any operator characters (`-`, `:`,
				// space-equivalents) in the local part are treated as
				// part of the address, not as Gmail search syntax.
				query := fmt.Sprintf("from:%q newer_than:%dd", email, recentDays)
				results, qerr := client.Search(ctx, query, maxRecent)
				if qerr != nil {
					addNotice("gmail search failed: " + qerr.Error())
					return
				}
				rec := &recentOut{
					Count:      len(results),
					WindowDays: recentDays,
					Truncated:  len(results) == maxRecent,
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
				store, err := deps.WikiStore()
				if err != nil {
					addNotice("memory unavailable: " + err.Error())
					return
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
					return
				}
				rows := make([]wikiHitOut, 0, len(hits))
				for _, h := range hits {
					row := wikiHitOut{Path: h.Path}
					if page, perr := store.ReadPage(h.Path); perr == nil && page != nil {
						row.Title = page.Meta.Title
						row.Summary = page.Meta.Summary
						row.Category = page.Meta.Category
					}
					rows = append(rows, row)
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

		// Cache the assembled response only when at least one source
		// actually contributed data — there's no point pinning an
		// all-empty result on the off chance a transient failure made
		// every source return nothing. The wikiHits slice is always
		// allocated, so check its length rather than nil.
		if cache != nil && (resp.Recent != nil || len(resp.WikiHits) > 0 || resp.WikiFacts != "") {
			cache.put(cacheKey, resp)
		}

		return rpcutil.RespondOK(req.ID, resp)
	}
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

// parseSender splits "Display Name <email@host>" into ("email@host",
// "Display Name"). Tolerant of bare emails ("a@b.com") and bare names
// ("Alice"). Empty parts are returned as "".
//
// **Strict on email**: an extracted candidate is only returned as `email`
// when it actually looks like one (`looksLikeEmail`). Without this guard
// inputs like `<noaddr>` or `<alice@x.com newer_than:365d>` would have
// dropped into the Gmail query verbatim, changing the search semantics or
// triggering useless API calls. Falls back to treating the input as a
// display name when no real address is found.
func parseSender(raw string) (email, display string) {
	if addr, err := mail.ParseAddress(raw); err == nil {
		candidate := strings.TrimSpace(addr.Address)
		if looksLikeEmail(candidate) {
			return candidate, strings.TrimSpace(addr.Name)
		}
	}
	// Fallback: look for an obvious "<email>" pattern even if
	// mail.ParseAddress refused (e.g., display name has quoting issues).
	if i := strings.Index(raw, "<"); i >= 0 {
		j := strings.Index(raw[i:], ">")
		if j > 0 {
			candidate := strings.TrimSpace(raw[i+1 : i+j])
			display = strings.TrimSpace(raw[:i])
			display = strings.Trim(display, `"' `)
			if looksLikeEmail(candidate) {
				return candidate, display
			}
			// Garbage inside angle brackets — keep the display
			// hint but skip Gmail lookup.
			return "", display
		}
	}
	// Bare email vs bare name.
	if looksLikeEmail(raw) {
		return raw, ""
	}
	return "", raw
}

// looksLikeEmail does the minimum validation needed to keep the address
// safe to drop into a Gmail search query. It is intentionally lax — we
// trust mail.ParseAddress for full RFC 5322; this guard catches the
// failure modes that survive the fallback parser (no `@`, embedded
// whitespace, query operators inside the angle brackets).
func looksLikeEmail(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, " \t\r\n\"'`<>") {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at < 1 || at == len(s)-1 {
		return false
	}
	if strings.IndexByte(s[at+1:], '@') >= 0 {
		return false
	}
	return true
}
