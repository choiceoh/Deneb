package skillsrpc

import (
	"context"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	handlerminiapp "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/minibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Wire type aliases — see handlerminiapp/rsi_status_contract.go.
type (
	RSILoopStatusResponse = handlerminiapp.RSILoopStatusResponse
	RSIHealthView         = handlerminiapp.RSIHealthView
	RSILayerView          = handlerminiapp.RSILayerView
	RSIMetricView         = handlerminiapp.RSIMetricView
)

// RSIStatusDeps wires the native + andromeda "recursive self-improvement"
// viewer to the genesis loop-status snapshot. Read-only.
type RSIStatusDeps struct {
	Status func() genesis.RSILoopStatus
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
	return minibind.BindOptional[params](func(_ context.Context, req *protocol.RequestFrame, _ params) *protocol.ResponseFrame {
		st := deps.Status()
		resp := RSILoopStatusResponse{
			Turning: st.Turning,
			Health: RSIHealthView{
				Evolves7d:         st.Health.Evolves7d,
				Confirmed7d:       st.Health.Confirmed7d,
				Rejected7d:        st.Health.Rejected7d,
				RolledBack7d:      st.Health.RolledBack7d,
				Genesis7d:         st.Health.Genesis7d,
				ConfirmRate:       st.Health.ConfirmRate,
				FalseAcceptRate:   st.Health.FalseAcceptRate,
				ResolvedEvolves7d: st.Health.ResolvedEvolves7d,
				Thrash:            st.Health.Thrash,
				AutoAdoptFrozen:   st.Health.AutoAdoptFrozen,
				MetaRevisions7d:   st.Health.MetaRevisions7d,
			},
		}
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
