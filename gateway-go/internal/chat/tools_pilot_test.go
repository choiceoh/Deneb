package chat

import (
	"strings"
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

func TestNormalizeMarkdown(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"unclosed code block",
			"```go\nfunc main() {\n  fmt.Println(\"hello\")",
			"```go\nfunc main() {\n  fmt.Println(\"hello\")\n```",
		},
		{
			"excessive blank lines",
			"line1\n\n\n\n\nline2",
			"line1\n\n\nline2",
		},
		{
			"trailing whitespace",
			"hello   \nworld  ",
			"hello\nworld",
		},
		{
			"already clean",
			"line1\nline2\n```\ncode\n```",
			"line1\nline2\n```\ncode\n```",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMarkdown(tt.input)
			if got != tt.want {
				t.Errorf("normalizeMarkdown() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestCleanListResponse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			"numbered list",
			"1. First item\n2. Second item\n3. Third item",
			"1. First item\n2. Second item\n3. Third item",
		},
		{
			"bullet list to numbered",
			"- First\n- Second\n- Third",
			"1. First\n2. Second\n3. Third",
		},
		{
			"mixed with preamble",
			"Here are the results:\n\n1. Alpha\n2. Beta",
			"Here are the results:\n\n1. Alpha\n2. Beta",
		},
		{
			"no list at all",
			"Just plain text with no list items.",
			"Just plain text with no list items.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanListResponse(tt.input)
			if got != tt.want {
				t.Errorf("cleanListResponse() =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestEnforceMaxLength(t *testing.T) {
	short := "hello world"
	if got := enforceMaxLength(short, 100); got != short {
		t.Error("short string should not be truncated")
	}

	// Line boundary cut.
	multiline := "Line one.\nLine two.\nLine three.\nLine four.\nLine five."
	result := enforceMaxLength(multiline, 30)
	if len(result) > 35 { // some overhead for ellipsis
		t.Errorf("enforceMaxLength too long: %d chars", len(result))
	}
	if !contains(result, "…") {
		t.Error("should contain ellipsis")
	}

	// Sentence boundary cut.
	prose := "This is the first sentence. This is the second sentence. This is very long text that keeps going."
	result = enforceMaxLength(prose, 60)
	if !contains(result, "…") {
		t.Error("should contain ellipsis")
	}
}

func TestPostProcessOutput(t *testing.T) {
	// Brief mode enforces length.
	long := strings.Repeat("가나다라 ", 200) // ~1000 chars Korean
	result := postProcessOutput(long, "text", "brief")
	if len(result) > briefMaxChars+10 {
		t.Errorf("brief mode should enforce length, got %d chars", len(result))
	}

	// JSON mode cleans fences.
	jsonWithFence := "```json\n{\"key\": \"value\"}\n```"
	result = postProcessOutput(jsonWithFence, "json", "")
	if result != `{"key": "value"}` {
		t.Errorf("json mode should strip fences, got %q", result)
	}

	// List mode normalizes.
	bullets := "- Alpha\n- Beta\n- Gamma"
	result = postProcessOutput(bullets, "list", "")
	if !contains(result, "1. Alpha") {
		t.Errorf("list mode should normalize bullets, got %q", result)
	}
}

func TestIsListItem(t *testing.T) {
	positives := []string{"1. item", "2. item", "10. item", "- item", "* item"}
	for _, s := range positives {
		if !isListItem(s) {
			t.Errorf("expected %q to be a list item", s)
		}
	}

	negatives := []string{"hello", "1x item", ".item", ""}
	for _, s := range negatives {
		if isListItem(s) {
			t.Errorf("expected %q to NOT be a list item", s)
		}
	}
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
