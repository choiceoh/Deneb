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

func TestIsLocalURL(t *testing.T) {
	local := []string{
		"http://127.0.0.1:8000/v1",
		"http://localhost:30000/v1",
		"http://0.0.0.0:8000",
		"http://[::1]:8000/v1",
	}
	for _, u := range local {
		if !isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = false, want true", u)
		}
	}
	remote := []string{
		"https://api.z.ai/api/anthropic",
		"https://openrouter.ai/api/v1",
		"",
	}
	for _, u := range remote {
		if isLocalURL(u) {
			t.Errorf("isLocalURL(%q) = true, want false", u)
		}
	}
}

func TestMergeModels(t *testing.T) {
	got := mergeModels([]string{"a", "b"}, []string{"b", "c", "  "})
	want := []string{"a", "b", "c"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("mergeModels = %v, want %v", got, want)
	}
}

func TestProviderEntries(t *testing.T) {
	entries := providerEntries(providerSpec{
		name:   "zai",
		models: []string{"glm-5-turbo", "anthropic/claude-x"},
	})
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if entries[0].fullID != "zai/glm-5-turbo" || entries[0].display != "glm-5-turbo" {
		t.Errorf("unexpected first entry: %+v", entries[0])
	}
	// shortModelName strips the inner provider prefix for the label.
	if entries[1].fullID != "zai/anthropic/claude-x" || entries[1].display != "claude-x" {
		t.Errorf("unexpected second entry: %+v", entries[1])
	}
}

func TestAssembleModelSections_OrderAndDedup(t *testing.T) {
	roles := []modelEntry{
		{provider: "zai", label: "main: glm-5-turbo", fullID: "zai/glm-5-turbo", display: "glm-5-turbo"},
	}
	// Providers arrive pre-sorted (loadConfiguredProviders sorts by name).
	providers := []providerSpec{
		{name: "vllm", models: []string{"gemma4", "qwen3-32b"}},
		{name: "zai", models: []string{"glm-5-turbo", "glm-5.1"}},
	}

	sections := assembleModelSections(roles, providers)

	wantTitles := []string{"역할", "vLLM", "Z.ai"}
	if len(sections) != len(wantTitles) {
		t.Fatalf("expected %d sections, got %d", len(wantTitles), len(sections))
	}
	for i, want := range wantTitles {
		if sections[i].title != want {
			t.Errorf("section %d title = %q, want %q", i, sections[i].title, want)
		}
	}

	// glm-5-turbo appears as a role → must not repeat in the Z.ai section.
	zai := sections[2]
	if len(zai.entries) != 1 || zai.entries[0].fullID != "zai/glm-5.1" {
		t.Errorf("Z.ai section after dedup = %+v, want only zai/glm-5.1", zai.entries)
	}
}

func TestAssembleModelSections_EmptyProviderOmitted(t *testing.T) {
	providers := []providerSpec{
		{name: "vllm", models: []string{"gemma4"}},
		{name: "empty", models: nil},
	}
	sections := assembleModelSections(nil, providers)
	for _, s := range sections {
		if s.title == "empty" {
			t.Error("a provider with no models should not produce a section")
		}
	}
	if len(sections) != 1 || sections[0].title != "vLLM" {
		t.Errorf("expected only the vLLM section, got %+v", sections)
	}
}

func TestAssembleModelSections_DropsOversizedCallback(t *testing.T) {
	huge := strings.Repeat("y", 70)
	providers := []providerSpec{
		{name: "zai", models: []string{"glm-5-turbo", huge}},
	}
	sections := assembleModelSections(nil, providers)
	if len(sections) != 1 || sections[0].title != "Z.ai" {
		t.Fatalf("expected a Z.ai section, got %+v", sections)
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

func TestAppendBuiltinProviders(t *testing.T) {
	// From an empty config, every built-in provider is offered — including
	// zai, which must never silently disappear from the keyboard.
	got := appendBuiltinProviders(nil)
	names := make(map[string]int)
	for _, pv := range got {
		names[pv.name]++
	}
	for _, want := range []string{"zai", "openrouter", "vllm", "localai", "kimi", "mimo-plan"} {
		if names[want] != 1 {
			t.Errorf("builtin provider %q appears %d times, want 1", want, names[want])
		}
	}

	// The merged list is sorted by name for a stable keyboard layout.
	for i := 1; i < len(got); i++ {
		if got[i-1].name > got[i].name {
			t.Errorf("providers not sorted: %q before %q", got[i-1].name, got[i].name)
		}
	}

	// A provider the operator already declared is not duplicated, and its
	// explicit config is preserved (the built-in does not overwrite it).
	configured := []providerSpec{{name: "zai", models: []string{"custom-model"}}}
	got = appendBuiltinProviders(configured)
	zaiCount := 0
	for _, pv := range got {
		if pv.name == "zai" {
			zaiCount++
			if len(pv.models) != 1 || pv.models[0] != "custom-model" {
				t.Errorf("configured zai spec was overwritten: %+v", pv)
			}
		}
	}
	if zaiCount != 1 {
		t.Errorf("zai appears %d times after merge, want 1 (no duplicate)", zaiCount)
	}
}
