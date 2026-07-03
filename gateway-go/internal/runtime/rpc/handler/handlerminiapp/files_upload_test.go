package handlerminiapp

import (
	"context"
	"encoding/base64"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// captureUploadStore records the overwrite flag the handler passes to Put so
// the wire→store plumbing of the editor-save flag is under test (Put's own
// overwrite behavior is covered in filestore/local_test.go).
type captureUploadStore struct {
	filestore.Store
	seenOverwrite bool
	seenPath      string
}

func (c *captureUploadStore) Put(_ context.Context, p string, _ []byte, overwrite bool) (*filestore.Entry, error) {
	c.seenPath = p
	c.seenOverwrite = overwrite
	return &filestore.Entry{Name: "x", PathDisplay: p}, nil
}

func TestFilesBrowseUpload_OverwriteFlagReachesStore(t *testing.T) {
	for _, tc := range []struct {
		name     string
		params   map[string]any
		wantOver bool
		wantPath string
	}{
		{
			name:     "default is a non-clobbering capture upload",
			params:   map[string]any{"path": "메일/견적서.pdf", "dataBase64": base64.StdEncoding.EncodeToString([]byte("pdf"))},
			wantOver: false,
			wantPath: "메일/견적서.pdf",
		},
		{
			name:     "overwrite=true is the editor save (replace in place)",
			params:   map[string]any{"path": "노트/메모.md", "dataBase64": base64.StdEncoding.EncodeToString([]byte("# 메모")), "overwrite": true},
			wantOver: true,
			wantPath: "노트/메모.md",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &captureUploadStore{}
			h := filesBrowseUpload(FilesBrowseDeps{Store: store})
			resp := h(authedCtx(), reqWith(t, "miniapp.files.upload", tc.params))
			if !resp.OK {
				t.Fatalf("upload failed: %+v", resp.Error)
			}
			if store.seenOverwrite != tc.wantOver {
				t.Errorf("overwrite passed to Put = %v, want %v", store.seenOverwrite, tc.wantOver)
			}
			if store.seenPath != tc.wantPath {
				t.Errorf("path passed to Put = %q, want %q", store.seenPath, tc.wantPath)
			}
		})
	}
}

// TestFilesBrowseUpload_RejectsBadBase64 guards the decode error path.
func TestFilesBrowseUpload_RejectsBadBase64(t *testing.T) {
	h := filesBrowseUpload(FilesBrowseDeps{Store: &captureUploadStore{}})
	resp := h(authedCtx(), reqWith(t, "miniapp.files.upload", map[string]any{"path": "x", "dataBase64": "!!!notbase64!!!"}))
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("want INVALID_REQUEST, got %+v", resp)
	}
}

// TestFilesBrowseUpload_EmptyPayloadSemantics pins the absent-vs-empty
// distinction: an ABSENT dataBase64 is a client bug and is rejected even with
// overwrite=true (it used to silently truncate the target file to zero
// bytes), while an explicit empty string is a legitimate editor save clearing
// a text file — but only on the overwrite path.
func TestFilesBrowseUpload_EmptyPayloadSemantics(t *testing.T) {
	run := func(t *testing.T, params map[string]any) (*captureUploadStore, *protocol.ResponseFrame) {
		t.Helper()
		store := &captureUploadStore{}
		h := filesBrowseUpload(FilesBrowseDeps{Store: store})
		return store, h(authedCtx(), reqWith(t, "miniapp.files.upload", params))
	}

	t.Run("absent dataBase64 with overwrite is rejected", func(t *testing.T) {
		store, resp := run(t, map[string]any{"path": "노트/메모.md", "overwrite": true})
		if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
			t.Fatalf("want MISSING_PARAM, got %+v", resp)
		}
		if store.seenPath != "" {
			t.Error("Put must not run for an absent payload")
		}
	})

	t.Run("absent dataBase64 without overwrite is rejected", func(t *testing.T) {
		if _, resp := run(t, map[string]any{"path": "노트/메모.md"}); resp.OK {
			t.Fatalf("want rejection, got OK: %+v", resp)
		}
	})

	t.Run("explicit empty string with overwrite is a legitimate clear-save", func(t *testing.T) {
		store, resp := run(t, map[string]any{"path": "노트/메모.md", "dataBase64": "", "overwrite": true})
		if !resp.OK {
			t.Fatalf("clear-save failed: %+v", resp.Error)
		}
		if store.seenPath != "노트/메모.md" || !store.seenOverwrite {
			t.Errorf("Put(path=%q, overwrite=%v), want the in-place zero-byte save", store.seenPath, store.seenOverwrite)
		}
	})

	t.Run("explicit empty string without overwrite is a botched capture", func(t *testing.T) {
		if _, resp := run(t, map[string]any{"path": "노트/메모.md", "dataBase64": ""}); resp.OK {
			t.Fatalf("want rejection, got OK: %+v", resp)
		}
	})
}

// TestFilesBrowseUpload_OverwriteRefreshesSemanticIndex: an in-place replace
// must drop the path's stale vectors (OnUpload → index Remove) so semantic
// search stops ranking the file by its old content; a fresh capture upload
// must not fire the hook.
func TestFilesBrowseUpload_OverwriteRefreshesSemanticIndex(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte("new content"))
	var dropped []string
	deps := FilesBrowseDeps{
		Store:    &captureUploadStore{},
		OnUpload: func(path string) { dropped = append(dropped, path) },
	}
	h := filesBrowseUpload(deps)

	if resp := h(authedCtx(), reqWith(t, "miniapp.files.upload",
		map[string]any{"path": "노트/메모.md", "dataBase64": payload, "overwrite": true})); !resp.OK {
		t.Fatalf("overwrite save failed: %+v", resp.Error)
	}
	if len(dropped) != 1 || dropped[0] != "노트/메모.md" {
		t.Errorf("OnUpload calls = %v, want the overwritten path once", dropped)
	}

	dropped = nil
	if resp := h(authedCtx(), reqWith(t, "miniapp.files.upload",
		map[string]any{"path": "메일/신규.pdf", "dataBase64": payload})); !resp.OK {
		t.Fatalf("capture upload failed: %+v", resp.Error)
	}
	if len(dropped) != 0 {
		t.Errorf("OnUpload calls = %v, want none for a fresh capture upload", dropped)
	}
}
