package handlerminiapp

// FAKE data only — invented names/companies, never the real roster. The real
// roster lives in the operator's {stateDir}/classification_rules.json, not the
// repo. (Privacy invariant carried over from the classification package.)

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/classification"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// --- fakes ----------------------------------------------------------------

type fakeDashboardCalendar struct {
	events []calendar.Event
	err    error
}

func (f fakeDashboardCalendar) ListRange(_ context.Context, _, _ time.Time, _ int) ([]calendar.Event, error) {
	return f.events, f.err
}

type fakeDashboardFeed struct {
	items []workfeed.Item
	err   error
}

func (f fakeDashboardFeed) List(limit int, includeAcked bool) ([]workfeed.Item, int, error) {
	return f.items, len(f.items), f.err
}

// fakeRules is a fixed ruleset using invented names, supplied via the Rules
// loader so no production JSON is read in tests.
func fakeRulesLoader() ClassifierRulesLoader {
	return func() (classification.Rules, error) {
		return classification.Rules{
			PersonToLane: map[string]classification.Lane{
				"홍길동": classification.LaneTeam1,
				"이영희": classification.LaneTeam2,
			},
			CompanyToLane: map[string]classification.Lane{},
			KeywordToLane: map[string]classification.Lane{
				"케이블": classification.LaneNamdo,
			},
		}, nil
	}
}

// findLane returns the lane with the given key from a response, or nil.
func findLane(lanes []LaneOut, key string) *LaneOut {
	for i := range lanes {
		if lanes[i].Key == key {
			return &lanes[i]
		}
	}
	return nil
}

// --- tests ----------------------------------------------------------------

func TestDashboardMethods_NilRulesReturnsNil(t *testing.T) {
	// Without a Rules loader there is nothing to classify by; the domain should
	// not register at all.
	if got := DashboardMethods(DashboardDeps{}); got != nil {
		t.Fatalf("DashboardMethods(no rules) = %v, want nil", got)
	}
}

func TestDashboardMethods_RegistersWithRulesOnly(t *testing.T) {
	m := DashboardMethods(DashboardDeps{Rules: fakeRulesLoader()})
	if _, ok := m["miniapp.dashboard.lanes"]; !ok {
		t.Fatalf("miniapp.dashboard.lanes not registered with rules-only deps")
	}
}

