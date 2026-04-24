// Copyright (c) Deneb authors. Licensed under the project license.

package redact

import (
	"regexp"
	"strings"
)

// Sensitive query-string parameter names (case-insensitive exact match).
// These catch opaque tokens whose values do not match any vendor prefix
// regex (short OAuth codes, pre-signed URL signatures, etc.).
var sensitiveQueryParams = map[string]struct{}{
	"access_token":    {},
	"refresh_token":   {},
	"id_token":        {},
	"token":           {},
	"api_key":         {},
	"apikey":          {},
	"client_secret":   {},
	"password":        {},
	"auth":            {},
	"jwt":             {},
	"session":         {},
	"secret":          {},
	"key":             {},
	"code":            {}, // OAuth authorization codes
	"signature":       {}, // pre-signed URL signatures
	"x-amz-signature": {},
}

// Known vendor prefix patterns (ordered roughly by popularity).
// The prefix literal makes these cheap to reject against the common case of
// ordinary text. Each pattern matches the prefix plus the contiguous token
// characters; lookbehind is applied via the combined prefixRE below.
var prefixPatterns = []string{
	`sk-[A-Za-z0-9_-]{10,}`,        // OpenAI / OpenRouter / Anthropic (sk-ant-*)
	`ghp_[A-Za-z0-9]{10,}`,         // GitHub PAT (classic)
	`github_pat_[A-Za-z0-9_]{10,}`, // GitHub PAT (fine-grained)
	`gho_[A-Za-z0-9]{10,}`,         // GitHub OAuth access token
	`ghu_[A-Za-z0-9]{10,}`,         // GitHub user-to-server token
	`ghs_[A-Za-z0-9]{10,}`,         // GitHub server-to-server token
	`ghr_[A-Za-z0-9]{10,}`,         // GitHub refresh token
	`xox[baprs]-[A-Za-z0-9-]{10,}`, // Slack tokens
	`AIza[A-Za-z0-9_-]{30,}`,       // Google API keys
	`pplx-[A-Za-z0-9]{10,}`,        // Perplexity
	`xai-[A-Za-z0-9]{10,}`,         // xAI (Grok)
	`nvapi-[A-Za-z0-9_-]{10,}`,     // NVIDIA API keys
	`fal_[A-Za-z0-9_-]{10,}`,       // Fal.ai
	`fc-[A-Za-z0-9]{10,}`,          // Firecrawl
	`bb_live_[A-Za-z0-9_-]{10,}`,   // BrowserBase
	`gAAAA[A-Za-z0-9_=-]{20,}`,     // Codex encrypted tokens
	`AKIA[A-Z0-9]{16}`,             // AWS Access Key ID
	`sk_live_[A-Za-z0-9]{10,}`,     // Stripe secret key (live)
	`sk_test_[A-Za-z0-9]{10,}`,     // Stripe secret key (test)
	`rk_live_[A-Za-z0-9]{10,}`,     // Stripe restricted key
	`SG\.[A-Za-z0-9_-]{10,}`,       // SendGrid API key
	`hf_[A-Za-z0-9]{10,}`,          // HuggingFace token
	`r8_[A-Za-z0-9]{10,}`,          // Replicate API token
	`npm_[A-Za-z0-9]{10,}`,         // npm access token
	`pypi-[A-Za-z0-9_-]{10,}`,      // PyPI API token
	`dop_v1_[A-Za-z0-9]{10,}`,      // DigitalOcean PAT
	`doo_v1_[A-Za-z0-9]{10,}`,      // DigitalOcean OAuth
	`am_[A-Za-z0-9_-]{10,}`,        // AgentMail API key
	`sk_[A-Za-z0-9_]{10,}`,         // ElevenLabs TTS key (sk_ underscore, not sk- dash)
	`tvly-[A-Za-z0-9]{10,}`,        // Tavily search API key
	`exa_[A-Za-z0-9]{10,}`,         // Exa search API key
	`gsk_[A-Za-z0-9]{10,}`,         // Groq Cloud API key
	`syt_[A-Za-z0-9]{10,}`,         // Matrix access token
	`retaindb_[A-Za-z0-9]{10,}`,    // RetainDB API key
	`hsk-[A-Za-z0-9]{10,}`,         // Hindsight API key
	`mem0_[A-Za-z0-9]{10,}`,        // Mem0 Platform API key
	`brv_[A-Za-z0-9]{10,}`,         // ByteRover API key
}

// Secret-like environment variable names. Used inside ENV-assignment matches.
// This is the names of suspicious envvars, not a credential itself; gosec G101
// flags the literal because the word "PASSWORD" is present.
//
//nolint:gosec // G101 — regex fragment matching sensitive envvar names, not a credential
const secretEnvNames = `(?:API_?KEY|TOKEN|SECRET|PASSWORD|PASSWD|CREDENTIAL|AUTH)`

