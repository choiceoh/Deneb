// mail_calendar.go — turns the schedule-worthy items of a mail analysis into
// calendar PROPOSALS (pending the operator's accept). Wired from the OnAnalyzed
// sink alongside the to-do creation.
//
// Non-invasive by design ("능동적이되 침해적이지 않게"): nothing is auto-added to
// the calendar — proposals only surface as a bell badge. Conservative on what
// qualifies (real meetings / important deadlines), to avoid a noisy bell:
//   - an action item is proposed only when its due hint resolves to a concrete
//     date AND it is high priority. (parseDueHint resolves to all-day dates
//     today; if it ever yields a specific time — a meeting — that qualifies too,
//     hence the allDay guard below.)
//   - a deal document's due date (납기·결제 기한) is proposed as a deadline.
//
// Each proposal is deduped by a stable per-mail Source key, so re-analysis of
// the same message never piles up duplicates.
package server

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// autoProposeCalendarFromMail creates calendar proposals from a mail analysis.
// Best-effort: a missing store or a per-item failure is logged and skipped.
func (s *Server) autoProposeCalendarFromMail(msg *gmail.MessageDetail, items []gmailpoll.ActionItem, deal *gmailpoll.DealInfo) int {
	if msg == nil {
		return 0
	}
	inputs := calendarProposalsFromMail(msg.ID, msg.Subject, msg.From, items, deal, time.Now())
	if len(inputs) == 0 {
		return 0
	}
	store, err := calprop.Default()
	if err != nil {
		s.logger.Warn("mail→calendar: proposal store unavailable", "id", msg.ID, "error", err)
		return len(inputs)
	}
	created := 0
	for _, in := range inputs {
		_, was, cerr := store.CreateIfAbsent(in)
		if cerr != nil {
			s.logger.Warn("mail→calendar: propose failed", "id", msg.ID, "title", in.Title, "error", cerr)
			continue
		}
		if was {
			created++
		}
	}
	if created > 0 {
		s.logger.Info("mail→calendar: proposed calendar events from analysis", "id", msg.ID, "count", created)
	}
	return len(inputs)
}

// calendarProposalsFromMail is the pure decision: pick the schedule-worthy items
// and build a calprop.CreateInput for each. Exposed for unit testing.
func calendarProposalsFromMail(msgID, subject, from string, items []gmailpoll.ActionItem, deal *gmailpoll.DealInfo, now time.Time) []calprop.CreateInput {
	var out []calprop.CreateInput

	for _, it := range items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		due, allDay := parseDueHint(it.DueHint, now)
		if due.IsZero() {
			continue // no concrete date ⇒ not calendar-worthy
		}
		// Promote to a timed event when the hint carries a clock time ("14시",
		// "오후 2시", "14:00") — a real meeting should land at its time, not be an
		// all-day blob. A timed item is calendar-worthy regardless of priority.
		if h, m, ok := parseTimeOfDay(it.DueHint); ok {
			due = time.Date(due.Year(), due.Month(), due.Day(), h, m, 0, 0, due.Location())
			allDay = false
		}
		// Conservative for the remaining all-day items: keep only high priority
		// (a dated-but-untimed follow-up). Timed meetings already passed above.
		if allDay && !strings.EqualFold(it.Priority, "high") {
			continue
		}
		out = append(out, calprop.CreateInput{
			Title:         title,
			Start:         formatProposalStart(due, allDay),
			AllDay:        allDay,
			Kind:          "meeting",
			Source:        "mail:" + msgID + "|" + title,
			SourceSubject: subject,
			SourceFrom:    from,
		})
	}

	if deal != nil {
		if d, ok := parseISODate(strings.TrimSpace(deal.DueDate)); ok {
			out = append(out, calprop.CreateInput{
				Title:         dealDeadlineTitle(deal),
				Start:         d.Format("2006-01-02"),
				AllDay:        true,
				Kind:          "deadline",
				Source:        "mail:" + msgID + "|deal-due",
				SourceSubject: subject,
				SourceFrom:    from,
			})
		}
	}
	return out
}

var (
	timeColonRe = regexp.MustCompile(`(\d{1,2}):(\d{2})`)
	timeKoRe    = regexp.MustCompile(`(오전|오후)?\s*(\d{1,2})\s*시(?:\s*(\d{1,2})\s*분)?`)
)

// parseTimeOfDay extracts a clock time from a Korean due hint: "14:00",
// "오후 2시", "오전 9시 30분", "14시". 오전/오후 (AM/PM) adjust a 12-hour value.
// ok is false when the hint carries no time (the event stays all-day). A
// "N시간" duration is excluded so "2시간 후" is not misread as 2:00.
func parseTimeOfDay(hint string) (hour, minute int, ok bool) {
	pm := strings.Contains(hint, "오후")
	am := strings.Contains(hint, "오전")
	apply := func(h int) int {
		if pm && h < 12 {
			return h + 12
		}
		if am && h == 12 {
			return 0
		}
		return h
	}
	if m := timeColonRe.FindStringSubmatch(hint); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		if h = apply(h); h < 24 && mi < 60 {
			return h, mi, true
		}
	}
	if !strings.Contains(hint, "시간") {
		if m := timeKoRe.FindStringSubmatch(hint); m != nil {
			h, _ := strconv.Atoi(m[2])
			mi := 0
			if m[3] != "" {
				mi, _ = strconv.Atoi(m[3])
			}
			if h = apply(h); h < 24 && mi < 60 {
				return h, mi, true
			}
		}
	}
	return 0, 0, false
}

// formatProposalStart renders a resolved due time as the proposal Start string:
// RFC3339 for a timed event, "2006-01-02" for an all-day one.
func formatProposalStart(t time.Time, allDay bool) string {
	if allDay {
		return t.Format("2006-01-02")
	}
	return t.Format(time.RFC3339)
}

// parseISODate parses a "YYYY-MM-DD" date (the deal extractor's dueDate format),
// in the local location. ok is false for empty/unparseable input.
func parseISODate(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// dealDeadlineTitle builds a human title for a deal's due date, e.g.
// "탑솔라 세금계산서 결제 기한".
func dealDeadlineTitle(deal *gmailpoll.DealInfo) string {
	parts := make([]string, 0, 3)
	if cp := strings.TrimSpace(deal.Counterparty); cp != "" {
		parts = append(parts, cp)
	}
	if dt := strings.TrimSpace(deal.DocType); dt != "" {
		parts = append(parts, dt)
	}
	parts = append(parts, "결제 기한")
	return strings.Join(parts, " ")
}
