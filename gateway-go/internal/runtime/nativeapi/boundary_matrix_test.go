package nativeapi

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

func TestSanitizeAttachmentFilenameBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw, want string }{
		{
			name: "empty",
			raw:  "",
			want: "attachment",
		},
		{
			name: "spaces",
			raw:  "   ",
			want: "attachment",
		},
		{
			name: "dot",
			raw:  ".",
			want: "attachment",
		},
		{
			name: "dot dot",
			raw:  "..",
			want: "attachment",
		},
		{
			name: "slash",
			raw:  "/",
			want: "attachment",
		},
		{
			name: "backslash",
			raw:  "\\",
			want: "attachment",
		},
		{
			name: "unix basename",
			raw:  "../secret.txt",
			want: "secret.txt",
		},
		{
			name: "nested unix",
			raw:  "a/b/c.pdf",
			want: "c.pdf",
		},
		{
			name: "windows basename",
			raw:  "C:\\Users\\x\\file.docx",
			want: "file.docx",
		},
		{
			name: "mixed separators",
			raw:  "a\\b/c.txt",
			want: "c.txt",
		},
		{
			name: "leading dot",
			raw:  " .hidden ",
			want: "hidden",
		},
		{
			name: "trailing dot",
			raw:  "file.txt.",
			want: "file.txt",
		},
		{
			name: "leading spaces",
			raw:  "  file.txt",
			want: "file.txt",
		},
		{
			name: "trailing spaces",
			raw:  "file.txt  ",
			want: "file.txt",
		},
		{
			name: "control nul",
			raw:  "a\u0000b.txt",
			want: "ab.txt",
		},
		{
			name: "control tab",
			raw:  "a\tb.txt",
			want: "ab.txt",
		},
		{
			name: "control newline",
			raw:  "a\nb.txt",
			want: "ab.txt",
		},
		{
			name: "delete",
			raw:  "ab.txt",
			want: "ab.txt",
		},
		{
			name: "unicode",
			raw:  "한글 문서.pdf",
			want: "한글 문서.pdf",
		},
		{
			name: "emoji",
			raw:  "🚀.png",
			want: "🚀.png",
		},
		{
			name: "quotes",
			raw:  "say\"hi\".txt",
			want: "say\"hi\".txt",
		},
		{
			name: "semicolon",
			raw:  "a;b.txt",
			want: "a;b.txt",
		},
		{
			name: "comma",
			raw:  "a,b.txt",
			want: "a,b.txt",
		},
		{
			name: "brackets",
			raw:  "[a].txt",
			want: "[a].txt",
		},
		{
			name: "only dots",
			raw:  "...",
			want: "attachment",
		},
		{
			name: "dot spaced",
			raw:  " . . ",
			want: "attachment",
		},
		{
			name: "path ending slash",
			raw:  "folder/",
			want: "attachment",
		},
		{
			name: "path ending backslash",
			raw:  "folder\\",
			want: "attachment",
		},
		{
			name: "relative current",
			raw:  "./file.txt",
			want: "file.txt",
		},
		{
			name: "relative parent",
			raw:  "../../file.txt",
			want: "file.txt",
		},
		{
			name: "double slash",
			raw:  "a//b.txt",
			want: "b.txt",
		},
		{
			name: "double backslash",
			raw:  "a\\\\b.txt",
			want: "b.txt",
		},
		{name: "max runes", raw: strings.Repeat("가", 180), want: strings.Repeat("가", 180)},
		{name: "over max runes", raw: strings.Repeat("나", 181), want: strings.Repeat("나", 180)},
		{name: "long ascii", raw: strings.Repeat("x", 1000), want: strings.Repeat("x", 180)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizeAttachmentFilename(tc.raw); got != tc.want {
				t.Fatalf("sanitize(%q)=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSafeAttachmentContentTypeBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, raw, want string }{
		{
			name: "empty",
			raw:  "",
			want: "application/octet-stream",
		},
		{
			name: "spaces",
			raw:  "   ",
			want: "application/octet-stream",
		},
		{
			name: "plain",
			raw:  "text/plain",
			want: "text/plain",
		},
		{
			name: "plain charset",
			raw:  "text/plain; charset=utf-8",
			want: "text/plain",
		},
		{
			name: "html",
			raw:  "text/html",
			want: "text/html",
		},
		{
			name: "json",
			raw:  "application/json",
			want: "application/json",
		},
		{
			name: "pdf",
			raw:  "application/pdf",
			want: "application/pdf",
		},
		{
			name: "jpeg",
			raw:  "image/jpeg",
			want: "image/jpeg",
		},
		{
			name: "png",
			raw:  "image/png",
			want: "image/png",
		},
		{
			name: "zip",
			raw:  "application/zip",
			want: "application/zip",
		},
		{
			name: "mixed case",
			raw:  "TEXT/PLAIN",
			want: "text/plain",
		},
		{
			name: "padded",
			raw:  " text/plain ",
			want: "text/plain",
		},
		{
			name: "quoted charset",
			raw:  "text/plain; charset=\"utf-8\"",
			want: "text/plain",
		},
		{
			name: "invalid word",
			raw:  "invalid",
			want: "application/octet-stream",
		},
		{
			name: "missing subtype",
			raw:  "text/",
			want: "application/octet-stream",
		},
		{
			name: "missing type",
			raw:  "/plain",
			want: "application/octet-stream",
		},
		{
			name: "double slash",
			raw:  "text//plain",
			want: "application/octet-stream",
		},
		{
			name: "newline",
			raw:  "text/plain\nx",
			want: "application/octet-stream",
		},
		{
			name: "nul",
			raw:  "text/plain\u0000",
			want: "application/octet-stream",
		},
		{
			name: "bad parameter",
			raw:  "text/plain; bad",
			want: "application/octet-stream",
		},
		{
			name: "wildcard",
			raw:  "*/*",
			want: "*/*",
		},
		{
			name: "vendor",
			raw:  "application/vnd.test+json",
			want: "application/vnd.test+json",
		},
		{
			name: "multipart",
			raw:  "multipart/form-data; boundary=x",
			want: "multipart/form-data",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := safeAttachmentContentType(tc.raw); got != tc.want {
				t.Fatalf("safe MIME(%q)=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestAttachmentErrorStatusBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "nil",
			err:  nil,
			want: 502,
		},
		{
			name: "not found",
			err:  errors.New("not found"),
			want: 404,
		},
		{
			name: "not found upper",
			err:  errors.New("NOT FOUND"),
			want: 404,
		},
		{
			name: "404",
			err:  errors.New("upstream 404"),
			want: 404,
		},
		{
			name: "404 embedded",
			err:  errors.New("error code=4040"),
			want: 404,
		},
		{
			name: "forbidden",
			err:  errors.New("forbidden"),
			want: 403,
		},
		{
			name: "forbidden upper",
			err:  errors.New("FORBIDDEN"),
			want: 403,
		},
		{
			name: "403",
			err:  errors.New("status 403"),
			want: 403,
		},
		{
			name: "invalid",
			err:  errors.New("invalid attachment"),
			want: 400,
		},
		{
			name: "invalid upper",
			err:  errors.New("INVALID"),
			want: 400,
		},
		{
			name: "400",
			err:  errors.New("status 400"),
			want: 400,
		},
		{
			name: "timeout",
			err:  errors.New("timeout"),
			want: 502,
		},
		{
			name: "unauthorized",
			err:  errors.New("unauthorized"),
			want: 502,
		},
		{
			name: "401",
			err:  errors.New("status 401"),
			want: 502,
		},
		{
			name: "server error",
			err:  errors.New("500"),
			want: 502,
		},
		{
			name: "empty",
			err:  errors.New(""),
			want: 502,
		},
		{
			name: "network",
			err:  errors.New("connection refused"),
			want: 502,
		},
		{
			name: "precedence 404 403",
			err:  errors.New("404 forbidden"),
			want: 404,
		},
		{
			name: "precedence 403 400",
			err:  errors.New("403 invalid"),
			want: 403,
		},
		{
			name: "wrapped",
			err:  errors.New("outer: not found"),
			want: 404,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := statusForMiniappGmailAttachmentError(tc.err); got != tc.want {
				t.Fatalf("status(%v)=%d want=%d", tc.err, got, tc.want)
			}
		})
	}
}

func TestClientAcceptsGzipBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, header string
		want         bool
	}{
		{
			name:   "empty",
			header: "",
			want:   false,
		},
		{
			name:   "gzip",
			header: "gzip",
			want:   true,
		},
		{
			name:   "upper",
			header: "GZIP",
			want:   true,
		},
		{
			name:   "mixed",
			header: "GZip",
			want:   true,
		},
		{
			name:   "padded",
			header: " gzip ",
			want:   true,
		},
		{
			name:   "deflate",
			header: "deflate",
			want:   false,
		},
		{
			name:   "br",
			header: "br",
			want:   false,
		},
		{
			name:   "x gzip",
			header: "x-gzip",
			want:   false,
		},
		{
			name:   "gzip list first",
			header: "gzip, deflate",
			want:   true,
		},
		{
			name:   "gzip list last",
			header: "br, gzip",
			want:   true,
		},
		{
			name:   "gzip list middle",
			header: "br, gzip, deflate",
			want:   true,
		},
		{
			name:   "quality one",
			header: "gzip;q=1",
			want:   true,
		},
		{
			name:   "quality zero",
			header: "gzip;q=0",
			want:   true,
		},
		{
			name:   "quality decimal",
			header: "gzip; q=0.5",
			want:   true,
		},
		{
			name:   "wildcard",
			header: "*",
			want:   false,
		},
		{
			name:   "empty tokens",
			header: ", ,",
			want:   false,
		},
		{
			name:   "substring",
			header: "notgzip",
			want:   false,
		},
		{
			name:   "parameter token",
			header: "gzip;level=9",
			want:   true,
		},
		{
			name:   "tabbed",
			header: "\tgzip\t",
			want:   true,
		},
		{
			name:   "duplicate",
			header: "gzip, gzip",
			want:   true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.header != "" {
				req.Header.Set("Accept-Encoding", tc.header)
			}
			if got := clientAcceptsGzip(req); got != tc.want {
				t.Fatalf("accepts(%q)=%v want=%v", tc.header, got, tc.want)
			}
		})
	}
}

