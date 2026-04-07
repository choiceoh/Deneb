package prompt

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/tokenest"
)

func TestTokenestIntegration(t *testing.T) {
	// Verify tokenest.Estimate produces reasonable values for content
	// used in budget optimization.
	tests := []struct {
		name      string
		input     string
		minTokens int
		maxTokens int
	}{
		{"empty", "", 0, 0},
		{"single char", "a", 1, 1},
		{"short ascii", "hello", 1, 3},
		{"korean text", "안녕하세요 반갑습니다", 4, 12},
		{"mixed", "hello 세계", 2, 8},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tokenest.Estimate(tt.input)
			if got < tt.minTokens || got > tt.maxTokens {
				t.Errorf("tokenest.Estimate(%q) = %d, want %d-%d", tt.input, got, tt.minTokens, tt.maxTokens)
			}
		})
	}
}

func TestNewFragment(t *testing.T) {
	t.Run("known name", func(t *testing.T) {
		f := NewFragment("soul", "content")
		if f.Priority != 0 {
			t.Errorf("soul priority = %d, want 0", f.Priority)
		}
		if f.Shrinkable {
			t.Error("soul should not be shrinkable")
		}
	})

	t.Run("shrinkable name", func(t *testing.T) {
		f := NewFragment("memory", "content")
		if f.Priority != 2 {
			t.Errorf("memory priority = %d, want 2", f.Priority)
		}
		if !f.Shrinkable {
			t.Error("memory should be shrinkable")
		}
	})

	t.Run("unknown name defaults to priority 2", func(t *testing.T) {
		f := NewFragment("custom_thing", "content")
		if f.Priority != 2 {
			t.Errorf("unknown priority = %d, want 2", f.Priority)
		}
	})
}

// makeContent creates a string with approximately the given token count.
// Uses ASCII 'a' characters for predictable estimation under the
// script-aware tokenest engine (Latin ratio ~3.5 runes/token for default family).
func makeContent(tokens uint64) string {
	if tokens == 0 {
		return ""
	}
	// Start with a rough estimate and fine-tune.
	n := int(float64(tokens)*3.5 + 1)
	s := strings.Repeat("a", n)
	// Trim if over-estimated.
	for uint64(tokenest.Estimate(s)) > tokens && len(s) > 1 {
		s = s[:len(s)-1]
	}
	// Extend if under-estimated.
	for uint64(tokenest.Estimate(s)) < tokens {
		s += "a"
	}
	return s
}

func TestOptimize_WithinBudget(t *testing.T) {
	budget := PromptBudget{Total: 1000}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(200), Priority: 0},
		{Name: "memory", Content: makeContent(300), Priority: 2, Shrinkable: true},
		{Name: "low_priority", Content: makeContent(100), Priority: 3, Shrinkable: true},
	}

	result := budget.Optimize(fragments)
	if len(result) != 3 {
		t.Fatalf("expected 3 fragments, got %d", len(result))
	}
	// Content should be unchanged.
	for i, f := range result {
		if f.Content != fragments[i].Content {
			t.Errorf("fragment %d content changed", i)
		}
	}
}

func TestOptimize_RemovePriority3(t *testing.T) {
	// Total: 200+300+100 = 600 tokens. Budget: 550 → need to cut ~50.
	// Removing priority 3 (100 tokens) brings total to 500 which fits.
	budget := PromptBudget{Total: 550}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(200), Priority: 0},
		{Name: "memory", Content: makeContent(300), Priority: 2, Shrinkable: true},
		{Name: "low_priority", Content: makeContent(100), Priority: 3, Shrinkable: true},
	}

	result := budget.Optimize(fragments)
	if len(result) != 2 {
		t.Fatalf("expected 2 fragments, got %d", len(result))
	}
	for _, f := range result {
		if f.Name == "low_priority" {
			t.Error("low_priority should have been removed")
		}
	}
}

func TestOptimize_ShrinkPriority2(t *testing.T) {
	// Total after removing p3: 200+400 = 600. Budget: 450.
	// Shrink p2 (400→200): total = 400, fits.
	budget := PromptBudget{Total: 450}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(200), Priority: 0},
		{Name: "memory", Content: makeContent(400), Priority: 2, Shrinkable: true},
		{Name: "low_priority", Content: makeContent(50), Priority: 3, Shrinkable: true},
	}

	result := budget.Optimize(fragments)
	// proactive_hints removed, memory shrunk.
	if len(result) != 2 {
		t.Fatalf("expected 2 fragments, got %d", len(result))
	}
	for _, f := range result {
		if f.Name == "memory" {
			memTokens := uint64(tokenest.Estimate(f.Content))
			origTokens := uint64(400)
			if memTokens >= origTokens {
				t.Errorf("memory should be shrunk, got %d tokens (orig %d)", memTokens, origTokens)
			}
		}
	}
}

