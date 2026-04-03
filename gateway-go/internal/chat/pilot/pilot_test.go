package pilot

import (
	"context"
	"encoding/json"
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
		got := ShouldUseThinking(tt.task, tt.sourceCount)
		if got != tt.want {
			t.Errorf("ShouldUseThinking(%q, %d) = %v, want %v", tt.task, tt.sourceCount, got, tt.want)
		}
	}
}

func TestShouldBypassPilotLLM(t *testing.T) {
	tests := []struct {
		name    string
		p       PilotParams
		sources []SourceSpec
		gather  []SourceResult
		want    bool
	}{
		{
			name:    "single short read",
			p:       PilotParams{Task: "explain"},
			sources: []SourceSpec{{Tool: "read"}},
			gather:  []SourceResult{{Label: "main.go", Content: "package main", SourceType: "file"}},
			want:    true,
		},
		{
			name:    "two short simple sources",
			p:       PilotParams{Task: "compare"},
			sources: []SourceSpec{{Tool: "read"}, {Tool: "grep"}},
			gather: []SourceResult{
				{Label: "a.go", Content: "func a() {}", SourceType: "file"},
				{Label: "grep: TODO", Content: "a.go:1: TODO", SourceType: "grep"},
			},
			want: true,
		},
		{
			name:    "single long read",
			p:       PilotParams{Task: "explain"},
			sources: []SourceSpec{{Tool: "read"}},
			gather:  []SourceResult{{Label: "main.go", Content: strings.Repeat("a", 1001), SourceType: "file"}},
			want:    false,
		},
		{
			name:    "noisy exec stays on pilot",
			p:       PilotParams{Task: "summarize"},
			sources: []SourceSpec{{Tool: "exec"}},
			gather:  []SourceResult{{Label: "$ go test", Content: "FAIL", SourceType: "exec"}},
			want:    false,
		},
		{
			name:    "chain disables bypass",
			p:       PilotParams{Task: "summarize", Chain: true},
			sources: []SourceSpec{{Tool: "read"}},
			gather:  []SourceResult{{Label: "main.go", Content: "package main", SourceType: "file"}},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldBypassPilotLLM(tt.p, tt.sources, tt.gather)
			if got != tt.want {
				t.Fatalf("ShouldBypassPilotLLM() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildPilotPassthroughResult(t *testing.T) {
	single := BuildPilotPassthroughResult([]SourceResult{{Label: "main.go", Content: "package main", SourceType: "file"}})
	if single != "package main" {
		t.Fatalf("unexpected single passthrough result: %q", single)
	}

	multi := BuildPilotPassthroughResult([]SourceResult{
		{Label: "a.go", Content: "package a", SourceType: "file"},
		{Label: "b.go", Content: "package b", SourceType: "file"},
	})
	if !strings.Contains(multi, "--- a.go ---") || !strings.Contains(multi, "package b") {
		t.Fatalf("unexpected multi passthrough result: %q", multi)
	}
}

type stubPilotExecutor struct {
	calls int
}

func (s *stubPilotExecutor) Execute(_ context.Context, _ string, _ json.RawMessage) (string, error) {
	s.calls++
	return "short content", nil
}

func TestToolPilotBypassesLocalLLMForShortRead(t *testing.T) {
	exec := &stubPilotExecutor{}
	fn := ToolPilot(exec, "")

	out, err := fn(context.Background(), MustJSON(map[string]any{
		"task": "explain this file",
		"file": "README.md",
	}))
	if err != nil {
		t.Fatalf("ToolPilot() error = %v", err)
	}
	if exec.calls != 1 {
		t.Fatalf("expected 1 tool execution, got %d", exec.calls)
	}
	if out != "short content" {
		t.Fatalf("unexpected passthrough output: %q", out)
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
			got := CleanJSONResponse(tt.input)
			if got != tt.want {
				t.Errorf("CleanJSONResponse() = %q, want %q", got, tt.want)
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
	if got := SmartTruncate(short, 100, "file"); got != short {
		t.Errorf("short content should not be truncated")
	}

	// File truncation: preserves head + tail.
	result := SmartTruncate(long, 2000, "file")
	if len(result) > 2200 { // some overhead from marker
		t.Errorf("file truncation too long: %d", len(result))
	}
	// Should contain marker.
	if !strings.Contains(result, "truncated") {
		t.Errorf("file truncation should contain truncation marker")
	}

	// Exec truncation: preserves tail.
	result = SmartTruncate(long, 2000, "exec")
	if !strings.Contains(result, "truncated") {
		t.Errorf("exec truncation should contain truncation marker")
	}

	// Default truncation: head only.
	result = SmartTruncate(long, 2000, "content")
	if !strings.Contains(result, "truncated at") {
		t.Errorf("default truncation should contain 'truncated at' marker")
	}
}

func TestBuildPilotPrompt(t *testing.T) {
	blocks := []SourceResult{
		{"main.go", "package main\nfunc main() {}", "file"},
		{"$ ls", "file1.go\nfile2.go", "exec"},
	}

	result := BuildPilotPrompt("리뷰해줘", "json", "brief", blocks)

	if !strings.Contains(result, "Task: 리뷰해줘") {
		t.Error("prompt should contain task")
	}
	if !strings.Contains(result, "Output format: json") {
		t.Error("prompt should contain output format")
	}
	if !strings.Contains(result, "under 500 characters") {
		t.Error("prompt should contain brief length hint")
	}
	if !strings.Contains(result, "--- main.go ---") {
		t.Error("prompt should contain source labels")
	}
}

func TestBuildPilotPromptNoBlocks(t *testing.T) {
	result := BuildPilotPrompt("just a question", "", "", nil)
	if result != "Task: just a question" {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestBuildFallbackResult(t *testing.T) {
	gathered := []SourceResult{
		{"main.go", "package main", "file"},
	}

	result := BuildFallbackResult("리뷰해줘", gathered)
	if !strings.Contains(result, "pilot model unavailable") {
		t.Error("fallback should mention pilot model unavailable")
	}
	if !strings.Contains(result, "Task: 리뷰해줘") {
		t.Error("fallback should contain task")
	}
	if !strings.Contains(result, "package main") {
		t.Error("fallback should contain source content")
	}
}

func TestBuildPilotSystemPrompt(t *testing.T) {
	prompt := BuildPilotSystemPrompt("/workspace", true)
	if !strings.Contains(prompt, "Workspace directory: /workspace") {
		t.Error("should contain workspace dir")
	}
	if !strings.Contains(prompt, "<think>") {
		t.Error("should contain thinking instruction when thinking=true")
	}

	prompt = BuildPilotSystemPrompt("", false)
	if strings.Contains(prompt, "Workspace") {
		t.Error("should not contain workspace when empty")
	}
	if strings.Contains(prompt, "<think>") {
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
		if got := SourceTypeFromTool(tool); got != want {
			t.Errorf("SourceTypeFromTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestTruncateHead(t *testing.T) {
	short := "hello"
	if got := TruncateHead(short, 100); got != short {
		t.Errorf("short string should not be truncated")
	}

	long := string(make([]byte, 500))
	result := TruncateHead(long, 100)
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
	if got := EnforceMaxLength(short, 100); got != short {
		t.Error("short string should not be truncated")
	}

	// Line boundary cut.
	multiline := "Line one.\nLine two.\nLine three.\nLine four.\nLine five."
	result := EnforceMaxLength(multiline, 30)
	if len(result) > 35 { // some overhead for ellipsis
		t.Errorf("EnforceMaxLength too long: %d chars", len(result))
	}
	if !strings.Contains(result, "…") {
		t.Error("should contain ellipsis")
	}

	// Sentence boundary cut.
	prose := "This is the first sentence. This is the second sentence. This is very long text that keeps going."
	result = EnforceMaxLength(prose, 60)
	if !strings.Contains(result, "…") {
		t.Error("should contain ellipsis")
	}
}

func TestPostProcessOutput(t *testing.T) {
	// Brief mode enforces length.
	long := strings.Repeat("가나다라 ", 200) // ~1000 chars Korean
	result := PostProcessOutput(long, "text", "brief")
	if len(result) > BriefMaxChars+10 {
		t.Errorf("brief mode should enforce length, got %d chars", len(result))
	}

	// JSON mode cleans fences.
	jsonWithFence := "```json\n{\"key\": \"value\"}\n```"
	result = PostProcessOutput(jsonWithFence, "json", "")
	if result != `{"key": "value"}` {
		t.Errorf("json mode should strip fences, got %q", result)
	}

	// List mode normalizes.
	bullets := "- Alpha\n- Beta\n- Gamma"
	result = PostProcessOutput(bullets, "list", "")
	if !strings.Contains(result, "1. Alpha") {
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

func TestSourceTypeFromTool_NewTools(t *testing.T) {
	tests := map[string]string{
		"gmail":              "content",
		"youtube_transcript": "content",
		"polaris":            "content",
		"image":              "content",
		"diff":               "file",
		"test":               "exec",
		"tree":               "file",
		"http":               "exec",
	}
	for tool, want := range tests {
		if got := SourceTypeFromTool(tool); got != want {
			t.Errorf("SourceTypeFromTool(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestSourceSucceeded(t *testing.T) {
	results := []SourceResult{
		{Label: "mem", Content: "some results", SourceType: "content"},
		{Label: "err", Content: "[tool error: failed]", SourceType: "content"},
		{Label: "skip", Content: "[skipped: mem did not succeed]", SourceType: "content"},
		{Label: "empty", Content: "", SourceType: "content"},
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
	gathered := []SourceResult{{Label: "data", Content: "foo bar\nbaz qux\nfoo baz", SourceType: "content"}}
	steps := []PostProcessStep{{Action: "filter_lines", Param: "foo"}}
	result := ApplyPostProcessSteps(gathered, steps)
	if result[0].Content != "foo bar\nfoo baz" {
		t.Errorf("unexpected filter result: %q", result[0].Content)
	}
}

func TestApplyPostProcessSteps_Unique(t *testing.T) {
	gathered := []SourceResult{{Label: "data", Content: "a\nb\na\nc\nb", SourceType: "content"}}
	steps := []PostProcessStep{{Action: "unique"}}
	result := ApplyPostProcessSteps(gathered, steps)
	if result[0].Content != "a\nb\nc" {
		t.Errorf("unexpected unique result: %q", result[0].Content)
	}
}

func TestApplyPostProcessSteps_Sort(t *testing.T) {
	gathered := []SourceResult{{Label: "data", Content: "cherry\napple\nbanana", SourceType: "content"}}
	steps := []PostProcessStep{{Action: "sort"}}
	result := ApplyPostProcessSteps(gathered, steps)
	if result[0].Content != "apple\nbanana\ncherry" {
		t.Errorf("unexpected sort result: %q", result[0].Content)
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
		got := ParseLineCount(tc.input, tc.defN)
		if got != tc.expected {
			t.Errorf("ParseLineCount(%q, %d) = %d, want %d", tc.input, tc.defN, got, tc.expected)
		}
	}
}
