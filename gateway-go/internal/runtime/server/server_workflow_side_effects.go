package server

import (
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
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
	s.registerProcessApprovalSideEffect(hub)
	s.configureAutonomousWorkflow(hub)
	s.registerSidecarHealthWatch()

	if s.chatHandler != nil {
		homeDir := workflowHomeDir()
		s.registerGroupwareRadarTask(homeDir)
		s.registerGroupwareBoardRadarTask(homeDir)
		s.registerHeartbeatWorkflowTasks(homeDir)
		s.registerGoalWorkflowTask(homeDir)

		// Daily offsite memory backup: tar.gz of the memory stores streamed
		// over ssh to the storage node (the NFS mount is read-only from this
		// host). Only registered for the production state dir — dev live-test
		// instances (DENEB_STATE_DIR=/tmp/...) must not ship archives.
		s.registerMemoryBackupTask(homeDir)

		// Mail backfill: drain mail the poll window never saw. The poll query
		// is `is:unread newer_than:1h` and skips outside business hours, so mail
		// that arrived at night was lost — measured at 41.7% of the 60-day-old
		// cohort. Also the "later pass" MarkAnalysisFailed has been promising.
		s.registerMailBackfillTask(homeDir)

		// Pipeline-gap census: counts what the mail→wiki path is still missing
		// so a silent regression has something that notices. Cheap, read-only.
		s.registerMailGapCensusTask(homeDir)

		// Wiki scout: every 12h, take stale project open questions plus the
		// operator's WIKI.md brief topics and run one bounded agent turn WITH
		// web access to chase externally-answerable answers. Findings persist
		// only via wiki ingest (자료) + 로그.md '질문해결' ops — rep-page body
		// stays untouched. Production state dir only.
		scout := s.registerWikiScoutTask(homeDir)

		// Project-wiki deep research: every 6h, pick one 프로젝트 page and run an
		// agent turn that re-investigates it from Deneb's own internal sources
		// (mail archive, polaris recall, knowledge graph, contacts, linked wiki
		// pages) and updates it in place. Internal-only (no web), silent, and
		// round-robin across project pages. Production state dir only — see
		// registerWikiResearchTask. The scout handle wires the immediate
		// post-cycle trigger: a fresh open question goes external right after
		// the internal pass fails to answer it, not next scheduled cycle.
		s.registerWikiResearchTask(homeDir, scout)

		// Wiki reviewer: every 2h, review pages created/updated since the last
		// pass for near-duplicates (skill-reviewer pattern for memory writes) —
		// deterministic candidate gather → one lightweight JSON verdict →
		// capped, git-snapshotted merges. Production state dir only.
		s.registerWikiReviewTask(homeDir)

		// kb-interview suggestion: daily, check a curated set of
		// business-knowledge domains (market/competitor/customer/pricing —
		// "listened knowledge" only in the operator's head) against wiki
		// coverage and, when one is missing or stale, post ONE workfeed
		// question card offering an interview. Tapping the chip sends a chat
		// message that trips the kb-interview skill trigger. Per-domain 14d
		// cooldown; production state dir only (RSI P5 demand generation).
		s.registerKBInterviewSuggestTask(homeDir)

		// Notification digest: every 12h, consume the phone-notification raw
		// ledger (phoneevents/ledger.go — KakaoTalk/approval/SMS events the
		// ephemeral judgment path would otherwise evaporate) into the wiki via
		// one internal-only agent turn. Alerting stays instant; memory is
		// batched. Production state dir only.
		s.registerNotiDigestTask(homeDir)

		// Supernote digest: every 6h, poll the Drive folder the Manta tablet
		// auto-syncs handwritten-note exports into, extract the on-device-
		// recognized text, and consolidate it into the wiki (project logs,
		// commitments, people). No-ops until the folder + Drive credentials
		// are configured. Production state dir only.
		s.registerSupernoteDigestTask(homeDir)

		s.registerMeetingHarvestWorkflow(homeDir)

		s.registerPlaudWorkflow(homeDir)

		s.registerModelMaintenanceWorkflows()
	}

	s.registerFileSemanticIndexWorkflow()

	// Propus: register autonomous tasks (services created in initGenesisServices).
	s.registerGenesisAutonomousTasks(hub)

	s.registerMailIngestWorkflows()
	s.registerCalendarBriefingWorkflow()
	s.registerRoleHealthWorkflow()
}
