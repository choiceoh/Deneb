// Early-phase RPC capability bootstrap for GatewayHub.
//
// Domain Handler Deps assembly lives in serverwire/early and serverwire/late.
// This file owns hub validation, store initialization, and the thin call into
// early.RegisterDomains. Late wiring: method_registry_late.go.
package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/market"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/push"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/localcal"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/insights"
	runtimenotify "github.com/choiceoh/deneb/gateway-go/internal/runtime/notify"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverwire/early"
)

// wikiSenderFacts resolves "who is this person to us" in-process from the wiki
// graph — used by the analyze pipeline and the sender_context card. The argument
// is the From header (name and, when present, "<address>"). It prefers the EMAIL
// join: resolving the sender's address to their 인물 page and seeding the graph
// from that exact page reaches the RIGHT person across 동명이인 (김성훈@bohae vs
// 김성훈@marsh), which the display name alone conflates. Falls back to the name
// graph when no address resolves. Returns "" when the wiki is unconfigured or
// nothing matches, so callers fall back cleanly (to graphify, or an empty card).
func (s *Server) wikiSenderFacts(ctx context.Context, from string) string {
	if s.Mail.WikiStore == nil {
		return ""
	}
	if email := senderEmailFromHeader(from); email != "" {
		if path := s.WikiStore().ResolvePersonByEmail(email); path != "" {
			if facts, err := s.WikiStore().PageConnections(ctx, path, 0); err == nil && facts != "" {
				return facts
			}
		}
	}
	// GraphContext strips any "<address>" itself, so the raw From is a safe query.
	facts, err := s.WikiStore().GraphContext(ctx, from, 0)
	if err != nil {
		return ""
	}
	return facts
}

// senderEmailFromHeader pulls the address out of a "Name <addr@host>" From header
// (or returns a bare address as-is). "" when the header carries no address.
func senderEmailFromHeader(from string) string {
	from = strings.TrimSpace(from)
	if i := strings.LastIndex(from, "<"); i >= 0 {
		if j := strings.Index(from[i:], ">"); j > 0 {
			from = from[i+1 : i+j]
		}
	}
	from = strings.TrimSpace(from)
	if strings.Contains(from, "@") {
		return from
	}
	return ""
}

// registerEarlyMethods registers all RPC domains that don't depend on chatHandler.
// Called after buildHub() but before registerSessionRPCMethods().
func (s *Server) registerEarlyMethods(hub *rpcutil.GatewayHub, denebDir string) error {
	hub.AdvancePhase(rpcutil.PhaseEarly)

	// Fail fast if core hub fields are missing.
	if err := hub.Validate(); err != nil {
		return fmt.Errorf("server init: hub validation: %w", err)
	}

	capabilities, err := s.initializeEarlyMethodCapabilities(hub, denebDir)
	if err != nil {
		return err
	}
	early.RegisterDomains(hub, denebDir, s.wirePorts(), capabilities)

	// Special-case registrations with embedded business logic.
	s.registerConfigLifecycleMethods()
	return nil
}

// initializeEarlyMethodCapabilities creates stores and long-lived services that
// early handlers consume, then asks serverwire to assemble Handler Deps bags.
func (s *Server) initializeEarlyMethodCapabilities(hub *rpcutil.GatewayHub, denebDir string) (early.EarlyCapabilities, error) {
	insightsEngine := insights.New(hub.Sessions(), s.usageTracker)
	hub.SetInsights(insightsEngine)
	s.insights = insightsEngine

	if cs, err := contacts.NewStore(filepath.Join(denebDir, "contacts.json")); err != nil {
		s.logger.Warn("contacts store init failed; contacts sync disabled", "error", err)
	} else {
		s.Mail.ContactsStore = cs
		hub.SetContactsStore(cs)
	}
	s.Mail.NativeSyncStore = nativesync.NewStore(filepath.Join(denebDir, "native_sync.jsonl"))
	s.Mail.WorkFeedStore = workfeed.NewStore(filepath.Join(denebDir, "workfeed.jsonl"))
	nativeWorkFeed := s.Mail.NativeWorkFeedStore()

	if calStore, err := localcal.Default(); err == nil && calStore != nil {
		calStore.SetChangeObserver(func(eventID string) {
			if _, err := s.NativeSyncStore().Append(nativesync.CalendarChanged(eventID)); err != nil {
				s.logger.Error("native sync: calendar event append failed", "eventID", eventID, "error", err)
			}
		})
	}

	s.pushTokenStore = push.NewStore(filepath.Join(denebDir, "push_tokens.json"))
	if fcmCfg := push.ConfigFromEnv(); fcmCfg.Enabled() {
		if sender, err := push.NewFCMSender(fcmCfg); err != nil {
			s.logger.Error("FCM push: credentials load failed; proactive FCM fallback disabled", "error", err)
		} else {
			s.pushNotifier = push.NewNotifier(push.NotifierDeps{
				Store:  s.pushTokenStore,
				Sender: sender,
				Logger: s.logger,
				Broadcast: func(event string, payload any) {
					s.broadcaster.Broadcast(event, eventPayloadFromAny(payload))
				},
				ShutdownCtx: s.ShutdownCtx(),
			})
			s.logger.Info("FCM push fallback enabled", "projectID", sender.ProjectID())
		}
	}

	s.notify = runtimenotify.NewService(hub.Sessions(), hub.Logger(), func(title, body string) {
		proactive.PublishWithFallback(s.PushHub(), s.pushNotifier, proactive.Event{Title: title, Body: body})
	}, s.BoundAddr)
	if s.notify != nil {
		s.broadcaster.RegisterTap(s.notify.Tap)
		s.notify.Start(s.ShutdownCtx())
	}

	s.marketCache = market.NewCache()

	caps, err := early.BuildEarlyCapabilities(hub, denebDir, early.EarlyCapInput{
		NativeWorkFeed: nativeWorkFeed,
		LogCapture:     s.logCapture,
		AgentLog:       func() *agentlog.Writer { return s.AgentLogWriter() },
		VllmBases: func() []string {
			if s.Chat.ModelRegistry == nil {
				return nil
			}
			return s.ModelRegistry().VllmBaseURLs()
		},
		NativeSync:  s.NativeSyncStore(),
		Dashboard:   s.Mail.DashboardDeps(),
		MarketCache: s.marketCache,
		ToolDeps:    s.ToolDeps(),
	})
	if err != nil {
		return early.EarlyCapabilities{}, fmt.Errorf("server init: miniapp module: %w", err)
	}
	return caps, nil
}
