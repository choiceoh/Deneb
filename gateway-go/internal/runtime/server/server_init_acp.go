package server

import (
	handlerwire "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/handlerwire"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// initACPSubsystem sets up the ACP (Agent Control Plane) registry, bindings,
// persistence, and lifecycle sync. Must be called after event infrastructure
// and sessions are initialized.
func (s *Server) initACPSubsystem(denebDir string) {
	acpRegistry := pipebind.NewACPRegistry()
	acpRegistry.SetSessionManager(s.sessions)
	acpBindings := pipebind.NewSessionBindingService()
	acpBindingStore := pipebind.NewBindingStore(pipebind.DefaultBindingStorePath(denebDir))
	if err := acpBindingStore.RestoreToService(acpBindings); err != nil {
		s.logger.Warn("failed to restore ACP bindings", "error", err)
	}

	// Restore agent registry from disk so subagent lineage survives restarts.
	acpRegistryStore := pipebind.NewRegistryStore(pipebind.DefaultRegistryStorePath(denebDir))
	if restored, err := acpRegistryStore.RestoreToRegistry(acpRegistry); err != nil {
		s.logger.Warn("failed to restore ACP registry", "error", err)
	} else if restored > 0 {
		s.logger.Info("restored ACP agents from disk", "count", restored)
	}

	s.acpLifecycleUnsub = pipebind.StartACPLifecycleSync(acpRegistry, s.sessions.EventBusRef())

	// Clear frozen context snapshots when sessions are evicted or deleted,
	// preventing stale snapshot accumulation in long-running gateways.
	s.snapshotLifecycleUnsub = s.sessions.EventBusRef().Subscribe(func(e domainbind.Event) {
		if e.Kind == domainbind.EventDeleted {
			pipebind.ClearSessionSnapshot(e.Key)
		}
	})
	s.acpDeps = &handlerwire.ProcessACPDeps{
		Registry:      acpRegistry,
		Bindings:      acpBindings,
		Infra:         &pipebind.SubagentInfraDeps{ACPRegistry: acpRegistry, Sessions: s.sessions},
		Sessions:      s.sessions,
		GatewaySubs:   s.gatewaySubs,
		BindingStore:  acpBindingStore,
		RegistryStore: acpRegistryStore,
		Translator:    pipebind.NewACPTranslator(acpRegistry, acpBindings),
	}
	s.acpDeps.SetEnabled(true)
}
