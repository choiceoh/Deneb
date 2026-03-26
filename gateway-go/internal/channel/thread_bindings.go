// Thread binding policy — resolves thread-to-session binding configuration
// per channel and account, including spawn policy for subagents and ACP.
//
// Mirrors src/channels/thread-bindings-policy.ts.
package channel

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const (
	defaultThreadBindingIdleHours   = 24
	defaultThreadBindingMaxAgeHours = 0
)

// ThreadBindingSpawnKind identifies the type of thread-bound session spawn.
type ThreadBindingSpawnKind string

const (
	SpawnKindSubagent ThreadBindingSpawnKind = "subagent"
	SpawnKindACP      ThreadBindingSpawnKind = "acp"
)

// ThreadBindingSpawnPolicy is the resolved thread binding policy.
type ThreadBindingSpawnPolicy struct {
	Channel      string
	AccountID    string
	Enabled      bool
	SpawnEnabled bool
}

// threadBindingsShape represents the thread bindings configuration block.
type threadBindingsShape struct {
	Enabled              *bool    `json:"enabled,omitempty"`
	IdleHours            *float64 `json:"idleHours,omitempty"`
	MaxAgeHours          *float64 `json:"maxAgeHours,omitempty"`
	SpawnSubagentSessions *bool   `json:"spawnSubagentSessions,omitempty"`
	SpawnAcpSessions     *bool    `json:"spawnAcpSessions,omitempty"`
}

// ResolveThreadBindingIdleTimeoutMs resolves the idle timeout in milliseconds.
// channelHours and sessionHours are optional overrides from config.
func ResolveThreadBindingIdleTimeoutMs(channelHours, sessionHours *float64) int64 {
	hours := defaultThreadBindingIdleHours
	if channelHours != nil && *channelHours >= 0 {
		hours = int(math.Floor(*channelHours))
	} else if sessionHours != nil && *sessionHours >= 0 {
		hours = int(math.Floor(*sessionHours))
	}
	return int64(hours) * 60 * 60 * 1000
}

// ResolveThreadBindingMaxAgeMs resolves the max age timeout in milliseconds.
func ResolveThreadBindingMaxAgeMs(channelHours, sessionHours *float64) int64 {
	hours := defaultThreadBindingMaxAgeHours
	if channelHours != nil && *channelHours >= 0 {
		hours = int(math.Floor(*channelHours))
	} else if sessionHours != nil && *sessionHours >= 0 {
		hours = int(math.Floor(*sessionHours))
	}
	return int64(hours) * 60 * 60 * 1000
}

// ResolveThreadBindingsEnabled resolves whether thread bindings are enabled.
func ResolveThreadBindingsEnabled(channelEnabled, sessionEnabled *bool) bool {
	if channelEnabled != nil {
		return *channelEnabled
	}
	if sessionEnabled != nil {
		return *sessionEnabled
	}
	return true // default enabled
}

// ResolveThreadBindingSpawnPolicy resolves the full spawn policy for a channel/account.
// rawChannelsConfig is the raw JSON of the "channels" config section.
// rawSessionConfig is the raw JSON of the "session" config section.
func ResolveThreadBindingSpawnPolicy(
	rawChannelsConfig, rawSessionConfig json.RawMessage,
	channel, accountID string,
	kind ThreadBindingSpawnKind,
) ThreadBindingSpawnPolicy {
	ch := normalizeChannelID(channel)
	acct := normalizeAccountID(accountID)

	root, account := resolveChannelThreadBindings(rawChannelsConfig, ch, acct)

	// Resolve enabled.
	var sessionEnabled *bool
	sessionTB := extractSessionThreadBindings(rawSessionConfig)
	if sessionTB != nil {
		sessionEnabled = sessionTB.Enabled
	}
	var channelEnabled *bool
	if account != nil && account.Enabled != nil {
		channelEnabled = account.Enabled
	} else if root != nil && root.Enabled != nil {
		channelEnabled = root.Enabled
	}
	enabled := ResolveThreadBindingsEnabled(channelEnabled, sessionEnabled)

	// Resolve spawn enabled.
	var spawnEnabled *bool
	spawnField := "spawnSubagentSessions"
	if kind == SpawnKindACP {
		spawnField = "spawnAcpSessions"
	}
	spawnEnabled = resolveSpawnFlag(account, root, spawnField)

	// Default: non-discord channels have spawn enabled.
	finalSpawn := true
	if spawnEnabled != nil {
		finalSpawn = *spawnEnabled
	} else if ch == "discord" {
		finalSpawn = false
	}

	return ThreadBindingSpawnPolicy{
		Channel:      ch,
		AccountID:    acct,
		Enabled:      enabled,
		SpawnEnabled: finalSpawn,
	}
}

