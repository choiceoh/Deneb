package nativeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
)

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
