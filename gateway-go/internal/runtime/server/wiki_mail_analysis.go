// wiki_mail_analysis.go — adapter between miniapp.gmail.analyze and the
// wiki store. Lifted out of method_registry.go so the wiring there
// stays a single line and the page-shaping logic has room to breathe.
//
// One page per message ID. We never rewrite an existing page from this
// path (the analysis cache short-circuits before reaching the wiki),
// so the frontmatter is set once at creation and left alone.

package server

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

// wikiProjectCategory is the wiki category whose pages are offered to the
// email analyzer as related-project candidates.
const wikiProjectCategory = "프로젝트"

// mailAnalysisWikiPath maps a Gmail message ID to its wiki page path — the
// project's 메일분석/ slot when the analyzer linked one, else the category-level
// unlinked bucket (see wiki/project_layout.go). One page per message.
func mailAnalysisWikiPath(msgID string, relatedProjects []string) string {
	return wiki.MailAnalysisPagePath(mailProjectName(relatedProjects), msgID)
}

// mailProjectName picks the owning project from the analyzer's related-project
// list: the first entry that is a real project 대표페이지 (new in-folder or legacy
// flat form). Empty when the mail relates to no project. The related list is the
// reliable project signal the analyzer computed — far better than guessing the
// project from the mail subject.
func mailProjectName(relatedProjects []string) string {
	for _, r := range relatedProjects {
		r = strings.TrimSpace(r)
		if !wiki.IsProjectRepPage(r) {
			continue
		}
		if name, ok := wiki.ProjectNameOf(r); ok {
			return name
		}
	}
	return ""
}

// buildMailAnalysisPage renders a wiki.Page from a fresh analysis. The
// body is a short metadata blockquote followed by the LLM markdown so
// memory.search hits show the From/Date/ID in the preview.
func buildMailAnalysisPage(in handlerminiapp.WikiAnalysisInput) *wiki.Page {
	title := strings.TrimSpace(in.Subject)
	if title == "" {
		title = "(제목 없음) " + in.MsgID
	}
	today := time.Now().UTC().Format("2006-01-02")

	// Domain tag groups newsletters from the same vendor without
	// flooding memory.search with noise. Empty when From has no @.
	var tags []string
	if d := senderDomain(in.From); d != "" {
		tags = []string{d}
	}

	var body strings.Builder
	body.WriteString("> From: ")
	body.WriteString(in.From)
	body.WriteString("\n> Date: ")
	body.WriteString(in.Date)
	body.WriteString("\n> Message ID: `")
	body.WriteString(in.MsgID)
	body.WriteString("`\n\n")
	body.WriteString(in.Analysis)

	return &wiki.Page{
		Meta: wiki.Frontmatter{
			Title:      title,
			Summary:    senderShortLabel(in.From) + " 메일 분석",
			Category:   wikiProjectCategory, // 프로젝트 (raw-data sub-folder; bucket = path dir)
			Tags:       tags,
			Related:    in.RelatedProjects, // wiki paths of projects the analyzer linked
			Created:    today,
			Updated:    today,
			Type:       "log",
			Confidence: "medium",
			Importance: 0.3,
		},
		Body: body.String(),
	}
}

// senderDomain pulls "domain.tld" from a From header. Handles both raw
// addresses and RFC 5322 display-name forms ("Name <a@b.com>").
func senderDomain(from string) string {
	s := from
	if i := strings.IndexByte(s, '<'); i >= 0 {
		if j := strings.IndexByte(s[i:], '>'); j >= 0 {
			s = s[i+1 : i+j]
		} else {
			s = s[i+1:]
		}
	}
	at := strings.IndexByte(s, '@')
	if at < 0 || at == len(s)-1 {
		return ""
	}
	return strings.TrimSpace(s[at+1:])
}

// senderShortLabel returns the display-name portion of a From header
// when present, falling back to the address otherwise.
func senderShortLabel(from string) string {
	if i := strings.IndexByte(from, '<'); i > 0 {
		return strings.TrimSpace(from[:i])
	}
	return strings.TrimSpace(from)
}

