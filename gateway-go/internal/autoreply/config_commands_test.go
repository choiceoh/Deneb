package autoreply

import "testing"

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

func TestParseConfigCommand_Set(t *testing.T) {
	cmd := ParseConfigCommand("/config set gateway.port 8080")
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

func TestParseConfigCommand_SetBoolean(t *testing.T) {
	cmd := ParseConfigCommand("/config set verbose true")
	if cmd == nil {
		t.Fatal("expected command")
	}
	if cmd.Action != ConfigActionSet {
		t.Fatalf("expected set, got %s", cmd.Action)
	}
	if cmd.Value != true {
		t.Fatalf("expected value=true, got %v", cmd.Value)
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
	if cmd.Path != "gateway.port" {
		t.Fatalf("expected path=gateway.port, got %q", cmd.Path)
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
		// Simple type comparison for non-map types.
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
