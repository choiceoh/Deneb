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

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/domainbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/platbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/infrabind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

func (s *Server) registerProcessApprovalSideEffect(hub *rpcutil.GatewayHub) {
	if s.processes == nil {
		return
	}
	// When a tool execution requires approval, create a request, broadcast it
	// to connected clients, and wait for the bounded decision.
	s.processes.SetApprover(func(ctx context.Context, req infrabind.ExecRequest) bool {
		ar := s.approvals.CreateRequest(domainbind.CreateRequestParams{
			Command:     req.Command,
			CommandArgv: req.Args,
			Cwd:         req.WorkingDir,
		})
		approvalWire, _ := svcbind.PayloadOf(map[string]any{
			"id":      ar.ID,
			"command": req.Command,
			"args":    req.Args,
		})
		hub.Broadcast("exec.approval.requested", approvalWire)
		waitCh := s.approvals.WaitForDecision(ar.ID)
		timer := time.NewTimer(30 * time.Second)
		defer timer.Stop()
		select {
		case <-waitCh:
			resolved := s.approvals.Get(ar.ID)
			if resolved != nil && resolved.Decision != nil {
				return *resolved.Decision == domainbind.DecisionAllowOnce || *resolved.Decision == domainbind.DecisionAllowAlways
			}
			return false
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		}
	})
}

func (s *Server) configureAutonomousWorkflow(hub *rpcutil.GatewayHub) {
	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = domainbind.NewService(s.logger)
	s.autonomousSvc.SetBehaviorLog(s.agentLogWriter)
	// Persist last-run times so deploy restarts do not reset daily/weekly tasks.
	if home, err := os.UserHomeDir(); err == nil {
		s.autonomousSvc.SetStateDir(filepath.Join(home, ".deneb"))
	}

	// Prompt snapshots must be configured before the service can begin a dream
	// cycle, otherwise a restart loses byte-identical APC prompt reuse.
	pipebind.ConfigurePromptSnapshots(infrabind.ResolveStateDir(), s.logger)
	s.autonomousSvc.OnEvent(func(event domainbind.CycleEvent) {
		dreamWire, _ := svcbind.PayloadOf(event)
		hub.Broadcast("dreaming.cycle", dreamWire)
		if event.Type == "dreaming_completed" {
			s.postDreamWorkfeedCard(event.DreamReport)
		}
	})
	if n := s.proactiveRelay.NotifierForSession(svcbind.DreamWorkSessionKey); n != nil {
		s.autonomousSvc.SetNotifier(n)
	}
	// Keep SetDreamer last: it starts the timer loop and can immediately emit.
	if s.wikiDreamer != nil {
		s.autonomousSvc.SetDreamer(s.wikiDreamer)
	}
}

func workflowHomeDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return homeDir
}

func (s *Server) registerHeartbeatWorkflowTasks(homeDir string) {
	s.autonomousSvc.RegisterTask(svcbind.NewBootTask(
		s.chatHandler, s.activity, s.logger, homeDir,
	))
	s.autonomousSvc.RegisterTask(svcbind.NewHeartbeatTask(svcbind.TaskConfig{
		ChatHandler: s.chatHandler,
		Activity:    s.activity,
		Logger:      s.logger,
		HomeDir:     homeDir,
		CollectSignals: svcbind.CombineCollectors(
			svcbind.CalendarSignalCollector(svcbind.ResolveCalendarClient),
			svcbind.TodoDeadlineCollector(),
			svcbind.DealDeadlineSignalCollector(func() *domainbind.WikiStore { return s.wikiStore }),
		),
		SignalConfig:              domainbind.SignalConfigForThreshold(svcbind.ProactiveEscalateThreshold(s.logger)),
		ProposedSelfCoding:        s.proposedSelfCodingFingerprint,
		DispatchBacklogSelfCoding: s.dispatchBacklogSelfCodingCount,
		PromoteRecurrences:        s.promoteSelfCodingRecurrences,
		PromoteClusters:           s.promoteSelfCodingClusters,
		SelfImproveSignals:        s.selfCodingFunnelSignals,
		SelfImproveEvidence:       s.selfCodingFailureEvidence,
		IdleSkillReview:           s.idleSkillReviewLaneIfProduction(homeDir),
	}))
}

func (s *Server) proposedSelfCodingFingerprint() (int, string) {
	tracker := s.genesisTracker
	if tracker == nil {
		return 0, ""
	}
	recs, err := tracker.RecentSelfCorrectionCandidates("", domainbind.SelfCorrectionStatusProposed, 20)
	if err != nil || len(recs) == 0 {
		return 0, ""
	}
	newest := recs[0]
	for _, record := range recs[1:] {
		if record.UpdatedAt > newest.UpdatedAt {
			newest = record
		}
	}
	return len(recs), fmt.Sprintf("%d:%s:%d", len(recs), newest.ID, newest.UpdatedAt)
}

