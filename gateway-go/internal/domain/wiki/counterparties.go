// counterparties.go — derives the "active counterparty" sender-domain set from
// project-linked mail-analysis pages, for the inbox priority scorer's
// combined-signal boost (mailpriority): the same 견적/기한 mail matters more
// when it comes from a company the user is actively working with.
//
// Source of truth: every analyzed mail the analyzer linked to a project lands
// at 프로젝트/<project>/메일분석/<msgID>.md with the sender's domain as a
// frontmatter tag (wiki_mail_analysis.go buildMailAnalysisPage). That makes
// the in-memory index sufficient — no page-body reads — and "recent
// project-linked mail from this domain" a cheap, deterministic definition of
// an active business relationship. Freemail domains are excluded: a
// gmail.com/naver.com tag identifies a person, not a counterparty company, and
// would boost unrelated senders sharing the host. (Freemail-only counterparty
// contacts are a known miss — matching them needs per-address extraction from
// page bodies, not worth the I/O for a glance marker.)
package wiki

import "strings"

// freemailDomains are consumer mail hosts whose domain says nothing about the
// sender's organization. Lowercase.
var freemailDomains = map[string]struct{}{
	"gmail.com":      {},
	"googlemail.com": {},
	"naver.com":      {},
	"daum.net":       {},
	"hanmail.net":    {},
	"kakao.com":      {},
	"nate.com":       {},
	"outlook.com":    {},
	"hotmail.com":    {},
	"live.com":       {},
	"yahoo.com":      {},
	"yahoo.co.kr":    {},
	"icloud.com":     {},
	"me.com":         {},
	"proton.me":      {},
	"protonmail.com": {},
}

// IsFreemailDomain reports whether a (lowercased or not) mail domain is a
// consumer host that must never count as a counterparty identity.
func IsFreemailDomain(domain string) bool {
	_, ok := freemailDomains[strings.ToLower(strings.TrimSpace(domain))]
	return ok
}

// ActiveCounterpartyDomains returns the lowercase sender domains of
// project-linked mail-analysis pages created on/after cutoff (YYYY-MM-DD;
// lexical compare — Created is the analysis day and, unlike Updated, is not
// re-stamped by later metadata churn such as ReclassifyUnlinkedMailAnalyses
// moving an old mail into a project, which would wrongly re-activate a stale
// sender domain for the whole window). Created is persisted in the index.md
// TSV, so it survives gateway restarts; entries without it (parsed from a
// pre-created-column index.md) fall back to Updated. Freemail domains are
// excluded.
// The iteration happens under the store's read lock (the returned map is a
// fresh copy): index entries are mutated in place by writers, so callers
// must never walk the live index themselves — use Store.SnapshotEntries
// (same contract as Tier1Pages).
func (s *Store) ActiveCounterpartyDomains(cutoff string) map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]struct{})
	if s.index == nil {
		return out
	}
	for path, entry := range s.index.Entries {
		if _, ok := ProjectOfLinkedMailAnalysis(path); !ok {
			continue
		}
		created := entry.Created
		if created == "" {
			created = entry.Updated
		}
		if created == "" || created < cutoff {
			continue
		}
		for _, tag := range entry.Tags {
			d := strings.ToLower(strings.TrimSpace(tag))
			// Mail-analysis tags carry exactly the sender domain; a defensive
			// dot check keeps any future non-domain tag out of the set.
			if d == "" || !strings.Contains(d, ".") || IsFreemailDomain(d) {
				continue
			}
			out[d] = struct{}{}
		}
	}
	return out
}
