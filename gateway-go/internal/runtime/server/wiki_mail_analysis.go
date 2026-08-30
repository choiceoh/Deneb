// wiki_mail_analysis.go — adapter between miniapp.mail.analyze and the
// wiki store. Lifted out of method_registry.go so the wiring there
// stays a single line and the page-shaping logic has room to breathe.
//
// One page per message ID. We never rewrite an existing page from this
// path (the analysis cache short-circuits before reaching the wiki),
// so the frontmatter is set once at creation and left alone.

package server

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailanalysis"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailwork"
	handlermail "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/mail"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
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
func buildMailAnalysisPage(in handlermail.WikiAnalysisInput) *wiki.Page {
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
	body.WriteString(mailMetadataLine(in.From))
	body.WriteString("\n> Date: ")
	body.WriteString(mailMetadataLine(in.Date))
	body.WriteString("\n> Message ID: `")
	body.WriteString(mailMetadataLine(in.MsgID))
	body.WriteString("`\n> Source: `")
	body.WriteString(mailSourceLocator(in.MsgID))
	body.WriteString("`")
	if link := gmailThreadLink(in.ThreadID); link != "" {
		body.WriteString(" ([Gmail 원문](")
		body.WriteString(link)
		body.WriteString("))")
	}
	if threadID := mailMetadataLine(in.ThreadID); threadID != "" {
		body.WriteString("\n> Thread ID: `")
		body.WriteString(threadID)
		body.WriteString("`")
	}
	if messageID := mailMetadataLine(in.MessageIDHeader); messageID != "" {
		body.WriteString("\n> RFC Message-ID: `")
		body.WriteString(messageID)
		body.WriteString("`")
	}
	body.WriteString("\n\n")
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
			Resource:   mailSourceLocator(in.MsgID),
		},
		Body: body.String(),
	}
}

func mailSourceLocator(msgID string) string {
	return "mail:" + mailMetadataLine(msgID)
}

func gmailThreadLink(threadID string) string {
	threadID = mailMetadataLine(threadID)
	if threadID == "" {
		return ""
	}
	u := url.URL{
		Scheme:   "https",
		Host:     "mail.google.com",
		Path:     "/mail/u/0/",
		Fragment: "all/" + threadID,
	}
	return u.String()
}

