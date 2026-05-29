// wiki_mail_analysis.go — adapter between miniapp.gmail.analyze and the
// wiki store. Lifted out of method_registry.go so the wiring there
// stays a single line and the page-shaping logic has room to breathe.
//
// One page per message ID. We never rewrite an existing page from this
// path (the analysis cache short-circuits before reaching the wiki),
// so the frontmatter is set once at creation and left alone.

package server

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

// wikiProjectCategory is the wiki category whose pages are offered to the
// email analyzer as related-project candidates.
const wikiProjectCategory = "프로젝트"

// mailAnalysisWikiPath maps a Gmail message ID to its wiki page path.
func mailAnalysisWikiPath(msgID string) string {
	return "mail-analyses/" + msgID + ".md"
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
			Category:   "mail-analysis",
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
		paths, err := store.ListPages(wikiProjectCategory)
		if err != nil {
			return nil
		}
		cands := make([]gmailpoll.ProjectCandidate, 0, len(paths))
		for _, p := range paths {
			c := gmailpoll.ProjectCandidate{Path: p}
			if page, err := store.ReadPage(p); err == nil && page != nil {
				c.Title = page.Meta.Title
				c.Summary = page.Meta.Summary
			}
			cands = append(cands, c)
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
func (s *Server) makeMailAnalysisSink() func(*gmail.MessageDetail, gmailpoll.AnalysisResult) {
	cacheStore := handlerminiapp.NewAnalysisStore(filepath.Join(s.denebDir, "cache", "mail_analysis"))
	return func(msg *gmail.MessageDetail, res gmailpoll.AnalysisResult) {
		if msg == nil {
			return
		}
		if err := cacheStore.SaveAnalysis(handlerminiapp.CachedAnalysis{
			MsgID:           msg.ID,
			Subject:         msg.Subject,
			From:            msg.From,
			Date:            msg.Date,
			Analysis:        res.Text,
			RelatedProjects: res.RelatedProjects,
			CreatedAt:       time.Now().UTC(),
		}); err != nil {
			s.logger.Warn("mail analysis cache 저장 실패", "id", msg.ID, "error", err)
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
			if err := s.wikiStore.WritePage(mailAnalysisWikiPath(msg.ID), page); err != nil {
				s.logger.Warn("mail analysis 위키 저장 실패", "id", msg.ID, "error", err)
			}
		}
	}
}
