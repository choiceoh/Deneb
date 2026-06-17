// mail_todo.go — turns the high-priority follow-up actions of a mail analysis
// into to-dos. Wired from the OnAnalyzed sink (wiki_mail_analysis.go).
//
// Non-invasive by design ("능동적이되 침해적이지 않게"): only "high" priority
// actions auto-create a to-do — medium/low stay as analysis context — and each
// to-do is deduped by a stable per-mail Source key so re-analysis of the same
// message never piles up duplicates. Every failure is logged, never fatal to
// the sink.
package server

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localtodo"
)

// mailTodoSourcePrefix marks a to-do as auto-created from a mail analysis.
const mailTodoSourcePrefix = "mail:"

// autoCreateTodosFromMail creates to-dos for the high-priority follow-up
// actions of a mail analysis. Best-effort: a missing store or a per-item
// failure is logged and skipped, never disrupting the analysis sink.
func (s *Server) autoCreateTodosFromMail(msg *gmail.MessageDetail, items []gmailpoll.ActionItem) int {
	if msg == nil {
		return 0
	}
	inputs := todosFromActionItems(msg.ID, msg.Subject, msg.From, items, time.Now())
	if len(inputs) == 0 {
		return 0
	}
	store, err := localtodo.Default()
	if err != nil {
		s.logger.Warn("mail→todo: store unavailable", "id", msg.ID, "error", err)
		return len(inputs)
	}
	created := 0
	for _, in := range inputs {
		td, wasCreated, cerr := store.CreateIfAbsent(in)
		if cerr != nil {
			s.logger.Warn("mail→todo: create failed", "id", msg.ID, "title", in.Title, "error", cerr)
			continue
		}
		if wasCreated {
			created++
			s.logger.Info("mail→todo: created to-do", "todoId", td.ID, "title", td.Title)
		}
	}
	if created > 0 {
		s.logger.Info("mail→todo: auto-created to-dos from analysis", "id", msg.ID, "count", created)
	}
	return len(inputs)
}

// todosFromActionItems is the pure decision: keep only high-priority actions
// and build a localtodo.CreateInput for each, with a back-reference Note and a
// stable Source dedup key. Returns nil when nothing qualifies. now is injected
// so due-date resolution is deterministic in tests.
func todosFromActionItems(msgID, subject, from string, items []gmailpoll.ActionItem, now time.Time) []localtodo.CreateInput {
	var out []localtodo.CreateInput
	for _, a := range items {
		if !strings.EqualFold(strings.TrimSpace(a.Priority), "high") {
			continue // only high-priority actions auto-create to-dos
		}
		title := strings.TrimSpace(a.Title)
		if title == "" {
			continue
		}
		due, allDay := parseDueHint(a.DueHint, now)
		out = append(out, localtodo.CreateInput{
			Title:     title,
			Note:      mailTodoNote(subject, from),
			Due:       due,
			DueAllDay: allDay,
			Source:    mailTodoSource(msgID, title),
		})
	}
	return out
}

// mailTodoNote builds the to-do note linking back to the originating mail.
func mailTodoNote(subject, from string) string {
	subject = strings.TrimSpace(subject)
	from = strings.TrimSpace(from)
	switch {
	case subject != "" && from != "":
		return fmt.Sprintf("메일: %s · %s", subject, from)
	case subject != "":
		return "메일: " + subject
	case from != "":
		return "메일: " + from
	default:
		return "메일 분석에서 자동 생성"
	}
}

// mailTodoSource is the dedup key: message id + normalized title, so the same
// action from the same mail can't be created twice regardless of order.
func mailTodoSource(msgID, title string) string {
	return mailTodoSourcePrefix + strings.TrimSpace(msgID) + "|" + strings.ToLower(strings.TrimSpace(title))
}

// --- due-hint resolution -------------------------------------------------

var relativeDaysRe = regexp.MustCompile(`(\d+)\s*([일주])\s*(?:후|뒤|내|이내)`)

var (
	isoDateRe  = regexp.MustCompile(`(\d{4})[-/.](\d{1,2})[-/.](\d{1,2})`)
	monthDayRe = regexp.MustCompile(`(\d{1,2})\s*월\s*(\d{1,2})\s*일`)
)

// parseDueHint resolves a free-text Korean/relative due cue to a date. Returns
// (zero, false) when the cue is empty or unrecognized — automation never
// invents a deadline it wasn't given. allDay is always true for the cues we
// parse: a to-do carries a date, not a wall-clock time.
func parseDueHint(hint string, now time.Time) (due time.Time, allDay bool) {
	h := strings.TrimSpace(hint)
	if h == "" {
		return time.Time{}, false
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	// Explicit dates first so "6월 15일" / "2026-06-15" aren't mis-read as a
	// relative "15일" offset.
	if t, ok := parseExplicitDate(h, today); ok {
		return t, true
	}

	switch {
	case strings.Contains(h, "오늘"):
		return today, true
	case strings.Contains(h, "내일"):
		return today.AddDate(0, 0, 1), true
	case strings.Contains(h, "모레"):
		return today.AddDate(0, 0, 2), true
	case strings.Contains(h, "다음 주"), strings.Contains(h, "다음주"), strings.Contains(h, "차주"):
		return today.AddDate(0, 0, 7), true
	case strings.Contains(h, "이번 주"), strings.Contains(h, "이번주"), strings.Contains(h, "금주"):
		return endOfWeek(today), true
	}

	if d, ok := parseRelativeDays(h); ok {
		return today.AddDate(0, 0, d), true
	}
	return time.Time{}, false
}

// parseRelativeDays handles "3일 후", "2주 뒤", "5일 이내" → day offset. The
// trailing 후/뒤/내/이내 is required so a bare "15일" inside a date isn't taken
// as an offset.
func parseRelativeDays(h string) (int, bool) {
	m := relativeDaysRe.FindStringSubmatch(h)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0, false
	}
	if m[2] == "주" {
		n *= 7
	}
	return n, true
}

// parseExplicitDate handles ISO (2026-06-15) and "6월 15일". A month/day with no
// year resolves to this year, rolling to next year if already past, so a new
// action never gets a due date in the past.
func parseExplicitDate(h string, today time.Time) (time.Time, bool) {
	if m := isoDateRe.FindStringSubmatch(h); m != nil {
		return ymd(atoiSafe(m[1]), atoiSafe(m[2]), atoiSafe(m[3]), today.Location())
	}
	if m := monthDayRe.FindStringSubmatch(h); m != nil {
		month, day := atoiSafe(m[1]), atoiSafe(m[2])
		t, ok := ymd(today.Year(), month, day, today.Location())
		if ok && t.Before(today) {
			t = t.AddDate(1, 0, 0)
		}
		return t, ok
	}
	return time.Time{}, false
}

// endOfWeek returns the Friday of today's week (KST work week), or today itself
// when it's already Friday or the weekend.
func endOfWeek(today time.Time) time.Time {
	offset := (int(time.Friday) - int(today.Weekday()) + 7) % 7
	return today.AddDate(0, 0, offset)
}

// ymd validates the month/day range loosely and returns a date-only time.
func ymd(y, m, d int, loc *time.Location) (time.Time, bool) {
	if m < 1 || m > 12 || d < 1 || d > 31 {
		return time.Time{}, false
	}
	return time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc), true
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
