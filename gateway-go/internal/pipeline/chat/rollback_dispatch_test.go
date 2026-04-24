package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/checkpoint"
)

func TestSplitRollbackArgs(t *testing.T) {
	tests := []struct {
		in       string
		wantSub  string
		wantRest string
	}{
		{"", "", ""},
		{"   ", "", ""},
		{"list", "list", ""},
		{"LIST 5", "list", "5"},
		{"list 10", "list", "10"},
		{"목록", "목록", ""},
		{"목록 7", "목록", "7"},
		{"diff abc123", "diff", "abc123"},
		{"비교 abc123", "비교", "abc123"},
		{"restore abc123", "restore", "abc123"},
		{"복원  abc123  ", "복원", "abc123"},
		// Subcommand is lowercased so "Restore" matches English dispatch,
		// but Korean aliases keep their runes unchanged.
		{"Restore abc123", "restore", "abc123"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			sub, rest := splitRollbackArgs(tt.in)
			if sub != tt.wantSub {
				t.Errorf("sub = %q, want %q", sub, tt.wantSub)
			}
			if rest != tt.wantRest {
				t.Errorf("rest = %q, want %q", rest, tt.wantRest)
			}
		})
	}
}

func TestParseRollbackCount(t *testing.T) {
	tests := []struct {
		in      string
		want    int
		wantErr bool
	}{
		{"5", 5, false},
		{"10개", 10, false},
		{" 7 개", 7, false},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
		{"999", rollbackMaxListLimit, false}, // clamped
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseRollbackCount(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("got = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestBaseName(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"", "-"},
		{"/home/user/file.go", "file.go"},
		{"file.go", "file.go"},
		{"/trailing/", "/trailing/"},
		{"C:\\Users\\file.go", "file.go"},
	}
	for _, tt := range tests {
		got := baseName(tt.in)
		if got != tt.want {
			t.Errorf("baseName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCountLines(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"a", 1},
		{"a\n", 1},
		{"a\nb", 2},
		{"a\nb\n", 2},
		{"a\nb\nc", 3},
	}
	for _, tt := range tests {
		got := countLines(tt.in)
		if got != tt.want {
			t.Errorf("countLines(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestTruncateForList(t *testing.T) {
	if got := truncateForList("hello", 10); got != "hello" {
		t.Errorf("no-truncate: got %q", got)
	}
	if got := truncateForList("hello world", 5); got != "hell…" {
		t.Errorf("truncate: got %q", got)
	}
	if got := truncateForList("안녕하세요", 3); got != "안녕…" {
		t.Errorf("korean: got %q", got)
	}
	if got := truncateForList("x", 0); got != "" {
		t.Errorf("zero: got %q", got)
	}
}

func TestRenderRollbackList_Empty(t *testing.T) {
	out := renderRollbackList(nil, 5)
	if !strings.Contains(out, "최근 체크포인트가 없습니다") {
		t.Errorf("expected empty notice, got %q", out)
	}
}

func TestRenderRollbackList_WithEntries(t *testing.T) {
	snaps := []*checkpoint.Snapshot{
		{
			ID:      "abc-1",
			Path:    "/tmp/sample.go",
			Seq:     1,
			TakenAt: time.Now(),
			Reason:  "fs_write",
		},
		{
			ID:        "abc-2",
			Path:      "/tmp/sample.go",
			Seq:       2,
			TakenAt:   time.Now(),
			Reason:    "pre-restore",
			Tombstone: true,
		},
	}
	out := renderRollbackList(snaps, 5)
	// Sanity checks.
	for _, want := range []string{"#1", "#2", "sample.go", "abc-1", "abc-2", "(삭제)", "/rollback restore", "/rollback diff"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestRenderRollbackDiff_Truncation(t *testing.T) {
	// Build a diff body larger than the cap so truncation actually triggers.
	line := strings.Repeat("+ long line content\n", 500)
	target := &checkpoint.Snapshot{Seq: 42, Path: "/tmp/big.txt"}
	out := renderRollbackDiff(target, line)
	if !strings.Contains(out, "체크포인트 #42 차이") {
		t.Errorf("missing header: %q", out[:80])
	}
	if !strings.Contains(out, "big.txt") {
		t.Errorf("missing filename: %q", out[:80])
	}
	if !strings.Contains(out, "… (총 ") {
		t.Errorf("expected truncation marker in:\n%s", out[len(out)-200:])
	}
	if len(out) > 4096 {
		t.Errorf("diff message exceeded Telegram limit: %d bytes", len(out))
	}
}

func TestRenderRollbackDiff_NoTruncation(t *testing.T) {
	diff := "--- old\n+++ new\n@@ -1,1 +1,1 @@\n-a\n+b\n"
	target := &checkpoint.Snapshot{Seq: 3, Path: "/x.go"}
	out := renderRollbackDiff(target, diff)
	if strings.Contains(out, "… (총 ") {
		t.Errorf("unexpected truncation marker in:\n%s", out)
	}
	if !strings.Contains(out, "체크포인트 #3 차이") {
		t.Errorf("missing header: %q", out)
	}
}

// TestRollbackEnd2End takes a real snapshot through pkg/checkpoint and runs
// the list/diff/restore dispatchers against it using an in-memory reply
// sink, verifying the Korean user-facing strings and that the restore
// actually rewrites the file content.
func TestRollbackEnd2End(t *testing.T) {
	root := t.TempDir()
	sessionKey := "telegram:123"
	mgr := checkpoint.New(root, sessionKey)

	// Stage: create a file, snapshot it, then mutate it.
	workdir := t.TempDir()
	target := filepath.Join(workdir, "hello.txt")
	if err := os.WriteFile(target, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := mgr.Snapshot(context.Background(), target, "fs_write")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if err := os.WriteFile(target, []byte("v2"), 0o644); err != nil {
		t.Fatal(err)
	}

	// List should contain snap.ID with Korean framing.
	snaps, err := mgr.List("", 5)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	listText := renderRollbackList(snaps, 5)
	if !strings.Contains(listText, snap.ID) {
		t.Errorf("expected snap ID in list output, got %q", listText)
	}
	if !strings.Contains(listText, "최근 체크포인트") {
		t.Errorf("expected Korean header, got %q", listText)
	}

	// Diff should describe the "- v1 / + v2" change.
	diff, err := mgr.Diff(snap.ID)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	diffText := renderRollbackDiff(snap, diff)
	if !strings.Contains(diffText, "hello.txt") {
		t.Errorf("diff missing filename: %q", diffText)
	}
	if !strings.Contains(diffText, "-v1") && !strings.Contains(diffText, "- v1") {
		t.Errorf("diff missing old content: %q", diffText)
	}

	// Restore should write v1 back.
	if _, err := mgr.Restore(context.Background(), snap.ID); err != nil {
		t.Fatalf("restore: %v", err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "v1" {
		t.Errorf("restore did not bring back v1; got %q", string(data))
	}
}
