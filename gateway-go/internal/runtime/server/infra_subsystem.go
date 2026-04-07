package server

import (
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/maintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/tasks"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/thinking"
)

// InfraSubsystem groups infrastructure services with independent lifecycles:
// background task control plane, thinking runtime, and maintenance runner.
// Embedded in Server so fields are promoted and existing access patterns are unchanged.
type InfraSubsystem struct {
	taskRegistry    *tasks.Registry
	taskStore       *tasks.Store
	thinkingRuntime *thinking.ThinkingRuntime
	maintRunner     *maintenance.Runner
}

// NewInfraSubsystem creates infrastructure services that can be eagerly initialized.
func NewInfraSubsystem(logger *slog.Logger, denebDir string) *InfraSubsystem {
	sub := &InfraSubsystem{
		thinkingRuntime: thinking.NewThinkingRuntime(),
		maintRunner:     maintenance.NewRunner(denebDir),
	}

	// Background task control plane (SQLite ledger).
	taskStore, err := tasks.OpenStore(tasks.DefaultStoreConfig(), logger)
	if err != nil {
		logger.Warn("task store open failed, task tracking disabled", "error", err)
	} else {
		sub.taskStore = taskStore
		reg, regErr := tasks.NewRegistry(taskStore, logger)
		if regErr != nil {
			logger.Warn("task registry init failed", "error", regErr)
		} else {
			sub.taskRegistry = reg
		}
	}

	return sub
}
