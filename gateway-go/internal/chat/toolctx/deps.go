package toolctx

import (
	"github.com/choiceoh/deneb/gateway-go/internal/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/cron"
	"github.com/choiceoh/deneb/gateway-go/internal/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/process"
	"github.com/choiceoh/deneb/gateway-go/internal/session"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// CoreToolDeps holds all dependencies for core agent tools.
// It composes focused dep structs for each tool group.
type CoreToolDeps struct {
	WorkspaceDir   string
	Process        ProcessDeps
	Sessions       SessionDeps
	Chrono         ChronoDeps
	Vega           VegaDeps
	LLMClient      *llm.Client
	DefaultModel   string
	ImageClient    *llm.Client            // lightweight model client for image analysis
	ImageModel     string                 // lightweight model name for image analysis
	AgentLog       *agentlog.Writer
	SpilloverStore *agent.SpilloverStore  // optional; spills large tool results to disk
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
}

// ChronoDeps holds dependencies for the cron scheduling tool.
type ChronoDeps struct {
	Scheduler *cron.Scheduler
	// SendFn sends a message to a target session, triggering an agent run.
	SendFn func(sessionKey, message string) error
}

// VegaDeps holds dependencies for vega search and health-check tools.
type VegaDeps struct {
	Backend        vega.Backend     // may be nil until SetVega is called
	MemoryStore    *memory.Store    // may be nil when aurora-memory is not configured
	MemoryEmbedder *memory.Embedder // may be nil when embedding is not configured
	RecallClient   *llm.Client      // fallback LLM for memory recall; may be nil
	RecallModel    string           // fallback model name for recall
}
