package server

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/toolbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
)

// initGenesisServices creates the genesis service, tracker, and evolver.
// Called after chatHandler is created but BEFORE registerLateMethods, so the
// RPC methods can be registered in method_registry.go (Rule 1 compliance).
//
// Core construction (catalog/service/tracker/evolver/meta) lives in
// skilllifecycle.BuildCore — the owning-module registrar port — so this
// composition root does not import generation/review leaves.
func (s *Server) initGenesisServices() {
	if s.chatHandler == nil || s.modelRegistry == nil {
		s.logger.Debug("genesis: skipped (chat handler or model registry unavailable)")
		return
	}

	lwClient := s.modelRegistry.Client(modelrole.RoleLightweight)
	lwModel := s.modelRegistry.Model(modelrole.RoleLightweight)
	if lwClient == nil || lwModel == "" {
		s.logger.Debug("genesis: skipped (lightweight model not configured)")
		return
	}

	workspaceDir := ""
	if s.toolDeps != nil {
		workspaceDir = s.toolDeps.WorkspaceDir
	}
	thinkingKwargs := s.genesisThinkingKwargs()
	mainClient := s.modelRegistry.Client(modelrole.RoleMain)
	mainModel := s.modelRegistry.Model(modelrole.RoleMain)

	var evolverRole modelrole.Role
	var evolverModel string
	bundle := skilllifecycle.BuildCore(skilllifecycle.CoreBuildInput{
		Logger:                s.logger,
		LWClient:              lwClient,
		LWModel:               lwModel,
		MainClient:            mainClient,
		MainModel:             mainModel,
		WorkspaceDir:          workspaceDir,
		BundledSkillsDir:      chat.BundledSkillsDir(),
		ThinkingKwargs:        thinkingKwargs,
		LowConfidenceObserver: s.postLowConfidenceEvolveCard,
		ConfigureEvolver: func(evolver *skilllifecycle.Evolver) (string, string) {
			evolverRole, evolverModel = s.configureGenesisEvolverModels(evolver)
			return string(evolverRole), evolverModel
		},
	})
	if bundle == nil {
		return
	}
	s.skillCatalog = bundle.Catalog
	s.genesisSvc = bundle.Service
	s.genesisTracker = bundle.Tracker
	s.genesisEvolver = bundle.Evolver
	if s.genesisTracker != nil {
		s.genesisTracker.SetExemplarEmbedder(s.embeddingClient)
	}
	s.genesisMeta = bundle.Meta
	cfg := bundle.Config

	// Iteration-based nudger (Hermes-style): fires a mid-session skill
	// review every N tool calls. Env var DENEB_SKILL_NUDGE_INTERVAL
	// overrides skilllifecycle.DefaultNudgeInterval; 0 disables.
	// The review fork dispatches through chat.SendSync, which re-resolves the model string into a
	// provider via resolveModel — so it needs the FULL "provider/model" id. Model() returns the
	// bare name (e.g. "step3p7"), which has no provider and fails client resolution
	// ("no LLM client available, provider=\"\""), silently killing every nudger review and leaving
	// the whole Propus loop dead. Generate() uses lwClient directly, so the bare name
	// is fine there; only this SendSync path needs the prefix.
	//
	// Role: the review must CALL skill_lifecycle (action=propose) — it is a tool-calling task, not a
	// text one. The lightweight role is a text model (summaries/titles/JSON; it never tool-calls
	// elsewhere in Deneb) and emits prose with ZERO tool calls, so every review no-ops and the whole
	// Propus loop produces nothing (verified on the host: review turns log toolCount=0). Use the
	// coding role — the same tool-capable model the evolver already drives (model-roles dogma #7:
	// tool-heavy roles need a measured tool-caller). Fall back to lightweight when coding is
	// unconfigured, so a host without a coding model keeps the prior behavior instead of an empty id.
	reviewModel := s.modelRegistry.FullModelID(modelrole.RoleCoding)
	if reviewModel == "" {
		reviewModel = s.modelRegistry.FullModelID(modelrole.RoleLightweight)
	}
	reviewFork := skilllifecycle.NewReviewFork(s.chatHandler, s.genesisTranscripts, s.genesisTracker, reviewModel, s.logger)
	s.genesisNudger = skilllifecycle.NewNudgerFromEnvWithTrackerAndReviewer(
		s.genesisSvc,
		s.genesisTracker,
		reviewFork,
		s.logger,
	)
	// Derive the background skill-review forks from the server shutdown context
	// so an in-flight genesis review is cancelled on graceful shutdown instead
	// of orphaning the goroutine (concurrency.md rule 7); the fork still outlives
	// the user-facing turn — this is the shutdown context, not the request one.
	s.genesisNudger.SetShutdownContext(s.ShutdownCtx())

	// Install an adapter so the chat handler can invoke the nudger
	// without importing the genesis package (dependency inversion).
	//
	// Production-state gated (same invariant as the idle-review lane and
	// workout): even though the genesis tracker now roots under
	// DENEB_STATE_DIR, a mis-set env still must not fire fenced reviews that
	// write review liveness into the production Propus sidecar. Observed
	// 2026-07-11: a dev-origin review completion reset production review_age
	// and pushed the idle backstop's first fire out by its full 6h staleness
	// window. An explicit DENEB_SKILL_NUDGE_INTERVAL overrides the gate so
	// nudger behavior stays live-testable (e.g. puppet mode) when that is the
	// point of the test.
	nudgerHome, _ := os.UserHomeDir()
	_, nudgerProdState := s.productionStateDir(nudgerHome)
	if !nudgerProdState && os.Getenv("DENEB_SKILL_NUDGE_INTERVAL") == "" {
		s.logger.Info("genesis: skill nudger disabled (non-production state dir; set DENEB_SKILL_NUDGE_INTERVAL to force)")
	} else if s.chatHandler != nil && s.genesisNudger.Enabled() {
		s.chatHandler.SetSkillNudger(skilllifecycle.NewSkillNudger(s.genesisNudger))
	}
	// Usage attribution is independent of the nudger: even with the nudger
	// disabled, recording which skills are used (and whether their turns
	// succeed) gives the Evolver the success-rate signal its
	// SkillsNeedingEvolution gate reads — without it the loop runs blind.
	if s.chatHandler != nil && s.genesisTracker != nil {
		s.chatHandler.SetSkillUsageRecorder(skilllifecycle.NewChatUsageRecorder(
			s.genesisTracker,
			s.genesisTranscripts,
			s.logger,
			replayExecutorEnabled(),
			// Lightweight text role drives the fresh-context relevance classifier
			// that keeps off-topic sessions out of a skill's held-out corpus.
			lwClient,
			lwModel,
		))
	}
	s.registerSkillLifecycleTool()

	s.logger.Info("genesis: services initialized",
		"model", lwModel, "evolverRole", evolverRole, "evolverModel", evolverModel, "outputDir", cfg.OutputDir,
		"nudgeInterval", s.genesisNudger.Interval(),
		"minToolCalls", cfg.MinToolCalls,
		"minTurns", cfg.MinTurns,
		"maxSkillsPerDay", cfg.MaxSkillsPerDay)
}

