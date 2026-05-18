package server

import (
	"encoding/json"
	"strconv"
)

// extractCronDefaultTo parses the operator's Telegram chat ID from the raw
// deneb.json config and returns it as a string suitable for use as
// cron.ServiceConfig.DefaultTo. Returns "" when Telegram is not usable
// (no chatID, or no botToken/botTokenRef configured).
//
// This intentionally avoids resolving secret references (botTokenRef via
// 1Password) — that resolution belongs to plugin construction in
// registerEarlyMethods. The chat ID alone is a plain int, so cron seeding
// does not need to pay the resolver cost on the startup path.
//
// Gating on a non-empty token mirrors the plugin-creation check at
// method_registry.go: if no token is configured, the Telegram plugin will
// not be created, and seeding a DefaultTo here would otherwise cause cron
// jobs (without a per-job Delivery.To) to resolve a target, run, then have
// delivery silently skipped — masking the misconfiguration as a successful
// run.
func extractCronDefaultTo(raw string) string {
	if raw == "" {
		return ""
	}
	var root struct {
		Channels struct {
			Telegram struct {
				ChatID      int64  `json:"chatID"`
				BotToken    string `json:"botToken"`
				BotTokenRef string `json:"botTokenRef"`
			} `json:"telegram"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(raw), &root); err != nil {
		return ""
	}
	tg := root.Channels.Telegram
	if tg.ChatID == 0 {
		return ""
	}
	if tg.BotToken == "" && tg.BotTokenRef == "" {
		return ""
	}
	return strconv.FormatInt(tg.ChatID, 10)
}
