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

	if s.chatHandler != nil {
		homeDir := workflowHomeDir()
		s.registerHeartbeatWorkflowTasks(homeDir)
		s.registerGoalWorkflowTask(homeDir)

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
