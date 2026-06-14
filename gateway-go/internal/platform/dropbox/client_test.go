package dropbox

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestServer returns an httptest server that mimics the subset of the
// Dropbox v2 API the client uses.
func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "refreshed-token",
			"token_type":   "bearer",
			"expires_in":   14400,
		})
	})

	mux.HandleFunc("/2/files/list_folder", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{".tag": "file", "name": "a.pdf", "path_display": "/a.pdf", "path_lower": "/a.pdf", "id": "id:1", "size": 1024, "server_modified": "2024-01-01T00:00:00Z"},
				{".tag": "folder", "name": "sub", "path_display": "/sub", "path_lower": "/sub", "id": "id:2"},
			},
			"has_more": false,
		})
	})

	mux.HandleFunc("/2/files/search_v2", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"matches": []map[string]any{
				{"metadata": map[string]any{".tag": "metadata", "metadata": map[string]any{
					".tag": "file", "name": "report.xlsx", "path_display": "/report.xlsx", "size": 2048,
				}}},
			},
		})
	})

	mux.HandleFunc("/2/files/download", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Dropbox-API-Result", `{".tag":"file","name":"a.pdf","path_display":"/a.pdf","size":5}`)
		_, _ = w.Write([]byte("hello"))
	})

	mux.HandleFunc("/2/files/upload", func(w http.ResponseWriter, r *http.Request) {
		// Echo the decoded path back so tests can verify the Dropbox-API-Arg
		// header round-trips Korean filenames through ASCII escaping.
		var a struct {
			Path string `json:"path"`
		}
		_ = json.Unmarshal([]byte(r.Header.Get("Dropbox-API-Arg")), &a)
		_ = json.NewEncoder(w).Encode(map[string]any{
			".tag": "file", "name": filepath.Base(a.Path), "path_display": a.Path, "size": 3,
		})
	})

	mux.HandleFunc("/2/sharing/create_shared_link_with_settings", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"url": "https://www.dropbox.com/s/abc/x?dl=0"})
	})

	return httptest.NewServer(mux)
}

func newTestClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return &Client{
		appKey:       "test-key",
		accessToken:  "valid-token",
		refreshToken: "refresh-token",
		expiry:       time.Now().Add(time.Hour),
		tokenPath:    filepath.Join(t.TempDir(), tokenFileName),
		httpClient:   server.Client(),
		tokenURL:     server.URL + "/oauth2/token",
		apiHost:      server.URL,
		contentHost:  server.URL,
	}
}

func TestClient_ListFolder(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	entries, err := c.ListFolder(context.Background(), "", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(entries))
	}
	if entries[0].Name != "a.pdf" || entries[0].IsFolder() {
		t.Errorf("entry 0 wrong: %+v", entries[0])
	}
	if !entries[1].IsFolder() {
		t.Errorf("entry 1 should be folder: %+v", entries[1])
	}
}

func TestClient_Search(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	entries, err := c.Search(context.Background(), "report", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name != "report.xlsx" {
		t.Fatalf("unexpected search result: %+v", entries)
	}
}

func TestClient_Download(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	data, meta, err := c.Download(context.Background(), "/a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("body = %q, want hello", data)
	}
	if meta == nil || meta.Name != "a.pdf" {
		t.Errorf("meta = %+v", meta)
	}
}

func TestClient_Upload_KoreanPathRoundTrip(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	meta, err := c.Upload(context.Background(), "/보고서.pdf", []byte("abc"), true)
	if err != nil {
		t.Fatal(err)
	}
	// The header is ASCII-escaped on the way out and decoded by the server;
	// a correct round-trip recovers the original Korean path.
	if meta.PathDisplay != "/보고서.pdf" {
		t.Errorf("path round-trip failed: %q", meta.PathDisplay)
	}
}

func TestClient_UploadTooLarge(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	_, err := c.Upload(context.Background(), "/big.bin", make([]byte, maxUploadBytes+1), true)
	if err == nil {
		t.Fatal("expected error for oversized upload")
	}
	if !strings.Contains(err.Error(), "150MB") {
		t.Errorf("error should mention size limit: %v", err)
	}
}

func TestClient_CreateSharedLink(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)

	link, err := c.CreateSharedLink(context.Background(), "/a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(link, "https://www.dropbox.com/") {
		t.Errorf("link = %q", link)
	}
}

