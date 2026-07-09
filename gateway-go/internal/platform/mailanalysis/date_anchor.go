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
package mailanalysis

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/mailbody"
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
		// Full YYYY-MM-DD per cell: the rule is "copy, don't calculate", so a
		// year-boundary week (late-Dec send, Jan deadline) must be copyable too.
		sb.WriteString(fmt.Sprintf("%s(%s)", d.Format("2006-01-02"), koreanWeekdays[int(d.Weekday())]))
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
	sent := mailbody.ParseMailDate(msg.Date)
	if sent.IsZero() {
		return ""
	}
	sent = sent.In(anchorTimezone)
	now = now.In(anchorTimezone)
	monday := mondayOf(sent)

	var sb strings.Builder
	// Last day of the send month (day 0 of the next month normalizes back).
	monthEnd := time.Date(sent.Year(), sent.Month()+1, 0, 0, 0, 0, 0, sent.Location())

	sb.WriteString("## 날짜 앵커 (헤더 기반 결정적 계산)\n")
	sb.WriteString("- 이 메일의 발송일: " + koreanDate(sent) + "\n")
	sb.WriteString("- 분석 실행 시각(참고용 — 본문 상대 표현의 기준 아님): " + koreanDate(now) + "\n")
	sb.WriteString("- 본문 지시어 환산: 오늘=" + koreanDate(sent) + " · 내일=" + koreanDate(sent.AddDate(0, 0, 1)) +
		" · 모레=" + koreanDate(sent.AddDate(0, 0, 2)) + " · 이번 달 말일=" + koreanDate(monthEnd) + "\n")
	sb.WriteString("- 발송일 기준 환산표 (주는 월~일):\n")
	sb.WriteString(weekRow("이번 주 (발송 주)", monday) + "\n")
	sb.WriteString(weekRow("다음 주", monday.AddDate(0, 0, 7)) + "\n")
	sb.WriteString(weekRow("그 다음 주", monday.AddDate(0, 0, 14)) + "\n")
	sb.WriteString("- 판독 규칙: 본문의 상대 날짜 표현(오늘·내일·모레·월말·이번/다음 주 ○요일 등)은 발송일 기준으로 위 지시어·환산표에서 찾아 절대 날짜로 옮겨 적는다. 직접 계산하지 말 것.")
	return sb.String()
}