// Header values are untrusted. Keep each provenance field on exactly one line
// so a forged folded header cannot inject wiki markdown or metadata rows.
func mailMetadataLine(value string) string {
	return strings.Join(strings.Fields(value), " ")
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
func (s *Server) projectCandidatesFn() func() []mailanalysis.ProjectCandidate {
	return func() []mailanalysis.ProjectCandidate {
		store := s.wikiStore
		if store == nil {
			return nil
		}
		// Only project 대표페이지 are related-project candidates — the analyzer must
		// not cite an auto-generated mail dump, deal ledger page, or sub-page as a
		// "related project". KnownProjects owns that layout rule.
		refs := store.KnownProjects()
		cands := make([]mailanalysis.ProjectCandidate, 0, len(refs))
		for _, r := range refs {
			cands = append(cands, mailanalysis.ProjectCandidate{
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
func (s *Server) makeMailAnalysisSink() func(*gmail.MessageDetail, mailanalysis.AnalysisResult) error {
	cacheStore := handlermail.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis"))
	workStore := s.mailWorkStatePathStore()
	return func(msg *gmail.MessageDetail, res mailanalysis.AnalysisResult) error {
		if msg == nil {
			return nil
		}
		var errs []error
		if err := cacheStore.SaveAnalysis(handlermail.CachedAnalysis{
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
		// A degraded synthesis (prefatory narration, a bare heading, or the
		// delivery layer's own timeout string) must not become a wiki page: it
		// would be indistinguishable from an analysis in recall. Skip the write
		// and mark the message failed so a later pass re-analyzes it — an
		// unanalyzed mail is recoverable, a poisoned page is not.
		if err := mailanalysis.AnalysisUsable(res.Text); err != nil {
			s.logger.Warn("mail analysis 위키 저장 거부 — 사용 불가한 본문", "id", msg.ID, "reason", err)
			_, _ = workStore.MarkAnalysisFailed(mailwork.MessageInput{
				ID: msg.ID, Subject: msg.Subject, From: msg.From, Date: msg.Date,
			}, err)
			return errors.Join(errs...)
		}
		// A correct analysis of mail that is not business mail still costs a page
		// that ranks like any other. Skip the write — but do NOT mark the message
		// failed the way an unusable body is: this analysis succeeded, and queuing
		// it for re-analysis would retry it forever. The result still reaches the
		// inbox card and the notifier; only the wiki page is withheld.
		if err := mailanalysis.AnalysisNonBusiness(res.Text); err != nil {
			s.logger.Info("mail analysis 위키 저장 생략 — 업무 메일 아님", "id", msg.ID, "reason", err)
			return errors.Join(errs...)
		}
		// Plaud's AutoFlow notice describes a meeting the MCP path may have
		// already written a 회의록 page AND a project status bullet for. Those two
		// writes — and only those two — are the ones that would duplicate; the
		// deal ledger, calendar proposals, card, and workflow state below are the
		// mail path's own and still run. The message is NOT queued for
		// re-analysis: like AnalysisNonBusiness, this analysis succeeded.
		meetingCovered := ""
		if s.wikiStore != nil {
			if meetingCovered = autoFlowMeetingCovered(s.wikiStore, msg.From, msg.Subject); meetingCovered != "" {
				s.logger.Info("mail analysis 위키 저장 생략 — 회의록이 이미 덮음",
					"id", msg.ID, "subject", msg.Subject, "meeting", meetingCovered)
			}
		}
		if s.wikiStore != nil && meetingCovered == "" {
			page := buildMailAnalysisPage(handlermail.WikiAnalysisInput{
				MsgID:           msg.ID,
				ThreadID:        msg.ThreadID,
				MessageIDHeader: msg.MessageIDHeader,
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
	workStore := s.mailWorkStatePathStore()
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
	workStore := s.mailWorkStatePathStore()
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

func (s *Server) makeMailSenderReviewSink() func(*gmail.MessageDetail, mailanalysis.SenderTrustDecision) error {
	workStore := s.mailWorkStatePathStore()
	return func(msg *gmail.MessageDetail, decision mailanalysis.SenderTrustDecision) error {
		if msg == nil || strings.TrimSpace(msg.ID) == "" {
			return nil
		}
		_, err := workStore.MarkAnalysisReview(mailwork.MessageInput{
			ID:       msg.ID,
			ThreadID: msg.ThreadID,
			From:     msg.From,
			Subject:  msg.Subject,
			Date:     msg.Date,
		}, decision.Reason)
		if err != nil {
			s.logger.Warn("mail workflow 검토 상태 저장 실패", "id", msg.ID, "error", err)
		}
		return err
	}
}

// fileDealFromMail files a structured business-document extraction onto its
// counterparty's 거래 wiki page. Silent and best-effort: no push, deduped by
// the mail id, failures logged only. nil deal (non-deal mail) is a no-op.
func (s *Server) fileDealFromMail(msg *gmail.MessageDetail, deal *mailanalysis.DealInfo, relatedProjects []string) {
	if deal == nil || msg == nil || s.wikiStore == nil {
		return
	}
	now := time.Now()
	input := wiki.DealPageInput{
		Counterparty:    deal.Counterparty,
		DocType:         deal.DocType,
		Amount:          deal.Amount,
		Date:            deal.Date,
		DueDate:         deal.DueDate,
		Items:           deal.Items,
		Summary:         deal.Summary,
		SourceRef:       "mail:" + msg.ID,
		RelatedProjects: directProjectPages(relatedProjects), // deal→project graph edge
		Terms:           dealTermsFromFacts(deal.Facts),      // quote-verified → ledger
	}
	// Requote diff must read the ledger BEFORE the upsert tees the new record
	// (otherwise the new row is its own "previous"). Deterministic, read-only.
	requote := s.wikiStore.DetectRequote(input, now)
	relPath, created, err := s.wikiStore.UpsertDealPage(input, now)
	if err != nil {
		s.logger.Warn("mail→deal: 거래 페이지 저장 실패", "id", msg.ID, "counterparty", deal.Counterparty, "error", err)
		return
	}
	s.logger.Info("mail→deal: 거래 페이지 갱신", "id", msg.ID, "path", relPath, "created", created)

	// Surface a detected price/volume change on the linked projects' 현재 상태
	// — the operator's glance surface. Idempotent by mail id; best-effort.
	if requote != nil {
		line := requote.StatusLine()
		s.logger.Info("mail→deal: 재견적 변동 감지", "id", msg.ID, "counterparty", deal.Counterparty, "detail", line)
		for _, p := range input.RelatedProjects {
			if aerr := s.wikiStore.AppendProjectStatusLine(p, line, "requote:mail:"+msg.ID, now); aerr != nil {
				s.logger.Warn("mail→deal: 재견적 상태줄 기록 실패", "id", msg.ID, "project", p, "error", aerr)
			}
		}
	}

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
func (s *Server) pinDealEvidenceToNotebook(msg *gmail.MessageDetail, deal *mailanalysis.DealInfo, dealRef string, relatedProjects []string) {
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

	s.pinDealAttachmentsToNotebook(msg, deal, dealRef)

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

// notebookAttachmentTextCap bounds one attachment's extracted text inside a
// notebook. Grounding stuffs the whole notebook into a wire tail, so a single
// 200-row 견적서 must not crowd out the rest of the deal.
const notebookAttachmentTextCap = 8000

// pinDealAttachmentsToNotebook pins the deal mail's ARCHIVED attachments — the
// actual 견적서/계약서 — onto the same deal notebook, extracted to text.
//
// Until now a deal notebook held only the analyzer's ~270-char extraction of the
// mail. Measured 2026-08-30: 74 notebooks, 103 sources, every one of them
// kind=note, while the mail archive held 41 xlsx / 46 docx / 202 pdf of the
// actual quotes and contracts. 76% of the notebooks had exactly one source — a
// binder with one summary in it, which is strictly worse than reading the mail.
// The numbers the operator asks about (단가, 수량, 납기 조건) live in the
// attachment, not in the summary of it.
//
// Best-effort and silent, like the note pin: the attachment must already be in
// the local archive (archiveAttachments put it there earlier in the same cycle),
// so this reads from disk and never fetches. Deduped per file by Ref, so
// re-analysis of the same mail never double-pins.
func (s *Server) pinDealAttachmentsToNotebook(msg *gmail.MessageDetail, deal *mailanalysis.DealInfo, dealRef string) {
	if s.notebookStore == nil || dealRef == "" || len(msg.Attachments) == 0 {
		return
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for _, att := range msg.Attachments {
		name := strings.TrimSpace(att.Filename)
		if name == "" {
			continue
		}
		path, ok := findArchivedAttachment(ctx, store, name)
		if !ok {
			continue
		}
		data, _, rerr := store.Get(ctx, path)
		if rerr != nil {
			continue
		}
		text := strings.TrimSpace(document.ExtractAttachmentText(ctx, data, name, att.MimeType))
		if text == "" {
			continue
		}
		if len(text) > notebookAttachmentTextCap {
			text = text[:notebookAttachmentTextCap] + "\n…(잘림 — 원본: " + path + ")"
		}
		added, perr := s.notebookStore.PinUnique(dealRef, deal.Counterparty, notebook.Source{
			Kind:  notebook.KindFile,
			Ref:   "mail:" + msg.ID + "#att:" + name,
			Title: name,
			Text:  text,
		})
		if perr != nil {
			s.logger.Warn("mail→notebook: 첨부 핀 실패", "id", msg.ID, "file", name, "error", perr)
			continue
		}
		if added {
			s.logger.Info("mail→notebook: 첨부 핀", "id", msg.ID, "deal", dealRef, "file", name, "chars", len(text))
		}
	}
}

// findArchivedAttachment locates an archived attachment by filename under the
// mail archive's dated folders.
//
// archiveAttachments writes "<ArchiveFolder>/<today>/<sender>_<filename>" with
// both components sanitized, and it runs earlier in the same analysis cycle —
// but reconstructing that exact name here would couple this to another package's
// sanitizer and break on a midnight rollover. A bounded suffix match over the
// two most recent day folders is the same answer without the coupling.
func findArchivedAttachment(ctx context.Context, store *filestore.LocalStore, filename string) (string, bool) {
	days, err := store.List(ctx, mailArchiveFolder, false, 0)
	if err != nil {
		return "", false
	}
	folders := make([]string, 0, len(days))
	for _, d := range days {
		if d.Tag == "folder" {
			folders = append(folders, d.PathDisplay)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(folders)))
	if len(folders) > 2 {
		folders = folders[:2]
	}
	want := "_" + filename
	for _, folder := range folders {
		entries, lerr := store.List(ctx, folder, false, 0)
		if lerr != nil {
			continue
		}
		for _, e := range entries {
			if e.Tag == "file" && strings.HasSuffix(e.Name, want) {
				return e.PathDisplay, true
			}
		}
	}
	return "", false
}

// mailArchiveFolder mirrors mailanalysis' default ArchiveFolder. Kept as a
// constant rather than imported so this read path does not depend on the
// analyzer's config surface.
const mailArchiveFolder = "/Deneb-Archive/메일"

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
		// Normalize backslashes universally (filepath.ToSlash is a no-op off
		// Windows) so separator variants dedupe and match as one path.
		r = strings.ReplaceAll(strings.TrimSpace(r), "\\", "/")
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

// dealTermsFromFacts maps the pipeline's quote-verified extraction onto the
// wiki ledger's term type (domain must not import platform, so the copy lives
// here). nil in, or nothing surviving, → nil out.
func dealTermsFromFacts(f *mailanalysis.DealFacts) *wiki.DealTerms {
	if f == nil {
		return nil
	}
	q := func(v mailanalysis.QuotedFact) wiki.QuotedTerm {
		return wiki.QuotedTerm{Value: v.Value, Quote: v.Quote}
	}
	t := &wiki.DealTerms{
		Capacity:     q(f.CapacityMW),
		UnitPrice:    q(f.UnitPrice),
		Payment:      q(f.PaymentTerms),
		Warranty:     q(f.Warranty),
		DelayPenalty: q(f.DelayPenalty),
	}
	if t.Empty() {
		return nil
	}
	return t
}

// dealEvidenceTitle is the human label for a pinned deal source ("견적서 · 탑솔라").
func dealEvidenceTitle(deal *mailanalysis.DealInfo) string {
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
func dealEvidenceText(deal *mailanalysis.DealInfo, msg *gmail.MessageDetail) string {
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
	// Quote-verified commercial terms (사실 레이어 2단계) — only fields that
	// passed the verbatim-quote gate exist here, so they are safe to cite.
	if f := deal.Facts; f != nil {
		writeField("물량", f.CapacityMW.Value)
		writeField("단가", f.UnitPrice.Value)
		writeField("지급조건", f.PaymentTerms.Value)
		writeField("하자보수", f.Warranty.Value)
		writeField("지체상금", f.DelayPenalty.Value)
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
func (s *Server) appendMailStatusToProjects(msg *gmail.MessageDetail, res mailanalysis.AnalysisResult) {
	if s.wikiStore == nil || msg == nil {
		return
	}
	line := mailStatusLine(msg, res)
	if line == "" {
		return
	}
	// Signal tag ("[결정·승인]", "[리스크]" …) from the mail's primary status signal.
	if tag := strings.TrimSpace(res.StatusTag); tag != "" {
		line = line + " " + tag
	}
	// Event date: a deal document carries its own date (견적서 일자 등), which is
	// "when it happened" — distinct from now, when Deneb processed the mail. Pass it
	// so the bullet leads with the document date instead of the processing day.
	eventDate := ""
	if d := res.Deal; d != nil {
		eventDate = d.Date
	}
	ref := "mail:" + msg.ID
	now := time.Now()
	for _, r := range directProjectPages(res.RelatedProjects) {
		if err := s.wikiStore.AppendProjectStatusLineAt(r, line, eventDate, ref, now); err != nil {
			s.logger.Warn("mail→project 현재 상태 갱신 실패", "id", msg.ID, "path", r, "error", err)
		}
	}
}

// mailStatusLine renders the one-line status entry for a project from an analyzed
// mail: the deal title when it's a recognized business document ("견적서 · 탑솔라
// 수신"), else the sender + subject. Empty when there's nothing to say.
func mailStatusLine(msg *gmail.MessageDetail, res mailanalysis.AnalysisResult) string {
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
		return sender + ": " + textutil.TruncateRunes(subj, 60, "...")
	}
	return textutil.TruncateRunes(subj, 60, "...")
}
