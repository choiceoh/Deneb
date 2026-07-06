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
}

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
	return env
}
