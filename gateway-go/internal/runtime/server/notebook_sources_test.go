package server

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestNotebookFetchURLRejectsInternal(t *testing.T) {
	// IP-literal internal targets are rejected by coresecurity.IsSafeURL before
	// any network call (no DNS needed), so this runs offline. The SSRF-safe
	// dialer in web.FetchRaw is the connect-time backstop (incl. redirects).
	for _, bad := range []string{
		"http://169.254.169.254/latest/meta-data/", // cloud metadata
		"http://127.0.0.1:8000/v1/models",          // loopback sidecar
		"http://[::1]:18789/",                      // loopback IPv6
		"file:///etc/passwd",                       // non-http scheme
		"ftp://example.com/x",                      // non-http scheme
	} {
		if _, err := notebookFetchURL(context.Background(), bad); err == nil {
			t.Fatalf("expected %q to be rejected as unsafe/unsupported", bad)
		}
	}
}

func TestNotebookReadDiary(t *testing.T) {
	diaryDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(diaryDir, "2026-06-22.md"), []byte("오늘 탑솔라 미팅."), 0o600); err != nil {
		t.Fatalf("write diary file: %v", err)
	}
	store, err := wiki.NewStore(t.TempDir(), diaryDir)
	if err != nil {
		t.Fatalf("wiki.NewStore: %v", err)
	}

	// A bare date ref resolves to <date>.md.
	got, err := notebookReadDiary(store, "2026-06-22")
	if err != nil || !strings.Contains(got, "탑솔라 미팅") {
		t.Fatalf("read diary by date: err=%v text=%q", err, got)
	}
	// filepath.Base blocks traversal — "../x" can never escape the diary dir.
	if _, err := notebookReadDiary(store, "../secret"); err == nil {
		t.Fatal("traversal ref must not resolve to a real file")
	}
	// Missing entry and nil store both error gracefully.
	if _, err := notebookReadDiary(store, "2099-01-01"); err == nil {
		t.Fatal("missing diary entry should error")
	}
	if _, err := notebookReadDiary(nil, "2026-06-22"); err == nil {
		t.Fatal("nil store should error")
	}
}
