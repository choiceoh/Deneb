package server

import (
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// approvalWikiExcerptMaxRunes bounds the analysis gist that lands in 로그.md —
// the full analysis stays in the approval cache; the wiki keeps the decision
// trail readable.
const approvalWikiExcerptMaxRunes = 480

// logApprovalAnalysisToWiki appends the approval-analysis gist to the owning
// project's 로그.md as a `결재` op (layout rule: 결재 events append to the log,
// never a new page). Deterministic project match only — no unique project in
// the title means silent skip (추측 금지). Idempotent per docId via a ref
// marker in the section body. Best-effort: failures never disturb the caller.
func (s *Server) logApprovalAnalysisToWiki(rec *groupware.ApprovalAnalysisRecord) {
	if s == nil || s.wikiStore == nil || rec == nil {
		return
	}
	docID := strings.TrimSpace(rec.DocID)
	title := strings.TrimSpace(rec.Title)
	analysis := strings.TrimSpace(rec.Analysis)
	if docID == "" || title == "" || analysis == "" {
		return
	}
	ref, ok := s.wikiStore.UniqueProjectInText(title)
	if !ok {
		return
	}
	project, ok := wiki.ProjectNameOf(ref.Path)
	if !ok {
		return
	}

	today := dentime.Now().Format("2006-01-02")
	marker := "<!-- ref=approval:" + docID + " -->"
	var section strings.Builder
	section.WriteString("## [" + today + "] 결재 | " + approvalLogText(title) + "\n")
	meta := make([]string, 0, 2)
	if d := strings.TrimSpace(rec.Drafter); d != "" {
		meta = append(meta, "기안 "+approvalLogText(d))
	}
	if imp := strings.TrimSpace(rec.Importance); imp != "" {
		meta = append(meta, "중요도 "+imp)
	}
	if len(meta) > 0 {
		section.WriteString("- " + strings.Join(meta, " · ") + "\n")
	}
	section.WriteString("\n" + approvalAnalysisExcerpt(analysis) + "\n\n" + marker + "\n")

	err := s.wikiStore.UpdatePage(wiki.LogPagePath(project), func(cur *wiki.Page) (*wiki.Page, error) {
		if cur == nil {
			p := wiki.NewPage(ref.Name+" 진행 로그", "프로젝트", nil)
			p.Meta.Type = "log"
			p.Meta.Summary = ref.Name + " 진행 로그"
			p.Body = section.String()
			return p, nil
		}
		if strings.Contains(cur.Body, marker) {
			return nil, errApprovalLogDuplicate
		}
		cur.Body = strings.TrimRight(cur.Body, "\n") + "\n\n" + section.String()
		cur.Meta.Updated = today
		return cur, nil
	})
	if err != nil && err != errApprovalLogDuplicate && s.logger != nil {
		s.logger.Warn("approval analysis wiki log failed", "docId", docID, "project", project, "error", err)
	}
	if err == nil && s.logger != nil {
		s.logger.Info("approval analysis logged to wiki", "docId", docID, "project", project)
	}
}

// errApprovalLogDuplicate aborts UpdatePage without a warning — the section is
// already there (analysis rerun / force refresh).
var errApprovalLogDuplicate = errSentinel("approval log already recorded")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// approvalAnalysisExcerpt keeps the gist: the IMPORTANCE marker line drops
// (already surfaced as 중요도), and the remainder is rune-bounded.
func approvalAnalysisExcerpt(analysis string) string {
	var kept []string
	for _, line := range strings.Split(analysis, "\n") {
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "IMPORTANCE:") {
			continue
		}
		kept = append(kept, line)
	}
	out := strings.TrimSpace(strings.Join(kept, "\n"))
	if utf8.RuneCountInString(out) > approvalWikiExcerptMaxRunes {
		out = string([]rune(out)[:approvalWikiExcerptMaxRunes]) + "\n…(전문은 결재 상세에서)"
	}
	return out
}

// approvalLogText flattens third-party text for a markdown heading/bullet so a
// crafted 결재 title can't inject new log sections (site-visit parity).
func approvalLogText(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' || r < 0x20 {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