func TestClient_CreateSharedLink_AlreadyExists(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2/sharing/create_shared_link_with_settings", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error_summary":"shared_link_already_exists/...","error":{".tag":"shared_link_already_exists"}}`))
	})
	mux.HandleFunc("/2/sharing/list_shared_links", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"links": []map[string]any{{"url": "https://www.dropbox.com/s/existing/x?dl=0"}},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)

	link, err := c.CreateSharedLink(context.Background(), "/a.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(link, "existing") {
		t.Errorf("should return the existing link, got %q", link)
	}
}

func TestClient_RefreshOnExpiry(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()
	c := newTestClient(t, srv)
	c.accessToken = "stale"
	c.expiry = time.Now().Add(-time.Minute) // forces a refresh

	tok, err := c.validToken(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if tok != "refreshed-token" {
		t.Errorf("token = %q, want refreshed-token", tok)
	}
	if time.Until(c.expiry) < time.Hour {
		t.Errorf("expiry not extended: %v", c.expiry)
	}
}

func TestAsciiEscapeJSON(t *testing.T) {
	out := asciiEscapeJSON([]byte(`{"path":"/한글 file.pdf"}`))
	for _, r := range out {
		if r > 127 {
			t.Fatalf("non-ASCII rune %q leaked into header value: %s", r, out)
		}
	}
	var v map[string]string
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("escaped output is not valid JSON: %v", err)
	}
	if v["path"] != "/한글 file.pdf" {
		t.Errorf("round-trip mismatch: %q", v["path"])
	}
}

func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := GeneratePKCE()
	if err != nil {
		t.Fatal(err)
	}
	if len(verifier) < 43 || len(verifier) > 128 {
		t.Errorf("verifier length %d out of spec [43,128]", len(verifier))
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Errorf("challenge is not S256(verifier)")
	}
}

func TestAuthorizeURL(t *testing.T) {
	u := AuthorizeURL("appkey123", "chal", DefaultScopes, "")
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	if q.Get("token_access_type") != "offline" {
		t.Error("missing token_access_type=offline (no refresh token without it)")
	}
	if q.Get("code_challenge") != "chal" || q.Get("code_challenge_method") != "S256" {
		t.Error("PKCE challenge params missing")
	}
	if !strings.Contains(q.Get("scope"), "files.content.read") {
		t.Errorf("scopes missing: %q", q.Get("scope"))
	}
}

func TestClient_LatestCursor(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/2/files/list_folder/get_latest_cursor", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"cursor": "cur-start"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)

	cur, err := c.LatestCursor(context.Background(), "/Deneb-Inbox", false)
	if err != nil {
		t.Fatal(err)
	}
	if cur != "cur-start" {
		t.Errorf("cursor = %q, want cur-start", cur)
	}
}

func TestClient_ListChanges(t *testing.T) {
	// Two-page delta: page 1 has_more=true, page 2 has_more=false.
	// folder and deleted entries must be filtered out.
	var page atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/2/files/list_folder/continue", func(w http.ResponseWriter, _ *http.Request) {
		if page.Add(1) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"entries": []map[string]any{
					{".tag": "file", "name": "new1.pdf", "path_display": "/Deneb-Inbox/new1.pdf", "size": 10},
					{".tag": "folder", "name": "sub", "path_display": "/Deneb-Inbox/sub"},
				},
				"cursor": "cur1", "has_more": true,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{".tag": "file", "name": "new2.xlsx", "path_display": "/Deneb-Inbox/new2.xlsx", "size": 20},
				{".tag": "deleted", "name": "old.txt", "path_display": "/Deneb-Inbox/old.txt"},
			},
			"cursor": "cur2", "has_more": false,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(t, srv)

	entries, newCur, err := c.ListChanges(context.Background(), "cur0")
	if err != nil {
		t.Fatal(err)
	}
	if newCur != "cur2" {
		t.Errorf("newCursor = %q, want cur2", newCur)
	}
	if len(entries) != 2 {
		t.Fatalf("want 2 file entries (folder+deleted filtered), got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "new1.pdf" || entries[1].Name != "new2.xlsx" {
		t.Errorf("entries = %+v", entries)
	}
}
