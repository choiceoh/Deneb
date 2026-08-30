package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// The window is carried in unix MILLIS and time.Duration counts NANOS. Every
// digest ever posted said "지난 0일간" because the conversion was missing: a
// 7-day window is 604,800,000 ms, which as a bare Duration is 0.6 seconds.
func TestDreamDigestWindowDaysUsesMillis(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		span time.Duration
		want int
	}{
		{"주간", 7 * 24 * time.Hour, 7},
		{"하루", 24 * time.Hour, 1},
		{"반나절은 0일", 12 * time.Hour, 0},
		{"3일", 72 * time.Hour, 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Assert on the RENDERED line, not a copy of the formula — a test
			// that recomputes the arithmetic passes just as happily when the
			// production path regresses.
			body := dreamDigestBody(dreamDigestStats{
				SinceMs: now.Add(-tc.span).UnixMilli(),
				UntilMs: now.UnixMilli(),
			})
			want := fmt.Sprintf("지난 %d일간", tc.want)
			if !strings.HasPrefix(body, want) {
				t.Fatalf("body opens with %q, want prefix %q",
					body[:min(len(body), 40)], want)
			}
		})
	}
}

// 평균 and 추이 are computed over different populations — every scored cycle vs
// the last N. Printed bare they read as a contradiction ("평균 72" next to
// "86→88"), so both must name their window.
func TestDreamDigestBodyLabelsBothQualityWindows(t *testing.T) {
	body := dreamDigestBody(dreamDigestStats{
		Cycles:        38,
		QualityScored: 21,
		QualityAvg:    72,
		QualityTrend:  []float64{86, 80, 88},
		SinceMs:       time.Now().Add(-7 * 24 * time.Hour).UnixMilli(),
		UntilMs:       time.Now().UnixMilli(),
	})
	if !strings.Contains(body, "지난 7일간") {
		t.Fatalf("window not rendered:\n%s", body)
	}
	// 38 cycles ran but only 21 scored — the average belongs to the 21, and
	// naming it "전체 38" would credit 17 cycles that never entered it.
	if !strings.Contains(body, "평균 품질(채점 21사이클)") {
		t.Fatalf("average does not name its population:\n%s", body)
	}
	if strings.Contains(body, "평균 품질(전체 38") {
		t.Fatal("average must not be attributed to unscored cycles")
	}
	if !strings.Contains(body, "품질 추이(최근 3사이클)") {
		t.Fatalf("trend does not name its population:\n%s", body)
	}
}
