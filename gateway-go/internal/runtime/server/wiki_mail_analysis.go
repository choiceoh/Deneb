// wiki_mail_analysis.go — adapter between miniapp.gmail.analyze and the
// wiki store. Lifted out of method_registry.go so the wiring there
// stays a single line and the page-shaping logic has room to breathe.
//
// One page per message ID. We never rewrite an existing page from this
// path (the analysis cache short-circuits before reaching the wiki),
// so the frontmatter is set once at creation and left alone.

package server

import (
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

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
