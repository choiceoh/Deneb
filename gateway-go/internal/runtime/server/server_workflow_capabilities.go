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
	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/prompt"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/calendar"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/goalloop"
	runtimeheartbeat "github.com/choiceoh/deneb/gateway-go/internal/runtime/heartbeat"
	runtimemeeting "github.com/choiceoh/deneb/gateway-go/internal/runtime/meeting"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/modelmaintenance"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rolehealth"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/skilllifecycle"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

func (s *Server) registerProcessApprovalSideEffect(hub *rpcutil.GatewayHub) {
	if s.processes == nil {
		return
	}
	// When a tool execution requires approval, create a request, broadcast it
	// to connected clients, and wait for the bounded decision.
	s.processes.SetApprover(func(ctx context.Context, req process.ExecRequest) bool {
		ar := s.approvals.CreateRequest(approval.CreateRequestParams{
			Command:     req.Command,
			CommandArgv: req.Args,
			Cwd:         req.WorkingDir,
		})
		approvalWire, _ := events.PayloadOf(map[string]any{
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

func (s *Server) configureAutonomousWorkflow(hub *rpcutil.GatewayHub) {
	// AuroraDream: memory consolidation service (dreaming-only, no goal cycles).
	s.autonomousSvc = autonomous.NewService(s.logger)
	s.autonomousSvc.SetBehaviorLog(s.agentLogWriter)
	// Persist last-run times under THIS process's state dir (DENEB_STATE_DIR),
	// never hard-coded ~/.deneb — a live-test instance must not rewrite the
	// production autonomous_state.json (and wipe prod-only task keys).
	s.autonomousSvc.SetStateDir(config.ResolveStateDir())

	// Prompt snapshots must be configured before the service can begin a dream
	// cycle, otherwise a restart loses byte-identical APC prompt reuse.
	chat.ConfigurePromptSnapshots(config.ResolveStateDir(), s.logger)
	// User-message tail register: same restart-survival contract for the
	// per-turn tail additions, so post-restart history reloads keep the
	// byte-identical wire form recorded by earlier runs (prompt cache).
	chat.ConfigureTailRegister(config.ResolveStateDir(), s.logger)
	s.autonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		dreamWire, _ := events.PayloadOf(event)
		hub.Broadcast("dreaming.cycle", dreamWire)
		if event.Type == "dreaming_completed" {
			s.postDreamWorkfeedCard(event.DreamReport)
		}
	})
	if n := s.proactiveRelay.NotifierForSession(proactive.DreamWorkSessionKey); n != nil {
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
	s.autonomousSvc.RegisterTask(runtimeheartbeat.NewBootTask(
		s.chatHandler, s.activity, s.logger, homeDir,
	))
	s.autonomousSvc.RegisterTask(runtimeheartbeat.NewTask(runtimeheartbeat.TaskConfig{
		ChatHandler: s.chatHandler,
		Activity:    s.activity,
		Logger:      s.logger,
		HomeDir:     homeDir,
		CollectSignals: runtimeheartbeat.CombineCollectors(
			runtimeheartbeat.CalendarSignalCollector(runtimemeeting.ResolveCalendarClient),
			runtimeheartbeat.TodoDeadlineCollector(),
			runtimeheartbeat.DealDeadlineSignalCollector(func() *wiki.Store { return s.wikiStore }),
		),
		SignalConfig:              autonomous.SignalConfigForThreshold(configresolve.ProactiveEscalateThreshold(s.logger)),
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
	recs, err := tracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusProposed, 20)
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
	recs, err := tracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusAccepted, 100)
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
		if !genesis.SourceAutoDispatches(rec.Source) {
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

func (s *Server) selfCodingFunnelSignals() (genesis.SelfCorrectionFunnelSummary, int) {
	tracker := s.genesisTracker
	if tracker == nil {
		return genesis.SelfCorrectionFunnelSummary{}, 0
	}
	return tracker.SelfCorrectionFunnel(), tracker.SelfHarnessSignals().TargetRecurrences7d
}

func (s *Server) selfCodingFailureEvidence(limit int) []genesis.FailureClusterSummary {
	tracker := s.genesisTracker
	if tracker == nil {
		return nil
	}
	clusters := tracker.FailureEvidenceClusters(limit)
	// tool_retry evidence: deterministic transcript mining of failed→successful
	// tool retries (genesis/retry_correction_miner.go). Lazy so the miner only
	// exists once the sweep actually reads evidence; the call itself is cheap
	// (incremental scan, ≤2/day cadence from the sweep).
	s.retryMinerOnce.Do(func() {
		s.retryMiner = skilllifecycle.NewRetryCorrectionMiner(config.ResolveStateDir(), s.logger)
	})
	return genesis.MergeFailureClusters(clusters, s.retryMiner.MineAndClusters(limit, time.Now()), limit)
}

func (s *Server) registerGoalWorkflowTask(homeDir string) {
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
}

func (s *Server) registerMeetingHarvestWorkflow(homeDir string) {
	if os.Getenv("DENEB_MEETING_HARVEST_DISABLE") == "1" {
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.meetingHarvest = runtimemeeting.NewHarvestService(
		func(text string) (bool, error) {
			return s.proactiveRelay.RelayNativeToOptions("", text,
				proactive.Options{MirrorTranscript: true})
		},
		runtimemeeting.ResolveCalendarClient,
		s.matchMeetingProjectName,
		filepath.Join(stateDir, runtimemeeting.HarvestStateFile),
		s.logger,
	)
	// Silent attendance record: log that a matched meeting happened to its
	// project, regardless of the ask cap or a reply. Resolves the project TYPED
	// from the calendar text (UniqueProjectInText → rep path), NOT the ask flow's
	// name string — a counterparty-only match has no single project ref, so it
	// returns handled=true (deliberate skip, nothing to write) rather than
	// re-interpreting a colliding name as a project. Returns false only on a
	// transient write failure so the harvest retries.
	s.meetingHarvest.SetAttendanceRecorder(func(ev calendar.Event) bool {
		st := s.wikiStore
		if st == nil {
			return true
		}
		ref, ok := st.UniqueProjectInText(runtimemeeting.MeetingMatchText(ev))
		if !ok {
			return true // no single project — skip, don't retry
		}
		return wikiwork.RecordMeetingAttendanceByPath(st, ref.Path, ev.Summary,
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
	return runtimemeeting.LooseUniqueNameMatch(text,
		runtimemeeting.KnownNames(st.KnownProjects(), st.KnownCounterparties()))
}

func (s *Server) registerPlaudWorkflow(homeDir string) {
	if os.Getenv(runtimemeeting.PlaudDisableEnv) == "1" {
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok || s.wikiStore == nil {
		return
	}
	s.plaudRecordings = runtimemeeting.NewPlaudService(
		s.callPlaudTool,
		s.completePlaudAnalysis,
		s.completePlaudStageOne,
		s.projectCandidatesFn(),
		s.businessTopicKnowledge,
		s.plaudGlossary,
		s.plaudCorrectionPrompt,
		configresolve.TopicsDir(),
		s.plaudProjectEntities,
		s.wikiStore.WritePage,
		s.wikiStore.AppendProjectStatusLine,
		s.relayMeetingReport,
		filepath.Join(stateDir, runtimemeeting.PlaudStateFile),
		s.logger,
	)
	s.plaudRecordings.SetCalendarLister(func(ctx context.Context, from, to time.Time) ([]calendar.Event, error) {
		client, err := runtimemeeting.ResolveCalendarClient()
		if err != nil || client == nil {
			return nil, err
		}
		return client.ListUpcoming(ctx, from, to, 40)
	})
	s.plaudRecordings.SetPriorMeetingLoader(s.plaudPriorMeeting)
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

func plaudChatRequest(model, system, user string, maxTokens int) llm.ChatRequest {
	return llm.ChatRequest{
		Model:     model,
		System:    llm.SystemString(system),
		Messages:  []llm.Message{llm.NewTextMessage("user", user)},
		MaxTokens: maxTokens,
		Thinking:  &llm.ThinkingConfig{Type: "disabled"},
	}
}

func (s *Server) businessTopicKnowledge() string {
	dir := configresolve.TopicsDir()
	if dir == "" {
		return ""
	}
	return prompt.LoadTopicKnowledge("", dir, "업무", "").Content
}

func (s *Server) plaudGlossary() string {
	return runtimemeeting.LoadPlaudGlossary(configresolve.TopicsDir())
}

func (s *Server) plaudCorrectionPrompt() string {
	return runtimemeeting.LoadPlaudCorrectionPrompt(configresolve.TopicsDir())
}

// plaudProjectEntities loads 대표페이지 + related 인물/거래처 titles for
// Plaud ASR correction. Caps related reads so a noisy related: list cannot
// fan out into the whole wiki.
func (s *Server) plaudProjectEntities(paths []string) []runtimemeeting.ProjectEntityFacts {
	if s.wikiStore == nil || len(paths) == 0 {
		return nil
	}
	const maxRelatedReads = 12
	out := make([]runtimemeeting.ProjectEntityFacts, 0, len(paths))
	relatedReads := 0
	for _, path := range paths {
		page, err := s.wikiStore.ReadPage(path)
		if err != nil || page == nil {
			continue
		}
		fact := runtimemeeting.ProjectEntityFacts{
			Path:   path,
			Title:  page.Meta.Title,
			Client: page.Meta.Client,
			Sites:  append([]string(nil), page.Meta.Sites...),
			Tags:   append([]string(nil), page.Meta.Tags...),
			Cues:   append([]string(nil), page.Meta.Cues...),
		}
		for _, rel := range page.Meta.Related {
			kind := runtimemeeting.RelatedEntityKind(rel)
			if kind == "" {
				continue
			}
			title := ""
			if relatedReads < maxRelatedReads {
				if rp, rerr := s.wikiStore.ReadPage(rel); rerr == nil && rp != nil {
					title = strings.TrimSpace(rp.Meta.Title)
					relatedReads++
				}
			}
			if title == "" {
				title = runtimemeeting.TitleFromRelatedPath(rel)
			}
			switch kind {
			case "person":
				fact.People = append(fact.People, title)
			case "org":
				fact.Orgs = append(fact.Orgs, title)
			}
		}
		out = append(out, fact)
	}
	return out
}

// plaudPriorMeeting returns the newest prior 회의록 under the same project for
// synthesis continuity. projectPath is a 대표페이지 path.
func (s *Server) plaudPriorMeeting(projectPath string) (title, body string) {
	if s.wikiStore == nil {
		return "", ""
	}
	name, ok := wiki.ProjectNameOf(projectPath)
	if !ok || name == "" {
		return "", ""
	}
	prefix := "프로젝트/" + name + "/회의록/"
	pages, err := s.wikiStore.ListPages("프로젝트")
	if err != nil {
		return "", ""
	}
	var newest string
	for _, p := range pages {
		p = filepath.ToSlash(p)
		if !strings.HasPrefix(p, prefix) || !strings.HasSuffix(p, ".md") {
			continue
		}
		if newest == "" || p > newest {
			newest = p
		}
	}
	if newest == "" {
		return "", ""
	}
	page, err := s.wikiStore.ReadPage(newest)
	if err != nil || page == nil {
		return "", ""
	}
	return page.Meta.Title, page.Body
}

func (s *Server) relayMeetingReport(text string) (bool, error) {
	return s.proactiveRelay.RelayNativeToOptions("", text,
		proactive.Options{WorkFeedSource: workfeed.SourceMeetingReport})
}

func (s *Server) registerModelMaintenanceWorkflows() {
	if s.modelRegistry == nil || s.agentLogWriter == nil {
		return
	}
	var notify func(ctx context.Context, msg string) error
	if notifier := s.proactiveRelay.NotifierForSession(proactive.NativeWorkSessionKey); notifier != nil {
		notify = notifier.Notify
	}
	s.modelMaintenance = modelmaintenance.New(modelmaintenance.Deps{
		Logs:      s.agentLogWriter,
		Registry:  s.modelRegistry,
		Summaries: s.polarisStore,
		Capture:   s.logCapture,
		StateDir:  config.ResolveStateDir(),
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
	cfgSnap, _ := config.LoadConfigFromDefaultPath()
	s.initGmailPoll(cfgSnap)
	s.initLMTPServer(cfgSnap)
}

func (s *Server) registerCalendarBriefingWorkflow() {
	s.calendarBriefing = runtimemeeting.NewCalendarBriefingService(
		func(text string) (bool, error) { return s.proactiveRelay.RelayNative(text) },
		runtimemeeting.ResolveCalendarClient,
		s.logger,
	)
	if s.calendarBriefing != nil {
		s.calendarBriefing.EnableEnrichment(
			func() *wiki.Store { return s.wikiStore },
			s.logger,
		)
	}
	s.calendarBriefing.Start(s.ShutdownCtx())
}

func (s *Server) registerRoleHealthWorkflow() {
	if s.modelRegistry == nil || s.roleHealth != nil {
		return
	}
	s.roleHealth = rolehealth.New(
		s.modelRegistry,
		s.logger,
		func(event string, payload events.EventPayload) {
			if s.broadcaster != nil {
				s.broadcaster.Broadcast(event, payload)
			}
		},
		filepath.Join(s.denebDir, "role_health.json"),
	)
	s.roleHealth.Start(s.ShutdownCtx())
}
