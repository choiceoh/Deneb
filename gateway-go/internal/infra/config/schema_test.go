package config

import "testing"

func TestLookupSchema(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		wantNil  bool
		wantType string
		wantDesc string
	}{
		{
			name:     "root",
			path:     "",
			wantType: "object",
			wantDesc: "Deneb configuration schema",
		},
		{
			name:     "gateway",
			path:     "gateway",
			wantType: "object",
			wantDesc: "Gateway server settings",
		},
		{
			name:     "gateway.port",
			path:     "gateway.port",
			wantType: "number",
			wantDesc: "Gateway port",
		},
		{
			name:     "gateway.mode",
			path:     "gateway.mode",
			wantType: "string",
		},
		{
			name:     "logging.level",
			path:     "logging.level",
			wantType: "string",
		},
		{
			name:     "session.mainKey",
			path:     "session.mainKey",
			wantType: "string",
		},
		{
			name:     "agents.maxConcurrent",
			path:     "agents.maxConcurrent",
			wantType: "number",
		},
		{
			name:    "nonexistent top-level",
			path:    "nonexistent",
			wantNil: true,
		},
		{
			name:    "nonexistent nested",
			path:    "gateway.nonexistent",
			wantNil: true,
		},
		{
			name:    "too deep path",
			path:    "gateway.port.extra",
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupSchema(tt.path)
			if tt.wantNil {
				if got != nil {
					t.Errorf("LookupSchema(%q) = %+v, want nil", tt.path, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("LookupSchema(%q) = nil, want non-nil", tt.path)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %q, want %q", got.Type, tt.wantType)
			}
			if tt.wantDesc != "" && got.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", got.Description, tt.wantDesc)
			}
		})
	}
}

func TestLookupSchemaDefaults(t *testing.T) {
	node := LookupSchema("gateway.port")
	if node == nil {
		t.Fatal("gateway.port should exist")
	}
	if node.Default != DefaultGatewayPort {
		t.Errorf("Default = %v, want %v", node.Default, DefaultGatewayPort)
	}

	node = LookupSchema("session.mainKey")
	if node == nil {
		t.Fatal("session.mainKey should exist")
	}
	if node.Default != "main" {
		t.Errorf("Default = %v, want %q", node.Default, "main")
	}
}

func TestLookupSchemaEnums(t *testing.T) {
	node := LookupSchema("gateway.mode")
	if node == nil {
		t.Fatal("gateway.mode should exist")
	}
	if len(node.Enum) != 2 {
		t.Fatalf("len(Enum) = %d, want 2", len(node.Enum))
	}
	if node.Enum[0] != "local" || node.Enum[1] != "remote" {
		t.Errorf("Enum = %v, want [local remote]", node.Enum)
	}

	node = LookupSchema("logging.level")
	if node == nil {
		t.Fatal("logging.level should exist")
	}
	if len(node.Enum) != 4 {
		t.Fatalf("len(Enum) = %d, want 4", len(node.Enum))
	}
}

func TestHashString(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			name:  "hello",
			input: "hello",
			want:  "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HashString(tt.input)
			if got != tt.want {
				t.Errorf("HashString(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	// Deterministic: same input always produces same hash.
	h1, h2 := HashString("test"), HashString("test")
	if h1 != h2 {
		t.Error("HashString should be deterministic")
	}

	// Different inputs produce different hashes.
	if HashString("a") == HashString("b") {
		t.Error("different inputs should produce different hashes")
	}
}
