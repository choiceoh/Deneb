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
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calprop"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmailpoll"
)

// autoProposeCalendarFromMail creates calendar proposals from a mail analysis.
// Best-effort: a missing store or a per-item failure is logged and skipped.
func (s *Server) autoProposeCalendarFromMail(msg *gmail.MessageDetail, items []gmailpoll.ActionItem, deal *gmailpoll.DealInfo) {
	if msg == nil {
		return
	}
	inputs := calendarProposalsFromMail(msg.ID, msg.Subject, msg.From, items, deal, time.Now())
	if len(inputs) == 0 {
		return
	}
	store, err := calprop.Default()
	if err != nil {
		s.logger.Warn("mail→calendar: proposal store unavailable", "id", msg.ID, "error", err)
		return
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
		// Conservative: keep real meetings (timed) and important deadlines (high
		// priority). Skip low-signal all-day, non-high items.
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
