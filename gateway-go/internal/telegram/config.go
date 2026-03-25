package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DmPolicy controls how Telegram DMs are handled.
type DmPolicy string

const (
	// DmPolicyPairing requires unknown senders to present a pairing code.
	DmPolicyPairing DmPolicy = "pairing"
	// DmPolicyAllowlist only allows senders in the AllowFrom list.
	DmPolicyAllowlist DmPolicy = "allowlist"
	// DmPolicyOpen allows all inbound DMs.
	DmPolicyOpen DmPolicy = "open"
	// DmPolicyDisabled ignores all inbound DMs.
	DmPolicyDisabled DmPolicy = "disabled"
)

// GroupPolicy controls how group messages are handled.
type GroupPolicy string

const (
	// GroupPolicyOpen allows all group messages (only mention-gating applies).
	GroupPolicyOpen GroupPolicy = "open"
	// GroupPolicyAllowlist only allows group messages from senders in groupAllowFrom/allowFrom.
	GroupPolicyAllowlist GroupPolicy = "allowlist"
	// GroupPolicyDisabled blocks all group messages entirely.
	GroupPolicyDisabled GroupPolicy = "disabled"
)

// StreamingMode controls stream preview behavior.
type StreamingMode string

const (
	StreamingOff      StreamingMode = "off"
	StreamingPartial  StreamingMode = "partial"
	StreamingBlock    StreamingMode = "block"
	StreamingProgress StreamingMode = "progress"
)

// ReplyToMode controls reply threading when reply tags are present.
type ReplyToMode string

const (
	ReplyToOff   ReplyToMode = "off"
	ReplyToFirst ReplyToMode = "first"
	ReplyToAll   ReplyToMode = "all"
)

// ChunkMode controls how outbound messages are split.
type ChunkMode string

const (
	ChunkModeLength  ChunkMode = "length"
	ChunkModeNewline ChunkMode = "newline"
)

// ReactionLevel controls agent's reaction capability.
type ReactionLevel string

const (
	ReactionOff       ReactionLevel = "off"
	ReactionAck       ReactionLevel = "ack"
	ReactionMinimal   ReactionLevel = "minimal"
	ReactionExtensive ReactionLevel = "extensive"
)

// TopicConfig holds per-topic overrides within a group or DM.
type TopicConfig struct {
	// RequireMention overrides the group-level mention requirement for this topic.
	RequireMention *bool `json:"requireMention,omitempty"`
	// GroupPolicy overrides the group-level group policy for this topic.
	GroupPolicy GroupPolicy `json:"groupPolicy,omitempty"`
	// Enabled disables the bot for this topic when false.
	Enabled *bool `json:"enabled,omitempty"`
	// AllowFrom is an optional sender allowlist for this topic.
	AllowFrom AllowList `json:"allowFrom,omitempty"`
	// AgentID routes this topic to a specific agent.
	AgentID string `json:"agentId,omitempty"`
}

// GroupConfig holds per-group configuration.
type GroupConfig struct {
	// RequireMention requires the bot to be @mentioned to respond.
	RequireMention *bool `json:"requireMention,omitempty"`
	// GroupPolicy overrides the account-level group policy for this group.
	GroupPolicy GroupPolicy `json:"groupPolicy,omitempty"`
	// Enabled disables the bot for this group when false.
	Enabled *bool `json:"enabled,omitempty"`
	// AllowFrom is an optional sender allowlist for this group.
	AllowFrom AllowList `json:"allowFrom,omitempty"`
	// Topics holds per-topic overrides (key is message_thread_id as string).
	Topics map[string]*TopicConfig `json:"topics,omitempty"`
}

// DirectConfig holds per-DM configuration.
type DirectConfig struct {
	// DmPolicy overrides the account-level DM policy for this chat.
	DmPolicy DmPolicy `json:"dmPolicy,omitempty"`
	// Enabled disables the bot for this DM when false.
	Enabled *bool `json:"enabled,omitempty"`
	// AllowFrom is an optional sender allowlist for this DM.
	AllowFrom AllowList `json:"allowFrom,omitempty"`
	// Topics holds per-topic overrides within this DM (key is message_thread_id).
	Topics map[string]*TopicConfig `json:"topics,omitempty"`
}

// SessionThreadBindingsConfig controls session-to-thread binding behavior.
type SessionThreadBindingsConfig struct {
	// Enabled toggles thread binding on/off (maps to deneb.json "enabled").
	Enabled *bool `json:"enabled,omitempty"`
	// SpawnSubagentSessions controls whether bound threads spawn sub-agent sessions.
	SpawnSubagentSessions *bool `json:"spawnSubagentSessions,omitempty"`
	// Mode controls binding behavior: "off", "auto", "explicit".
	Mode string `json:"mode,omitempty"`
}

