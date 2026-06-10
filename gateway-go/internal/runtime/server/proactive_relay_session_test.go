package server

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// A proactive delivery to a brand-new sub-session must register that session
// in the manager immediately — the native drawer reads Manager.List(), so a
// transcript-only session would otherwise stay invisible until the next
// restart's transcript rescan. Regression guard for the client:main:dream
// first-delivery gap.
func TestRelayRegistersNewSubSession(t *testing.T) {
	mgr := &session.Manager{}
	d := proactiveRelayDeps{
		transcriptStore: newRecordingTranscriptStore(),
		sessions:        mgr,
	}

	if ok, err := d.relayNativeTo(dreamWorkSessionKey, "📖 Wiki Dream 완료: 제안 2, 생성 1, 수정 0 (3.2s)"); err != nil || !ok {
		t.Fatalf("relay: ok=%v err=%v", ok, err)
	}

	s := mgr.Get(dreamWorkSessionKey)
	if s == nil {
		t.Fatalf("session %q not registered in manager", dreamWorkSessionKey)
	}
	if s.Kind != session.KindDirect || s.Status != session.StatusDone || s.Channel != "client" {
		t.Errorf("registered shape = kind %q status %q channel %q; want direct/done/client",
			s.Kind, s.Status, s.Channel)
	}
	if s.UpdatedAt <= 0 {
		t.Errorf("UpdatedAt = %d, want message timestamp", s.UpdatedAt)
	}
}

// A delivery to an already-registered session must only advance UpdatedAt —
// never replace the row. A concurrent agent run owns its status and stats;
// clobbering a RUNNING session back to done would corrupt the live drawer row.
func TestRelayTouchesExistingSessionWithoutClobber(t *testing.T) {
	mgr := &session.Manager{}
	if err := mgr.Set(&session.Session{
		Key:       dreamWorkSessionKey,
		Kind:      session.KindDirect,
		Status:    session.StatusRunning,
		Channel:   "client",
		Model:     "vllm/step3p7",
		UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("seed session: %v", err)
	}
	d := proactiveRelayDeps{
		transcriptStore: newRecordingTranscriptStore(),
		sessions:        mgr,
	}

	if ok, err := d.relayNativeTo(dreamWorkSessionKey, "📖 Wiki Dream 완료: 제안 1, 생성 0, 수정 1 (2.0s)"); err != nil || !ok {
		t.Fatalf("relay: ok=%v err=%v", ok, err)
	}

	s := mgr.Get(dreamWorkSessionKey)
	if s == nil {
		t.Fatal("session vanished")
	}
	if s.Status != session.StatusRunning {
		t.Errorf("status = %q, want running (must not clobber a live run)", s.Status)
	}
	if s.Model != "vllm/step3p7" {
		t.Errorf("model = %q, want preserved", s.Model)
	}
	if s.UpdatedAt <= 1 {
		t.Errorf("UpdatedAt = %d, want advanced past seed value", s.UpdatedAt)
	}
}

// A suppressed delivery (contentless "변경 없음") writes nothing, so it must not
// register a session either — an empty drawer row with no transcript would be
// exactly the kind of ghost the suppression floor exists to prevent.
func TestRelaySuppressedDeliveryRegistersNothing(t *testing.T) {
	mgr := &session.Manager{}
	d := proactiveRelayDeps{
		transcriptStore: newRecordingTranscriptStore(),
		sessions:        mgr,
	}

	if ok, _ := d.relayNativeTo(dreamWorkSessionKey, "🌙 Aurora Dream 완료: 변경 없음 (1.2s)"); ok {
		t.Fatal("contentless body should be suppressed")
	}
	if s := mgr.Get(dreamWorkSessionKey); s != nil {
		t.Errorf("suppressed delivery registered session %+v", s)
	}
}

// Older wiring and tests construct proactiveRelayDeps without a session
// manager; delivery must still work with registration skipped.
func TestRelayNilSessionsIsSafe(t *testing.T) {
	d := proactiveRelayDeps{transcriptStore: newRecordingTranscriptStore()}
	if ok, err := d.relayNativeTo(dreamWorkSessionKey, "📖 Wiki Dream 완료: 제안 1 (1.0s)"); err != nil || !ok {
		t.Fatalf("relay without sessions: ok=%v err=%v", ok, err)
	}
}
