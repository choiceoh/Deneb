package chat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSessionMemory_Trim(t *testing.T) {
	m := &SessionMemory{
		Title:   string(make([]rune, 200)),
		Current: string(make([]rune, 300)),
		Files:   make([]string, 25),
	}
	for i := range m.Files {
		m.Files[i] = "file.go"
	}
	m.Trim()

	if len([]rune(m.Title)) > maxTitleRunes+3 { // +3 for "..."
		t.Errorf("Title not trimmed: got %d runes", len([]rune(m.Title)))
	}
	if len([]rune(m.Current)) > maxCurrentRunes+3 {
		t.Errorf("Current not trimmed: got %d runes", len([]rune(m.Current)))
	}
	if len(m.Files) != maxFiles {
		t.Errorf("Files not trimmed: got %d, want %d", len(m.Files), maxFiles)
	}
}

func TestSessionMemory_TrimWorklog(t *testing.T) {
	m := &SessionMemory{}
	for i := 0; i < 30; i++ {
		m.Worklog = append(m.Worklog, Entry{Time: "10:00", Text: "item"})
	}
	m.Trim()
	if len(m.Worklog) != maxWorklog {
		t.Errorf("Worklog not trimmed: got %d, want %d", len(m.Worklog), maxWorklog)
	}
}

func TestSessionMemory_IsEmpty(t *testing.T) {
	m := &SessionMemory{}
	if !m.IsEmpty() {
		t.Error("empty memory should return IsEmpty=true")
	}
	m.Title = "test"
	if m.IsEmpty() {
		t.Error("non-empty memory should return IsEmpty=false")
	}
}

func TestSessionMemory_FormatForPrompt_Empty(t *testing.T) {
	m := &SessionMemory{}
	if got := m.FormatForPrompt(); got != "" {
		t.Errorf("empty memory should format as empty string, got %q", got)
	}
}

func TestSessionMemory_FormatForPrompt(t *testing.T) {
	m := &SessionMemory{
		Title:   "Memory system refactoring",
		Current: "4/7 files modified",
		Files:   []string{"executor.go", "spillover.go"},
		Errors:  []string{"build failed (missing import) -> fixed"},
		Worklog: []Entry{{Time: "10:30", Text: "started refactoring"}},
	}
	got := m.FormatForPrompt()
	for _, want := range []string{
		"## Session State",
		"Title: Memory system refactoring",
		"Current: 4/7 files modified",
		"Files: executor.go, spillover.go",
		"Error 1:",
		"[10:30] started refactoring",
	} {
		if !contains(got, want) {
			t.Errorf("FormatForPrompt missing %q in:\n%s", want, got)
		}
	}
}

func TestSessionMemory_JSONRoundTrip(t *testing.T) {
	m := &SessionMemory{
		Title:     "Test session",
		Current:   "doing stuff",
		TaskSpec:  "build a feature",
		Files:     []string{"a.go", "b.go"},
		Functions: []string{"Foo", "Bar"},
		Workflow:  []string{"[✓] plan", "[→] implement"},
		Errors:    []string{"type error -> fixed"},
		Learnings: []string{"always check nil"},
		Results:   []string{"feature working"},
		Worklog:   []Entry{{Time: "09:00", Text: "started"}},
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got SessionMemory
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Title != m.Title {
		t.Errorf("Title = %q, want %q", got.Title, m.Title)
	}
	if len(got.Files) != len(m.Files) {
		t.Errorf("Files len = %d, want %d", len(got.Files), len(m.Files))
	}
	if len(got.Worklog) != 1 || got.Worklog[0].Time != "09:00" {
		t.Errorf("Worklog roundtrip failed: %+v", got.Worklog)
	}
}

func TestSessionMemoryStore_SetGetDelete(t *testing.T) {
	store := NewSessionMemoryStore("")
	mem := &SessionMemory{Title: "test"}
	store.Set("session:1", mem)

	got := store.Get("session:1")
	if got == nil || got.Title != "test" {
		t.Errorf("Get after Set: got %+v", got)
	}

	store.Delete("session:1")
	if store.Get("session:1") != nil {
		t.Error("Get after Delete should return nil")
	}
}

func TestSessionMemoryStore_DiskPersistence(t *testing.T) {
	dir := t.TempDir()
	store := NewSessionMemoryStore(dir)
	mem := &SessionMemory{
		Title:   "persistent session",
		Current: "testing disk I/O",
		Files:   []string{"test.go"},
	}
	store.Set("telegram:123", mem)

	// Wait for async disk write.
	// saveToDisk runs in a goroutine; give it a moment.
	path := filepath.Join(dir, "telegram_123.memory.json")
	for i := 0; i < 50; i++ {
		if _, err := os.Stat(path); err == nil {
			break
		}
		// Busy-wait: 10ms intervals, max 500ms.
		// This is a test helper, not production code.
		func() {
			ch := make(chan struct{})
			go func() { close(ch) }()
			<-ch
		}()
	}

	// Load into a new store.
	store2 := NewSessionMemoryStore(dir)
	loaded := store2.LoadFromDisk()
	if loaded != 1 {
		t.Fatalf("LoadFromDisk: loaded %d, want 1", loaded)
	}
	got := store2.Get("telegram:123")
	if got == nil {
		t.Fatal("loaded memory is nil")
	}
	if got.Title != "persistent session" {
		t.Errorf("loaded Title = %q, want %q", got.Title, "persistent session")
	}
}

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		wantLen  int
		wantDots bool
	}{
		{"hello", 10, 5, false},
		{"hello world", 5, 8, true}, // 5 runes + "..."
		{"한글 테스트 문장", 3, 6, true},   // 3 Korean runes + "..."
	}
	for _, tt := range tests {
		got := truncateRunes(tt.input, tt.max)
		runes := []rune(got)
		if len(runes) != tt.wantLen {
			t.Errorf("truncateRunes(%q, %d) = %q (%d runes), want %d runes",
				tt.input, tt.max, got, len(runes), tt.wantLen)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{`{"title":"test"}`, `{"title":"test"}`},
		{"```json\n{\"title\":\"test\"}\n```", `{"title":"test"}`},
		{"Here's the update:\n```\n{\"title\":\"test\"}\n```\n", `{"title":"test"}`},
		{`null`, `null`},
	}
	for _, tt := range tests {
		got := extractJSON(tt.input)
		if got != tt.want {
			t.Errorf("extractJSON(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestTailSlice(t *testing.T) {
	s := []string{"a", "b", "c", "d", "e"}
	got := tailSlice(s, 3)
	if len(got) != 3 || got[0] != "c" {
		t.Errorf("tailSlice = %v, want [c d e]", got)
	}
	// No-op when within limit.
	got2 := tailSlice(s, 10)
	if len(got2) != 5 {
		t.Errorf("tailSlice(5, 10) = %d items, want 5", len(got2))
	}
}

func TestSanitizeSessionKey(t *testing.T) {
	key := "telegram:12345"
	safe := sanitizeSessionKey(key)
	if safe != "telegram_12345" {
		t.Errorf("sanitize = %q, want %q", safe, "telegram_12345")
	}
	restored := unsanitizeSessionKey(safe)
	if restored != key {
		t.Errorf("unsanitize = %q, want %q", restored, key)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsStr(s, substr)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
