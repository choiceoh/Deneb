package serverchat

import (
	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/acp"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	handlerprocess "github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/handler/process"
)

// InitACPSubsystem sets up the ACP (Agent Control Plane) registry, bindings,
// persistence, and lifecycle sync. Must be called after event infrastructure
// and sessions are initialized. Called from the composition root's New().
func (m *Manager) InitACPSubsystem(denebDir string) {
	logger := m.Host.Logger()
	acpRegistry := acp.NewACPRegistry()
	acpRegistry.SetSessionManager(m.Sessions)
	acpBindings := acp.NewSessionBindingService()
	acpBindingStore := acp.NewBindingStore(acp.DefaultBindingStorePath(denebDir))
	if err := acpBindingStore.RestoreToService(acpBindings); err != nil {
		logger.Warn("failed to restore ACP bindings", "error", err)
	}

	// Restore agent registry from disk so subagent lineage survives restarts.
	acpRegistryStore := acp.NewRegistryStore(acp.DefaultRegistryStorePath(denebDir))
	if restored, err := acpRegistryStore.RestoreToRegistry(acpRegistry); err != nil {
		logger.Warn("failed to restore ACP registry", "error", err)
	} else if restored > 0 {
		logger.Info("restored ACP agents from disk", "count", restored)
	}

	m.ACPLifecycleUnsub = acp.StartACPLifecycleSync(acpRegistry, m.Sessions.EventBusRef())

	// Clear frozen context snapshots when sessions are evicted or deleted,
	// preventing stale snapshot accumulation in long-running gateways.
	m.SnapshotLifecycleUnsub = m.Sessions.EventBusRef().Subscribe(func(e session.Event) {
		if e.Kind == session.EventDeleted {
			prompt.ClearSessionSnapshot(e.Key)
		}
	})
	m.ACPDeps = &handlerprocess.ACPDeps{
		Registry:      acpRegistry,
		Bindings:      acpBindings,
		Infra:         &acp.SubagentInfraDeps{ACPRegistry: acpRegistry, Sessions: m.Sessions},
		Sessions:      m.Sessions,
		GatewaySubs:   m.Host.GatewaySubs(),
		BindingStore:  acpBindingStore,
		RegistryStore: acpRegistryStore,
		Translator:    acp.NewACPTranslator(acpRegistry, acpBindings),
	}
	m.ACPDeps.SetEnabled(true)
}
