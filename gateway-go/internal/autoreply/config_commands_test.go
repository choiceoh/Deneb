package autoreply

import "testing"

// --- ParseConfigCommand ---

func TestParseConfigCommand_Show(t *testing.T) {
	cmd := ParseConfigCommand("/config show")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionShow {
		t.Fatalf("expected show, got %s", cmd.Action)
	}
}

func TestParseConfigCommand_ShowPath(t *testing.T) {
	cmd := ParseConfigCommand("/config show gateway.port")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionShow {
		t.Fatalf("expected show, got %s", cmd.Action)
	}
	if cmd.Path != "gateway.port" {
		t.Fatalf("expected path=gateway.port, got %q", cmd.Path)
	}
}

func TestParseConfigCommand_Get(t *testing.T) {
	cmd := ParseConfigCommand("/config get model")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionShow {
		t.Fatalf("expected show (alias), got %s", cmd.Action)
	}
}

func TestParseConfigCommand_SetEquals(t *testing.T) {
	cmd := ParseConfigCommand("/config set gateway.port=8080")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionSet {
		t.Fatalf("expected set, got %s", cmd.Action)
	}
	if cmd.Path != "gateway.port" {
		t.Fatalf("expected path=gateway.port, got %q", cmd.Path)
	}
	val, ok := cmd.Value.(float64)
	if !ok || val != 8080 {
		t.Fatalf("expected value=8080, got %v", cmd.Value)
	}
}

func TestParseConfigCommand_SetSpace(t *testing.T) {
	cmd := ParseConfigCommand("/config set gateway.port 8080")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionSet {
		t.Fatalf("expected set, got %s", cmd.Action)
	}
	val, ok := cmd.Value.(float64)
	if !ok || val != 8080 {
		t.Fatalf("expected value=8080, got %v", cmd.Value)
	}
}

func TestParseConfigCommand_SetBoolean(t *testing.T) {
	cmd := ParseConfigCommand("/config set verbose true")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Value != true {
		t.Fatalf("expected value=true, got %v", cmd.Value)
	}
}

func TestParseConfigCommand_SetJSON(t *testing.T) {
	cmd := ParseConfigCommand(`/config set agents.defaults {"model":"gpt-5"}`)
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionSet {
		t.Fatalf("expected set, got %s", cmd.Action)
	}
	m, ok := cmd.Value.(map[string]any)
	if !ok {
		t.Fatalf("expected map value, got %T", cmd.Value)
	}
	if m["model"] != "gpt-5" {
		t.Fatalf("expected model=gpt-5, got %v", m["model"])
	}
}

func TestParseConfigCommand_Unset(t *testing.T) {
	cmd := ParseConfigCommand("/config unset gateway.port")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionUnset {
		t.Fatalf("expected unset, got %s", cmd.Action)
	}
}

func TestParseConfigCommand_UnsetNoPath(t *testing.T) {
	cmd := ParseConfigCommand("/config unset")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionError {
		t.Fatalf("expected error, got %s", cmd.Action)
	}
}

func TestParseConfigCommand_NotConfig(t *testing.T) {
	cmd := ParseConfigCommand("hello world")
	if cmd != nil {
		t.Fatal("expected nil for non-config command")
	}
}

func TestParseConfigCommand_BareConfig(t *testing.T) {
	cmd := ParseConfigCommand("/config")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionShow {
		t.Fatalf("expected show, got %s", cmd.Action)
	}
}

func TestParseConfigCommand_UnknownAction(t *testing.T) {
	cmd := ParseConfigCommand("/config foobar")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionError {
		t.Fatalf("expected error for unknown action, got %s", cmd.Action)
	}
}

// --- CanBypassConfigWritePolicy ---

func TestCanBypassConfigWritePolicy(t *testing.T) {
	if !CanBypassConfigWritePolicy("telegram", []string{"operator.admin"}) {
		t.Fatal("expected admin scope to bypass")
	}
	if !CanBypassConfigWritePolicy("telegram", []string{"config.write"}) {
		t.Fatal("expected config.write scope to bypass")
	}
	if CanBypassConfigWritePolicy("telegram", []string{"operator.read"}) {
		t.Fatal("expected read scope to not bypass")
	}
}
