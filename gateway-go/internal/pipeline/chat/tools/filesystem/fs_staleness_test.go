package filesystem

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func callToolCtx(ctx context.Context, t *testing.T, fn toolport.ToolFunc, params any) (string, error) {
	t.Helper()
	raw := testutil.Must(json.Marshal(params))
	return fn(ctx, json.RawMessage(raw))
}

// bumpMTime moves the file's mtime forward so staleness checks cannot be
// defeated by coarse filesystem timestamp granularity.
func bumpMTime(t *testing.T, path string) {
	t.Helper()
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
}

// A partial (offset/limit) read must still arm the modified-since-read guard:
// previously only default full reads populated the cache, so editing a large
// file seen through offset/limit had no staleness protection at all.
func TestEditBlockedAfterPartialReadWhenFileChangedExternally(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	original := "alpha\nbravo\ncharlie\ndelta\necho\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithFileCache(context.Background(), agent.NewFileCache(0))

	if _, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{
		"file_path": "notes.txt", "offset": 2, "limit": 2,
	}); err != nil {
		t.Fatalf("partial read: %v", err)
	}

	// External writer changes the file after our read.
	if err := os.WriteFile(path, []byte("alpha\nbravo\nCHANGED\ndelta\necho\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bumpMTime(t, path)

	_, err := callToolCtx(ctx, t, ToolEdit(tmp), map[string]any{
		"file_path": "notes.txt", "old_string": "delta", "new_string": "DELTA",
	})
	if err == nil || !strings.Contains(err.Error(), "modified since last read") {
		t.Fatalf("edit after external change must be blocked with a re-read hint, got: %v", err)
	}
}

// An unchanged file stays editable after a partial read — the new baseline
// must not over-block the normal read→edit flow.
func TestEditAllowedAfterPartialReadWhenFileUnchanged(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\nbravo\ncharlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithFileCache(context.Background(), agent.NewFileCache(0))

	if _, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{
		"file_path": "notes.txt", "offset": 1, "limit": 2,
	}); err != nil {
		t.Fatalf("partial read: %v", err)
	}
	out, err := callToolCtx(ctx, t, ToolEdit(tmp), map[string]any{
		"file_path": "notes.txt", "old_string": "bravo", "new_string": "BRAVO",
	})
	if err != nil {
		t.Fatalf("edit after clean partial read: %v", err)
	}
	if !strings.Contains(out, "Edited") {
		t.Fatalf("unexpected edit result: %q", out)
	}
}

// Reading a file again after editing it must serve the post-edit content.
// UpdateAfterWrite used to refresh mtime/hash but keep the pre-edit rendered
// output, so the dedup cache replayed stale content on the next default read.
func TestReadAfterEditServesFreshContent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\nbravo\ncharlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithFileCache(context.Background(), agent.NewFileCache(0))

	if _, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{"file_path": "notes.txt"}); err != nil {
		t.Fatalf("initial read: %v", err)
	}
	if _, err := callToolCtx(ctx, t, ToolEdit(tmp), map[string]any{
		"file_path": "notes.txt", "old_string": "bravo", "new_string": "EDITED-LINE",
	}); err != nil {
		t.Fatalf("edit: %v", err)
	}
	out, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{"file_path": "notes.txt"})
	if err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if !strings.Contains(out, "EDITED-LINE") || strings.Contains(out, "bravo") {
		t.Fatalf("re-read after edit must serve post-edit content, got:\n%s", out)
	}
}

// A staleness-only baseline (from a partial read) must never be served as a
// cached full read: the next default read hits the disk.
func TestPartialReadBaselineIsNotServedAsCachedRead(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "notes.txt")
	if err := os.WriteFile(path, []byte("alpha\nbravo\ncharlie\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithFileCache(context.Background(), agent.NewFileCache(0))

	if _, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{
		"file_path": "notes.txt", "offset": 1, "limit": 1,
	}); err != nil {
		t.Fatalf("partial read: %v", err)
	}
	out, err := callToolCtx(ctx, t, ToolRead(tmp), map[string]any{"file_path": "notes.txt"})
	if err != nil {
		t.Fatalf("full read: %v", err)
	}
	if !strings.Contains(out, "charlie") {
		t.Fatalf("full read after partial read must serve the whole file, got:\n%s", out)
	}
}
