package filestore

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *LocalStore {
	t.Helper()
	s, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	return s
}

func TestLocalStore_PutGetRoundtrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	want := []byte("안녕하세요 견적서입니다")

	meta, err := s.Put(ctx, "/메일/견적서.txt", want, false)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if meta.PathDisplay != "/메일/견적서.txt" {
		t.Errorf("PathDisplay = %q, want /메일/견적서.txt", meta.PathDisplay)
	}
	if meta.Tag != "file" || meta.Size != int64(len(want)) {
		t.Errorf("meta = %+v, want file size %d", meta, len(want))
	}
	if meta.ServerModified == "" {
		t.Error("ServerModified empty; want RFC3339 timestamp")
	}

	got, gotMeta, err := s.Get(ctx, "/메일/견적서.txt")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("Get bytes = %q, want %q", got, want)
	}
	if gotMeta.PathLower != "/메일/견적서.txt" {
		t.Errorf("PathLower = %q", gotMeta.PathLower)
	}
}

func TestLocalStore_PutAutorename(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	first, err := s.Put(ctx, "/doc.pdf", []byte("v1"), false)
	if err != nil {
		t.Fatalf("Put #1: %v", err)
	}
	if first.PathDisplay != "/doc.pdf" {
		t.Fatalf("first PathDisplay = %q, want /doc.pdf", first.PathDisplay)
	}

	second, err := s.Put(ctx, "/doc.pdf", []byte("v2"), false)
	if err != nil {
		t.Fatalf("Put #2: %v", err)
	}
	if second.PathDisplay != "/doc (1).pdf" {
		t.Errorf("autorename PathDisplay = %q, want /doc (1).pdf", second.PathDisplay)
	}

	// Original must be untouched by the autorenamed second write.
	got, _, err := s.Get(ctx, "/doc.pdf")
	if err != nil || string(got) != "v1" {
		t.Errorf("original = %q (err %v), want v1", got, err)
	}
}

func TestLocalStore_PutOverwrite(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.Put(ctx, "/x.txt", []byte("old"), true); err != nil {
		t.Fatalf("Put #1: %v", err)
	}
	meta, err := s.Put(ctx, "/x.txt", []byte("new"), true)
	if err != nil {
		t.Fatalf("Put #2: %v", err)
	}
	if meta.PathDisplay != "/x.txt" {
		t.Errorf("overwrite PathDisplay = %q, want /x.txt (no rename)", meta.PathDisplay)
	}
	got, _, _ := s.Get(ctx, "/x.txt")
	if string(got) != "new" {
		t.Errorf("after overwrite = %q, want new", got)
	}
}

func TestLocalStore_List(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustPut(t, s, "/a.txt", "a")
	mustPut(t, s, "/sub/b.txt", "b")
	mustPut(t, s, "/sub/c.txt", "c")

	// Non-recursive root: a.txt (file) + sub (folder). Folders sort first.
	top, err := s.List(ctx, "/", false, 0)
	if err != nil {
		t.Fatalf("List root: %v", err)
	}
	if len(top) != 2 {
		t.Fatalf("root entries = %d, want 2 (%+v)", len(top), top)
	}
	if !top[0].IsFolder() || top[0].Name != "sub" {
		t.Errorf("top[0] = %+v, want folder 'sub' first", top[0])
	}
	if top[1].Name != "a.txt" || top[1].PathDisplay != "/a.txt" {
		t.Errorf("top[1] = %+v, want file a.txt", top[1])
	}

	// Recursive: a.txt, sub, sub/b.txt, sub/c.txt = 4.
	all, err := s.List(ctx, "/", true, 0)
	if err != nil {
		t.Fatalf("List recursive: %v", err)
	}
	if len(all) != 4 {
		t.Errorf("recursive entries = %d, want 4 (%+v)", len(all), all)
	}

	// List a subfolder by virtual path.
	subEntries, err := s.List(ctx, "/sub", false, 0)
	if err != nil {
		t.Fatalf("List /sub: %v", err)
	}
	if len(subEntries) != 2 || subEntries[0].PathDisplay != "/sub/b.txt" {
		t.Errorf("sub entries = %+v, want b.txt,c.txt", subEntries)
	}
}

