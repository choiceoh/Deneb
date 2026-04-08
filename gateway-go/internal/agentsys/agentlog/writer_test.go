package agentlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriter_AppendAndRead(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// Append a few entries for the same session.
	entries := []LogEntry{
		{Ts: 1000, Type: TypeRunStart, RunID: "run1", Session: "sess1"},
		{Ts: 2000, Type: TypeRunPrep, RunID: "run1", Session: "sess1"},
		{Ts: 3000, Type: TypeTurnLLM, RunID: "run1", Session: "sess1"},
		{Ts: 4000, Type: TypeTurnTool, RunID: "run1", Session: "sess1"},
		{Ts: 5000, Type: TypeRunEnd, RunID: "run1", Session: "sess1"},
	}
	for _, e := range entries {
		if err := w.Append(e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	// Verify file exists.
	path := filepath.Join(dir, "sess1.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected log file at %s", path)
	}

	// Read all entries.
	result := w.Read(ReadOpts{SessionKey: "sess1"})
	if result.Total != 5 {
		t.Errorf("Total = %d, want 5", result.Total)
	}
	if len(result.Entries) != 5 {
		t.Fatalf("Entries = %d, want 5", len(result.Entries))
	}
	// Newest first.
	if result.Entries[0].Ts != 5000 {
		t.Errorf("first entry Ts = %d, want 5000 (newest first)", result.Entries[0].Ts)
	}
}

func TestWriter_ReadFilterByRunID(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	_ = w.Append(LogEntry{Ts: 1000, Type: TypeRunStart, RunID: "run1", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 2000, Type: TypeRunEnd, RunID: "run1", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 3000, Type: TypeRunStart, RunID: "run2", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 4000, Type: TypeRunEnd, RunID: "run2", Session: "s1"})

	result := w.Read(ReadOpts{SessionKey: "s1", RunID: "run1"})
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
	for _, e := range result.Entries {
		if e.RunID != "run1" {
			t.Errorf("unexpected runId %q", e.RunID)
		}
	}
}

func TestWriter_ReadFilterByType(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	_ = w.Append(LogEntry{Ts: 1000, Type: TypeRunStart, RunID: "r1", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 2000, Type: TypeTurnTool, RunID: "r1", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 3000, Type: TypeTurnTool, RunID: "r1", Session: "s1"})
	_ = w.Append(LogEntry{Ts: 4000, Type: TypeRunEnd, RunID: "r1", Session: "s1"})

	result := w.Read(ReadOpts{SessionKey: "s1", Type: TypeTurnTool})
	if result.Total != 2 {
		t.Errorf("Total = %d, want 2", result.Total)
	}
}

func TestWriter_ReadLimit(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	for i := range 10 {
		_ = w.Append(LogEntry{Ts: int64(i), Type: TypeTurnLLM, RunID: "r1", Session: "s1"})
	}

	result := w.Read(ReadOpts{SessionKey: "s1", Limit: 3})
	if len(result.Entries) != 3 {
		t.Errorf("got %d entries, want 3", len(result.Entries))
	}
	if result.Total != 10 {
		t.Errorf("Total = %d, want 10", result.Total)
	}
}

func TestWriter_ReadEmptySession(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	result := w.Read(ReadOpts{SessionKey: "nonexistent"})
	if result.Total != 0 {
		t.Errorf("Total = %d, want 0", result.Total)
	}
	if len(result.Entries) != 0 {
		t.Errorf("Entries = %d, want 0", len(result.Entries))
	}
}

func TestWriter_PathSanitization(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	// Path traversal attempt should be sanitized.
	_ = w.Append(LogEntry{Ts: 1000, Type: TypeRunStart, RunID: "r1", Session: "../../etc/passwd"})
	// Should not create files outside baseDir.
	result := w.Read(ReadOpts{SessionKey: "../../etc/passwd"})
	if result.Total != 1 {
		t.Errorf("Total = %d, want 1", result.Total)
	}
	// Verify the file is inside baseDir.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("got %d, want 1 file in baseDir", len(entries))
	}
}

func TestRunLogger_NilSafe(t *testing.T) {
	// Nil RunLogger should not panic.
	var rl *RunLogger
	rl.LogStart(RunStartData{Model: "test"})
	rl.LogPrep(RunPrepData{PrepMs: 100})
	rl.LogTurnLLM(TurnLLMData{Turn: 1})
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "exec"})
	rl.LogEnd(RunEndData{StopReason: "end_turn"})
	rl.LogError(RunErrorData{Error: "test error"})
}

func TestRunLogger_NilWriter(t *testing.T) {
	rl := NewRunLogger(nil, "sess", "run")
	if rl != nil {
		t.Error("expected nil RunLogger when Writer is nil")
	}
}

func TestRunLogger_Integration(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	rl := NewRunLogger(w, "sess1", "run_abc")

	rl.LogStart(RunStartData{Model: "claude", Provider: "anthropic", Message: "hello"})
	rl.LogPrep(RunPrepData{SystemPromptChars: 5000, ContextMessages: 3, PrepMs: 50})
	rl.LogTurnLLM(TurnLLMData{Turn: 1, InputTokens: 100, OutputTokens: 50, ToolCalls: 1})
	rl.LogTurnTool(TurnToolData{Turn: 1, Name: "exec", DurationMs: 200, OutputLen: 500})
	rl.LogTurnLLM(TurnLLMData{Turn: 2, InputTokens: 200, OutputTokens: 80, StopReason: "end_turn"})
	rl.LogEnd(RunEndData{StopReason: "end_turn", Turns: 2, InputTokens: 300, OutputTokens: 130, TextLen: 200})

	result := w.Read(ReadOpts{SessionKey: "sess1"})
	if result.Total != 6 {
		t.Fatalf("Total = %d, want 6", result.Total)
	}

	// Check first entry (newest = run.end).
	if result.Entries[0].Type != TypeRunEnd {
		t.Errorf("newest entry type = %q, want %q", result.Entries[0].Type, TypeRunEnd)
	}
	if result.Entries[0].RunID != "run_abc" {
		t.Errorf("runId = %q, want %q", result.Entries[0].RunID, "run_abc")
	}

	// Check that run.end data has TotalMs filled by RunLogger.
	var endData RunEndData
	if err := json.Unmarshal(result.Entries[0].Data, &endData); err != nil {
		t.Fatalf("unmarshal RunEndData: %v", err)
	}
	if endData.TotalMs < 0 {
		t.Error("TotalMs should be >= 0 (auto-computed by RunLogger)")
	}
}

func TestRunLogger_MessageTruncation(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	rl := NewRunLogger(w, "sess1", "run1")

	longMsg := strings.Repeat("A", 5000)
	rl.LogStart(RunStartData{Message: longMsg})

	result := w.Read(ReadOpts{SessionKey: "sess1"})
	if result.Total != 1 {
		t.Fatal("expected 1 entry")
	}

	var data RunStartData
	if err := json.Unmarshal(result.Entries[0].Data, &data); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(data.Message) > maxMessageLen+10 {
		t.Errorf("message not truncated: len=%d", len(data.Message))
	}
}
