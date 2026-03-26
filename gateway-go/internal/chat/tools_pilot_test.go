package chat

import (
	"testing"
)

func TestExpandShortcuts_HTTP(t *testing.T) {
	p := pilotParams{
		Task: "analyze",
		HTTP: "https://api.example.com/data",
	}
	specs := expandShortcuts(p)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Tool != "http" {
		t.Errorf("expected tool 'http', got %q", specs[0].Tool)
	}
}

func TestExpandShortcuts_KVKey(t *testing.T) {
	p := pilotParams{
		Task:  "check",
		KVKey: "config.theme",
	}
	specs := expandShortcuts(p)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Tool != "kv" {
		t.Errorf("expected tool 'kv', got %q", specs[0].Tool)
	}
}

func TestExpandShortcuts_Memory(t *testing.T) {
	p := pilotParams{
		Task:   "summarize",
		Memory: "배포 결정",
	}
	specs := expandShortcuts(p)
	if len(specs) != 1 {
		t.Fatalf("expected 1 spec, got %d", len(specs))
	}
	if specs[0].Tool != "memory_search" {
		t.Errorf("expected tool 'memory_search', got %q", specs[0].Tool)
	}
}

func TestExpandShortcuts_AllNew(t *testing.T) {
	p := pilotParams{
		Task:   "full analysis",
		File:   "main.go",
		HTTP:   "https://example.com",
		KVKey:  "key",
		Memory: "query",
	}
	specs := expandShortcuts(p)
	if len(specs) != 4 {
		t.Fatalf("expected 4 specs, got %d", len(specs))
	}
}

func TestSourceSucceeded(t *testing.T) {
	results := []sourceResult{
		{label: "mem", content: "some results"},
		{label: "err", content: "[tool error: failed]"},
		{label: "skip", content: "[skipped: mem did not succeed]"},
		{label: "empty", content: ""},
	}

	if !sourceSucceeded(results, "mem") {
		t.Error("expected 'mem' to be successful")
	}
	if sourceSucceeded(results, "err") {
		t.Error("expected 'err' to not be successful")
	}
	if sourceSucceeded(results, "skip") {
		t.Error("expected 'skip' to not be successful")
	}
	if sourceSucceeded(results, "empty") {
		t.Error("expected 'empty' to not be successful")
	}
	if sourceSucceeded(results, "nonexistent") {
		t.Error("expected 'nonexistent' to not be successful")
	}
}

func TestApplyPostProcessSteps_FilterLines(t *testing.T) {
	gathered := []sourceResult{
		{label: "data", content: "foo bar\nbaz qux\nfoo baz"},
	}
	steps := []postProcessStep{
		{Action: "filter_lines", Param: "foo"},
	}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "foo bar\nfoo baz" {
		t.Errorf("unexpected filter result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Head(t *testing.T) {
	gathered := []sourceResult{
		{label: "data", content: "line1\nline2\nline3\nline4\nline5"},
	}
	steps := []postProcessStep{
		{Action: "head", Param: "2"},
	}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "line1\nline2\n[... 3 more lines]" {
		t.Errorf("unexpected head result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Tail(t *testing.T) {
	gathered := []sourceResult{
		{label: "data", content: "line1\nline2\nline3\nline4\nline5"},
	}
	steps := []postProcessStep{
		{Action: "tail", Param: "2"},
	}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "[3 lines before ...]\nline4\nline5" {
		t.Errorf("unexpected tail result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Unique(t *testing.T) {
	gathered := []sourceResult{
		{label: "data", content: "a\nb\na\nc\nb"},
	}
	steps := []postProcessStep{
		{Action: "unique"},
	}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "a\nb\nc" {
		t.Errorf("unexpected unique result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Sort(t *testing.T) {
	gathered := []sourceResult{
		{label: "data", content: "cherry\napple\nbanana"},
	}
	steps := []postProcessStep{
		{Action: "sort"},
	}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "apple\nbanana\ncherry" {
		t.Errorf("unexpected sort result: %q", result[0].content)
	}
}

func TestParseLineCount(t *testing.T) {
	cases := []struct {
		input    string
		defN     int
		expected int
	}{
		{"10", 20, 10},
		{"", 20, 20},
		{"abc", 20, 20},
		{"5x", 20, 5},
	}
	for _, tc := range cases {
		got := parseLineCount(tc.input, tc.defN)
		if got != tc.expected {
			t.Errorf("parseLineCount(%q, %d) = %d, want %d", tc.input, tc.defN, got, tc.expected)
		}
	}
}

func TestPilotTimeout(t *testing.T) {
	// 0 sources: base timeout.
	if pilotTimeout(0) != pilotBaseTimeout {
		t.Error("expected base timeout for 0 sources")
	}
	// 10 sources: base + 150s.
	expected := pilotBaseTimeout + 10*pilotPerSourceExtra
	if pilotTimeout(10) != expected {
		t.Errorf("expected %v, got %v", expected, pilotTimeout(10))
	}
}
