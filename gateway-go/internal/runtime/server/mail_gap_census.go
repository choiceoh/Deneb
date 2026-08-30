// mail_gap_census.go — the net under the mail→wiki pipeline.
//
// Two defects found on 2026-08-29/30 shared one property: both grew for months
// without failing anything. 33 duplicate 메일분석 pages accumulated against 48
// 회의록 pages because the work feed deduplicated cards and the wiki did not; 96
// messages were never analyzed because the poll window closed before anyone
// looked. Neither had a number attached to it, so neither was noticeable until
// somebody went looking by hand.
//
// This task attaches the numbers. It writes nothing and decides nothing — it
// counts, and logs at Warn when a count crosses a threshold that says a
// mechanism has stopped working. That is the whole point: the repairs those two
// audits produced are themselves silent if they break.
package server

import (
	"context"
	"strings"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

const (
	mailGapCensusInterval = 12 * time.Hour
	// mailGapPendingWarn is the level at which "not yet analyzed" stops looking
	// like flight time. The healthy recent cohorts sit at 0–2% of ~65 messages
	// per week, so a standing queue this size means the drain has stalled.
	mailGapPendingWarn = 25
	// mailGapDuplicateWarn is deliberately 1: after the AutoFlow gate, a
	// 메일분석 page whose meeting already has a 회의록 page should not exist. One is
	// the 15-minute poll race; a handful means the gate stopped matching, which
	// is what happens if Plaud changes its subject format.
	mailGapDuplicateWarn = 3
)

type mailGapCensusTask struct{ s *Server }

func (t mailGapCensusTask) Name() string                  { return "mail-gap-census" }
func (t mailGapCensusTask) Interval() time.Duration       { return mailGapCensusInterval }
func (t mailGapCensusTask) Run(ctx context.Context) error { return t.s.runMailGapCensus(ctx) }

func (s *Server) registerMailGapCensusTask(homeDir string) {
	if s.autonomousSvc == nil {
		return
	}
	if _, ok := s.productionStateDir(homeDir); !ok {
		return
	}
	s.autonomousSvc.RegisterTask(mailGapCensusTask{s: s})
}

func (s *Server) runMailGapCensus(_ context.Context) error {
	pending := len(s.mailWorkStatePathStore().PendingAnalysis(mailBackfillBacklogCap, mailBackfillMinAge))
	dupes := s.countAutoFlowDuplicates()

	level := s.logger.Info
	if pending >= mailGapPendingWarn || dupes >= mailGapDuplicateWarn {
		level = s.logger.Warn
	}
	level("mail gap census",
		"미분석대기", pending, "임계", mailGapPendingWarn,
		"회의중복", dupes, "임계", mailGapDuplicateWarn)
	return nil
}

// countAutoFlowDuplicates counts 메일분석 pages whose meeting already has a 회의록
// page — exactly what autoFlowMeetingCovered prevents at write time and
// wikirepair's autoflowdup pass clears retroactively. A rising count is the
// signature of the gate no longer matching.
func (s *Server) countAutoFlowDuplicates() int {
	if s.wikiStore == nil {
		return 0
	}
	pages, err := s.wikiStore.ListPages(wikiProjectCategory)
	if err != nil {
		return 0
	}
	n := 0
	for _, rp := range pages {
		if !strings.Contains(rp, "/메일분석/") {
			continue
		}
		page, err := s.wikiStore.ReadPage(rp)
		if err != nil || page == nil {
			continue
		}
		name, ok := strings.CutPrefix(strings.TrimSpace(page.Meta.Title), autoFlowSubjectPrefix)
		if !ok {
			continue
		}
		if wiki.MeetingPageCoveringSlug(pages, wiki.MeetingSlug(strings.TrimSpace(name))) != "" {
			n++
		}
	}
	return n
}
