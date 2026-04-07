package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestSpilloverStore_BelowThreshold(t *testing.T) {
	content := strings.Repeat("a", MaxResultChars-1)
	// Content below threshold should NOT be spilled by callers.
	// The store itself doesn't enforce the threshold — that's the caller's job.
	// This test just confirms MaxResultChars is sane.
	if len(content) >= MaxResultChars {
		t.Fatal("test content should be below threshold")
	}
}

func TestSpilloverStore_StoreAndLoad(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("x", MaxResultChars+100)
	spillID := testutil.Must(store.Store("telegram:user1:main", "read", content))
	if !strings.HasPrefix(spillID, "sp_") {
		t.Fatalf("spill ID should start with sp_, got %q", spillID)
	}

	// Load with same session.
	loaded := testutil.Must(store.Load(spillID, "telegram:user1:main"))
	if loaded != content {
		t.Fatalf("loaded content mismatch: got %d chars, want %d", len(loaded), len(content))
	}
}

func TestSpilloverStore_SessionIsolation(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("y", MaxResultChars+1)
	spillID := testutil.Must(store.Store("session-a", "grep", content))

	// Load from different session should fail.
	_, err := store.Load(spillID, "session-b")
	if err == nil {
		t.Fatal("expected error for different session, got nil")
	}
	if !strings.Contains(err.Error(), "different session") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpilloverStore_UnknownID(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	_, err := store.Load("sp_nonexistent", "any-session")
	if err == nil {
		t.Fatal("expected error for unknown ID, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSpilloverStore_FormatPreview(t *testing.T) {
	content := strings.Repeat("A", 500) + strings.Repeat("B", 500) + strings.Repeat("C", MaxResultChars)
	preview := FormatPreview("sp_test123", "read", content)

	if !strings.Contains(preview, "sp_test123") {
		t.Error("preview should contain spill ID")
	}
	if !strings.Contains(preview, "read") {
		t.Error("preview should contain tool name")
	}
	if !strings.Contains(preview, "read_spillover") {
		t.Error("preview should contain read_spillover instruction")
	}
	if !strings.Contains(preview, "--- Preview (first") {
		t.Error("preview should contain head section")
	}
	if !strings.Contains(preview, "--- Preview (last") {
		t.Error("preview should contain tail section")
	}
	// Preview should be much smaller than original.
	if len(preview) > PreviewHeadChars+PreviewTailChars+500 {
		t.Errorf("preview too large: %d chars", len(preview))
	}
}

func TestSpilloverStore_SpillAndPreview(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("z", MaxResultChars+5000)
	result := store.SpillAndPreview("sess1", "exec", content)

	if strings.Contains(result, strings.Repeat("z", MaxResultChars)) {
		t.Error("result should be preview, not full content")
	}
	if !strings.Contains(result, "SpillOver: ID=sp_") {
		t.Error("result should contain SpillOver header")
	}
	if !strings.Contains(result, "read_spillover") {
		t.Error("result should contain read_spillover instruction")
	}
}

func TestSpilloverStore_CleanSession(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("m", MaxResultChars+1)

	// Store two entries for session-a, one for session-b.
	idA1, _ := store.Store("session-a", "read", content)
	_, _ = store.Store("session-a", "grep", content+"extra")
	idB, _ := store.Store("session-b", "exec", content+"other")

	store.CleanSession("session-a")

	// session-a entries should be gone.
	_, err := store.Load(idA1, "session-a")
	if err == nil {
		t.Error("session-a entry should be cleaned")
	}

	// session-b should still exist.
	_, err = store.Load(idB, "session-b")
	if err != nil {
		t.Errorf("session-b entry should survive: %v", err)
	}

	// Verify files on disk.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "session_a") || strings.Contains(e.Name(), "session-a") {
			t.Errorf("session-a file should be deleted: %s", e.Name())
		}
	}
}

func TestSpilloverStore_CleanExpired(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("e", MaxResultChars+1)
	spillID, _ := store.Store("sess", "read", content)

	// Backdate the entry.
	store.mu.Lock()
	store.index[spillID].CreatedAt = time.Now().Add(-SpilloverTTL - time.Minute)
	store.mu.Unlock()

	store.cleanExpired()

	_, err := store.Load(spillID, "sess")
	if err == nil {
		t.Error("expired entry should be cleaned")
	}
}

func TestSpilloverStore_StartCleanup(t *testing.T) {
	// Just verify StartCleanup doesn't panic and respects cancellation.
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	ctx, cancel := context.WithCancel(context.Background())
	store.StartCleanup(ctx)
	cancel() // should stop the goroutine
}

func TestSpilloverStore_DiskFile(t *testing.T) {
	dir := t.TempDir()
	store := NewSpilloverStore(dir)

	content := strings.Repeat("d", MaxResultChars+1)
	_, err := store.Store("s1", "read", content)
	testutil.NoError(t, err)

	// Verify exactly one file exists on disk.
	entries := testutil.Must(os.ReadDir(dir))
	if len(entries) != 1 {
		t.Fatalf("expected 1 file, got %d", len(entries))
	}

	// Verify file content.
	data := testutil.Must(os.ReadFile(filepath.Join(dir, entries[0].Name())))
	if string(data) != content {
		t.Error("file content mismatch")
	}
}

func TestSanitizeSessionKey(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"telegram:user1:main", "telegram_user1_main"},
		{"direct/localhost/main", "direct_localhost_main"},
		{"simple", "simple"},
	}
	for _, tt := range tests {
		got := sanitizeSessionKey(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSessionKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSanitizeToolName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"read_file", "read_file"},
		{"exec", "exec"},
		{"../bad", "bad"},
		{"", "tool"},
	}
	for _, tt := range tests {
		got := sanitizeToolName(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeToolName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
