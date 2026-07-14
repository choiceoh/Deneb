package genesis

import (
	"testing"
	"time"
)

func TestRecentRealUseSessionsBySkill_WindowDedupCapOrder(t *testing.T) {
	tr := newTestTracker(t)
	now := time.Now().UnixMilli()
	hourMs := int64(time.Hour / time.Millisecond)

	seed := []UsageRecord{
		// Newest-first expected: s3 (now-1h), s2 (now-2h), s1 (now-3h, duplicate later).
		{SkillName: "contract-review", SessionKey: "s1", Success: false, ErrorMsg: "boom", UsedAt: now - 3*hourMs, Source: UsageSourceReal},
		{SkillName: "contract-review", SessionKey: "s2", Success: true, UsedAt: now - 2*hourMs, Source: UsageSourceReal},
		{SkillName: "contract-review", SessionKey: "s3", Success: true, UsedAt: now - hourMs, Source: UsageSourceReal},
		{SkillName: "contract-review", SessionKey: "s1", Success: true, UsedAt: now - 30*hourMs, Source: UsageSourceReal},
		// Review-fork use never earns bench coverage.
		{SkillName: "contract-review", SessionKey: "system:skill-review:x", Success: true, UsedAt: now - hourMs, Source: usageSourceReviewConsult},
		// Outside the window.
		{SkillName: "ocr-run", SessionKey: "old", Success: true, UsedAt: now - 10*24*hourMs, Source: UsageSourceReal},
	}
	for _, r := range seed {
		appendFunnel(t, tr.usagePath, r)
	}

	got := tr.RecentRealUseSessionsBySkill(7*24*time.Hour, 2)
	if len(got) != 1 {
		t.Fatalf("only contract-review has in-window real use, got %v", got)
	}
	sessions := got["contract-review"]
	if len(sessions) != 2 || sessions[0] != "s3" || sessions[1] != "s2" {
		t.Fatalf("want newest-first capped [s3 s2], got %v", sessions)
	}
}
