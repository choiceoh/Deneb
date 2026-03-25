package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// TalkDeps holds the dependencies for talk RPC methods.
type TalkDeps struct {
	Talk *talk.State
}

// RegisterTalkMethods registers talk.config and talk.mode RPC methods.
func RegisterTalkMethods(d *Dispatcher, deps TalkDeps) {
	if deps.Talk == nil {
		return
	}

	d.Register("talk.config", talkConfig(deps))
	d.Register("talk.mode", talkMode(deps))
}

func talkConfig(deps TalkDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			IncludeSecrets bool `json:"includeSecrets,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		cfg := deps.Talk.GetConfig(p.IncludeSecrets)
		resp := protocol.MustResponseOK(req.ID, map[string]any{"config": cfg})
		return resp
	}
}

func talkMode(deps TalkDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Enabled bool   `json:"enabled"`
			Phase   string `json:"phase,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}

		result := deps.Talk.SetMode(p.Enabled, p.Phase)
		resp := protocol.MustResponseOK(req.ID, result)
		return resp
	}
}
