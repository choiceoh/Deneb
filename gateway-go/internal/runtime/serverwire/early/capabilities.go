// Early-phase capability bootstrap for GatewayHub registration.
package early

import (
	"errors"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	minimodule "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerminiapp/module"
	handlerobservatory "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observatory"
	handlerobserve "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire/porttypes"
)

// EarlyCapInput is the store/callback bag needed to build EarlyCapabilities
// after Server has created early-phase stores.
type EarlyCapInput struct {
	NativeWorkFeed porttypes.WorkFeedMirror
	LogCapture     *observe.LogCapture
	AgentLog       func() *agentlog.Writer
	VllmBases      func() []string
	NativeSync     *nativesync.Store
	Dashboard      minimodule.DashboardDeps
	MarketCache    *market.Cache
	ToolDeps       *chat.CoreToolDeps
}

// EarlyCapabilities holds phase-local deps created before early domain registration.
type EarlyCapabilities struct {
	NativeWorkFeed porttypes.WorkFeedMirror
	Observe        handlerobserve.Deps
	Observatory    handlerobservatory.Deps
	Miniapp        map[string]rpcutil.HandlerFunc
}

// BuildEarlyCapabilities assembles observe/observatory/miniapp method bags after
// Server has created the backing stores. Keeps that Handler Deps assembly out of
// package server.
func BuildEarlyCapabilities(hub *rpcutil.GatewayHub, denebDir string, in EarlyCapInput) (EarlyCapabilities, error) {
	observeDeps := handlerobserve.Deps{
		Capture:   in.LogCapture,
		AgentLog:  in.AgentLog,
		VllmBases: in.VllmBases,
		Logger:    hub.Logger(),
	}
	if observeDeps.AgentLog == nil {
		observeDeps.AgentLog = func() *agentlog.Writer { return nil }
	}
	if observeDeps.VllmBases == nil {
		observeDeps.VllmBases = func() []string { return nil }
	}

	observatoryDeps := handlerobservatory.Deps{
		StateDir: func() string { return denebDir },
	}

	marketDeps := minimodule.MarketDeps{}
	if in.MarketCache != nil {
		marketDeps.Fetch = in.MarketCache.Summary
	}

	miniappMethods, err := minimodule.Methods(minimodule.Dependencies{
		Sync:      minimodule.SyncDeps{Store: in.NativeSync},
		Dashboard: in.Dashboard,
		Sessions: minimodule.SessionsDeps{
			Manager: hub.Sessions(),
			Transcripts: func() (minimodule.TranscriptLoader, error) {
				if in.ToolDeps == nil || in.ToolDeps.Sessions.Transcript == nil {
					return nil, serverwire.ErrTranscriptUnavailable
				}
				return in.ToolDeps.Sessions.Transcript, nil
			},
		},
		Contacts: minimodule.ContactsDeps{
			Store: func() (*contacts.Store, error) {
				cs := hub.ContactsStore()
				if cs == nil {
					return nil, errors.New("contacts store not configured")
				}
				return cs, nil
			},
		},
		Market: marketDeps,
	})
	if err != nil {
		return EarlyCapabilities{}, err
	}

	return EarlyCapabilities{
		NativeWorkFeed: in.NativeWorkFeed,
		Observe:        observeDeps,
		Observatory:    observatoryDeps,
		Miniapp:        miniappMethods,
	}, nil
}
