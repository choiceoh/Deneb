package groupware

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFromEnv_RequiresUserAndPassword(t *testing.T) {
	t.Setenv("DENEB_GROUPWARE_USER", "")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "")
	if _, ok := FromEnv(); ok {
		t.Fatal("expected disabled without credentials")
	}
	t.Setenv("DENEB_GROUPWARE_USER", "alice")
	t.Setenv("DENEB_GROUPWARE_PASSWORD", "secret")
	cfg, ok := FromEnv()
	if !ok {
		t.Fatal("expected enabled")
	}
	if cfg.URL != "https://tsgw.topsolar.kr" || cfg.Company != "topsolar" || cfg.User != "alice" {
		t.Fatalf("defaults: %+v", cfg)
	}
}

func TestDefaultReaderJS_FindsScript(t *testing.T) {
	p := defaultReaderJS()
	if p == "" {
		t.Fatal("reader script not discovered")
	}
	if filepath.Base(p) != "read.mjs" {
		t.Fatalf("unexpected path %q", p)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultReaderJS_FallsBackWhenCallerPathMissing(t *testing.T) {
	// Simulate production WorkingDirectory=/repo-root (systemd) without relying
	// on runtime.Caller surviving -trimpath: chdir to repo root and ensure the
	// cwd candidate still resolves.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := wd
	for {
		if _, err := os.Stat(filepath.Join(root, "scripts", "dev", "groupware-reader", "read.mjs")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatal("repo root not found from test cwd")
		}
		root = parent
	}
	t.Chdir(root)
	p := defaultReaderJS()
	if p == "" {
		t.Fatal("cwd fallback should find read.mjs")
	}
	want := filepath.Join(root, "scripts", "dev", "groupware-reader", "read.mjs")
	if filepath.Clean(p) != filepath.Clean(want) && !strings.HasSuffix(filepath.Clean(p), "groupware-reader/read.mjs") {
		t.Fatalf("got %q", p)
	}
}

func TestStatusLine_OffAndOn(t *testing.T) {
	if !strings.Contains(StatusLine(Config{}, false), "꺼짐") {
		t.Fatal("expected off message")
	}
	msg := StatusLine(Config{URL: "https://example.com", User: "alice", Password: "sekrit", Company: "co"}, true)
	if !strings.Contains(msg, "설정됨") || !strings.Contains(msg, "alice") || strings.Contains(msg, "sekrit") {
		t.Fatalf("got %q", msg)
	}
}

func TestRunWithOutputLimitRejectsOversizedReaderStdout(t *testing.T) {
	script := filepath.Join(t.TempDir(), "reader.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '1234567890'\n"), 0o700); err != nil {
		t.Fatalf("write fake reader: %v", err)
	}
	out, err := runWithOutputLimit(context.Background(), Config{
		User: "alice", Password: "secret", ReaderJS: script, NodeBin: "/bin/sh", Timeout: time.Second,
	}, Request{Area: AreaApproval, Action: ActionAttachment}, 5)
	if err == nil {
		t.Fatalf("oversized reader output unexpectedly succeeded: %q", out)
	}
	if !strings.Contains(out, "안전 한도(5 bytes)") {
		t.Fatalf("oversized reader output = %q", out)
	}
}

func TestReadApproval_EmptyWithoutCreds(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if got := ReadApproval(ctx, Config{}, "아마란스", "종류: 전자결재"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractDocID(t *testing.T) {
	t.Parallel()
	if got := ExtractDocID("제목: x\nid: 99178\n"); got != "99178" {
		t.Fatalf("got %q", got)
	}
	if got := ExtractDocID("none"); got != "" {
		t.Fatalf("empty got %q", got)
	}
}

func TestParseApprovalSummaries(t *testing.T) {
	t.Parallel()
	got, err := parseApprovalSummaries(`[{
		"docId":"99178","title":"구매 품의","docNo":"EAP-42",
		"drafter":"홍길동","date":"2026-07-16","status":"결재대기","folder":"pending"
	}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DocID != "99178" || got[0].Folder != "pending" || got[0].Drafter != "홍길동" {
		t.Fatalf("parsed summaries = %+v", got)
	}
	if _, err := parseApprovalSummaries("출처: radar\n[]"); err == nil {
		t.Fatal("expected provenance-prefixed human output to fail JSON parsing")
	}
}

func TestParseBoardSummaries(t *testing.T) {
	t.Parallel()
	got, err := parseBoardSummaries(`[{
		"postId":"17106","title":"휴무 일정","author":"인사팀",
		"date":"2026-07-16","categoryId":"42"
	}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PostID != "17106" || got[0].Author != "인사팀" || got[0].CategoryID != "42" {
		t.Fatalf("parsed summaries = %+v", got)
	}
	empty, err := parseBoardSummaries("null")
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("null summaries = %#v, want non-nil empty slice", empty)
	}
	if _, err := parseBoardSummaries("게시판 최근 글"); err == nil {
		t.Fatal("expected human output to fail JSON parsing")
	}
}
