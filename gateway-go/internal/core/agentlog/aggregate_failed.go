package agentlog

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// FailedRequest is one real-user run that ended in run.error: a request the
// operator actually made that the agent failed to complete. This is the
// demand evidence for the curriculum lane's failed-request mining (RSI P5-1)
// — scarcer than coverage-gap inference but strictly more grounded, because
// the environment already asked for the capability in its own words.
type FailedRequest struct {
	Session string `json:"session"`
	Message string `json:"message"` // run.start user input (write-truncated)
	Error   string `json:"error"`
	Ts      int64  `json:"ts"` //nolint:staticcheck // ST1003 — matches LogEntry
}

// liveTestSessionPrefix marks the mock native client's synthetic sessions
// (scripts/mock_native_client.py) — live-test traffic shares the prod
// agent-log dir, and synthetic asks must never read as operator demand.
const liveTestSessionPrefix = "client:lt-"

// FailedUserRequests scans REAL client sessions ("client:*"; system:*/cron:*
// never match the glob, and the live-test synthetic prefix is skipped) for
// run.error entries since sinceMs, joins each to its run's user message,
// dedups by message text (retries of the same ask collapse to the newest)
// and returns newest-first, capped at limit. Errors whose run.start message
// is empty or already rotated away are dropped: a request we cannot quote is
// unusable as demand evidence (the curriculum's verbatim-quote grounding
// gate binds proposals to the request text). Run-ID correlation stays
// per-session-file, mirroring AggregateByModel.
func (w *Writer) FailedUserRequests(sinceMs int64, limit int) []FailedRequest {
	if w == nil || limit <= 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "client:*.jsonl"))
	var out []FailedRequest
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), liveTestSessionPrefix) {
			continue
		}
		starts := map[string]string{} // runId -> user message
		for _, e := range readAllEntries(path) {
			switch e.Type {
			case TypeRunStart:
				var d RunStartData
				if json.Unmarshal(e.Data, &d) == nil {
					starts[e.RunID] = strings.TrimSpace(d.Message)
				}
			case TypeRunError:
				if e.Ts < sinceMs {
					continue
				}
				msg := starts[e.RunID]
				if msg == "" {
					continue
				}
				var d RunErrorData
				_ = json.Unmarshal(e.Data, &d)
				out = append(out, FailedRequest{
					Session: e.Session,
					Message: msg,
					Error:   strings.TrimSpace(d.Error),
					Ts:      e.Ts,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ts > out[j].Ts })
	seen := map[string]bool{}
	deduped := out[:0]
	for _, r := range out {
		if seen[r.Message] {
			continue
		}
		seen[r.Message] = true
		deduped = append(deduped, r)
		if len(deduped) >= limit {
			break
		}
	}
	return deduped
}
