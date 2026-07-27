package groupwareapi

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func TestApprovalAttachmentGuardBoundary(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, token, docID, attachment string
		download                       AttachmentDownload
		want                           int
		contains                       string
	}{
		{name: "missing token", want: http.StatusUnauthorized, contains: "missing client token"},
		{name: "invalid token", token: "wrong", want: http.StatusUnauthorized, contains: "invalid client token"},
		{name: "download unavailable", token: token, docID: "d1", attachment: "1", want: http.StatusServiceUnavailable, contains: "download unavailable"},
		{name: "missing doc id", token: token, attachment: "1", download: noopDownload, want: http.StatusBadRequest, contains: "missing docId"},
		{name: "missing attachment", token: token, docID: "d1", download: noopDownload, want: http.StatusBadRequest, contains: "missing docId"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(Config{Download: tc.download})
			rec := httptest.NewRecorder()
			h.ApprovalAttachment(rec, httptest.NewRequest(http.MethodGet, "/attachment?"+approvalAttachmentQuery(tc.token, tc.docID, tc.attachment, "", ""), nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Fatalf("body = %q, missing %q", rec.Body.String(), tc.contains)
			}
		})
	}
}

func noopDownload(context.Context, string, string) (string, string, []byte, error) {
	return "", "", nil, nil
}

func TestApprovalAttachmentStreamsSanitizedDownload(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	var seenDocID, seenAttachment string
	h := New(Config{
		Download: func(_ context.Context, docID, attachment string) (string, string, []byte, error) {
			seenDocID = docID
			seenAttachment = attachment
			return "../server-name.txt", "text/plain; charset=utf-8", []byte("payload"), nil
		},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/attachment?"+approvalAttachmentQuery(token, "doc-1", "2", "client-name.pdf", "application/pdf; charset=binary"), nil)
	h.ApprovalAttachment(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if seenDocID != "doc-1" || seenAttachment != "2" {
		t.Fatalf("download args = %q/%q", seenDocID, seenAttachment)
	}
	if rec.Body.String() != "payload" {
		t.Fatalf("body = %q", rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/pdf" {
		t.Fatalf("content type = %q", rec.Header().Get("Content-Type"))
	}
	_, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
	if err != nil || params["filename"] != "client-name.pdf" {
		t.Fatalf("disposition = %q params=%v err=%v", rec.Header().Get("Content-Disposition"), params, err)
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff missing")
	}
}

func TestApprovalAttachmentErrorBoundary(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		err  error
		data []byte
		want int
	}{
		{name: "not found", err: errors.New("not found"), want: http.StatusNotFound},
		{name: "forbidden", err: errors.New("forbidden"), want: http.StatusForbidden},
		{name: "invalid", err: errors.New("invalid selector"), want: http.StatusBadRequest},
		{name: "upstream", err: errors.New("timeout"), want: http.StatusBadGateway},
		{name: "empty", data: nil, want: http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(Config{
				Download: func(context.Context, string, string) (string, string, []byte, error) {
					return "file.txt", "text/plain", tc.data, tc.err
				},
			})
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/attachment?"+approvalAttachmentQuery(token, "d1", "1", "", ""), nil)
			h.ApprovalAttachment(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func approvalAttachmentQuery(token, docID, attachment, filename, mimeType string) string {
	values := url.Values{}
	if token != "" {
		values.Set("clientToken", token)
	}
	if docID != "" {
		values.Set("docId", docID)
	}
	if attachment != "" {
		values.Set("attachment", attachment)
	}
	if filename != "" {
		values.Set("filename", filename)
	}
	if mimeType != "" {
		values.Set("mimeType", mimeType)
	}
	return values.Encode()
}
