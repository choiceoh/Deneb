package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func TestToolBlackboardPlanPutAndRequire(t *testing.T) {
	board := toolport.NewBlackboard()
	ctx := toolport.WithBlackboard(context.Background(), board)
	fn := ToolBlackboard()

	planIn := json.RawMessage(`{
		"action":"plan",
		"steps":[
			{"id":"extract","outputs":["order_id"]},
			{"id":"notify","inputs":["order_id"],"outputs":["sent"]}
		]
	}`)
	out, err := fn(ctx, planIn)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if !strings.Contains(out, "extract") || !strings.Contains(out, "notify") {
		t.Fatalf("plan output=%q", out)
	}

	if _, err := fn(ctx, json.RawMessage(`{"action":"begin","step":"extract"}`)); err != nil {
		t.Fatalf("begin extract: %v", err)
	}
	if _, err := fn(ctx, json.RawMessage(`{"action":"end","step":"extract","outputs":{"order_id":"A-1"}}`)); err != nil {
		t.Fatalf("end extract: %v", err)
	}
	requireOut, err := fn(ctx, json.RawMessage(`{"action":"require","keys":["order_id"]}`))
	if err != nil || !strings.Contains(requireOut, `"A-1"`) {
		t.Fatalf("require: out=%q err=%v", requireOut, err)
	}
	if _, err := fn(ctx, json.RawMessage(`{"action":"require","keys":["missing_key"]}`)); err == nil {
		t.Fatal("expected fail-closed require")
	}
}

func TestToolBlackboardUnavailableWithoutContext(t *testing.T) {
	_, err := ToolBlackboard()(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("err=%v", err)
	}
}
