package toolctx

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group.
type CoreToolDeps struct {
	WorkspaceDir   string
	Process        ProcessDeps
	Sessions       SessionDeps
	Chrono         ChronoDeps
	Wiki           WikiDeps
	LLMClient      *llm.Client
	DefaultModel   string
	AgentLog       *agentlog.Writer
	SpilloverStore *agent.SpilloverStore // optional; spills large tool results to disk

	// SessionMemoryFn returns session memory content for a given session key.
	// Nil means no session memory is available.
	SessionMemoryFn func(sessionKey string) string
}

// ProcessDeps holds dependencies for exec and process management tools.
type ProcessDeps struct {
	Mgr          *process.Manager
	WorkspaceDir string
}

// SessionDeps holds dependencies for session management tools.
type SessionDeps struct {
	Manager    *session.Manager
	Transcript TranscriptStore
	// SendFn sends a message to a target session, triggering an agent run.
	SendFn func(sessionKey, message string) error
	// SubagentDefaultModel is the default model for sub-agent sessions
	// (from agents.defaults.subagents.model in deneb.json).
	SubagentDefaultModel string
}

// ChronoDeps holds dependencies for the cron scheduling tool.
type ChronoDeps struct {
	Service *cron.Service          // persistent cron service
	RunLog  *cron.PersistentRunLog // run history
	// SendFn sends a message to a target session, triggering an agent run.
	SendFn func(sessionKey, message string) error
}

// WikiDeps holds dependencies for the wiki knowledge base tool.
type WikiDeps struct {
	Store *wiki.Store // may be nil when wiki is not enabled
}
