package auth

import "strings"

// NormalizeInputHostnameAllowlist normalizes a hostname allowlist for security validation.
// Returns nil if the input is nil, empty, or contains only whitespace entries.
// This mirrors src/gateway/auth/input-allowlist.ts.
func NormalizeInputHostnameAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, v := range values {
		trimmed := strings.TrimSpace(v)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}
