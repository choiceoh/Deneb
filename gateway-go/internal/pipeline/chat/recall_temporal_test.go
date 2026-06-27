package chat

import (
	"testing"
	"time"
)

var testKST = time.FixedZone("KST", 9*3600)

func tsKST(y int, m time.Month, d int) int64 {
	return time.Date(y, m, d, 12, 0, 0, 0, testKST).UnixMilli()
}

func inRange(tr recallTemporalRange, ms int64) bool {
	return tr.ok && ms >= tr.From && ms <= tr.To
}

func TestParseRecallTemporalRangeAt(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, testKST) // mid-June anchor

	cases := []struct {
		name   string
		msg    string
		wantOK bool
		in     []int64 // must be inside the range
		out    []int64 // must be outside the range
	}{
		{"어제", "어제 단가 얼마였지", true,
			[]int64{tsKST(2026, 6, 14)}, []int64{tsKST(2026, 6, 15), tsKST(2026, 6, 13)}},
		{"그저께", "그저께 뭐라고 했어", true,
			[]int64{tsKST(2026, 6, 13)}, []int64{tsKST(2026, 6, 14)}},
		{"지난달", "지난달 트라이브 단가", true,
			[]int64{tsKST(2026, 5, 10)}, []int64{tsKST(2026, 6, 1), tsKST(2026, 4, 30)}},
		{"이번달", "이번 달에 만난 사람", true,
			[]int64{tsKST(2026, 6, 5)}, []int64{tsKST(2026, 5, 31)}},
		{"작년", "작년 견적 어땠지", true,
			[]int64{tsKST(2025, 7, 1)}, []int64{tsKST(2026, 1, 1), tsKST(2024, 12, 31)}},
		{"재작년", "재작년에 한 일", true,
			[]int64{tsKST(2024, 3, 1)}, []int64{tsKST(2025, 1, 1)}},
		{"올해", "올해 목표 뭐였지", true,
			[]int64{tsKST(2026, 2, 1)}, []int64{tsKST(2025, 12, 31)}},
		{"N월-past", "6월 단가 얼마", true,
			[]int64{tsKST(2026, 6, 10)}, []int64{tsKST(2026, 7, 1), tsKST(2025, 6, 10)}},
		{"N월-future-is-lastyear", "11월에 뭐 했지", true,
			[]int64{tsKST(2025, 11, 10)}, []int64{tsKST(2026, 11, 10)}},
		{"no-cue", "트라이브 단가 얼마였지", false, nil, nil},
		{"vague", "그때 단가 얼마였더라", false, nil, nil},
		{"개월-not-month", "3개월 전 일은 기억 안 나", false, nil, nil},
		{"월요일-not-month", "월요일에 만나기로 했었나", false, nil, nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tr := parseRecallTemporalRangeAt(c.msg, now)
			if tr.ok != c.wantOK {
				t.Fatalf("ok=%v want %v (range %+v)", tr.ok, c.wantOK, tr)
			}
			for _, ms := range c.in {
				if !inRange(tr, ms) {
					t.Errorf("ts %d should be IN range %+v", ms, tr)
				}
			}
			for _, ms := range c.out {
				if inRange(tr, ms) {
					t.Errorf("ts %d should be OUT of range %+v", ms, tr)
				}
			}
		})
	}
}

func TestParseRecallTemporalRangeWeeks(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, testKST)
	this := parseRecallTemporalRangeAt("이번주 한 일", now)
	last := parseRecallTemporalRangeAt("지난주 한 일", now)
	if !this.ok || !last.ok {
		t.Fatal("week cues should ground")
	}
	if !inRange(this, now.UnixMilli()) {
		t.Error("now should be within 이번주")
	}
	if last.To >= this.From {
		t.Errorf("지난주 (%d..%d) must end before 이번주 starts (%d)", last.From, last.To, this.From)
	}
	const day = int64(24 * 60 * 60 * 1000)
	if span := last.To - last.From; span < 7*day-day/2 || span > 7*day {
		t.Errorf("지난주 span = %dms, want ~7 days", span)
	}
}
