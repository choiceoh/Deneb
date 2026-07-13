// counterparties.go — derives the "active counterparty" sender-domain set from
// project-linked mail-analysis pages, for the inbox priority scorer's
// combined-signal boost (mailpriority): the same 견적/기한 mail matters more
// when it comes from a company the user is actively working with.
//
// Source of truth: every analyzed mail the analyzer linked to a project lands
// at 프로젝트/<project>/메일분석/<msgID>.md with the sender's domain as a
// frontmatter tag (wiki_mail_analysis.go buildMailAnalysisPage). That makes
// the in-memory Index sufficient — no page-body reads — and "recent
// project-linked mail from this domain" a cheap, deterministic definition of
// an active business relationship. Freemail domains are excluded: a
// gmail.com/naver.com tag identifies a person, not a counterparty company, and
// would boost unrelated senders sharing the host. (Freemail-only counterparty
// contacts are a known miss — matching them needs per-address extraction from
// page bodies, not worth the I/O for a glance marker.)
package wiki

import (
	"sort"
	"strings"
)

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

// isFreemailDomain reports whether a (lowercased or not) mail domain is a
// consumer host that must never count as a counterparty identity.
func isFreemailDomain(domain string) bool {
	_, ok := freemailDomains[strings.ToLower(strings.TrimSpace(domain))]
	return ok
}

// ActiveCounterpartyDomains returns the lowercase sender domains of
// project-linked mail-analysis pages created on/after cutoff (YYYY-MM-DD;
// lexical compare — Created is the analysis day and, unlike Updated, is not
// re-stamped by later metadata churn such as ReclassifyUnlinkedMailAnalyses
// moving an old mail into a project, which would wrongly re-activate a stale
// sender domain for the whole window). Created is persisted in the Index.md
// TSV, so it survives gateway restarts; entries without it (parsed from a
// pre-created-column Index.md) fall back to Updated. Freemail domains are
// excluded.
// The iteration happens under the store's read lock (the returned map is a
// fresh copy): Index entries are mutated in place by writers, so callers
// must never walk the live Index themselves — use Store.SnapshotEntries
// (same contract as Tier1Pages).
func (s *Store) ActiveCounterpartyDomains(cutoff string) map[string]struct{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]struct{})
	if s.Index == nil {
		return out
	}
	for path, entry := range s.Index.Entries {
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
			if d == "" || !strings.Contains(d, ".") || isFreemailDomain(d) {
				continue
			}
			out[d] = struct{}{}
		}
	}
	return out
}

// maxCounterpartyProjects caps how many linked projects a single domain lists
// in the mail-analysis party anchor — a glance label, not a ledger.
const maxCounterpartyProjects = 3

// CounterpartyProjects returns, per active counterparty domain, the distinct
// project names its project-linked mail analyses belong to — most recently
// active project first, capped at maxCounterpartyProjects. Same window,
// created-date, and freemail rules as ActiveCounterpartyDomains (the walks are
// kept separate on purpose: this one carries per-project recency bookkeeping
// the boolean set never needs). Deterministic wiki-Index walk under the read
// lock; feeds the mail-analysis party anchor's counterparty labels.
func (s *Store) CounterpartyProjects(cutoff string) map[string][]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Index == nil {
		return map[string][]string{}
	}
	// domain → project → latest created (for recency ordering).
	latest := map[string]map[string]string{}
	for path, entry := range s.Index.Entries {
		project, ok := ProjectOfLinkedMailAnalysis(path)
		if !ok {
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
			if d == "" || !strings.Contains(d, ".") || isFreemailDomain(d) {
				continue
			}
			m := latest[d]
			if m == nil {
				m = map[string]string{}
				latest[d] = m
			}
			if created > m[project] {
				m[project] = created
			}
		}
	}
	out := make(map[string][]string, len(latest))
	for d, projs := range latest {
		names := make([]string, 0, len(projs))
		for p := range projs {
			names = append(names, p)
		}
		sort.Slice(names, func(i, j int) bool {
			if projs[names[i]] != projs[names[j]] {
				return projs[names[i]] > projs[names[j]] // newest activity first
			}
			return names[i] < names[j] // deterministic tie-break
		})
		if len(names) > maxCounterpartyProjects {
			names = names[:maxCounterpartyProjects]
		}
		out[d] = names
	}
	return out
}
