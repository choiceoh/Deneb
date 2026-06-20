// dashboard.go — miniapp.dashboard.* RPC handlers.
//
//   miniapp.dashboard.lanes — work items grouped into the operator's managed
//                             parts (레인) for the "파트별 업무 현황" screen.
//
// The operator is a solar-group executive overseeing five parts (기획조정실
// 1/2/3팀, 남도에코, plus a 개인/기타 catch-all) and an extra 미분류 holding lane
// for items no signal could place. This handler loads work from each data
// source, runs every item through the rule-based classifier
// (internal/domain/classification), and returns them grouped by lane.
//
// Scope (1차): calendar (upcoming events) + work feed — the two most visible
// surfaces. Mail-analysis and to-dos are deliberately left as a seam: add a
// laneSource (below) and a projection without touching the grouping/response
// code. See the "// SEAM:" markers.
//
// Privacy: the classifier's person/거래처 roster is loaded from
// {stateDir}/classification_rules.json (operator data, never in the repo). This
// handler holds no names — it only projects rows into classification.Signals.

package handlerminiapp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcerr"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// RefType values name the origin kind of a dashboard item so the native client
// can route a tap back to the right detail screen (calendar event, work-feed
// card, …). Stable strings — the client switches on them.
const (
	dashboardRefCalendar = "calendar"
	dashboardRefWorkFeed = "workfeed"
)

const (
	// dashboardCalendarHours is how far ahead the calendar source looks. The
	// dashboard is a "what's on each part's plate now" view, so a 2-week horizon
	// keeps it current without pulling the whole year.
	dashboardCalendarHours = 24 * 14
	// dashboardCalendarLimit caps calendar rows pulled before grouping.
	dashboardCalendarLimit = 250
	// dashboardWorkFeedLimit caps work-feed rows pulled before grouping.
	dashboardWorkFeedLimit = 100
)

// ClassifierRulesLoader resolves the current classification ruleset. Injected
// (not called directly) so tests supply fixed fake rules and production wires
// org.LoadRules (org chart → derived rules, else the legacy classification
// JSON). Returning an error degrades the dashboard to keyword-only defaults
// rather than failing the call.
type ClassifierRulesLoader func() (classification.Rules, error)

// DashboardLanesLoader resolves the dashboard's lane definitions (column set +
// order). Production wires org.LoadLanes: when the operator's org chart defines
// parts, the columns come from the chart (the chart is master); otherwise it
// returns nil and the dashboard falls back to the legacy hardcoded part set
// (classification.AllLanes). nil loader or a nil/empty/errored result → legacy
// lanes, so the dashboard always renders a part skeleton.
type DashboardLanesLoader func() ([]org.LaneDef, error)

// DashboardCalendarSource yields calendar events in [from, to). Mirrors the
// CalendarClient/LocalCalendar split the calendar handler uses, but the
// dashboard only needs a single merged read, so it's one method. nil disables
// the calendar source (the dashboard still renders the other sources).
type DashboardCalendarSource interface {
	ListRange(ctx context.Context, from, to time.Time, limit int) ([]calendar.Event, error)
}

// DashboardWorkFeedSource yields recent unacked work-feed items. A subset of the
// workfeed store. nil disables the work-feed source.
type DashboardWorkFeedSource interface {
	List(limit int, includeAcked bool) ([]workfeed.Item, int, error)
}

// DashboardDeps wires the dashboard's data sources and the classifier. Every
// source is optional (nil = that source contributes nothing); Rules is required
// for the handler to register (without a ruleset there is nothing to classify
// by). The handler degrades per-source so a down Google calendar or empty feed
// never fails the whole dashboard.
//
// SEAM: to add a new source (mail analysis, to-dos), add a field here + a
// laneSource in collectItems — the grouping/response code is source-agnostic.
type DashboardDeps struct {
	Rules    ClassifierRulesLoader
	Lanes    DashboardLanesLoader // optional; nil → legacy hardcoded lanes
	Calendar DashboardCalendarSource
	WorkFeed DashboardWorkFeedSource
}

