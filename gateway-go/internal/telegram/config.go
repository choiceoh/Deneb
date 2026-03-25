package telegram

// Config holds Telegram channel configuration loaded from deneb.json.
type Config struct {
	// BotToken is the Telegram Bot API token.
	BotToken string `json:"botToken"`
	// AllowFrom is the list of allowed user IDs for DMs.
	AllowFrom []int64 `json:"allowFrom,omitempty"`
	// GroupAllowFrom is the list of allowed user IDs in groups.
	GroupAllowFrom []int64 `json:"groupAllowFrom,omitempty"`
	// Proxy is an HTTP proxy URL for API calls.
	Proxy string `json:"proxy,omitempty"`
	// TimeoutSeconds is the API call timeout (default 30).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// LinkPreview controls whether link previews are shown.
	LinkPreview bool `json:"linkPreview,omitempty"`
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
