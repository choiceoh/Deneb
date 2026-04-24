// Copyright (c) Deneb authors. Licensed under the project license.

// Package redact strips well-known secret patterns from strings before they
// leave Deneb (logs, Telegram messages, session transcripts, crash dumps).
//
// Policy:
//   - "Fail open" is forbidden — a nil-safe, best-effort pass is always
//     cheaper than leaking a key into chat history.
//   - Enable/disable is captured at package init from DENEB_REDACT_SECRETS
//     (default true). Runtime mutation of the env var cannot re-enable or
//     disable redaction mid-process; this guards against LLM-crafted
//     `export DENEB_REDACT_SECRETS=false` attacks.
//
// Usage:
//
//	clean := redact.String(dirty)
//	opts.ReplaceAttr = redact.AttrReplacer(opts.ReplaceAttr)  // slog handler
package redact

import (
	"log/slog"
	"os"
	"strings"
)

// enabled captures the DENEB_REDACT_SECRETS environment variable at package
// init. Subsequent os.Setenv calls cannot flip the flag — this is a deliberate
// defense against LLM-crafted `export DENEB_REDACT_SECRETS=false` inside
// tool output.
var enabled = parseEnabled(os.Getenv("DENEB_REDACT_SECRETS"))

// parseEnabled mirrors the Hermes truthiness rules: any value in
// {"0","false","no","off"} (case-insensitive) disables. Default is on.
func parseEnabled(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

// Enabled reports whether redaction is active. Captured at package init from
// the DENEB_REDACT_SECRETS environment variable.
func Enabled() bool { return enabled }

// String returns a copy of in with every known secret pattern masked.
// Never returns a longer string than the total masked replacement, and is
// nil-safe: empty input returns empty output.
//
// When redaction is disabled (Enabled() == false) the input is returned
// unchanged.
func String(in string) string {
	if in == "" || !enabled {
		return in
	}

	// Cheap early exit: if the input contains none of the sentinel substrings
	// that every pattern requires, we can skip regex work entirely. This keeps
	// log-heavy paths fast on the common case of no secrets.
	if !mightContainSecret(in) {
		return in
	}

	out := in

	// Ordered pipeline. Most specific patterns run first so that e.g.
	// a GitHub PAT embedded in a JSON field is masked via the prefix rule
	// (preserving prefix6) rather than the generic JSON rule.
	out = applyPrefixRedaction(out)
	out = applyEnvAssignment(out)
	out = applyJSONFields(out)
	out = applyAuthHeader(out)
	out = applyAPIKeyHeader(out)
	out = applyTelegramToken(out)
	out = applyPrivateKeyBlock(out)
	out = applyDBConnString(out)
	out = applyJWT(out)
	out = applyURLUserinfo(out)
	out = applyURLQueryParams(out)
	out = applyFormBody(out)
	out = applyDiscordMention(out)
	out = applyPhone(out)

	return out
}

// Bytes is the []byte variant of String, useful for network body redaction
// where the caller already has bytes in hand. It avoids an extra allocation
// when nothing matches.
func Bytes(in []byte) []byte {
	if len(in) == 0 || !enabled {
		return in
	}
	s := string(in)
	out := String(s)
	if out == s {
		return in
	}
	return []byte(out)
}

// AttrReplacer returns a slog.HandlerOptions-compatible ReplaceAttr that
// redacts string attribute values. If prev is non-nil it is applied first,
// then the redaction runs on the returned Attr.
//
// Non-string attribute kinds (int, bool, time, duration, ...) are left alone
// — secrets masquerading as integers are exceedingly rare and the cost of
// stringifying every attribute is not justified.
func AttrReplacer(prev func([]string, slog.Attr) slog.Attr) func([]string, slog.Attr) slog.Attr {
	return func(groups []string, a slog.Attr) slog.Attr {
		if prev != nil {
			a = prev(groups, a)
		}
		if !enabled {
			return a
		}
		switch a.Value.Kind() {
		case slog.KindString:
			s := a.Value.String()
			r := String(s)
			if r != s {
				a.Value = slog.StringValue(r)
			}
		case slog.KindAny:
			// If the wrapped value is an error, redact its rendered form.
			// slog renders errors via %s, so redacting the Error() string is
			// equivalent to what the handler would emit. Other Any kinds
			// (structs, maps) are left to the handler's default rendering —
			// wrapping those would require round-tripping through JSON.
			if err, ok := a.Value.Any().(error); ok && err != nil {
				s := err.Error()
				r := String(s)
				if r != s {
					a.Value = slog.StringValue(r)
				}
			}
		case slog.KindBool,
			slog.KindDuration,
			slog.KindFloat64,
			slog.KindInt64,
			slog.KindTime,
			slog.KindUint64,
			slog.KindGroup,
			slog.KindLogValuer:
			// Non-string primitives cannot embed our target patterns.
			// (LogValuer resolves via the handler's own Resolve() chain, so
			// any string it eventually yields is handled there — not here.)
		}
		return a
	}
}

// mightContainSecret is a fast rejection check. Every pattern in this
// package requires at least one of these substrings to be present, so if
// none appear we can skip the regex pipeline entirely.
//
// The list is intentionally conservative: false positives (running the
// regex pipeline unnecessarily) are cheap; false negatives would be a
// security bug. Any new pattern added to patterns.go must either embed one
// of these substrings or extend this filter.
func mightContainSecret(s string) bool {
	return containsAny(s,
		// Vendor prefixes (contiguous, high-signal)
		"sk-", "sk_", "ghp_", "gho_", "ghu_", "ghs_", "ghr_", "github_pat_",
		"xox", "AIza", "pplx-", "xai-", "nvapi-", "fal_", "fc-", "bb_live_",
		"gAAAA", "AKIA", "rk_live_", "SG.", "hf_", "r8_", "npm_", "pypi-",
		"dop_v1_", "doo_v1_", "am_", "tvly-", "exa_", "gsk_", "syt_",
		"retaindb_", "hsk-", "mem0_", "brv_",
		// JWTs and PEM blocks
		"eyJ", "-----BEGIN",
		// DB and URL schemes
		"postgres://", "postgresql://", "mysql://", "mongodb://",
		"mongodb+srv://", "redis://", "amqp://",
		// Telegram bot prefix
		"bot",
		// Phone / Discord
		"+", "<@",
		// Header names
		"uthorization", "-API-Key", "-api-key", "x-api-key", "X-Api-Key",
		// Structured field keywords (case variants)
		"api_key", "apiKey", "api-key", "API_KEY",
		"token", "TOKEN", "Token",
		"secret", "SECRET", "Secret",
		"password", "PASSWORD", "Password", "PASSWD",
		"credential", "CREDENTIAL", "Credential",
		"access_token", "refresh_token", "id_token",
		"client_secret",
		"AUTH",
		// Form/query delimiters
		"&", "?",
	)
}

// containsAny returns true if s contains any of the provided substrings.
// Hand-rolled to avoid building a variadic slice allocation on every call
// and to allow the compiler to hoist the slice into a read-only literal.
func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// ---- pattern application helpers ---------------------------------------
//
// Each helper takes a string, finds every match of one pattern from
// patterns.go, and returns the redacted string. They are ordered from most
// to least specific in String() above.

// applyPrefixRedaction masks every vendor-prefixed token in s. A word-
// boundary check is applied after the regex match so that `notsk-foo` is
// not redacted but `key=sk-abc...` is. RE2 lacks lookbehind, so we do the
// boundary check in Go.
func applyPrefixRedaction(s string) string {
	locs := prefixRE.FindAllStringIndex(s, -1)
	if len(locs) == 0 {
		return s
	}
	var out strings.Builder
	out.Grow(len(s))
	last := 0
	for _, loc := range locs {
		start, end := loc[0], loc[1]
		if !isTokenBoundary(s, start-1) || !isTokenBoundary(s, end) {
			continue
		}
		out.WriteString(s[last:start])
		out.WriteString(maskToken(s[start:end]))
		last = end
	}
	if last == 0 {
		// Every candidate was rejected by the boundary check.
		return s
	}
	out.WriteString(s[last:])
	return out.String()
}

// applyEnvAssignment handles ENV-style assignments: `OPENAI_API_KEY=sk-abc`.
func applyEnvAssignment(s string) string {
	return envAssignRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := envAssignRE.FindStringSubmatch(match)
		if len(sub) < 6 {
			return match
		}
		name, qOpen, value, qClose, trail := sub[1], sub[2], sub[3], sub[4], sub[5]
		return name + "=" + qOpen + maskToken(value) + qClose + trail
	})
}