func (s *Server) refreshCodingModelConsumers() {
	if s.modelRegistry == nil {
		return
	}
	codingModel := s.modelRegistry.FullModelID(modelrole.RoleCoding)
	if s.toolDeps != nil {
		s.toolDeps.Sessions.CodingDefaultModel = codingModel
	}
	if s.genesisEvolver != nil {
		role, model := s.configureGenesisEvolverModels(s.genesisEvolver)
		s.logger.Info("genesis: evolver model refreshed",
			"codingModel", codingModel, "evolverRole", role, "evolverModel", model)
	}
}

func (s *Server) configureGenesisEvolverModels(evolver *skilllifecycle.Evolver) (modelrole.Role, string) {
	if evolver == nil || s.modelRegistry == nil {
		return "", ""
	}
	evolverRole := modelrole.RoleLightweight
	evolverClient := s.modelRegistry.Client(modelrole.RoleLightweight)
	evolverModel := s.modelRegistry.Model(modelrole.RoleLightweight)
	if codingModel := s.modelRegistry.Model(modelrole.RoleCoding); codingModel != "" {
		if codingClient := s.modelRegistry.Client(modelrole.RoleCoding); codingClient != nil {
			evolverRole = modelrole.RoleCoding
			evolverClient = codingClient
			evolverModel = codingModel
		}
	}
	evolver.SetPrimary(evolverClient, evolverModel)

	// Teacher-escalation: wire the stronger main model so a rewrite that fails
	// the default lightweight self-test gets one escalated attempt (#4). When a
	// dedicated coding model is configured, it owns the patch-generation path;
	// keep main out of the rewrite loop so code/skill edits are made by the
	// coding role the operator selected.
	mainClient := s.modelRegistry.Client(modelrole.RoleMain)
	mainModel := s.modelRegistry.Model(modelrole.RoleMain)
	if evolverRole != modelrole.RoleCoding && mainClient != nil && mainModel != "" && mainModel != evolverModel {
		evolver.SetTeacher(mainClient, mainModel)
	} else {
		evolver.SetTeacher(nil, "")
	}

	// Judge (B1): wire main as the independent candidate judge regardless of who
	// owns rewrites. Decoupled from the teacher so the coding-model path (teacher
	// nil above) still grades with a non-producer model — otherwise
	// pickCandidateJudge fell back to the coding model judging its own rewrite
	// (same-family self-preference bias, arXiv:2508.02994). When main is the
	// producer or unavailable the judge stays unset and pickCandidateJudge logs
	// the self-judge fallback.
	if mainClient != nil && mainModel != "" && mainModel != evolverModel {
		evolver.SetJudge(mainClient, mainModel)
	} else {
		evolver.SetJudge(nil, "")
	}
	evolver.SetThinkingKwargs(s.genesisThinkingKwargs())

	// Behavioral-replay executor (DENEB_SKILL_EVOLVE_REPLAY, on by default; set
	// =0 to force off). A lightweight, LOCAL model simulates the production agent
	// following a skill so the held-out gate can score tool-call behavior and
	// reject a rewrite that regresses it. Lightweight (not main) is the right
	// role: a consistent, cheap discriminator for the original-vs-candidate delta
	// that avoids chat contention — the gate scores both bodies with the SAME
	// model, so any executor bias cancels (docs/agent-rules/model-roles.md). It is
	// the strongest "did it break what worked" gate (#6); the engine no-ops when
	// the replay-case corpus is empty, so defaulting on only adds cost once cases
	// exist.
	if replayExecutorEnabled() {
		evolver.SetReplayExecutor(
			s.modelRegistry.Client(modelrole.RoleLightweight),
			s.modelRegistry.Model(modelrole.RoleLightweight),
		)
	} else {
		evolver.SetReplayExecutor(nil, "")
	}
	return evolverRole, evolverModel
}

