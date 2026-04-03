// Package platform provides RPC method handlers for the platform domain,
// covering wizard, talk, and secret subsystems.
//
// It exposes WizardMethods, TalkMethods, and SecretMethods, which return
// handler maps that can be bulk-registered on the rpc.Dispatcher.
package platform

import (
	"context"
	"encoding/json"

	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/secret"
	"github.com/choiceoh/deneb/gateway-go/internal/talk"
	"github.com/choiceoh/deneb/gateway-go/internal/wizard"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// ---------------------------------------------------------------------------
// Wizard
// ---------------------------------------------------------------------------

// WizardDeps holds the dependencies for wizard RPC methods.
type WizardDeps struct {
	Engine *wizard.Engine
}

// WizardMethods returns the wizard RPC handlers keyed by method name.
// If deps.Engine is nil, nil is returned.
func WizardMethods(deps WizardDeps) map[string]rpcutil.HandlerFunc {
	if deps.Engine == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"wizard.start":  wizardStart(deps),
		"wizard.next":   wizardNext(deps),
		"wizard.cancel": wizardCancel(deps),
		"wizard.status": wizardStatus(deps),
	}
}

func wizardStart(deps WizardDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Mode      string `json:"mode"`
			Workspace string `json:"workspace,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.Mode == "" {
			return rpcerr.MissingParam("mode").Response(req.ID)
		}

		session := deps.Engine.Start(p.Mode, p.Workspace)
		return rpcutil.RespondOK(req.ID, session)
	}
}

func wizardNext(deps WizardDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionID string         `json:"sessionId"`
			Answer    *wizard.Answer `json:"answer,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SessionID == "" {
			return rpcerr.MissingParam("sessionId").Response(req.ID)
		}

		session, err := deps.Engine.Next(p.SessionID, p.Answer)
		if err != nil {
			return rpcerr.New(protocol.ErrNotFound, err.Error()).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, session)
	}
}

func wizardCancel(deps WizardDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionID string `json:"sessionId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SessionID == "" {
			return rpcerr.MissingParam("sessionId").Response(req.ID)
		}

		session, err := deps.Engine.Cancel(p.SessionID)
		if err != nil {
			return rpcerr.New(protocol.ErrNotFound, err.Error()).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"status": session.Status,
		})
	}
}

func wizardStatus(deps WizardDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			SessionID string `json:"sessionId"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.SessionID == "" {
			return rpcerr.MissingParam("sessionId").Response(req.ID)
		}

		session, err := deps.Engine.GetStatus(p.SessionID)
		if err != nil {
			return rpcerr.New(protocol.ErrNotFound, err.Error()).Response(req.ID)
		}

		return rpcutil.RespondOK(req.ID, map[string]any{
			"status": session.Status,
			"error":  session.Error,
		})
	}
}

// ---------------------------------------------------------------------------
// Talk
// ---------------------------------------------------------------------------

// TalkDeps holds the dependencies for talk RPC methods.
type TalkDeps struct {
	Talk *talk.State
}

// TalkMethods returns the talk RPC handlers keyed by method name.
// If deps.Talk is nil, nil is returned.
func TalkMethods(deps TalkDeps) map[string]rpcutil.HandlerFunc {
	if deps.Talk == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"talk.config": talkConfig(deps),
		"talk.mode":   talkMode(deps),
	}
}

func talkConfig(deps TalkDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			IncludeSecrets bool `json:"includeSecrets,omitempty"`
		}
		_ = json.Unmarshal(req.Params, &p)

		cfg := deps.Talk.GetConfig(p.IncludeSecrets)
		return rpcutil.RespondOK(req.ID, map[string]any{"config": cfg})
	}
}

func talkMode(deps TalkDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			Enabled bool   `json:"enabled"`
			Phase   string `json:"phase,omitempty"`
		}](req)
		if errResp != nil {
			return errResp
		}

		result := deps.Talk.SetMode(p.Enabled, p.Phase)
		return rpcutil.RespondOK(req.ID, result)
	}
}

// ---------------------------------------------------------------------------
// Secret
// ---------------------------------------------------------------------------

// SecretDeps holds the dependencies for secrets RPC methods.
type SecretDeps struct {
	Resolver *secret.Resolver
}

// SecretMethods returns the secrets RPC handlers keyed by method name.
// If deps.Resolver is nil, nil is returned.
func SecretMethods(deps SecretDeps) map[string]rpcutil.HandlerFunc {
	if deps.Resolver == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"secrets.reload":  secretsReload(deps),
		"secrets.resolve": secretsResolve(deps),
	}
}

func secretsReload(deps SecretDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		result := deps.Resolver.Reload()
		return rpcutil.RespondOK(req.ID, result)
	}
}

func secretsResolve(deps SecretDeps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		p, errResp := rpcutil.DecodeParams[struct {
			CommandName string   `json:"commandName"`
			TargetIDs   []string `json:"targetIds"`
		}](req)
		if errResp != nil {
			return errResp
		}
		if p.CommandName == "" || len(p.TargetIDs) == 0 {
			return rpcerr.MissingParam("commandName and targetIds").Response(req.ID)
		}

		result := deps.Resolver.Resolve(p.CommandName, p.TargetIDs)
		return rpcutil.RespondOK(req.ID, result)
	}
}
