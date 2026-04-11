package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newTestRegistryWithNames(names ...string) *ToolRegistry {
	r := NewToolRegistry()
	noopFn := func(_ context.Context, _ json.RawMessage) (string, error) { return "", nil }
	for _, n := range names {
		r.RegisterTool(ToolDef{
			Name:        n,
			Description: "test " + n,
			InputSchema: map[string]any{"type": "object"},
			Fn:          noopFn,
		})
	}
	return r
}

func TestSuggestToolNames_TypoSuggestions(t *testing.T) {
	r := newTestRegistryWithNames("read", "write", "edit", "grep", "exec", "tree")

	cases := []struct {
		typo string
		want string // expected first suggestion
	}{
		{"grpe", "grep"},
		{"reed", "read"},
		{"wrte", "write"},
	}
	for _, tc := range cases {
		got := r.suggestToolNames(tc.typo, 3, dynamicMaxDistance(tc.typo))
		if len(got) == 0 || got[0] != tc.want {
			t.Errorf("suggestToolNames(%q) = %v, want first=%q", tc.typo, got, tc.want)
		}
	}
}

func TestSuggestToolNames_UnrelatedReturnsNil(t *testing.T) {
	r := newTestRegistryWithNames("read", "write", "grep")
	got := r.suggestToolNames("zzzzz", 3, dynamicMaxDistance("zzzzz"))
	if len(got) != 0 {
		t.Errorf("suggestToolNames(unrelated) = %v, want empty", got)
	}
}

func TestSuggestToolNames_EmptyRegistry(t *testing.T) {
	r := NewToolRegistry()
	got := r.suggestToolNames("read", 3, 2)
	if len(got) != 0 {
		t.Errorf("suggestToolNames on empty registry = %v, want empty", got)
	}
}

func TestUnknownToolError_IncludesSuggestion(t *testing.T) {
	r := newTestRegistryWithNames("read", "write", "grep", "find")
	err := r.unknownToolError("grpe")
	if err == nil {
		t.Fatal("unknownToolError returned nil")
	}
	msg := err.Error()
	if !strings.Contains(msg, "unknown tool") {
		t.Errorf("error missing 'unknown tool' prefix: %q", msg)
	}
	if !strings.Contains(msg, "grep") {
		t.Errorf("error missing 'grep' suggestion: %q", msg)
	}
}

func TestUnknownToolError_NoSuggestionForGarbage(t *testing.T) {
	r := newTestRegistryWithNames("read", "write", "grep", "find")
	err := r.unknownToolError("qqqqqqq")
	if err == nil {
		t.Fatal("unknownToolError returned nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "Did you mean") {
		t.Errorf("unexpected suggestion for garbage input: %q", msg)
	}
}

func TestDynamicMaxDistance(t *testing.T) {
	cases := []struct {
		name string
		want int
	}{
		{"kv", 1},
		{"rd", 1},
		{"grep", 2},
		{"write", 2},
		{"sessions", 3},
		{"sessions_spawn", 3},
	}
	for _, tc := range cases {
		if got := dynamicMaxDistance(tc.name); got != tc.want {
			t.Errorf("dynamicMaxDistance(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestToolNameEditDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"grep", "grep", 0},
		{"grpe", "grep", 2},
		{"find", "fnd", 1},
		{"read", "", 4},
		{"", "read", 4},
	}
	for _, tc := range cases {
		if got := toolNameEditDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("toolNameEditDistance(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
