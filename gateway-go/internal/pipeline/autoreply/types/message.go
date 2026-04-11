package types

// SessionOrigin holds routing/session fields shared between MsgContext and SessionState.
// Embedded in both to eliminate field duplication (DRY).
type SessionOrigin struct {
	SessionKey string
	Channel    string
	AccountID  string
	ThreadID   string
	IsGroup    bool
}

// MediaContext holds media attachment fields for inbound messages.
// Supports both single-file (MediaPath/MediaURL) and multi-file (MediaPaths/MediaURLs)
// patterns with parallel MediaTypes array for per-file MIME types.
type MediaContext struct {
	MediaPath       string
	MediaPaths      []string // multiple media file paths
	MediaURL        string
	MediaURLs       []string // multiple media URLs
	MediaType       string
	MediaTypes      []string // per-file media types (parallel to MediaPaths/MediaURLs)
	MediaRemoteHost string   // remote host for SCP-based media staging
}

// SenderInfo holds metadata about the message sender.
type SenderInfo struct {
	SenderID      string
	SenderName    string
	ForwardedFrom string
	WasMentioned  bool
	ChatType      string // "direct", "group", "supergroup", "channel"
}

// CommandControl holds command processing state set during inbound dispatch.
type CommandControl struct {
	CommandBody       string
	CommandAuthorized bool
	CommandSource     string // "text", "native", or "inline"
}

// MsgContext represents the inbound message context (mirrors TS MsgContext).
// Fields are grouped by concern via embedded structs; promoted field access
// (e.g. msg.SessionKey, msg.MediaPath) works unchanged at all read sites.
type MsgContext struct {
	// Message content: body variants for different processing stages.
	Body            string
	BodyForAgent    string
	BodyForCommands string
	RawBody         string
	From            string
	To              string
	MessageSid      string
	ReplyToID       string

	SessionOrigin  // routing: SessionKey, Channel, AccountID, ThreadID, IsGroup
	MediaContext   // attachments: MediaPath(s), MediaURL(s), MediaType(s), MediaRemoteHost
	SenderInfo     // sender: SenderID, SenderName, ForwardedFrom, WasMentioned, ChatType
	CommandControl // command: CommandBody, CommandAuthorized, CommandSource
}

// TemplateContext provides agent execution context for system prompt injection.
type TemplateContext struct {
	AgentID string
	SessionOrigin
	MediaContext
}

// BlockReplyContext provides context for block-level reply delivery.
type BlockReplyContext struct {
	To string
	SessionOrigin
}

// ModelSelectedContext tracks the resolved model for a reply.
type ModelSelectedContext struct {
	Provider   string
	Model      string
	FastMode   bool
	IsOverride bool
	IsFallback bool
}

// AgentRunStartParams contains metadata about an agent run starting.
type AgentRunStartParams struct {
	SessionKey string
	RunID      string
	Model      string
	Provider   string
}

// GetReplyOptions holds the full configuration for generating a reply.
type GetReplyOptions struct {
	RunID                  string
	IsHeartbeat            bool
	HeartbeatModelOverride string
	TypingPolicy           TypingPolicy
	SuppressTyping         bool
	SuppressToolErrors     bool
	SkillFilter            []string
	TimeoutOverrideMs      int64
	// Token budget overrides (0 = use model defaults).
	ContextTokens int
	MaxTokens     int
	// Callbacks
	OnAgentRunStart func(params AgentRunStartParams)
	OnReplyStart    func()
	OnTypingCleanup func()
	OnBlockReply    func(payload ReplyPayload)
	OnToolResult    func(payload ReplyPayload)
}
