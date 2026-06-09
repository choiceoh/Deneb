package tools

import (
	"encoding/json"
	"os"
	"path/filepath"
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
