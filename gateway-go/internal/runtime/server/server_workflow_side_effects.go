package server

import (
	"context"
	"os"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/approval"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/process"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/events"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/rpc/rpcutil"
)

// workflowHomeDir returns the OS user home dir, or "" if unavailable. Small
// enough to duplicate rather than importing serverauto (which has its own
// copy) just for this one-liner.
func workflowHomeDir() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return homeDir
}

// registerProcessApprovalSideEffect wires the tool-exec approval gate: when a
// tool execution requires approval, create a request, broadcast it to
// connected clients, and wait for the bounded decision. Stays in the
// composition root — s.processes/s.approvals are WorkflowSubsystem-owned and
// this is pure infra plumbing, not feature-domain wiring.
func (s *Server) registerProcessApprovalSideEffect(hub *rpcutil.GatewayHub) {
	if s.processes == nil {
		return
	}
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

// registerWorkflowSideEffects wires non-RPC business logic: process approval
// callbacks, autonomous/dreaming service, native notifiers, and memory flush.
// All RPC domain registrations (approval, agent CRUD) are now
// handled by registerEarlyMethods via hub adapters. Orchestrates the mail
// (servermail), chat (serverchat), and autonomous (serverauto) composition
// roots — each owns its own registration logic; this just sequences them.
func (s *Server) registerWorkflowSideEffects(hub *rpcutil.GatewayHub) {
	s.registerProcessApprovalSideEffect(hub)
	s.Auto.ConfigureAutonomousWorkflow(hub)

	if s.Chat.ChatHandler != nil {
		homeDir := workflowHomeDir()
		s.Auto.RegisterHeartbeatWorkflowTasks(homeDir)
		s.Auto.RegisterGoalWorkflowTask(homeDir)

		// Daily offsite memory backup: tar.gz of the memory stores streamed
		// over ssh to the storage node (the NFS mount is read-only from this
		// host). Only registered for the production state dir — dev live-test
		// instances (DENEB_STATE_DIR=/tmp/...) must not ship archives.
		s.Mail.RegisterMemoryBackupTask(homeDir)

		// Wiki scout: every 12h, take stale project open questions plus the
		// operator's WIKI.md brief topics and run one bounded agent turn WITH
		// web access to chase externally-answerable answers. Findings persist
		// only via wiki ingest (자료) + 로그.md '질문해결' ops — rep-page body
		// stays untouched. Production state dir only.
		scout := s.Mail.RegisterWikiScoutTask(homeDir)

		// Project-wiki deep research: every 6h, pick one 프로젝트 page and run an
		// agent turn that re-investigates it from Deneb's own internal sources
		// (mail archive, polaris recall, knowledge graph, contacts, linked wiki
		// pages) and updates it in place. Internal-only (no web), silent, and
		// round-robin across project pages. Production state dir only — see
		// RegisterWikiResearchTask. The scout handle wires the immediate
		// post-cycle trigger: a fresh open question goes external right after
		// the internal pass fails to answer it, not next scheduled cycle.
		s.Mail.RegisterWikiResearchTask(homeDir, scout)

		// Wiki reviewer: every 2h, review pages created/updated since the last
		// pass for near-duplicates (skill-reviewer pattern for memory writes) —
		// deterministic candidate gather → one lightweight JSON verdict →
		// capped, git-snapshotted merges. Production state dir only.
		s.Mail.RegisterWikiReviewTask(homeDir)

		// Notification digest: every 12h, consume the phone-notification raw
		// ledger (phoneevents/ledger.go — KakaoTalk/approval/SMS events the
		// ephemeral judgment path would otherwise evaporate) into the wiki via
		// one internal-only agent turn. Alerting stays instant; memory is
		// batched. Production state dir only.
		s.Mail.RegisterNotiDigestTask(homeDir)

		// Supernote digest: every 6h, poll the Drive folder the Manta tablet
		// auto-syncs handwritten-note exports into, extract the on-device-
		// recognized text, and consolidate it into the wiki (project logs,
		// commitments, people). No-ops until the folder + Drive credentials
		// are configured. Production state dir only.
		s.Mail.RegisterSupernoteDigestTask(homeDir)

		s.Auto.RegisterMeetingHarvestWorkflow(homeDir)

		s.Auto.RegisterPlaudWorkflow(homeDir)

		s.Auto.RegisterModelMaintenanceWorkflows()
	}

	s.Auto.RegisterFileSemanticIndexWorkflow()

	// Propus: register autonomous tasks (services created in InitGenesisServices).
	s.Auto.RegisterGenesisAutonomousTasks(hub)

	s.Chat.RegisterMailIngestWorkflows()
	s.Auto.RegisterCalendarBriefingWorkflow()
	s.Auto.RegisterRoleHealthWorkflow()
}