func TestNativeRPCGuardBoundaryMatrix(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{})
	tests := []struct {
		name, token, body string
		want              int
		contains          string
	}{
		{name: "missing auth", token: "", body: `{}`, want: 401, contains: "missing client token"},
		{name: "invalid auth", token: "wrong", body: `{}`, want: 401, contains: "invalid client token"},
		{name: "empty body", token: token, body: "", want: 400, contains: "empty body"},
		{name: "invalid json", token: token, body: `{`, want: 400, contains: "invalid frame"},
		{name: "empty object", token: token, body: `{}`, want: 400, contains: "missing id or method"},
		{name: "missing id", token: token, body: `{"method":"miniapp.test"}`, want: 400, contains: "missing id or method"},
		{name: "missing method", token: token, body: `{"id":"1"}`, want: 400, contains: "missing id or method"},
		{name: "outside namespace", token: token, body: `{"id":"1","method":"admin.delete"}`, want: 403, contains: "outside miniapp"},
		{name: "near namespace", token: token, body: `{"id":"1","method":"miniapplication.test"}`, want: 403, contains: "outside miniapp"},
		{name: "dispatcher unavailable", token: token, body: `{"id":"1","method":"miniapp.test"}`, want: 503, contains: "dispatcher not ready"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/miniapp/rpc", strings.NewReader(tc.body))
			if tc.token != "" {
				req.Header.Set(clientauth.Header, tc.token)
			}
			rec := httptest.NewRecorder()
			h.RPC(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Errorf("body=%q missing %q", rec.Body.String(), tc.contains)
			}
		})
	}
}