// dispatchBacklogSelfCodingCount counts accepted, dispatchable code candidates
// that coding-dispatch has NOT yet claimed (no blocking marker). Used to
// suppress the generator sweep while the consumer lane is backed up.
// Tracker read errors fail-closed (return 1) so a broken ledger cannot re-open
// mining while backlog visibility is unknown (bot review #3612).
func (s *Server) dispatchBacklogSelfCodingCount() int {
	tracker := s.genesisTracker
	if tracker == nil {
		return 0
	}
	recs, err := tracker.RecentSelfCorrectionCandidates("", domainbind.SelfCorrectionStatusAccepted, 100)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("dispatch backlog: self-correction read failed — suppressing sweep", "error", err)
		}
		return 1
	}
	n := 0
	for _, rec := range recs {
		if rec.Scope != "code" {
			continue
		}
		if !domainbind.SourceAutoDispatches(rec.Source) {
			continue
		}
		if tracker.DispatchMarkerBlocks(rec.ID) {
			continue
		}
		n++
	}
	return n
}

func (s *Server) promoteSelfCodingRecurrences() (int, error) {
	tracker := s.genesisTracker
	if tracker == nil {
		return 0, nil
	}
	return tracker.PromoteTargetRecurrenceCandidates()
}

func (s *Server) promoteSelfCodingClusters() (int, error) {
	tracker := s.genesisTracker
	if tracker == nil {
		return 0, nil
	}
	return tracker.PromoteFailureClusterCandidates()
}

func (s *Server) selfCodingFunnelSignals() (domainbind.SelfCorrectionFunnelSummary, int) {
	tracker := s.genesisTracker
	if tracker == nil {
		return domainbind.SelfCorrectionFunnelSummary{}, 0
	}
	return tracker.SelfCorrectionFunnel(), tracker.SelfHarnessSignals().TargetRecurrences7d
}

func (s *Server) selfCodingFailureEvidence(limit int) []domainbind.FailureClusterSummary {
	tracker := s.genesisTracker
	if tracker == nil {
		return nil
	}
	return tracker.FailureEvidenceClusters(limit)
}

