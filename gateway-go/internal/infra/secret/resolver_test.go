package secret

import (
	"testing"
)

func TestResolve(t *testing.T) {
	r := NewResolver()
	r.Set("openai.apiKey", "sk-123")
	r.Set("openai.orgId", "org-456")

	result := r.Resolve("openai", []string{"apiKey", "orgId", "missing"})
	if !result.OK {
		t.Fatal("expected OK")
	}
	if len(result.Assignments) != 2 {
		t.Fatalf("expected 2 assignments, got %d", len(result.Assignments))
	}
	if len(result.InactiveRefPaths) != 1 {
		t.Fatalf("expected 1 inactive, got %d", len(result.InactiveRefPaths))
	}
	if result.InactiveRefPaths[0] != "openai.missing" {
		t.Fatalf("expected 'openai.missing', got %q", result.InactiveRefPaths[0])
	}
}

func TestReload(t *testing.T) {
	r := NewResolver()
	result := r.Reload()
	if !result.OK {
		t.Fatal("expected OK")
	}
}

func TestResolveEmpty(t *testing.T) {
	r := NewResolver()
	result := r.Resolve("cmd", []string{})
	if !result.OK {
		t.Fatal("expected OK")
	}
	if len(result.Assignments) != 0 {
		t.Fatal("expected 0 assignments")
	}
}