// applyJSONFields handles `"apiKey": "value"` style entries.
func applyJSONFields(s string) string {
	return jsonFieldRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := jsonFieldRE.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		return sub[1] + `: "` + maskToken(sub[2]) + `"`
	})
}

// applyAuthHeader masks the token portion of `Authorization: Bearer <token>`.
func applyAuthHeader(s string) string {
	return authHeaderRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := authHeaderRE.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		return sub[1] + maskToken(sub[2])
	})
}

// applyAPIKeyHeader masks an `X-API-Key: <value>` header.
func applyAPIKeyHeader(s string) string {
	return apiKeyHeaderRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := apiKeyHeaderRE.FindStringSubmatch(match)
		if len(sub) < 3 {
			return match
		}
		return sub[1] + maskToken(sub[2])
	})
}

// applyTelegramToken masks Telegram bot tokens `bot<digits>:<token>`.
func applyTelegramToken(s string) string {
	return telegramRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := telegramRE.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		return sub[1] + sub[2] + ":***"
	})
}

// applyPrivateKeyBlock replaces every PEM private-key block with a placeholder.
func applyPrivateKeyBlock(s string) string {
	return privateKeyRE.ReplaceAllString(s, "[REDACTED PRIVATE KEY]")
}

// applyDBConnString masks the password in `scheme://user:pass@host`.
func applyDBConnString(s string) string {
	return dbConnStrRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := dbConnStrRE.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		return sub[1] + "***" + sub[3]
	})
}

