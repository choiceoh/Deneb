package handlerminiapp

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAnalysisStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := NewAnalysisStore(dir)

	rec := &analysisRecord{
		MsgID:         "abc123",
		Subject:       "Hello",
		From:          "a@b.com",
		Date:          "2026-05-27T10:00:00Z",
		Analysis:      "## 핵심 요약\n- foo\n- bar",
		DurationMs:    1234,
		PromptVersion: AnalysisPromptVersion,
		CreatedAt:     time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC),
	}
	if err := store.save(rec); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.load("abc123")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got == nil {
		t.Fatal("load returned nil for a previously-saved record")
	}
	if got.MsgID != rec.MsgID || got.Analysis != rec.Analysis {
		t.Errorf("round-trip mismatch: got %+v want %+v", got, rec)
	}
}

func TestAnalysisStore_LoadMiss_NoFile(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())
	got, err := store.load("never-saved")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil miss, got %+v", got)
	}
}

func TestAnalysisStore_LoadMiss_PromptVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	store := NewAnalysisStore(dir)
	if err := store.save(&analysisRecord{
		MsgID:         "v1msg",
		Analysis:      "old",
		PromptVersion: "v0", // intentionally mismatched
		CreatedAt:     time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := store.load("v1msg")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != nil {
		t.Errorf("expected miss on prompt version mismatch, got %+v", got)
	}
}

// Defense-in-depth: a hostile msgID containing path separators must
// neither escape the cache dir on save nor resolve on load.
func TestAnalysisStore_PathFor_RejectsTraversal(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())
	for _, id := range []string{
		"",
		"../escape",
		"a/b",
		"a\\b",
		"a.json", // contains '.' — Gmail IDs are [a-zA-Z0-9_-] only
	} {
		if p := store.pathFor(id); p != "" {
			t.Errorf("pathFor(%q) returned %q, want empty (refused)", id, p)
		}
	}
}

func TestAnalysisStore_Save_RefusesHostileID(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())
	err := store.save(&analysisRecord{MsgID: "../oops", Analysis: "x"})
	if err == nil {
		t.Fatal("expected error for hostile msgID, got nil")
	}
}

// Nil/zero store must be safe to call — handlers use that as the
// "caching disabled" signal.
func TestAnalysisStore_ZeroValue_IsNoOp(t *testing.T) {
	var s *AnalysisStore
	if got, err := s.load("any"); got != nil || err != nil {
		t.Errorf("nil store load: got=%+v err=%v, want nil/nil", got, err)
	}
	if err := s.save(&analysisRecord{MsgID: "x"}); err != nil {
		t.Errorf("nil store save: err=%v, want nil", err)
	}

	empty := NewAnalysisStore("")
	if got, err := empty.load("any"); got != nil || err != nil {
		t.Errorf("empty-dir store load: got=%+v err=%v, want nil/nil", got, err)
	}
}

// Corrupt JSON on disk should surface as an error so the handler can
// log + fall through to a fresh LLM run (rather than silently masking
// the cache as missing).
func TestAnalysisStore_LoadCorrupt_Error(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := NewAnalysisStore(dir)
	if _, err := store.load("broken"); err == nil {
		t.Error("expected error for corrupt JSON, got nil")
	}
}
