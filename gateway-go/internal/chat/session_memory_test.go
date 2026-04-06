package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatForPrompt_Empty(t *testing.T) {
	if got := FormatForPrompt(""); got != "" {
		t.Errorf("empty content should format as empty, got %q", got)
	}
}

func TestFormatForPrompt_Template(t *testing.T) {
	// Template-only content should be treated as empty.
	if got := FormatForPrompt(sessionMemoryTemplate); got != "" {
		t.Errorf("template-only content should format as empty, got len=%d", len(got))
	}
}

func TestFormatForPrompt_WithContent(t *testing.T) {
	content := `# Session Title
메모리 시스템 리팩토링 세션

# Current State
4/7 파일 수정 완료

# Worklog
[14:30] 시작
[14:35] 구조체 완료`

	got := FormatForPrompt(content)

	mustContain := []string{
		"## Session State",
		"# Session Title",
		"메모리 시스템 리팩토링 세션",
		"# Current State",
		"4/7 파일 수정 완료",
		"# Worklog",
		"[14:30] 시작",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("FormatForPrompt missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionMemoryStore_GetSetDelete(t *testing.T) {
	store := NewSessionMemoryStore("")
	store.Set("s1", "# Session Title\ntest session")

	got := store.Get("s1")
	if !strings.Contains(got, "test session") {
		t.Errorf("Get after Set: %q", got)
	}

	store.Delete("s1")
	if store.Get("s1") != "" {
		t.Error("Get after Delete should be empty")
	}
}

func TestSessionMemoryStore_DiskPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMemoryStore(dir)
	store.Set("telegram:123", "# Session Title\npersistent session\n\n# Current State\ntesting disk")

	// Wait for async disk write.
	path := filepath.Join(dir, "telegram_123.memory.md")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("disk file not created: %v", err)
	}

	// Load into a fresh store.
	store2 := NewSessionMemoryStore(dir)
	loaded := store2.LoadFromDisk()
	if loaded != 1 {
		t.Fatalf("LoadFromDisk: loaded %d, want 1", loaded)
	}
	got := store2.Get("telegram:123")
	if !strings.Contains(got, "persistent session") || !strings.Contains(got, "testing disk") {
		t.Errorf("loaded memory: %q", got)
	}
}

func TestEnforceTokenBudget_Short(t *testing.T) {
	content := "# Session Title\nshort"
	got := enforceTokenBudget(content, 12000)
	if got != content {
		t.Errorf("short content should not be modified")
	}
}

func TestEnforceTokenBudget_Long(t *testing.T) {
	// Build a section that exceeds maxSectionTokens (2000 tokens = 8000 chars).
	// Use a body that's clearly over the per-section char limit.
	longBody := strings.Repeat("abcdefgh\n", maxSectionTokens) // ~18000 chars > 8000
	content := "# Session Title\ntest\n\n# Worklog\n" + longBody
	// Use a small total budget to trigger enforcement.
	got := enforceTokenBudget(content, 3000)
	if strings.Contains(got, longBody) {
		t.Error("long section should have been truncated")
	}
	if !strings.Contains(got, "[... 섹션이 길어서 잘림 ...]") {
		t.Error("truncated section should have marker")
	}
}

func TestExtractTitle(t *testing.T) {
	content := `# Session Title
_세션 제목_
메모리 시스템 리팩토링

# Current State
진행 중`
	got := extractTitle(content)
	if got != "메모리 시스템 리팩토링" {
		t.Errorf("extractTitle = %q, want %q", got, "메모리 시스템 리팩토링")
	}
}

func TestExtractTitle_Empty(t *testing.T) {
	content := "# Session Title\n_설명_\n\n# Current State\nfoo"
	got := extractTitle(content)
	if got != "" {
		t.Errorf("extractTitle should be empty for template-only title, got %q", got)
	}
}

func TestParseSections(t *testing.T) {
	content := `# Title
body1

# State
body2
line2`
	sections := parseSections(content)
	if len(sections) != 2 {
		t.Fatalf("parseSections: got %d sections, want 2", len(sections))
	}
	if sections[0].header != "# Title" {
		t.Errorf("section 0 header: %q", sections[0].header)
	}
	if !strings.Contains(sections[1].body, "body2") {
		t.Errorf("section 1 body missing 'body2': %q", sections[1].body)
	}
}

