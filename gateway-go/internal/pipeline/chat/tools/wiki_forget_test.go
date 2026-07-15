package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func newWikiForgetTestStore(t *testing.T) *wiki.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func TestWikiForgetRemovesPageAndFlushesSession(t *testing.T) {
	store := newWikiForgetTestStore(t)
	page := wiki.NewPage("옛회사", "기타", nil)
	page.Body = "폐업한 회사 정보"
	if err := store.WritePage("기타/옛회사.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	var flushed string
	tool := ToolWikiForget(&tooldeps.WikiDeps{Store: store}, func(sk string) { flushed = sk })
	ctx := toolport.WithSessionKey(context.Background(), "sess-1")

	// Missing reason → guidance, page preserved, no flush.
	out, err := tool(ctx, json.RawMessage(`{"path":"기타/옛회사"}`))
	if err != nil {
		t.Fatalf("forget(no reason): %v", err)
	}
	if !strings.Contains(out, "reason") && !strings.Contains(out, "사유") {
		t.Fatalf("expected reason guidance, got: %s", out)
	}
	if flushed != "" {
		t.Fatalf("flush should not fire on a guidance path")
	}

	// With reason → removed + session flushed.
	out, err = tool(ctx, json.RawMessage(`{"path":"기타/옛회사","reason":"오정보"}`))
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !strings.Contains(out, "잊음") {
		t.Fatalf("unexpected forget reply: %s", out)
	}
	if _, rerr := store.ReadPage("기타/옛회사.md"); rerr == nil {
		t.Fatalf("page should be gone after forget")
	}
	if flushed != "sess-1" {
		t.Fatalf("session cache flush not invoked with the session key, got %q", flushed)
	}
}

func TestWikiForgetRejectsPathEscape(t *testing.T) {
	store := newWikiForgetTestStore(t)
	tool := ToolWikiForget(&tooldeps.WikiDeps{Store: store}, nil)
	out, err := tool(context.Background(), json.RawMessage(`{"path":"../../etc/passwd","reason":"x"}`))
	if err != nil {
		t.Fatalf("forget(escape): %v", err)
	}
	if !strings.Contains(out, "위키 루트 밖") {
		t.Fatalf("expected path-escape guidance, got: %s", out)
	}
}

func TestWikiForgetRefusesDealLedgerPage(t *testing.T) {
	store := newWikiForgetTestStore(t)
	tool := ToolWikiForget(&tooldeps.WikiDeps{Store: store}, nil)
	out, err := tool(context.Background(), json.RawMessage(`{"path":"프로젝트/거래/JA Solar","reason":"x"}`))
	if err != nil {
		t.Fatalf("forget(deal): %v", err)
	}
	if !strings.Contains(out, "거래") || !strings.Contains(out, "실패") {
		t.Fatalf("expected deal-ledger refusal, got: %s", out)
	}
}
