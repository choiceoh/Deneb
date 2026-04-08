package telegram

// ChunkMode controls how outbound messages are split.
type ChunkMode string

const (
	ChunkModeLength  ChunkMode = "length"
	ChunkModeNewline ChunkMode = "newline"
)

// SessionThreadBindingsConfig controls session-to-thread binding behavior.
type SessionThreadBindingsConfig struct {
	// Enabled toggles thread binding on/off (maps to deneb.json "enabled").
	Enabled *bool `json:"enabled,omitempty"`
	// SpawnSubagentSessions controls whether bound threads spawn sub-agent sessions.
	SpawnSubagentSessions *bool `json:"spawnSubagentSessions,omitempty"`
	// Mode controls binding behavior: "off", "auto", "explicit".
	Mode string `json:"mode,omitempty"`
}

// Config holds Telegram channel configuration loaded from deneb.json.
// Single-user, single-channel deployment — no multi-user access control.
type Config struct {
	// BotToken is the Telegram Bot API token.
	BotToken string `json:"botToken"`

	// ChatID is the operator's Telegram chat ID for message delivery
	// (cron output, gmail notifications, dreaming events, etc.).
	ChatID int64 `json:"chatID,omitempty"`

	// Enabled controls whether this Telegram account is active. Default: true.
	Enabled *bool `json:"enabled,omitempty"`

	// --- Streaming ---

	// BlockStreaming disables block streaming for this account.
	BlockStreaming *bool `json:"blockStreaming,omitempty"`

	// --- Optional features ---

	// DmHistoryLimit is the max DM turns to keep as history context.
	DmHistoryLimit *int `json:"dmHistoryLimit,omitempty"`
	// ThreadBindings controls session-to-thread binding behavior.
	ThreadBindings *SessionThreadBindingsConfig `json:"threadBindings,omitempty"`

	// --- Connection ---

	// Proxy is an HTTP proxy URL for API calls.
	Proxy string `json:"proxy,omitempty"`
	// TimeoutSeconds is the API call timeout (default 30).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// Silent disables notification sounds for sent messages.
	Silent bool `json:"silent,omitempty"`
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
