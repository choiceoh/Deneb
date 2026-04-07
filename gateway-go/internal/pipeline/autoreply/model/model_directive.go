package model

import (
	"regexp"
	"strings"
)

// ModelDirective holds the result of extracting a /model directive from a message.
type ModelDirective struct {
	Cleaned      string // message body with the directive removed
	RawModel     string // raw model reference (e.g., "anthropic/claude-3")
	RawProfile   string // auth profile suffix (e.g., ":work")
	HasDirective bool
}

var modelDirectiveRe = regexp.MustCompile(`(?i)(?:^|\s)/model(?:$|\s|:)\s*:?\s*([A-Za-z0-9_.:\@-]+(?:/[A-Za-z0-9_.:\@-]+)*)?`)
var multiSpaceRe = regexp.MustCompile(`\s+`)

// ExtractModelDirective parses a /model directive from the message body and
// returns the cleaned text with the directive removed.
func ExtractModelDirective(body string, aliases []string) ModelDirective {
	if body == "" {
		return ModelDirective{Cleaned: ""}
	}

	// Try /model first.
	match := modelDirectiveRe.FindStringSubmatchIndex(body)

	// Try aliases if /model didn't match.
	var aliasMatch []int
	if match == nil && len(aliases) > 0 {
		trimmedAliases := make([]string, 0, len(aliases))
		for _, a := range aliases {
			t := strings.TrimSpace(a)
			if t != "" {
				trimmedAliases = append(trimmedAliases, regexp.QuoteMeta(t))
			}
		}
		if len(trimmedAliases) > 0 {
			aliasRe := regexp.MustCompile(`(?i)(?:^|\s)/(` + strings.Join(trimmedAliases, "|") + `)(?:$|\s|:)(?:\s*:\s*)?`)
			aliasMatch = aliasRe.FindStringSubmatchIndex(body)
		}
	}

	if match == nil && aliasMatch == nil {
		return ModelDirective{Cleaned: strings.TrimSpace(body)}
	}

	useMatch := match
	if useMatch == nil {
		useMatch = aliasMatch
	}

	var raw string
	if len(match) > 3 && match[2] != -1 {
		raw = strings.TrimSpace(body[match[2]:match[3]])
	} else if len(aliasMatch) > 3 && aliasMatch[2] != -1 {
		raw = strings.TrimSpace(body[aliasMatch[2]:aliasMatch[3]])
	}

	rawModel := raw
	rawProfile := ""
	if raw != "" {
		if idx := strings.LastIndex(raw, ":"); idx > 0 && idx < len(raw)-1 {
			rawModel = raw[:idx]
			rawProfile = raw[idx+1:]
		}
	}

	cleaned := body[:useMatch[0]] + " " + body[useMatch[1]:]
	cleaned = strings.TrimSpace(multiSpaceRe.ReplaceAllString(cleaned, " "))

	return ModelDirective{
		Cleaned:      cleaned,
		RawModel:     rawModel,
		RawProfile:   rawProfile,
		HasDirective: true,
	}
}
