package server

// dashboard_sources.go — adapters that feed the miniapp.dashboard.* handler.
//
// The dashboard groups work items by the operator's managed parts (레인). Its
// handler (handlerminiapp/dashboard.go) takes narrow data-source interfaces; this
// file builds the production implementations from the same stores the calendar
// and work-feed RPCs use, plus the classifier ruleset loader.

import (
	"context"
	"sort"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
)

// dashboardCalendarSource adapts the hybrid calendar (read-only Google client +
// local store) into the single merged read the dashboard needs. It mirrors the
// calendar handler's listMerged: Google events (when the client is configured
// and healthy) unioned with local events, sorted by start. Either side may be
// absent — a nil client or a Google error degrades to local-only so the
// dashboard's calendar lane keeps working without OAuth.
type dashboardCalendarSource struct {
	client func() (handlerminiapp.CalendarClient, error)
	local  handlerminiapp.LocalCalendar
}

// ListRange returns events in [from, to), Google ∪ local, sorted by start and
// capped at limit. Best-effort: a Google factory/list error is swallowed when a
// local store can still answer (the dashboard prefers a partial calendar over an
// errored one); only with no source at all does it return empty.
func (d dashboardCalendarSource) ListRange(ctx context.Context, from, to time.Time, limit int) ([]calendar.Event, error) {
	var merged []calendar.Event
	if d.client != nil {
		if client, err := d.client(); err == nil {
			if events, err := client.ListUpcoming(ctx, from, to, limit); err == nil {
				merged = append(merged, events...)
			}
		}
	}
	if d.local != nil {
		merged = append(merged, d.local.ListRange(from, to)...)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].Start.Before(merged[j].Start) })
	if limit > 0 && len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}

// dashboardDeps assembles the production DashboardDeps. Sources are nil-tolerant:
// a nil work-feed store or calendar simply drops that lane's contributions.
//
// Rules + Lanes both derive from the operator's org chart when present (the
// chart is the master): org.LoadRules derives classification rules from the
// chart's lane-tagged nodes, and org.LoadLanes derives the dashboard column set
// from the same nodes. When no org.json exists (or it defines no parts), both
// fall back to the legacy classification path — org.LoadRules → the operator's
// {stateDir}/classification_rules.json (or keyword defaults), and org.LoadLanes
// → nil so the handler uses its hardcoded part set. Both are always non-nil so
// the dashboard always registers and always renders a part skeleton.
func (s *Server) dashboardDeps() handlerminiapp.DashboardDeps {
	var wf handlerminiapp.DashboardWorkFeedSource
	if nwf := s.nativeWorkFeedStore(); nwf != nil {
		wf = nwf
	}
	return handlerminiapp.DashboardDeps{
		Rules: func() (classification.Rules, error) { return org.LoadRules() },
		Lanes: func() ([]org.LaneDef, error) { return org.LoadLanes() },
		Calendar: dashboardCalendarSource{
			client: func() (handlerminiapp.CalendarClient, error) { return calendar.DefaultClient() },
			local:  resolveLocalCalendar(s.logger),
		},
		WorkFeed: wf,
	}
}

// orgDeps assembles the production OrgDeps for the miniapp.org.* editor. Load
// reads the operator's {stateDir}/org.json (missing → empty tree); SavePath
// resolves that same path for the atomic write. Always non-nil so the editor
// registers unconditionally (a fresh install opens to a blank chart).
func (s *Server) orgDeps() handlerminiapp.OrgDeps {
	return handlerminiapp.OrgDeps{
		Load:     func() (org.OrgTree, error) { return org.Load() },
		SavePath: org.ResolvePath,
	}
}
