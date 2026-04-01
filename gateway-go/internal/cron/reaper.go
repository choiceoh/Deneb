package cron

import (
	"strings"
)

const (
	defaultRetentionMs = 24 * 3600 * 1000 // 24 hours
)

// IsCronRunSessionKey returns true if the key looks like a cron run session.
func IsCronRunSessionKey(key string) bool {
	return strings.HasPrefix(key, "cron:")
}

// ResolveRetentionMs returns the configured retention or the default.
// Kept for backward-compatible config parsing; actual GC is handled by
// session.Manager's Kind-based retention (KindCron → 24h).
func ResolveRetentionMs(configuredMs int64) int64 {
	if configuredMs > 0 {
		return configuredMs
	}
	return defaultRetentionMs
}
