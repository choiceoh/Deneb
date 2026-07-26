package process

import (
	"log/slog"
	"strings"
)

// isBlockedEnvKey returns true if the given environment variable key
// should be stripped from subprocess environments.
func isBlockedEnvKey(key string) bool {
	if _, ok := blockedEnvKeys[key]; ok {
		return true
	}
	upper := strings.ToUpper(key)
	for _, prefix := range blockedEnvPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	// Block NODE_OPTIONS containing --require or --import (partial filter).
	// NODE_OPTIONS itself is allowed for safe flags like --max-old-space-size.
	if upper == "NODE_OPTIONS" {
		return false // handled separately in sanitizeNodeOptions
	}
	return false
}

// sanitizeNodeOptions removes dangerous flags from NODE_OPTIONS.
// Returns the sanitized value. If empty after sanitization, returns "".
func sanitizeNodeOptions(value string) string {
	parts := strings.Fields(value)
	var safe []string
	skip := false
	for _, part := range parts {
		if skip {
			skip = false
			continue
		}
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "--require") || strings.HasPrefix(lower, "--import") ||
			strings.HasPrefix(lower, "-r=") || strings.HasPrefix(lower, "--loader") {
			// If it's --require=X or --require X (next arg)
			if !strings.Contains(part, "=") {
				skip = true // skip next argument too
			}
			continue
		}
		safe = append(safe, part)
	}
	return strings.Join(safe, " ")
}

// SanitizeEnv filters a slice of "KEY=VALUE" strings before a subprocess spawn.
// It removes two classes silently: code-injection vectors (isBlockedEnvKey) and
// the operator's credentials (isSecretEnvKey) — the latter so a model-controlled
// shell can't exfiltrate provider keys. NODE_OPTIONS is sanitized in place rather
// than fully blocked.
func SanitizeEnv(env []string, logger *slog.Logger) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		key, value, found := strings.Cut(entry, "=")
		if !found {
			result = append(result, entry)
			continue
		}
		if isBlockedEnvKey(key) {
			if logger != nil {
				logger.Info("exec sandbox: blocked env var", "key", key)
			}
			continue
		}
		if isSecretEnvKey(key) {
			if logger != nil {
				logger.Info("exec sandbox: withheld secret env var", "key", key)
			}
			continue
		}
		if strings.ToUpper(key) == "NODE_OPTIONS" {
			sanitized := sanitizeNodeOptions(value)
			if sanitized != value && logger != nil {
				logger.Info("exec sandbox: sanitized NODE_OPTIONS")
			}
			if sanitized != "" {
				result = append(result, key+"="+sanitized)
			}
			continue
		}
		result = append(result, entry)
	}
	return result
}
