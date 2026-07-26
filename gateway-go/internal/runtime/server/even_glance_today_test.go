package server

import (
	"testing"
	"time"
)

// The reported symptom: the glasses' alert page showed a ten-day backlog whose
// newest entry was four days old, because "unread" was standing in for "recent".
func TestSameDay(t *testing.T) {
	loc := time.FixedZone("KST", 9*3600)
	now := time.Date(2026, 7, 26, 9, 0, 0, 0, loc)
	ms := func(tm time.Time) int64 { return tm.UnixMilli() }

	cases := []struct {
		name string
		at   int64
		want bool
	}{
		{"이른 오늘", ms(time.Date(2026, 7, 26, 0, 5, 0, 0, loc)), true},
		{"같은 시각", ms(now), true},
		{"늦은 오늘", ms(time.Date(2026, 7, 26, 23, 55, 0, 0, loc)), true},
		// A rolling 24h window would let this through at 9am; a calendar day
		// must not, or "오늘" quietly means "since yesterday morning".
		{"어제 저녁", ms(time.Date(2026, 7, 25, 22, 0, 0, 0, loc)), false},
		{"나흘 전 (보고된 증상)", ms(time.Date(2026, 7, 22, 9, 0, 0, 0, loc)), false},
		{"타임스탬프 없음", 0, false},
		{"음수", -1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameDay(tc.at, now); got != tc.want {
				t.Errorf("sameDay = %v, want %v", got, tc.want)
			}
		})
	}
}
