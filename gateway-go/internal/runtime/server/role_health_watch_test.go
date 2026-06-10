package server

import (
	"bytes"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

func newTestRoleHealthWatch(t *testing.T) (*roleHealthWatch, *bytes.Buffer, *[]string) {
	t.Helper()
	var logBuf bytes.Buffer
	var events []string
	w := &roleHealthWatch{
		logger:    slog.New(slog.NewTextHandler(&logBuf, nil)),
		statePath: filepath.Join(t.TempDir(), "role_health.json"),
		broadcast: func(event string, _ any) { events = append(events, event) },
	}
	return w, &logBuf, &events
}

func TestRoleHealthWatch_EdgeAlertsOnly(t *testing.T) {
	w, logBuf, events := newTestRoleHealthWatch(t)
	targets := []roleHealthTarget{{providerID: "zai", model: "glm-5.1", roles: []string{"fallback"}}}

	// First cycle: unknown → auth must alert (Error + broadcast).
	w.applyVerdicts(targets, map[string]string{"zai": roleHealthAuth})
	if !strings.Contains(logBuf.String(), "model role provider unhealthy") {
		t.Fatalf("expected unhealthy Error log, got: %s", logBuf.String())
	}
	if len(*events) != 1 {
		t.Fatalf("expected 1 broadcast, got %d", len(*events))
	}

	// Second cycle, same verdict: NO new alert (edge-only, no spam).
	logBuf.Reset()
	w.applyVerdicts(targets, map[string]string{"zai": roleHealthAuth})
	if strings.Contains(logBuf.String(), "unhealthy") || len(*events) != 1 {
		t.Fatalf("steady-state bad verdict must not re-alert (events=%d, log=%s)", len(*events), logBuf.String())
	}

	// Recovery: auth → ok alerts once at Info.
	logBuf.Reset()
	w.applyVerdicts(targets, map[string]string{"zai": roleHealthOK})
	if !strings.Contains(logBuf.String(), "recovered") {
		t.Fatalf("expected recovery log, got: %s", logBuf.String())
	}
	if len(*events) != 2 {
		t.Fatalf("expected 2 broadcasts after recovery, got %d", len(*events))
	}

	// Steady-state ok: silent.
	logBuf.Reset()
	w.applyVerdicts(targets, map[string]string{"zai": roleHealthOK})
	if logBuf.Len() > 0 || len(*events) != 2 {
		t.Fatalf("steady-state ok must be silent (events=%d, log=%s)", len(*events), logBuf.String())
	}
}

func TestRoleHealthWatch_StatePersistsAcrossRestart(t *testing.T) {
	w, _, _ := newTestRoleHealthWatch(t)
	targets := []roleHealthTarget{{providerID: "mimo-plan", model: "mimo-v2.5-pro", roles: []string{"fallback"}}}
	w.applyVerdicts(targets, map[string]string{"mimo-plan": roleHealthOK})

	// A "restarted" watch sharing the state file sees the prior verdicts and
	// a fresh probe clock — so a 3-12 min SIGUSR1 restart cadence cannot
	// reset the interval into never-fires or probe-on-every-boot.
	w2 := &roleHealthWatch{logger: w.logger, statePath: w.statePath}
	w2.loadState()
	if w2.state.Verdicts["mimo-plan"] != roleHealthOK {
		t.Fatalf("verdict not persisted: %+v", w2.state)
	}
	if w2.state.LastProbeMs == 0 {
		t.Fatal("probe clock not persisted")
	}
	wait := w2.untilNextProbe()
	if wait < roleHealthInterval-time.Minute || wait > roleHealthInterval {
		t.Fatalf("fresh probe clock should defer ~one interval, got %v", wait)
	}

	// A stale clock (interval already elapsed) probes after the boot delay.
	w2.state.LastProbeMs = time.Now().Add(-2 * roleHealthInterval).UnixMilli()
	if wait := w2.untilNextProbe(); wait != roleHealthBootDelay {
		t.Fatalf("overdue probe should wait only the boot delay, got %v", wait)
	}

	// Corrupt state file degrades to zero state, not a crash.
	if err := os.WriteFile(w.statePath, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	w3 := &roleHealthWatch{logger: w.logger, statePath: w.statePath}
	w3.loadState()
	if w3.state.LastProbeMs != 0 {
		t.Fatalf("corrupt state should reset, got %+v", w3.state)
	}
}

func TestClassifyProbeError(t *testing.T) {
	// Request-level 401 wrapped the way httpretry surfaces it (the exact
	// shape of the 2026-06 Z.AI incident).
	authErr := &httpretry.APIError{StatusCode: 401, Message: `{"error":{"message":"token expired or incorrect","type":"401"}}`}
	if got := classifyProbeError(authErr); got != roleHealthAuth {
		t.Fatalf("401 APIError = %q, want auth", got)
	}
	if got := classifyProbeError(&httpretry.APIError{StatusCode: 403, Message: "forbidden"}); got != roleHealthAuth {
		t.Fatalf("403 APIError should classify auth, got %q", got)
	}
	// Provider-specific auth message without a lifted status (mid-stream
	// error events reach us as bare text).
	if got := classifyProbeError(errors.New("API error: token expired or incorrect")); got != roleHealthAuth {
		t.Fatalf("auth message should classify auth, got %q", got)
	}
	// Network-ish failure stays "down", never "auth".
	if got := classifyProbeError(errors.New("dial tcp: connection refused")); got != roleHealthDown {
		t.Fatalf("network error = %q, want down", got)
	}
	if got := classifyProbeError(&httpretry.APIError{StatusCode: 502, Message: "bad gateway"}); got != roleHealthDown {
		t.Fatalf("502 = %q, want down", got)
	}
}
