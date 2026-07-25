package server

import (
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
)

// With the Google write mirror on (default since #4210), a local event also
// exists on Google. The HUD's own dedup keys on event ID and CANNOT catch that
// pair — the local copy carries a "local:" id and the Google copy carries
// Google's — so the glasses showed one meeting twice. The miniapp calendar list
// drops the mirrors; the HUD has to drop the same ones.
func TestFilterMirroredEventsDropsOnlyDenebAuthoredCopies(t *testing.T) {
	start := time.Now()
	google := []calendar.Event{
		{ID: "g-mirror-1", Summary: "탑솔라 미팅", Start: start},   // Deneb wrote this one
		{ID: "g-native-1", Summary: "외부 주최 회의", Start: start}, // genuinely Google-only
	}
	mirrored := map[string]struct{}{"g-mirror-1": {}}

	got := filterMirroredEvents(google, mirrored)
	if len(got) != 1 || got[0].ID != "g-native-1" {
		t.Fatalf("mirror must be dropped and the native event kept, got %+v", got)
	}
}

// Write sync off / nothing mirrored yet must be a pure pass-through — no Google
// event is a mirror of anything, and dropping any of them would hide real ones.
func TestFilterMirroredEventsPassesThroughWhenNothingMirrored(t *testing.T) {
	google := []calendar.Event{{ID: "g-1"}, {ID: "g-2"}}
	for name, mirrored := range map[string]map[string]struct{}{
		"nil map":   nil,
		"empty map": {},
	} {
		if got := filterMirroredEvents(google, mirrored); len(got) != 2 {
			t.Errorf("%s: want both events kept, got %+v", name, got)
		}
	}
}
