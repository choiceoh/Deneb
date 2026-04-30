// Package openaiapi exposes Deneb's gateway as an OpenAI-compatible
// HTTP endpoint for IDE clients (Zed, OpenCode) consuming Deneb agent
// state over Tailscale.
//
// Wiring rule: server.buildMux calls Mount(mux, Deps{...}). All
// dependencies flow through Deps; this package never imports the
// server package or any other runtime package outside leaf deps
// (modelrole, llm types).
package openaiapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

// Deps wires the OpenAI-compatible endpoints to gateway state.
//
// StartedAt is a closure rather than a value because buildMux runs
// before Server.startedAt is assigned; capturing the value here would
// freeze it at zero. Closure resolves lazily at request time.
//
// ChatClient is also a closure so the underlying *llm.Client can be
// swapped at runtime (model role reconfig) without rebuilding Deps.
// Returns nil when the role has no configured model.
type Deps struct {
	Logger        *slog.Logger
	AuthToken     string // empty disables bearer enforcement (dev/loopback)
	ModelRegistry ModelRegistry
	ChatClient    func(role modelrole.Role) ChatStreamer
	StartedAt     func() time.Time
}

// ModelRegistry is the subset of *modelrole.Registry consumed here.
// Defined as an interface so tests can supply fakes without pulling
// the full registry construction.
type ModelRegistry interface {
	Model(role modelrole.Role) string       // bare model name for upstream wire
	FullModelID(role modelrole.Role) string // "provider/model" for /v1/models display
}

// ChatStreamer is the subset of *llm.Client used by chat completions.
// Defined as an interface so the handler can be tested with a fake
// streamer that yields canned events.
type ChatStreamer interface {
	StreamChat(ctx context.Context, req llm.ChatRequest) (<-chan llm.StreamEvent, error)
}

// Mount registers /v1/* routes on mux.
func Mount(mux *http.ServeMux, deps Deps) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	r := &routes{deps: deps}
	mux.Handle("GET /v1/models", r.bearerAuth(http.HandlerFunc(r.handleModels)))
	mux.Handle("POST /v1/chat/completions", r.bearerAuth(http.HandlerFunc(r.handleChatCompletions)))
}

type routes struct {
	deps Deps
}
