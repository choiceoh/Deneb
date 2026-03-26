// payload_migration.go — Normalize legacy cron payload channel/provider fields.
// Mirrors src/cron/payload-migration.ts (40 LOC).
package cron

import "strings"

// MigrateLegacyCronPayloadMap normalizes the channel/provider field in a payload map.
// If a "provider" field exists, its value is moved to "channel" (lowercased) and "provider" is removed.
// Returns true if any mutation occurred.
func MigrateLegacyCronPayloadMap(payload map[string]any) bool {
	mutated := false

	channelValue, _ := payload["channel"].(string)
	providerValue, _ := payload["provider"].(string)

	nextChannel := ""
	if ch := strings.TrimSpace(channelValue); ch != "" {
		nextChannel = strings.ToLower(ch)
	} else if pv := strings.TrimSpace(providerValue); pv != "" {
		nextChannel = strings.ToLower(pv)
	}

	if nextChannel != "" && channelValue != nextChannel {
		payload["channel"] = nextChannel
		mutated = true
	}

	if _, has := payload["provider"]; has {
		delete(payload, "provider")
		mutated = true
	}

	return mutated
}
