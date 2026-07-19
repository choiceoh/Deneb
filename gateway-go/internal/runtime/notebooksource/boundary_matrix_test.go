package notebooksource

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

func TestFetchURLUnsafeBoundaryMatrix(t *testing.T) {
	t.Parallel()
	tests := []struct{ name, rawURL string }{
		{
			name:   "empty",
			rawURL: "",
		},
		{
			name:   "space",
			rawURL: " ",
		},
		{
			name:   "http missing slashes",
			rawURL: "http:example.com",
		},
		{
			name:   "https missing slashes",
			rawURL: "https:example.com",
		},
		{
			name:   "ftp",
			rawURL: "ftp://example.com/file",
		},
		{
			name:   "file",
			rawURL: "file:///etc/passwd",
		},
		{
			name:   "data",
			rawURL: "data:text/plain,hello",
		},
		{
			name:   "javascript",
			rawURL: "javascript:alert(1)",
		},
		{
			name:   "mailto",
			rawURL: "mailto:user@example.com",
		},
		{
			name:   "ssh",
			rawURL: "ssh://127.0.0.1",
		},
		{
			name:   "relative",
			rawURL: "/local/path",
		},
		{
			name:   "bare host",
			rawURL: "example.com",
		},
		{
			name:   "scheme upper",
			rawURL: "HTTP://127.0.0.1",
		},
		{
			name:   "loopback ipv4",
			rawURL: "http://127.0.0.1",
		},
		{
			name:   "loopback ipv4 port",
			rawURL: "http://127.0.0.1:8080/x",
		},
		{
			name:   "loopback short",
			rawURL: "http://127.1",
		},
		{
			name:   "loopback ipv6",
			rawURL: "http://[::1]/",
		},
		{
			name:   "unspecified ipv4",
			rawURL: "http://0.0.0.0/",
		},
		{
			name:   "unspecified ipv6",
			rawURL: "http://[::]/",
		},
		{
			name:   "metadata",
			rawURL: "http://169.254.169.254/latest",
		},
		{
			name:   "private 10",
			rawURL: "http://10.0.0.1/",
		},
		{
			name:   "private 172",
			rawURL: "http://172.16.0.1/",
		},
		{
			name:   "private 192",
			rawURL: "http://192.168.1.1/",
		},
		{
			name:   "link local ipv6",
			rawURL: "http://[fe80::1]/",
		},
		{
			name:   "multicast",
			rawURL: "http://224.0.0.1/",
		},
		{
			name:   "localhost hostname",
			rawURL: "http://localhost/",
		},
		{
			name:   "localhost subdomain",
			rawURL: "http://localhost.localdomain/",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := notebookFetchURL(context.Background(), tc.rawURL); err == nil {
				t.Fatalf("FetchURL(%q)=(%q,nil), want rejection", tc.rawURL, got)
			}
			if got, err := FetchURL(context.Background(), tc.rawURL); err == nil {
				t.Fatalf("exported FetchURL(%q)=(%q,nil)", tc.rawURL, got)
			}
		})
	}
}

func TestReadDiaryBoundaryMatrix(t *testing.T) {
	diaryDir := t.TempDir()
	files := map[string]string{
		"2026-07-11.md":  "today",
		"simple.md":      "simple",
		"spaced name.md": "spaces",
		"한글.md":          "한국어",
		"already.MD.md":  "uppercase suffix behavior",
		"...md":          "dotdot remains contained",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(diaryDir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	store, err := wiki.NewStore(t.TempDir(), diaryDir)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, ref, want string
		wantErr         bool
	}{
		{name: "bare date", ref: "2026-07-11", want: "today"},
		{name: "date extension", ref: "2026-07-11.md", want: "today"},
		{name: "trimmed", ref: "  simple  ", want: "simple"},
		{name: "spaced name", ref: "spaced name", want: "spaces"},
		{name: "unicode", ref: "한글", want: "한국어"},
		{name: "uppercase extension appends", ref: "already.MD", want: "uppercase suffix behavior"},
		{name: "dot dot stays contained", ref: "..", want: "dotdot remains contained"},
		{name: "missing", ref: "missing", wantErr: true},
		{name: "empty", ref: "", wantErr: true},
		{name: "space", ref: "   ", wantErr: true},
		{name: "dot", ref: ".", wantErr: true},
		{name: "slash", ref: "/", wantErr: true},
		{name: "traversal missing", ref: "../outside", wantErr: true},
		{name: "nested missing", ref: "nested/simple", want: "simple"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := notebookReadDiary(store, tc.ref)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v got=%q", err, tc.wantErr, got)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("got=%q want=%q", got, tc.want)
			}
			got2, err2 := ReadDiary(store, tc.ref)
			if (err2 != nil) != tc.wantErr || got2 != got {
				t.Errorf("exported=(%q,%v) internal=(%q,%v)", got2, err2, got, err)
			}
		})
	}
}

func TestReadDiaryNilAndMissingDirectoryBoundaries(t *testing.T) {
	for _, ref := range []string{"", "today", "../secret", strings.Repeat("x", 4096)} {
		if got, err := notebookReadDiary(nil, ref); err == nil || got != "" {
			t.Errorf("nil store ref=%q got=%q err=%v", ref, got, err)
		}
		if got, err := ReadDiary(nil, ref); err == nil || got != "" {
			t.Errorf("exported nil store ref=%q got=%q err=%v", ref, got, err)
		}
	}
	store, err := wiki.NewStore(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := notebookReadDiary(store, "today"); err == nil || got != "" {
		t.Errorf("empty diary dir got=%q err=%v", got, err)
	}
}

func TestReadDiaryDoesNotEscapeDirectory(t *testing.T) {
	root := t.TempDir()
	diary := filepath.Join(root, "diary")
	if err := os.MkdirAll(diary, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "secret.md")
	if err := os.WriteFile(outside, []byte("outside-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := wiki.NewStore(t.TempDir(), diary)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range []string{"../secret", "../../secret", outside, "nested/../../secret"} {
		got, _ := notebookReadDiary(store, ref)
		if strings.Contains(got, "outside-secret") {
			t.Fatalf("ref %q escaped diary: %q", ref, got)
		}
	}
}

func TestReadDiaryConcurrent(t *testing.T) {
	diary := t.TempDir()
	if err := os.WriteFile(filepath.Join(diary, "entry.md"), []byte("stable body"), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := wiki.NewStore(t.TempDir(), diary)
	if err != nil {
		t.Fatal(err)
	}
	const workers = 128
	const iterations = 50
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got, err := notebookReadDiary(store, "entry")
				if err != nil || got != "stable body" {
					errs <- fmt.Errorf("worker=%d i=%d got=%q err=%w", worker, i, got, err)
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

func TestNotebookFetchCapIsPositiveBoundary(t *testing.T) {
	if notebookFetchMaxBytes != 4<<20 {
		t.Errorf("fetch cap=%d", notebookFetchMaxBytes)
	}
	if notebookFetchMaxBytes <= 0 {
		t.Error("fetch cap must be positive")
	}
}