func (s *Server) registerGoalWorkflowTask(homeDir string) {
	goalStateDir := ""
	if homeDir != "" {
		goalStateDir = filepath.Join(homeDir, ".deneb")
	}
	goalStore := domainbind.NewGoalsStore(goalStateDir, s.logger)
	domainbind.SetDefault(goalStore)
	s.autonomousSvc.RegisterTask(svcbind.NewGoalLoopTask(
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
}

func (s *Server) registerMeetingHarvestWorkflow(homeDir string) {
	if os.Getenv("DENEB_MEETING_HARVEST_DISABLE") == "1" {
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.meetingHarvest = svcbind.NewHarvestService(
		func(text string) (bool, error) {
			return s.proactiveRelay.RelayNativeToOptions("", text,
				svcbind.Options{MirrorTranscript: true})
		},
		svcbind.ResolveCalendarClient,
		s.matchMeetingProjectName,
		filepath.Join(stateDir, svcbind.HarvestStateFile),
		s.logger,
	)
	// Silent attendance record: log that a matched meeting happened to its
	// project, regardless of the ask cap or a reply. Resolves the project TYPED
	// from the calendar text (UniqueProjectInText → rep path), NOT the ask flow's
	// name string — a counterparty-only match has no single project ref, so it
	// returns handled=true (deliberate skip, nothing to write) rather than
	// re-interpreting a colliding name as a project. Returns false only on a
	// transient write failure so the harvest retries.
	s.meetingHarvest.SetAttendanceRecorder(func(ev platbind.Event) bool {
		st := s.wikiStore
		if st == nil {
			return true
		}
		ref, ok := st.UniqueProjectInText(svcbind.MeetingMatchText(ev))
		if !ok {
			return true // no single project — skip, don't retry
		}
		return svcbind.RecordMeetingAttendanceByPath(st, ref.Path, ev.Summary,
			ev.End.In(dentime.Location()).Format("2006-01-02"))
	})
	s.meetingHarvest.Start(s.ShutdownCtx())
}

func (s *Server) matchMeetingProjectName(text string) string {
	st := s.wikiStore
	if st == nil {
		return ""
	}
	if ref, ok := st.UniqueProjectInText(text); ok {
		return ref.Name
	}
	if counterparties := st.MatchCounterpartiesInText(text, 1); len(counterparties) > 0 {
		return counterparties[0].Name
	}
	return svcbind.LooseUniqueNameMatch(text,
		svcbind.KnownNames(st.KnownProjects(), st.KnownCounterparties()))
}

func (s *Server) registerPlaudWorkflow(homeDir string) {
	if os.Getenv(svcbind.PlaudDisableEnv) == "1" {
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok || s.wikiStore == nil {
		return
	}
	s.plaudRecordings = svcbind.NewPlaudService(
		s.callPlaudTool,
		s.completePlaudAnalysis,
		s.completePlaudStageOne,
		s.projectCandidatesFn(),
		s.businessTopicKnowledge,
		s.wikiStore.WritePage,
		s.wikiStore.AppendProjectStatusLine,
		s.relayMeetingReport,
		filepath.Join(stateDir, svcbind.PlaudStateFile),
		s.logger,
	)
	s.plaudRecordings.Start(s.ShutdownCtx())
}

func (s *Server) callPlaudTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	client := s.externalMCP["plaud"]
	if client == nil {
		return "", errors.New("unknown tool: plaud mcp not configured")
	}
	return client.CallTool(ctx, strings.TrimPrefix(name, "plaud_"), args)
}

func (s *Server) completePlaudAnalysis(ctx context.Context, system, user string, maxTokens int) (string, error) {
	client, model, _, _ := s.mailAnalysisModels()
	if client == nil {
		return "", errors.New("main-role model unavailable")
	}
	return client.Complete(ctx, plaudChatRequest(model, system, user, maxTokens))
}

func (s *Server) completePlaudStageOne(ctx context.Context, system, user string, maxTokens int) (string, error) {
	_, _, client, model := s.mailAnalysisModels()
	if client == nil {
		return "", errors.New("stage-1 model unavailable")
	}
	return client.Complete(ctx, plaudChatRequest(model, system, user, maxTokens))
}

func plaudChatRequest(model, system, user string, maxTokens int) aibind.ChatRequest {
	return aibind.ChatRequest{
		Model:     model,
		System:    aibind.SystemString(system),
		Messages:  []aibind.Message{aibind.NewTextMessage("user", user)},
		MaxTokens: maxTokens,
		Thinking:  &aibind.ThinkingConfig{Type: "disabled"},
	}
}

func (s *Server) businessTopicKnowledge() string {
	dir := svcbind.TopicsDir()
	if dir == "" {
		return ""
	}
	return pipebind.LoadTopicKnowledge("", dir, "업무", "").Content
}

func (s *Server) relayMeetingReport(text string) (bool, error) {
	return s.proactiveRelay.RelayNativeToOptions("", text,
		svcbind.Options{WorkFeedSource: domainbind.SourceMeetingReport})
}

func (s *Server) registerModelMaintenanceWorkflows() {
	if s.modelRegistry == nil || s.agentLogWriter == nil {
		return
	}
	var notify func(ctx context.Context, msg string) error
	if notifier := s.proactiveRelay.NotifierForSession(svcbind.NativeWorkSessionKey); notifier != nil {
		notify = notifier.Notify
	}
	s.modelMaintenance = svcbind.NewModelMaintenance(svcbind.ModelMaintenanceDeps{
		Logs:      s.agentLogWriter,
		Registry:  s.modelRegistry,
		Summaries: s.polarisStore,
		Capture:   s.logCapture,
		StateDir:  infrabind.ResolveStateDir(),
		Notify:    notify,
		Logger:    s.logger,
	})
	for _, task := range s.modelMaintenance.Tasks() {
		s.autonomousSvc.RegisterTask(task)
	}
}

func (s *Server) registerFileSemanticIndexWorkflow() {
	if s.fileSemindex == nil {
		return
	}
	if task := s.fileSemindex.Task(); task != nil {
		s.autonomousSvc.RegisterTask(task)
	}
}

func (s *Server) registerMailIngestWorkflows() {
	cfgSnap, _ := infrabind.LoadConfigFromDefaultPath()
	s.initGmailPoll(cfgSnap)
	s.initLMTPServer(cfgSnap)
}

func (s *Server) registerCalendarBriefingWorkflow() {
	s.calendarBriefing = svcbind.NewCalendarBriefingService(
		func(text string) (bool, error) { return s.proactiveRelay.RelayNative(text) },
		svcbind.ResolveCalendarClient,
		s.logger,
	)
	if s.calendarBriefing != nil {
		s.calendarBriefing.EnableEnrichment(
			func() *domainbind.WikiStore { return s.wikiStore },
			s.logger,
		)
	}
	s.calendarBriefing.Start(s.ShutdownCtx())
}

func (s *Server) registerRoleHealthWorkflow() {
	if s.modelRegistry == nil || s.roleHealth != nil {
		return
	}
	s.roleHealth = svcbind.NewRoleHealth(
		s.modelRegistry,
		s.logger,
		func(event string, payload svcbind.EventPayload) {
			if s.broadcaster != nil {
				s.broadcaster.Broadcast(event, payload)
			}
		},
		filepath.Join(s.denebDir, "role_health.json"),
	)
	s.roleHealth.Start(s.ShutdownCtx())
}
