package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWeeklyWriteSidecar verifies the structured JSON snapshot is written next to
// the render output and round-trips through the envelope shape. The rendered
// PDF/PNG are terminal artifacts the agent can't re-read; this sidecar is what a
// later turn (or the dropbox backup) consumes, so it must be valid JSON that
// preserves the envelope — including the *int days_to_due pointer.
func TestWeeklyWriteSidecar(t *testing.T) {
	dir := t.TempDir()
	due := 2
	env := weeklyEnvelope{
		Office:      "기획조정실",
		Reporter:    "오선택 실장",
		WeekDone:    "26.06.01~26.06.07",
		WeekPlanned: "26.06.08~26.06.14",
		GeneratedAt: "2026-06-09T10:00:00+09:00",
		Groups: []weeklyGroup{{
			Sogan: "2팀",
			Label: "태양광 발전 (2팀)",
			Projects: []weeklyProject{{
				Sogan:       "2팀",
				Title:       "루프탑 A",
				Capacity:    "2.5MW",
				DoneLine:    "착공",
				PlannedLine: "준공",
				DaysToDue:   &due,
			}},
		}},
		Issues: []string{"루프탑 A : 마감 D-2 (2026-06-11)"},
	}

	weeklyWriteSidecar(env, dir)

	data, err := os.ReadFile(filepath.Join(dir, weeklySidecarName))
	if err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	var got weeklyEnvelope
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v", err)
	}
	if got.Office != env.Office || got.Reporter != env.Reporter {
		t.Errorf("header round-trip mismatch: office=%q reporter=%q", got.Office, got.Reporter)
	}
	if len(got.Groups) != 1 || len(got.Groups[0].Projects) != 1 {
		t.Fatalf("groups round-trip mismatch: %+v", got.Groups)
	}
	p := got.Groups[0].Projects[0]
	if p.Title != "루프탑 A" || p.Capacity != "2.5MW" {
		t.Errorf("project round-trip mismatch: %+v", p)
	}
	if p.DaysToDue == nil || *p.DaysToDue != due {
		t.Errorf("days_to_due pointer round-trip mismatch: %v", p.DaysToDue)
	}
	if len(got.Issues) != 1 {
		t.Errorf("issues round-trip mismatch: %+v", got.Issues)
	}
}

// TestWeeklyWriteSidecarBadDir ensures a write to a nonexistent directory is a
// no-op, not a panic — the sidecar is best-effort and must never block a render.
func TestWeeklyWriteSidecarBadDir(t *testing.T) {
	// Does not exist; WriteFile fails, function swallows the error.
	weeklyWriteSidecar(weeklyEnvelope{Office: "x"}, filepath.Join(t.TempDir(), "no", "such", "dir"))
}

// TestComposeWeeklyText locks the deterministic 주간업무보고 양식 — the cron path now
// emits this directly (no LLM), so the exact structure (header, ▢ group, • project
// with 실시/예정, ⚠️ 현안) must stay byte-stable.
func TestComposeWeeklyText(t *testing.T) {
	env := weeklyEnvelope{
		Office:      "기획조정실",
		WeekDone:    "26.06.01~26.06.07",
		WeekPlanned: "26.06.08~26.06.14",
		Groups: []weeklyGroup{
			{Label: "사업개발 (1팀)", Projects: []weeklyProject{
				{Title: "아르고에너지 NDA", Capacity: "400MW", DoneLine: "NDA 서명 완료", PlannedLine: "Teaser 송부"},
			}},
		},
		Issues: []string{"해남 EPC : 마감 D-3 (2026-06-16)"},
	}
	got := composeWeeklyText(env)
	for _, want := range []string{
		"📋 주간업무보고 — 기획조정실",
		"실시 26.06.01~26.06.07 / 예정 26.06.08~26.06.14",
		"▢ 사업개발 (1팀)",
		"  • 아르고에너지 NDA(400MW)",
		"     - 실시: NDA 서명 완료",
		"     - 예정: Teaser 송부",
		"⚠️ 현안",
		"  - 해남 EPC : 마감 D-3 (2026-06-16)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}

// TestComposeWeeklyTextEmpty verifies the no-activity case yields an explicit line
// (not a bare header), matching the form's empty-state rule.
func TestComposeWeeklyTextEmpty(t *testing.T) {
	got := composeWeeklyText(weeklyEnvelope{Office: "기획조정실", WeekDone: "a", WeekPlanned: "b"})
	if !strings.Contains(got, "이번 주 보고할 프로젝트 활동이 없습니다") {
		t.Errorf("empty report should state no activity, got:\n%s", got)
	}
}
