package googledrive

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClientAt builds a Client pointed at a local test server with a fixed
// token, bypassing the credential file + OAuth refresh.
func newTestClientAt(t *testing.T, base string) *Client {
	t.Helper()
	return &Client{
		httpClient: http.DefaultClient,
		baseURL:    base,
		tokenFn:    func(context.Context) (string, error) { return "test-token", nil },
	}
}

// TestListFolderFilesQueryAndParse verifies the folder query is well-formed and
// the JSON response parses into File metadata. It stubs the HTTP layer via a
// test server and a token-free request path.
func TestListFolderFilesQueryAndParse(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"files":[
			{"id":"1","name":"회의.pdf","mimeType":"application/pdf","modifiedTime":"2026-07-12T10:00:00Z"}
		]}`))
	}))
	defer srv.Close()

	c := newTestClientAt(t, srv.URL)
	files, err := c.ListFolderFiles(context.Background(), "FOLDER", "2026-07-12T09:00:00Z")
	if err != nil {
		t.Fatalf("ListFolderFiles: %v", err)
	}
	if len(files) != 1 || files[0].Name != "회의.pdf" || files[0].ID != "1" {
		t.Fatalf("files=%+v", files)
	}
	if !strings.Contains(gotQuery, "'FOLDER' in parents") ||
		!strings.Contains(gotQuery, "trashed = false") ||
		!strings.Contains(gotQuery, "modifiedTime > '2026-07-12T09:00:00Z'") {
		t.Errorf("malformed query: %q", gotQuery)
	}
}

func TestDownloadFileBounds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") != "media" {
			t.Errorf("download missing alt=media: %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("PDF-BYTES"))
	}))
	defer srv.Close()

	c := newTestClientAt(t, srv.URL)
	data, err := c.DownloadFile(context.Background(), "file1")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "PDF-BYTES" {
		t.Errorf("data=%q", data)
	}
}

func TestListEmptyFolderIDRejected(t *testing.T) {
	c := &Client{httpClient: http.DefaultClient}
	if _, err := c.ListFolderFiles(context.Background(), "", ""); err == nil {
		t.Error("empty folder ID must be rejected")
	}
	if _, err := c.DownloadFile(context.Background(), ""); err == nil {
		t.Error("empty file ID must be rejected")
	}
}
