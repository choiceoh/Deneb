// Package curriculumenv assembles the curriculum lane's environment demand
// digest (RSI P5-1). Extracted from runtime/server so the composition root
// keeps only the wiring: this package owns the digest FORMAT and the source
// orchestration (fetch, nil-tolerance, cap, sort), and server passes its stores
// in through the narrow interfaces below. Future demand sources (e.g. upcoming
// calendar commitments) land here, not in the server package.
//
// The curriculum producer reads this digest to widen demand mining beyond
// tracker-local evidence: it sees what the operator is ACTUALLY working on
// (recent feed items) and the wiki domains in play, so proposed capabilities
// target real environment gaps, not catalog-internal rearrangement. Genesis
// stays a leaf — the closure server injects owns all workfeed/wiki knowledge.
package curriculumenv

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

// domainWindow bounds how far back the wiki-domain lookback reaches.
const domainWindow = 7 * 24 * time.Hour

// feedCap / domainCap bound each section so the digest stays compact.
const (
	feedCap   = 20
	domainCap = 15
)

// FeedLister is the narrow slice of the workfeed store the digest needs.
// *workfeed.Store satisfies it structurally.
type FeedLister interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
}

// WikiDomainSource is the narrow slice of the wiki store the digest needs.
// *wiki.Store satisfies it structurally.
type WikiDomainSource interface {
	ActiveCounterpartyDomains(cutoff string) map[string]struct{}
}

// Sources are the environment stores the digest reads. Every field is
// nil-tolerant (early/late binding): a nil source drops its section, and an
// all-nil Sources yields "" so the curriculum prompt omits the block. Now
// defaults to time.Now when unset.
type Sources struct {
	Feed FeedLister
	Wiki WikiDomainSource
	Now  func() time.Time
}

// Digest formats a compact environment summary: recent feed-item titles (active
// work shape) + active wiki counterparty domains (environment breadth). Empty
// sections are omitted; a fully empty digest returns "".
func Digest(s Sources) string {
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	var b strings.Builder

	// Active work: recent feed items (titles only — the producer needs the
	// shape of what's happening, not detail).
	if s.Feed != nil {
		items, _, err := s.Feed.List(feedCap, false)
		if err == nil && len(items) > 0 {
			b.WriteString("최근 업무 피드(최대 20):\n")
			for _, item := range items {
				if title := strings.TrimSpace(item.Title); title != "" {
					fmt.Fprintf(&b, "- %s\n", truncRunes(title, 80))
				}
			}
		}
	}

	// Environment breadth: active wiki counterparty domains (who the operator
	// is engaging with).
	if s.Wiki != nil {
		cutoff := now().Add(-domainWindow).Format("2006-01-02")
		domains := s.Wiki.ActiveCounterpartyDomains(cutoff)
		if len(domains) > 0 {
			sorted := make([]string, 0, len(domains))
			for d := range domains {
				sorted = append(sorted, d)
			}
			sort.Strings(sorted)
			if len(sorted) > domainCap {
				sorted = sorted[:domainCap]
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "활성 위키 상대 도메인(최대 15): %s\n", strings.Join(sorted, " · "))
		}
	}

	return strings.TrimSpace(b.String())
}

// truncRunes caps a string to n runes with an ellipsis.
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
