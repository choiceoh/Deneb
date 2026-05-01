package chat

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

func TestApplyThinkSlashCommand_TogglesInterleaved(t *testing.T) {
	m := session.NewManager()
	m.Create("s1", session.KindDirect)
	level := "high"
	m.Patch("s1", session.PatchFields{ThinkingLevel: &level})

	// First toggle: off → on.
	resp := applyThinkSlashCommand(m, "s1", "interleaved")
	if !strings.Contains(resp, "ON") {
		t.Errorf("first toggle response = %q, want ON", resp)
	}
	got := m.Get("s1")
	if got.InterleavedThinking == nil || !*got.InterleavedThinking {
		t.Fatalf("InterleavedThinking after first toggle = %v, want true", got.InterleavedThinking)
	}

	// Second toggle: on → off.
	resp = applyThinkSlashCommand(m, "s1", "interleaved")
	if !strings.Contains(resp, "OFF") {
		t.Errorf("second toggle response = %q, want OFF", resp)
	}
	got = m.Get("s1")
	if got.InterleavedThinking == nil || *got.InterleavedThinking {
		t.Fatalf("InterleavedThinking after second toggle = %v, want false", got.InterleavedThinking)
	}
}

func TestApplyThinkSlashCommand_ExplicitOnOff(t *testing.T) {
	m := session.NewManager()
	m.Create("s1", session.KindDirect)
	level := "high"
	m.Patch("s1", session.PatchFields{ThinkingLevel: &level})

	resp := applyThinkSlashCommand(m, "s1", "interleaved on")
	if !strings.Contains(resp, "ON") {
		t.Errorf("`on` response = %q, want ON", resp)
	}
	if got := m.Get("s1"); got.InterleavedThinking == nil || !*got.InterleavedThinking {
		t.Fatalf("after `on` got %v", got.InterleavedThinking)
	}

	resp = applyThinkSlashCommand(m, "s1", "interleaved off")
	if !strings.Contains(resp, "OFF") {
		t.Errorf("`off` response = %q, want OFF", resp)
	}
	if got := m.Get("s1"); got.InterleavedThinking == nil || *got.InterleavedThinking {
		t.Fatalf("after `off` got %v", got.InterleavedThinking)
	}
}

func TestApplyThinkSlashCommand_WarnsWhenLevelOff(t *testing.T) {
	m := session.NewManager()
	m.Create("s1", session.KindDirect)

	resp := applyThinkSlashCommand(m, "s1", "interleaved on")
	if !strings.Contains(resp, "ON") {
		t.Errorf("response = %q, want ON marker", resp)
	}
	// User should see a warning that no thinking level is set so the flag has
	// no effect until they enable thinking.
	if !strings.Contains(resp, "레벨") {
		t.Errorf("response = %q, want warning about thinking level", resp)
	}
}

func TestApplyThinkSlashCommand_NoSession(t *testing.T) {
	m := session.NewManager()
	resp := applyThinkSlashCommand(m, "missing", "interleaved")
	if !strings.Contains(resp, "세션") {
		t.Errorf("response = %q, want session-missing message", resp)
	}
}

func TestApplyThinkSlashCommand_StatusReport(t *testing.T) {
	m := session.NewManager()
	m.Create("s1", session.KindDirect)

	resp := applyThinkSlashCommand(m, "s1", "")
	if !strings.Contains(resp, "off") {
		t.Errorf("status response = %q, want default off", resp)
	}
}
