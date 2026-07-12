package handlerminiapp

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RSIStatusDeps wires the native + andromeda "recursive self-improvement"
// viewer to the genesis loop-status snapshot. Read-only.
type RSIStatusDeps struct {
	Status func() genesis.RSILoopStatus
}

// RSILoopStatusResponse is the miniapp.rsi.status payload: the four loop layers
// (L1 skill evolution, L2 meta-evolution, L3 verifier co-evolution, L4 source
// self-edit) each with an honest state and display metrics, plus how many are
// turning right now.
//
//deneb:wire
type RSILoopStatusResponse struct {
	Layers  []RSILayerView `json:"layers"`
	Turning int            `json:"turning"`
}

// RSILayerView is one loop layer's classified state. State is one of LIVE,
// DATA-GATED, STARVED, FROZEN, IDLE.
//
//deneb:wire
type RSILayerView struct {
	Key       string          `json:"key"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Diagnosis string          `json:"diagnosis"`
	Detail    string          `json:"detail,omitempty"`
	Metrics   []RSIMetricView `json:"metrics,omitempty"`
}

// RSIMetricView is one display metric (label + preformatted value).
//
//deneb:wire
type RSIMetricView struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// RSIStatusMethods registers the read-only RSI loop-status RPC.
func RSIStatusMethods(deps RSIStatusDeps) map[string]rpcutil.HandlerFunc {
	if deps.Status == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.rsi.status": rsiStatus(deps),
	}
}

func rsiStatus(deps RSIStatusDeps) rpcutil.HandlerFunc {
	type params struct{}
	return bindAuthenticatedOptional[params](func(_ context.Context, req *protocol.RequestFrame, _ params) *protocol.ResponseFrame {
		st := deps.Status()
		resp := RSILoopStatusResponse{Turning: st.Turning}
		resp.Layers = make([]RSILayerView, 0, len(st.Layers))
		for _, l := range st.Layers {
			view := RSILayerView{Key: l.Key, Title: l.Title, State: l.State, Diagnosis: l.Diagnosis, Detail: l.Detail}
			if len(l.Metrics) > 0 {
				view.Metrics = make([]RSIMetricView, 0, len(l.Metrics))
				for _, m := range l.Metrics {
					view.Metrics = append(view.Metrics, RSIMetricView{Label: m.Label, Value: m.Value})
				}
			}
			resp.Layers = append(resp.Layers, view)
		}
		return rpcutil.RespondOK(req.ID, resp)
	})
}
