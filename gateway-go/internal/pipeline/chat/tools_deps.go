package chat

import "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"

// Type aliases — canonical definitions are in toolctx/.

// CoreToolDeps holds all dependencies for core agent tools.
type CoreToolDeps = toolctx.CoreToolDeps

// ProcessDeps holds dependencies for exec and process management tools.
type ProcessDeps = toolctx.ProcessDeps

// SessionDeps holds dependencies for session management tools.
type SessionDeps = toolctx.SessionDeps

// ChronoDeps holds dependencies for the cron scheduling tool.
type ChronoDeps = toolctx.ChronoDeps

// WikiDeps holds dependencies for the wiki knowledge base tool.
type WikiDeps = toolctx.WikiDeps
