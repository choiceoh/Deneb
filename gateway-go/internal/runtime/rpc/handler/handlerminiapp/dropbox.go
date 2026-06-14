package handlerminiapp

import (
	"context"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// DropboxStatusOut reports the host-side Dropbox connection state for the
// native connect wizard: whether a token is stored (connected) and whether an
// app key is already saved (so the UI can skip re-asking for it on reconnect).
type DropboxStatusOut struct {
	Connected     bool `json:"connected"`
	AppConfigured bool `json:"appConfigured"`
}

// DropboxBeginOut carries the OAuth consent URL the user opens to authorize
// Deneb. After approving, Dropbox shows an out-of-band code to paste back via
// miniapp.dropbox.complete.
type DropboxBeginOut struct {
	AuthURL string `json:"authUrl"`
}

// DropboxDeps wires the native Dropbox connect flow (PKCE) to the host-side
// credential store. The verifier minted in Begin must be held server-side until
// Complete exchanges the pasted code for a refresh token.
type DropboxDeps struct {
	Status func() DropboxStatusOut
	// Begin mints the consent URL. redirectURI is the native app's deep-link
	// (e.g. "deneb://dropbox-auth") for the auto-capture flow, or "" for the
	// out-of-band paste-code flow (desktop / host CLI).
	Begin    func(appKey, appSecret, redirectURI string) (DropboxBeginOut, error)
	Complete func(ctx context.Context, code string) error
}

// DropboxMethods exposes the native Dropbox connect wizard RPCs.
func DropboxMethods(deps DropboxDeps) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		"miniapp.dropbox.status":   dropboxStatus(deps),
		"miniapp.dropbox.begin":    dropboxBegin(deps),
		"miniapp.dropbox.complete": dropboxComplete(deps),
	}
}

func dropboxStatus(deps DropboxDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.dropbox.status requires client identity context").Response(req.ID)
		}
		if deps.Status == nil {
			return rpcerr.Unavailable("dropbox status is unavailable").Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, deps.Status())
	}
}

func dropboxBegin(deps DropboxDeps) rpcutil.HandlerFunc {
	type params struct {
		AppKey      string `json:"appKey"`
		AppSecret   string `json:"appSecret"`
		RedirectURI string `json:"redirectUri"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.dropbox.begin requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			if deps.Begin == nil {
				return nil, rpcerr.Unavailable("dropbox connect is unavailable")
			}
			return deps.Begin(strings.TrimSpace(p.AppKey), strings.TrimSpace(p.AppSecret), strings.TrimSpace(p.RedirectURI))
		})
	}
}

func dropboxComplete(deps DropboxDeps) rpcutil.HandlerFunc {
	type params struct {
		Code string `json:"code"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if clientauth.FromContext(ctx) == nil {
			return rpcerr.New(protocol.ErrUnauthorized, "miniapp.dropbox.complete requires client identity context").Response(req.ID)
		}
		return rpcutil.BindCtx[params](ctx, req, func(ctx context.Context, p params) (any, error) {
			code := strings.TrimSpace(p.Code)
			if code == "" {
				return nil, rpcerr.MissingParam("code")
			}
			if deps.Complete == nil {
				return nil, rpcerr.Unavailable("dropbox connect is unavailable")
			}
			if err := deps.Complete(ctx, code); err != nil {
				return nil, err
			}
			return map[string]any{"connected": true}, nil
		})
	}
}
