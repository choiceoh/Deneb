package chat

import (
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
)

func bulletCount(digest string) int {
	n := 0
	for _, ln := range strings.Split(digest, "\n") {
		if strings.HasPrefix(ln, "- ") {
			n++
		}
	}
	return n
}

func TestBuildTodayFeedDigest(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Skip("no Asia/Seoul tzdata")
	}
	now := time.Date(2026, 6, 15, 14, 0, 0, 0, loc)
	startOfDay := time.Date(2026, 6, 15, 0, 0, 0, 0, loc).UnixMilli()

	// Empty feed → empty digest (a quiet day injects nothing).
	if got := buildTodayFeedDigest(nil, now); got != "" {
		t.Errorf("empty feed should yield empty digest, got %q", got)
	}

	// Only pre-today items → excluded → empty.
	old := []workfeed.Item{{Title: "어제 리포트", CreatedAtMs: startOfDay - 1}}
	if got := buildTodayFeedDigest(old, now); got != "" {
		t.Errorf("pre-today items must be excluded, got %q", got)
	}

	// Today's items render with the header; pre-today ones are dropped.
	items := []workfeed.Item{
		{Title: "메일 분석", Summary: "탑솔라 견적 회신 필요", CreatedAtMs: startOfDay + 1000},
		{Title: "어제 것", CreatedAtMs: startOfDay - 5000},
		{Title: "회의 녹음 요약", Summary: "다음 액션 3건", CreatedAtMs: startOfDay + 2000},
	}
	got := buildTodayFeedDigest(items, now)
	if !strings.Contains(got, "오늘의 업무 피드") {
		t.Errorf("digest missing header: %q", got)
	}
	if !strings.Contains(got, "메일 분석: 탑솔라 견적 회신 필요") ||
		!strings.Contains(got, "회의 녹음 요약: 다음 액션 3건") {
		t.Errorf("digest missing today's items: %q", got)
	}
	if strings.Contains(got, "어제 것") {
		t.Errorf("digest must exclude pre-today items: %q", got)
	}
	if n := bulletCount(got); n != 2 {
		t.Errorf("expected 2 bullets, got %d: %q", n, got)
	}

	// More than the line cap → capped.
	many := make([]workfeed.Item, feedDigestLineCap+10)
	for i := range many {
		many[i] = workfeed.Item{Title: "항목", CreatedAtMs: startOfDay + int64(i)}
	}
	if n := bulletCount(buildTodayFeedDigest(many, now)); n != feedDigestLineCap {
		t.Errorf("digest should cap at %d bullets, got %d", feedDigestLineCap, n)
	}

	// Long summary is rune-truncated with an ellipsis.
	long := []workfeed.Item{{Title: "긴 항목", Summary: strings.Repeat("가", feedDigestRuneCap+50), CreatedAtMs: startOfDay + 1}}
	d := buildTodayFeedDigest(long, now)
	if !strings.Contains(d, "…") {
		t.Errorf("over-long line should be truncated with an ellipsis: %q", d)
	}
}