// ExecApprovalConfig configures Telegram-native exec approval delivery.
type ExecApprovalConfig struct {
	// Enabled enables Telegram exec approvals.
	Enabled bool `json:"enabled,omitempty"`
	// Approvers is the list of Telegram user IDs allowed to approve exec requests.
	Approvers []int64 `json:"approvers,omitempty"`
	// AgentFilter restricts approvals to specific agent IDs.
	AgentFilter []string `json:"agentFilter,omitempty"`
	// SessionFilter restricts approvals to matching session key patterns.
	SessionFilter []string `json:"sessionFilter,omitempty"`
	// Target controls where approval prompts are sent: "dm", "channel", "both".
	Target string `json:"target,omitempty"`
}

// Config holds Telegram channel configuration loaded from deneb.json.
type Config struct {
	// BotToken is the Telegram Bot API token.
	BotToken string `json:"botToken"`

	// --- Access control (required) ---

	// DmPolicy controls how DMs are handled (default: "pairing").
	DmPolicy DmPolicy `json:"dmPolicy,omitempty"`
	// GroupPolicy controls how group messages are handled (default: "open").
	GroupPolicy GroupPolicy `json:"groupPolicy,omitempty"`
	// Enabled controls whether this Telegram account is active. Default: true.
	Enabled *bool `json:"enabled,omitempty"`

	// --- Allowlists ---

	// AllowFrom is the allowlist for DM senders.
	// Supports numeric user IDs, "@username" strings, and "*" wildcard.
	AllowFrom AllowList `json:"allowFrom,omitempty"`
	// GroupAllowFrom is the allowlist for group message senders.
	// Same format as AllowFrom.
	GroupAllowFrom AllowList `json:"groupAllowFrom,omitempty"`

	// --- Per-chat overrides ---

	// Groups holds per-group configuration (key is chat ID as string).
	Groups map[string]*GroupConfig `json:"groups,omitempty"`
	// Direct holds per-DM configuration (key is chat ID as string).
	Direct map[string]*DirectConfig `json:"direct,omitempty"`

	// --- Streaming (required) ---

	// Streaming controls stream preview mode.
	Streaming StreamingMode `json:"streaming,omitempty"`
	// BlockStreaming disables block streaming for this account.
	BlockStreaming *bool `json:"blockStreaming,omitempty"`

	// --- Nice-to-have features ---

	// ChunkMode controls message splitting: "length" (default) or "newline".
	ChunkMode ChunkMode `json:"chunkMode,omitempty"`
	// ReplyToMode controls reply threading (off|first|all).
	ReplyToMode ReplyToMode `json:"replyToMode,omitempty"`
	// DmHistoryLimit is the max DM turns to keep as history context.
	DmHistoryLimit *int `json:"dmHistoryLimit,omitempty"`
	// ThreadBindings controls session-to-thread binding behavior.
	ThreadBindings *SessionThreadBindingsConfig `json:"threadBindings,omitempty"`
	// ReactionLevel controls agent's reaction capability (off|ack|minimal|extensive).
	ReactionLevel ReactionLevel `json:"reactionLevel,omitempty"`
	// ConfigWrites allows channel-initiated config writes (default: true).
	ConfigWrites *bool `json:"configWrites,omitempty"`
	// ExecApprovals configures Telegram-native exec approval delivery.
	ExecApprovals *ExecApprovalConfig `json:"execApprovals,omitempty"`

	// --- Existing fields ---

	// Proxy is an HTTP proxy URL for API calls.
	Proxy string `json:"proxy,omitempty"`
	// TimeoutSeconds is the API call timeout (default 30).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// LinkPreview controls whether link previews are shown (default true).
	// Pointer distinguishes unset (nil → true) from explicit false.
	LinkPreview *bool `json:"linkPreview,omitempty"`
	// Silent disables notification sounds for sent messages.
	Silent bool `json:"silent,omitempty"`
	// TextChunkLimit overrides the outbound text chunk size (chars). Default: 4000.
	TextChunkLimit int `json:"textChunkLimit,omitempty"`
}

// EffectiveTimeout returns the timeout in seconds, using the default if not set.
func (c *Config) EffectiveTimeout() int {
	if c.TimeoutSeconds > 0 {
		return c.TimeoutSeconds
	}
	return 30
}

// IsEnabled returns whether this Telegram account is active.
func (c *Config) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsBlockStreamingDisabled returns whether block streaming is disabled.
func (c *Config) IsBlockStreamingDisabled() bool {
	if c.BlockStreaming == nil {
		return false
	}
	return *c.BlockStreaming
}

// IsConfigWritesEnabled returns whether channel config writes are allowed.
func (c *Config) IsConfigWritesEnabled() bool {
	if c.ConfigWrites == nil {
		return true
	}
	return *c.ConfigWrites
}

