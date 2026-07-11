package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modeltuner"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/regressionwatch"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compaction"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/compactuner"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/goalloop"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	runtimemeeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// resolveFeedWorkModel returns the display name of the model behind proactive
// 업무 feed reports — the main agent-turn model. Cron morning letter, mail
// analysis synthesis, heartbeat, goal, and event ingest all run as main-role
// turns, so the main model is the "작업 모델" for the feed. Returns "" when the
// model registry is unwired (older tests), which leaves the feed footer off.
func (s *Server) resolveFeedWorkModel() string {
	if s.modelRegistry == nil {
		return ""
	}
	return s.modelRegistry.Model(modelrole.RoleMain)
}

// registerWorkflowSideEffects wires non-RPC business logic: process approval
// callbacks, autonomous/dreaming service, native notifiers, and memory flush.
// All RPC domain registrations (approval, agent CRUD) are now
// handled by registerEarlyMethods via hub adapters.
func (s *Server) registerWorkflowSideEffects(hub *rpcutil.GatewayHub) {
	// Wire process approval callback using the Go approval store directly.
	// When a tool execution requires approval, create an approval request,
	// broadcast it to WS clients, and wait for a decision.
	if s.processes != nil {
		s.processes.SetApprover(func(ctx context.Context, req process.ExecRequest) bool {
			ar := s.approvals.CreateRequest(approval.CreateRequestParams{
				Command:     req.Command,
				CommandArgv: req.Args,
				Cwd:         req.WorkingDir,
			})
			hub.Broadcast("exec.approval.requested", map[string]any{
				"id":      ar.ID,
				"command": req.Command,
				"args":    req.Args,
			})
			// Wait for decision with timeout.
			waitCh := s.approvals.WaitForDecision(ar.ID)
			timer := time.NewTimer(30 * time.Second)
			defer timer.Stop()
			select {
			case <-waitCh:
				resolved := s.approvals.Get(ar.ID)
				if resolved != nil && resolved.Decision != nil {
					return *resolved.Decision == approval.DecisionAllowOnce || *resolved.Decision == approval.DecisionAllowAlways
				}
				return false
			case <-ctx.Done():
				return false
			case <-timer.C:
				return false
			}
		})
	}

	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = autonomous.NewService(s.logger)
	s.autonomousSvc.SetBehaviorLog(s.agentLogWriter)

	// Persist task last-run times under ~/.deneb so periodic intervals survive
	// the frequent auto-deploy SIGUSR1 restarts. Without this, every restart
	// re-runs all tasks 30s in, defeating 24h (boot) and weekly (evolution)
	// schedules.
	if home, err := os.UserHomeDir(); err == nil {
		s.autonomousSvc.SetStateDir(filepath.Join(home, ".deneb"))
	}

	// Persist per-session frozen system-prompt snapshots (tier-1 wiki, context
	// files, topic knowledge) so the same SIGUSR1 restarts don't force a
	// per-session vLLM APC re-prefill: the restored snapshot reproduces the
	// system prompt byte-for-byte, keeping the engine's KV cache for the tool
	// schemas + history valid. DENEB_STATE_DIR-aware so a dev gateway never
	// writes into the production file. Loaded in restoreAndWakeSessions's
	// goroutine (server_lifecycle.go). See chat/prompt_snapshot_persist.go.
	chat.ConfigurePromptSnapshots(config.ResolveStateDir(), s.logger)

	// Broadcast dreaming events to SSE clients, and surface completed
	// cycles that actually changed pages as a work-feed card — the proposal
	// JSON always existed but had no user-facing surface.
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		hub.Broadcast("dreaming.cycle", event)
		if event.Type == "dreaming_completed" {
			s.postDreamWorkfeedCard(event.DreamReport)
		}
	})

	// Wire the proactive relay as the dreaming notifier, bound to a dedicated
	// client:main:dream sub-session. Aurora Dream lifecycle messages (often "변경
	// 없음" or diagnostics) land in their own native conversation instead of the
	// primary 업무 feed (client:main); a follow-up like "방금 뭔 얘기야?" opened there is
	// still answered with the dream delivery in view.
	if n := s.proactiveRelay.NotifierForSession(proactive.DreamWorkSessionKey); n != nil {
		s.autonomousSvc.SetNotifier(n)
	}

	// Wire the wiki dreamer for autonomous diary → wiki consolidation — LAST:
	// SetDreamer starts the dream timer loop, whose immediate overdue check can
	// finish a short cycle right away, so the event listener and notifier above
	// must already be in place or the first cycle's events would be dropped.
	if s.wikiDreamer != nil {
		s.autonomousSvc.SetDreamer(s.wikiDreamer)
	}

	// Register boot task: on startup (and daily thereafter), runs a full
	// agent turn using ~/.deneb/BOOT.md content for proactive initialization.
	if s.chatHandler != nil {
		homeDir := ""
		if h, err := os.UserHomeDir(); err == nil {
			homeDir = h
		}
		s.autonomousSvc.RegisterTask(runtimeheartbeat.NewBootTask(
			s.chatHandler, s.activity, s.logger, homeDir,
		))

		// Register heartbeat task: every 30 minutes during active hours
		// (08:00–23:00 Asia/Seoul), checks ~/.deneb/HEARTBEAT.md for
		// user-defined tasks and executes them autonomously.
		s.autonomousSvc.RegisterTask(runtimeheartbeat.NewTask(runtimeheartbeat.TaskConfig{
			ChatHandler: s.chatHandler,
			Activity:    s.activity,
			Logger:      s.logger,
			HomeDir:     homeDir,
			// Proactive signal augmentation: calendar conflicts / imminent
			// meetings plus due to-dos (dream-captured open loops included)
			// surface into the heartbeat turn. Best-effort; no-op when the
			// sources are absent. See heartbeat_signals.go / open_loop_sink.go.
			CollectSignals: runtimeheartbeat.CombineCollectors(
				runtimeheartbeat.CalendarSignalCollector(runtimemeeting.ResolveCalendarClient),
				runtimeheartbeat.TodoDeadlineCollector(),
				runtimeheartbeat.DealDeadlineSignalCollector(func() *wiki.Store { return s.wikiStore }),
			),
			SignalConfig: autonomous.SignalConfigForThreshold(configresolve.ProactiveEscalateThreshold(s.logger)),
			// Self-coding review lane: pending proposed candidates from the
			// genesis tracker (read per tick — the tracker late-binds during
			// genesis init, and the field read keeps the closure ordering-immune).
			ProposedSelfCoding: func() (int, string) {
				tracker := s.genesisTracker
				if tracker == nil {
					return 0, ""
				}
				recs, err := tracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusProposed, 20)
				if err != nil || len(recs) == 0 {
					return 0, ""
				}
				newest := recs[0]
				for _, r := range recs[1:] {
					if r.UpdatedAt > newest.UpdatedAt {
						newest = r
					}
				}
				return len(recs), fmt.Sprintf("%d:%s:%d", len(recs), newest.ID, newest.UpdatedAt)
			},
			// Active capture: recurrence promotion runs deterministically each
			// tick; the sweep lane asks the turn to mine signals only when the
			// queue is empty (heartbeat_selfimprove_sweep.go).
			PromoteRecurrences: func() (int, error) {
				tracker := s.genesisTracker
				if tracker == nil {
					return 0, nil
				}
				return tracker.PromoteTargetRecurrenceCandidates()
			},
			// Active capture, LLM-independent: the top recurring failure clusters
			// become candidates each tick so the queue is fed even when the sweep
			// turn ignores its nudge (heartbeat_selfimprove_sweep.go).
			PromoteClusters: func() (int, error) {
				tracker := s.genesisTracker
				if tracker == nil {
					return 0, nil
				}
				return tracker.PromoteFailureClusterCandidates()
			},
			SelfImproveSignals: func() (genesis.SelfCorrectionFunnelSummary, int) {
				tracker := s.genesisTracker
				if tracker == nil {
					return genesis.SelfCorrectionFunnelSummary{}, 0
				}
				return tracker.SelfCorrectionFunnel(), tracker.SelfHarnessSignals().TargetRecurrences7d
			},
			SelfImproveEvidence: func(limit int) []genesis.FailureClusterSummary {
				tracker := s.genesisTracker
				if tracker == nil {
					return nil
				}
				return tracker.FailureEvidenceClusters(limit)
			},
			// Idle review backstop: fires a fenced Propus review of the most
			// recent real session when none has completed for hours (user
			// away / deploy churn) — see heartbeat_idle_review.go.
			// Production-state gated (same invariant as memory-backup): the
			// lane reads ~/.deneb transcripts and writes genesis liveness, so
			// a dev/live-test instance must never run live reviews into
			// production Propus state.
			IdleSkillReview: s.idleSkillReviewLaneIfProduction(homeDir),
		}))

		// Register the goal loop (Ralph loop): advances active standing goals
		// (set via /goal) one run at a time while the user is idle, judging
		// completion with the lightweight model and enforcing a per-goal
		// idempotency ledger so a re-driven run never repeats a destructive
		// action. State persists in ~/.deneb/goals.json (beside
		// autonomous_state.json) so a standing goal survives the auto-deploy
		// restarts. The store singleton is shared with the /goal slash command.
		goalStateDir := ""
		if homeDir != "" {
			goalStateDir = filepath.Join(homeDir, ".deneb")
		}
		goalStore := goals.NewStore(goalStateDir, s.logger)
		goals.SetDefault(goalStore)
		s.autonomousSvc.RegisterTask(goalloop.NewTask(
			s.chatHandler,
			goalStore,
			s.activity,
			s.logger,
			func(ctx context.Context, sessionKey, msg string) error {
				n := s.proactiveRelay.NotifierForSession(sessionKey)
				if n == nil {
					return nil
				}
				return n.Notify(ctx, msg)
			},
		))

		// Daily offsite memory backup: tar.gz of the memory stores streamed
		// over ssh to the storage node (the NFS mount is read-only from this
		// host). Only registered for the production state dir — dev live-test
		// instances (DENEB_STATE_DIR=/tmp/...) must not ship archives.
		s.registerMemoryBackupTask(homeDir)

		// Project-wiki deep research: every 6h, pick one 프로젝트 page and run an
		// agent turn that re-investigates it from Deneb's own internal sources
		// (mail archive, polaris recall, knowledge graph, contacts, linked wiki
		// pages) and updates it in place. Internal-only (no web), silent, and
		// round-robin across project pages. Production state dir only — see
		// registerWikiResearchTask.
		s.registerWikiResearchTask(homeDir)

		// Wiki reviewer: every 2h, review pages created/updated since the last
		// pass for near-duplicates (skill-reviewer pattern for memory writes) —
		// deterministic candidate gather → one lightweight JSON verdict →
		// capped, git-snapshotted merges. Production state dir only.
		s.registerWikiReviewTask(homeDir)

		// Post-meeting harvest: after a project/counterparty-linked calendar
		// event ends, push ONE follow-up question ("결과 한 줄로 알려주세요")
		// into the main transcript so the outcome lands in the wiki flywheel
		// instead of evaporating. Deterministic matchers, 2/day cap, 08–21 KST
		// — see meeting_harvest.go. PRODUCTION ONLY: dev live-test gateways
		// share the prod transcript dir (homeDir-based), so a dev instance
		// running this would double-ask the operator with its own state.
		if os.Getenv("DENEB_MEETING_HARVEST_DISABLE") != "1" {
			if stateDir, ok := s.productionStateDir(homeDir); ok {
				s.meetingHarvest = runtimemeeting.NewHarvestService(
					// mirrorTranscript: the question must land in the
					// client:main transcript too — the feed answer path sends
					// only the user's typed reply as the next prompt, and the
					// filing loop needs the question (with project name) right
					// above it in context.
					func(text string) (bool, error) {
						return s.proactiveRelay.RelayNativeToOptions("", text,
							proactive.Options{MirrorTranscript: true})
					},
					runtimemeeting.ResolveCalendarClient,
					func(text string) string {
						st := s.wikiStore
						if st == nil {
							return ""
						}
						// Unique-by-specificity, not top-of-list: a bare 거래처
						// mention matches every project of that client at the
						// same key length, and an arbitrary first pick would
						// mis-file — on a tie fall through to the counterparty
						// ledger match, which IS the right anchor for a
						// client-level mention.
						if ref, ok := st.UniqueProjectInText(text); ok {
							return ref.Name
						}
						if cps := st.MatchCounterpartiesInText(text, 1); len(cps) > 0 {
							return cps[0].Name
						}
						// Terse real-world titles ("비금도 … 견학", "JA 이용원
						// 상무") never contain the full compound name — recover
						// via unique token containment over projects+ledgers
						// jointly (ambiguous tokens resolve to none).
						return runtimemeeting.LooseUniqueNameMatch(text,
							runtimemeeting.KnownNames(st.KnownProjects(), st.KnownCounterparties()))
					},
					filepath.Join(stateDir, runtimemeeting.HarvestStateFile),
					s.logger,
				)
				s.meetingHarvest.Start(s.ShutdownCtx())
			}
		}

		// Plaud recording analysis: poll the recorder's MCP tools for new
		// recordings and give each meeting the mail-grade treatment —
		// transcript → meeting report (analysis role) → 회의록 wiki page +
		// project status bullet + work-feed card. PRODUCTION ONLY for the
		// same shared-state reason as meeting harvest; where the MCP env is
		// not configured (dev boxes) the tools never register and every tick
		// is a quiet skip. See plaud_recordings.go.
		if os.Getenv(runtimemeeting.PlaudDisableEnv) != "1" {
			if stateDir, ok := s.productionStateDir(homeDir); ok && s.wikiStore != nil {
				s.plaudRecordings = runtimemeeting.NewPlaudService(
					// Direct shared-client call, NOT chatToolRegistry.Execute:
					// the executor's head/tail truncation+spillover protects
					// CHAT context, but this pipeline must read the FULL
					// transcript (round-2 live check: mid-meeting content was
					// spilled and the synthesis saw a gutted JSON). The #3218
					// shared registry exists exactly for ingest-type consumers.
					func(ctx context.Context, name string, args json.RawMessage) (string, error) {
						client := s.externalMCP["plaud"]
						if client == nil {
							// Same shape as a registry miss so the service's
							// "not wired yet" branch handles it quietly.
							return "", errors.New("unknown tool: plaud mcp not configured")
						}
						return client.CallTool(ctx, strings.TrimPrefix(name, "plaud_"), args)
					},
					// Synthesis = analysis role (user-facing report, cloud OK);
					// resolved per call so model-registry changes apply live.
					// Thinking disabled like every mail synthesis call: a
					// reasoning model otherwise burns the MaxTokens budget on
					// thinking (first-tick incident: 1461/1600 tokens reasoning).
					func(ctx context.Context, system, user string, maxTokens int) (string, error) {
						client, model, _, _ := s.mailAnalysisModels()
						if client == nil {
							return "", errors.New("analysis model unavailable")
						}
						return client.Complete(ctx, llm.ChatRequest{
							Model:     model,
							System:    llm.SystemString(system),
							Messages:  []llm.Message{llm.NewTextMessage("user", user)},
							MaxTokens: maxTokens,
							Thinking:  &llm.ThinkingConfig{Type: "disabled"},
						})
					},
					// Chunk gists = local stage-1 model (model-roles dogma).
					func(ctx context.Context, system, user string, maxTokens int) (string, error) {
						_, _, client, model := s.mailAnalysisModels()
						if client == nil {
							return "", errors.New("stage-1 model unavailable")
						}
						return client.Complete(ctx, llm.ChatRequest{
							Model:     model,
							System:    llm.SystemString(system),
							Messages:  []llm.Message{llm.NewTextMessage("user", user)},
							MaxTokens: maxTokens,
							Thinking:  &llm.ThinkingConfig{Type: "disabled"},
						})
					},
					s.projectCandidatesFn(),
					// 업무 topic knowledge (company/org/people, ~2KB) — the
					// context slice that lifts term correction; fail-open.
					func() string {
						dir := configresolve.TopicsDir()
						if dir == "" {
							return ""
						}
						return prompt.LoadTopicKnowledge("", dir, "업무", "").Content
					},
					s.wikiStore.WritePage,
					s.wikiStore.AppendProjectStatusLine,
					func(text string) (bool, error) {
						return s.proactiveRelay.RelayNativeToOptions("", text,
							proactive.Options{WorkFeedSource: workfeed.SourceMeetingReport})
					},
					filepath.Join(stateDir, runtimemeeting.PlaudStateFile),
					s.logger,
				)
				s.plaudRecordings.Start(s.ShutdownCtx())
			}
		}

		// Model tuner: every 6h, aggregate the last 24h of agent logs by
		// model, auto-apply the bounded output-token floor for models that
		// keep hitting the ceiling, and calibrate newly served vLLM models.
		// The 5 advise-only verdicts (stall / cache_break / latency / fallback
		// / tool_errors) also continue to surface under the native model picker
		// (miniapp_models AdvisoryLines/NoteFor, read from the scorecard); the
		// scorecard remains the durable record. Notify pushes a proactive
		// operator message via the SAME native relay cron/gmail-poll use, bound
		// to client:main — but only when the recommendation set changes and is
		// non-empty (Fingerprint dedup in Run), so a steady state stays quiet
		// instead of pinging every cycle. Scorecard: ~/.deneb/model-stats.json.
		if s.modelRegistry != nil && s.agentLogWriter != nil {
			var tunerNotify func(ctx context.Context, msg string) error
			if n := s.proactiveRelay.NotifierForSession(proactive.NativeWorkSessionKey); n != nil {
				tunerNotify = n.Notify
			}
			s.autonomousSvc.RegisterTask(modeltuner.NewTask(modeltuner.Deps{
				Logs:     s.agentLogWriter,
				Registry: s.modelRegistry,
				// DENEB_STATE_DIR-aware so a dev gateway's tuner never writes
				// into the production ~/.deneb scorecard.
				StatePath: modeltuner.DefaultStatePath(),
				Notify:    tunerNotify,
				Logger:    s.logger,
			}))

			// Regression watch (Stage 1 of the autoresearch cold-start trigger):
			// every 6h, sample operational telemetry (agentlog rates, model-health
			// circuit, error-log spikes — see regressionSources), compare to a
			// rolling baseline, and log regressions. The optimize-GOAL path is still
			// gated until these thresholds are validated against real traffic, but a
			// detected regression now also pushes a proactive operator notification
			// (regressed signal names + deltas) via the SAME native relay, bound to
			// client:main and de-duped per regression set so a standing regression
			// pings once, not every cycle. Baseline: ~/.deneb/regression-baseline.json.
			var regressionNotify func(ctx context.Context, msg string) error
			if n := s.proactiveRelay.NotifierForSession(proactive.NativeWorkSessionKey); n != nil {
				regressionNotify = n.Notify
			}
			s.autonomousSvc.RegisterTask(regressionwatch.NewTask(regressionwatch.Deps{
				Sources:   s.regressionSources(),
				StatePath: regressionwatch.DefaultStatePath(),
				Notify:    regressionNotify,
				Logger:    s.logger,
			}))

			// Compaction tuner (opt-in): refine the summarizer's preservation
			// guidelines from recent summaries' vagueness. Off by default — it
			// auto-edits a prompt, so it ships behind a flag.
			if os.Getenv("DENEB_COMPACTION_TUNER") == "1" && s.polarisStore != nil && s.modelRegistry != nil {
				if lw := s.modelRegistry.Client(modelrole.RoleLightweight); lw != nil {
					var compactNotify func(ctx context.Context, msg string) error
					if n := s.proactiveRelay.NotifierForSession(proactive.NativeWorkSessionKey); n != nil {
						compactNotify = n.Notify
					}
					s.compactTuner = compactuner.NewTask(compactuner.Deps{
						Summaries:  s.polarisStore,
						Guidelines: compaction.NewGuidelineStore(filepath.Join(config.ResolveStateDir(), compaction.GuidelineFileName)),
						Client:     lw,
						Model:      s.modelRegistry.Model(modelrole.RoleLightweight),
						Notify:     compactNotify,
						Logger:     s.logger,
					})
					s.autonomousSvc.RegisterTask(s.compactTuner)
					s.logger.Info("compaction-tuner: enabled (DENEB_COMPACTION_TUNER)")
				}
			}
		}
	}

	// File semantic index: background reindex task (15 min, plus a first cycle
	// shortly after boot via the service's initial-grace). Keeps the BGE-M3 vector
	// index over the file store fresh so semantic file search finds new/changed
	// files. No-op when the index/store/embedding client isn't wired.
	if s.fileSemindex != nil {
		if task := s.fileSemindex.Task(); task != nil {
			s.autonomousSvc.RegisterTask(task)
		}
	}

	// Propus: register autonomous tasks (services created in initGenesisServices).
	s.registerGenesisAutonomousTasks(hub)

	// Gmail polling service: periodic new-email analysis via LLM.
	// Load deneb.json once and share the snapshot across the poll initializers.
	cfgSnap, _ := config.LoadConfigFromDefaultPath()
	s.initGmailPoll(cfgSnap)
	s.initLMTPServer(cfgSnap)

	// Calendar briefing service: D-15min push for upcoming meetings.
	// Delivers to the native client (업무 transcript + live push) via the
	// shared proactive relay. Returns nil and is a no-op when calendar OAuth
	// isn't configured, so safe to wire unconditionally.
	s.calendarBriefing = runtimemeeting.NewCalendarBriefingService(
		func(text string) (bool, error) { return s.proactiveRelay.RelayNative(text) },
		runtimemeeting.ResolveCalendarClient,
		s.logger,
	)
	if s.calendarBriefing != nil {
		// Best-effort context enrichment: per-attendee mail freshness + wiki
		// notes, topic-related recent mail, and a past wiki record. Backed by
		// gmail.DefaultClient and the late-bound wiki store; both degrade to
		// the base D-15 reminder when unavailable. wikiStore is read at call
		// time (set during the session phase) so construction order can't
		// capture a nil store.
		s.calendarBriefing.EnableEnrichment(
			func() *wiki.Store { return s.wikiStore },
			s.logger,
		)
	}
	s.calendarBriefing.Start(s.ShutdownCtx())

	// Periodic auth/liveness probe for role-assigned cloud providers — the
	// fallback role gets no organic traffic, so a dead credential there is
	// invisible until the safety net is actually needed (role_health_watch.go).
	if s.modelRegistry != nil && s.roleHealth == nil {
		s.roleHealth = rolehealth.New(
			s.modelRegistry,
			s.logger,
			func(event string, payload any) {
				if s.broadcaster != nil {
					s.broadcaster.Broadcast(event, payload)
				}
			},
			filepath.Join(s.denebDir, "role_health.json"),
		)
		s.roleHealth.Start(s.ShutdownCtx())
	}
}
