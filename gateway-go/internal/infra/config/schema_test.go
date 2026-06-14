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
			name:     "gateway.bind",
			path:     "gateway.bind",
			wantType: "string",
		},
		{
			name:     "logging.level",
			path:     "logging.level",
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
}

func TestLookupSchemaEnums(t *testing.T) {
	node := LookupSchema("gateway.bind")
	if node == nil {
		t.Fatal("gateway.bind should exist")
	}
	if len(node.Enum) != 5 {
		t.Fatalf("len(Enum) = %d, want 5", len(node.Enum))
	}
	if node.Enum[0] != BindAuto {
		t.Errorf("Enum[0] = %v, want %q", node.Enum[0], BindAuto)
	}

	node = LookupSchema("logging.level")
	if node == nil {
		t.Fatal("logging.level should exist")
	}
	if len(node.Enum) != 4 {
		t.Fatalf("len(Enum) = %d, want 4", len(node.Enum))
	}
}
