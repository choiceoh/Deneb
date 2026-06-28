package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/observatory"
)

func TestWatchdogAlerts(t *testing.T) {
	rep := observatory.Report{
		Liveness: []observatory.LoopStatus{
			{Name: "dreamer", AgeHours: 60, Fresh: false},    // stale → alert
			{Name: "skill-review", AgeHours: 2, Fresh: true}, // fresh → none
			{Name: "diary", Missing: true},                   // missing → none
		},
		Failures: []observatory.FailureCount{
			{Pattern: "type-coercion drop", Count: 7}, // ≥5 → alert
			{Pattern: "unknown tool", Count: 2},       // <5 → none
		},
	}

	titles := map[string]bool{}
	alerts := watchdogAlerts(rep, observatoryFailAlertThreshold)
	for _, a := range alerts {
		titles[a.Title] = true
		// The changing age/count must live in the body, not the title, or the
		// alert gate (keyed on title) would re-push every tick.
		if strings.ContainsAny(a.Title, "0123456789") {
			t.Errorf("title %q embeds a number — breaks gate dedup", a.Title)
		}
	}
	if len(alerts) != 2 {
		t.Fatalf("want 2 alerts, got %d: %+v", len(alerts), alerts)
	}
	if !titles["개선 루프 정지: dreamer"] {
		t.Error("missing dreamer-stale alert")
	}
	if !titles["침묵 실패 급증: type-coercion drop"] {
		t.Error("missing failure-spike alert")
	}
	if titles["개선 루프 정지: skill-review"] {
		t.Error("a fresh loop must not alert")
	}
	if titles["개선 루프 정지: diary"] {
		t.Error("a missing loop must not alert")
	}
}

func TestWatchdogAlerts_AllHealthy(t *testing.T) {
	rep := observatory.Report{
		Liveness: []observatory.LoopStatus{{Name: "dreamer", AgeHours: 2, Fresh: true}},
		Failures: []observatory.FailureCount{{Pattern: "x", Count: 1}},
	}
	if a := watchdogAlerts(rep, observatoryFailAlertThreshold); len(a) != 0 {
		t.Errorf("a healthy snapshot must yield no alerts, got %+v", a)
	}
}