// EffectiveLinkPreview returns the link preview setting, defaulting to true.
func (c *Config) EffectiveLinkPreview() bool {
	if c.LinkPreview == nil {
		return true
	}
	return *c.LinkPreview
}

// EffectiveDmPolicy returns the DM policy, defaulting to "pairing".
func (c *Config) EffectiveDmPolicy() DmPolicy {
	if c.DmPolicy == "" {
		return DmPolicyPairing
	}
	return c.DmPolicy
}

// EffectiveGroupPolicy returns the group policy, defaulting to "open".
func (c *Config) EffectiveGroupPolicy() GroupPolicy {
	if c.GroupPolicy == "" {
		return GroupPolicyOpen
	}
	return c.GroupPolicy
}

// EffectiveStreamingMode returns the streaming mode, defaulting to "off".
func (c *Config) EffectiveStreamingMode() StreamingMode {
	if c.Streaming == "" {
		return StreamingOff
	}
	return c.Streaming
}

// EffectiveChunkMode returns the chunk mode, defaulting to "length".
func (c *Config) EffectiveChunkMode() ChunkMode {
	if c.ChunkMode == "" {
		return ChunkModeLength
	}
	return c.ChunkMode
}

// EffectiveReplyToMode returns the reply-to mode, defaulting to "off".
func (c *Config) EffectiveReplyToMode() ReplyToMode {
	if c.ReplyToMode == "" {
		return ReplyToOff
	}
	return c.ReplyToMode
}

// EffectiveReactionLevel returns the reaction level, defaulting to "ack".
func (c *Config) EffectiveReactionLevel() ReactionLevel {
	if c.ReactionLevel == "" {
		return ReactionAck
	}
	return c.ReactionLevel
}

// EffectiveTextChunkLimit returns the text chunk limit, defaulting to TextChunkLimit.
func (c *Config) EffectiveTextChunkLimit() int {
	if c.TextChunkLimit > 0 {
		return c.TextChunkLimit
	}
	return TextChunkLimit
}

// AllowList holds a parsed allowlist that supports numeric IDs, usernames, and wildcards.
// Matches the TypeScript AllowFrom type: Array<string | number>.
type AllowList struct {
	IDs       []int64
	Usernames []string
	Wildcard  bool
}

// AllowsAll returns true if the wildcard "*" is set.
func (a *AllowList) AllowsAll() bool {
	return a.Wildcard
}

// IsEmpty returns true if no entries are configured.
func (a *AllowList) IsEmpty() bool {
	return !a.Wildcard && len(a.IDs) == 0 && len(a.Usernames) == 0
}

// ContainsID checks if the given user ID is in the allowlist.
func (a *AllowList) ContainsID(id int64) bool {
	for _, v := range a.IDs {
		if v == id {
			return true
		}
	}
	return false
}

// ContainsUsername checks if the given username is in the allowlist (case-insensitive).
func (a *AllowList) ContainsUsername(username string) bool {
	lower := strings.ToLower(username)
	for _, v := range a.Usernames {
		if strings.ToLower(v) == lower {
			return true
		}
	}
	return false
}

// MatchesUser checks if the given user matches this allowlist by ID, username, or wildcard.
func (a *AllowList) MatchesUser(user *User) bool {
	if a.IsEmpty() || a.AllowsAll() {
		return true
	}
	if a.ContainsID(user.ID) {
		return true
	}
	if user.Username != "" && a.ContainsUsername(user.Username) {
		return true
	}
	return false
}

// UnmarshalJSON parses a JSON array of mixed numbers and strings.
// Numbers → IDs, "*" → Wildcard, strings → Usernames (with optional "@" prefix stripped).
func (a *AllowList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("allowList: expected JSON array: %w", err)
	}

	for _, elem := range raw {
		// Try number first.
		var num int64
		if err := json.Unmarshal(elem, &num); err == nil {
			a.IDs = append(a.IDs, num)
			continue
		}

		// Must be a string.
		var str string
		if err := json.Unmarshal(elem, &str); err != nil {
			return fmt.Errorf("allowList: element must be number or string: %s", string(elem))
		}

		if str == "*" {
			a.Wildcard = true
		} else {
			a.Usernames = append(a.Usernames, strings.TrimPrefix(str, "@"))
		}
	}
	return nil
}

// MarshalJSON serializes the AllowList back to a JSON array.
func (a AllowList) MarshalJSON() ([]byte, error) {
	var elems []any
	for _, id := range a.IDs {
		elems = append(elems, id)
	}
	if a.Wildcard {
		elems = append(elems, "*")
	}
	for _, u := range a.Usernames {
		elems = append(elems, "@"+u)
	}
	if elems == nil {
		return []byte("[]"), nil
	}
	return json.Marshal(elems)
}
