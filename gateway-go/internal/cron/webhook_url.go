// webhook_url.go — Validates webhook URLs for cron delivery.
// Mirrors src/cron/webhook-url.ts (22 LOC).
package cron

import "net/url"

// NormalizeHTTPWebhookURL validates that the value is a valid http/https URL.
// Returns the trimmed URL string, or empty string if invalid.
func NormalizeHTTPWebhookURL(value string) string {
	if value == "" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return value
}
