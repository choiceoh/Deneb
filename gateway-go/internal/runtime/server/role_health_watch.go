// role_health_watch.go — periodic auth/liveness probe for role-assigned model
// providers.
//
// Why this exists: the fallback role is a safety net that gets no organic
// traffic until something else is already failing, so a dead credential on it
// stays invisible. The 2026-06 Z.AI key expiry sat unnoticed for weeks until
// the one surface hard-pinned to the fallback role (miniapp gmail.analyze)
// started returning 401. Reachability probes cannot see this failure class:
// Z.AI answers GET /models with 200 even for an expired key (measured), so
// the only honest signal is a real, minimal completion call through the same
// llm.Client production traffic uses.
//
// Design:
//   - Probes the unique cloud providers behind the core roles (main / tiny /
//     lightweight / analysis / coding / fallback). Local endpoints are skipped — they
//     have no credential to expire and the miniapp reachability probes
//     already cover them.
//   - One 1-token completion per provider per cycle (~a few tokens/day).
//   - The probe clock is persisted (denebDir/role_health.json) because the
//     gateway is restarted externally every few minutes; an in-memory ticker
//     would reset forever and never fire (the #2132 periodic-task death
//     pattern), while probe-on-every-boot would turn restarts into a probe
//     storm.
//   - Alerts fire only on state EDGES (ok→bad, bad→ok). slog.Error records
//     are mirrored to the monitoring chat by notify_slog, so an Error here
//     reaches the operator without extra push wiring; a broadcast event is
//     emitted alongside for UI/monitoring taps.
//   - The miniapp models tab overlays the latest verdicts so a key-dead
//     provider shows an explicit "auth" state instead of a green dot.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/pkg/llmerr"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

const (
	// roleHealthInterval is the wall-clock spacing between probe cycles,
	// enforced across restarts via the persisted state file.
	roleHealthInterval = 6 * time.Hour
	// roleHealthBootDelay keeps the first cycle (when one is due) off the
	// boot critical path and out of the restart rush.
	roleHealthBootDelay = 2 * time.Minute
	// roleHealthProbeTimeout bounds a single provider probe.
	roleHealthProbeTimeout = 30 * time.Second
	// roleHealthRetryDelay spaces the single retry that guards "down"
	// verdicts against transient blips (a busy local vLLM, a restart). Auth
	// verdicts are deterministic and never retried.
	roleHealthRetryDelay = 10 * time.Second
)

// Role-provider verdicts. Stored as strings so the persisted file and the
// miniapp health overlay share one vocabulary.
const (
	roleHealthOK   = "ok"
	roleHealthAuth = "auth" // credential rejected (401/403-class)
	roleHealthDown = "down" // network failure / non-auth API error
)

// roleHealthState is the persisted shape: when the last cycle ran and what
// it concluded per provider. Surviving restarts is the whole point — see
// the file header.
type roleHealthState struct {
	LastProbeMs int64             `json:"lastProbeMs"`
	Verdicts    map[string]string `json:"verdicts"`
}

// roleHealthTarget is one unique cloud provider to probe, with the role set
// that depends on it (for alert context) and a wired client to probe through.
type roleHealthTarget struct {
	providerID string
	model      string
	roles      []string
	client     *llm.Client
}

// roleHealthWatch owns the probe loop and the latest verdicts.
//
// Lock hierarchy: mu only guards state; it is never held across probes,
// logging, or broadcasts.
type roleHealthWatch struct {
	logger    *slog.Logger
	registry  *modelrole.Registry
	broadcast func(event string, payload any) // nil-safe wrapper, never nil
	statePath string

	mu    sync.Mutex
	state roleHealthState

	// probeFn is swappable for tests; defaults to probeProviderOnce. The
	// second return is the raw error text ("" when healthy) — a bare "down"
	// verdict is undiagnosable from the monitoring chat.
	probeFn func(ctx context.Context, t roleHealthTarget) (verdict, errText string)
}

