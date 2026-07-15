// Package serverwire owns thin re-exports and helpers for GatewayHub RPC wiring.
//
// Heavy Ports field types live in serverwire/porttypes. Domain registration
// tables live in serverwire/early and serverwire/late.
//
// This package must not import runtime/server.
package serverwire

import (
	"errors"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire/porttypes"
)

// Sentinel errors returned by late-bound miniapp factories (UNAVAILABLE).
var (
	ErrWikiDisabled          = errors.New("wiki knowledge base not configured")
	ErrTranscriptUnavailable = errors.New("session transcript store not initialized")
	ErrCronUnavailable       = errors.New("cron service not configured")
	ErrNotebookDisabled      = errors.New("notebook store not configured")
)

// Re-exports so callers keep a stable serverwire.* surface.
type (
	Ports             = porttypes.Ports
	WorkFeedMirror    = porttypes.WorkFeedMirror
	WorkFeedPorts     = porttypes.WorkFeed
	MailAnalysisPorts = porttypes.MailAnalysis
	PhonePorts        = porttypes.Phone
	GenesisPorts      = porttypes.Genesis
	CapabilityFlags   = porttypes.Caps
)

// WithMailAliases returns a miniapp.gmail.* method map extended with miniapp.mail.* aliases.
func WithMailAliases(m map[string]rpcutil.HandlerFunc) map[string]rpcutil.HandlerFunc {
	out := make(map[string]rpcutil.HandlerFunc, len(m)*2)
	for name, h := range m {
		out[name] = h
		if rest, ok := strings.CutPrefix(name, "miniapp.gmail."); ok {
			out["miniapp.mail."+rest] = h
		}
	}
	return out
}
