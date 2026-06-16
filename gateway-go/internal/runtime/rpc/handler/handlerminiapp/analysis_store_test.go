package handlerminiapp

import (
	"os"
	"path/filepath"
	"strings"
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
func TestAnalysisStore_PathFor_SanitizesUnsafe(t *testing.T) {
	dir := t.TempDir()
	store := NewAnalysisStore(dir)

	// Empty id has no cache path.
	if p := store.pathFor(""); p != "" {
		t.Errorf("pathFor(%q) = %q, want empty", "", p)
	}

	// Unsafe / dotted ids are sanitized into a filename that stays INSIDE the
	// cache dir — never refused (the old guard dropped these), never escaping.
	for _, id := range []string{"../escape", "a/b", "a\\b", "..", "/etc/passwd", "id@host.example.com"} {
		p := store.pathFor(id)
		if p == "" {
			t.Errorf("pathFor(%q) = empty, want a sanitized in-dir path", id)
			continue
		}
		if filepath.Dir(p) != dir {
			t.Errorf("pathFor(%q) = %q escapes the cache dir %q", id, p, dir)
		}
		if strings.ContainsAny(filepath.Base(p), `/\`) {
			t.Errorf("pathFor(%q) base %q still contains a separator", id, filepath.Base(p))
		}
	}
}

// The LMTP ingest path keys analyses by RFC 5322 Message-IDs (dots + '@'), which
// the old guard rejected — every LMTP-ingested analysis silently failed to cache.
func TestAnalysisStore_RoundTrip_DottedMessageID(t *testing.T) {
	store := NewAnalysisStore(t.TempDir())
	id := "lmtp-test-001@topsolar-test.example"
	if err := store.save(&analysisRecord{MsgID: id, Analysis: "견적 분석", PromptVersion: AnalysisPromptVersion}); err != nil {
		t.Fatalf("save dotted Message-ID: %v", err)
	}
	got, err := store.load(id)
	if err != nil || got == nil {
		t.Fatalf("load dotted Message-ID: got=%+v err=%v", got, err)
	}
	if got.Analysis != "견적 분석" {
		t.Errorf("round-trip Analysis = %q, want %q", got.Analysis, "견적 분석")
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
