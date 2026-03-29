package chat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"

	"github.com/choiceoh/deneb/gateway-go/internal/cron"
)

// ProcessDeps holds dependencies for exec and process management tools.
type ProcessDeps struct {
	Mgr          *process.Manager
	WorkspaceDir string
}

// SessionDeps holds dependencies for session management tools.
// SendFn may be wired after initial construction to avoid circular deps.
type SessionDeps struct {
	Manager    *session.Manager
	Transcript TranscriptStore
	// SendFn sends a message to a target session, triggering an agent run.
	// Set after Handler creation to avoid circular deps.
	SendFn func(sessionKey, message string) error
}

// ChronoDeps holds dependencies for the cron scheduling tool.
// SendFn may be wired after initial construction to avoid circular deps.
type ChronoDeps struct {
	Scheduler *cron.Scheduler
	// SendFn sends a message to a target session, triggering an agent run.
	// Set after Handler creation to avoid circular deps.
	SendFn func(sessionKey, message string) error
}

// VegaDeps holds dependencies for vega search and health-check tools.
// Backend may be set after initial construction via SetVega late-binding.
type VegaDeps struct {
	Backend     vega.Backend  // may be nil until SetVega is called
	MemoryStore *memory.Store // may be nil when aurora-memory is not configured
}
