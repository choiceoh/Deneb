package enginecontrol

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginelive"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// Follower moves roles (modelpicker.Controller.FollowEngine).
type Follower interface {
	FollowEngine(ctx context.Context, entries []Entry, served string) ([]Move, []Skip)
}

// Liveness is the engine liveness watcher as the follow reads it
// (*enginelive.Watcher; nil-safe).
type Liveness interface {
	State(endpoint string) (enginelive.EngineState, bool)
}

// FollowTask moves the roles on the local engine to the model its production
// serves, every 30 s. The fleet switches models (st_production.py, chosen from
// the engine screen or by hand); without this every local role would keep
// asking for the old model, the router would fail each request over, and a
// switch of the engine would silently hand every local role to the fallback
// chain.
//
// It follows what PRODUCTION serves, read off the engine's own door: the root
// status names the model and the fleet lease owner, and only an owner of kind
// production ("production/<host>/<pid>" — the supervisor's and deploy-watch's
// boots) counts. A campaign's window — a ticket or a session booting another
// model for an hour — moves nothing, and neither does an engine that is down.
type FollowTask struct {
	Endpoint string // the engine's /metrics URL
	Picker   Follower
	Entries  func() []Entry // the router's entries at the engine, re-read each pass
	Liveness Liveness
	Log      *FollowLog
	Logger   *slog.Logger
	// Fleet reads the door's root status; nil is observe.FetchEngineFleet.
	Fleet func(ctx context.Context, metricsURL string) (observe.EngineFleet, bool)

	held map[string]bool // role|reason already warned about, so a held-back role is said once
}

func (t *FollowTask) Name() string            { return "engine-follow" }
func (t *FollowTask) Interval() time.Duration { return 30 * time.Second }

// Run makes one pass.
func (t *FollowTask) Run(ctx context.Context) error {
	if t.Liveness != nil {
		if st, ok := t.Liveness.State(t.Endpoint); ok && st.Down {
			return nil // nothing serves; the fallback chain already carries the roles
		}
	}
	fetch := t.Fleet
	if fetch == nil {
		fetch = observe.FetchEngineFleet
	}
	fleet, ok := fetch(ctx, t.Endpoint)
	if !ok || fleet.Model == "" || !strings.HasPrefix(fleet.Owner, "production/") {
		return nil
	}
	var entries []Entry
	if t.Entries != nil {
		entries = t.Entries()
	}
	moves, skips := t.Picker.FollowEngine(ctx, entries, fleet.Model)
	t.Log.Record(FollowReport{At: time.Now(), Served: fleet.Model, Moves: moves, Skips: skips})
	logger := t.Logger
	if logger == nil {
		logger = slog.Default()
	}
	for _, m := range moves {
		logger.Info("engine serves another model: a local role follows it",
			"role", m.Role, "from", m.From, "to", m.To, "served", fleet.Model, "owner", fleet.Owner)
	}
	held := make(map[string]bool, len(skips))
	for _, s := range skips {
		key := s.Role + "|" + s.Reason
		held[key] = true
		if !t.held[key] {
			logger.Warn("engine serves another model: a local role cannot follow it",
				"role", s.Role, "from", s.From, "reason", s.Reason, "served", fleet.Model)
		}
	}
	t.held = held
	return nil
}
