package autoreply

// GetReplyOptions holds the full configuration for generating a reply.
type GetReplyOptions struct {
	RunID                string
	IsHeartbeat          bool
	HeartbeatModelOverride string
	TypingPolicy         TypingPolicy
	SuppressTyping       bool
	SuppressToolErrors   bool
	SkillFilter          []string
	TimeoutOverrideMs    int64
	// Callbacks
	OnAgentRunStart  func(params AgentRunStartParams)
	OnReplyStart     func()
	OnTypingCleanup  func()
	OnBlockReply     func(payload ReplyPayload)
	OnToolResult     func(payload ReplyPayload)
}

// AgentRunStartParams contains metadata about an agent run starting.
type AgentRunStartParams struct {
	SessionKey string
	RunID      string
	Model      string
	Provider   string
}

// BlockReplyContext provides context for block-level reply delivery.
type BlockReplyContext struct {
	SessionKey string
	Channel    string
	To         string
	AccountID  string
	ThreadID   string
}

// ModelSelectedContext tracks the resolved model for a reply.
type ModelSelectedContext struct {
	Provider     string
	Model        string
	ThinkLevel   ThinkLevel
	FastMode     bool
	IsOverride   bool
	IsFallback   bool
}

// MsgContext represents the inbound message context (mirrors TS MsgContext).
type MsgContext struct {
	Body               string
	BodyForAgent       string
	CommandBody        string
	BodyForCommands    string
	RawBody            string
	From               string
	To                 string
	SessionKey         string
	MessageSid         string
	ReplyToID          string
	MediaPath          string
	MediaPaths         []string // multiple media file paths
	MediaUrl           string
	MediaUrls          []string // multiple media URLs
	MediaType          string
	MediaTypes         []string // per-file media types (parallel to MediaPaths/MediaUrls)
	MediaRemoteHost    string   // remote host for SCP-based media staging
	Transcript         string
	WasMentioned       bool
	CommandAuthorized  bool
	CommandSource      string // "text" or "native"
	Channel            string
	AccountID          string
	ThreadID           string
	IsGroup            bool
	ChatType           string // "direct", "group", "supergroup", "channel"
	SenderID           string
	SenderName         string
	ForwardedFrom      string
}

// TemplateContext provides agent execution context for system prompt injection.
type TemplateContext struct {
	SessionKey string
	AgentID    string
	Channel    string
	IsGroup    bool
	MediaPath  string
	MediaPaths []string
	MediaUrl   string
	MediaUrls  []string
}
