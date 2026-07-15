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

func TestStatusLine_OffAndOn(t *testing.T) {
	if !strings.Contains(StatusLine(Config{}, false), "꺼짐") {
		t.Fatal("expected off message")
	}
	msg := StatusLine(Config{URL: "https://example.com", User: "alice", Password: "sekrit", Company: "co"}, true)
	if !strings.Contains(msg, "설정됨") || !strings.Contains(msg, "alice") || strings.Contains(msg, "sekrit") {
		t.Fatalf("got %q", msg)
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