// replayExecutorEnabled reports whether the behavioral-replay validation gate is
// switched on via DENEB_SKILL_EVOLVE_REPLAY. On by default (#6): it is the
// strongest regression gate and no-ops without a replay corpus, so it only
// spends its two-extra-local-LLM-calls-per-case once cases exist. Set
// DENEB_SKILL_EVOLVE_REPLAY=0 (or false/no/off) to force it off.
func replayExecutorEnabled() bool {
	switch strings.TrimSpace(strings.ToLower(os.Getenv("DENEB_SKILL_EVOLVE_REPLAY"))) {
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func (s *Server) genesisThinkingKwargs() map[string]string {
	if s.modelRegistry == nil {
		return nil
	}
	// Resolve per-model thinking toggles so the evolver's judge/teacher/rewrite
	// calls truly disable reasoning on dual-mode vLLM models (dsv4) instead of
	// burning their whole output budget on chain-of-thought and returning
	// truncated JSON ("judge error"). Keyed by bare model name to match the names
	// the evolver passes to thinkingOff.
	thinkingKwargs := map[string]string{}
	for _, role := range []modelrole.Role{modelrole.RoleLightweight, modelrole.RoleCoding, modelrole.RoleMain} {
		mc := s.modelRegistry.Config(role)
		if mc.Model == "" {
			continue
		}
		if k := s.modelRegistry.CapabilityForModel(mc.ProviderID, mc.Model).ThinkingToggleKwarg; k != "" {
			thinkingKwargs[mc.Model] = k
		}
	}
	return thinkingKwargs
}

func (s *Server) registerSkillLifecycleTool() {
	if s.chatHandler == nil || s.genesisSvc == nil {
		return
	}
	var fixturePath string
	if home, err := os.UserHomeDir(); err == nil {
		fixturePath = runtimeheartbeat.FixturePath(home)
	}
	var shadowComplete runtimeheartbeat.ShadowCompleteFunc
	// Heartbeat shadow-replay wiring (P1): text-only lightweight executor over
	// the harvested fixture corpus. Same model both sides, thinking disabled on
	// dual-mode models (the evolver judge's dsv4 lesson). Missing pieces leave
	// the action cleanly unconfigured.
	if lwClient := s.modelRegistry.Client(modelrole.RoleLightweight); lwClient != nil {
		lwModel := s.modelRegistry.Model(modelrole.RoleLightweight)
		thinkingKwargs := s.genesisThinkingKwargs()
		shadowComplete = func(ctx context.Context, system, user string) (string, error) {
			req := llm.ChatRequest{
				Model:     lwModel,
				System:    llm.SystemString(system),
				Messages:  []llm.Message{llm.NewTextMessage("user", user)},
				MaxTokens: 4096,
			}
			if kw := thinkingKwargs[lwModel]; kw != "" {
				req.Thinking = &llm.ThinkingConfig{Type: "disabled", TemplateKwarg: kw}
			}
			return lwClient.Complete(ctx, req)
		}
	}
	var shadowReplay func(context.Context, string, int) (toolbind.HeartbeatShadowReplayResult, error)
	if fixturePath != "" && shadowComplete != nil {
		shadowReplay = func(ctx context.Context, candidate string, limit int) (toolbind.HeartbeatShadowReplayResult, error) {
			report, err := runtimeheartbeat.RunShadowReplay(ctx, fixturePath, candidate, limit, shadowComplete)
			if err != nil {
				return toolbind.HeartbeatShadowReplayResult{}, err
			}
			return heartbeatShadowReplayToolResult(report), nil
		}
	}
	backend := skilllifecycle.NewBackend(skilllifecycle.BackendConfig{
		Genesis:      s.genesisSvc,
		Evolver:      s.genesisEvolver,
		Tracker:      s.genesisTracker,
		Transcripts:  s.genesisTranscripts,
		Logger:       s.logger,
		ShadowReplay: shadowReplay,
	})
	s.chatHandler.RegisterTool(toolport.ToolDef{
		Name:        "skill_lifecycle",
		Description: toolbind.SkillLifecycleToolDescription(),
		InputSchema: toolbind.SkillLifecycleToolSchema(),
		Fn:          toolbind.ToolSkillLifecycle(backend),
		Deferred:    true,
		// Late-bound tool is outside tool_schemas MaxOutputs map. Status is a
		// multi-section JSON surface; 48K leaves room after status slim.
		MaxOutput: 48 * 1024,
	})
}

func heartbeatShadowReplayToolResult(report runtimeheartbeat.ShadowReplayReport) toolbind.HeartbeatShadowReplayResult {
	results := make([]toolbind.HeartbeatShadowReplayFixtureResult, 0, len(report.Results))
	for _, result := range report.Results {
		results = append(results, toolbind.HeartbeatShadowReplayFixtureResult{
			FiredAt:       result.FiredAt,
			Split:         result.Split,
			Quiet:         result.Quiet,
			OriginalPass:  result.OriginalPass,
			CandidatePass: result.CandidatePass,
			Note:          result.Note,
		})
	}
	return toolbind.HeartbeatShadowReplayResult{
		OK:                report.OK,
		Verdict:           report.Verdict,
		Reason:            report.Reason,
		Fixtures:          report.Fixtures,
		HeldInOriginal:    report.HeldInOriginal,
		HeldInCandidate:   report.HeldInCandidate,
		HeldInTotal:       report.HeldInTotal,
		HeldOutOriginal:   report.HeldOutOriginal,
		HeldOutCandidate:  report.HeldOutCandidate,
		HeldOutTotal:      report.HeldOutTotal,
		Results:           results,
		DryRun:            report.DryRun,
		ContractDriftNote: report.ContractDriftNote,
	}
}

// registerGenesisAutonomousTasks registers periodic background tasks for genesis.
// Called during registerWorkflowSideEffects (non-RPC phase).
func (s *Server) registerGenesisAutonomousTasks(_ *rpcutil.GatewayHub) {
	if s.genesisSvc == nil || s.autonomousSvc == nil {
		return
	}

	if s.genesisTracker != nil {
		evolveTask := &genesis.EvolutionTask{
			Evolver: s.genesisEvolver,
			Logger:  s.logger,
			// RSI P1 materialization rides the first real evolution tick:
			// autonomous tasks only Run() after Service.Start(), which unit
			// tests (including the method-registry snapshot, which builds the
			// full wiring) never call — so no test can write production
			// state. The production-state gate additionally covers
			// dev/live-test instances (same invariant as memory-backup).
			Bootstrap: func() {
				home, err := os.UserHomeDir()
				if err != nil {
					return
				}
				if _, prod := s.productionStateDir(home); prod {
					s.genesisMeta.MaterializeDefaults(skilllifecycle.DefaultMetaArtifacts())
				}
			},
		}
		s.autonomousSvc.RegisterTask(evolveTask)
		// RSI P2 slow loop (propose-only): weekly, one meta-artifact revision
		// proposal per cycle, alternating producer/evaluator epochs. Never
		// touches live artifacts — writes <name>.proposed + the ledger.
		s.autonomousSvc.RegisterTask(&genesis.MetaEvolutionTask{
			Evolver: s.genesisEvolver,
			Meta:    s.genesisMeta,
			Tracker: s.genesisTracker,
			Logger:  s.logger,
			// Surface each revision as a work-feed card (auto-adopted
			// notification with 되돌리기 veto, or the propose-only decision card
			// when the kill switch is off), and revert-watch notifications.
			OnProposal:        s.postMetaProposalCard,
			OnProposalExpired: s.settleMetaProposalCard,
			OnReverted:        s.postMetaRevertedCard,
			OnDriftFreeze:     s.postDriftFreezeCard,
			// RSI P5-5: advisory runtime-health evidence — grounds the producer's
			// prose on operator-experienced latency/reliability (p95 agentMs,
			// error/timeout/tool-error rates). Sourced from the shared agentlog
			// writer; no gate reads it.
			RuntimeHealth: s.metaRuntimeHealthEvidence,
			// RSI P5-5: advisory codebase-health evidence — grounds the producer's
			// prose on the accepted structural quality state (overall score,
			// weakest pillars). Sourced from the checked-in health-v2 baseline.
			QualityBench: s.metaQualityBenchEvidence,
			// RSI P5-5: advisory RSI Bench evidence — process+utility composite.
			RSIBench: s.metaRSIBenchEvidence,
			// RSI P5-4 slice 2: the genesis-epoch shadow bench executes both
			// prompts on the PRODUCTION genesis model (never persists, no
			// daily-cap interaction).
			GenesisGen: s.genesisSvc.ShadowGenerate,
		})
		s.autonomousSvc.RegisterTask(&genesis.SkillCuratorTask{
			Tracker: s.genesisTracker,
			Logger:  s.logger,
			Config:  genesis.SkillCuratorConfigFromEnv(),
		})

		// Deterministic bench growth: retro-extract held-out validation cases
		// from recent real skill-use transcripts until each actively-used skill
		// holds a minimum corpus (validation_backfill_task.go). Without this the
		// behavioral held-out gate stays inert on skills whose capture-time
		// extraction never fired.
		s.autonomousSvc.RegisterTask(skilllifecycle.NewValidationBackfillTask(
			skilllifecycle.NewBackend(skilllifecycle.BackendConfig{
				Genesis:     s.genesisSvc,
				Evolver:     s.genesisEvolver,
				Tracker:     s.genesisTracker,
				Transcripts: s.genesisTranscripts,
				Logger:      s.logger,
				// Same lightweight relevance classifier as the real-time capture:
				// keep consulted-but-off-topic sessions out of the backfilled corpus.
				RelevanceClient: s.modelRegistry.Client(modelrole.RoleLightweight),
				RelevanceModel:  s.modelRegistry.Model(modelrole.RoleLightweight),
			}),
			s.logger,
		))

		// Synthetic exercise lane (genesis/workout.go): replay covered skills'
		// own held-out cases against their CURRENT bodies on the local replay
		// executor — a week of real-usage failure signal, overnight. Shares the
		// replay flag with the behavioral gate; workout evidence is quarantined
		// from real-usage stats by Source.
		//
		// Production-state-dir gated (unlike evolve/curator/backfill): tracker
		// paths honor DENEB_STATE_DIR, but workout still makes live model calls
		// and writes SYNTHETIC failure rows — keep it off outside the production
		// state dir (same invariant as memory-backup / wiki-research).
		workoutHome, _ := os.UserHomeDir()
		_, isProdState := s.productionStateDir(workoutHome)
		// P3 label food factory (judge-accuracy standing lane): planted-defect
		// replay through the LIVE judge + deterministic false-reject mining.
		// Same gate as workout — live model calls against the production judge.
		if isProdState {
			s.autonomousSvc.RegisterTask(&genesis.JudgeAccuracyTask{
				Evolver: s.genesisEvolver,
				Meta:    s.genesisMeta,
				Tracker: s.genesisTracker,
				Logger:  s.logger,
			})
			// Graduation-ladder watch: fires a feed card once per row
			// transition into READY (evidence met — operator decision
			// available). Prod-gated: writes the shared snapshot and posts
			// operator-facing cards.
			s.autonomousSvc.RegisterTask(&genesis.LadderWatchTask{
				Tracker: s.genesisTracker,
				Logger:  s.logger,
				OnReady: s.postLadderReadyCard,
				// Operator directive 2026-07-14: evidence-met unlocks execute
				// automatically; this card is the notification + 재잠금 veto.
				OnGraduated: s.postGraduationCard,
			})
			// Self-correction Trust Inbox watch: one approve/reject card per
			// NEW proposed candidate, whatever producer recorded it (chat tool,
			// evolver mining, runtime-error mining). Prod-gated: writes the
			// shared watch snapshot and posts operator-facing cards.
			s.autonomousSvc.RegisterTask(&genesis.SelfCorrectionWatchTask{
				Tracker: s.genesisTracker,
				Logger:  s.logger,
				OnNew:   s.postSelfCorrectionCard,
			})
			// Adversarial coverage: deterministically mutate each skill body
			// (section drops, tool-reference drops) and author the held-out
			// cases that catch uncaught mutations — "harder tests, found
			// automatically". No LLM; prod-gated because it writes shared
			// validation state.
			s.autonomousSvc.RegisterTask(&genesis.AdversarialCoverageTask{
				Evolver: s.genesisEvolver,
				Tracker: s.genesisTracker,
				Logger:  s.logger,
			})
			// Runtime-error mining: recurring, code-actionable gateway errors in
			// the live error ring become propose-only scope=code candidates — the
			// second, always-available L4 fuel source (evolve-tool-gap is rare).
			// Propose-only and staged: coding-dispatch does NOT yet accept the
			// runtime-error source, so candidates accumulate for review before any
			// autonomous source edit is dispatched.
			if s.logCapture != nil {
				s.autonomousSvc.RegisterTask(&genesis.RuntimeErrorMiningTask{
					// Warn+, not Error-only: graceful degradation downgrades
					// real defects (a fallback-rescued model failure logs
					// Warn), so an Error-only feed goes blind exactly where
					// the system self-heals. The task applies a stricter
					// recurrence floor to warn-only signatures.
					ErrorLines: func(limit int) []observe.LogLine {
						r := s.logCapture.Ring()
						if r == nil {
							return nil
						}
						return r.Query(observe.QueryOpts{MinLevel: slog.LevelWarn, Limit: limit})
					},
					Tracker: s.genesisTracker,
					Logger:  s.logger,
				})
			}
			// Curriculum lane (RSI P5-1): coverage-gap demand injection for the
			// fast loop — proposes at most one missing capability per cycle with
			// validation cases authored first, filed as a route=genesis
			// opportunity for the existing reviews to route. Propose-only;
			// prod-gated for the same reason as the lanes above (live LLM call +
			// shared genesis writes).
			s.autonomousSvc.RegisterTask(&genesis.CurriculumTask{
				Evolver: s.genesisEvolver,
				Tracker: s.genesisTracker,
				Logger:  s.logger,
				// RSI P5-1 slice-2: widen demand mining beyond tracker-local
				// evidence — the producer sees the operator's active work
				// (feed items) and wiki environment (counterparty domains).
				EnvDigest: s.curriculumEnvDigest,
			})

			// Genesis backlog drain: route=genesis opportunities used to pile
			// up with no consumer (creation relied on a nudged session
			// executing inline — 1/10 follow-through observed 2026-07-18);
			// this deterministically pushes the most-recurring demand through
			// the existing generation gates, one attempt per run.
			s.autonomousSvc.RegisterTask(&genesis.GenesisBacklogDrainTask{
				Tracker: s.genesisTracker,
				Genesis: s.genesisSvc,
				Logger:  s.logger,
			})
		}
		if replayExecutorEnabled() && isProdState {
			workoutEngine := genesis.NewSkillValidationEngine(s.genesisTracker, s.logger)
			workoutEngine.SetExecutor(
				s.modelRegistry.Client(modelrole.RoleLightweight),
				s.modelRegistry.Model(modelrole.RoleLightweight),
			)
			s.autonomousSvc.RegisterTask(&genesis.SkillWorkoutTask{
				Engine:  workoutEngine,
				Tracker: s.genesisTracker,
				Catalog: s.skillCatalog,
				Logger:  s.logger,
			})
			s.autonomousSvc.RegisterTask(&genesis.SkillAblationTask{
				Engine:  workoutEngine,
				Tracker: s.genesisTracker,
				Catalog: s.skillCatalog,
				Logger:  s.logger,
			})
		} else if replayExecutorEnabled() {
			s.logger.Info("genesis: skill-workout/ablation lanes disabled (non-production state dir)")
		}

		// Event-driven evolve: after N new skills accumulate, run a cycle in
		// the background instead of waiting for the 6h periodic task. The
		// periodic task remains a backstop; EvolveUnderperformers is TryLock-
		// serialized so the two paths never overlap, and minGap suppresses a
		// re-fire too soon after a cycle.
		s.genesisTracker.SetEvolveTrigger(func() {
			ctx, cancel := context.WithTimeout(s.ShutdownCtx(), 10*time.Minute)
			defer cancel()
			_ = evolveTask.Run(ctx)
		}, genesis.DefaultEvolveEventThreshold, 30*time.Minute)

		// Post-evolve rollback: revert an evolution that regresses (N consecutive
		// post-evolve failures restore the skill from its backup). Closes the
		// evolve loop — generate -> gate -> cross-model judge -> watch -> revert.
		s.genesisTracker.SetRollback(s.genesisEvolver.RollbackSkillWithResult, genesis.DefaultRollbackThreshold)
	}
}