// projectCandidatesFn returns a provider that lists registered project wiki
// pages (path + title + summary) for related-project selection during email
// analysis. Returns nil when the wiki store is unavailable. Shared by the
// autonomous poller and the Mini App's manual analyze path so both cite
// projects from the same source.
func (s *Server) projectCandidatesFn() func() []gmailpoll.ProjectCandidate {
	return func() []gmailpoll.ProjectCandidate {
		store := s.wikiStore
		if store == nil {
			return nil
		}
		// Only project 대표페이지 are related-project candidates — the analyzer must
		// not cite an auto-generated mail dump, deal ledger page, or sub-page as a
		// "related project". KnownProjects owns that layout rule.
		refs := store.KnownProjects()
		cands := make([]gmailpoll.ProjectCandidate, 0, len(refs))
		for _, r := range refs {
			cands = append(cands, gmailpoll.ProjectCandidate{
				Path:    r.Path,
				Title:   r.Name,
				Summary: r.Summary,
			})
		}
		return cands
	}
}

// makeMailAnalysisSink returns the OnAnalyzed callback for the autonomous
// poller: it persists each individually-analyzed email into the Mini App's
// analysis cache AND writes a per-message wiki page (Related = projects),
// mirroring what the manual analyze handler does on a fresh run. This is
// what lets a polled email show up already-analyzed in the Mini App with no
// manual tap.
func (s *Server) makeMailAnalysisSink() func(*gmail.MessageDetail, gmailpoll.AnalysisResult) error {
	cacheStore := handlerminiapp.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis"))
	workStore := mailwork.New(filepath.Join(s.denebDir, "mail_work_state.json"))
	return func(msg *gmail.MessageDetail, res gmailpoll.AnalysisResult) error {
		if msg == nil {
			return nil
		}
		var errs []error
		if err := cacheStore.SaveAnalysis(handlerminiapp.CachedAnalysis{
			MsgID:           msg.ID,
			Subject:         msg.Subject,
			From:            msg.From,
			Date:            msg.Date,
			Analysis:        res.Text,
			Importance:      res.Importance,
			RelatedProjects: res.RelatedProjects,
			CreatedAt:       time.Now().UTC(),
		}); err != nil {
			s.logger.Warn("mail analysis cache 저장 실패", "id", msg.ID, "error", err)
			errs = append(errs, err)
		}
		if s.wikiStore != nil {
			page := buildMailAnalysisPage(handlerminiapp.WikiAnalysisInput{
				MsgID:           msg.ID,
				Subject:         msg.Subject,
				From:            msg.From,
				Date:            msg.Date,
				Analysis:        res.Text,
				RelatedProjects: res.RelatedProjects,
			})
			if err := s.wikiStore.WritePage(mailAnalysisWikiPath(msg.ID, res.RelatedProjects), page); err != nil {
				s.logger.Warn("mail analysis 위키 저장 실패", "id", msg.ID, "error", err)
				errs = append(errs, err)
			}
			// Event-driven freshness: prepend a dated bullet onto each linked
			// project 대표페이지's "## 현재 상태" section so the 모아보기 reflects this
			// mail without waiting for the next dream cycle. No LLM — deterministic
			// line, idempotent by mail id. Best-effort: failures log, never fail
			// the analysis.
			s.appendMailStatusToProjects(msg, res)
		}
		// Mail no longer auto-creates to-dos (operator approval first): schedule-worthy
		// follow-ups surface as calendar PROPOSALS (the bell) to accept — see below.
		todoCount := 0
		// File any extracted business document onto a 거래 wiki page (silent
		// knowledge enrichment — no push). RelatedProjects is the analyzer's
		// resolved project linkage, stamped onto the deal notebook for exact
		// client-side project matching.
		s.fileDealFromMail(msg, res.Deal, res.RelatedProjects)
		// Propose schedule-worthy items (meetings, deadlines) as calendar
		// proposals the operator accepts from the calendar bell. See
		// mail_calendar.go. No push — bell badge only.
		calendarCount := s.autoProposeCalendarFromMail(msg, res.ActionItems, res.Deal, res.Importance)
		if _, err := workStore.MarkAnalysisDone(mailwork.AnalysisInput{
			MessageInput: mailwork.MessageInput{
				ID:              msg.ID,
				ThreadID:        msg.ThreadID,
				From:            msg.From,
				Subject:         msg.Subject,
				Date:            msg.Date,
				HasAttachment:   len(msg.Attachments) > 0,
				AttachmentCount: len(msg.Attachments),
			},
			Quality:               res.Importance,
			DerivedCountsKnown:    true,
			CalendarProposalCount: calendarCount,
			TodoCount:             todoCount,
		}); err != nil {
			s.logger.Warn("mail workflow state 저장 실패", "id", msg.ID, "error", err)
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}
}

func (s *Server) makeMailFeedDeliverySink() func([]string) {
	workStore := mailwork.New(filepath.Join(s.denebDir, "mail_work_state.json"))
	return func(ids []string) {
		for _, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, err := workStore.MarkFeedCreated(id); err != nil {
				s.logger.Warn("mail workflow feed 상태 저장 실패", "id", id, "error", err)
			}
		}
	}
}

