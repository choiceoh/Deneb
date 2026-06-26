package web

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

func TestFetchedDocVPath(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a/b/report.pdf?x=1": "/web/example.com/report.pdf",
		"https://host.org/file.docx":             "/web/host.org/file.docx",
		"not a url":                              "/web/web/not a url", // no host → "web"; documentName keeps the tail
	}
	for in, want := range cases {
		if got := fetchedDocVPath(in); got != want {
			t.Errorf("fetchedDocVPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArchiveFetchedDocument_PersistsToStore(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir()) // redirect store off the real ~/.deneb
	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "")

	archiveFetchedDocument(context.Background(), "https://example.com/papers/x.pdf", []byte("%PDF-1.4 body"))

	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := store.Stat(context.Background(), "/web/example.com/x.pdf"); err != nil {
		t.Fatalf("fetched doc not archived at /web/example.com/x.pdf: %v", err)
	}
}

func TestArchiveFetchedDocument_DisabledAndOversized(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())

	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "0")
	archiveFetchedDocument(context.Background(), "https://x.com/y.pdf", []byte("data"))

	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "")
	archiveFetchedDocument(context.Background(), "https://x.com/big.pdf", make([]byte, fetchedDocArchiveMaxBytes+1))

	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := store.Stat(context.Background(), "/web/x.com/y.pdf"); err == nil {
		t.Fatal("archived while disabled")
	}
	if _, err := store.Stat(context.Background(), "/web/x.com/big.pdf"); err == nil {
		t.Fatal("archived an oversized payload")
	}
}
