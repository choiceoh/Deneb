// mail_counterparty.go — cached "active counterparty" sender lookup for the
// inbox priority scorer (mailpriority). Bridges the wiki-derived domain set
// (wiki.ActiveCounterpartyDomains — recent project-linked mail analyses) to
// the per-row Score call, which must stay cheap enough to run inline on every
// miniapp.mail.list_recent row: the wiki index scan happens at most once per
// TTL, every row in between is one lock + map hit.
package mailflow

import (
	"strings"
	"sync"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

const (
	// counterpartyTTL bounds staleness of the cached domain set. A new deal's
	// first analyzed mail starts boosting within this window — plenty for a
	// glance marker, and 6 index scans/hour costs nothing.
	counterpartyTTL = 10 * time.Minute
	// counterpartyWindowDays defines "active": a project-linked mail analysis
	// within the window keeps the relationship boosted. 60 days ≈ the cadence
	// of a live deal; the wiki's own retention archives mail analyses at 90
	// days, so this is the stricter of the two on purpose ("최근에 실제로
	// 오간" 관계만).
	counterpartyWindowDays = 60
)

// counterpartyLookup lazily builds and caches the active-counterparty domain
// set. The wiki store is read through a getter because it is late-bound
// (created during the session registration phase, after the Gmail RPC wiring
// that captures this lookup); a nil store just means no boost yet.
type counterpartyLookup struct {
	store func() *wiki.Store

	mu      sync.Mutex
	builtAt time.Time
	domains map[string]struct{}
}

// CounterpartyLookup caches active counterparty email domains from the wiki.
type CounterpartyLookup = counterpartyLookup

func newCounterpartyLookup(store func() *wiki.Store) *counterpartyLookup {
	return &counterpartyLookup{store: store}
}

// NewCounterpartyLookup constructs a lazy active-counterparty lookup.
func NewCounterpartyLookup(store func() *wiki.Store) *CounterpartyLookup {
	return newCounterpartyLookup(store)
}

// Has reports whether email's domain belongs to an active counterparty.
// Thread-safe; refreshes the cached set when the TTL lapses.
func (c *counterpartyLookup) Has(email string) bool {
	at := strings.LastIndexByte(email, '@')
	if at < 0 || at == len(email)-1 {
		return false
	}
	domain := strings.ToLower(email[at+1:])

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.domains == nil || time.Since(c.builtAt) > counterpartyTTL {
		set, ok := c.build()
		if !ok {
			// Store not wired yet (late-bound): don't cache the empty set —
			// stamping builtAt here would keep the boost off for a full TTL
			// after the store appears. Retry on the next row instead.
			return false
		}
		c.domains, c.builtAt = set, time.Now()
	}
	_, ok := c.domains[domain]
	return ok
}

// build snapshots the wiki-derived set; ok=false when the store is not wired
// yet (the caller skips caching so the next call retries immediately).
func (c *counterpartyLookup) build() (map[string]struct{}, bool) {
	s := c.store()
	if s == nil {
		return nil, false
	}
	cutoff := dentime.Now().AddDate(0, 0, -counterpartyWindowDays).Format("2006-01-02")
	return s.ActiveCounterpartyDomains(cutoff), true
}

// counterpartyProjectsCache mirrors counterpartyLookup for the richer
// domain → linked-projects map that the mail-analysis party anchor renders
// ("외부(hre-korea.com · 활성 거래처: 당진-솔라빌리지)"). Zero value is ready to
// use — it lives as a value field on MemorySubsystem so no constructor wiring
// is needed. Same TTL/window; a nil store returns nil without stamping the
// cache (late-bound store: retry on the next mail).
type counterpartyProjectsCache struct {
	mu       sync.Mutex
	builtAt  time.Time
	projects map[string][]string
}

// CounterpartyProjectsCache caches explicit project links by sender domain.
type CounterpartyProjectsCache = counterpartyProjectsCache

func (c *counterpartyProjectsCache) lookup(store *wiki.Store, domain string) []string {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if store == nil || domain == "" {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.projects == nil || time.Since(c.builtAt) > counterpartyTTL {
		cutoff := dentime.Now().AddDate(0, 0, -counterpartyWindowDays).Format("2006-01-02")
		c.projects = store.CounterpartyProjects(cutoff)
		c.builtAt = time.Now()
	}
	// Defensive copy: the cache is shared across goroutines and callers may
	// append/sort — handing out the backing array would corrupt the cache.
	projects := c.projects[domain]
	if len(projects) == 0 {
		return nil
	}
	return append([]string(nil), projects...)
}

// Lookup returns the projects explicitly linked to a sender domain.
func (c *counterpartyProjectsCache) Lookup(store *wiki.Store, domain string) []string {
	return c.lookup(store, domain)
}

// mailCounterpartyProjects is the PipelineDeps-injected lookup: linked project
// names for an active counterparty domain, nil otherwise. Deterministic and
// cheap (10-min cached index walk).
