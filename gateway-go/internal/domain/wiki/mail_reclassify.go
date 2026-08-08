// mail_reclassify.go — retroactive filing of unlinked mail analyses.
//
// A mail the analyzer couldn't link at arrival lands in the category-level
// 프로젝트/메일분석/ bucket. Projects appear later (a folder minted the day after
// the first mails about it), so the bucket accumulates pages that NOW have an
// obvious home. This pass re-files them deterministically — no LLM:
//
//	signal 1: a Related entry resolving to a project (the link pruner repairs
//	          related paths, so a repaired edge is a strong signal);
//	signal 2: the mail title names exactly ONE known project (normalized
//	          containment; ambiguity = stay put — a wrong filing is worse
//	          than an unfiled mail);
//	signal 3: sender-domain evidence histogram — the mail's sender domain has
//	          ≥3 mails ALREADY FILED under exactly one active project and zero
//	          under any other (measured 2026-08-08: the whole unlinked backlog
//	          had zero signal-1/2 hits while sunkean.com sat at 5:0 filed
//	          evidence). The corpus itself is the mapping — no external table.
//	          OBSERVE-FIRST: proposals are returned, not moved, until
//	          DENEB_MAIL_RECLASS_DOMAIN=1 arms it (the contacts-dedup
//	          over-merge incident is why batch movers audit before acting).
//
// Runs from the wiki-review task, capped per cycle. MovePage keeps every
// inbound reference intact; the project's 대표페이지 is appended to the moved
// mail's Related so the project corner picks it up (projectOwnedRefs resolves
// ownership through that edge).
package wiki

