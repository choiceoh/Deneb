package fileapi

import (
	"context"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
)

func TestDownloadRequiresPathAndAuthorization(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h := New(nil)

	tests := []struct {
		name     string
		query    url.Values
		want     int
		contains string
	}{
		{name: "missing path", query: url.Values{}, want: http.StatusBadRequest, contains: "missing path"},
		{
			name:     "missing auth",
			query:    url.Values{"path": {"/x"}},
			want:     http.StatusUnauthorized,
			contains: "missing client token or share signature",
		},
		{
			name:     "invalid client token",
			query:    url.Values{"path": {"/x"}, "clientToken": {"wrong"}},
			want:     http.StatusUnauthorized,
			contains: "invalid client token",
		},
		{
			name:     "invalid share signature",
			query:    url.Values{"path": {"/x"}, "exp": {strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10)}, "sig": {"bad"}},
			want:     http.StatusForbidden,
			contains: "invalid or expired share link",
		},
		{
			name:     "authorized missing file",
			query:    url.Values{"path": {"/x"}, "clientToken": {token}},
			want:     http.StatusNotFound,
			contains: "file not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/files/download?"+tc.query.Encode(), nil)
			h.Download(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("body = %q, missing %q", rec.Body.String(), tc.contains)
			}
		})
	}
}

func TestDownloadStreamsFileWithClientTokenOrShareSignature(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	t.Setenv("DENEB_FILES_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "/docs/report.txt", []byte("payload"), true); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		query url.Values
	}{
		{
			name:  "client token",
			query: url.Values{"path": {"/docs/report.txt"}, "clientToken": {token}},
		},
		{
			name: "share signature",
			query: func() url.Values {
				exp := time.Now().Add(time.Hour).Unix()
				return url.Values{
					"path": {"/docs/report.txt"},
					"exp":  {strconv.FormatInt(exp, 10)},
					"sig":  {fileshare.Sign("/docs/report.txt", exp)},
				}
			}(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/files/download?"+tc.query.Encode(), nil)
			New(nil).Download(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
			}
			if rec.Body.String() != "payload" {
				t.Fatalf("body = %q", rec.Body.String())
			}
			if rec.Header().Get("Content-Type") != "application/octet-stream" {
				t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
			}
			_, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
			if err != nil || params["filename"] != "report.txt" {
				t.Fatalf("disposition = %q params=%v err=%v", rec.Header().Get("Content-Disposition"), params, err)
			}
		})
	}
}
