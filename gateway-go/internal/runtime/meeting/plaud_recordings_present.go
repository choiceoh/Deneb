package meeting

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// meetingFilename builds the wiki filename: a Korean-safe slug of the name
// plus an id prefix so retitled recordings stay idempotent by id.
func meetingFilename(f plaudFile) string {
	slug := MeetingSlug(f.Name)
	id := f.ID
	if len(id) > 8 {
		id = id[:8]
	}
	if slug == "" {
		return "회의-" + id + ".md"
	}
	return slug + "-" + id + ".md"
}

// MeetingSlug keeps unicode letters/digits joined by single hyphens, bounded.
//
// Exported because the mail-arrival path needs the SAME slug to recognize that
// a Plaud AutoFlow notice describes a meeting this service already wrote a page
// for (wiki_mail_autoflow.go). Two copies of this rule would drift apart and the
// duplicate-suppression would go quiet without failing anything.
func MeetingSlug(name string) string {
	var b strings.Builder
	lastHyphen := true
	runes := 0
	for _, r := range strings.TrimSpace(name) {
		if runes >= 48 {
			break
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
			runes++
			continue
		}
		if !lastHyphen {
			b.WriteRune('-')
			lastHyphen = true
			runes++
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildMeetingPage(f plaudFile, report string, related []string, transcript string, now time.Time, loc *time.Location) *wiki.Page {
	title := strings.TrimSpace(f.Name)
	if title == "" {
		title = "회의 " + f.ID
	}
	today := now.UTC().Format("2006-01-02")

	var body strings.Builder
	fmt.Fprintf(&body, "> 녹음: %s (KST) · %d분 · Plaud `%s`\n\n",
		f.StartAt.In(loc).Format("2006-01-02 15:04"), int(f.Duration.Minutes()), f.ID)
	body.WriteString(report)
	// A bounded transcript tail keeps the page greppable/quotable without
	// storing a 2-hour transcript in the wiki (the recorder holds the full
	// text; the agent can re-pull via plaud_get_transcript).
	body.WriteString("\n\n## 전사 발췌\n\n```\n")
	body.WriteString(textutil.TruncateRunes(transcript, 4000, ""))
	body.WriteString("\n```\n")

	return &wiki.Page{
		Meta: wiki.Frontmatter{
			Title:      title,
			Summary:    "회의 녹음 분석 (Plaud)",
			Category:   meetingWikiCategory,
			Tags:       []string{"회의록"},
			Related:    related,
			Created:    today,
			Updated:    today,
			Type:       "log",
			Confidence: "medium",
			Importance: 0.4,
		},
		Body: body.String(),
	}
}

// meetingStatusLine is the dated bullet appended to linked project rep pages.
func meetingStatusLine(f plaudFile, loc *time.Location) string {
	return fmt.Sprintf("회의(녹음): %s (%s, %d분) — 회의록 페이지 참조",
		strings.TrimSpace(f.Name), f.StartAt.In(loc).Format("01-02 15:04"), int(f.Duration.Minutes()))
}

// meetingCardBody is the work-feed card text: the report minus the transcript
// (the card is a read-in-feed artifact; the page carries the excerpt).
func meetingCardBody(f plaudFile, report string, pagePath string, loc *time.Location) string {
	var b strings.Builder
	fmt.Fprintf(&b, "🎙 회의 분석: %s\n%s (KST) · %d분\n\n",
		strings.TrimSpace(f.Name), f.StartAt.In(loc).Format("2006-01-02 15:04"), int(f.Duration.Minutes()))
	b.WriteString(report)
	fmt.Fprintf(&b, "\n\n회의록: %s", pagePath)
	return b.String()
}
