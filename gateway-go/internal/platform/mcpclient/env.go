package mcpclient

import (
	"os"
	"strings"
)

// childEnvAllowlist names the variables a spawned MCP server inherits:
// process basics, node/npm caches under HOME, TLS/proxy egress, and locale.
// Everything else — provider keys, mail credentials — is withheld.
var childEnvAllowlist = []string{
	"PATH", "HOME", "USER", "LOGNAME", "SHELL", "TMPDIR", "TERM", "LANG",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy",
	"SSL_CERT_FILE", "SSL_CERT_DIR", "NODE_EXTRA_CA_CERTS",
	"XDG_CACHE_HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR",
	"DO_NOT_TRACK",
}

// childEnvDefaults are exported to every spawned MCP server unless the parent
// already sets them. DO_NOT_TRACK=1 (consoledonottrack.com) is the cross-tool
// telemetry opt-out most CLI/npm packages honour. 2026-08-17/18: an npx MCP
// server spent a DNS outage retrying PostHog and flooded the journal with 650+
// "mcp server stderr" lines — third-party telemetry this gateway never asked
// for. Opt out at the process boundary; the stderr collapse in client.go
// covers servers that ignore the convention.
var childEnvDefaults = [][2]string{{"DO_NOT_TRACK", "1"}}

// childEnv builds the allowlisted environment for the child process
// (LC_* locale variables pass through as a prefix family).
func childEnv() []string {
	var env []string
	for _, key := range childEnvAllowlist {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "LC_") {
			env = append(env, kv)
		}
	}
	for _, kv := range childEnvDefaults {
		if _, ok := os.LookupEnv(kv[0]); !ok {
			env = append(env, kv[0]+"="+kv[1])
		}
	}
	return env
}