// Wire shapes for the native client. Marked //deneb:wire so the Kotlin types are
// generated from these (one source of truth, no hand-mirrored drift).

// DashboardItem is one classified work item in a lane. RefType + RefID let the
// client open the underlying object (a calendar event, a work-feed card);
// WhenMs is the item's salient time (event start, card creation) in epoch millis
// — 0 when the source has no meaningful time.
//
//deneb:wire
type DashboardItem struct {
	Title    string `json:"title"`
	Subtitle string `json:"subtitle,omitempty"`
	Source   string `json:"source"`
	RefType  string `json:"refType,omitempty"`
	RefID    string `json:"refId,omitempty"`
	WhenMs   int64  `json:"whenMs,omitempty"`
}

// LaneOut is one part's bucket: a stable key, its Korean display name, and the
// items classified into it. Items are sorted soonest-first within a lane.
//
//deneb:wire
type LaneOut struct {
	Key   string          `json:"key"`
	Name  string          `json:"name"`
	Items []DashboardItem `json:"items"`
}

// DashboardOut is the miniapp.dashboard.lanes response: every real part lane in
// fixed order, then the 미분류 holding lane last. Empty lanes are included (so the
// client renders all five parts) — only the trailing 미분류 lane is omitted when
// it has no items.
//
//deneb:wire
type DashboardOut struct {
	Lanes []LaneOut `json:"lanes"`
}

// DashboardMethods returns the miniapp.dashboard.* handler map. Requires a Rules
// loader (the classifier's data); with no data sources at all it still registers
// but every call returns empty lanes. Returns nil only when Rules is unset, so
// method_registry.go can skip registration cleanly in that (test-only) case.
func DashboardMethods(deps DashboardDeps) map[string]rpcutil.HandlerFunc {
	if deps.Rules == nil {
		return nil
	}
	return map[string]rpcutil.HandlerFunc{
		"miniapp.dashboard.lanes": dashboardLanes(deps),
	}
}

func dashboardLanes(deps DashboardDeps) rpcutil.HandlerFunc {
	return func(ctx context.Context, req *protocol.RequestFrame) *protocol.ResponseFrame {
		if errResp := requireAuth(ctx, req.ID); errResp != nil {
			return errResp
		}
		// Tolerate (and ignore) any params for forward-compat — the dashboard
		// currently takes none, but accepting a body keeps the client free to
		// send filters later without a 400.
		if len(req.Params) > 0 {
			var ignore map[string]any
			if err := json.Unmarshal(req.Params, &ignore); err != nil {
				return rpcerr.InvalidParams(err).Response(req.ID)
			}
		}

		// Resolve the ruleset. A load error degrades to keyword-only defaults
		// (better a partial dashboard than none); the classifier with default
		// rules still buckets by domain keyword.
		rules, err := deps.Rules()
		if err != nil {
			rules = classification.DefaultRules()
		}

		// Resolve the column set: org chart lanes when defined, else the legacy
		// hardcoded part set. An error/empty result falls back to legacy too, so
		// the dashboard always shows a part skeleton.
		lanes := resolveLanes(deps)

		items := collectItems(ctx, deps, rules)
		out := groupByLane(items, lanes)
		return rpcutil.RespondOK(req.ID, out)
	}
}

// classifiedItem pairs a projected DashboardItem with the lane it was assigned,
// so collectItems (which knows each source) and groupByLane (which is source-
// agnostic) stay decoupled.
type classifiedItem struct {
	lane classification.Lane
	item DashboardItem
}