func TestGmailAttachmentGuardBoundaryMatrix(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	h := New(Config{})
	tests := []struct {
		name, token, message, attachment string
		want                             int
		contains                         string
	}{
		{name: "missing token", want: 401, contains: "missing client token"},
		{name: "invalid token", token: "wrong", want: 401, contains: "invalid client token"},
		{name: "missing both ids", token: token, want: 400, contains: "missing messageId"},
		{name: "missing message", token: token, attachment: "a", want: 400, contains: "missing messageId"},
		{name: "missing attachment", token: token, message: "m", want: 400, contains: "missing messageId"},
		{name: "factory unavailable", token: token, message: "m", attachment: "a", want: 503, contains: "client unavailable"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			values := url.Values{}
			if tc.token != "" {
				values.Set("clientToken", tc.token)
			}
			if tc.message != "" {
				values.Set("messageId", tc.message)
			}
			if tc.attachment != "" {
				values.Set("attachmentId", tc.attachment)
			}
			req := httptest.NewRequest(http.MethodGet, "/attachment?"+values.Encode(), nil)
			rec := httptest.NewRecorder()
			h.GmailAttachment(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", rec.Code, tc.want, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.contains) {
				t.Errorf("body=%q", rec.Body.String())
			}
		})
	}
}

type staticAttachmentClient struct {
	data []byte
	err  error
}

