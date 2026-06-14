package server

import (
	"context"
	"sync"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// dropboxPending holds the in-flight PKCE state between the connect wizard's
// begin and complete calls. Single-user deployment → at most one connect flow
// at a time, so one mutex-guarded slot is enough (no per-session map).
type dropboxPending struct {
	mu          sync.Mutex
	verifier    string
	appKey      string
	appSecret   string
	redirectURI string // must match between authorize and token exchange
}

// miniappDropboxMethods wires the native Dropbox connect wizard
// (miniapp.dropbox.{status,begin,complete}) to the host credential store.
func (s *Server) miniappDropboxMethods() map[string]rpcutil.HandlerFunc {
	return handlerminiapp.DropboxMethods(handlerminiapp.DropboxDeps{
		Status:   s.dropboxStatus,
		Begin:    s.dropboxBegin,
		Complete: s.dropboxComplete,
	})
}

// dropboxStatus reports whether a Dropbox token + app key are saved on the host.
func (s *Server) dropboxStatus() handlerminiapp.DropboxStatusOut {
	_, _, appOK := dropbox.LoadApp(dropbox.CredentialsDir())
	return handlerminiapp.DropboxStatusOut{
		Connected:     dropbox.HasToken(),
		AppConfigured: appOK,
	}
}

// dropboxBegin mints a PKCE verifier/challenge, persists the app key for later
// reconnects, stashes the verifier (+ redirect URI) for the matching complete,
// and returns the OAuth consent URL. When appKey is empty it reuses a previously
// saved app. redirectURI is the native deep-link for auto-capture, or "" for the
// out-of-band paste-code flow (desktop).
func (s *Server) dropboxBegin(appKey, appSecret, redirectURI string) (handlerminiapp.DropboxBeginOut, error) {
	dir := dropbox.CredentialsDir()
	if appKey == "" {
		if k, sec, ok := dropbox.LoadApp(dir); ok {
			appKey, appSecret = k, sec
		}
	}
	if appKey == "" {
		return handlerminiapp.DropboxBeginOut{}, rpcerr.InvalidRequest("Dropbox App key가 필요합니다 (Dropbox App Console에서 발급)")
	}
	verifier, challenge, err := dropbox.GeneratePKCE()
	if err != nil {
		return handlerminiapp.DropboxBeginOut{}, rpcerr.WrapDependencyFailed("generate pkce", err)
	}
	// Save the app up front so a later reconnect can omit the key.
	if err := dropbox.SaveApp(dir, appKey, appSecret); err != nil {
		return handlerminiapp.DropboxBeginOut{}, rpcerr.WrapDependencyFailed("save dropbox app", err)
	}
	s.dropboxPKCE.mu.Lock()
	s.dropboxPKCE.verifier = verifier
	s.dropboxPKCE.appKey = appKey
	s.dropboxPKCE.appSecret = appSecret
	s.dropboxPKCE.redirectURI = redirectURI
	s.dropboxPKCE.mu.Unlock()
	return handlerminiapp.DropboxBeginOut{
		AuthURL: dropbox.AuthorizeURL(appKey, challenge, dropbox.DefaultScopes, redirectURI),
	}, nil
}

// dropboxComplete exchanges the pasted authorization code for a refresh token
// using the verifier from begin, then persists it (enabling the dropbox tool).
func (s *Server) dropboxComplete(ctx context.Context, code string) error {
	s.dropboxPKCE.mu.Lock()
	verifier, appKey, appSecret, redirectURI := s.dropboxPKCE.verifier, s.dropboxPKCE.appKey, s.dropboxPKCE.appSecret, s.dropboxPKCE.redirectURI
	s.dropboxPKCE.mu.Unlock()
	if verifier == "" || appKey == "" {
		return rpcerr.InvalidRequest("진행 중인 Dropbox 연동이 없습니다 — 먼저 연동을 시작하세요")
	}
	tr, err := dropbox.ExchangeCode(ctx, appKey, appSecret, code, verifier, redirectURI)
	if err != nil {
		return rpcerr.WrapDependencyFailed("dropbox code exchange", err)
	}
	if err := dropbox.SaveToken(dropbox.CredentialsDir(), tr); err != nil {
		return rpcerr.WrapDependencyFailed("save dropbox token", err)
	}
	// One-shot: clear the verifier so a stale code can't be replayed.
	s.dropboxPKCE.mu.Lock()
	s.dropboxPKCE.verifier = ""
	s.dropboxPKCE.mu.Unlock()
	return nil
}
