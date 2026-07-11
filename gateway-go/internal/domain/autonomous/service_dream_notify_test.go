package autonomous

import (
	"context"
	"strings"
	"testing"
)

// A dream cycle that completes with 0 net changes but a phase error must headline
// as a failure — not "완료: 변경 없음" with the real error buried in the summary. That
// masking made a dream that errored every cycle (e.g. the synthesis tags-unmarshal
// failure) look like a quiet no-op in the work feed.
func TestServiceDreamPhaseErrorSurfacesInHeadline(t *testing.T) {
	svc := NewService(nil)
	d := &fakeDreamer{shouldDream: true, runReport: &DreamReport{
		DurationMs:  300,
		PhaseErrors: []string{"synthesis: parse LLM response: cannot unmarshal"},
	}}
	n := &fakeNotifier{}
	events := make(chan CycleEvent, 4)
	svc.OnEvent(func(ev CycleEvent) { events <- ev })
	svc.SetNotifier(n)
	svc.SetDreamer(d)

	svc.IncrementDreamTurn(context.Background())
	_ = waitForEvent(t, events, "dreaming_started")
	_ = waitForEvent(t, events, "dreaming_completed")

	n.mu.Lock()
	msg := n.message
	n.mu.Unlock()

	if !strings.Contains(msg, "실패") {
		t.Errorf("phase-error dream headline should surface 실패, got %q", msg)
	}
	if strings.Contains(msg, "완료: 변경 없음") {
		t.Errorf("phase-error dream was masked as 완료: 변경 없음: %q", msg)
	}
}
