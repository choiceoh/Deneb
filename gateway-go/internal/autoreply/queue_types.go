// queue_types.go — Extended queue types for the followup queue system.
// Mirrors src/auto-reply/reply/queue/types.ts (94 LOC).
package autoreply

// FollowupQueueMode defines how followup messages are processed.
// This extends the simpler QueueMode used in the basic message queue.
type FollowupQueueMode string

const (
	FollowupModeSteer        FollowupQueueMode = "steer"
	FollowupModeFollowup     FollowupQueueMode = "followup"
	FollowupModeCollect      FollowupQueueMode = "collect"
	FollowupModeSteerBacklog FollowupQueueMode = "steer-backlog"
	FollowupModeInterrupt    FollowupQueueMode = "interrupt"
	FollowupModeQueue        FollowupQueueMode = "queue"
)

// FollowupDropPolicy defines what happens when the followup queue overflows.
type FollowupDropPolicy string

const (
	FollowupDropOld       FollowupDropPolicy = "old"
	FollowupDropNew       FollowupDropPolicy = "new"
	FollowupDropSummarize FollowupDropPolicy = "summarize"
)

// FollowupDedupeMode controls how duplicate queue entries are detected.
type FollowupDedupeMode string

const (
	DedupeMessageID FollowupDedupeMode = "message-id"
	DedupePrompt    FollowupDedupeMode = "prompt"
	DedupeNone      FollowupDedupeMode = "none"
)

// FollowupQueueSettings configures the followup queue behavior.
type FollowupQueueSettings struct {
	Mode       FollowupQueueMode  `json:"mode"`
	DebounceMs int                `json:"debounceMs,omitempty"`
	Cap        int                `json:"cap,omitempty"`
	DropPolicy FollowupDropPolicy `json:"dropPolicy,omitempty"`
}

// FollowupRunContext holds the agent execution context for a queued followup run.
type FollowupRunContext struct {
	AgentID          string `json:"agentId"`
	AgentDir         string `json:"agentDir"`
	SessionID        string `json:"sessionId"`
	SessionKey       string `json:"sessionKey,omitempty"`
	SessionFile      string `json:"sessionFile"`
	WorkspaceDir     string `json:"workspaceDir"`
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	AuthProfileID    string `json:"authProfileId,omitempty"`
	TimeoutMs        int64  `json:"timeoutMs"`
	MessageProvider  string `json:"messageProvider,omitempty"`
	AgentAccountID   string `json:"agentAccountId,omitempty"`
	GroupID          string `json:"groupId,omitempty"`
	GroupChannel     string `json:"groupChannel,omitempty"`
	GroupSpace       string `json:"groupSpace,omitempty"`
	SenderID         string `json:"senderId,omitempty"`
	SenderName       string `json:"senderName,omitempty"`
	SenderUsername   string `json:"senderUsername,omitempty"`
	SenderIsOwner    bool   `json:"senderIsOwner,omitempty"`
	ThinkLevel       string `json:"thinkLevel,omitempty"`
	VerboseLevel     string `json:"verboseLevel,omitempty"`
	ReasoningLevel   string `json:"reasoningLevel,omitempty"`
	ElevatedLevel    string `json:"elevatedLevel,omitempty"`
	BlockReplyBreak  string `json:"blockReplyBreak,omitempty"`
}

// FollowupRun represents a queued followup message with routing metadata.
type FollowupRun struct {
	Prompt               string              `json:"prompt"`
	MessageID            string              `json:"messageId,omitempty"`
	SummaryLine          string              `json:"summaryLine,omitempty"`
	EnqueuedAt           int64               `json:"enqueuedAt"`
	OriginatingChannel   string              `json:"originatingChannel,omitempty"`
	OriginatingTo        string              `json:"originatingTo,omitempty"`
	OriginatingAccountID string              `json:"originatingAccountId,omitempty"`
	OriginatingThreadID  string              `json:"originatingThreadId,omitempty"`
	OriginatingChatType  string              `json:"originatingChatType,omitempty"`
	Run                  *FollowupRunContext `json:"run"`
}

// ResolveFollowupQueueSettingsParams holds the inputs for resolving queue settings.
type ResolveFollowupQueueSettingsParams struct {
	Channel     string
	InlineMode  FollowupQueueMode
	SessionMode string // from session entry
	ConfigMode  string // from config
	DebounceMs  int
	Cap         int
	DropPolicy  FollowupDropPolicy
}
