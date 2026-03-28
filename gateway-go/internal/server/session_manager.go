package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/rpc"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/transcript"
)

// SessionManager groups session-lifecycle dependencies: the session store,
// key cache, transcript writer, and per-session RPC state (presence, heartbeat).
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type SessionManager struct {
	sessions       *session.Manager
	keyCache       *session.KeyCache
	transcript     *transcript.Writer
	presenceStore  *rpc.PresenceStore
	heartbeatState *rpc.HeartbeatState
}
