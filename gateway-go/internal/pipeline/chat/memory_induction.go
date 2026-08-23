package chat

import (
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/pkg/promptguard"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
)

// memoryInductionSequencer preserves user-turn order across the background
// workers below. Capturing only time.Now before spawning is insufficient: the
// fact journal stores millisecond timestamps, so two adjacent turns can tie and
// a later-scheduled older worker can still win the latest-value policy.
type memoryInductionSequencer struct {
	mu           sync.Mutex
	tail         <-chan struct{}
	lastAtMillis int64
	nextSequence uint64
}

type memoryInductionTurn struct {
	previous <-chan struct{}
	complete chan struct{}
	at       time.Time
	sequence uint64
}

var memoryInductionTurns memoryInductionSequencer

func (s *memoryInductionSequencer) reserve(now time.Time) memoryInductionTurn {
	s.mu.Lock()
	defer s.mu.Unlock()

	previous := s.tail
	if previous == nil {
		start := make(chan struct{})
		close(start)
		previous = start
	}
	complete := make(chan struct{})
	s.tail = complete

	atMillis := now.UTC().UnixMilli()
	if atMillis <= s.lastAtMillis {
		atMillis = s.lastAtMillis + 1
	}
	s.lastAtMillis = atMillis
	s.nextSequence++
	return memoryInductionTurn{
		previous: previous,
		complete: complete,
		at:       time.UnixMilli(atMillis).UTC(),
		sequence: s.nextSequence,
	}
}

func (t memoryInductionTurn) wait() {
	<-t.previous
}

func (t memoryInductionTurn) finish() {
	close(t.complete)
}

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
	if logger == nil {
		logger = slog.Default()
	}
	if params.GateUntrustedTools && promptguard.HasThreat(msg) {
		// The same opt-in boundary that protects irreversible tools also protects
		// this implicit durable write path. Otherwise pasted promptware containing
		// a profile/forget cue could bypass the tool gate and mint direct_user
		// authority before the model runs.
		logger.Warn("memory induction skipped promptware-tainted user turn",
			"session", params.SessionKey, "signals", promptguard.Labels(promptguard.Scan(msg)))
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
	if ind.Route == memory.RouteMemory && !trustedMemoryInductionRun(params) {
		logger.Warn("memory induction skipped non-user fact mutation origin", "session", sessionKey)
		return
	}
	factPlaneRoute := ind.Route == memory.RouteMemory && deps.memory.Wiki != nil
	var memoryTurn *memoryInductionTurn
	if ind.Route == memory.RouteMemory {
		// Preserve direct-user turn order for both the canonical fact plane and
		// the wiki-disabled legacy MEMORY.md fallback. Startup migration uses
		// append order, so allowing legacy workers to race can resurrect an older
		// preference on the next restart.
		turn := memoryInductionTurns.reserve(time.Now())
		memoryTurn = &turn
	}
	safego.GoWithSlog(logger, "memory-induction", func() {
		if memoryTurn != nil {
			memoryTurn.wait()
			defer memoryTurn.finish()
		}
		// The fact plane is the canonical profile sink when wiki is available.
		// Unlike the legacy MEMORY.md append path it resolves a stable fact key,
		// permanently retains the old version, and projects only the winner.
		// direct_user is a trusted authority: only the exact native home session
		// may use it. Sub-conversations and external/group sessions retain the
		// legacy MainSessionOnly drop behavior below.
		if factPlaneRoute {
			if ind.Candidate.Forget {
				factResult, err := deps.memory.Wiki.TombstoneFact(wiki.FactTombstoneInput{
					Subject: ind.Candidate.SubjectID,
					Key:     ind.Candidate.FactKey, Authority: wiki.FactAuthorityDirectUser,
					Sources: []string{"session:" + sessionKey}, Actor: "chat-memory-induction",
					Reason: "직접 사용자 삭제 요청", At: memoryTurn.at,
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
				At:        memoryTurn.at,
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

		var inductionNow func() time.Time
		if memoryTurn != nil {
			at := memoryTurn.at
			inductionNow = func() time.Time { return at }
		}
		res, err := memory.Apply(ind, memory.ApplyOptions{
			WorkspaceDir:    workspace,
			LedgerPath:      ledger,
			SessionKey:      sessionKey,
			MainSessionOnly: true,
			Now:             inductionNow,
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
