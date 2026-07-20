package chat

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func TestResolveBoardRefsInjectsTypedValues(t *testing.T) {
	board := toolport.NewBlackboard()
	if err := board.Put("phone", json.RawMessage(`"01012345678"`), "test"); err != nil {
		t.Fatal(err)
	}
	if err := board.Put("qty", json.RawMessage(`3`), "test"); err != nil {
		t.Fatal(err)
	}
	ctx := toolport.WithBlackboard(context.Background(), board)
	in := json.RawMessage(`{"to":"$board.phone","count":"$board.qty","note":"literal"}`)
	out, err := resolveBoardRefs(ctx, in)
	if err != nil {
		t.Fatalf("resolveBoardRefs: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if got["to"] != "01012345678" {
		t.Fatalf("to=%v", got["to"])
	}
	if got["count"] != float64(3) {
		t.Fatalf("count=%v", got["count"])
	}
	if got["note"] != "literal" {
		t.Fatalf("note=%v", got["note"])
	}
}

func TestResolveBoardRefsFailClosed(t *testing.T) {
	board := toolport.NewBlackboard()
	ctx := toolport.WithBlackboard(context.Background(), board)
	_, err := resolveBoardRefs(ctx, json.RawMessage(`{"to":"$board.phone"}`))
	if err == nil || !strings.Contains(err.Error(), "missing key") {
		t.Fatalf("err=%v", err)
	}
}