// startRoleHealthWatch launches the watch loop. Idempotent; no-ops when the
// model registry is absent (provider-less dev gateway).
func (s *Server) startRoleHealthWatch() {
	if s.modelRegistry == nil || s.roleHealth != nil {
		return
	}
	w := &roleHealthWatch{
		logger:   s.logger,
		registry: s.modelRegistry,
		broadcast: func(event string, payload any) {
			if s.broadcaster != nil {
				s.broadcaster.Broadcast(event, payload)
			}
		},
		statePath: filepath.Join(s.denebDir, "role_health.json"),
	}
	w.probeFn = w.probeProviderOnce
	w.loadState()
	s.roleHealth = w

	s.logger.Info("role health watch started",
		"interval", roleHealthInterval.String(), "state", w.statePath)
	safego.GoWithSlog(s.logger, "role-health-watch", func() {
		w.run(s.ShutdownCtx())
	})
}

// roleHealthVerdicts returns a copy of the latest per-provider verdicts for
// the miniapp models tab overlay. Nil when the watch is not running.
func (s *Server) roleHealthVerdicts() map[string]string {
	if s.roleHealth == nil {
		return nil
	}
	return s.roleHealth.verdictsCopy()
}

func (w *roleHealthWatch) verdictsCopy() map[string]string {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]string, len(w.state.Verdicts))
	for k, v := range w.state.Verdicts {
		out[k] = v
	}
	return out
}

// run sleeps until the next persisted due time, probes, and repeats. The
// persisted clock means a gateway restarted every few minutes still probes
// only once per roleHealthInterval.
func (w *roleHealthWatch) run(ctx context.Context) {
	for {
		wait := w.untilNextProbe()
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		w.probeCycle(ctx)
	}
}

func (w *roleHealthWatch) untilNextProbe() time.Duration {
	w.mu.Lock()
	last := time.UnixMilli(w.state.LastProbeMs)
	w.mu.Unlock()
	due := last.Add(roleHealthInterval)
	if wait := time.Until(due); wait > roleHealthBootDelay {
		return wait
	}
	return roleHealthBootDelay
}

// probeCycle probes every target once and alerts on verdict edges. A "down"
// verdict gets one retry after a short delay before it counts — a 6-hour
// cadence means a single transient blip (busy vLLM, mid-restart) would
// otherwise read as an outage for the next quarter day. Auth rejections are
// deterministic, so they count immediately.
func (w *roleHealthWatch) probeCycle(ctx context.Context) {
	targets := w.collectTargets()
	verdicts := make(map[string]string, len(targets))
	errs := make(map[string]string, len(targets))
	for _, t := range targets {
		verdict, errText := w.probeOnceWithTimeout(ctx, t)
		if verdict == roleHealthDown {
			w.logger.Warn("model role provider probe failed, retrying once",
				"provider", t.providerID, "error", errText)
			select {
			case <-ctx.Done():
				return
			case <-time.After(roleHealthRetryDelay):
			}
			verdict, errText = w.probeOnceWithTimeout(ctx, t)
		}
		verdicts[t.providerID], errs[t.providerID] = verdict, errText
		if ctx.Err() != nil {
			return // shutdown mid-cycle: keep prior state, no partial alerts
		}
	}
	w.applyVerdicts(targets, verdicts, errs)
}

func (w *roleHealthWatch) probeOnceWithTimeout(ctx context.Context, t roleHealthTarget) (string, string) {
	probeCtx, cancel := context.WithTimeout(ctx, roleHealthProbeTimeout)
	defer cancel()
	return w.probeFn(probeCtx, t)
}

// collectTargets resolves the unique non-local providers behind the roles.
func (w *roleHealthWatch) collectTargets() []roleHealthTarget {
	reg := w.registry
	if reg == nil {
		return nil
	}
	byProvider := make(map[string]*roleHealthTarget)
	for _, role := range []modelrole.Role{
		modelrole.RoleMain, modelrole.RoleTiny, modelrole.RoleLightweight,
		modelrole.RoleAnalysis, modelrole.RoleCoding, modelrole.RoleFallback,
	} {
		cfg := reg.Config(role)
		if cfg.ProviderID == "" || cfg.BaseURL == "" || isLocalURL(cfg.BaseURL) {
			continue
		}
		if t, ok := byProvider[cfg.ProviderID]; ok {
			t.roles = append(t.roles, string(role))
			continue
		}
		client := reg.Client(role)
		if client == nil {
			continue
		}
		byProvider[cfg.ProviderID] = &roleHealthTarget{
			providerID: cfg.ProviderID,
			model:      cfg.Model,
			roles:      []string{string(role)},
			client:     client,
		}
	}
	targets := make([]roleHealthTarget, 0, len(byProvider))
	for _, t := range byProvider {
		targets = append(targets, *t)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].providerID < targets[j].providerID })
	return targets
}

