package rpc

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// WizardDeps holds the dependencies for wizard RPC methods.
type WizardDeps struct {
	Engine *wizard.Engine
}

// RegisterWizardMethods registers wizard.start, wizard.next, wizard.cancel,
// and wizard.status RPC methods.
func RegisterWizardMethods(d *Dispatcher, deps WizardDeps) {
	if deps.Engine == nil {
		return
	}

	d.Register("wizard.start", wizardStart(deps))
	d.Register("wizard.next", wizardNext(deps))
	d.Register("wizard.cancel", wizardCancel(deps))
	d.Register("wizard.status", wizardStatus(deps))
}

func wizardStart(deps WizardDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Mode      string `json:"mode"`
			Workspace string `json:"workspace,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.Mode == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "mode is required"))
		}

		session := deps.Engine.Start(p.Mode, p.Workspace)
		resp, _ := protocol.NewResponseOK(req.ID, session)
		return resp
	}
}

func wizardNext(deps WizardDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionID string         `json:"sessionId"`
			Answer    *wizard.Answer `json:"answer,omitempty"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrInvalidRequest, "invalid params: "+err.Error()))
		}
		if p.SessionID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionId is required"))
		}

		session, err := deps.Engine.Next(p.SessionID, p.Answer)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, session)
		return resp
	}
}

func wizardCancel(deps WizardDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.SessionID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionId is required"))
		}

		session, err := deps.Engine.Cancel(p.SessionID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"status": session.Status,
		})
		return resp
	}
}

func wizardStatus(deps WizardDeps) HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.SessionID == "" {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrMissingParam, "sessionId is required"))
		}

		session, err := deps.Engine.GetStatus(p.SessionID)
		if err != nil {
			return protocol.NewResponseError(req.ID, protocol.NewError(
				protocol.ErrNotFound, err.Error()))
		}

		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"status": session.Status,
			"error":  session.Error,
		})
		return resp
	}
}
