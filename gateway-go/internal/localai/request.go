// Package localai provides a centralized hub for all local AI LLM requests.
//
// Instead of scattered direct calls to the local model, every caller goes through
// Hub.Submit(). The hub manages a shared token budget, priority queue, response
// cache, inference-based health checks, and zombie request cleanup.
package localai

import (
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/llm"
)

// Priority determines dispatch ordering and admission control behavior.
type Priority int

const (
	// PriorityCritical — pilot, proactive context, memory recall.
	// Allowed to overdraw token budget by 25%.
	PriorityCritical Priority = iota

	// PriorityNormal — session memory, fact extraction.
	// Admitted only within budget.
	PriorityNormal

	// PriorityBackground — dreaming, auto-memory, activity summary.
	// Dropped when queue depth exceeds limit.
	PriorityBackground
)

// Request describes a single LLM call to be dispatched through the hub.
type Request struct {
	// System prompt (plain string, hub wraps as llm.SystemString).
	System string

	// Messages to send. Most callers use a single user message, but session
	// memory uses multi-turn (user → assistant → user).
	Messages []llm.Message

	// MaxTokens caps the response length.
	MaxTokens int

	// Priority controls dispatch order and admission behavior.
	Priority Priority

	// CallerTag identifies the caller for metrics (e.g., "proactive", "session_memory").
	CallerTag string

	// ExtraBody is merged into the OpenAI-compatible request body.
	// The hub merges NoThinking defaults (currently empty) and a server-side
	// "timeout" field; caller entries are merged on top.
	ExtraBody map[string]any

	// ResponseFormat requests structured output (e.g., json_object mode).
	ResponseFormat *llm.ResponseFormat

	// EstInputTokens is the caller's estimate of input token count.
	// If zero, the hub estimates from message content using rune-based counting.
	EstInputTokens int

	// NoCache disables response caching for this request.
	// Use for non-deterministic calls (JSON-mode dreaming phases) where retries
	// may intentionally produce different results.
	NoCache bool

	// CacheTTL overrides the default cache TTL for this request's response.
	// Zero means use the hub's default (5 minutes). Negative means no cache.
	CacheTTL time.Duration
}

// Response is the result of a hub submission.
type Response struct {
	// Text is the collected response text.
	Text string

	// FromCache is true if the response was served from cache.
	FromCache bool

	// Duration is the wall-clock time from submission to response.
	Duration time.Duration
}

// SimpleRequest creates a minimal Request with a single user message.
func SimpleRequest(system, userMessage string, maxTokens int, priority Priority, tag string) Request {
	return Request{
		System:    system,
		Messages:  []llm.Message{llm.NewTextMessage("user", userMessage)},
		MaxTokens: maxTokens,
		Priority:  priority,
		CallerTag: tag,
	}
}
