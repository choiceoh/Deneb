package servermail

// dashboard_sources.go — adapters that feed the miniapp.dashboard.* handler.
//
// The dashboard groups work items by the operator's managed parts (레인). Its
// handler (handlerminiapp/dashboard.go) takes narrow data-source interfaces; this
// file builds the production implementations from the same stores the calendar
// and work-feed RPCs use, plus the classifier ruleset loader.

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp"
	minimodule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/module"
	minischedule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/schedule"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire"
)

// dashboardCalendarSource adapts the hybrid calendar (read-only Google client +
// local store) into the single merged read the dashboard needs. It mirrors the
// calendar handler's listMerged: Google events (when the client is configured
// and healthy) unioned with local events, sorted by start. Either side may be
// absent — a nil client or a Google error degrades to local-only so the
// dashboard's calendar lane keeps working without OAuth.
type dashboardCalendarSource struct {
	client func() (minischedule.CalendarClient, error)
	local  minischedule.LocalCalendar
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

// DashboardDeps assembles the production DashboardDeps. Sources are nil-tolerant:
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
func (m *Manager) DashboardDeps() minimodule.DashboardDeps {
	var wf minimodule.DashboardWorkFeedSource
	if nwf := m.NativeWorkFeedStore(); nwf != nil {
		wf = nwf
	}
	return minimodule.OrgDashboardDeps(
		dashboardCalendarSource{
			client: func() (minischedule.CalendarClient, error) { return calendar.DefaultClient() },
			local:  serverwire.ResolveLocalCalendar(m.Host.Logger()),
		},
		wf,
	)
}

// OrgDeps assembles the production OrgDeps for the miniapp.org.* editor. Load
// reads the operator's {stateDir}/org.json (missing → empty tree); SavePath
// resolves that same path for the atomic write. Always non-nil so the editor
// registers unconditionally (a fresh install opens to a blank chart).
// LookupContact wires the contacts store so org.get enriches each member with
// their phone/email (read-only; never persisted) — nil-safe when the store is
// absent (enrichment just yields nothing).
func (m *Manager) OrgDeps() handlerminiapp.OrgDeps {
	return handlerminiapp.OrgDeps{
		Load:          func() (org.OrgTree, error) { return org.Load() },
		SavePath:      org.ResolvePath,
		LookupContact: orgContactLookup(m.ContactsStore),
		// Read m.WikiStore lazily at GET time (not OrgDeps() time): the org editor
		// registers before the wiki store is wired, so capturing the field's value
		// here would freeze a nil. A nil store at call time simply yields no person
		// links (graceful).
		ResolvePeople: func(names []string) map[string]string {
			if m.WikiStore == nil {
				return nil
			}
			return m.WikiStore.ResolvePersonPaths(names)
		},
		// Email join (preferred): a member's enriched address → their 인물 page,
		// robust across 동명이인. Lazy m.WikiStore read (same reason as above).
		ResolvePersonByEmail: func(email string) string {
			if m.WikiStore == nil {
				return ""
			}
			return m.WikiStore.ResolvePersonByEmail(email)
		},
	}
}

// orgContactLookup builds the name → (phones, emails) enrichment used by
// miniapp.org.get. It matches a member's display name to the address book the
// same way the people directory does — via contacts.NormalizePersonName, which peels
// honorific/role suffixes and affiliation parentheticals so "김민준 부장" matches
// the contact "김민준" (exact on the normalized key; no substring matching, which
// would mis-pair "이수" with "이수민").
//
// A nil store (contacts sync disabled / load failed) yields a nil function so
// OrgDeps.LookupContact stays nil and the handler skips enrichment cleanly. The
// index is rebuilt from the current snapshot on each call so freshly-synced
// contacts are reflected without restarting; the chart is tiny and GET is
// infrequent, so one O(contacts) build per request is negligible. When several
// contacts collapse to the same normalized name (homonyms), their phones/emails
// are unioned (deduped, first-seen order).
func orgContactLookup(store *contacts.Store) func(name string) (phones, emails []string) {
	if store == nil {
		return nil
	}
	return func(name string) (phones, emails []string) {
		key := contacts.NormalizePersonName(name)
		if key == "" {
			return nil, nil
		}
		for _, c := range store.All() {
			if contacts.NormalizePersonName(c.Name) != key {
				continue
			}
			phones = appendDedup(phones, c.Phones)
			emails = appendDedup(emails, c.Emails)
		}
		return phones, emails
	}
}

// appendDedup appends the trimmed, non-empty entries of add to dst, skipping any
// value already present (case-sensitive for phones; callers pass already-formatted
// strings). Preserves first-seen order. Returns dst unchanged when add is empty.
func appendDedup(dst, add []string) []string {
	for _, v := range add {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		dup := false
		for _, e := range dst {
			if e == v {
				dup = true
				break
			}
		}
		if !dup {
			dst = append(dst, v)
		}
	}
	return dst
}
