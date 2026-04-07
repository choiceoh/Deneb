package chat

import (
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

func TestPartitionTools(t *testing.T) {
	tools := []llm.Tool{
		{Name: "grep"},
		{Name: "mcp_search"},
		{Name: "exec"},
		{Name: "read"},
		{Name: "mcp_fetch"},
		{Name: "edit"},
	}
	builtins := map[string]bool{"grep": true, "exec": true, "read": true, "edit": true}

	p := PartitionTools(tools, builtins)

	// Builtins should be sorted.
	if len(p.Builtin) != 4 {
		t.Fatalf("builtin count = %d, want 4", len(p.Builtin))
	}
	expected := []string{"edit", "exec", "grep", "read"}
	for i, name := range expected {
		if p.Builtin[i].Name != name {
			t.Errorf("builtin[%d] = %q, want %q", i, p.Builtin[i].Name, name)
		}
	}

	// Dynamic should be sorted.
	if len(p.Dynamic) != 2 {
		t.Fatalf("dynamic count = %d, want 2", len(p.Dynamic))
	}
	if p.Dynamic[0].Name != "mcp_fetch" || p.Dynamic[1].Name != "mcp_search" {
		t.Errorf("dynamic order wrong: %v", p.Dynamic)
	}

	// Cache key should be deterministic.
	p2 := PartitionTools(tools, builtins)
	if p.CacheKey != p2.CacheKey {
		t.Error("cache key should be deterministic")
	}
	if p.CacheKey == "" {
		t.Error("cache key should not be empty")
	}
}

func TestMergedTools(t *testing.T) {
	tools := []llm.Tool{
		{Name: "mcp_a"},
		{Name: "exec"},
		{Name: "read"},
	}
	builtins := map[string]bool{"exec": true, "read": true}
	p := PartitionTools(tools, builtins)

	merged := p.MergedTools()
	if len(merged) != 3 {
		t.Fatalf("merged count = %d, want 3", len(merged))
	}
	// Builtins first (sorted), then dynamic.
	if merged[0].Name != "exec" || merged[1].Name != "read" || merged[2].Name != "mcp_a" {
		t.Errorf("merged order: %v", merged)
	}
}

func TestFilterDeniedTools(t *testing.T) {
	tools := []llm.Tool{
		{Name: "read"},
		{Name: "exec"},
		{Name: "dangerous"},
		{Name: "edit"},
	}

	t.Run("empty deny set", func(t *testing.T) {
		result := FilterDeniedTools(tools, nil)
		if len(result) != 4 {
			t.Errorf("expected 4, got %d", len(result))
		}
	})

	t.Run("filters denied tools", func(t *testing.T) {
		deny := map[string]bool{"dangerous": true, "exec": true}
		result := FilterDeniedTools(tools, deny)
		if len(result) != 2 {
			t.Fatalf("expected 2, got %d", len(result))
		}
		for _, r := range result {
			if deny[r.Name] {
				t.Errorf("denied tool %q should be filtered", r.Name)
			}
		}
	})
}
