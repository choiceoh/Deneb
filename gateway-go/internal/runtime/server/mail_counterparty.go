// mail_counterparty.go — cached "active counterparty" sender lookup for the
// inbox priority scorer (mailpriority). Bridges the wiki-derived domain set
// (wiki.ActiveCounterpartyDomains — recent project-linked mail analyses) to
// the per-row Score call, which must stay cheap enough to run inline on every
// miniapp.gmail.list_recent row: the wiki index scan happens at most once per
// TTL, every row in between is one lock + map hit.
package server

import (
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
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

func newCounterpartyLookup(store func() *wiki.Store) *counterpartyLookup {
	return &counterpartyLookup{store: store}
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
		c.domains = c.build()
		c.builtAt = time.Now()
	}
	_, ok := c.domains[domain]
	return ok
}

// build snapshots the wiki-derived set; empty (never nil) when the store is
// not wired yet so the next TTL lapse retries.
func (c *counterpartyLookup) build() map[string]struct{} {
	s := c.store()
	if s == nil {
		return map[string]struct{}{}
	}
	cutoff := dentime.Now().AddDate(0, 0, -counterpartyWindowDays).Format("2006-01-02")
	return s.ActiveCounterpartyDomains(cutoff)
}
