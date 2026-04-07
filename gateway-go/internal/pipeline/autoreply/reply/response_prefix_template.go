// response_prefix_template.go — Template interpolation for response prefix.
// Mirrors src/auto-reply/reply/response-prefix-template.ts (101 LOC).
// Supports {variable} placeholders with case-insensitive matching.
package reply

import (
	"regexp"
	"strings"
)

// ResponsePrefixContext holds values for template variable interpolation.
type ResponsePrefixContext struct {
	// Short model name (e.g., "gpt-5.2", "claude-opus-4-6").
	Model string
	// Full model ID including provider (e.g., "openai-codex/gpt-5.2").
	ModelFull string
	// Provider name (e.g., "openai-codex", "anthropic").
	Provider string
	// Current thinking level (e.g., "high", "low", "off").
	ThinkingLevel string
	// Agent identity name.
	IdentityName string
}

var templateVarPattern = regexp.MustCompile(`\{([a-zA-Z][a-zA-Z0-9.]*)\}`)

// ResolveResponsePrefixTemplate interpolates template variables in a response
// prefix string. Unresolved variables remain as literal text.
func ResolveResponsePrefixTemplate(template string, ctx ResponsePrefixContext) string {
	if template == "" {
		return ""
	}

	return templateVarPattern.ReplaceAllStringFunc(template, func(match string) string {
		// Extract variable name from {varName}.
		varName := match[1 : len(match)-1]
		normalizedVar := strings.ToLower(varName)

		switch normalizedVar {
		case "model":
			if ctx.Model != "" {
				return ctx.Model
			}
			return match
		case "modelfull":
			if ctx.ModelFull != "" {
				return ctx.ModelFull
			}
			return match
		case "provider":
			if ctx.Provider != "" {
				return ctx.Provider
			}
			return match
		case "thinkinglevel", "think":
			if ctx.ThinkingLevel != "" {
				return ctx.ThinkingLevel
			}
			return match
		case "identity.name", "identityname":
			if ctx.IdentityName != "" {
				return ctx.IdentityName
			}
			return match
		default:
			return match
		}
	})
}

var (
	dateSuffixRe   = regexp.MustCompile(`-\d{8}$`)
	latestSuffixRe = regexp.MustCompile(`-latest$`)
)

// ExtractShortModelName strips provider prefix and date/version suffixes from
// a full model string.
//
// Examples:
//
//	"openai-codex/gpt-5.2" → "gpt-5.2"
//	"claude-opus-4-6-20260205" → "claude-opus-4-6"
//	"gpt-5.2-latest" → "gpt-5.2"
func ExtractShortModelName(fullModel string) string {
	// Strip provider prefix.
	modelPart := fullModel
	if slash := strings.LastIndex(fullModel, "/"); slash >= 0 {
		modelPart = fullModel[slash+1:]
	}
	// Strip date suffix.
	modelPart = dateSuffixRe.ReplaceAllString(modelPart, "")
	// Strip -latest suffix.
	modelPart = latestSuffixRe.ReplaceAllString(modelPart, "")
	return modelPart
}

// HasTemplateVariables returns true if the template string contains any
// {variable} placeholders.
func HasTemplateVariables(template string) bool {
	if template == "" {
		return false
	}
	return templateVarPattern.MatchString(template)
}