// applyJWT masks JWT tokens.
func applyJWT(s string) string {
	return jwtRE.ReplaceAllStringFunc(s, maskToken)
}

// applyURLUserinfo masks `http(s)://user:pass@host` for non-DB schemes.
func applyURLUserinfo(s string) string {
	return urlUserinfoRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := urlUserinfoRE.FindStringSubmatch(match)
		if len(sub) < 4 {
			return match
		}
		return sub[1] + "://" + sub[2] + ":***@"
	})
}

// applyURLQueryParams scans text for URLs with query strings and redacts
// sensitive parameter values.
func applyURLQueryParams(s string) string {
	return urlWithQueryRE.ReplaceAllStringFunc(s, func(match string) string {
		sub := urlWithQueryRE.FindStringSubmatch(match)
		if len(sub) < 5 {
			return match
		}
		scheme, authority, path, query := sub[1], sub[2], sub[3], sub[4]
		fragment := ""
		if len(sub) >= 6 {
			fragment = sub[5]
		}
		return scheme + "://" + authority + path + "?" + redactQueryString(query) + fragment
	})
}

// applyFormBody redacts sensitive values in a form-urlencoded body only when
// the entire input looks like a pure form body (k=v&k=v).
func applyFormBody(s string) string {
	if s == "" || strings.Contains(s, "\n") || !strings.Contains(s, "&") {
		return s
	}
	trimmed := strings.TrimSpace(s)
	if !formBodyRE.MatchString(trimmed) {
		return s
	}
	redacted := redactQueryString(trimmed)
	if trimmed == s {
		return redacted
	}
	return strings.Replace(s, trimmed, redacted, 1)
}

// applyDiscordMention scrubs Discord user/role mentions.
func applyDiscordMention(s string) string {
	return discordMentionRE.ReplaceAllStringFunc(s, func(match string) string {
		if strings.HasPrefix(match, "<@!") {
			return "<@!***>"
		}
		return "<@***>"
	})
}

// applyPhone masks E.164 phone numbers.
func applyPhone(s string) string {
	return phoneRE.ReplaceAllStringFunc(s, func(match string) string {
		if len(match) <= 8 {
			return match[:2] + "****" + match[len(match)-2:]
		}
		return match[:4] + "****" + match[len(match)-4:]
	})
}

// redactQueryString walks a `k=v&k=v` string and masks values whose keys
// match sensitiveQueryParams. Malformed pairs (no `=`) pass through
// unchanged to preserve the original shape.
func redactQueryString(query string) string {
	if query == "" {
		return query
	}
	var out strings.Builder
	out.Grow(len(query))
	first := true
	for _, pair := range strings.Split(query, "&") {
		if !first {
			out.WriteByte('&')
		}
		first = false
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			out.WriteString(pair)
			continue
		}
		key := pair[:eq]
		if _, ok := sensitiveQueryParams[strings.ToLower(key)]; ok {
			out.WriteString(key)
			out.WriteString("=***")
		} else {
			out.WriteString(pair)
		}
	}
	return out.String()
}
