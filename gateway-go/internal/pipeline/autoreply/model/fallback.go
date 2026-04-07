package model

import (
	"fmt"
	"strings"
)

const fallbackReasonPartMax = 80

// FallbackAttempt records a single model fallback attempt.
type FallbackAttempt struct {
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Code     string `json:"code,omitempty"`
	Status   int    `json:"status,omitempty"`
	Error    string `json:"error,omitempty"`
}

// FallbackNoticeState holds session-level fallback tracking fields.
type FallbackNoticeState struct {
	SelectedModel string `json:"fallbackNoticeSelectedModel,omitempty"`
	ActiveModel   string `json:"fallbackNoticeActiveModel,omitempty"`
	Reason        string `json:"fallbackNoticeReason,omitempty"`
}

// FallbackTransition describes the full state transition for a fallback event.
type FallbackTransition struct {
	SelectedModelRef     string
	ActiveModelRef       string
	FallbackActive       bool
	FallbackTransitioned bool
	FallbackCleared      bool
	ReasonSummary        string
	AttemptSummaries     []string
	PreviousState        FallbackNoticeState
	NextState            FallbackNoticeState
	StateChanged         bool
}

// FormatProviderModelRef formats a provider/model reference string.
func FormatProviderModelRef(provider, model string) string {
	if provider == "" {
		return model
	}
	return provider + "/" + model
}

func normalizeFallbackModelRef(value string) string {
	return strings.TrimSpace(value)
}

func truncateFallbackReasonPart(value string, maxLen int) string {
	if maxLen <= 0 {
		maxLen = fallbackReasonPartMax
	}
	text := strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if len(text) <= maxLen {
		return text
	}
	if maxLen <= 1 {
		return "…"
	}
	return strings.TrimRight(text[:maxLen-1], " ") + "…"
}

// FormatFallbackAttemptReason returns a human-readable reason for a fallback attempt.
func FormatFallbackAttemptReason(attempt FallbackAttempt) string {
	if reason := strings.TrimSpace(attempt.Reason); reason != "" {
		return strings.ReplaceAll(reason, "_", " ")
	}
	if code := strings.TrimSpace(attempt.Code); code != "" {
		return code
	}
	if attempt.Status > 0 {
		return fmt.Sprintf("HTTP %d", attempt.Status)
	}
	if attempt.Error != "" {
		return truncateFallbackReasonPart(attempt.Error, fallbackReasonPartMax)
	}
	return "error"
}

// BuildFallbackReasonSummary creates a summary string from fallback attempts.
func BuildFallbackReasonSummary(attempts []FallbackAttempt) string {
	if len(attempts) == 0 {
		return "selected model unavailable"
	}
	firstReason := FormatFallbackAttemptReason(attempts[0])
	more := ""
	if len(attempts) > 1 {
		more = fmt.Sprintf(" (+%d more attempts)", len(attempts)-1)
	}
	return truncateFallbackReasonPart(firstReason, fallbackReasonPartMax) + more
}

// BuildFallbackAttemptSummaries returns per-attempt summary strings.
func BuildFallbackAttemptSummaries(attempts []FallbackAttempt) []string {
	summaries := make([]string, len(attempts))
	for i, a := range attempts {
		ref := FormatProviderModelRef(a.Provider, a.Model)
		reason := FormatFallbackAttemptReason(a)
		summaries[i] = truncateFallbackReasonPart(ref+" "+reason, fallbackReasonPartMax)
	}
	return summaries
}

// BuildFallbackNotice creates a user-visible fallback notice string.
// Returns "" if selected and active models are the same (no fallback).
func BuildFallbackNotice(selectedProvider, selectedModel, activeProvider, activeModel string, attempts []FallbackAttempt) string {
	selected := FormatProviderModelRef(selectedProvider, selectedModel)
	active := FormatProviderModelRef(activeProvider, activeModel)
	if selected == active {
		return ""
	}
	reasonSummary := BuildFallbackReasonSummary(attempts)
	return fmt.Sprintf("↪️ Model Fallback: %s (selected %s; %s)", active, selected, reasonSummary)
}

// BuildFallbackClearedNotice creates a notice when fallback is cleared.
func BuildFallbackClearedNotice(selectedProvider, selectedModel, previousActiveModel string) string {
	selected := FormatProviderModelRef(selectedProvider, selectedModel)
	previous := normalizeFallbackModelRef(previousActiveModel)
	if previous != "" && previous != selected {
		return fmt.Sprintf("↪️ Model Fallback cleared: %s (was %s)", selected, previous)
	}
	return fmt.Sprintf("↪️ Model Fallback cleared: %s", selected)
}

// ResolveActiveFallbackState checks if fallback is currently active.
func ResolveActiveFallbackState(selectedModelRef, activeModelRef string, state *FallbackNoticeState) (active bool, reason string) {
	if state == nil {
		return false, ""
	}
	sel := normalizeFallbackModelRef(state.SelectedModel)
	act := normalizeFallbackModelRef(state.ActiveModel)
	r := normalizeFallbackModelRef(state.Reason)
	fallbackActive := selectedModelRef != activeModelRef &&
		sel == selectedModelRef &&
		act == activeModelRef
	if fallbackActive {
		return true, r
	}
	return false, ""
}

// ResolveFallbackTransition computes the full state transition for a fallback event.
func ResolveFallbackTransition(
	selectedProvider, selectedModel, activeProvider, activeModel string,
	attempts []FallbackAttempt,
	state *FallbackNoticeState,
) FallbackTransition {
	selectedModelRef := FormatProviderModelRef(selectedProvider, selectedModel)
	activeModelRef := FormatProviderModelRef(activeProvider, activeModel)

	var prev FallbackNoticeState
	if state != nil {
		prev = *state
	}

	fallbackActive := selectedModelRef != activeModelRef
	fallbackTransitioned := fallbackActive &&
		(normalizeFallbackModelRef(prev.SelectedModel) != selectedModelRef ||
			normalizeFallbackModelRef(prev.ActiveModel) != activeModelRef)
	fallbackCleared := !fallbackActive &&
		(prev.SelectedModel != "" || prev.ActiveModel != "")
	reasonSummary := BuildFallbackReasonSummary(attempts)
	attemptSummaries := BuildFallbackAttemptSummaries(attempts)

	var next FallbackNoticeState
	if fallbackActive {
		next = FallbackNoticeState{
			SelectedModel: selectedModelRef,
			ActiveModel:   activeModelRef,
			Reason:        reasonSummary,
		}
	}

	stateChanged := normalizeFallbackModelRef(prev.SelectedModel) != normalizeFallbackModelRef(next.SelectedModel) ||
		normalizeFallbackModelRef(prev.ActiveModel) != normalizeFallbackModelRef(next.ActiveModel) ||
		normalizeFallbackModelRef(prev.Reason) != normalizeFallbackModelRef(next.Reason)

	return FallbackTransition{
		SelectedModelRef:     selectedModelRef,
		ActiveModelRef:       activeModelRef,
		FallbackActive:       fallbackActive,
		FallbackTransitioned: fallbackTransitioned,
		FallbackCleared:      fallbackCleared,
		ReasonSummary:        reasonSummary,
		AttemptSummaries:     attemptSummaries,
		PreviousState:        prev,
		NextState:            next,
		StateChanged:         stateChanged,
	}
}