// collectItems pulls every data source, projects each row into
// (classification.Signals → DashboardItem), classifies it, and returns the flat
// list. Each source is best-effort: an error or nil source contributes nothing
// and never aborts the others.
//
// SEAM: new sources slot in here as additional best-effort blocks. The signal
// projection (what People/Companies/Text a source supplies) is the only source-
// specific logic; everything downstream is generic.
func collectItems(ctx context.Context, deps DashboardDeps, rules classification.Rules) []classifiedItem {
	var out []classifiedItem

	// --- Calendar source -------------------------------------------------
	if deps.Calendar != nil {
		now := time.Now()
		events, err := deps.Calendar.ListRange(ctx, now, now.Add(dashboardCalendarHours*time.Hour), dashboardCalendarLimit)
		if err == nil {
			for _, ev := range events {
				sig, item := projectCalendarEvent(ev)
				lane, _ := rules.Classify(sig)
				out = append(out, classifiedItem{lane: lane, item: item})
			}
		}
	}

	// --- Work-feed source ------------------------------------------------
	if deps.WorkFeed != nil {
		feed, _, err := deps.WorkFeed.List(dashboardWorkFeedLimit, false)
		if err == nil {
			for _, it := range feed {
				sig, item := projectWorkFeedItem(it)
				lane, _ := rules.Classify(sig)
				out = append(out, classifiedItem{lane: lane, item: item})
			}
		}
	}

	// SEAM: mail-analysis and to-do sources project here the same way.

	return out
}

// projectCalendarEvent maps a calendar event to its classification signals and
// its dashboard row. People = attendees + organizer display names (the strong
// signal); Companies = the organizer's org-bearing parenthetical if any (best-
// effort — calendar has no structured org field, so we lean on attendee names);
// Text = summary + description for the keyword pass.
func projectCalendarEvent(ev calendar.Event) (classification.Signals, DashboardItem) {
	var people []string
	if n := attendeeName(ev.Organizer); n != "" {
		people = append(people, n)
	}
	for _, a := range ev.Attendees {
		if a.Self {
			continue // the operator themself is not a part signal
		}
		if n := attendeeName(a); n != "" {
			people = append(people, n)
		}
	}
	sig := classification.Signals{
		People: people,
		Text:   strings.TrimSpace(ev.Summary + " " + ev.Description),
	}
	item := DashboardItem{
		Title:    firstNonEmpty(ev.Summary, "(제목 없음)"),
		Subtitle: calendarSubtitle(ev),
		Source:   "calendar",
		RefType:  dashboardRefCalendar,
		RefID:    ev.ID,
		WhenMs:   timeToMillis(ev.Start),
	}
	return sig, item
}

// projectWorkFeedItem maps a work-feed card to signals + a dashboard row. A card
// has no attendee list, so the title/summary/body carry the only signal — fed to
// the keyword (and, if a name appears, person) pass via Text. RefID is the card
// id so the client can open it.
func projectWorkFeedItem(it workfeed.Item) (classification.Signals, DashboardItem) {
	text := strings.TrimSpace(strings.Join([]string{it.Title, it.Summary, it.Body}, " "))
	sig := classification.Signals{Text: text}
	item := DashboardItem{
		Title:    firstNonEmpty(it.Title, "업무 항목"),
		Subtitle: workFeedSubtitle(it),
		Source:   "workfeed",
		RefType:  dashboardRefWorkFeed,
		RefID:    it.ID,
		WhenMs:   it.CreatedAtMs,
	}
	return sig, item
}

// resolveLanes resolves the dashboard's column set. When the org chart defines
// parts (deps.Lanes returns them), those drive the columns — the chart is the
// master. Otherwise (nil loader, error, or no lane nodes) it falls back to the
// legacy hardcoded part set so every prior deployment renders unchanged.
func resolveLanes(deps DashboardDeps) []org.LaneDef {
	if deps.Lanes != nil {
		if defs, err := deps.Lanes(); err == nil && len(defs) > 0 {
			return defs
		}
	}
	// Legacy fallback: the fixed classification lanes with their Korean labels.
	defs := make([]org.LaneDef, 0, len(classification.AllLanes))
	for _, lane := range classification.AllLanes {
		defs = append(defs, org.LaneDef{Key: string(lane), Name: classification.DisplayName(lane)})
	}
	return defs
}

