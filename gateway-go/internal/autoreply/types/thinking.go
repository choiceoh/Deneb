// Package types defines shared data types for the autoreply subsystem.
// These are pure value types with no dependencies on autoreply internals.
package types

import (
	"regexp"
	"strings"
)

// ThinkLevel represents the thinking/reasoning depth for LLM inference.
type ThinkLevel string

const (
	ThinkOff      ThinkLevel = "off"
	ThinkMinimal  ThinkLevel = "minimal"
	ThinkLow      ThinkLevel = "low"
	ThinkMedium   ThinkLevel = "medium"
	ThinkHigh     ThinkLevel = "high"
	ThinkXHigh    ThinkLevel = "xhigh"
	ThinkAdaptive ThinkLevel = "adaptive"
)

// VerboseLevel controls how much detail is shown in replies.
type VerboseLevel string

const (
	VerboseOff  VerboseLevel = "off"
	VerboseOn   VerboseLevel = "on"
	VerboseFull VerboseLevel = "full"
)

// ElevatedLevel controls tool execution permissions.
type ElevatedLevel string

const (
	ElevatedOff  ElevatedLevel = "off"
	ElevatedOn   ElevatedLevel = "on"
	ElevatedAsk  ElevatedLevel = "ask"
	ElevatedFull ElevatedLevel = "full"
)

// ReasoningLevel controls whether reasoning/thinking is shown.
type ReasoningLevel string

const (
	ReasoningOff    ReasoningLevel = "off"
	ReasoningOn     ReasoningLevel = "on"
	ReasoningStream ReasoningLevel = "stream"
)

// UsageDisplayLevel controls token usage display.
type UsageDisplayLevel string

const (
	UsageOff    UsageDisplayLevel = "off"
	UsageTokens UsageDisplayLevel = "tokens"
	UsageFull   UsageDisplayLevel = "full"
)

// BudgetTokens returns the token budget for a thinking level.
// Returns 0 for ThinkOff or unrecognized levels (meaning thinking is disabled).
func (l ThinkLevel) BudgetTokens() int {
	switch l {
	case ThinkMinimal:
		return 1024
	case ThinkLow:
		return 4096
	case ThinkMedium:
		return 10240
	case ThinkHigh:
		return 32768
	case ThinkXHigh:
		return 65536
	case ThinkAdaptive:
		return 16384
	default:
		return 0
	}
}

// BaseThinkingLevels returns the standard thinking levels (without xhigh).
func BaseThinkingLevels() []ThinkLevel {
	return []ThinkLevel{ThinkOff, ThinkMinimal, ThinkLow, ThinkMedium, ThinkHigh, ThinkAdaptive}
}

var wsCollapseThinkRe = regexp.MustCompile(`[\s_-]+`)

// NormalizeThinkLevel normalizes a user-provided thinking level string.
func NormalizeThinkLevel(raw string) (ThinkLevel, bool) {
	if raw == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	collapsed := wsCollapseThinkRe.ReplaceAllString(key, "")

	switch collapsed {
	case "adaptive", "auto":
		return ThinkAdaptive, true
	case "xhigh", "extrahigh":
		return ThinkXHigh, true
	}

	switch key {
	case "off":
		return ThinkOff, true
	case "on", "enable", "enabled":
		return ThinkLow, true
	case "min", "minimal":
		return ThinkMinimal, true
	case "low", "thinkhard", "think-hard", "think_hard":
		return ThinkLow, true
	case "mid", "med", "medium", "thinkharder", "think-harder", "harder":
		return ThinkMedium, true
	case "high", "ultra", "ultrathink", "thinkhardest", "highest", "max":
		return ThinkHigh, true
	case "think":
		return ThinkMinimal, true
	}
	return "", false
}

// NormalizeVerboseLevel normalizes a verbose level string.
func NormalizeVerboseLevel(raw string) (VerboseLevel, bool) {
	if raw == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "off", "false", "no", "0":
		return VerboseOff, true
	case "full", "all", "everything":
		return VerboseFull, true
	case "on", "minimal", "true", "yes", "1":
		return VerboseOn, true
	}
	return "", false
}

// NormalizeElevatedLevel normalizes an elevated level string.
func NormalizeElevatedLevel(raw string) (ElevatedLevel, bool) {
	if raw == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "off", "false", "no", "0":
		return ElevatedOff, true
	case "full", "auto", "auto-approve", "autoapprove":
		return ElevatedFull, true
	case "ask", "prompt", "approval", "approve":
		return ElevatedAsk, true
	case "on", "true", "yes", "1":
		return ElevatedOn, true
	}
	return "", false
}

// NormalizeReasoningLevel normalizes a reasoning level string.
func NormalizeReasoningLevel(raw string) (ReasoningLevel, bool) {
	if raw == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "off", "false", "no", "0", "hide", "hidden", "disable", "disabled":
		return ReasoningOff, true
	case "on", "true", "yes", "1", "show", "visible", "enable", "enabled":
		return ReasoningOn, true
	case "stream", "streaming", "draft", "live":
		return ReasoningStream, true
	}
	return "", false
}

// NormalizeFastMode normalizes a fast mode string.
func NormalizeFastMode(raw string) (bool, bool) {
	if raw == "" {
		return false, false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "off", "false", "no", "0", "disable", "disabled", "normal":
		return false, true
	case "on", "true", "yes", "1", "enable", "enabled", "fast":
		return true, true
	}
	return false, false
}

// NormalizeUsageDisplay normalizes a usage display level string.
func NormalizeUsageDisplay(raw string) (UsageDisplayLevel, bool) {
	if raw == "" {
		return "", false
	}
	key := strings.ToLower(strings.TrimSpace(raw))
	switch key {
	case "off", "false", "no", "0", "disable", "disabled":
		return UsageOff, true
	case "on", "true", "yes", "1", "enable", "enabled":
		return UsageTokens, true
	case "tokens", "token", "tok", "minimal", "min":
		return UsageTokens, true
	case "full", "session":
		return UsageFull, true
	}
	return "", false
}

// NormalizeProviderId normalizes a provider string to a canonical form.
func NormalizeProviderId(provider string) string {
	if provider == "" {
		return ""
	}
	normalized := strings.ToLower(strings.TrimSpace(provider))
	switch normalized {
	case "z.ai", "z-ai":
		return "zai"
	case "bedrock", "aws-bedrock":
		return "amazon-bedrock"
	}
	return normalized
}

// IsBinaryThinkingProvider returns true if the provider only supports on/off thinking.
func IsBinaryThinkingProvider(provider string) bool {
	return NormalizeProviderId(provider) == "zai"
}

// ListThinkingLevelLabels returns the appropriate labels for a provider's thinking levels.
func ListThinkingLevelLabels(provider string) []string {
	if IsBinaryThinkingProvider(provider) {
		return []string{"off", "on"}
	}
	levels := BaseThinkingLevels()
	result := make([]string, len(levels))
	for i, l := range levels {
		result[i] = string(l)
	}
	return result
}

// FormatThinkingLevels returns a comma-separated string of thinking levels.
func FormatThinkingLevels(provider, separator string) string {
	if separator == "" {
		separator = ", "
	}
	return strings.Join(ListThinkingLevelLabels(provider), separator)
}
