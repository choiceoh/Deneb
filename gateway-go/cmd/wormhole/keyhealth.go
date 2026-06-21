package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Key-health probing: wormhole fronts cloud models whose upstream API keys can go
// dead or hit a quota (a key expires, a plan lapses) — and the failure is silent
// until a real request 401s in the middle of a turn. This probes each keyed cloud
// model's auth on a slow cadence and surfaces the result in /status, so a dead key
// shows up in the gateway's model picker BEFORE it breaks a request.
//
// The probe is a 1-token chat completion, because the cheaper GET /models is
// unreliable here — z.ai's coding endpoint 401s /models even for a valid key, so
// only the actual completion path distinguishes a live key from a dead one. A
// dead key 401s before generating, so it costs nothing; a live key costs one
// token per cycle, which at the interval below is negligible. Local (keyless)
// models are skipped — they have no upstream credential to check.
const (
	keyHealthRefreshInterval = 15 * time.Minute
	keyHealthProbeTimeout    = 20 * time.Second
)

// keyHealthState is the last probe outcome for one cloud model. Stored keyless;
// surfaced (as a label) in /status.
type keyHealthState struct {
	OK        bool      // last probe authenticated (HTTP 200)
	Status    int       // last HTTP status (0 = never probed / unreachable)
	CheckedAt time.Time // when the probe ran
	Err       string    // short reason when !OK ("" on OK)
}

// label projects a probe outcome into the /status string. cloud=false → "" (a
// local model has no key to check).
func (s keyHealthState) label(cloud bool) string {
	switch {
	case !cloud:
		return ""
	case s.CheckedAt.IsZero():
		return "unchecked"
	case s.OK:
		return "ok"
	case s.Status == http.StatusUnauthorized || s.Status == http.StatusForbidden:
		return "auth_failed"
	case s.Status == http.StatusTooManyRequests:
		return "rate_limited"
	case s.Status == 0:
		return "unreachable"
	default:
		return fmt.Sprintf("http_%d", s.Status)
	}
}

// refreshKeyHealth probes every keyed cloud model's auth and swaps in a fresh map
// (keyed by client-facing model name). Mirrors refreshWindows: best-effort, sole
// writer, atomic swap. Logs a Warn only on a transition INTO auth-failure so a
// newly-dead key is visible in the journal without spamming every cycle.
func (rt *router) refreshKeyHealth(parent context.Context) {
	prev := rt.keyHealth.Load()
	next := map[string]keyHealthState{}
	for _, m := range rt.mergedModels() {
		e, ok := rt.lookup(m.Name) // resolve fleet-backed entries to a live URL
		if !ok || e.URL == "" || e.isLocal() || e.Key == "" {
			continue // only keyed cloud models have an upstream credential to check
		}
		ctx, cancel := context.WithTimeout(parent, keyHealthProbeTimeout)
		st := probeKeyAuth(ctx, rt.client, e)
		cancel()
		next[m.Name] = st

		// Transition INTO auth-failure (was OK or unprobed) → Warn once.
		if st.label(true) == "auth_failed" {
			was := keyHealthState{}
			if prev != nil {
				was = (*prev)[m.Name]
			}
			if was.label(true) != "auth_failed" {
				rt.log.Warn("upstream key health: auth failed — a dead/invalid key will break this model",
					"model", m.Name, "status", st.Status)
			}
		}
	}
	rt.keyHealth.Store(&next)
}

// probeKeyAuth sends a 1-token completion to a cloud backend and classifies the
// HTTP status: 200 → live, 401/403 → dead key, anything else recorded as-is.
func probeKeyAuth(ctx context.Context, client *http.Client, e modelEntry) keyHealthState {
	st := keyHealthState{CheckedAt: time.Now()}
	upModel := e.UpstreamModel
	if upModel == "" {
		upModel = e.Name
	}
	path, body := "/chat/completions", probeBody(upModel)
	if e.protocol() == protocolAnthropic {
		path = "/messages"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(e.URL, "/")+path, bytes.NewReader(body))
	if err != nil {
		st.Err = err.Error()
		return st
	}
	req.Header.Set("Content-Type", "application/json")
	applyUpstreamAuth(req, e, req) // clientReq=req: anthropic-version falls back to the default
	resp, err := client.Do(req)
	if err != nil {
		st.Err = "unreachable: " + err.Error()
		return st
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	st.Status = resp.StatusCode
	switch {
	case resp.StatusCode == http.StatusOK:
		st.OK = true
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		st.Err = "auth failed (dead/invalid key)"
	default:
		st.Err = fmt.Sprintf("http %d", resp.StatusCode)
	}
	return st
}

func probeBody(model string) []byte {
	b, _ := json.Marshal(map[string]any{
		"model":      model,
		"max_tokens": 1,
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
	})
	return b
}