func (c staticAttachmentClient) GetAttachment(context.Context, string, string) ([]byte, error) {
	return c.data, c.err
}

func TestGmailAttachmentSuccessAndErrorBoundary(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		client MailAttachmentClient
		want   int
	}{
		{name: "success", client: staticAttachmentClient{data: []byte("payload")}, want: 200},
		{name: "not found", client: staticAttachmentClient{err: errors.New("not found")}, want: 404},
		{name: "forbidden", client: staticAttachmentClient{err: errors.New("forbidden")}, want: 403},
		{name: "invalid", client: staticAttachmentClient{err: errors.New("invalid")}, want: 400},
		{name: "upstream", client: staticAttachmentClient{err: errors.New("timeout")}, want: 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(Config{AttachmentFactory: func() (MailAttachmentClient, error) { return tc.client, nil }})
			u := "/attachment?clientToken=" + url.QueryEscape(token) + "&messageId=m&attachmentId=a&filename=" + url.QueryEscape("../안전.pdf") + "&mimeType=" + url.QueryEscape("application/pdf; charset=binary")
			rec := httptest.NewRecorder()
			h.GmailAttachment(rec, httptest.NewRequest(http.MethodGet, u, nil))
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%q", rec.Code, tc.want, rec.Body.String())
			}
			if tc.want == 200 {
				if rec.Body.String() != "payload" {
					t.Errorf("body=%q", rec.Body.String())
				}
				if rec.Header().Get("Content-Type") != "application/pdf" {
					t.Errorf("Content-Type=%q", rec.Header().Get("Content-Type"))
				}
				_, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
				if err != nil || params["filename"] != "안전.pdf" {
					t.Errorf("disposition=%q params=%v err=%v", rec.Header().Get("Content-Disposition"), params, err)
				}
				if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
					t.Error("nosniff missing")
				}
			}
		})
	}
}

func TestNativeAPIPureBoundariesConcurrent(t *testing.T) {
	const workers = 128
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {

		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				name := fmt.Sprintf("../file-%d-%d.txt", worker, i)
				if got := sanitizeAttachmentFilename(name); strings.Contains(got, "/") || strings.Contains(got, "\\") {
					errs <- fmt.Errorf("unsafe name=%q", got)
					return
				}
				if safeAttachmentContentType("text/plain; charset=utf-8") != "text/plain" {
					errs <- fmt.Errorf("mime mismatch")
					return
				}
				if statusForMiniappGmailAttachmentError(errors.New("not found")) != 404 {
					errs <- fmt.Errorf("status mismatch")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
