package autoreply

import (
	"testing"
)

func TestParseConfigValue(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		want    any
	}{
		{"empty", "", true, nil},
		{"whitespace", "  ", true, nil},
		{"true", "true", false, true},
		{"false", "false", false, false},
		{"null", "null", false, nil},
		{"integer", "42", false, float64(42)},
		{"negative", "-7", false, float64(-7)},
		{"float", "3.14", false, float64(3.14)},
		{"bare string", "hello", false, "hello"},
		{"json object", `{"a":1}`, false, map[string]any{"a": float64(1)}},
		{"json array", `[1,2]`, false, []any{float64(1), float64(2)}},
		{"quoted string", `"hello"`, false, "hello"},
		{"single quoted", `'hello'`, false, "hello"},
		{"invalid json", `{bad`, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseConfigValue(tt.input)
			if tt.wantErr {
				if result.Error == "" {
					t.Errorf("expected error, got value %v", result.Value)
				}
				return
			}
			if result.Error != "" {
				t.Errorf("unexpected error: %s", result.Error)
				return
			}
			// For nil check.
			if tt.want == nil {
				if result.Value != nil {
					t.Errorf("want nil, got %v", result.Value)
				}
				return
			}
			// Type-specific comparison.
			switch w := tt.want.(type) {
			case bool:
				if v, ok := result.Value.(bool); !ok || v != w {
					t.Errorf("want %v, got %v", w, result.Value)
				}
			case float64:
				if v, ok := result.Value.(float64); !ok || v != w {
					t.Errorf("want %v, got %v", w, result.Value)
				}
			case string:
				if v, ok := result.Value.(string); !ok || v != w {
					t.Errorf("want %q, got %v", w, result.Value)
				}
			}
		})
	}
}
