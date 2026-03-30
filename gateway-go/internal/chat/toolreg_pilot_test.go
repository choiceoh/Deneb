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
		{"리뷰해줘", 1, true},                // Korean keyword "리뷰"
		{"분석해봐", 0, true},                // Korean keyword "분석"
		{"analyze this code", 0, true},   // English keyword
		{"compare these files", 0, true}, // English keyword
		{"debug this issue", 1, true},    // English keyword
		{"요약해줘", 1, false},               // No complex keyword
		{"read this file", 1, false},     // Simple task
		{"list files", 3, true},          // 3+ sources triggers thinking
		{"hello", 5, true},               // Many sources
	}

	for _, tt := range tests {
		got := shouldUseThinking(tt.task, tt.sourceCount)
		if got != tt.want {
			t.Errorf("shouldUseThinking(%q, %d) = %v, want %v", tt.task, tt.sourceCount, got, tt.want)
		}
	}
}

// StripThinkingTags is now tested in pkg/jsonutil/extract_test.go.

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

// --- Tests for new integration features ---

func TestExpandShortcuts_HTTP(t *testing.T) {
	p := pilotParams{Task: "analyze", HTTP: "https://api.example.com/data"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "http" {
		t.Errorf("expected 1 http spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_KVKey(t *testing.T) {
	p := pilotParams{Task: "check", KVKey: "config.theme"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "kv" {
		t.Errorf("expected 1 kv spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Memory(t *testing.T) {
	p := pilotParams{Task: "summarize", Memory: "배포 결정"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "memory_search" {
		t.Errorf("expected 1 memory_search spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Gmail(t *testing.T) {
	p := pilotParams{Task: "summarize", Gmail: "from:alice subject:회의"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "gmail" {
		t.Errorf("expected 1 gmail spec, got %d", len(specs))
	}
	if specs[0].Label != "gmail: from:alice subject:회의" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}
}

func TestExpandShortcuts_YouTube(t *testing.T) {
	p := pilotParams{Task: "summarize", YouTube: "https://youtube.com/watch?v=abc123"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "youtube_transcript" {
		t.Errorf("expected 1 youtube_transcript spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Polaris(t *testing.T) {
	p := pilotParams{Task: "explain", Polaris: "aurora context engine"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "polaris" {
		t.Errorf("expected 1 polaris spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Image(t *testing.T) {
	p := pilotParams{Task: "describe", Image: "/tmp/screenshot.png"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "image" {
		t.Errorf("expected 1 image spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_Vega(t *testing.T) {
	p := pilotParams{Task: "search", Vega: "비금도 진행상황"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "vega" {
		t.Errorf("expected 1 vega spec, got %d", len(specs))
	}
}

func TestExpandShortcuts_AllNew(t *testing.T) {
	p := pilotParams{
		Task:    "analyze everything",
		Gmail:   "invoice",
		YouTube: "https://youtube.com/watch?v=x",
		Polaris: "tools",
		Image:   "/tmp/img.png",
		Vega:    "프로젝트 현황",
	}
	specs := expandShortcuts(p)
	if len(specs) != 5 {
		t.Fatalf("expected 5 specs, got %d", len(specs))
	}
	tools := make([]string, len(specs))
	for i, s := range specs {
		tools[i] = s.Tool
	}
	expected := []string{"gmail", "youtube_transcript", "polaris", "image", "vega"}
	for i, want := range expected {
		if tools[i] != want {
			t.Errorf("spec[%d].Tool = %q, want %q", i, tools[i], want)
		}
	}
}

func TestSourceTypeFromTool_NewTools(t *testing.T) {
	tests := map[string]string{
		"gmail":              "content",
		"youtube_transcript": "content",
		"polaris":            "content",
		"image":              "content",
		"vega":               "content",
		"diff":               "file",
		"test":               "exec",
		"tree":               "file",
		"http":               "exec",
	}
	for tool, want := range tests {
		if got := sourceTypeFromTool(tool); got != want {
			t.Errorf("sourceTypeFromTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestSourceSucceeded(t *testing.T) {
	results := []sourceResult{
		{label: "mem", content: "some results", sourceType: "content"},
		{label: "err", content: "[tool error: failed]", sourceType: "content"},
		{label: "skip", content: "[skipped: mem did not succeed]", sourceType: "content"},
		{label: "empty", content: "", sourceType: "content"},
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
}

func TestApplyPostProcessSteps_FilterLines(t *testing.T) {
	gathered := []sourceResult{{label: "data", content: "foo bar\nbaz qux\nfoo baz", sourceType: "content"}}
	steps := []postProcessStep{{Action: "filter_lines", Param: "foo"}}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "foo bar\nfoo baz" {
		t.Errorf("unexpected filter result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Unique(t *testing.T) {
	gathered := []sourceResult{{label: "data", content: "a\nb\na\nc\nb", sourceType: "content"}}
	steps := []postProcessStep{{Action: "unique"}}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "a\nb\nc" {
		t.Errorf("unexpected unique result: %q", result[0].content)
	}
}

func TestApplyPostProcessSteps_Sort(t *testing.T) {
	gathered := []sourceResult{{label: "data", content: "cherry\napple\nbanana", sourceType: "content"}}
	steps := []postProcessStep{{Action: "sort"}}
	result := applyPostProcessSteps(gathered, steps)
	if result[0].content != "apple\nbanana\ncherry" {
		t.Errorf("unexpected sort result: %q", result[0].content)
	}
}

func TestExpandShortcuts_Diff(t *testing.T) {
	// "all" mode.
	p := pilotParams{Task: "review", Diff: "all"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "diff" {
		t.Fatalf("expected 1 diff spec, got %d", len(specs))
	}
	if specs[0].Label != "diff: all" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// Commit hash mode.
	p2 := pilotParams{Task: "review", Diff: "abc123"}
	specs2 := expandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "diff" {
		t.Fatalf("expected 1 diff spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Test(t *testing.T) {
	// Specific path.
	p := pilotParams{Task: "analyze", Test: "gateway-go/..."}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "test" {
		t.Fatalf("expected 1 test spec, got %d", len(specs))
	}
	if specs[0].Label != "test: gateway-go/..." {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// "all" mode.
	p2 := pilotParams{Task: "analyze", Test: "all"}
	specs2 := expandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "test" {
		t.Fatalf("expected 1 test spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Tree(t *testing.T) {
	p := pilotParams{Task: "overview", Tree: "/home/user/project"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "tree" {
		t.Fatalf("expected 1 tree spec, got %d", len(specs))
	}
	if specs[0].Label != "tree: /home/user/project" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}
}

func TestExpandShortcuts_GitLog(t *testing.T) {
	// "recent" mode.
	p := pilotParams{Task: "summarize", GitLog: "recent"}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "git" {
		t.Fatalf("expected 1 git spec, got %d", len(specs))
	}
	if specs[0].Label != "git_log: recent" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// "oneline" mode.
	p2 := pilotParams{Task: "summarize", GitLog: "oneline"}
	specs2 := expandShortcuts(p2)
	if len(specs2) != 1 || specs2[0].Tool != "git" {
		t.Fatalf("expected 1 git spec, got %d", len(specs2))
	}
}

func TestExpandShortcuts_Health(t *testing.T) {
	p := pilotParams{Task: "diagnose", Health: true}
	specs := expandShortcuts(p)
	if len(specs) != 1 || specs[0].Tool != "health_check" {
		t.Fatalf("expected 1 health_check spec, got %d", len(specs))
	}
	if specs[0].Label != "health_check" {
		t.Errorf("unexpected label: %q", specs[0].Label)
	}

	// Health=false should not add a spec.
	p2 := pilotParams{Task: "diagnose", Health: false}
	specs2 := expandShortcuts(p2)
	if len(specs2) != 0 {
		t.Errorf("expected 0 specs for health=false, got %d", len(specs2))
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
	}
	for _, tc := range cases {
		got := parseLineCount(tc.input, tc.defN)
		if got != tc.expected {
			t.Errorf("parseLineCount(%q, %d) = %d, want %d", tc.input, tc.defN, got, tc.expected)
		}
	}
}