// groupByLane buckets classified items into the given lane order, then appends
// the 미분류 holding lane only if it has items. Every defined part lane is always
// present (even empty) so the client renders the full part skeleton. Items
// within a lane are sorted soonest-first (WhenMs ascending; 0/no-time sinks to
// the bottom). The lane set comes from resolveLanes (org chart or legacy).
func groupByLane(items []classifiedItem, lanes []org.LaneDef) DashboardOut {
	byLane := make(map[classification.Lane][]DashboardItem)
	for _, ci := range items {
		byLane[ci.lane] = append(byLane[ci.lane], ci.item)
	}

	out := DashboardOut{}
	for _, def := range lanes {
		lane := classification.Lane(def.Key)
		out.Lanes = append(out.Lanes, LaneOut{
			Key:   def.Key,
			Name:  laneDisplayName(def),
			Items: sortItems(byLane[lane]),
		})
	}
	// 미분류 last, and only when non-empty — it's a triage bucket, not a part, so
	// don't show an empty one. (Chart-defined lanes never use this reserved key,
	// so a derived part can't collide with it.)
	if unclassified := byLane[classification.LaneUnclassified]; len(unclassified) > 0 {
		out.Lanes = append(out.Lanes, LaneOut{
			Key:   string(classification.LaneUnclassified),
			Name:  classification.DisplayName(classification.LaneUnclassified),
			Items: sortItems(unclassified),
		})
	}
	return out
}

// laneDisplayName returns a lane's column title: its defined Name, falling back
// to the key if the chart left a lane node unnamed (defensive — Validate already
// rejects empty names, so this only guards the legacy path's edge).
func laneDisplayName(def org.LaneDef) string {
	if n := strings.TrimSpace(def.Name); n != "" {
		return n
	}
	return def.Key
}

// sortItems orders a lane's items soonest-first. Items with a time (WhenMs > 0)
// sort ascending before timeless ones; ties break by title for stable output.
func sortItems(items []DashboardItem) []DashboardItem {
	sort.SliceStable(items, func(i, j int) bool {
		ti, tj := items[i].WhenMs, items[j].WhenMs
		switch {
		case ti > 0 && tj > 0:
			if ti != tj {
				return ti < tj
			}
		case ti > 0:
			return true // timed before untimed
		case tj > 0:
			return false
		}
		return items[i].Title < items[j].Title
	})
	return items
}

// --- small helpers ---------------------------------------------------------

// attendeeName prefers an attendee's display name, falling back to the local
// part of their email so an unnamed invitee can still match a person rule keyed
// on a username-like token.
func attendeeName(a calendar.Attendee) string {
	if n := strings.TrimSpace(a.DisplayName); n != "" {
		return n
	}
	if e := strings.TrimSpace(a.Email); e != "" {
		if at := strings.IndexByte(e, '@'); at > 0 {
			return e[:at]
		}
		return e
	}
	return ""
}

// calendarSubtitle renders a short, human time hint for an event row.
func calendarSubtitle(ev calendar.Event) string {
	if ev.Start.IsZero() {
		return "일정"
	}
	if ev.AllDay {
		return ev.Start.Format("1월 2일") + " 종일"
	}
	return ev.Start.Format("1월 2일 15:04")
}

// workFeedSubtitle renders a short hint for a work-feed row from its summary,
// falling back to a generic label.
func workFeedSubtitle(it workfeed.Item) string {
	if s := strings.TrimSpace(it.Summary); s != "" {
		return workfeed.Preview(s, 60)
	}
	return "업무 피드"
}

func firstNonEmpty(s, fallback string) string {
	if t := strings.TrimSpace(s); t != "" {
		return t
	}
	return fallback
}

func timeToMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}