// JSON / structured-body key names that carry secret values. Match is
// case-insensitive.
const jsonKeyNames = `(?:api_?[Kk]ey|token|secret|password|access_token|refresh_token|auth_token|bearer|secret_value|raw_secret|secret_input|key_material|client_secret|apiKey)`

// Compiled regex tables — each is a single MustCompile at package init so
// matching is a constant cost per call.
var (
	// Alternation of all known vendor prefixes, guarded with non-consumed
	// lookaround-style word boundaries built via the wrapping captures in
	// applyPrefixRedaction. The RE2 engine does not support lookbehind, so
	// boundary checks are applied in Go code after the match.
	prefixRE = regexp.MustCompile(`(` + strings.Join(prefixPatterns, "|") + `)`)

	// ENV assignments: KEY=value where KEY matches secretEnvNames.
	envAssignRE = regexp.MustCompile(
		`([A-Z0-9_]{0,50}` + secretEnvNames + `[A-Z0-9_]{0,50})\s*=\s*(['"]?)(\S+?)(['"]?)(\s|$)`,
	)

	// JSON fields: "apiKey": "value" or "token":"value" (case-insensitive).
	jsonFieldRE = regexp.MustCompile(
		`(?i)("` + jsonKeyNames + `")\s*:\s*"([^"]+)"`,
	)

	// Authorization / X-API-Key headers.
	authHeaderRE = regexp.MustCompile(
		`(?i)(Authorization:\s*Bearer\s+)(\S+)`,
	)
	apiKeyHeaderRE = regexp.MustCompile(
		`(?i)(X-API-Key:\s*)(\S+)`,
	)

	// Telegram bot tokens: bot<digits>:<token> or <digits>:<token>.
	telegramRE = regexp.MustCompile(
		`(bot)?(\d{8,}):([-A-Za-z0-9_]{30,})`,
	)

	// PEM private-key blocks.
	privateKeyRE = regexp.MustCompile(
		`-----BEGIN[A-Z ]*PRIVATE KEY-----[\s\S]*?-----END[A-Z ]*PRIVATE KEY-----`,
	)

	// Database connection strings: protocol://user:PASSWORD@host.
	dbConnStrRE = regexp.MustCompile(
		`(?i)((?:postgres(?:ql)?|mysql|mongodb(?:\+srv)?|redis|amqp)://[^:/\s]+:)([^@\s]+)(@)`,
	)

	// JWTs always start with eyJ (base64 of `{`). We accept 1-, 2-, and
	// 3-part variants to cover malformed examples as well as full JWTs.
	jwtRE = regexp.MustCompile(
		`eyJ[A-Za-z0-9_-]{10,}(?:\.[A-Za-z0-9_=-]{4,}){0,2}`,
	)

	// Discord mentions: <@snowflake> or <@!snowflake>.
	discordMentionRE = regexp.MustCompile(`<@!?(\d{17,20})>`)

	// E.164 phone numbers: 7-15 digits.
	phoneRE = regexp.MustCompile(`(\+[1-9]\d{6,14})`)

	// URLs containing a query string — scheme://authority[path]?query[#frag].
	urlWithQueryRE = regexp.MustCompile(
		`(https?|wss?|ftp)://([^\s/?#]+)([^\s?#]*)\?([^\s#]+)(#\S*)?`,
	)

	// URLs containing userinfo — scheme://user:password@host (non-DB schemes).
	urlUserinfoRE = regexp.MustCompile(
		`(https?|wss?|ftp)://([^/\s:@]+):([^/\s@]+)@`,
	)

	// Conservative form-urlencoded body check: entire text must be k=v&k=v.
	formBodyRE = regexp.MustCompile(
		`^[A-Za-z_][A-Za-z0-9_.-]*=[^&\s]*(?:&[A-Za-z_][A-Za-z0-9_.-]*=[^&\s]*)+$`,
	)
)

// isTokenBoundary reports whether the byte at position i in s is a valid
// boundary for a vendor-prefix token. We treat identifier characters
// ([A-Za-z0-9_-]) as part of an extended token, so a prefix embedded in a
// larger identifier (e.g. "notsk-foo") is not redacted.
//
// i == -1 or i == len(s) denote start/end of string (both are boundaries).
func isTokenBoundary(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return true
	}
	c := s[i]
	switch {
	case c >= 'A' && c <= 'Z':
	case c >= 'a' && c <= 'z':
	case c >= '0' && c <= '9':
	case c == '_' || c == '-':
	default:
		return true
	}
	return false
}