// probeProviderOnce sends a minimal 1-token completion through the exact
// client production traffic uses, so credential resolution (static key, Kimi
// CLI token func, headers) is probed truthfully.
func (w *roleHealthWatch) probeProviderOnce(ctx context.Context, t roleHealthTarget) (string, string) {
	events, err := t.client.StreamChat(ctx, llm.ChatRequest{
		Model:     t.model,
		Messages:  []llm.Message{llm.NewTextMessage("user", "ping")},
		MaxTokens: 1,
		Stream:    true,
		Thinking:  &llm.ThinkingConfig{Type: "disabled"},
	})
	if err != nil {
		return classifyProbeError(err), err.Error()
	}
	// Drain to completion so the transport goroutine never leaks; a
	// mid-stream "error" event still counts as a verdict (auth failures on
	// some providers surface here rather than as a request-level error).
	verdict, errText := roleHealthOK, ""
	for ev := range events {
		if ev.Type != "error" || verdict != roleHealthOK {
			continue
		}
		var errBody struct {
			Message string `json:"message"`
		}
		msg := string(ev.Payload)
		if json.Unmarshal(ev.Payload, &errBody) == nil && errBody.Message != "" {
			msg = errBody.Message
		}
		verdict, errText = classifyProbeError(errors.New(msg)), msg
	}
	return verdict, errText
}

// classifyProbeError maps a probe failure to a verdict, lifting the HTTP
// status out of wrapped *httpretry.APIError the same way the chat pipeline
// does so 401/403 classify as auth instead of unknown.
func classifyProbeError(err error) string {
	var apiErr *httpretry.APIError
	status := 0
	var body []byte
	if errors.As(err, &apiErr) {
		status = apiErr.StatusCode
		if apiErr.Message != "" {
			body = []byte(apiErr.Message)
		}
	}
	if llmerr.Classify(err, status, body).Reason == llmerr.ReasonAuth {
		return roleHealthAuth
	}
	return roleHealthDown
}

// applyVerdicts persists the cycle result and alerts on edges only.
func (w *roleHealthWatch) applyVerdicts(targets []roleHealthTarget, verdicts, errs map[string]string) {
	w.mu.Lock()
	prev := w.state.Verdicts
	w.state.Verdicts = verdicts
	w.state.LastProbeMs = time.Now().UnixMilli()
	w.mu.Unlock()
	w.saveState()

	for _, t := range targets {
		now := verdicts[t.providerID]
		before, known := prev[t.providerID]
		roles := strings.Join(t.roles, ",")
		switch {
		case now != roleHealthOK && (!known || before == roleHealthOK):
			// A role provider just died — the user-visible failure may not
			// happen until the role is actually exercised, which is exactly
			// why this must be loud now (Error mirrors to the monitoring
			// chat via notify_slog).
			w.logger.Error("model role provider unhealthy",
				"provider", t.providerID, "model", t.model, "roles", roles,
				"verdict", now, "error", errs[t.providerID])
			w.emit(t, now, errs[t.providerID])
		case now == roleHealthOK && known && before != roleHealthOK:
			w.logger.Info("model role provider recovered",
				"provider", t.providerID, "model", t.model, "roles", roles)
			w.emit(t, now, "")
		}
	}
}

func (w *roleHealthWatch) emit(t roleHealthTarget, verdict, errText string) {
	if w.broadcast == nil {
		return
	}
	w.broadcast("model.role_health", map[string]any{
		"provider": t.providerID,
		"model":    t.model,
		"roles":    t.roles,
		"verdict":  verdict,
		"error":    errText,
	})
}

func (w *roleHealthWatch) loadState() {
	data, err := os.ReadFile(w.statePath)
	if err != nil {
		return // first run / unreadable: zero state probes after boot delay
	}
	var st roleHealthState
	if json.Unmarshal(data, &st) == nil {
		w.state = st
	}
}

func (w *roleHealthWatch) saveState() {
	w.mu.Lock()
	data, err := json.Marshal(w.state)
	w.mu.Unlock()
	if err != nil {
		return
	}
	if err := os.WriteFile(w.statePath, data, 0o600); err != nil {
		w.logger.Warn("role health state persist failed", "path", w.statePath, "error", err)
	}
}
