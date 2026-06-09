package chat

// Broadcast event payloads — the canonical catalog of every event the chat
// pipeline pushes to the native client. These named structs replace the ad-hoc
// map[string]any literals that were scattered across run_*.go and rpc.go so that:
//
//   - field names are checked by the compiler (no silent map-key typos),
//   - the wire shape of every event lives in one documented place, and
//   - drift between emitters of the same event surfaces as an explicit
//     `omitempty` field instead of a silent key that's present on one code path
//     and absent on another.
//
// WIRE COMPATIBILITY IS PRESERVED EXACTLY. Every json tag matches the prior map
// key verbatim, and `omitempty` is applied ONLY to keys that were already absent
// from some emitters (see the per-field notes). The native client parses these
// payloads dynamically (by key), so this is a gateway-side correctness and
// documentation change — the JSON bytes on the wire are unchanged. The round-trip
// checks in broadcast_events_test.go lock that invariant.
//
// Reference: .claude/rules/logging.md treats delivery/empty-response events as
// "user did not get a reply" signals; keeping their schema explicit here makes
// those failure payloads auditable.

// SessionsChangedEvent — "sessions.changed". The session lifecycle moved
// (started / merged / aborted / finished / panic). The most frequently emitted
// event. DeltaMs is set only on the "merged" path (rpc.go), so it is omitempty:
// a zero value is dropped from the wire exactly as the other emitters never sent
// the key.
type SessionsChangedEvent struct {
	SessionKey string `json:"sessionKey"`
	Reason     string `json:"reason"`
	Status     string `json:"status"`
	DeltaMs    int64  `json:"deltaMs,omitempty"`
}

// ChatDeliveryFailedEvent — "chat.delivery_failed". A final reply could not be
// delivered to the native client (the user got nothing). Error is present only
// when an underlying call returned one (stop_fallback_error / reply_func_error),
// absent for the structural reasons (parse_directives_nil / reply_func_nil), so
// it is omitempty to match.
type ChatDeliveryFailedEvent struct {
	Session string `json:"session"`
	Channel string `json:"channel"`
	Reason  string `json:"reason"`
	Error   string `json:"error,omitempty"`
}

// ChatEmptyResponseEvent — "chat.empty_response". The turn finished but produced
// no deliverable text. Turns has no omitempty: the prior map always included it
// (even at 0).
type ChatEmptyResponseEvent struct {
	Session    string `json:"session"`
	Channel    string `json:"channel"`
	StopReason string `json:"stopReason"`
	Turns      int    `json:"turns"`
}

// ChatMediaDeliveryFailedEvent — "chat.media_delivery_failed". Some media URLs
// failed to deliver. All fields were always present in the prior map.
type ChatMediaDeliveryFailedEvent struct {
	Session string   `json:"session"`
	Channel string   `json:"channel"`
	Count   int      `json:"count"`
	Total   int      `json:"total"`
	URLs    []string `json:"urls"`
}

// SessionToolEvent — "session.tool". A tool call started/finished within a run
// (used by the native client's tool-activity surface). Emitted on every tool
// call, so it is high-frequency.
type SessionToolEvent struct {
	SessionKey string `json:"sessionKey"`
	RunID      string `json:"runId"`
	Tool       string `json:"tool"`
	ToolUseID  string `json:"toolUseId"`
	IsError    bool   `json:"isError"`
}

// ChatToolFailedEvent — "chat.tool_failed". A mutation tool failed in-band. The
// prior map carried BOTH "session" and "sessionKey" with the same value; both
// are kept verbatim for wire compatibility (the client may key on either).
type ChatToolFailedEvent struct {
	Session    string `json:"session"`
	SessionKey string `json:"sessionKey"`
	RunID      string `json:"runId"`
	Tool       string `json:"tool"`
	Reason     string `json:"reason"`
	Error      string `json:"error"`
}

// ChatCompactionDegradedEvent — "chat.compaction_degraded". Compaction ran but
// could not reach the target budget. All fields always present.
type ChatCompactionDegradedEvent struct {
	Session      string `json:"session"`
	TokensBefore int    `json:"tokensBefore"`
	TokensAfter  int    `json:"tokensAfter"`
	Budget       int    `json:"budget"`
}

// ChatCompactionStuckEvent — "chat.compaction_stuck". Compaction could not make
// progress. The two emitters diverge: the protected-zone path sends Budget, the
// idempotent-compaction path sends InputHash. Both are omitempty so each path's
// JSON matches what it sent before (the other key was simply absent).
type ChatCompactionStuckEvent struct {
	Reason       string `json:"reason"`
	MessageCount int    `json:"messageCount"`
	Budget       int    `json:"budget,omitempty"`
	InputHash    string `json:"inputHash,omitempty"`
}

// ChatContextOverflowEvent — "chat.context_overflow_unrecoverable". All
// compaction retries were exhausted and the context still overflows. All fields
// always present.
type ChatContextOverflowEvent struct {
	Model        string `json:"model"`
	MessageCount int    `json:"messageCount"`
	Attempts     int    `json:"attempts"`
	Error        string `json:"error"`
}
