package tools

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
)

func TestWikiForgetActionRemovesPageWithReason(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	page := wiki.NewPage("옛회사", "기타", nil)
	page.Body = "폐업한 회사 정보"
	if err := store.WritePage("기타/옛회사.md", page); err != nil {
		t.Fatalf("WritePage: %v", err)
	}

	tool := ToolWiki(&tooldeps.WikiDeps{Store: store}, dir)

	// Missing reason → guidance, page preserved.
	out, err := tool(context.Background(), json.RawMessage(`{"action":"forget","query":"기타/옛회사"}`))
	if err != nil {
		t.Fatalf("forget(no reason): %v", err)
	}
	if !strings.Contains(out, "사유") {
		t.Fatalf("expected reason guidance, got: %s", out)
	}
	if _, rerr := store.ReadPage("기타/옛회사.md"); rerr != nil {
		t.Fatalf("page removed without a reason: %v", rerr)
	}

	// With reason → removed.
	out, err = tool(context.Background(), json.RawMessage(`{"action":"forget","query":"기타/옛회사","content":"오정보"}`))
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if !strings.Contains(out, "잊음") {
		t.Fatalf("unexpected forget reply: %s", out)
	}
	if _, rerr := store.ReadPage("기타/옛회사.md"); rerr == nil {
		t.Fatalf("page should be gone after forget")
	}
}

func TestWikiForgetRejectsPathEscape(t *testing.T) {
	dir := t.TempDir()
	store, err := wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tool := ToolWiki(&tooldeps.WikiDeps{Store: store}, dir)
	out, err := tool(context.Background(), json.RawMessage(`{"action":"forget","query":"../../etc/passwd","content":"x"}`))
	if err != nil {
		t.Fatalf("forget(escape): %v", err)
	}
	if !strings.Contains(out, "위키 루트 밖") {
		t.Fatalf("expected path-escape guidance, got: %s", out)
	}
}
