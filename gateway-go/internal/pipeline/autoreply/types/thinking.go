// Package types defines shared data types for the autoreply subsystem.
// These are pure value types with no dependencies on autoreply internals.
package types

import (
	"strings"
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
func NormalizeFastMode(raw string) (value, ok bool) {
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
