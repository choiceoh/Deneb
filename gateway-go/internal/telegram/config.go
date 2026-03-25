package telegram

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Config holds Telegram channel configuration loaded from deneb.json.
type Config struct {
	// BotToken is the Telegram Bot API token.
	BotToken string `json:"botToken"`
	// AllowFrom is the allowlist for DM senders.
	// Supports numeric user IDs, "@username" strings, and "*" wildcard.
	AllowFrom AllowList `json:"allowFrom,omitempty"`
	// GroupAllowFrom is the allowlist for group message senders.
	// Same format as AllowFrom.
	GroupAllowFrom AllowList `json:"groupAllowFrom,omitempty"`
	// Proxy is an HTTP proxy URL for API calls.
	Proxy string `json:"proxy,omitempty"`
	// TimeoutSeconds is the API call timeout (default 30).
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
	// LinkPreview controls whether link previews are shown (default true).
	// Pointer distinguishes unset (nil → true) from explicit false.
	LinkPreview *bool `json:"linkPreview,omitempty"`
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

// EffectiveLinkPreview returns the link preview setting, defaulting to true.
func (c *Config) EffectiveLinkPreview() bool {
	if c.LinkPreview == nil {
		return true
	}
	return *c.LinkPreview
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
