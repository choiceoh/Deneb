package chat

import (
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
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
		// The fact plane is the canonical profile sink when wiki is available.
		// Unlike the legacy MEMORY.md append path it resolves a stable fact key,
		// permanently retains the old version, and projects only the winner.
		if ind.Route == memory.RouteMemory && deps.memory.Wiki != nil {
			if ind.Candidate.Forget {
				factResult, err := deps.memory.Wiki.TombstoneFact(wiki.FactTombstoneInput{
					Subject: ind.Candidate.SubjectID,
					Key:     ind.Candidate.FactKey, Authority: wiki.FactAuthorityDirectUser,
					Sources: []string{"session:" + sessionKey}, Actor: "chat-memory-induction",
					Reason: "직접 사용자 삭제 요청",
				})
				if err != nil {
					logger.Error("memory induction fact tombstone failed",
						"error", err, "target", ind.Candidate.Target, "session", sessionKey)
					return
				}
				if factResult.Committed {
					ClearFactDerivedCachesAtRevision(uint64(factResult.Revision), factResult.ProjectionError)
					logger.Info("memory induction tombstoned fact",
						"revision", factResult.Revision, "resolution", factResult.Resolution,
						"session", sessionKey, "projectionDegraded", factResult.ProjectionError != "")
				}
				return
			}
			factResult, err := deps.memory.Wiki.UpsertFact(wiki.FactInput{
				Subject: ind.Candidate.SubjectID,
				Key:     ind.Candidate.FactKey, Value: ind.Candidate.Text,
				Kind:      wiki.FactKind(ind.Candidate.FactKind),
				Authority: wiki.FactAuthorityDirectUser,
				Sources:   []string{"session:" + sessionKey},
				Actor:     "chat-memory-induction",
				Reason:    "직접 사용자 발화",
			})
			if err != nil {
				logger.Error("memory induction fact commit failed",
					"error", err, "target", ind.Candidate.Target, "session", sessionKey)
				return
			}
			if factResult.Committed {
				// The legacy context and tier-1 snapshots intentionally freeze per
				// session. A rare explicit correction is the exception: clear all
				// three derived caches so the old value cannot coexist next turn.
				ClearFactDerivedCachesAtRevision(uint64(factResult.Revision), factResult.ProjectionError)
				logger.Info(
					"memory induction committed fact",
					"revision", factResult.Revision,
					"status", factResult.Status,
					"resolution", factResult.Resolution,
					"session", sessionKey,
					"projectionDegraded", factResult.ProjectionError != "",
				)
			}
			return
		}

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
