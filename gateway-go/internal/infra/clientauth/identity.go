package clientauth

import (
	"context"
	"time"
)

// User identifies the authenticated operator behind a native-client request.
// The field shape carries over from the retired Telegram WebApp user object so
// the miniapp.* RPC handlers (notably whoami) keep their response contract
// unchanged after the Telegram bot retirement.
type User struct {
	ID              int64  `json:"id"`
	IsBot           bool   `json:"is_bot,omitempty"`
	FirstName       string `json:"first_name,omitempty"`
	LastName        string `json:"last_name,omitempty"`
	Username        string `json:"username,omitempty"`
	LanguageCode    string `json:"language_code,omitempty"`
	IsPremium       bool   `json:"is_premium,omitempty"`
	AllowsWriteToPM bool   `json:"allows_write_to_pm,omitempty"`
	PhotoURL        string `json:"photo_url,omitempty"`
}

// Identity is the authenticated-operator context attached by the miniapp HTTP
// bridge after client-token auth succeeds. Downstream miniapp.* RPC handlers
// retrieve it with FromContext to confirm auth and read the operator user.
//
// It replaces the former Telegram WebApp InitData: the single operator now
// authenticates with the X-Deneb-Client-Token header, and the bridge builds a
// synthetic Identity (see the server's syntheticOperatorIdentity) carrying a
// stable operator User.
type Identity struct {
	User     *User
	AuthDate time.Time
	ChatType string
	Raw      map[string]string
}

// identityContextKey is an unexported type so the context value cannot collide
// with another package's keys.
type identityContextKey struct{}

// WithContext returns a copy of ctx carrying the operator Identity. The miniapp
// HTTP bridge calls this after client-token auth succeeds; downstream RPC
// handlers then retrieve it with FromContext.
func WithContext(ctx context.Context, id *Identity) context.Context {
	if id == nil {
		return ctx
	}
	return context.WithValue(ctx, identityContextKey{}, id)
}

// FromContext returns the Identity attached by WithContext, or nil if the
// context carries none. A nil result means the caller reached the handler
// without passing the auth middleware — handlers should respond with an
// UNAUTHORIZED error in that case.
func FromContext(ctx context.Context) *Identity {
	if ctx == nil {
		return nil
	}
	id, _ := ctx.Value(identityContextKey{}).(*Identity)
	return id
}