// FormatThreadBindingDisabledError returns a user-facing error message.
func FormatThreadBindingDisabledError(channel, accountID string) string {
	if channel == "discord" {
		return "Discord thread bindings are disabled (set channels.discord.threadBindings.enabled=true to override for this account, or session.threadBindings.enabled=true globally)."
	}
	return fmt.Sprintf("Thread bindings are disabled for %s (set session.threadBindings.enabled=true to enable).", channel)
}

// FormatThreadBindingSpawnDisabledError returns a user-facing error message.
func FormatThreadBindingSpawnDisabledError(channel, accountID string, kind ThreadBindingSpawnKind) string {
	if channel == "discord" {
		return fmt.Sprintf("Discord thread-bound %s spawns are disabled for this account (set channels.discord.threadBindings.spawn%sSessions=true to enable).",
			kind, strings.Title(string(kind))) //nolint:staticcheck
	}
	return fmt.Sprintf("Thread-bound %s spawns are disabled for %s.", kind, channel)
}

// --- internal helpers ---

func normalizeChannelID(v string) string {
	return strings.TrimSpace(strings.ToLower(v))
}

func normalizeAccountID(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return "default"
	}
	return strings.ToLower(trimmed)
}

func resolveChannelThreadBindings(rawChannels json.RawMessage, channel, accountID string) (root, account *threadBindingsShape) {
	if len(rawChannels) == 0 {
		return nil, nil
	}
	var channels map[string]json.RawMessage
	if json.Unmarshal(rawChannels, &channels) != nil {
		return nil, nil
	}
	chRaw, ok := channels[channel]
	if !ok {
		return nil, nil
	}
	var chConfig struct {
		ThreadBindings *threadBindingsShape            `json:"threadBindings,omitempty"`
		Accounts       map[string]*struct {
			ThreadBindings *threadBindingsShape `json:"threadBindings,omitempty"`
		} `json:"accounts,omitempty"`
	}
	if json.Unmarshal(chRaw, &chConfig) != nil {
		return nil, nil
	}
	root = chConfig.ThreadBindings
	if acctCfg, ok := chConfig.Accounts[accountID]; ok && acctCfg != nil {
		account = acctCfg.ThreadBindings
	}
	return root, account
}

func extractSessionThreadBindings(rawSession json.RawMessage) *threadBindingsShape {
	if len(rawSession) == 0 {
		return nil
	}
	var sess struct {
		ThreadBindings *threadBindingsShape `json:"threadBindings,omitempty"`
	}
	if json.Unmarshal(rawSession, &sess) != nil {
		return nil
	}
	return sess.ThreadBindings
}

func resolveSpawnFlag(account, root *threadBindingsShape, field string) *bool {
	if account != nil {
		switch field {
		case "spawnSubagentSessions":
			if account.SpawnSubagentSessions != nil {
				return account.SpawnSubagentSessions
			}
		case "spawnAcpSessions":
			if account.SpawnAcpSessions != nil {
				return account.SpawnAcpSessions
			}
		}
	}
	if root != nil {
		switch field {
		case "spawnSubagentSessions":
			return root.SpawnSubagentSessions
		case "spawnAcpSessions":
			return root.SpawnAcpSessions
		}
	}
	return nil
}
