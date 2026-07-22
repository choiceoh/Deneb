package pathutil

import (
	"path/filepath"
	"testing"
)

func TestSafeFileName(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"client:main", "client:main"},
		{"a/b", "a_b"},
		{`a\b`, "a_b"},
		{"..", "_invalid_"},
		{".", "_invalid_"},
		{"", "_invalid_"},
		{"/\\\x00", "_invalid_"},
		{"foo/../../etc", "foo_.._.._etc"},
	}
	for _, tc := range cases {
		if got := SafeFileName(tc.in); got != tc.want {
			t.Errorf("SafeFileName(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestJoinUnderRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := JoinUnder(root, "../outside"); err == nil {
		t.Fatal("expected escape to fail")
	}
	if _, err := JoinUnder(root, "/etc/passwd"); err == nil {
		t.Fatal("expected absolute elem to fail")
	}
	got, err := JoinUnder(root, "ok.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "ok.jsonl")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	nested, err := JoinUnder(root, filepath.Join("sub", "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if nested != filepath.Join(root, "sub", "x.json") {
		t.Fatalf("nested=%q", nested)
	}
}

func TestMustJoinUnderFallback(t *testing.T) {
	root := t.TempDir()
	got := MustJoinUnder(root, "../x")
	if got != filepath.Join(root, "_invalid_") {
		t.Fatalf("fallback=%q", got)
	}
}