func TestTruncRunes(t *testing.T) {
	tests := []struct {
		input   string
		max     int
		wantLen int
	}{
		{"hello", 10, 5},
		{"hello world", 5, 6},  // 5 + "…"
		{"한글 테스트 문장입니다", 4, 5}, // 4 Korean runes + "…"
		{"", 10, 0},
	}
	for _, tt := range tests {
		got := truncRunes(tt.input, tt.max)
		runes := []rune(got)
		if len(runes) != tt.wantLen {
			t.Errorf("truncRunes(%q, %d) = %q (%d runes), want %d runes",
				tt.input, tt.max, got, len(runes), tt.wantLen)
		}
	}
}

func TestStripCodeFence(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{`{"a":"b"}`, `{"a":"b"}`},
		{"```json\n{\"a\":\"b\"}\n```", `{"a":"b"}`},
		{"```markdown\n# Title\ncontent\n```", "# Title\ncontent"},
		{"Here:\n```\n{\"a\":\"b\"}\n```\n", `{"a":"b"}`},
		{"null", "null"},
	}
	for _, tt := range tests {
		if got := stripCodeFence(tt.input); got != tt.want {
			t.Errorf("stripCodeFence(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeKey(t *testing.T) {
	key := "telegram:12345:thread:42"
	safe := sanitizeKey(key)
	if strings.Contains(safe, ":") {
		t.Errorf("sanitized key still has colons: %q", safe)
	}
	if unsanitizeKey(safe) != key {
		t.Errorf("round-trip failed: %q", unsanitizeKey(safe))
	}
}

func TestFormatTranscriptForMemory(t *testing.T) {
	msgs := []ChatMessage{
		NewTextChatMessage("user", "Fix the bug", 1711324800000),
		NewTextChatMessage("assistant", "Tools used: edit x1\n\nFixed.", 1711324860000),
	}
	got := formatTranscriptForMemory(msgs)
	if !strings.Contains(got, "user]") || !strings.Contains(got, "assistant]") {
		t.Errorf("should contain role labels: %q", got)
	}
	if !strings.Contains(got, "Fix the bug") || !strings.Contains(got, "Fixed.") {
		t.Errorf("should contain message content: %q", got)
	}
}

func TestFormatRichContent(t *testing.T) {
	// Rich content with tool_use and tool_result blocks.
	rich := `[{"type":"text","text":"Let me read that file."},{"type":"tool_use","name":"read_file","input":{"path":"config.yaml"}},{"type":"tool_result","tool_use_id":"1","content":"port: 8080\nhost: localhost"}]`
	got := formatRichContent([]byte(rich))
	if !strings.Contains(got, "Let me read that file.") {
		t.Error("should contain text block")
	}
	// Tool_use blocks must be fully omitted — even [name] bracket format
	// causes the main LLM to mimic it as text instead of real tool calls.
	if strings.Contains(got, "[read_file]") {
		t.Error("should NOT contain tool name bracket — causes LLM to mimic tool syntax as text")
	}
	if strings.Contains(got, "read_file") {
		t.Error("should NOT contain tool name at all")
	}
	// Raw JSON input must NOT appear.
	if strings.Contains(got, "config.yaml") {
		t.Error("should NOT contain raw tool input")
	}
	// Successful tool result content must NOT appear.
	if strings.Contains(got, "port: 8080") {
		t.Error("should NOT contain tool result content")
	}
}

func TestFormatRichContent_ToolError(t *testing.T) {
	rich := `[{"type":"text","text":"Reading..."},{"type":"tool_use","name":"read_file","input":{"path":"missing.txt"}},{"type":"tool_result","tool_use_id":"1","content":"file not found","is_error":true}]`
	got := formatRichContent([]byte(rich))
	// Tool_use name must NOT appear.
	if strings.Contains(got, "read_file") {
		t.Error("should NOT contain tool name")
	}
	if !strings.Contains(got, "[오류]") {
		t.Error("should contain error annotation for failed tool result")
	}
}

func TestFormatRichContent_TextOnly(t *testing.T) {
	textJSON, _ := json.Marshal("plain text")
	got := formatRichContent(textJSON)
	if got != "plain text" {
		t.Errorf("text-only content: got %q, want %q", got, "plain text")
	}
}
