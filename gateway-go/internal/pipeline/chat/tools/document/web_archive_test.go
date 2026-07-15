package document

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
)

func TestFetchedWebDocVPathFormatsVirtualPath(t *testing.T) {
	cases := map[string]string{
		"https://example.com/a/b/report.pdf?x=1": "/web/example.com/report.pdf",
		"https://host.org/file.docx":             "/web/host.org/file.docx",
		"not a url":                              "/web/web/not a url",
	}
	for in, want := range cases {
		if got := fetchedWebDocVPath(in); got != want {
			t.Errorf("fetchedWebDocVPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArchiveFetchedWebDocSavesToStore(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "")

	ArchiveFetchedWebDoc(context.Background(), "https://example.com/papers/x.pdf", []byte("%PDF-1.4 body"))

	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if _, err := store.Stat(context.Background(), "/web/example.com/x.pdf"); err != nil {
		t.Fatalf("fetched doc not archived at /web/example.com/x.pdf: %v", err)
	}
}

func TestArchiveFetchedWebDocSkipsWhenDisabledOrOversized(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", t.TempDir())

	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "0")
	ArchiveFetchedWebDoc(context.Background(), "https://x.com/y.pdf", []byte("data"))

	t.Setenv("DENEB_ARCHIVE_FETCHED_DOCS", "")
	ArchiveFetchedWebDoc(context.Background(), "https://x.com/big.pdf", make([]byte, fetchedWebDocArchiveMaxBytes+1))

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
