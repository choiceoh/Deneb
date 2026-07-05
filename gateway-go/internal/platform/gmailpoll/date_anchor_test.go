package gmailpoll

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// The measured trap: a Thursday-sent mail saying "다음 주 금요일까지" must be
// resolvable by table lookup — the table's 다음 주 row must carry the exact
// Friday date so the model copies instead of computing (dsv4 computed it
// off-by-one in both prompt formats, 2026-07-04).
func TestBuildDateAnchor_ThursdayNextWeekTable(t *testing.T) {
	// 2026-07-02 is a real Thursday.
	msg := &gmail.MessageDetail{Date: "Thu, 2 Jul 2026 10:05:00 +0900"}
	now := time.Date(2026, 7, 5, 10, 0, 0, 0, anchorTimezone)
	got := buildDateAnchor(msg, now)

	for _, want := range []string{
		"발송일: 2026-07-02(목)",
		"분석 시점(오늘): 2026-07-05(일)",
		"이번 주 (발송 주)",
		"06-29(월)",         // 발송 주 월요일
		"다음 주: 07-06(월)",   // 다음 주 시작
		"07-10(금)",         // ★ 다음 주 금요일 — the trap answer, by lookup
		"그 다음 주: 07-13(월)", // "그 다음 주 월요일" (m5의 내부 보고일 표현)
		"직접 계산하지 말 것",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("date anchor missing %q:\n%s", want, got)
		}
	}
}

// Sunday-sent mail: Monday-start weeks mean "이번 주" reaches back six days.
func TestBuildDateAnchor_SundayWeekStart(t *testing.T) {
	msg := &gmail.MessageDetail{Date: "Sun, 5 Jul 2026 09:00:00 +0900"}
	got := buildDateAnchor(msg, time.Date(2026, 7, 5, 12, 0, 0, 0, anchorTimezone))
	if !strings.Contains(got, "이번 주 (발송 주): 06-29(월)") {
		t.Errorf("Sunday mail must anchor to the preceding Monday:\n%s", got)
	}
}

// UTC-offset headers must land on the KST calendar date (a late-UTC send is
// the next day in Seoul).
func TestBuildDateAnchor_ForeignOffsetNormalizedToKST(t *testing.T) {
	msg := &gmail.MessageDetail{Date: "Fri, 3 Jul 2026 20:30:00 +0000"} // = 7/4 05:30 KST
	got := buildDateAnchor(msg, time.Date(2026, 7, 5, 12, 0, 0, 0, anchorTimezone))
	if !strings.Contains(got, "발송일: 2026-07-04(토)") {
		t.Errorf("UTC evening send must render as the KST next day:\n%s", got)
	}
}

func TestBuildDateAnchor_UnparsableDateFailsOpen(t *testing.T) {
	for _, raw := range []string{"", "날짜없음", "2026/07/03 목요일쯤"} {
		if got := buildDateAnchor(&gmail.MessageDetail{Date: raw}, time.Now()); got != "" {
			t.Errorf("unparsable date %q must skip the anchor, got %q", raw, got)
		}
	}
	if got := buildDateAnchor(nil, time.Now()); got != "" {
		t.Errorf("nil message must skip the anchor")
	}
}
