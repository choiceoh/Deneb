// Package auth credentials implements gateway credential resolution for single-user deployment.
//
// This is a simplified port of src/gateway/auth/credential-planner.ts and
// src/gateway/auth/probe-auth.ts for the DGX Spark single-user environment.
package auth

import (
	"os"
	"strings"
)

// CredentialSource describes where a credential was resolved from.
type CredentialSource string

const (
	CredentialSourceEnv       CredentialSource = "env"
	CredentialSourceConfig    CredentialSource = "config"
	CredentialSourceGenerated CredentialSource = "generated"
	CredentialSourceNone      CredentialSource = "none"
)

// CredentialPlan holds the resolved gateway authentication credentials.
// For single-user DGX Spark deployment, this is significantly simplified
// from the TypeScript version (no remote mode, no Tailscale, no secret refs).
type CredentialPlan struct {
	Mode     AuthMode         `json:"mode"`     // "token", "password", "none"
	Token    string           `json:"-"`         // resolved token (never serialized)
	Password string           `json:"-"`         // resolved password (never serialized)
	Source   CredentialSource `json:"source"`    // where the winning credential came from
}

// CredentialConfig holds the config-file credential values needed for resolution.
type CredentialConfig struct {
	AuthMode string // gateway.auth.mode
	Token    string // gateway.auth.token
	Password string // gateway.auth.password
}

// ResolveCredentials resolves gateway authentication credentials from environment
// and config. Resolution order for token: DENEB_GATEWAY_TOKEN env → config token.
// Resolution order for password: DENEB_GATEWAY_PASSWORD env → config password.
func ResolveCredentials(cfg CredentialConfig) *CredentialPlan {
	plan := &CredentialPlan{}

	// Determine auth mode from config (default: token).
	mode := strings.TrimSpace(cfg.AuthMode)
	switch mode {
	case "none":
		plan.Mode = AuthModeNone
		plan.Source = CredentialSourceNone
		return plan
	case "password":
		plan.Mode = AuthModePassword
	default:
		plan.Mode = AuthModeToken
	}

	// Resolve token.
	if plan.Mode == AuthModeToken {
		// 1. Environment variable (primary).
		if envToken := readEnvCredential("DENEB_GATEWAY_TOKEN"); envToken != "" {
			plan.Token = envToken
			plan.Source = CredentialSourceEnv
			return plan
		}
		// 2. Config file.
		if cfg.Token != "" {
			plan.Token = cfg.Token
			plan.Source = CredentialSourceConfig
			return plan
		}
		// No token found — source remains empty.
		plan.Source = CredentialSourceNone
		return plan
	}

	// Resolve password.
	if plan.Mode == AuthModePassword {
		// 1. Environment variable.
		if envPw := readEnvCredential("DENEB_GATEWAY_PASSWORD"); envPw != "" {
			plan.Password = envPw
			plan.Source = CredentialSourceEnv
			return plan
		}
		// 2. Config file.
		if cfg.Password != "" {
			plan.Password = cfg.Password
			plan.Source = CredentialSourceConfig
			return plan
		}
		plan.Source = CredentialSourceNone
		return plan
	}

	return plan
}

// ResolveProbeAuth resolves lightweight read-only credentials for gateway
// health probes. Same resolution as ResolveCredentials but intended for
// probe-role access (read-only, no write scopes).
func ResolveProbeAuth(cfg CredentialConfig) *CredentialPlan {
	plan := ResolveCredentials(cfg)
	// Probe auth uses the same credentials but the caller assigns RoleProbe.
	return plan
}

// readEnvCredential reads a credential from an environment variable.
// Returns empty string if not set or contains only whitespace.
// Rejects unresolved secret references like ${VAR_NAME}.
func readEnvCredential(key string) string {
	val := strings.TrimSpace(os.Getenv(key))
	if val == "" {
		return ""
	}
	// Reject unresolved secret references (e.g., "${SOME_VAR}").
	if strings.HasPrefix(val, "${") && strings.HasSuffix(val, "}") {
		return ""
	}
	return val
}