import (
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

// ReclassifyResult is one re-filed (or, for observe-mode domain proposals,
// would-be re-filed) mail page. Signal names which rule matched — the audit
// trail that makes judgment quality reviewable.
type ReclassifyResult struct {
	From    string
	To      string
	Project string
	Signal  string // "related" | "title" | "domain"
}

// ReclassifyUnlinkedMailAnalyses re-files pages from the category-level
// 메일분석 bucket into per-project slots, at most maxMoves per call. moved
// holds actual moves (signals 1/2 always; signal 3 too once armed);
// domainProposals holds observe-mode signal-3 candidates that were NOT moved —
// the caller records them for operator audit. Armed runs return proposals
// empty (they become moves).
func (s *Store) ReclassifyUnlinkedMailAnalyses(now time.Time, maxMoves int) (moved, domainProposals []ReclassifyResult) {
	if s == nil || maxMoves <= 0 {
		return nil, nil
	}
	bucket := projectCategoryPrefix + "/" + mailAnalysisDir
	pages, err := s.ListPages(bucket)
	if err != nil {
		return nil, nil
	}

	projects := s.knownProjects()
	repByName := make(map[string]string, len(projects))
	for _, ref := range projects {
		if name, ok := ProjectNameOf(ref.Path); ok {
			repByName[name] = ref.Path
		}
	}
	hist := s.buildMailDomainHistogram(repByName)
	armed := mailDomainSignalArmed()

	for _, rp := range pages {
		if len(moved) >= maxMoves {
			break
		}
		rp = strings.ReplaceAll(rp, "\\", "/")
		if path.Dir(rp) != bucket { // only the flat unlinked bucket
			continue
		}
		page, perr := s.ReadPage(rp)
		if perr != nil || page == nil {
			continue
		}
		project, signal := reclassifyTarget(page, projects)
		if project == "" {
			project = domainReclassifyTarget(page, hist)
			if project == "" {
				continue
			}
			signal = "domain"
			if !armed {
				// Observe mode: record what WOULD move, touch nothing. Bounded
				// by the same per-cycle cap so the audit trail can't balloon.
				if len(domainProposals) < maxMoves {
					domainProposals = append(domainProposals, ReclassifyResult{
						From:    rp,
						To:      MailAnalysisPagePath(project, strings.TrimSuffix(path.Base(rp), ".md")),
						Project: project,
						Signal:  signal,
					})
				}
				continue
			}
		}
		repPath, ok := repByName[project]
		if !ok {
			continue
		}
		dst := MailAnalysisPagePath(project, strings.TrimSuffix(path.Base(rp), ".md"))
		if err := s.MovePage(rp, dst); err != nil {
			continue // e.g. same msgID already filed there — leave for the operator
		}
		// The project-corner ownership edge: mail → 대표페이지 via Related.
		_ = s.UpdatePage(dst, func(cur *Page) (*Page, error) {
			if cur == nil {
				return nil, nil
			}
			for _, r := range cur.Meta.Related {
				if r == repPath {
					return nil, nil // already linked — skip the write
				}
			}
			cur.Meta.Related = append(cur.Meta.Related, repPath)
			cur.Meta.Updated = now.Format("2006-01-02")
			return cur, nil
		})
		moved = append(moved, ReclassifyResult{From: rp, To: dst, Project: project, Signal: signal})
	}
	return moved, domainProposals
}

// reclassifyTarget picks the owning project for an unlinked mail page, or ""
// when no unambiguous signal exists (모호하면 잔류 — a wrong filing is worse than
// an unfiled mail). Matching runs against the caller's already-fetched project
// list; re-listing per candidate was a silent N× knownProjects scan.
func reclassifyTarget(page *Page, projects []ProjectRef) (project, signal string) {
	// Signal 1: Related entries resolving into projects — but only when they
	// all agree. Two DISTINCT related projects are exactly the ambiguity the
	// doctrine says to leave alone (returning the first was arbitrary).
	related := ""
	for _, r := range page.Meta.Related {
		name, ok := ProjectNameOf(strings.ReplaceAll(strings.TrimSpace(r), "\\", "/"))
		if !ok {
			continue
		}
		if related != "" && related != name {
			return "", "" // ≥2 distinct projects → ambiguous, stay put
		}
		related = name
	}
	if related != "" {
		return related, "related"
	}
	// Signal 2: the title resolves to exactly one project by its most
	// specific identity key (uniqueProjectIn): a title naming one project
	// outright files there even when siblings also hit via their shared
	// 거래처 key, while a bare client mention ties across the client's
	// projects and stays put.
	hay := normalizeTitleKey(strings.TrimSpace(page.Meta.Title))
	if hay == "" {
		return "", ""
	}
	if ref, ok := uniqueProjectIn(hay, projects); ok {
		if name, k := ProjectNameOf(ref.Path); k {
			return name, "title"
		}
	}
	return "", ""
}

// mailDomainSignalArmed reads the rollout gate for signal 3. Off (any value
// but "1", including unset) = observe mode: proposals are surfaced, nothing
// moves — the reviewer's observe→arm pattern for batch mutations.
func mailDomainSignalArmed() bool {
	return os.Getenv("DENEB_MAIL_RECLASS_DOMAIN") == "1"
}

// mailDomainMinEvidence is the minimum already-filed mail count before a
// domain's histogram may fire: a single accidental filing must not seed a
// self-reinforcing mapping (the keyword-filter false-positive incident is the
// cautionary precedent for evidence thresholds).
const mailDomainMinEvidence = 3

// mailDomainBlocklist holds domains that can never identify a project: our
// own (internal senders write about every project) and freemail providers
// (counterparty staff on personal accounts share domains across companies).
var mailDomainBlocklist = map[string]bool{
	"topsolar.kr": true,
	"gmail.com":   true, "naver.com": true, "daum.net": true,
	"hanmail.net": true, "nate.com": true, "kakao.com": true,
	"outlook.com": true, "hotmail.com": true, "yahoo.com": true,
}

// mailDomainTagPattern recognizes the sender-domain tag the mail analyzer
// stamps on every analysis page (e.g. "sunkean.com").
var mailDomainTagPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z]{2,}$`)

// mailSenderDomain extracts the sender domain from an analysis page's tags,
// "" when absent or blocklisted.
func mailSenderDomain(page *Page) string {
	if page == nil {
		return ""
	}
	for _, tag := range page.Meta.Tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if mailDomainTagPattern.MatchString(tag) {
			if mailDomainBlocklist[tag] {
				return ""
			}
			return tag
		}
	}
	return ""
}

// buildMailDomainHistogram counts already-filed analysis mails per (sender
// domain → project folder) across the ACTIVE projects in repByName. Reads the
// in-memory index snapshot only (tags ride on IndexEntry) — zero file reads.
func (s *Store) buildMailDomainHistogram(repByName map[string]string) map[string]map[string]int {
	hist := make(map[string]map[string]int)
	for p, entry := range s.snapshotEntries() {
		p = strings.ReplaceAll(p, "\\", "/")
		if !IsMailAnalysisPath(p) {
			continue
		}
		folder, ok := ProjectNameOf(p)
		if !ok {
			continue
		}
		if _, active := repByName[folder]; !active {
			continue // archived/unknown projects contribute no evidence
		}
		domain := mailSenderDomain(&Page{Meta: Frontmatter{Tags: entry.Tags}})
		if domain == "" {
			continue
		}
		byProject := hist[domain]
		if byProject == nil {
			byProject = make(map[string]int)
			hist[domain] = byProject
		}
		byProject[folder]++
	}
	return hist
}

// domainReclassifyTarget is signal 3: the mail's sender domain has enough
// filed evidence (≥ mailDomainMinEvidence) under exactly ONE active project
// and none anywhere else. Any competing project — even a single mail — is the
// multi-deal-counterparty ambiguity the doctrine leaves alone.
func domainReclassifyTarget(page *Page, hist map[string]map[string]int) string {
	domain := mailSenderDomain(page)
	if domain == "" {
		return ""
	}
	byProject := hist[domain]
	if len(byProject) != 1 {
		return "" // zero evidence, or split across projects → stay put
	}
	for folder, count := range byProject {
		if count >= mailDomainMinEvidence {
			return folder
		}
	}
	return ""
}