func (s *Server) makeMailAnalysisFailureSink() func(*gmail.MessageDetail, error) {
	workStore := mailwork.New(filepath.Join(s.denebDir, "mail_work_state.json"))
	return func(msg *gmail.MessageDetail, err error) {
		if msg == nil || strings.TrimSpace(msg.ID) == "" {
			return
		}
		if _, werr := workStore.MarkAnalysisFailed(mailwork.MessageInput{
			ID:              msg.ID,
			ThreadID:        msg.ThreadID,
			From:            msg.From,
			Subject:         msg.Subject,
			Date:            msg.Date,
			HasAttachment:   len(msg.Attachments) > 0,
			AttachmentCount: len(msg.Attachments),
		}, err); werr != nil {
			s.logger.Warn("mail workflow failure 상태 저장 실패", "id", msg.ID, "error", werr)
		}
	}
}

// fileDealFromMail files a structured business-document extraction onto its
// counterparty's 거래 wiki page. Silent and best-effort: no push, deduped by
// the mail id, failures logged only. nil deal (non-deal mail) is a no-op.
func (s *Server) fileDealFromMail(msg *gmail.MessageDetail, deal *gmailpoll.DealInfo, relatedProjects []string) {
	if deal == nil || msg == nil || s.wikiStore == nil {
		return
	}
	relPath, created, err := s.wikiStore.UpsertDealPage(wiki.DealPageInput{
		Counterparty:    deal.Counterparty,
		DocType:         deal.DocType,
		Amount:          deal.Amount,
		Date:            deal.Date,
		DueDate:         deal.DueDate,
		Items:           deal.Items,
		Summary:         deal.Summary,
		SourceRef:       "mail:" + msg.ID,
		RelatedProjects: directProjectPages(relatedProjects), // deal→project graph edge
	}, time.Now())
	if err != nil {
		s.logger.Warn("mail→deal: 거래 페이지 저장 실패", "id", msg.ID, "counterparty", deal.Counterparty, "error", err)
		return
	}
	s.logger.Info("mail→deal: 거래 페이지 갱신", "id", msg.ID, "path", relPath, "created", created)

	// Pin the raw deal evidence to the same deal's notebook (keyed by the deal
	// page path, so curated facts (wiki) and citable evidence (notebook) share
	// one identity). Same IsDeal gate as the wiki write — only recognized deal
	// documents, not every email — so the notebook stays high-signal.
	s.pinDealEvidenceToNotebook(msg, deal, relPath, relatedProjects)

	// A brand-new deal page (created) means Deneb doesn't yet know which team owns
	// this deal — ask instead of guessing. created is true only at mint time, so
	// the question fires exactly once per new deal. See deal_question.go.
	if created {
		s.appendDealQuestionCard(deal, relPath)
	}
}

// pinDealEvidenceToNotebook auto-pins a deal email's extraction onto its deal
// notebook. Silent, best-effort, deduped by mail id (PinUnique): re-analysis of
// the same email never double-pins. Mirrors fileDealFromMail's silent behavior.
func (s *Server) pinDealEvidenceToNotebook(msg *gmail.MessageDetail, deal *gmailpoll.DealInfo, dealRef string, relatedProjects []string) {
	if s.notebookStore == nil || dealRef == "" {
		return
	}
	added, err := s.notebookStore.PinUnique(dealRef, deal.Counterparty, notebook.Source{
		Kind:  notebook.KindNote,
		Ref:   "mail:" + msg.ID, // provenance + dedup key
		Title: dealEvidenceTitle(deal),
		Text:  dealEvidenceText(deal, msg),
	})
	if err != nil {
		s.logger.Warn("mail→notebook: 딜 증거 핀 실패", "id", msg.ID, "deal", dealRef, "error", err)
		return
	}
	if added {
		s.logger.Info("mail→notebook: 딜 증거 핀", "id", msg.ID, "deal", dealRef)
	}

	// Stamp the analyzer's resolved project linkage onto the deal notebook (각인):
	// the dealRef is keyed by counterparty, which can differ from the project name,
	// so without this the project corner can't link the notebook to its project.
	// Idempotent and best-effort — a failure logs, never fails the analysis.
	if refs := directProjectPages(relatedProjects); len(refs) > 0 {
		if _, serr := s.notebookStore.StampProjectRefs(dealRef, refs); serr != nil {
			s.logger.Warn("mail→notebook: 프로젝트 각인 실패", "id", msg.ID, "deal", dealRef, "error", serr)
		}
	}
}