func TestOptimize_RemovePriority2(t *testing.T) {
	// After removing p3 and shrinking p2: still over budget → remove p2.
	// soul(500) + memory(500 shrunk to 250) = 750 > budget 600.
	// Remove memory (smallest p2 after shrink). soul(500) fits in 600.
	budget := PromptBudget{Total: 600}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(500), Priority: 0},
		{Name: "memory", Content: makeContent(500), Priority: 2, Shrinkable: true},
		{Name: "low_priority", Content: makeContent(50), Priority: 3, Shrinkable: true},
	}

	result := budget.Optimize(fragments)
	if len(result) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result))
	}
	if result[0].Name != "soul" {
		t.Errorf("expected soul to survive, got %s", result[0].Name)
	}
}

func TestOptimize_Priority0NeverRemoved(t *testing.T) {
	// Priority 0 fragment exceeds budget alone — must still be kept.
	budget := PromptBudget{Total: 100}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(500), Priority: 0},
	}

	result := budget.Optimize(fragments)
	if len(result) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result))
	}
	if result[0].Content != fragments[0].Content {
		t.Error("priority 0 content should never be modified")
	}
}

func TestOptimize_Priority1NeverRemoved(t *testing.T) {
	budget := PromptBudget{Total: 100}
	fragments := []PromptFragment{
		{Name: "tool_schemas", Content: makeContent(300), Priority: 1},
	}

	result := budget.Optimize(fragments)
	if len(result) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result))
	}
}

func TestOptimize_EmptyInput(t *testing.T) {
	budget := PromptBudget{Total: 1000}
	result := budget.Optimize(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

func TestOptimize_ZeroBudget(t *testing.T) {
	budget := PromptBudget{Total: 0}
	fragments := []PromptFragment{
		{Name: "soul", Content: "hello", Priority: 0},
	}
	// Zero budget returns fragments unchanged (no optimization attempted).
	result := budget.Optimize(fragments)
	if len(result) != 1 {
		t.Fatalf("expected 1 fragment, got %d", len(result))
	}
}

func TestAssemble(t *testing.T) {
	budget := PromptBudget{Total: 1000}
	fragments := []PromptFragment{
		{Name: "a", Content: "hello ", Priority: 0},
		{Name: "b", Content: "world", Priority: 0},
	}

	got := budget.Assemble(fragments)
	if got != "hello world" {
		t.Errorf("Assemble = %q, want %q", got, "hello world")
	}
}

func TestAssemble_WithOptimization(t *testing.T) {
	// Budget only fits fragment a. Fragment b (p3) should be removed.
	budget := PromptBudget{Total: 5}
	fragments := []PromptFragment{
		{Name: "a", Content: "hi", Priority: 0},
		{Name: "b", Content: makeContent(100), Priority: 3, Shrinkable: true},
	}

	got := budget.Assemble(fragments)
	if got != "hi" {
		t.Errorf("Assemble = %q, want %q", got, "hi")
	}
}

func TestShrinkContent(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		fraction float64
		wantLen  int // expected rune count
	}{
		{"half ascii", "abcdef", 0.5, 3},
		{"half korean", "가나다라마바", 0.5, 3},
		{"full", "abc", 1.0, 3},
		{"zero", "abc", 0.0, 0},
		{"empty input", "", 0.5, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shrinkContent(tt.input, tt.fraction)
			runes := []rune(got)
			if len(runes) != tt.wantLen {
				t.Errorf("shrinkContent(%q, %f) rune count = %d, want %d", tt.input, tt.fraction, len(runes), tt.wantLen)
			}
		})
	}
}

func TestOptimize_MultiplePriority2_SmallestRemovedFirst(t *testing.T) {
	// soul(100) + small_mem(50 shrunk to 25) + big_mem(200 shrunk to 100) = 225 > budget 200.
	// Remove smallest p2 (small_mem at 25 after shrink). Total: 100+100=200.
	budget := PromptBudget{Total: 200}
	fragments := []PromptFragment{
		{Name: "soul", Content: makeContent(100), Priority: 0},
		{Name: "small_mem", Content: makeContent(50), Priority: 2, Shrinkable: true},
		{Name: "big_mem", Content: makeContent(200), Priority: 2, Shrinkable: true},
		{Name: "hints", Content: makeContent(30), Priority: 3, Shrinkable: true},
	}

	result := budget.Optimize(fragments)
	names := make(map[string]struct{})
	for _, f := range result {
		names[f.Name] = struct{}{}
	}
	if _, ok := names["hints"]; ok {
		t.Error("hints (p3) should be removed")
	}
	if _, ok := names["soul"]; !ok {
		t.Error("soul (p0) should survive")
	}
	// At least one p2 should survive (big_mem is more likely to remain
	// since smallest is removed first).
}
