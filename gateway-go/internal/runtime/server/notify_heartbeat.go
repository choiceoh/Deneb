// notify_heartbeat.go — notifyService self-health heartbeat: the periodic
// liveness ticker, the /health self-poll probe, and the heartbeat/hang-alert
// line formatting. Split from notify_relay.go (pure move).
package server

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"
)

// heartbeatInterval drives the periodic liveness ping to the monitoring
// chat. Five minutes is a compromise: short enough that an operator
// notices a hung gateway within a coffee break, long enough that it
// doesn't drown the chat. The first beat skips a fixed warmup window
// after start so boot-time activity has time to settle.
const heartbeatInterval = 5 * time.Minute

// heartbeatWarmup suppresses the heartbeat for this long after start so
// the operator doesn't see a blast of "alive" pings during boot, where
// every subsystem is logging its own initialization noise.
const heartbeatWarmup = 30 * time.Second

// selfPollTimeout caps each /health probe so a hung HTTP mux is detected
// quickly. Three seconds is well above normal /health latency (single-digit
// ms) and short enough that operators get the alert on the same heartbeat
// cycle as the hang.
const selfPollTimeout = 3 * time.Second

// goroutineWarnAbsolute is the absolute-count threshold for goroutine
// leak detection. The gateway's steady state runs ~50–200 goroutines;
// 2000 is a clear leak signal. Threshold-based alerts coalesce via the
// same 30s debounce as broadcast events.
const goroutineWarnAbsolute = 2000

// allocWarnBytes triggers a memory-pressure alert. 2 GiB is well above
// healthy alloc on a single-user gateway and below typical OOM thresholds,
// giving the operator advance warning rather than a postmortem from logs.
const allocWarnBytes = 2 * 1024 * 1024 * 1024

// runHeartbeat fires a liveness ping every heartbeatInterval. The point
// is detection of "gateway is alive but the broadcast taps are silent
// because nothing's happening" vs "gateway is hung and even broadcasts
// stopped". Without this, an operator can't distinguish the two from the
// monitoring chat alone.
//
// startTime is captured BEFORE the warmup delay so the reported uptime
// matches the operator's intuition ("gateway started at ...") instead of
// "time since the heartbeat goroutine began ticking".
func (n *notifyService) runHeartbeat(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			n.logger.Error("panic in heartbeat loop", "panic", r)
		}
	}()
	startTime := time.Now()

	// Warmup delay so boot-time noise doesn't trigger the first beat.
	select {
	case <-ctx.Done():
		return
	case <-time.After(heartbeatWarmup):
	}

	t := time.NewTicker(heartbeatInterval)
	defer t.Stop()

	// Send one beat immediately after warmup so the operator gets a
	// "monitoring channel is wired" confirmation without waiting 5 min.
	n.enqueueHeartbeat(startTime)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			n.enqueueHeartbeat(startTime)
		}
	}
}

// enqueueHeartbeat formats the current liveness summary and pushes it
// onto the worker queue. Drops silently on overflow — heartbeats are
// best-effort. Uses the same per-event debounce key so a manual force
// (future) can't double-fire within the interval.
//
// Before queueing, runs a self-poll against the gateway's own /health
// endpoint. A failed self-poll means the HTTP mux is hung even though
// this goroutine is alive — exactly the case the basic "💓 alive" line
// would mislead about. The self-poll result is woven into the heartbeat
// body as a 🚨 prefix on failure.
func (n *notifyService) enqueueHeartbeat(startTime time.Time) {
	now := time.Now()
	pollOK, pollLatency, pollErr := n.selfPoll(context.Background())

	body := n.buildHeartbeatLine(startTime, now)
	if !pollOK {
		body = n.composeHangAlert(pollErr) + "\n" + body
	} else if pollLatency > 0 {
		body += fmt.Sprintf(" — /health %s", humanLatency(pollLatency))
	}

	if !n.checkDebounce("_heartbeat") {
		return
	}
	select {
	case n.queue <- notifyEvent{name: "_heartbeat", payload: body}:
		n.markSent("_heartbeat")
	default:
		// Silent drop on overflow — sending another log here would loop
		// back through the slog forwarder.
	}
}

