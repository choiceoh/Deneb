// date_anchor.go builds the deterministic date-anchor block injected into
// mail-analysis prompts: the mail's send date, its weekday, and an explicit
// relative-date conversion table (this week / next week / the week after,
// Monday-start, KST) so "다음 주 금요일까지" resolves to one exact date by
// lookup instead of model arithmetic. No LLM involved (결정적 포맷 도그마) —
// the party-anchor sibling for time.
//
// Why: relative-date arithmetic is a measured model weakness. The mail-bench
// date trap ("발송일 목요일, 다음 주 금요일까지 회신") was answered wrong by
// dsv4 in both prompt formats (2026-07-04, off-by-one day with a mislabeled
// weekday) and only variably right by others; a supplied conversion table
// turns the calculation into copying. Real production stake: reply deadlines
// and 기성/견적 마감 dates flow straight into the analysis card the operator
// acts on.
package gmailpoll

import (
	"fmt"
	"net/mail"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

var koreanWeekdays = [7]string{"일", "월", "화", "수", "목", "금", "토"}

// anchorTimezone pins the conversion table to operator local time. Mail Date
// headers carry their own offsets; rendering in one zone keeps the table
// coherent regardless of sender timezone.
var anchorTimezone = func() *time.Location {
	if loc, err := time.LoadLocation("Asia/Seoul"); err == nil {
		return loc
	}
	return time.FixedZone("KST", 9*3600)
}()

// parseMailDate parses an RFC 5322 Date header value, tolerating the common
// Korean-mail variants (missing weekday, "(KST)" comments are handled by
// net/mail already). Returns zero time when nothing parses (fail-open: the
// caller skips the anchor).
func parseMailDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if t, err := mail.ParseDate(raw); err == nil {
		return t
	}
	for _, layout := range []string{time.RFC1123Z, time.RFC1123, "2006-01-02 15:04:05 -0700", "2006-01-02"} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}

func koreanDate(t time.Time) string {
	return fmt.Sprintf("%s(%s)", t.Format("2006-01-02"), koreanWeekdays[int(t.Weekday())])
}

// mondayOf returns the Monday 00:00 of t's week (Monday-start, Korean business
// convention: "이번 주" spans 월~일).
func mondayOf(t time.Time) time.Time {
	y, m, d := t.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, t.Location())
	offset := (int(day.Weekday()) + 6) % 7 // Mon=0 … Sun=6
	return day.AddDate(0, 0, -offset)
}

func weekRow(label string, monday time.Time) string {
	var sb strings.Builder
	sb.WriteString("  - " + label + ": ")
	for i := 0; i < 7; i++ {
		d := monday.AddDate(0, 0, i)
		if i > 0 {
			sb.WriteString(" ")
		}
		sb.WriteString(fmt.Sprintf("%s(%s)", d.Format("01-02"), koreanWeekdays[int(d.Weekday())]))
	}
	return sb.String()
}

// buildDateAnchor renders the date-anchor block for one message, or "" when
// the Date header does not parse. now is the analysis wall-clock (injected
// for testability); the conversion table is anchored on the SEND date because
// that is the reference point of relative phrases in the mail body.
func buildDateAnchor(msg *gmail.MessageDetail, now time.Time) string {
	if msg == nil {
		return ""
	}
	sent := parseMailDate(msg.Date)
	if sent.IsZero() {
		return ""
	}
	sent = sent.In(anchorTimezone)
	now = now.In(anchorTimezone)
	monday := mondayOf(sent)

	var sb strings.Builder
	sb.WriteString("## 날짜 앵커 (헤더 기반 결정적 계산)\n")
	sb.WriteString("- 이 메일의 발송일: " + koreanDate(sent) + "\n")
	sb.WriteString("- 분석 시점(오늘): " + koreanDate(now) + "\n")
	sb.WriteString("- 발송일 기준 환산표 (주는 월~일):\n")
	sb.WriteString(weekRow("이번 주 (발송 주)", monday) + "\n")
	sb.WriteString(weekRow("다음 주", monday.AddDate(0, 0, 7)) + "\n")
	sb.WriteString(weekRow("그 다음 주", monday.AddDate(0, 0, 14)) + "\n")
	sb.WriteString("- 판독 규칙: 본문의 상대 날짜 표현(내일·이번/다음 주 ○요일·월말 등)은 발송일 기준으로 이 표에서 찾아 절대 날짜로 옮겨 적는다. 직접 계산하지 말 것.")
	return sb.String()
}
