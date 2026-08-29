package server

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/groupware"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// approvalWikiExcerptMaxRunes bounds the analysis gist that lands in 로그.md —
// the full analysis stays in the approval cache; the wiki keeps the decision
// trail readable.
const approvalWikiExcerptMaxRunes = 480

// resolveApprovalProject pins an approval to exactly one active project.
// Title first (short, high-precision); if no unique hit, title+body so a
// body-only site/project mention still files. Ambiguous matches stay silent
// (추측 금지 — UniqueProjectInText's specificity-tie rule).
func resolveApprovalProject(store *wiki.Store, title, body string) (wiki.ProjectRef, bool) {
	if store == nil {
		return wiki.ProjectRef{}, false
	}
	title = strings.TrimSpace(title)
	if title != "" {
		if ref, ok := store.UniqueProjectInText(title); ok {
			return ref, true
		}
	}
	hay := title
	if b := strings.TrimSpace(body); b != "" {
		if hay != "" {
			hay += "\n"
		}
		hay += b
	}
	if hay == "" {
		return wiki.ProjectRef{}, false
	}
	return store.UniqueProjectInText(hay)
}

// logApprovalAnalysisToWiki appends the approval-analysis gist to the owning
// project's 로그.md as a `결재` op (layout rule: 결재 events append to the log,
// never a new page) and prepends a glance bullet on the 대표페이지 현재 상태.
// Requires rec.ProjectFile (agent judgment) AND a unique project match.
// Idempotent per docId. Best-effort: failures never disturb the caller.
//
// body is optional matching fuel (title alone often omits the site/project name).
func (s *Server) logApprovalAnalysisToWiki(rec *groupware.ApprovalAnalysisRecord, body string) {
	if s == nil || s.wikiStore == nil || rec == nil || !rec.ProjectFile {
		return
	}
	docID := strings.TrimSpace(rec.DocID)
	title := strings.TrimSpace(rec.Title)
	analysis := strings.TrimSpace(rec.Analysis)
	if docID == "" || title == "" || analysis == "" {
		return
	}
	ref, ok := resolveApprovalProject(s.wikiStore, title, body)
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
	if err != nil && !errors.Is(err, errApprovalLogDuplicate) && s.logger != nil {
		s.logger.Warn("approval analysis wiki log failed", "docId", docID, "project", project, "error", err)
	}
	if err == nil && s.logger != nil {
		s.logger.Info("approval analysis logged to wiki", "docId", docID, "project", project)
	}

	// Glance surface (메일 패리티): 대표페이지 현재 상태 — digest/모아보기.
	s.appendApprovalStatusToProject(ref.Path, title, "approval:"+docID)
}

// appendApprovalStatusToProject prepends one dated bullet on the project's
// 대표페이지 현재 상태. Idempotent by ref (dealRefMarker). Best-effort.
func (s *Server) appendApprovalStatusToProject(repPath, title, ref string) {
	if s == nil || s.wikiStore == nil {
		return
	}
	line := "결재: " + textutil.TruncateRunes(approvalLogText(title), 60, "...")
	if err := s.wikiStore.AppendProjectStatusLine(repPath, line, ref, time.Now()); err != nil && s.logger != nil {
		s.logger.Warn("approval→project 현재 상태 갱신 실패", "path", repPath, "ref", ref, "error", err)
	}
}

// errApprovalLogDuplicate aborts UpdatePage without a warning — the section is
// already there (analysis rerun / force refresh).
var errApprovalLogDuplicate = sentinelError("approval log already recorded")

type sentinelError string

func (e sentinelError) Error() string { return string(e) }

// approvalAnalysisExcerpt keeps the gist: machine trailers (IMPORTANCE /
// PROJECT_FILE) drop, and the remainder is rune-bounded.
func approvalAnalysisExcerpt(analysis string) string {
	out := stripApprovalImportanceMarker(analysis)
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

// approvalProjectStateContext renders the matched project's own state for the
// approval prompt: summary plus the curated 현재 상태 bullets from its
// 대표페이지.
//
// resolveApprovalProject already identified the project, and the prompt then
// carried only its NAME. The other context an approval gets — 단가 이력 and
// 전례 — answers whether the AMOUNT is right; neither says anything about
// where the project stands, which is the half that decides whether the
// document should go out at all.
//
// Bounded and best-effort: an unreadable page yields "" and the prompt keeps
// the plain name, exactly as before.
func approvalProjectStateContext(store *wiki.Store, ref wiki.ProjectRef) string {
	if store == nil || strings.TrimSpace(ref.Path) == "" {
		return ""
	}
	page, err := store.ReadPage(ref.Path)
	if err != nil || page == nil {
		return ""
	}
	var parts []string
	if summary := strings.TrimSpace(page.Meta.Summary); summary != "" {
		parts = append(parts, "- summary: "+summary)
	}
	if status := strings.TrimSpace(page.Section("현재 상태")); status != "" {
		parts = append(parts, "- 현재 상태: "+status)
	}
	if len(parts) == 0 {
		return ""
	}
	return truncateRunes("### 「"+ref.Name+"」 현재 상태 (위키)\n"+
		strings.Join(parts, "\n"), approvalProjectStateMaxRune)
}

// approvalProjectStateMaxRune bounds the project-state block. Smaller than the
// document body budget: this is orienting context, not the subject of the
// analysis.
const approvalProjectStateMaxRune = 1200
