package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
)

func TestSessionLabelStore_RoundTripAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-labels.json")

	if got := loadSessionLabels(path); len(got) != 0 {
		t.Fatalf("missing file should load empty, got %v", got)
	}

	labels := map[string]string{"client:main:abc": "커밋 컨벤션 정리", "chat:old": "옛 대화"}
	if err := saveSessionLabels(path, labels); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadSessionLabels(path)
	if got["client:main:abc"] != "커밋 컨벤션 정리" || got["chat:old"] != "옛 대화" {
		t.Fatalf("roundtrip = %v", got)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSessionLabels(path); len(got) != 0 {
		t.Fatalf("corrupt file should degrade to empty, got %v", got)
	}
}

func TestSessionPinsStore_RoundTripAndCorruptFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session-pins.json")

	if got := loadSessionPins(path); len(got) != 0 {
		t.Fatalf("missing file should load empty, got %v", got)
	}

	pins := map[string]bool{"client:main:abc": true, "chat:old": true}
	if err := saveSessionPins(path, pins); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadSessionPins(path)
	if len(got) != 2 || !got["client:main:abc"] || !got["chat:old"] {
		t.Fatalf("roundtrip = %v", got)
	}

	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := loadSessionPins(path); len(got) != 0 {
		t.Fatalf("corrupt file should degrade to empty, got %v", got)
	}
}

func TestSnapshotSessionPins_FiltersToRestorablePinned(t *testing.T) {
	sessions := []*session.Session{
		{Key: "client:main:abc", Label: "이름", LabelPinned: true},   // kept
		{Key: "client:main:auto", Label: "자동", LabelPinned: false}, // not pinned
		{Key: "cron:job", Label: "크론", LabelPinned: true},          // not restorable
		nil,
	}
	got := snapshotSessionPins(sessions)
	if len(got) != 1 || !got["client:main:abc"] {
		t.Fatalf("snapshot = %v", got)
	}
	if pinsEqual(got, map[string]bool{}) {
		t.Error("pinsEqual should distinguish a non-empty set from empty")
	}
}

func TestSnapshotSessionLabels_FiltersToRestorableLabeled(t *testing.T) {
	sessions := []*session.Session{
		{Key: "client:main:abc", Label: "제트 여객기 이야기"}, // kept
		{Key: "client:main:empty", Label: "  "},       // no label
		{Key: "cron:job", Label: "크론 라벨"},             // not restorable
		nil,
	}
	got := snapshotSessionLabels(sessions)
	if len(got) != 1 || got["client:main:abc"] != "제트 여객기 이야기" {
		t.Fatalf("snapshot = %v", got)
	}
}

func TestReadTranscriptFirstExchange_ParsesUserAndAssistant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	lines := `{"type":"session","version":1,"id":"s","timestamp":1}
{"role":"user","content":"[2026-06-13T23:33:24+09:00] 머지랑 리베이스 차이 알려줘","timestamp":2}
{"role":"assistant","content":[{"type":"tool_use","name":"grep"},{"type":"text","text":"머지는 두 브랜치를…"}],"timestamp":3}
`
	if err := os.WriteFile(path, []byte(lines), 0o600); err != nil {
		t.Fatal(err)
	}
	userMsg, reply := readTranscriptFirstExchange(path)
	if userMsg != "머지랑 리베이스 차이 알려줘" {
		t.Fatalf("userMsg = %q (timestamp prefix must be stripped)", userMsg)
	}
	if reply != "머지는 두 브랜치를…" {
		t.Fatalf("reply = %q (first text block after tool_use)", reply)
	}

	// A transcript with only a header yields nothing (backfill skips it).
	empty := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(empty, []byte(`{"type":"session","version":1}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if u, r := readTranscriptFirstExchange(empty); u != "" || r != "" {
		t.Fatalf("header-only transcript = (%q, %q), want empty", u, r)
	}
}

func TestSortSessionKeysNewestFirst(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.jsonl")
	fresh := filepath.Join(dir, "fresh.jsonl")
	if err := os.WriteFile(old, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fresh, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	got := sortSessionKeysNewestFirst(dir, []string{"old", "fresh"})
	if got[0] != "fresh" || got[1] != "old" {
		t.Fatalf("order = %v, want [fresh old]", got)
	}
}

func TestRestoreAppliesStoredLabels(t *testing.T) {
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	makeSessionTranscript(t, transcriptDir, "client:main:labeled")
	storePath := filepath.Join(tmpHome, ".deneb", "session-labels.json")
	if err := saveSessionLabels(storePath, map[string]string{"client:main:labeled": "저장된 제목"}); err != nil {
		t.Fatal(err)
	}

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)
	srv.restoreAndWakeSessions(context.Background())
	time.Sleep(50 * time.Millisecond)

	got := mgr.Get("client:main:labeled")
	if got == nil || got.Label != "저장된 제목" {
		t.Fatalf("restored session label = %+v, want 저장된 제목", got)
	}
}

// A user rename pins the label; the pin must survive the hot-swap restart so the
// auto-titler doesn't re-title a user-chosen name after the next deploy.
func TestRestoreAppliesStoredPins(t *testing.T) {
	tmpHome := t.TempDir()
	transcriptDir := filepath.Join(tmpHome, ".deneb", "transcripts")
	if err := os.MkdirAll(transcriptDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", tmpHome)

	makeSessionTranscript(t, transcriptDir, "client:main:pinned")
	labelPath := filepath.Join(tmpHome, ".deneb", "session-labels.json")
	if err := saveSessionLabels(labelPath, map[string]string{"client:main:pinned": "내가 정한 이름"}); err != nil {
		t.Fatal(err)
	}
	pinsPath := filepath.Join(tmpHome, ".deneb", "session-pins.json")
	if err := saveSessionPins(pinsPath, map[string]bool{"client:main:pinned": true}); err != nil {
		t.Fatal(err)
	}

	mgr := session.NewManager()
	srv := newTestServerForRestore(mgr)
	srv.restoreAndWakeSessions(context.Background())
	time.Sleep(50 * time.Millisecond)

	got := mgr.Get("client:main:pinned")
	if got == nil || !got.LabelPinned {
		t.Fatalf("restored session should be pinned, got %+v", got)
	}
	if got.Label != "내가 정한 이름" {
		t.Fatalf("restored pinned label = %q, want 내가 정한 이름", got.Label)
	}
}
