package shortid

import (
	"strings"
	"testing"
)

func TestNew_Format(t *testing.T) {
	id := New("run")
	if !strings.HasPrefix(id, "run_") {
		t.Fatalf("expected prefix run_, got %s", id)
	}
	// "run_" (4) + up to 11 base62 chars = max 15
	if len(id) > 15 {
		t.Fatalf("expected len <= 15, got %d (%s)", len(id), id)
	}
	t.Logf("generated id: %s (len=%d)", id, len(id))
}

func TestNew_Uniqueness(t *testing.T) {
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := New("x")
		if _, ok := seen[id]; ok {
			t.Fatalf("duplicate id at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestEncodeBase62(t *testing.T) {
	tests := []struct {
		input int64
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{61, "z"},
		{62, "10"},
		{3844, "100"},
	}
	for _, tt := range tests {
		got := encodeBase62(tt.input)
		if got != tt.want {
			t.Errorf("encodeBase62(%d) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
