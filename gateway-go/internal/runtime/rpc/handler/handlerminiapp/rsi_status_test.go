package handlerminiapp

import (
	"context"
	"testing"

	rsistatus "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/status"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func sampleRSIStatus() rsistatus.LoopStatus {
	return rsistatus.LoopStatus{
		Turning: 1,
		Layers: []rsistatus.Layer{
			{
				Key: "L1", Title: "skill evolution", State: rsistatus.StateLive, Diagnosis: "3 evolved this week",
				Metrics: []rsistatus.Metric{{Label: "evolved (7d)", Value: "3"}},
			},
			{Key: "L4", Title: "source self-edit", State: rsistatus.StateStarved, Diagnosis: "no dispatchable code candidates yet"},
		},
	}
}

func TestRSIStatusMethods_NilStatusRegistersNothing(t *testing.T) {
	if RSIStatusMethods(RSIStatusDeps{}) != nil {
		t.Fatal("nil Status must register no methods")
	}
}

func TestRSIStatus_WithIdentity(t *testing.T) {
	h := rsiStatus(RSIStatusDeps{Status: sampleRSIStatus})
	ctx := clientauth.WithContext(context.Background(), sampleIdentity())

	got := decodePayload(t, h(ctx, newReq(t, "miniapp.rsi.status")))
	if got["turning"] != float64(1) {
		t.Errorf("turning = %v, want 1", got["turning"])
	}
	layers, ok := got["layers"].([]any)
	if !ok || len(layers) != 2 {
		t.Fatalf("layers = %#v, want 2", got["layers"])
	}
	l1, _ := layers[0].(map[string]any)
	if l1["key"] != "L1" || l1["state"] != rsistatus.StateLive {
		t.Errorf("L1 layer = %#v", l1)
	}
	metrics, ok := l1["metrics"].([]any)
	if !ok || len(metrics) != 1 {
		t.Fatalf("L1 metrics = %#v, want 1", l1["metrics"])
	}
	l4, _ := layers[1].(map[string]any)
	if l4["state"] != rsistatus.StateStarved {
		t.Errorf("L4 state = %v, want STARVED", l4["state"])
	}
}

func TestRSIStatusReturnsUnauthorizedWithoutIdentity(t *testing.T) {
	h := rsiStatus(RSIStatusDeps{Status: func() rsistatus.LoopStatus {
		t.Fatal("Status must not be called without client identity")
		return rsistatus.LoopStatus{}
	}})

	resp := h(context.Background(), newReq(t, "miniapp.rsi.status"))
	if resp.OK {
		t.Fatal("expected unauthorized response")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("error code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}
