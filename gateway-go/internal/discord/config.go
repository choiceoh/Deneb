package discord

import "fmt"

// Config holds Discord channel configuration loaded from deneb.json.
type Config struct {
	// BotToken is the Discord bot token.
	BotToken string `json:"botToken"`

	// GuildID is the target guild (server) ID.
	GuildID string `json:"guildId"`

	// AllowedChannels is the list of channel IDs the bot responds in.
	// If empty, the bot responds in all channels it can see.
	AllowedChannels []string `json:"allowedChannels,omitempty"`

	// AllowFrom is the list of user IDs allowed to interact with the bot.
	// If empty, all users in the guild can interact.
	AllowFrom []string `json:"allowFrom,omitempty"`

	// Enabled controls whether this Discord account is active. Default: true.
	Enabled *bool `json:"enabled,omitempty"`

	// RequireMention requires @bot mention to trigger a response.
	// When true, messages without a mention are ignored. Default: false.
	RequireMention bool `json:"requireMention,omitempty"`
}

// IsEnabled returns whether this Discord account is active.
func (c *Config) IsEnabled() bool {
	if c.Enabled == nil {
		return true
	}
	return *c.Enabled
}

// IsChannelAllowed checks if the given channel ID is in the allowlist.
// Returns true if no allowlist is configured (allow all).
func (c *Config) IsChannelAllowed(channelID string) bool {
	if len(c.AllowedChannels) == 0 {
		return true
	}
	for _, id := range c.AllowedChannels {
		if id == channelID {
			return true
		}
	}
	return false
}

// IsUserAllowed checks if the given user ID is in the allowlist.
// Returns true if no allowlist is configured (allow all).
func (c *Config) IsUserAllowed(userID string) bool {
	if len(c.AllowFrom) == 0 {
		return true
	}
	for _, id := range c.AllowFrom {
		if id == userID {
			return true
		}
	}
	return false
}

// Validate checks the config for common mistakes and returns an error if invalid.
func (c *Config) Validate() error {
	if c.BotToken == "" {
		return fmt.Errorf("discord: botToken is required")
	}
	if len(c.BotToken) < 50 {
		return fmt.Errorf("discord: botToken looks too short (expected ~70 chars)")
	}
	if c.GuildID != "" && len(c.GuildID) < 17 {
		return fmt.Errorf("discord: guildId looks invalid (expected snowflake ID)")
	}
	for _, ch := range c.AllowedChannels {
		if len(ch) < 17 {
			return fmt.Errorf("discord: allowedChannels entry %q looks invalid (expected snowflake ID)", ch)
		}
	}
	return nil
}
