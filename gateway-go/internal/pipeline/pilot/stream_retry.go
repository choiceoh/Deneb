package pilot

import (
	"context"
	"errors"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// StreamError is an error the provider reported inside a stream the HTTP layer
// had already accepted with 200.
//
// OpenRouter reports an upstream provider failing this way — measured
// 2026-09-15 on nvidia/nemotron-3-super-120b-a12b:free, 8 of 14 streaming
// requests came back HTTP 200 carrying {"error":{"code":502,"message":"Upstream
// error from Nvidia: Service temporarily overloaded"}}, none as an HTTP error.
// The LLM client's retries key off the HTTP status and never saw them, and this
// helper walked the fallback chain only when a stream failed to start: every one
// of those calls failed outright instead of reaching the next model.
type StreamError struct {
	Message string
	// Code is the error's numeric code when the provider sent one — for
	// OpenRouter, the upstream's HTTP status (502, 429). Zero when absent.
	Code int
	// Partial reports whether text had already streamed before the error.
	Partial bool
}

// Error keeps the historical "stream error: ..." wording callers match on.
func (e *StreamError) Error() string {
	return "stream error: " + e.Message
}

// inBandRetryDelay is llm.InBandRetryDelay, held in a var so tests do not sleep
// through it.
var inBandRetryDelay = llm.InBandRetryDelay

// classifyStreamFailure decides what an in-stream error allows. transient: the
// chain may move on to the next model. retrySame: a quick retry of the same
// model is worth it first.
//
// Only errors before any text qualify — a model that failed mid-answer is not
// replayed. A rate limit (429) moves on without retrying: another request
// inside the same window meets the same limit. A deterministic error (the model
// rejecting the request) is neither, and surfaces to the caller as it always
// did.
func classifyStreamFailure(err error) (transient, retrySame bool) {
	var se *StreamError
	if !errors.As(err, &se) {
		return false, false
	}
	return llm.ClassifyInBandError(se.Code, se.Message, se.Partial)
}

// streamCandidate runs one model: start the stream, collect it, and retry a
// transient in-band failure a bounded number of times. started reports whether
// the last attempt got a stream at all — a failure to start is the chain's to
// handle, exactly as before.
func streamCandidate(ctx context.Context, client *llm.Client, req llm.ChatRequest) (text string, usage llm.TokenUsage, started bool, err error) {
	for attempt := 0; ; attempt++ {
		events, startErr := client.StreamChat(ctx, req)
		if startErr != nil {
			return "", llm.TokenUsage{}, false, startErr
		}
		text, usage, err = collectStreamCore(ctx, events)
		if err == nil {
			return text, usage, true, nil
		}
		_, retrySame := classifyStreamFailure(err)
		if !retrySame || attempt >= llm.InBandRetries {
			return text, usage, true, err
		}
		select {
		case <-ctx.Done():
			return text, usage, true, err
		case <-time.After(inBandRetryDelay * time.Duration(attempt+1)):
		}
	}
}
