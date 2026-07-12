// Package module composes independently owned Mini App capabilities into one
// registration surface for the gateway server. Feature packages own behavior;
// this package owns only method-set composition and duplicate protection.
package module

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/org"
	contactsapi "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/dashboard"
	marketapi "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/market"
	sessionsapi "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/sessions"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/syncapi"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// DuplicateMethodError reports an ownership collision between capabilities.
// It is returned to the server composition root instead of panicking in a
// library package, so gateway startup can fail through its normal error path.
type DuplicateMethodError struct {
	Method string
}

func (e *DuplicateMethodError) Error() string {
	return fmt.Sprintf("handlerminiapp module: duplicate method %q", e.Method)
}

type (
	ContactsDeps            = contactsapi.ContactsDeps
	DashboardDeps           = dashboard.DashboardDeps
	DashboardCalendarSource = dashboard.DashboardCalendarSource
	DashboardWorkFeedSource = dashboard.DashboardWorkFeedSource
	SessionsDeps            = sessionsapi.SessionsDeps
	SessionsLister          = sessionsapi.SessionsLister
	TranscriptLoader        = sessionsapi.TranscriptLoader
	MarketDeps              = marketapi.MarketDeps
	SyncDeps                = syncapi.SyncDeps
	NativeSyncStore         = syncapi.NativeSyncStore
)

// OrgDashboardDeps binds the dashboard to the org chart as its source of
// classification rules and lane definitions. Keeping this policy in the
// miniapp module prevents the gateway server from owning domain translation.
func OrgDashboardDeps(calendar DashboardCalendarSource, workFeed DashboardWorkFeedSource) DashboardDeps {
	return DashboardDeps{
		Rules:    org.LoadRules,
		Lanes:    org.LoadLanes,
		Calendar: calendar,
		WorkFeed: workFeed,
	}
}

// Dependencies keeps each capability's ports separate. Adding a capability
// does not expand another capability's contract.
type Dependencies struct {
	Contacts  ContactsDeps
	Dashboard DashboardDeps
	Sessions  SessionsDeps
	Market    MarketDeps
	Sync      SyncDeps
}

// Methods composes the independently owned handler maps. A duplicate method is
// a programmer error: silently overwriting it would make registration order a
// hidden behavior change.
func Methods(deps Dependencies) (map[string]rpcutil.HandlerFunc, error) {
	sets := []map[string]rpcutil.HandlerFunc{
		contactsapi.ContactsMethods(deps.Contacts),
		dashboard.DashboardMethods(deps.Dashboard),
		sessionsapi.SessionsMethods(deps.Sessions),
		marketapi.MarketMethods(deps.Market),
		syncapi.SyncMethods(deps.Sync),
	}
	return mergeMethodSets(sets...)
}

func mergeMethodSets(sets ...map[string]rpcutil.HandlerFunc) (map[string]rpcutil.HandlerFunc, error) {
	var methods map[string]rpcutil.HandlerFunc
	for _, set := range sets {
		for name, handler := range set {
			if methods == nil {
				methods = make(map[string]rpcutil.HandlerFunc)
			}
			if _, exists := methods[name]; exists {
				return nil, &DuplicateMethodError{Method: name}
			}
			methods[name] = handler
		}
	}
	return methods, nil
}
