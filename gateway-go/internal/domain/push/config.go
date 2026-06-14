// Package push delivers proactive notifications to the native client via
// Firebase Cloud Messaging (FCM HTTP v1). It is the fallback path used when no
// native client holds a live SSE connection to GET /api/v1/miniapp/events — i.e.
// the app is fully closed or the device is in Android Doze, so the in-memory
// live push (see runtime/server/client_push.go) reaches nobody.
//
// The send path is DORMANT unless a Google service-account credentials file is
// configured (DENEB_FCM_CREDENTIALS_FILE). With no credentials:
//   - device-token registration (miniapp.push.register) still works and tokens
//     accumulate harmlessly, so delivery starts the moment creds are provisioned;
//   - no FCM request is ever attempted (no project, no network, no behavior change).
//
// This is the "scaffolding behind a flag" posture for an integration whose
// Firebase project has not been provisioned yet. The proactive report is always
// also written to the session transcript, so a missed push is never data loss —
// FCM only changes whether a fully-closed device gets a live tap.
package push

import (
	"os"
	"strings"
)

// Config holds the FCM integration configuration. It is intentionally minimal:
// the project ID, token URI, signing key and client email all live inside the
// service-account JSON, so a single file path is the only thing the operator
// must provide.
type Config struct {
	// CredentialsFile is the path to a Google service-account JSON with the
	// Firebase Cloud Messaging API enabled. Empty disables all sending.
	CredentialsFile string
	// Disabled forces the integration off even when CredentialsFile is set
	// (DENEB_FCM_DISABLE=1) — an operator kill-switch without unsetting the path.
	Disabled bool
}

// Enabled reports whether FCM sending is configured and not force-disabled.
func (c Config) Enabled() bool {
	return !c.Disabled && strings.TrimSpace(c.CredentialsFile) != ""
}

// ConfigFromEnv reads the FCM configuration from environment variables:
//
//	DENEB_FCM_CREDENTIALS_FILE  path to the service-account JSON (enables sending)
//	DENEB_FCM_DISABLE           "1"/"true"/"yes"/"on" to force the integration off
//
// Mirrors the dormant-by-env pattern used by the Hindsight integration
// (DENEB_HINDSIGHT_URL): unset means the whole send path stays asleep.
func ConfigFromEnv() Config {
	return Config{
		CredentialsFile: strings.TrimSpace(os.Getenv("DENEB_FCM_CREDENTIALS_FILE")),
		Disabled:        envTrue(os.Getenv("DENEB_FCM_DISABLE")),
	}
}

func envTrue(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
