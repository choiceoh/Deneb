package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSessionMemory_IsEmpty(t *testing.T) {
	m := &SessionMemory{}
	if !m.IsEmpty() {
		t.Error("zero-value should be empty")
	}
	m.Summary = "test"
	if m.IsEmpty() {
		t.Error("non-zero Summary should make it non-empty")
	}
}

func TestSessionMemory_Trim(t *testing.T) {
	long := strings.Repeat("가", smLimitSummary+100)
	m := &SessionMemory{
		Summary:     long,
		State:       strings.Repeat("나", smLimitState+50),
		TaskContext: strings.Repeat("다", smLimitTaskContext+50),
		Progress:    strings.Repeat("라", smLimitProgress+50),
		Decisions:   strings.Repeat("마", smLimitDecisions+50),
		Errors:      strings.Repeat("바", smLimitErrors+50),
		Worklog:     strings.Repeat("사", smLimitWorklog+50),
	}
	m.Trim()

	// Check each section is within limit + 1 rune for "…".
	checks := []struct {
		name  string
		value string
		limit int
	}{
		{"Summary", m.Summary, smLimitSummary},
		{"State", m.State, smLimitState},
		{"TaskContext", m.TaskContext, smLimitTaskContext},
		{"Progress", m.Progress, smLimitProgress},
		{"Decisions", m.Decisions, smLimitDecisions},
		{"Errors", m.Errors, smLimitErrors},
		{"Worklog", m.Worklog, smLimitWorklog},
	}
	for _, c := range checks {
		runes := []rune(c.value)
		// limit + 1 for the "…" character
		if len(runes) > c.limit+1 {
			t.Errorf("%s: %d runes, want <= %d", c.name, len(runes), c.limit+1)
		}
	}
}

func TestSessionMemory_TrimNoOp(t *testing.T) {
	m := &SessionMemory{Summary: "short"}
	m.Trim()
	if m.Summary != "short" {
		t.Errorf("Trim modified short string: %q", m.Summary)
	}
}

func TestSessionMemory_FormatForPrompt_Empty(t *testing.T) {
	m := &SessionMemory{}
	if got := m.FormatForPrompt(); got != "" {
		t.Errorf("empty memory should format as empty, got %q", got)
	}
}

func TestSessionMemory_FormatForPrompt(t *testing.T) {
	m := &SessionMemory{
		Summary:  "메모리 시스템 리팩토링 세션",
		State:    "4/7 파일 수정 완료",
		Progress: "- [✓] 구조체 정의\n- [→] spillover 구현\n- [ ] 테스트",
		Errors:   "빌드 실패 (import 누락) → 수정 완료",
		Worklog:  "[14:30] 시작\n[14:35] 구조체 완료",
	}
	got := m.FormatForPrompt()

	mustContain := []string{
		"## Session State",
		"### 요약\n메모리 시스템 리팩토링 세션",
		"### 현재 상태\n4/7 파일 수정 완료",
		"### 진행 상황",
		"### 오류/문제",
		"### 작업 이력",
	}
	for _, want := range mustContain {
		if !strings.Contains(got, want) {
			t.Errorf("FormatForPrompt missing %q in:\n%s", want, got)
		}
	}

	// Empty sections should not appear.
	mustNotContain := []string{"### 작업 컨텍스트", "### 결정 사항"}
	for _, unwant := range mustNotContain {
		if strings.Contains(got, unwant) {
			t.Errorf("FormatForPrompt should omit empty section %q", unwant)
		}
	}
}

func TestSessionMemory_JSONRoundTrip(t *testing.T) {
	m := &SessionMemory{
		Summary:     "테스트 세션",
		State:       "진행 중",
		TaskContext: "기능 구현",
		Progress:    "50% 완료",
		Decisions:   "접근법 A 선택",
		Errors:      "없음",
		Worklog:     "[10:00] 시작",
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SessionMemory
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Summary != m.Summary || got.Decisions != m.Decisions || got.Worklog != m.Worklog {
		t.Errorf("roundtrip mismatch: got %+v", got)
	}
}

func TestSessionMemoryStore_GetSetDelete(t *testing.T) {
	store := NewSessionMemoryStore("")
	mem := &SessionMemory{Summary: "test"}
	store.Set("s1", mem)

	got := store.Get("s1")
	if got == nil || got.Summary != "test" {
		t.Errorf("Get after Set: %+v", got)
	}

	store.Delete("s1")
	if store.Get("s1") != nil {
		t.Error("Get after Delete should be nil")
	}
}

func TestSessionMemoryStore_DiskPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMemoryStore(dir)
	mem := &SessionMemory{
		Summary: "persistent",
		State:   "testing disk",
	}
	store.Set("telegram:123", mem)

	// Wait for async disk write (short busy-wait acceptable in tests).
	path := filepath.Join(dir, "telegram_123.memory.json")
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
	if got == nil || got.Summary != "persistent" || got.State != "testing disk" {
		t.Errorf("loaded memory: %+v", got)
	}
}

func TestTruncRunes(t *testing.T) {
	tests := []struct {
		input   string
		max     int
		wantLen int
	}{
		{"hello", 10, 5},       // no truncation
		{"hello world", 5, 6},  // 5 + "…"
		{"한글 테스트 문장입니다", 4, 5}, // 4 Korean runes + "…"
		{"", 10, 0},            // empty
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
