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
	if resp.OK || resp.Error == nil || resp.Error.Code != protocol.ErrInvalidParams {
		t.Fatalf("want INVALID_PARAMS, got %+v", resp)
	}
}
