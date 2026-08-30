// wiki_mail_autoflow.go — duplicate suppression between the two paths that can
// both describe the same meeting.
//
// Plaud reaches Deneb twice for one recording: the MCP polling service reads the
// transcript and writes a 회의록 page (the deep, meeting-shaped path), and Plaud's
// cloud separately mails an [Plaud-AutoFlow] notice whose analysis becomes a
// 메일분석 page. The mail path is deliberately kept as the auth-fallback — when the
// MCP token expires it is the only record — so it cannot simply be removed.
//
// The work feed already collapses the second card (workfeed.AppendIfNew); the
// wiki did not, and the 2026-08-29 audit found 33 메일분석 pages against 48 회의록
// pages total, with one 07-10 meeting present four times. This file is the wiki's
// half of the same rule: withhold the derived page when the deep path already
// covered the meeting, write it when it did not.
package server

import (
	"strings"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

const (
	// autoFlowSender and autoFlowSubjectPrefix identify an AutoFlow notice.
	// Both are required: a human forwarding the notice keeps the subject but not
	// the sender, and plaud.ai's other mail (billing, product) keeps the sender
	// but not the subject. Neither reads the analysis text, so unlike
	// AnalysisNonBusiness this gate does not depend on how the model phrased
	// anything.
	autoFlowSender        = "no-reply@plaud.ai"
	autoFlowSubjectPrefix = "[Plaud-AutoFlow]"

	// mailAnalysisDirName is the per-project 메일분석 folder segment.
	mailAnalysisDirName = "메일분석"
)

// meetingPageLister is the sliver of the wiki store this file needs. Narrow on
// purpose: both call sites hold a *wiki.Store, and a test needs no more.
type meetingPageLister interface {
	ListPages(category string) ([]string, error)
}

// autoFlowMeetingName returns the meeting name carried by a Plaud AutoFlow mail
// subject, and whether this is such a mail at all. The name is the recording
// name Plaud also gave the MCP file, which is what makes the slug comparable.
func autoFlowMeetingName(from, subject string) (string, bool) {
	if !strings.Contains(strings.ToLower(from), autoFlowSender) {
		return "", false
	}
	rest, ok := strings.CutPrefix(strings.TrimSpace(subject), autoFlowSubjectPrefix)
	if !ok {
		return "", false
	}
	name := strings.TrimSpace(rest)
	if name == "" {
		return "", false
	}
	return name, true
}

// autoFlowMeetingCovered reports the 회의록 page that already covers this mail's
// meeting, or "" when none does — in which case the mail page must still be
// written, because the deep path has not run (or cannot).
//
// The match is on the title slug and is deliberately NOT scoped to the project.
// The mail analyzer's RELATED_PROJECTS and the meeting service's project
// resolution disagree on 12 of the 28 covered cases in the live wiki, so scoping
// would miss 43% of the duplicates it exists to catch. The slug carries the
// meeting's own date and title, and no two 회의록 stems in the corpus share a
// prefix, so cross-project collision is not the risk that scoping would trade
// for.
func autoFlowMeetingCovered(store meetingPageLister, from, subject string) string {
	name, ok := autoFlowMeetingName(from, subject)
	if !ok || store == nil {
		return ""
	}
	slug := wiki.MeetingSlug(name)
	if slug == "" {
		return ""
	}
	pages, err := store.ListPages(wikiProjectCategory)
	if err != nil {
		// Listing failed: we cannot tell whether the meeting is covered, so we
		// write. Losing a page to a transient read error is the one outcome this
		// gate must never produce.
		return ""
	}
	return wiki.MeetingPageCoveringSlug(pages, slug)
}

// foldCoveredMailAnalysis removes the 메일분석 page written from the AutoFlow
// notice for meetingName, and reports which page it removed.
//
// This is the losing side of the same rule autoFlowMeetingCovered enforces at
// write time, and it exists because that gate cannot see into the future: the
// Plaud poll runs every 15 minutes, so a notice arriving inside that window
// writes its page before the meeting record exists. Deleting is safe here in a
// way it is not in general — identity is exact (the AutoFlow subject IS the
// recording name, matched through the shared slug rule), the caller has just
// written the authoritative record of the same meeting, and the mail itself
// stays in the archive. On the live corpus the same match scored 28/28 with no
// false positives.
func (s *Server) foldCoveredMailAnalysis(meetingName string) (string, bool) {
	if s.wikiStore == nil || strings.TrimSpace(meetingName) == "" {
		return "", false
	}
	want := wiki.MeetingSlug(meetingName)
	if want == "" {
		return "", false
	}
	pages, err := s.wikiStore.ListPages(wikiProjectCategory)
	if err != nil {
		return "", false
	}
	for _, rp := range pages {
		if !strings.Contains(rp, "/"+mailAnalysisDirName+"/") {
			continue
		}
		page, err := s.wikiStore.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		name, ok := strings.CutPrefix(strings.TrimSpace(page.Meta.Title), autoFlowSubjectPrefix)
		if !ok || wiki.MeetingSlug(strings.TrimSpace(name)) != want {
			continue
		}
		if err := s.wikiStore.DeletePage(rp); err != nil {
			s.logger.Warn("중복 메일분석 삭제 실패", "path", rp, "error", err)
			return "", false
		}
		return rp, true
	}
	return "", false
}