// directProjectPages filters a related-project list to project 대표페이지 paths
// (new in-folder or legacy flat form — wiki.IsProjectRepPage owns the rule),
// dropping raw-data pages (메일분석/, 거래/) and any non-프로젝트 entry, deduped in
// order. This is the reliable project signal the analyzer computed; the same
// filter gates both the 현재 상태 status update and notebook 각인 so they link to
// the same canonical pages.
func directProjectPages(related []string) []string {
	out := make([]string, 0, len(related))
	seen := make(map[string]bool, len(related))
	for _, r := range related {
		r = strings.TrimSpace(r)
		if !wiki.IsProjectRepPage(r) {
			continue
		}
		if seen[r] {
			continue
		}
		seen[r] = true
		out = append(out, r)
	}
	return out
}

// dealEvidenceTitle is the human label for a pinned deal source ("견적서 · 탑솔라").
func dealEvidenceTitle(deal *gmailpoll.DealInfo) string {
	parts := make([]string, 0, 2)
	if t := strings.TrimSpace(deal.DocType); t != "" {
		parts = append(parts, t)
	}
	if c := strings.TrimSpace(deal.Counterparty); c != "" {
		parts = append(parts, c)
	}
	if len(parts) == 0 {
		return "거래 문서"
	}
	return strings.Join(parts, " · ")
}

// dealEvidenceText renders the extracted deal fields as the pinned note body —
// the citable evidence a brief grounds on.
func dealEvidenceText(deal *gmailpoll.DealInfo, msg *gmail.MessageDetail) string {
	var b strings.Builder
	writeField := func(label, val string) {
		if v := strings.TrimSpace(val); v != "" {
			fmt.Fprintf(&b, "%s: %s\n", label, v)
		}
	}
	writeField("거래처", deal.Counterparty)
	writeField("문서", deal.DocType)
	writeField("금액", deal.Amount)
	writeField("일자", deal.Date)
	writeField("마감", deal.DueDate)
	if len(deal.Items) > 0 {
		fmt.Fprintf(&b, "품목: %s\n", strings.Join(deal.Items, ", "))
	}
	writeField("요약", deal.Summary)
	writeField("메일 제목", msg.Subject)
	return strings.TrimSpace(b.String())
}

// appendMailStatusToProjects prepends a dated status bullet onto every project
// 대표페이지 the analyzer linked, so the 모아보기 reflects a freshly-analyzed mail
// between dream cycles. Only direct project pages (프로젝트/<name>.md, count of
// "/" == 1) are touched — the raw-data sub-folders (mail-analyses/, 거래/) are
// skipped, mirroring projectCandidatesFn. Idempotent by mail id; best-effort
// (a failure logs, never fails the analysis).
func (s *Server) appendMailStatusToProjects(msg *gmail.MessageDetail, res gmailpoll.AnalysisResult) {
	if s.wikiStore == nil || msg == nil {
		return
	}
	line := mailStatusLine(msg, res)
	if line == "" {
		return
	}
	ref := "mail:" + msg.ID
	now := time.Now()
	for _, r := range directProjectPages(res.RelatedProjects) {
		if err := s.wikiStore.AppendProjectStatusLine(r, line, ref, now); err != nil {
			s.logger.Warn("mail→project 현재 상태 갱신 실패", "id", msg.ID, "path", r, "error", err)
		}
	}
}

// mailStatusLine renders the one-line status entry for a project from an analyzed
// mail: the deal title when it's a recognized business document ("견적서 · 탑솔라
// 수신"), else the sender + subject. Empty when there's nothing to say.
func mailStatusLine(msg *gmail.MessageDetail, res gmailpoll.AnalysisResult) string {
	if d := res.Deal; d != nil {
		if t := strings.TrimSpace(dealEvidenceTitle(d)); t != "" && t != "거래 문서" {
			return t + " 수신"
		}
	}
	subj := strings.TrimSpace(msg.Subject)
	if subj == "" {
		return ""
	}
	if sender := strings.TrimSpace(senderShortLabel(msg.From)); sender != "" {
		return sender + ": " + clipRunes(subj, 60)
	}
	return clipRunes(subj, 60)
}