func TestDashboardLanes_RequiresAuth(t *testing.T) {
	h := dashboardLanes(DashboardDeps{Rules: fakeRulesLoader()})
	resp := h(context.Background(), reqWith(t, "miniapp.dashboard.lanes", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized without client identity")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestDashboardLanes_GroupsByPartFromAllSources(t *testing.T) {
	now := time.Now()
	deps := DashboardDeps{
		Rules: fakeRulesLoader(),
		Calendar: fakeDashboardCalendar{events: []calendar.Event{
			// Organizer 홍길동 → team1 (person, strong).
			{
				ID: "ev1", Summary: "1팀 협의", Start: now.Add(2 * time.Hour),
				Organizer: calendar.Attendee{DisplayName: "홍길동 부장"},
			},
			// Attendee 이영희 → team2.
			{
				ID: "ev2", Summary: "지붕 검토", Start: now.Add(time.Hour),
				Attendees: []calendar.Attendee{{DisplayName: "이영희"}},
			},
			// No known person/keyword → unclassified.
			{ID: "ev3", Summary: "일반 회의", Start: now.Add(3 * time.Hour)},
		}},
		WorkFeed: fakeDashboardFeed{items: []workfeed.Item{
			// Body mentions 케이블 → namdo (keyword, weak).
			{ID: "wf1", Title: "케이블 포설 일정", CreatedAtMs: now.UnixMilli()},
		}},
	}
	h := dashboardLanes(deps)
	resp := h(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))

	var got DashboardOut
	decode(t, resp, &got)

	// All five real parts present (even empty ones), in fixed order.
	wantOrder := []string{"team1", "team2", "team3", "namdo", "personal"}
	for i, key := range wantOrder {
		if i >= len(got.Lanes) {
			t.Fatalf("lanes shorter than expected: %d lanes, want >= %d", len(got.Lanes), len(wantOrder))
		}
		if got.Lanes[i].Key != key {
			t.Fatalf("lane[%d].Key = %q, want %q", i, got.Lanes[i].Key, key)
		}
	}

	// team1 has the organizer-matched event.
	if l := findLane(got.Lanes, "team1"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "ev1" {
		t.Fatalf("team1 = %+v, want [ev1]", l)
	}
	// team2 has the attendee-matched event.
	if l := findLane(got.Lanes, "team2"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "ev2" {
		t.Fatalf("team2 = %+v, want [ev2]", l)
	}
	// namdo has the keyword-matched work-feed card with correct RefType.
	if l := findLane(got.Lanes, "namdo"); l == nil || len(l.Items) != 1 ||
		l.Items[0].RefID != "wf1" || l.Items[0].RefType != dashboardRefWorkFeed {
		t.Fatalf("namdo = %+v, want [wf1/workfeed]", l)
	}
	// Display names are the Korean labels.
	if l := findLane(got.Lanes, "team1"); l.Name != "기획조정실 1팀" {
		t.Fatalf("team1 name = %q, want 기획조정실 1팀", l.Name)
	}
	// 미분류 lane present and last (it has ev3).
	last := got.Lanes[len(got.Lanes)-1]
	if last.Key != string(classification.LaneUnclassified) {
		t.Fatalf("last lane = %q, want unclassified", last.Key)
	}
	if len(last.Items) != 1 || last.Items[0].RefID != "ev3" {
		t.Fatalf("unclassified = %+v, want [ev3]", last.Items)
	}
}

func TestDashboardLanes_UnclassifiedOmittedWhenEmpty(t *testing.T) {
	now := time.Now()
	deps := DashboardDeps{
		Rules: fakeRulesLoader(),
		Calendar: fakeDashboardCalendar{events: []calendar.Event{
			{ID: "ev1", Summary: "x", Start: now, Organizer: calendar.Attendee{DisplayName: "홍길동"}},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)
	// Exactly the five real lanes — no trailing 미분류 (everything classified).
	if len(got.Lanes) != len(classification.AllLanes) {
		t.Fatalf("lanes = %d, want %d (no empty unclassified)", len(got.Lanes), len(classification.AllLanes))
	}
	if findLane(got.Lanes, string(classification.LaneUnclassified)) != nil {
		t.Fatalf("empty unclassified lane should be omitted")
	}
}

func TestDashboardLanes_ItemsSortedSoonestFirst(t *testing.T) {
	now := time.Now()
	// Three team1 events out of order; expect ascending WhenMs within the lane.
	deps := DashboardDeps{
		Rules: fakeRulesLoader(),
		Calendar: fakeDashboardCalendar{events: []calendar.Event{
			{ID: "late", Summary: "c", Start: now.Add(5 * time.Hour), Organizer: calendar.Attendee{DisplayName: "홍길동"}},
			{ID: "soon", Summary: "a", Start: now.Add(time.Hour), Organizer: calendar.Attendee{DisplayName: "홍길동"}},
			{ID: "mid", Summary: "b", Start: now.Add(3 * time.Hour), Organizer: calendar.Attendee{DisplayName: "홍길동"}},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)
	l := findLane(got.Lanes, "team1")
	if l == nil || len(l.Items) != 3 {
		t.Fatalf("team1 = %+v, want 3 items", l)
	}
	order := []string{l.Items[0].RefID, l.Items[1].RefID, l.Items[2].RefID}
	want := []string{"soon", "mid", "late"}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("sort order = %v, want %v", order, want)
		}
	}
}

func TestDashboardLanes_SourceErrorDegradesNotFails(t *testing.T) {
	now := time.Now()
	// Calendar errors, but the work feed still classifies — the call must succeed
	// with the feed's contribution rather than failing the whole dashboard.
	deps := DashboardDeps{
		Rules:    fakeRulesLoader(),
		Calendar: fakeDashboardCalendar{err: errors.New("google down")},
		WorkFeed: fakeDashboardFeed{items: []workfeed.Item{
			{ID: "wf1", Title: "케이블 작업", CreatedAtMs: now.UnixMilli()},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	if !resp.OK {
		t.Fatalf("expected OK despite calendar error, got code=%s", resp.Error.Code)
	}
	var got DashboardOut
	decode(t, resp, &got)
	if l := findLane(got.Lanes, "namdo"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "wf1" {
		t.Fatalf("namdo = %+v, want [wf1] from feed", l)
	}
}

func TestDashboardLanes_RulesLoaderErrorFallsBackToDefaults(t *testing.T) {
	now := time.Now()
	// Rules loader fails → handler uses classification.DefaultRules() (keyword
	// defaults). The default 인허가 keyword routes to team1.
	deps := DashboardDeps{
		Rules: func() (classification.Rules, error) {
			return classification.Rules{}, errors.New("rules file unreadable")
		},
		WorkFeed: fakeDashboardFeed{items: []workfeed.Item{
			{ID: "wf1", Title: "인허가 신청 건", CreatedAtMs: now.UnixMilli()},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	if !resp.OK {
		t.Fatalf("expected OK with default rules, got code=%s", resp.Error.Code)
	}
	var got DashboardOut
	decode(t, resp, &got)
	if l := findLane(got.Lanes, "team1"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "wf1" {
		t.Fatalf("team1 = %+v, want [wf1] via default keyword 인허가", l)
	}
}

func TestDashboardLanes_OrgChartLanesDriveColumns(t *testing.T) {
	// When a Lanes loader yields org-chart lanes, the dashboard renders THOSE
	// columns (chart order + names + custom keys), not the legacy hardcoded set.
	now := time.Now()
	deps := DashboardDeps{
		// Rules route a custom lane key "sales" that the legacy set never had.
		Rules: func() (classification.Rules, error) {
			return classification.Rules{
				KeywordToLane: map[string]classification.Lane{"제안": classification.Lane("sales")},
			}, nil
		},
		// Chart defines two parts with custom keys/names, in this order.
		Lanes: func() ([]org.LaneDef, error) {
			return []org.LaneDef{
				{Key: "sales", Name: "영업본부"},
				{Key: "ops", Name: "운영팀"},
			}, nil
		},
		WorkFeed: fakeDashboardFeed{items: []workfeed.Item{
			{ID: "wf1", Title: "제안서 작성", CreatedAtMs: now.UnixMilli()},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)

	// Exactly the chart's two lanes, in chart order, with chart names.
	if len(got.Lanes) != 2 {
		t.Fatalf("lanes = %d, want 2 chart lanes", len(got.Lanes))
	}
	if got.Lanes[0].Key != "sales" || got.Lanes[0].Name != "영업본부" {
		t.Fatalf("lane[0] = %+v, want sales/영업본부", got.Lanes[0])
	}
	if got.Lanes[1].Key != "ops" || got.Lanes[1].Name != "운영팀" {
		t.Fatalf("lane[1] = %+v, want ops/운영팀", got.Lanes[1])
	}
	// The custom-lane keyword routed the card into the chart lane.
	if l := findLane(got.Lanes, "sales"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "wf1" {
		t.Fatalf("sales = %+v, want [wf1]", l)
	}
	// No legacy lane (team1/…) leaked in.
	if findLane(got.Lanes, "team1") != nil {
		t.Fatal("legacy hardcoded lane leaked despite org chart lanes")
	}
}

// TestDashboardLanes_ProductionPath_NoSilentDropWithNodeIDLanes drives the REAL
// production wiring (org.LoadRules → DeriveRules, org.LoadLanes → DeriveLanes)
// off an on-disk chart whose lane keys are node ids (n<ms>), exactly as the
// native editor writes them. A work item whose text matches a generic domain
// keyword (인허가) must NOT vanish: with the HIGH fix, DeriveRules no longer seeds
// the constant-lane keyword defaults, so the item is unclassified and surfaces
// in 미분류 instead of being routed to a non-existent "team1" column and dropped.
//
// This exercises the path the unit tests above bypass by injecting Rules/Lanes
// by hand — the original regression lived precisely in that gap.
func TestDashboardLanes_ProductionPath_NoSilentDropWithNodeIDLanes(t *testing.T) {
	// A minimal chart with ONE lane node keyed by a node id (like the editor).
	// It enumerates no keywords, so the generic 인허가 term matches nothing here.
	chart := map[string]any{
		"nodes": []map[string]any{
			{"id": "n100", "name": "영업본부", "type": "team", "lane": "n100"},
		},
	}
	orgPath := filepath.Join(t.TempDir(), "org.json")
	data, err := json.Marshal(chart)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(orgPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DENEB_ORG_FILE", orgPath)

	now := time.Now()
	deps := DashboardDeps{
		// REAL production loaders — same closures as dashboard_sources.go.
		Rules: func() (classification.Rules, error) { return org.LoadRules() },
		Lanes: func() ([]org.LaneDef, error) { return org.LoadLanes() },
		WorkFeed: fakeDashboardFeed{items: []workfeed.Item{
			// 인허가 is a default keyword (would route to constant lane team1).
			{ID: "wf1", Title: "인허가 신청 건", CreatedAtMs: now.UnixMilli()},
		}},
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)

	// The chart's single node-id column is present.
	if findLane(got.Lanes, "n100") == nil {
		t.Fatalf("chart lane n100 missing; lanes=%+v", got.Lanes)
	}
	// No constant legacy lane leaked (chart is master).
	if findLane(got.Lanes, "team1") != nil {
		t.Fatal("constant lane team1 leaked despite node-id chart lanes")
	}
	// THE INVARIANT: the item is not lost. It surfaces in 미분류.
	un := findLane(got.Lanes, string(classification.LaneUnclassified))
	if un == nil || len(un.Items) != 1 || un.Items[0].RefID != "wf1" {
		t.Fatalf("item silently dropped: unclassified=%+v, want [wf1]", un)
	}
}

// TestGroupByLane_AbsorbsOrphanedBucket is the unit-level guarantee for the
// groupByLane safety net: an item classified to a lane that is in neither the
// column set nor the reserved key is folded into 미분류, never dropped.
func TestGroupByLane_AbsorbsOrphanedBucket(t *testing.T) {
	items := []classifiedItem{
		// Lane "ghost" matches no column below.
		{lane: classification.Lane("ghost"), item: DashboardItem{Title: "고아", RefID: "x1"}},
		// A normal item in a real column.
		{lane: classification.Lane("real"), item: DashboardItem{Title: "정상", RefID: "x2"}},
	}
	out := groupByLane(items, []org.LaneDef{{Key: "real", Name: "실재"}})

	if l := findLane(out.Lanes, "real"); l == nil || len(l.Items) != 1 || l.Items[0].RefID != "x2" {
		t.Fatalf("real lane = %+v, want [x2]", l)
	}
	// The orphaned item is rescued into 미분류 rather than vanishing.
	un := findLane(out.Lanes, string(classification.LaneUnclassified))
	if un == nil || len(un.Items) != 1 || un.Items[0].RefID != "x1" {
		t.Fatalf("orphaned bucket not absorbed: unclassified=%+v, want [x1]", un)
	}
}

func TestDashboardLanes_LanesLoaderEmptyFallsBackToLegacy(t *testing.T) {
	// A Lanes loader that returns nil/empty (no chart parts) → legacy hardcoded
	// lanes, preserving prior behavior.
	deps := DashboardDeps{
		Rules: fakeRulesLoader(),
		Lanes: func() ([]org.LaneDef, error) { return nil, nil },
	}
	resp := dashboardLanes(deps)(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)
	if len(got.Lanes) != len(classification.AllLanes) {
		t.Fatalf("lanes = %d, want %d legacy lanes", len(got.Lanes), len(classification.AllLanes))
	}
	if got.Lanes[0].Key != string(classification.LaneTeam1) {
		t.Fatalf("lane[0] = %q, want team1 (legacy)", got.Lanes[0].Key)
	}
}

func TestDashboardLanes_NoSourcesReturnsEmptyLanes(t *testing.T) {
	// Rules present but no data sources — every real lane present and empty, no
	// 미분류 lane (nothing to triage). Confirms the dashboard renders the part
	// skeleton even on a cold gateway.
	resp := dashboardLanes(DashboardDeps{Rules: fakeRulesLoader()})(authedCtx(), reqWith(t, "miniapp.dashboard.lanes", nil))
	var got DashboardOut
	decode(t, resp, &got)
	if len(got.Lanes) != len(classification.AllLanes) {
		t.Fatalf("lanes = %d, want %d empty part lanes", len(got.Lanes), len(classification.AllLanes))
	}
	for _, l := range got.Lanes {
		if len(l.Items) != 0 {
			t.Fatalf("lane %q should be empty, got %d items", l.Key, len(l.Items))
		}
	}
}
