package autoreply

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
)

// CommandContextFull provides the full context for command dispatch,
// mirroring CommandContext from src/auto-reply/reply/commands-types.ts.
type CommandContextFull struct {
	Surface               string
	Channel               string
	ChannelID             string
	OwnerList             []string
	SenderIsOwner         bool
	IsAuthorizedSender    bool
	SenderID              string
	AbortKey              string
	RawBodyNormalized     string
	CommandBodyNormalized string
	From                  string
	To                    string
	// Internal marker to prevent duplicate reset-hook emission.
	ResetHookTriggered bool
}

// HandleCommandsFullParams holds the full parameter set for the commands dispatch pipeline.
//
// Mirrors HandleCommandsParams from src/auto-reply/reply/commands-types.ts.
type HandleCommandsFullParams struct {
	Ctx           *types.MsgContext
	RootCtx       *types.MsgContext // ACP root context (may differ from Ctx)
	Command       CommandContextFull
	AgentID       string
	AgentDir      string
	SessionKey    string
	WorkspaceDir  string
	Provider      string
	Model         string
	ContextTokens int
	IsGroup       bool

	// Resolved inference levels.
	ResolvedThinkLevel       types.ThinkLevel
	ResolvedVerboseLevel     types.VerboseLevel
	ResolvedReasoningLevel   types.ReasoningLevel
	ResolvedElevatedLevel    types.ElevatedLevel
	ResolvedBlockStreamBreak string // "text_end" | "message_end"

	// Typing.
	Typing *TypingController

	// Elevated mode state.
	Elevated ElevatedState
}

// ElevatedState tracks elevated mode permissions.
type ElevatedState struct {
	Enabled  bool
	Allowed  bool
	Failures []ElevatedFailure
}

// ElevatedFailure records a specific gate failure.
type ElevatedFailure struct {
	Gate string `json:"gate"`
	Key  string `json:"key"`
}

// CommandHandlerFullResult holds the outcome of a command handler.
//
// Mirrors CommandHandlerResult from src/auto-reply/reply/commands-types.ts.
type CommandHandlerFullResult struct {
	Reply          *types.ReplyPayload
	ShouldContinue bool
}

// CommandHandlerFull is a function that processes a command and returns a result.
//
// Mirrors CommandHandler from src/auto-reply/reply/commands-types.ts.
type CommandHandlerFull func(params HandleCommandsFullParams, allowTextCommands bool) *CommandHandlerFullResult