func TestLocalStore_Search(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustPut(t, s, "/메일/2026견적서.pdf", "x")
	mustPut(t, s, "/메일/명세서.pdf", "y")
	mustPut(t, s, "/계약/견적_초안.docx", "z")

	hits, err := s.Search(ctx, "견적", 0)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("search '견적' = %d hits, want 2 (%+v)", len(hits), hits)
	}
	for _, h := range hits {
		if !strings.Contains(h.Name, "견적") {
			t.Errorf("hit %q does not contain 견적", h.Name)
		}
	}

	none, err := s.Search(ctx, "없는파일명", 0)
	if err != nil {
		t.Fatalf("Search empty: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("search miss = %d, want 0", len(none))
	}

	if _, err := s.Search(ctx, "   ", 0); err == nil {
		t.Error("blank query should error")
	}
}

// TestLocalStore_PathEscape is the security-critical test: a virtual path with
// "../" (or absolute re-anchoring) must never read or write outside the root.
func TestLocalStore_PathEscape(t *testing.T) {
	root := t.TempDir()
	s, err := NewLocalStore(root)
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	ctx := context.Background()

	// Plant a secret OUTSIDE the root, in the parent dir.
	secret := filepath.Join(filepath.Dir(root), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatalf("plant secret: %v", err)
	}

	// Every one of these must NOT yield the parent's secret.
	for _, esc := range []string{
		"/../secret.txt",
		"/../../secret.txt",
		"../secret.txt",
		"/sub/../../secret.txt",
		"/../../../../../../etc/passwd",
	} {
		if data, _, err := s.Get(ctx, esc); err == nil && string(data) == "TOPSECRET" {
			t.Errorf("Get(%q) escaped the root and read the secret!", esc)
		}
	}

	// A traversal Put must land inside root, never overwrite the outside secret.
	if _, err := s.Put(ctx, "/../secret.txt", []byte("HACKED"), true); err != nil {
		t.Fatalf("Put traversal: %v", err)
	}
	got, _ := os.ReadFile(secret)
	if string(got) != "TOPSECRET" {
		t.Errorf("outside secret was clobbered: %q", got)
	}
	// The write should have been clamped to root/secret.txt instead.
	if _, err := os.Stat(filepath.Join(root, "secret.txt")); err != nil {
		t.Errorf("clamped write not found inside root: %v", err)
	}
}

func TestLocalStore_StatAndDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustPut(t, s, "/note.txt", "hi")

	st, err := s.Stat(ctx, "/note.txt")
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Tag != "file" || st.Name != "note.txt" {
		t.Errorf("Stat = %+v", st)
	}

	if err := s.Delete(ctx, "/note.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Stat(ctx, "/note.txt"); err == nil {
		t.Error("Stat after Delete should fail")
	}

	if err := s.Delete(ctx, "/"); err == nil {
		t.Error("Delete root should be rejected")
	}
}

func TestLocalStore_Open(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mustPut(t, s, "/dir/file.txt", "streamed content")

	f, meta, err := s.Open(ctx, "/dir/file.txt")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = f.Close() }()
	if meta.Name != "file.txt" || meta.PathDisplay != "/dir/file.txt" {
		t.Errorf("Open meta = %+v", meta)
	}
	got, err := io.ReadAll(f)
	if err != nil || string(got) != "streamed content" {
		t.Errorf("read = %q (err %v)", got, err)
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Errorf("Seek (ServeContent needs this): %v", err)
	}

	if _, _, err := s.Open(ctx, "/dir"); err == nil {
		t.Error("Open on a directory should error")
	}
	if _, _, err := s.Open(ctx, "/missing.txt"); err == nil {
		t.Error("Open on a missing file should error")
	}
}

func TestVPath(t *testing.T) {
	cases := map[string]string{
		"":            "/",
		"/":           "/",
		"foo":         "/foo",
		"/foo/bar":    "/foo/bar",
		"/foo/../bar": "/bar",
		"/../escape":  "/escape", // clamped, never climbs above root
		"/a//b":       "/a/b",
		"/메일/견적서.pdf": "/메일/견적서.pdf",
	}
	for in, want := range cases {
		if got := vpath(in); got != want {
			t.Errorf("vpath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDefaultDir(t *testing.T) {
	t.Setenv("DENEB_FILES_DIR", "/tmp/deneb-files-test")
	if got := DefaultDir(); got != "/tmp/deneb-files-test" {
		t.Errorf("DefaultDir with env = %q", got)
	}
	t.Setenv("DENEB_FILES_DIR", "")
	if got := DefaultDir(); !strings.HasSuffix(got, filepath.Join(".deneb", "files")) {
		t.Errorf("DefaultDir fallback = %q, want …/.deneb/files", got)
	}
}

func mustPut(t *testing.T, s *LocalStore, path, content string) {
	t.Helper()
	if _, err := s.Put(context.Background(), path, []byte(content), true); err != nil {
		t.Fatalf("Put(%q): %v", path, err)
	}
}
