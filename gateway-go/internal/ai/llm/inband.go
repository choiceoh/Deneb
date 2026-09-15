package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
)

// InBandRetries bounds same-model retries of a transient in-band error, and
// InBandRetryDelay is their base spacing (×retry number). The failures return in
// about 0.2s, so two quick retries cost under two seconds and recover most calls
// at the measured failure rate before the caller moves on to a model that may be
// slower or billed. Every caller retrying in-band errors uses these, so a helper
// call and a mail extraction give the same upstream the same patience.
const (
	InBandRetries    = 2
	InBandRetryDelay = 400 * time.Millisecond
)

// InBandErrorCode returns the numeric code of a stream "error" event payload —
// top-level ({"code":502,...}, as probeOpenAIError repacks it) or nested
// ({"error":{"code":502}}) — or 0 when there is none.
func InBandErrorCode(payload FlexibleJSON) int {
	var body struct {
		Code  json.RawMessage `json:"code"`
		Error struct {
			Code json.RawMessage `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(payload.Bytes(), &body) != nil {
		return 0
	}
	var code int
	if json.Unmarshal(body.Code, &code) == nil && code != 0 {
		return code
	}
	if json.Unmarshal(body.Error.Code, &code) == nil {
		return code
	}
	return 0
}

// ClassifyInBandError decides what an error reported inside an HTTP-200 stream
// allows a caller with somewhere else to go. transient: moving on to the next
// model is right. retrySame: a quick retry of the same model is worth it first.
//
// OpenRouter reports an upstream failing this way — 2026-09-15, 8 of 14
// streaming requests to a free Nvidia endpoint came back 200 carrying
// {"error":{"code":502,"message":"...Service temporarily overloaded"}}. The
// client's retries key off the HTTP status and never see it.
//
//   - Only an error before any text qualifies; a model that failed mid-answer
//     is not replayed.
//   - 429 moves on without retrying: another request in the same window meets
//     the same limit.
//   - 5xx retries, then moves on.
//   - Without a code, only what the shared classifier recognizes counts. The
//     wording alone is not trusted: walking the chain on an error that was the
//     model rejecting the request would hide it.
func ClassifyInBandError(code int, message string, partial bool) (transient, retrySame bool) {
	if partial {
		return false, false
	}
	switch {
	case code == http.StatusTooManyRequests:
		return true, false
	case code >= 500 && code < 600:
		return true, true
	case code != 0:
		return false, false
	}
	switch llmerr.Classify(errors.New(message), 0, nil).Reason {
	case llmerr.ReasonRateLimit:
		return true, false
	case llmerr.ReasonServerError, llmerr.ReasonOverloaded, llmerr.ReasonTimeout:
		return true, true
	default:
		return false, false
	}
}
