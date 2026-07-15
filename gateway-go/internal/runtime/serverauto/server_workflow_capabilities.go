package serverauto

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/goals"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/config"
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
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// ConfigureAutonomousWorkflow wires the AuroraDream service (dreaming-only,
// no goal cycles): memory consolidation driven by the autonomous scheduler.
func (m *Manager) ConfigureAutonomousWorkflow(hub *rpcutil.GatewayHub) {
	logger := m.Host.Logger()
	m.AutonomousSvc = autonomous.NewService(logger)
	m.AutonomousSvc.SetBehaviorLog(m.AgentLogWriter)
	// Persist last-run times so deploy restarts do not reset daily/weekly tasks.
	if home, err := os.UserHomeDir(); err == nil {
		m.AutonomousSvc.SetStateDir(filepath.Join(home, ".deneb"))
	}

	// Prompt snapshots must be configured before the service can begin a dream
	// cycle, otherwise a restart loses byte-identical APC prompt reuse.
	chat.ConfigurePromptSnapshots(config.ResolveStateDir(), logger)
	m.AutonomousSvc.OnEvent(func(event autonomous.CycleEvent) {
		dreamWire, _ := events.PayloadOf(event)
		hub.Broadcast("dreaming.cycle", dreamWire)
		if event.Type == "dreaming_completed" {
			m.Host.PostDreamWorkfeedCard(event.DreamReport)
		}
	})
	if n := m.Host.ProactiveRelay().NotifierForSession(proactive.DreamWorkSessionKey); n != nil {
		m.AutonomousSvc.SetNotifier(n)
	}
	// Keep SetDreamer last: it starts the timer loop and can immediately emit.
	if m.WikiDreamer != nil {
		m.AutonomousSvc.SetDreamer(m.WikiDreamer)
	}
}

func workflowHomeDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return homeDir
}

func (m *Manager) RegisterHeartbeatWorkflowTasks(homeDir string) {
	logger := m.Host.Logger()
	chatHandler := m.Host.ChatHandler()
	activity := m.Host.Activity()
	m.AutonomousSvc.RegisterTask(runtimeheartbeat.NewBootTask(
		chatHandler, activity, logger, homeDir,
	))
	m.AutonomousSvc.RegisterTask(runtimeheartbeat.NewTask(runtimeheartbeat.TaskConfig{
		ChatHandler: chatHandler,
		Activity:    activity,
		Logger:      logger,
		HomeDir:     homeDir,
		CollectSignals: runtimeheartbeat.CombineCollectors(
			runtimeheartbeat.CalendarSignalCollector(runtimemeeting.ResolveCalendarClient),
			runtimeheartbeat.TodoDeadlineCollector(),
			runtimeheartbeat.DealDeadlineSignalCollector(func() *wiki.Store { return m.Host.WikiStore() }),
		),
		SignalConfig:              autonomous.SignalConfigForThreshold(configresolve.ProactiveEscalateThreshold(logger)),
		ProposedSelfCoding:        m.proposedSelfCodingFingerprint,
		DispatchBacklogSelfCoding: m.dispatchBacklogSelfCodingCount,
		PromoteRecurrences:        m.promoteSelfCodingRecurrences,
		PromoteClusters:           m.promoteSelfCodingClusters,
		SelfImproveSignals:        m.selfCodingFunnelSignals,
		SelfImproveEvidence:       m.selfCodingFailureEvidence,
		IdleSkillReview:           m.idleSkillReviewLaneIfProduction(homeDir),
	}))
}

