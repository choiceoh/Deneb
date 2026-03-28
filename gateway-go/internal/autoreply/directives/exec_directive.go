// exec_directive.go — /exec directive parsing with key=value arguments.
// Mirrors src/auto-reply/reply/exec/directive.ts (210 LOC).
package directives

import (
	"regexp"
	"strings"
)

// ExecHost represents the execution host type.
type ExecHost string

const (
	ExecHostSandbox ExecHost = "sandbox"
	ExecHostGateway ExecHost = "gateway"
	ExecHostNode    ExecHost = "node"
)

// ExecSecurity represents the execution security level.
type ExecSecurity string

const (
	ExecSecurityDeny      ExecSecurity = "deny"
	ExecSecurityAllowlist ExecSecurity = "allowlist"
	ExecSecurityFull      ExecSecurity = "full"
)

// ExecAsk represents the ask/approval mode.
type ExecAsk string

const (
	ExecAskOff    ExecAsk = "off"
	ExecAskOnMiss ExecAsk = "on-miss"
	ExecAskAlways ExecAsk = "always"
)

// ExecDirectiveParse holds the result of parsing an /exec directive.
type ExecDirectiveParse struct {
	Cleaned      string
	HasDirective bool

	ExecHost     ExecHost
	ExecSecurity ExecSecurity
	ExecAsk      ExecAsk
	ExecNode     string

	RawExecHost     string
	RawExecSecurity string
	RawExecAsk      string
	RawExecNode     string

	HasExecOptions  bool
	InvalidHost     bool
	InvalidSecurity bool
	InvalidAsk      bool
	InvalidNode     bool
}

var execDirectiveRe = regexp.MustCompile(`(?i)(?:^|\s)/exec(?:$|\s|:)`)

// NormalizeExecHost validates and normalizes an exec host value.
func NormalizeExecHost(value string) (ExecHost, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "sandbox":
		return ExecHostSandbox, true
	case "gateway":
		return ExecHostGateway, true
	case "node":
		return ExecHostNode, true
	}
	return "", false
}

// NormalizeExecSecurity validates and normalizes an exec security value.
func NormalizeExecSecurity(value string) (ExecSecurity, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "deny":
		return ExecSecurityDeny, true
	case "allowlist":
		return ExecSecurityAllowlist, true
	case "full":
		return ExecSecurityFull, true
	}
	return "", false
}

// NormalizeExecAsk validates and normalizes an exec ask value.
func NormalizeExecAsk(value string) (ExecAsk, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "off":
		return ExecAskOff, true
	case "on-miss":
		return ExecAskOnMiss, true
	case "always":
		return ExecAskAlways, true
	}
	return "", false
}

// splitKeyValue splits a token on '=' or ':' into key and value.
func splitKeyValue(token string) (key, value string, ok bool) {
	eq := strings.IndexByte(token, '=')
	colon := strings.IndexByte(token, ':')

	idx := -1
	if eq == -1 {
		idx = colon
	} else if colon == -1 {
		idx = eq
	} else if eq < colon {
		idx = eq
	} else {
		idx = colon
	}

	if idx == -1 {
		return "", "", false
	}

	key = strings.ToLower(strings.TrimSpace(token[:idx]))
	value = strings.TrimSpace(token[idx+1:])
	if key == "" {
		return "", "", false
	}
	return key, value, true
}

// parseExecDirectiveArgs parses key=value arguments after /exec.
func parseExecDirectiveArgs(raw string) (result ExecDirectiveParse, consumed int) {
	runes := []rune(raw)
	n := len(runes)
	i := SkipDirectiveArgPrefix(raw)
	consumed = i

	for i < n {
		token, nextI := TakeDirectiveToken(raw, i)
		if token == "" {
			break
		}

		key, value, ok := splitKeyValue(token)
		if !ok {
			break
		}

		switch key {
		case "host":
			result.RawExecHost = value
			host, valid := NormalizeExecHost(value)
			if valid {
				result.ExecHost = host
			} else {
				result.InvalidHost = true
			}
			result.HasExecOptions = true
			i = nextI
			consumed = i

		case "security":
			result.RawExecSecurity = value
			sec, valid := NormalizeExecSecurity(value)
			if valid {
				result.ExecSecurity = sec
			} else {
				result.InvalidSecurity = true
			}
			result.HasExecOptions = true
			i = nextI
			consumed = i

		case "ask":
			result.RawExecAsk = value
			ask, valid := NormalizeExecAsk(value)
			if valid {
				result.ExecAsk = ask
			} else {
				result.InvalidAsk = true
			}
			result.HasExecOptions = true
			i = nextI
			consumed = i

		case "node":
			result.RawExecNode = value
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				result.InvalidNode = true
			} else {
				result.ExecNode = trimmed
			}
			result.HasExecOptions = true
			i = nextI
			consumed = i

		default:
			// Unknown key — stop consuming.
			goto done
		}
	}

done:
	return result, consumed
}

// ExtractExecDirective extracts an /exec directive from the message body.
func ExtractExecDirective(body string) ExecDirectiveParse {
	if body == "" {
		return ExecDirectiveParse{Cleaned: ""}
	}

	loc := execDirectiveRe.FindStringIndex(body)
	if loc == nil {
		return ExecDirectiveParse{Cleaned: strings.TrimSpace(body)}
	}

	// Find the exact position of "/exec" within the match.
	matchStr := body[loc[0]:loc[1]]
	execIdx := strings.Index(strings.ToLower(matchStr), "/exec")
	start := loc[0] + execIdx
	argsStart := start + len("/exec")

	parsed, consumed := parseExecDirectiveArgs(body[argsStart:])

	// Build cleaned body.
	cleanedRaw := body[:start] + " " + body[argsStart+consumed:]
	cleaned := strings.TrimSpace(multiSpaceRe.ReplaceAllString(cleanedRaw, " "))

	return ExecDirectiveParse{
		Cleaned:         cleaned,
		HasDirective:    true,
		ExecHost:        parsed.ExecHost,
		ExecSecurity:    parsed.ExecSecurity,
		ExecAsk:         parsed.ExecAsk,
		ExecNode:        parsed.ExecNode,
		RawExecHost:     parsed.RawExecHost,
		RawExecSecurity: parsed.RawExecSecurity,
		RawExecAsk:      parsed.RawExecAsk,
		RawExecNode:     parsed.RawExecNode,
		HasExecOptions:  parsed.HasExecOptions,
		InvalidHost:     parsed.InvalidHost,
		InvalidSecurity: parsed.InvalidSecurity,
		InvalidAsk:      parsed.InvalidAsk,
		InvalidNode:     parsed.InvalidNode,
	}
}
