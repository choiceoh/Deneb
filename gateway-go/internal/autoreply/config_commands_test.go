package autoreply

import "testing"

// --- ParseSlashCommandOrNull ---

func TestParseSlashCommandOrNull_Match(t *testing.T) {
	result := ParseSlashCommandOrNull("/config show foo", "/config", "show", "Invalid.")
	if result == nil {
		t.Fatal("expected result")
	}
	if !result.OK {
		t.Fatal("expected OK")
	}
	if result.Action != "show" {
		t.Fatalf("expected action=show, got %q", result.Action)
	}
	if result.Args != "foo" {
		t.Fatalf("expected args=foo, got %q", result.Args)
	}
}

func TestParseSlashCommandOrNull_NoMatch(t *testing.T) {
	result := ParseSlashCommandOrNull("hello", "/config", "show", "Invalid.")
	if result != nil {
		t.Fatal("expected nil for non-matching command")
	}
}

func TestParseSlashCommandOrNull_EmptyArgs(t *testing.T) {
	result := ParseSlashCommandOrNull("/config", "/config", "show", "Invalid.")
	if result == nil || !result.OK {
		t.Fatal("expected OK result")
	}
	if result.Action != "show" {
		t.Fatalf("expected default action=show, got %q", result.Action)
	}
}

func TestParseSlashCommandOrNull_ColonSyntax(t *testing.T) {
	result := ParseSlashCommandOrNull("/config:set foo=bar", "/config", "show", "Invalid.")
	if result == nil || !result.OK {
		t.Fatal("expected OK result")
	}
	if result.Action != "set" {
		t.Fatalf("expected action=set, got %q", result.Action)
	}
}

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

// --- ParseDebugCommand ---

func TestParseDebugCommand_Show(t *testing.T) {
	cmd := ParseDebugCommand("/debug show")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionShow {
		t.Fatalf("expected show, got %s", cmd.Action)
	}
}

func TestParseDebugCommand_Reset(t *testing.T) {
	cmd := ParseDebugCommand("/debug reset")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionUnset {
		t.Fatalf("expected unset (reset), got %s", cmd.Action)
	}
}

func TestParseDebugCommand_Set(t *testing.T) {
	cmd := ParseDebugCommand("/debug set verbose=true")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionSet {
		t.Fatalf("expected set, got %s", cmd.Action)
	}
}

// --- ParseConfigValue ---

func TestParseConfigValue_Types(t *testing.T) {
	tests := []struct {
		input   string
		want    any
		wantErr bool
	}{
		{"42", float64(42), false},
		{"3.14", float64(3.14), false},
		{"true", true, false},
		{"false", false, false},
		{"null", nil, false},
		{`"hello"`, "hello", false},
		{"plain", "plain", false},
		{`{"key": "value"}`, map[string]any{"key": "value"}, false},
		{`[1, 2, 3]`, []any{float64(1), float64(2), float64(3)}, false},
		{"", nil, true},
		{`{invalid`, nil, true},
	}

	for _, tt := range tests {
		val, errMsg := ParseConfigValue(tt.input)
		if tt.wantErr {
			if errMsg == "" {
				t.Errorf("ParseConfigValue(%q): expected error", tt.input)
			}
			continue
		}
		if errMsg != "" {
			t.Errorf("ParseConfigValue(%q): unexpected error: %s", tt.input, errMsg)
			continue
		}
		switch expected := tt.want.(type) {
		case float64:
			if v, ok := val.(float64); !ok || v != expected {
				t.Errorf("ParseConfigValue(%q) = %v, want %v", tt.input, val, expected)
			}
		case bool:
			if v, ok := val.(bool); !ok || v != expected {
				t.Errorf("ParseConfigValue(%q) = %v, want %v", tt.input, val, expected)
			}
		case string:
			if v, ok := val.(string); !ok || v != expected {
				t.Errorf("ParseConfigValue(%q) = %v, want %v", tt.input, val, expected)
			}
		case nil:
			if val != nil {
				t.Errorf("ParseConfigValue(%q) = %v, want nil", tt.input, val)
			}
		}
	}
}

func TestParseConfigValue_SingleQuoted(t *testing.T) {
	val, err := ParseConfigValue(`'hello'`)
	if err != "" {
		t.Fatalf("unexpected error: %s", err)
	}
	if val != "hello" {
		t.Fatalf("expected 'hello', got %v", val)
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
