package process

import "strings"

// blockedEnvKeys lists environment variable names that must be stripped from
// subprocess environments. These variables can be exploited to inject code
// into child processes via build-tool hooks, dynamic linker manipulation,
// or language runtime startup hooks.
//
// Categories:
//   - Dynamic linker: LD_PRELOAD, LD_LIBRARY_PATH, DYLD_*
//   - Shell injection: BASH_ENV, ENV, ZDOTDIR
//   - JVM injection: MAVEN_OPTS, SBT_OPTS, GRADLE_OPTS, _JAVA_OPTIONS, JAVA_TOOL_OPTIONS
//   - Language runtimes: PYTHONSTARTUP, PERL5OPT, RUBYOPT
//   - .NET: DOTNET_STARTUP_HOOKS, DOTNET_ROOT
//   - glibc tunables: GLIBC_TUNABLES
var blockedEnvKeys = map[string]bool{
	"LD_PRELOAD":           true,
	"LD_LIBRARY_PATH":      true,
	"BASH_ENV":             true,
	"ENV":                  true,
	"ZDOTDIR":              true,
	"MAVEN_OPTS":           true,
	"SBT_OPTS":             true,
	"GRADLE_OPTS":          true,
	"_JAVA_OPTIONS":        true,
	"JAVA_TOOL_OPTIONS":    true,
	"PYTHONSTARTUP":        true,
	"PERL5OPT":             true,
	"RUBYOPT":              true,
	"DOTNET_STARTUP_HOOKS": true,
	"DOTNET_ROOT":          true,
	"GLIBC_TUNABLES":       true,
}

// blockedEnvPrefixes lists environment variable prefixes that should be blocked.
var blockedEnvPrefixes = []string{
	"DYLD_",    // macOS dynamic linker
	"LD_AUDIT", // glibc audit module
}

// isBlockedEnvKey returns true if the given environment variable key
// should be stripped from subprocess environments.
func isBlockedEnvKey(key string) bool {
	if blockedEnvKeys[key] {
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

// SanitizeEnv filters dangerous environment variables from a slice of
// "KEY=VALUE" strings. Blocked variables are silently removed.
// NODE_OPTIONS is sanitized rather than fully blocked.
func SanitizeEnv(env []string, logger interface{ Info(string, ...any) }) []string {
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