// selfPoll issues a short-deadline GET to the gateway's own /health
// endpoint. Returns (ok=true, latency, nil) when the response is 2xx
// within selfPollTimeout. Returns (ok=false, _, err) on timeout, network
// error, or non-2xx — these are all "the gateway can't answer its own
// health probe" which the operator should be alerted to.
//
// Returns (ok=true, 0, nil) when boundAddr is unavailable (listener not
// yet bound, e.g. during the warmup tick). The first beat after listener
// bind will report a real status.
func (n *notifyService) selfPoll(ctx context.Context) (ok bool, latency time.Duration, err error) {
	if n.boundAddr == nil {
		return true, 0, nil
	}
	addr := n.boundAddr()
	if addr == "" {
		return true, 0, nil
	}
	if n.httpClient == nil {
		return true, 0, nil
	}

	url := "http://" + addr + "/health"
	pollCtx, cancel := context.WithTimeout(ctx, selfPollTimeout)
	defer cancel()
	req, reqErr := http.NewRequestWithContext(pollCtx, http.MethodGet, url, http.NoBody)
	if reqErr != nil {
		return false, 0, reqErr
	}

	start := time.Now()
	resp, doErr := n.httpClient.Do(req)
	latency = time.Since(start)
	if doErr != nil {
		return false, latency, doErr
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, latency, fmt.Errorf("self-poll status %d", resp.StatusCode)
	}
	return true, latency, nil
}

// composeHangAlert formats the leading 🚨 line for a heartbeat where the
// self-poll failed. Includes the underlying error truncated for chat.
// Kept short on purpose — the operator's first action is "is the box
// reachable", not "what's the stack trace".
func (n *notifyService) composeHangAlert(pollErr error) string {
	msg := "(unknown error)"
	if pollErr != nil {
		msg = truncate(pollErr.Error(), 200)
	}
	return "🚨 게이트웨이 응답 없음 — self-poll 실패: " + msg
}

// humanLatency formats a duration as a coarse Korean shorthand suitable
// for the heartbeat line. Granularity matches operator expectation:
// "/health 2ms", "/health 134ms", "/health 1.2s".
func humanLatency(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
}

// buildHeartbeatLine composes the "I'm alive" message. Includes uptime,
// goroutine count, allocated memory, active session count. Numbers come
// from runtime stats; a single beat reads them once for consistency.
//
// When goroutine count or memory alloc cross the warning thresholds,
// the prefix flips from "💓 정상" to "⚠️ 부하" so the operator notices
// at a glance without having to compare numbers across messages.
func (n *notifyService) buildHeartbeatLine(startTime, now time.Time) string {
	uptime := humanDuration(now.Sub(startTime))
	goroutines := runtime.NumGoroutine()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	allocBytes := ms.Alloc
	allocMB := allocBytes / (1024 * 1024)

	sessionCount := 0
	if n.sessions != nil {
		sessionCount = n.sessions.Count()
	}

	prefix := "💓 게이트웨이 정상"
	var warnings []string
	if goroutines >= goroutineWarnAbsolute {
		warnings = append(warnings, fmt.Sprintf("goroutine %d (>%d)", goroutines, goroutineWarnAbsolute))
	}
	if allocBytes >= allocWarnBytes {
		warnings = append(warnings, fmt.Sprintf("mem %dMB (>%dMB)", allocMB, allocWarnBytes/(1024*1024)))
	}
	if len(warnings) > 0 {
		prefix = "⚠️ 게이트웨이 부하 — " + strings.Join(warnings, ", ")
	}

	return fmt.Sprintf(
		"%s — uptime %s, 세션 %d, goroutine %d, mem %dMB",
		prefix, uptime, sessionCount, goroutines, allocMB,
	)
}
