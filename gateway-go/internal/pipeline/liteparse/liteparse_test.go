package liteparse

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func forceAvailable(t *testing.T, available bool) {
	t.Helper()
	availableOnce = sync.Once{}
	availableOnce.Do(func() { availableVal = available })
	t.Cleanup(func() {
		availableOnce = sync.Once{}
		availableVal = false
	})
}

func installFakeLit(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "lit")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	availableOnce = sync.Once{}
	t.Cleanup(func() {
		availableOnce = sync.Once{}
		availableVal = false
	})
}

func TestParseValidatesAvailabilityAndInput(t *testing.T) {
	forceAvailable(t, false)
	if _, err := Parse(context.Background(), []byte("doc"), "a.pdf"); err == nil || !strings.Contains(err.Error(), "lit CLI not found") {
		t.Fatalf("missing CLI error = %v", err)
	}

	forceAvailable(t, true)
	if _, err := Parse(context.Background(), nil, "a.pdf"); err == nil || !strings.Contains(err.Error(), "empty document") {
		t.Fatalf("empty input error = %v", err)
	}
	if _, err := Parse(context.Background(), make([]byte, maxDocumentSize+1), "a.pdf"); err == nil || !strings.Contains(err.Error(), "document too large") {
		t.Fatalf("large input error = %v", err)
	}
}

func TestParsePreservesExtensionAndTrimsOutput(t *testing.T) {
	installFakeLit(t, `case "$2" in *.pdf) printf '  parsed pdf  ';; *) exit 9;; esac`)
	got, err := Parse(context.Background(), []byte("document"), "Quarterly.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if got != "parsed pdf" {
		t.Fatalf("Parse() = %q", got)
	}
}

func TestParseReturnsPartialOutputOnCommandFailure(t *testing.T) {
	installFakeLit(t, "printf 'partial result'; printf 'bad input' >&2; exit 1")
	got, err := Parse(context.Background(), []byte("document"), "note.txt")
	if err != nil {
		t.Fatalf("partial output should be usable: %v", err)
	}
	if got != "partial result" {
		t.Fatalf("Parse() = %q", got)
	}
}
