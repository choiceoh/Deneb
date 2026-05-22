package server

import (
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

func TestShortModelName(t *testing.T) {
	cases := map[string]string{
		"zai/glm-5-turbo":                      "glm-5-turbo",
		"openrouter/anthropic/claude-opus-4.7": "claude-opus-4.7",
		"gemma4":                               "gemma4",
		"trailing/":                            "trailing/",
	}
	for in, want := range cases {
		if got := shortModelName(in); got != want {
			t.Errorf("shortModelName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCallbackFits(t *testing.T) {
	if !callbackFits("zai/glm-5-turbo") {
		t.Error("short model ID should fit in callback budget")
	}
	// 6 bytes for "model:" prefix + this 59-byte ID = 65 > 64.
	tooLong := "openrouter/" + strings.Repeat("x", 48)
	if callbackFits(tooLong) {
		t.Errorf("oversized model ID (%d bytes) should not fit", len(tooLong))
	}
}

func TestCuratedEntries(t *testing.T) {
	entries := curatedEntries("zai", []string{"glm-5-turbo", "  ", "glm-5.1"})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (blank dropped), got %d", len(entries))
	}
	if entries[0].fullID != "zai/glm-5-turbo" || entries[0].display != "glm-5-turbo" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	if entries[1].fullID != "zai/glm-5.1" {
		t.Errorf("unexpected second entry fullID: %q", entries[1].fullID)
	}
}

func TestAssembleModelSections_OrderAndDedup(t *testing.T) {
	roles := []modelEntry{
		{provider: "zai", label: "main: glm-5-turbo", fullID: "zai/glm-5-turbo", display: "glm-5-turbo"},
		{provider: "vllm", label: "lightweight: gemma4", fullID: "vllm/gemma4", display: "gemma4"},
	}
	discovered := map[string][]string{
		"vllm":    {"gemma4", "qwen3-32b"},
		"localai": {"phi-5"},
	}

	sections := assembleModelSections(roles, discovered, true)

	wantTitles := []string{"역할", "Z.ai", "로컬 vLLM", "로컬 AI", "OpenRouter"}
	if len(sections) != len(wantTitles) {
		t.Fatalf("expected %d sections, got %d", len(wantTitles), len(sections))
	}
	for i, want := range wantTitles {
		if sections[i].title != want {
			t.Errorf("section %d title = %q, want %q", i, sections[i].title, want)
		}
	}

	// glm-5-turbo appears as a role → must not repeat in the Z.ai section.
	for _, e := range sections[1].entries {
		if e.fullID == "zai/glm-5-turbo" {
			t.Error("zai/glm-5-turbo duplicated in Z.ai section despite role entry")
		}
	}
	// gemma4 appears as a role → vLLM section should only carry the rest.
	if len(sections[2].entries) != 1 || sections[2].entries[0].fullID != "vllm/qwen3-32b" {
		t.Errorf("vLLM section after dedup = %+v, want only vllm/qwen3-32b", sections[2].entries)
	}
}

func TestAssembleModelSections_OpenRouterGated(t *testing.T) {
	roles := []modelEntry{
		{provider: "zai", label: "main", fullID: "zai/glm-5-turbo", display: "glm-5-turbo"},
	}
	sections := assembleModelSections(roles, nil, false)
	for _, s := range sections {
		if s.title == "OpenRouter" {
			t.Error("OpenRouter section should be omitted when key is absent")
		}
	}
	// No local discovery → no local sections.
	for _, s := range sections {
		if s.title == "로컬 vLLM" || s.title == "로컬 AI" {
			t.Errorf("local section %q present without discovery results", s.title)
		}
	}
}

func TestAssembleModelSections_DropsOversizedCallback(t *testing.T) {
	huge := strings.Repeat("y", 70)
	roles := []modelEntry{
		{provider: "zai", label: "ok", fullID: "zai/glm-5-turbo", display: "glm-5-turbo"},
		{provider: "zai", label: "huge", fullID: "zai/" + huge, display: huge},
	}
	sections := assembleModelSections(roles, nil, false)
	if len(sections) == 0 || sections[0].title != "역할" {
		t.Fatal("expected a 역할 section")
	}
	if len(sections[0].entries) != 1 {
		t.Errorf("oversized callback entry should be dropped, got %d entries", len(sections[0].entries))
	}
}

func TestBuildModelKeyboard(t *testing.T) {
	sections := []modelSection{
		{title: "역할", entries: []modelEntry{
			{label: "main: a", fullID: "zai/a"},
			{label: "lightweight: b", fullID: "vllm/b"},
			{label: "fallback: c", fullID: "vllm/c"},
		}},
	}
	kb := buildModelKeyboard(sections, "vllm/b")
	if kb == nil {
		t.Fatal("expected a keyboard")
	}
	// Row 0: header. Row 1: a + b (2-col). Row 2: c.
	if len(kb.InlineKeyboard) != 3 {
		t.Fatalf("expected 3 rows (header + 2 model rows), got %d", len(kb.InlineKeyboard))
	}

	header := kb.InlineKeyboard[0]
	if len(header) != 1 || header[0].CallbackData != telegram.ActionNoop+":" {
		t.Errorf("row 0 should be a noop header, got %+v", header)
	}
	if !strings.Contains(header[0].Text, "역할") {
		t.Errorf("header text missing section title: %q", header[0].Text)
	}

	first := kb.InlineKeyboard[1]
	if len(first) != 2 {
		t.Fatalf("expected 2 buttons in first model row, got %d", len(first))
	}
	if first[0].CallbackData != telegram.ActionModelSwitch+":zai/a" {
		t.Errorf("unexpected callback data: %q", first[0].CallbackData)
	}
	// vllm/b is the current model → its label carries the checkmark.
	if !strings.HasPrefix(first[1].Text, "✓ ") {
		t.Errorf("current model button should be checkmarked, got %q", first[1].Text)
	}
	if strings.HasPrefix(first[0].Text, "✓ ") {
		t.Errorf("non-current model button should not be checkmarked, got %q", first[0].Text)
	}
}

func TestBuildModelKeyboard_EmptyReturnsNil(t *testing.T) {
	if kb := buildModelKeyboard(nil, ""); kb != nil {
		t.Error("empty sections should yield a nil keyboard")
	}
}
