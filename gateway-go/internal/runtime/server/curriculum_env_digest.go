package server

// curriculumEnvDigest — the injected closure for CurriculumTask's EnvDigest
// field (RSI P5-1 slice-2). Widens demand mining beyond tracker-local evidence:
// the curriculum producer sees what the operator is ACTUALLY working on (recent
// feed items) and the wiki domains in play, so proposed capabilities target
// real environment gaps, not catalog-internal rearrangement.
//
// Genesis stays a leaf — this closure owns all workfeed/wiki knowledge. Both
// stores are tolerant of nil (early/late binding): nil stores yield a shorter
// digest, never a crash.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// curriculumEnvDigestWindow bounds how far back the digest looks.
const curriculumEnvDigestWindow = 7 * 24 * time.Hour

// curriculumEnvDigest formats a compact environment summary: recent feed-item
// titles (active work shape) + active wiki counterparty domains (environment
// breadth). Empty sections are omitted; a fully empty digest returns "" so the
// curriculum prompt drops the section.
func (s *Server) curriculumEnvDigest(_ context.Context) string {
	var b strings.Builder

	// Active work: recent feed items (titles only — the producer needs the
	// shape of what's happening, not detail).
	if s.workFeedStore != nil {
		items, _, err := s.workFeedStore.List(20, false)
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
	// is engaging with). wikiStore is late-bound — nil is fine.
	if s.wikiStore != nil {
		cutoff := time.Now().Add(-curriculumEnvDigestWindow).Format("2006-01-02")
		domains := s.wikiStore.ActiveCounterpartyDomains(cutoff)
		if len(domains) > 0 {
			sorted := make([]string, 0, len(domains))
			for d := range domains {
				sorted = append(sorted, d)
			}
			sort.Strings(sorted)
			if len(sorted) > 15 {
				sorted = sorted[:15]
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
