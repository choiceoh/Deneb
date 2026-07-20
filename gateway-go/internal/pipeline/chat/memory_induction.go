package chat

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// maybeRunMemoryInduction classifies the user turn into a WriteTarget and
// applies governed writeback (profile→MEMORY.md, procedure→ledger, exclude drop).
// Runs in the background beside diary recording; failures are Warn-level only.
func maybeRunMemoryInduction(deps runDeps, params RunParams, result *agent.AgentResult, logger *slog.Logger) {
	if result == nil || deps.briefcaseMode {
		return
	}
	if !shouldRecordRunDiary(params) {
		return
	}
	msg := strings.TrimSpace(params.Message)
	if msg == "" {
		return
	}
	ind := memory.InduceFromTurn(msg)
	if ind == nil {
		return
	}
	// Episodic/drop need no disk work — avoid spawning a goroutine for the common case.
	if ind.Route == memory.RouteDiaryOnly || ind.Route == memory.RouteDrop || ind.Route == memory.RouteUnspecified {
		return
	}
	workspace := strings.TrimSpace(deps.workspaceDir)
	if workspace == "" {
		workspace = strings.TrimSpace(deps.promptWorkspaceDir)
	}
	if workspace == "" || strings.HasPrefix(workspace, "/briefcase/") {
		// Last resort: resolve from config (same source as chat_pipeline wiring).
		workspace = strings.TrimSpace(config.ResolveAgentWorkspaceDir(nil))
	}
	ledger := memory.DefaultLedgerPath(config.ResolveStateDir())
	sessionKey := params.SessionKey
	if logger == nil {
		logger = slog.Default()
	}
	safego.GoWithSlog(logger, "memory-induction", func() {
		res, err := memory.Apply(ind, memory.ApplyOptions{
			WorkspaceDir:    workspace,
			LedgerPath:      ledger,
			SessionKey:      sessionKey,
			MainSessionOnly: true,
		})
		if err != nil {
			logger.Warn("memory induction apply failed", "error", err, "route", ind.Route, "target", ind.Candidate.Target)
			return
		}
		if res.Wrote {
			logger.Info(
				"memory induction wrote",
				"route", res.Route,
				"target", res.Target,
				"session", sessionKey,
				"ledger", filepath.Base(ledger),
			)
			return
		}
		if res.Dropped != "" {
			logger.Warn(
				"memory induction skipped",
				"route", res.Route,
				"target", res.Target,
				"reason", res.Dropped,
				"session", sessionKey,
				"workspace", workspace != "",
			)
		}
	})
}
