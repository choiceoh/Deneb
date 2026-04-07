package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// initACPSubsystem sets up the ACP (Agent Control Plane) registry, bindings,
// persistence, and lifecycle sync. Must be called after event infrastructure
// and sessions are initialized.
func (s *Server) initACPSubsystem(denebDir string) {
	acpRegistry := acp.NewACPRegistry()
	acpRegistry.SetSessionManager(s.sessions)
	acpBindings := acp.NewSessionBindingService()
	acpBindingStore := acp.NewBindingStore(acp.DefaultBindingStorePath(denebDir))
	if err := acpBindingStore.RestoreToService(acpBindings); err != nil {
		s.logger.Warn("failed to restore ACP bindings", "error", err)
	}

	// Restore agent registry from disk so subagent lineage survives restarts.
	acpRegistryStore := acp.NewRegistryStore(acp.DefaultRegistryStorePath(denebDir))
	if restored, err := acpRegistryStore.RestoreToRegistry(acpRegistry); err != nil {
		s.logger.Warn("failed to restore ACP registry", "error", err)
	} else if restored > 0 {
		s.logger.Info("restored ACP agents from disk", "count", restored)
	}

	s.acpLifecycleUnsub = acp.StartACPLifecycleSync(acpRegistry, s.sessions.EventBusRef())

	// Clear frozen context snapshots when sessions are evicted or deleted,
	// preventing stale snapshot accumulation in long-running gateways.
	s.snapshotLifecycleUnsub = s.sessions.EventBusRef().Subscribe(func(e session.Event) {
		if e.Kind == session.EventDeleted {
			prompt.ClearSessionSnapshot(e.Key)
		}
	})
	s.acpDeps = &handlerprocess.ACPDeps{
		Registry:      acpRegistry,
		Bindings:      acpBindings,
		Infra:         &acp.SubagentInfraDeps{ACPRegistry: acpRegistry, Sessions: s.sessions},
		Sessions:      s.sessions,
		GatewaySubs:   s.gatewaySubs,
		BindingStore:  acpBindingStore,
		RegistryStore: acpRegistryStore,
		Translator:    acp.NewACPTranslator(acpRegistry, acpBindings),
	}
	s.acpDeps.SetEnabled(true)
}
