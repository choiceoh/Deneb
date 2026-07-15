package secret

import (
	"encoding/json"
	"testing"
)

func TestResolve_ReturnsAssignmentsAndMissingRefs(t *testing.T) {
	r := NewResolver()
	r.Set("openai.apiKey", json.RawMessage(`"sk-123"`))
	r.Set("openai.orgId", json.RawMessage(`"org-456"`))

	result := r.Resolve("openai", []string{"apiKey", "orgId", "missing"})
	if !result.OK {
		t.Fatal("expected OK")
	}
	if len(result.Assignments) != 2 {
		t.Fatalf("got %d, want 2 assignments", len(result.Assignments))
	}
	if len(result.InactiveRefPaths) != 1 {
		t.Fatalf("got %d, want 1 inactive", len(result.InactiveRefPaths))
	}
	if result.InactiveRefPaths[0] != "openai.missing" {
		t.Fatalf("got %q, want 'openai.missing'", result.InactiveRefPaths[0])
	}
}

func TestReload(t *testing.T) {
	r := NewResolver()
	result := r.Reload()
	if !result.OK {
		t.Fatal("expected OK")
	}
}
