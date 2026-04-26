// Package openaiapi exposes Deneb's gateway as an OpenAI-compatible
// HTTP endpoint for IDE clients (Zed, OpenCode) consuming Deneb agent
// state over Tailscale.
//
// This v1 skeleton wires only GET /v1/models. Chat completions
// (POST /v1/chat/completions) follows in a subsequent step.
//
// Wiring rule: server.buildMux calls Mount(mux, Deps{...}). All
// dependencies flow through Deps; this package never imports the
// server package or any other runtime package outside leaf deps
// (modelrole).
package openaiapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// Deps wires the OpenAI-compatible endpoints to gateway state.
//
// StartedAt is a closure rather than a value because buildMux runs
// before Server.startedAt is assigned; capturing the value here would
// freeze it at zero. Closure resolves lazily at request time.
type Deps struct {
	Logger        *slog.Logger
	AuthToken     string // empty disables bearer enforcement (dev/loopback)
	ModelRegistry ModelRegistry
	StartedAt     func() time.Time
}

// ModelRegistry is the subset of *modelrole.Registry consumed here.
// Defined as an interface so tests can supply fakes without pulling
// the full registry construction.
type ModelRegistry interface {
	FullModelID(role modelrole.Role) string
}

// Mount registers /v1/* routes on mux.
func Mount(mux *http.ServeMux, deps Deps) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	r := &routes{deps: deps}
	mux.Handle("GET /v1/models", r.bearerAuth(http.HandlerFunc(r.handleModels)))
}

type routes struct {
	deps Deps
}
