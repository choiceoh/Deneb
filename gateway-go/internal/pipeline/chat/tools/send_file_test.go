package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

func TestArchiveSentFile_PersistsToStore(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir()) // redirect the store off the real ~/.deneb
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "") // default on

	src := filepath.Join(t.TempDir(), "report.txt")
	if err := os.WriteFile(src, []byte("hello deneb"), 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	info, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat src: %v", err)
	}

	vpath := archiveSentFile(context.Background(), src, info.Size())
	if vpath == "" {
		t.Fatal("archiveSentFile returned empty vpath, want a store path")
	}
	if !strings.HasPrefix(vpath, "/전송/") || !strings.HasSuffix(vpath, "report.txt") {
		t.Fatalf("vpath = %q, want /전송/<date>/report.txt", vpath)
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := store.Stat(context.Background(), vpath); err != nil {
		t.Fatalf("archived file not found in store at %s: %v", vpath, err)
	}
}

func TestArchiveSentFile_DisabledByEnv(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "0")

	src := filepath.Join(t.TempDir(), "x.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if v := archiveSentFile(context.Background(), src, 1); v != "" {
		t.Fatalf("archive ran while disabled, vpath=%q", v)
	}
}

func TestArchiveSentFile_SkipsMissingAndOversized(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	t.Setenv("DENEB_ARCHIVE_SENT_FILES", "")

	if v := archiveSentFile(context.Background(), "/nonexistent/file.txt", 10); v != "" {
		t.Fatalf("archive returned %q for a missing file", v)
	}
	src := filepath.Join(t.TempDir(), "big.bin")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if v := archiveSentFile(context.Background(), src, sentFileArchiveMaxBytes+1); v != "" {
		t.Fatalf("archive returned %q for an oversized file", v)
	}
}
