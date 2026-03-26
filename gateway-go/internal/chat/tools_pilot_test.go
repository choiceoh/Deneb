package chat

import (
	"testing"
)

func TestShouldUseThinking(t *testing.T) {
	tests := []struct {
		task        string
		sourceCount int
		want        bool
	}{
		{"리뷰해줘", 1, true},             // Korean keyword "리뷰"
		{"분석해봐", 0, true},              // Korean keyword "분석"
		{"analyze this code", 0, true},    // English keyword
		{"compare these files", 0, true},  // English keyword
		{"debug this issue", 1, true},     // English keyword
		{"요약해줘", 1, false},              // No complex keyword
		{"read this file", 1, false},      // Simple task
		{"list files", 3, true},           // 3+ sources triggers thinking
		{"hello", 5, true},                // Many sources
	}

	for _, tt := range tests {
		got := shouldUseThinking(tt.task, tt.sourceCount)
		if got != tt.want {
			t.Errorf("shouldUseThinking(%q, %d) = %v, want %v", tt.task, tt.sourceCount, got, tt.want)
		}
	}
}

// stripThinkingTags is tested in web_fetch_test.go (shared function).

func TestCleanJSONResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"valid json", `{"key": "value"}`, `{"key": "value"}`},
		{"with json fence", "```json\n{\"key\": \"value\"}\n```", `{"key": "value"}`},
		{"with plain fence", "```\n[1, 2, 3]\n```", `[1, 2, 3]`},
		{"with prefix text", "Here is the result: {\"key\": \"value\"}", `{"key": "value"}`},
		{"array with prefix", "Result:\n[1, 2, 3]", `[1, 2, 3]`},
		{"not json at all", "just plain text", "just plain text"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanJSONResponse(tt.input)
			if got != tt.want {
				t.Errorf("cleanJSONResponse() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSmartTruncate(t *testing.T) {
	longContent := make([]byte, 5000)
	for i := range longContent {
		longContent[i] = 'a'
	}
	long := string(longContent)

	// Short content — no truncation.
	short := "hello world"
	if got := smartTruncate(short, 100, "file"); got != short {
		t.Errorf("short content should not be truncated")
	}

	// File truncation: preserves head + tail.
	result := smartTruncate(long, 2000, "file")
	if len(result) > 2200 { // some overhead from marker
		t.Errorf("file truncation too long: %d", len(result))
	}
	// Should contain marker.
	if !contains(result, "truncated") {
		t.Errorf("file truncation should contain truncation marker")
	}

	// Exec truncation: preserves tail.
	result = smartTruncate(long, 2000, "exec")
	if !contains(result, "truncated") {
		t.Errorf("exec truncation should contain truncation marker")
	}

	// Default truncation: head only.
	result = smartTruncate(long, 2000, "content")
	if !contains(result, "truncated at") {
		t.Errorf("default truncation should contain 'truncated at' marker")
	}
}

func TestExpandShortcuts(t *testing.T) {
	p := pilotParams{
		Task: "test",
		File: "main.go",
		Exec: "ls -la",
		Grep: "TODO",
		Path: "src/",
		Find: "*.go",
		URL:  "https://example.com",
	}

	specs := expandShortcuts(p)

	// Should have 5 sources: file, exec, grep, find, url.
	if len(specs) != 5 {
		t.Fatalf("expected 5 specs, got %d", len(specs))
	}

	// Verify tool names.
	tools := make([]string, len(specs))
	for i, s := range specs {
		tools[i] = s.Tool
	}

	expected := []string{"read", "exec", "grep", "find", "web_fetch"}
	for i, want := range expected {
		if tools[i] != want {
			t.Errorf("spec[%d].Tool = %q, want %q", i, tools[i], want)
		}
	}
}

func TestExpandShortcutsMultipleFiles(t *testing.T) {
	p := pilotParams{
		Task:  "test",
		Files: []string{"a.go", "b.go", "c.go"},
	}

	specs := expandShortcuts(p)
	if len(specs) != 3 {
		t.Fatalf("expected 3 specs, got %d", len(specs))
	}
	for _, s := range specs {
		if s.Tool != "read" {
			t.Errorf("expected tool 'read', got %q", s.Tool)
		}
	}
}

func TestBuildPilotPrompt(t *testing.T) {
	blocks := []sourceResult{
		{"main.go", "package main\nfunc main() {}", "file"},
		{"$ ls", "file1.go\nfile2.go", "exec"},
	}

	result := buildPilotPrompt("리뷰해줘", "json", "brief", blocks)

	if !contains(result, "Task: 리뷰해줘") {
		t.Error("prompt should contain task")
	}
	if !contains(result, "Output format: json") {
		t.Error("prompt should contain output format")
	}
	if !contains(result, "under 500 characters") {
		t.Error("prompt should contain brief length hint")
	}
	if !contains(result, "--- main.go ---") {
		t.Error("prompt should contain source labels")
	}
}

func TestBuildPilotPromptNoBlocks(t *testing.T) {
	result := buildPilotPrompt("just a question", "", "", nil)
	if result != "Task: just a question" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestBuildFallbackResult(t *testing.T) {
	gathered := []sourceResult{
		{"main.go", "package main", "file"},
	}

	result := buildFallbackResult("리뷰해줘", gathered)
	if !contains(result, "sglang") {
		t.Error("fallback should mention sglang")
	}
	if !contains(result, "Task: 리뷰해줘") {
		t.Error("fallback should contain task")
	}
	if !contains(result, "package main") {
		t.Error("fallback should contain source content")
	}
}

func TestBuildPilotSystemPrompt(t *testing.T) {
	prompt := buildPilotSystemPrompt("/workspace", true)
	if !contains(prompt, "Workspace directory: /workspace") {
		t.Error("should contain workspace dir")
	}
	if !contains(prompt, "<think>") {
		t.Error("should contain thinking instruction when thinking=true")
	}

	prompt = buildPilotSystemPrompt("", false)
	if contains(prompt, "Workspace") {
		t.Error("should not contain workspace when empty")
	}
	if contains(prompt, "<think>") {
		t.Error("should not contain thinking instruction when thinking=false")
	}
}

func TestSourceTypeFromTool(t *testing.T) {
	tests := map[string]string{
		"read":      "file",
		"exec":      "exec",
		"grep":      "grep",
		"find":      "find",
		"web_fetch": "url",
		"unknown":   "content",
	}

	for tool, want := range tests {
		if got := sourceTypeFromTool(tool); got != want {
			t.Errorf("sourceTypeFromTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestTruncateHead(t *testing.T) {
	short := "hello"
	if got := truncateHead(short, 100); got != short {
		t.Errorf("short string should not be truncated")
	}

	long := string(make([]byte, 500))
	result := truncateHead(long, 100)
	if len(result) > 200 {
		t.Errorf("truncated result too long: %d", len(result))
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