func (m *Manager) proposedSelfCodingFingerprint() (int, string) {
	tracker := m.GenesisTracker
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
func (m *Manager) dispatchBacklogSelfCodingCount() int {
	tracker := m.GenesisTracker
	if tracker == nil {
		return 0
	}
	recs, err := tracker.RecentSelfCorrectionCandidates("", genesis.SelfCorrectionStatusAccepted, 100)
	if err != nil {
		m.Host.Logger().Warn("dispatch backlog: self-correction read failed — suppressing sweep", "error", err)
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

func (m *Manager) promoteSelfCodingRecurrences() (int, error) {
	tracker := m.GenesisTracker
	if tracker == nil {
		return 0, nil
	}
	return tracker.PromoteTargetRecurrenceCandidates()
}

func (m *Manager) promoteSelfCodingClusters() (int, error) {
	tracker := m.GenesisTracker
	if tracker == nil {
		return 0, nil
	}
	return tracker.PromoteFailureClusterCandidates()
}

func (m *Manager) selfCodingFunnelSignals() (genesis.SelfCorrectionFunnelSummary, int) {
	tracker := m.GenesisTracker
	if tracker == nil {
		return genesis.SelfCorrectionFunnelSummary{}, 0
	}
	return tracker.SelfCorrectionFunnel(), tracker.SelfHarnessSignals().TargetRecurrences7d
}

func (m *Manager) selfCodingFailureEvidence(limit int) []genesis.FailureClusterSummary {
	tracker := m.GenesisTracker
	if tracker == nil {
		return nil
	}
	return tracker.FailureEvidenceClusters(limit)
}

func (m *Manager) RegisterGoalWorkflowTask(homeDir string) {
	logger := m.Host.Logger()
	goalStateDir := ""
	if homeDir != "" {
		goalStateDir = filepath.Join(homeDir, ".deneb")
	}
	goalStore := goals.NewStore(goalStateDir, logger)
	goals.SetDefault(goalStore)
	m.AutonomousSvc.RegisterTask(goalloop.NewTask(
		m.Host.ChatHandler(),
		goalStore,
		m.Host.Activity(),
		logger,
		func(ctx context.Context, sessionKey, msg string) error {
			n := m.Host.ProactiveRelay().NotifierForSession(sessionKey)
			if n == nil {
				return nil
			}
			return n.Notify(ctx, msg)
		},
	))
}

func (m *Manager) RegisterMeetingHarvestWorkflow(homeDir string) {
	if os.Getenv("DENEB_MEETING_HARVEST_DISABLE") == "1" {
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return
	}
	logger := m.Host.Logger()
	m.meetingHarvest = runtimemeeting.NewHarvestService(
		func(text string) (bool, error) {
			return m.Host.ProactiveRelay().RelayNativeToOptions("", text,
				proactive.Options{MirrorTranscript: true})
		},
		runtimemeeting.ResolveCalendarClient,
		m.matchMeetingProjectName,
		filepath.Join(stateDir, runtimemeeting.HarvestStateFile),
		logger,
	)
	// Silent attendance record: log that a matched meeting happened to its
	// project, regardless of the ask cap or a reply. Resolves the project TYPED
	// from the calendar text (UniqueProjectInText → rep path), NOT the ask flow's
	// name string — a counterparty-only match has no single project ref, so it
	// returns handled=true (deliberate skip, nothing to write) rather than
	// re-interpreting a colliding name as a project. Returns false only on a
	// transient write failure so the harvest retries.
	m.meetingHarvest.SetAttendanceRecorder(func(ev calendar.Event) bool {
		st := m.Host.WikiStore()
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
	m.meetingHarvest.Start(m.Host.ShutdownCtx())
}

func (m *Manager) matchMeetingProjectName(text string) string {
	st := m.Host.WikiStore()
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

func (m *Manager) RegisterPlaudWorkflow(homeDir string) {
	if os.Getenv(runtimemeeting.PlaudDisableEnv) == "1" {
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	wikiStore := m.Host.WikiStore()
	if !ok || wikiStore == nil {
		return
	}
	logger := m.Host.Logger()
	m.plaudRecordings = runtimemeeting.NewPlaudService(
		m.callPlaudTool,
		m.completePlaudAnalysis,
		m.completePlaudStageOne,
		m.Host.ProjectCandidatesFn(),
		m.businessTopicKnowledge,
		wikiStore.WritePage,
		wikiStore.AppendProjectStatusLine,
		m.relayMeetingReport,
		filepath.Join(stateDir, runtimemeeting.PlaudStateFile),
		logger,
	)
	m.plaudRecordings.Start(m.Host.ShutdownCtx())
}

func (m *Manager) callPlaudTool(ctx context.Context, name string, args json.RawMessage) (string, error) {
	client := m.Host.ExternalMCPClient("plaud")
	if client == nil {
		return "", errors.New("unknown tool: plaud mcp not configured")
	}
	return client.CallTool(ctx, strings.TrimPrefix(name, "plaud_"), args)
}

func (m *Manager) completePlaudAnalysis(ctx context.Context, system, user string, maxTokens int) (string, error) {
	client, model, _, _ := m.Host.MailAnalysisModels()
	if client == nil {
		return "", errors.New("main-role model unavailable")
	}
	return client.Complete(ctx, plaudChatRequest(model, system, user, maxTokens))
}

func (m *Manager) completePlaudStageOne(ctx context.Context, system, user string, maxTokens int) (string, error) {
	_, _, client, model := m.Host.MailAnalysisModels()
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

func (m *Manager) businessTopicKnowledge() string {
	dir := configresolve.TopicsDir()
	if dir == "" {
		return ""
	}
	return prompt.LoadTopicKnowledge("", dir, "업무", "").Content
}

func (m *Manager) relayMeetingReport(text string) (bool, error) {
	return m.Host.ProactiveRelay().RelayNativeToOptions("", text,
		proactive.Options{WorkFeedSource: workfeed.SourceMeetingReport})
}

func (m *Manager) RegisterModelMaintenanceWorkflows() {
	modelRegistry := m.Host.ModelRegistry()
	if modelRegistry == nil || m.AgentLogWriter == nil {
		return
	}
	logger := m.Host.Logger()
	var notify func(ctx context.Context, msg string) error
	if notifier := m.Host.ProactiveRelay().NotifierForSession(proactive.NativeWorkSessionKey); notifier != nil {
		notify = notifier.Notify
	}
	m.ModelMaintenance = modelmaintenance.New(modelmaintenance.Deps{
		Logs:      m.AgentLogWriter,
		Registry:  modelRegistry,
		Summaries: m.Host.PolarisStore(),
		Capture:   m.Host.LogCapture(),
		StateDir:  config.ResolveStateDir(),
		Notify:    notify,
		Logger:    logger,
	})
	for _, task := range m.ModelMaintenance.Tasks() {
		m.AutonomousSvc.RegisterTask(task)
	}
}

func (m *Manager) RegisterFileSemanticIndexWorkflow() {
	fileSemindex := m.Host.FileSemindex()
	if fileSemindex == nil {
		return
	}
	if task := fileSemindex.Task(); task != nil {
		m.AutonomousSvc.RegisterTask(task)
	}
}

func (m *Manager) RegisterCalendarBriefingWorkflow() {
	logger := m.Host.Logger()
	m.calendarBriefing = runtimemeeting.NewCalendarBriefingService(
		func(text string) (bool, error) { return m.Host.ProactiveRelay().RelayNative(text) },
		runtimemeeting.ResolveCalendarClient,
		logger,
	)
	if m.calendarBriefing != nil {
		m.calendarBriefing.EnableEnrichment(
			func() *wiki.Store { return m.Host.WikiStore() },
			logger,
		)
	}
	m.calendarBriefing.Start(m.Host.ShutdownCtx())
}

func (m *Manager) RegisterRoleHealthWorkflow() {
	modelRegistry := m.Host.ModelRegistry()
	if modelRegistry == nil || m.RoleHealth != nil {
		return
	}
	logger := m.Host.Logger()
	broadcaster := m.Host.Broadcaster()
	m.RoleHealth = rolehealth.New(
		modelRegistry,
		logger,
		func(event string, payload events.EventPayload) {
			if broadcaster != nil {
				broadcaster.Broadcast(event, payload)
			}
		},
		filepath.Join(m.Host.DenebDir(), "role_health.json"),
	)
	m.RoleHealth.Start(m.Host.ShutdownCtx())
}
