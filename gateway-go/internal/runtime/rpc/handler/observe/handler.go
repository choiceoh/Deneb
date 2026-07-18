// Package observe (handler) exposes Deneb's unified observation plane over RPC.
//
// It is one of the three thin adapters over the runtime/observe core (the other
// two being the internal chat tool and the native dashboard). Four methods, one
// JSON schema, all read-only:
//
//   - observe.turn     — join one run's agentlog turn-shape with its captured
//     slog lines into a single TurnView (the "what happened on run X" answer)
//   - observe.logs     — query the in-memory log ring by runId/session/level/…
//   - observe.behavior — cross-session behavior aggregate (tool usage, proactive
//     funnel, background-job health) from agentlog
//   - observe.health   — self-status of the observation plane itself
//
// The package name intentionally matches runtime/observe (same idiom as the
// insights handler): local symbols (Deps, Methods) are unqualified, and the
// `observe.` prefix refers to the imported core.
package observe

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/core/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// Deps carries the observation plane's two data sources.
//
// Capture (the slog ring) is created in server.New and exists from early
// registration onward. AgentLog is a getter, not a value, because the
// *agentlog.Writer is only constructed later in registerSessionRPCMethods — so
// the handler resolves it lazily at call time rather than capturing a nil.
type Deps struct {
	Capture  *observe.LogCapture
	AgentLog func() *agentlog.Writer
	Logger   *slog.Logger

	// VllmBases lazily lists the deduped base URLs of OpenAI-mode vLLM roles
	// (lazy for the same reason as AgentLog: the model registry is built
	// after early registration). observe.health scrapes each endpoint's
	// /metrics for the engine-level prefix-cache hit rate; nil or an empty
	// list simply omits the field.
	VllmBases func() []string

	// StateDir resolves the deneb state dir — observe.workstation_usage reads
	// the workstation-tool tally (utility-grounding ledger) from its cache.
	StateDir func() string
}

func (d Deps) ring() *observe.Ring {
	if d.Capture == nil {
		return nil
	}
	return d.Capture.Ring()
}

func (d Deps) alog() *agentlog.Writer {
	if d.AgentLog == nil {
		return nil
	}
	return d.AgentLog()
}

// Methods returns the in-process observe.* handler map — reachable by the chat
// tool, cron, and other in-process callers, but NOT over HTTP (handleMiniappRPC
// confines remote callers to the miniapp.* namespace).
func Methods(deps Deps) map[string]rpcutil.HandlerFunc {
	return methodsWithPrefix(deps, "observe.")
}

// MiniappMethods returns the same four handlers under the miniapp.observe.*
// prefix so remote adapters (the native dashboard, the external CLI holding a
// client token) can reach the observation plane. Because the wire surface is
// gated to miniapp.*, this is the only path in — and it inherits client-token
// auth for free, so the broader RPC surface stays closed.
func MiniappMethods(deps Deps) map[string]rpcutil.HandlerFunc {
	return methodsWithPrefix(deps, "miniapp.observe.")
}

func methodsWithPrefix(deps Deps, p string) map[string]rpcutil.HandlerFunc {
	return map[string]rpcutil.HandlerFunc{
		p + "turn":                 turnHandler(deps),
		p + "logs":                 logsHandler(deps),
		p + "behavior":             behaviorHandler(deps),
		p + "health":               healthHandler(deps),
		p + "workstation_usage":    workstationUsageHandler(deps),
		p + "workstation_feedback": workstationFeedbackHandler(deps),
	}
}

// feedbackMu serializes read-modify-write of the feedback tally file (the
// desktop reports verdicts fire-and-forget; two overlapping reports must not
// lose a count).
var feedbackMu sync.Mutex

// workstationFeedbackKept mirrors the on-disk per-action tally.
type workstationFeedbackKept struct {
	Kept    int `json:"kept"`
	Dropped int `json:"dropped"`
}

// workstationFeedbackHandler records the desktop's adoption verdict for a
// dispatched workspace command: kept = the arranged pane survived the first
// two minutes; dropped = the user closed/replaced it right away. This is the
// utility-grounding signal the raw dispatch tally can't provide — the
// workstation tool reads it back to tune how eagerly it drives the screen.
func workstationFeedbackHandler(deps Deps) rpcutil.HandlerFunc {
	type params struct {
		Action string `json:"action"`
		Kept   bool   `json:"kept"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p params
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		action := strings.TrimSpace(p.Action)
		if action == "" {
			return rpcerr.Newf(protocol.ErrMissingParam, "workstation_feedback requires action").Response(req.ID)
		}
		if deps.StateDir == nil {
			return rpcutil.RespondOK(req.ID, map[string]bool{"recorded": false})
		}
		dir := deps.StateDir()
		if dir == "" {
			return rpcutil.RespondOK(req.ID, map[string]bool{"recorded": false})
		}
		path := filepath.Join(dir, "cache", "workstation_feedback.json")
		feedbackMu.Lock()
		defer feedbackMu.Unlock()
		tally := struct {
			ByAction map[string]workstationFeedbackKept `json:"byAction"`
			LastAt   string                             `json:"lastAt"`
		}{ByAction: map[string]workstationFeedbackKept{}}
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &tally)
			if tally.ByAction == nil {
				tally.ByAction = map[string]workstationFeedbackKept{}
			}
		}
		entry := tally.ByAction[action]
		if p.Kept {
			entry.Kept++
		} else {
			entry.Dropped++
		}
		tally.ByAction[action] = entry
		tally.LastAt = time.Now().UTC().Format(time.RFC3339)
		data, err := json.Marshal(tally)
		if err != nil {
			return rpcerr.WrapUnavailable("feedback encode failed", err).Response(req.ID)
		}
		if err := atomicfile.WriteFile(path, data, nil); err != nil {
			return rpcerr.WrapUnavailable("feedback write failed", err).Response(req.ID)
		}
		return rpcutil.RespondOK(req.ID, map[string]bool{"recorded": true})
	}
}

// workstationUsageHandler surfaces the workstation-tool usage ledger (written
// by server_workstation.go on every dispatch) so the 관찰 pane can answer
// "화면 조종이 실제로 쓰이는가" without shelling into the state dir.
func workstationUsageHandler(deps Deps) rpcutil.HandlerFunc {
	type out struct {
		Total    int            `json:"total"`
		ByAction map[string]int `json:"byAction"`
		LastAt   string         `json:"lastAt"`
	}
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		usage := out{ByAction: map[string]int{}}
		if deps.StateDir != nil {
			if dir := deps.StateDir(); dir != "" {
				if data, err := os.ReadFile(filepath.Join(dir, "cache", "workstation_usage.json")); err == nil {
					_ = json.Unmarshal(data, &usage)
					if usage.ByAction == nil {
						usage.ByAction = map[string]int{}
					}
				}
			}
		}
		return rpcutil.RespondOK(req.ID, usage)
	}
}

func turnHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RunID string `json:"runId"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		if p.RunID == "" {
			return rpcerr.Newf(protocol.ErrMissingParam, "observe.turn requires runId").Response(req.ID)
		}
		view := observe.BuildTurnView(deps.alog(), deps.ring(), p.RunID)
		resp, _ := protocol.NewResponseOK(req.ID, view)
		return resp
	}
}

func logsHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			RunID    string `json:"runId"`
			Session  string `json:"session"`
			Level    string `json:"level"`   // debug|info|warn|error (default debug = all)
			Days     int    `json:"days"`    // window in days (convenience for sinceMs)
			SinceMs  int64  `json:"sinceMs"` // explicit window start; takes precedence
			Contains string `json:"contains"`
			Limit    int    `json:"limit"`
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		ring := deps.ring()
		if ring == nil {
			// Capture not wired (e.g. a logger-less test server). Be explicit so
			// a caller doesn't read an empty slice as "no matching logs".
			resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
				"lines": []observe.LogLine{}, "count": 0, "captureDisabled": true,
			})
			return resp
		}
		since := p.SinceMs
		if since == 0 && p.Days > 0 {
			since = time.Now().Add(-time.Duration(p.Days) * 24 * time.Hour).UnixMilli()
		}
		lines := ring.Query(observe.QueryOpts{
			RunID:    p.RunID,
			Session:  p.Session,
			MinLevel: observe.ParseLevel(p.Level),
			SinceMs:  since,
			Contains: p.Contains,
			Limit:    p.Limit,
		})
		resp, _ := protocol.NewResponseOK(req.ID, map[string]any{
			"lines": lines, "count": len(lines),
		})
		return resp
	}
}

func behaviorHandler(deps Deps) rpcutil.HandlerFunc {
	return func(_ context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		var p struct {
			Days    int   `json:"days"`    // window in days (convenience for sinceMs)
			SinceMs int64 `json:"sinceMs"` // explicit window start; takes precedence
		}
		if len(req.Params) > 0 {
			_ = json.Unmarshal(req.Params, &p)
		}
		alog := deps.alog()
		if alog == nil {
			resp, _ := protocol.NewResponseOK(req.ID, agentlog.AggregateResult{
				ProactiveDecisions: map[string]int{},
				BackgroundJobs:     map[string]int{},
				BackgroundErrors:   map[string]int{},
			})
			return resp
		}
		since := p.SinceMs
		if since == 0 && p.Days > 0 {
			since = time.Now().Add(-time.Duration(p.Days) * 24 * time.Hour).UnixMilli()
		}
		agg := alog.Aggregate(since)
		resp, _ := protocol.NewResponseOK(req.ID, agg)
		return resp
	}
}

// healthHandler reports the observation plane's own liveness — is capture wired,
// how full is the ring, how many recent errors, and a 24h behavior glance. This
// answers "is the thing I observe Deneb through actually working" before an
// agent trusts the other three methods. When an OpenAI-mode vLLM role is
// configured it also scrapes the engine's /metrics for the prefix-cache hit
// rate (cumulative since engine boot) — absent on non-vLLM deployments or
// when the engine is down.
func healthHandler(deps Deps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		out := map[string]any{
			"captureEnabled":  deps.ring() != nil,
			"agentLogEnabled": deps.alog() != nil,
		}
		if deps.VllmBases != nil {
			if stats := observe.FetchVllmPrefixCaches(ctx, deps.VllmBases()); len(stats) > 0 {
				out["vllmPrefixCache"] = stats
			}
		}
		if ring := deps.ring(); ring != nil {
			out["ringCapacity"] = ring.Cap()
			out["ringUsed"] = ring.Len()
			out["recentErrors"] = len(ring.Query(observe.QueryOpts{MinLevel: slog.LevelError, Limit: 1000}))
		}
		if alog := deps.alog(); alog != nil {
			since := time.Now().Add(-24 * time.Hour).UnixMilli()
			agg := alog.Aggregate(since)
			bgErr := 0
			for _, n := range agg.BackgroundErrors {
				bgErr += n
			}
			out["runs24h"] = agg.Runs
			out["proactiveRuns24h"] = agg.ProactiveRuns
			out["compactedRuns24h"] = agg.CompactedRuns
			out["backgroundErrors24h"] = bgErr
		}
		resp, _ := protocol.NewResponseOK(req.ID, out)
		return resp
	}
}
