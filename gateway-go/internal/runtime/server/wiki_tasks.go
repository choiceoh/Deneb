package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/pipebind"
)

// registerWikiResearchTask wires the project-wiki refresh only for the
// production state directory. scout (nil when disabled) receives an immediate
// trigger after each research turn so freshly written open questions go
// external without waiting for the scheduled scout cycle.
func (s *Server) registerWikiResearchTask(homeDir string, scout *svcbind.ScoutTask) {
	if s.chatHandler == nil || s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_RESEARCH_DISABLE") == "1" {
		s.logger.Info("wiki-research disabled via DENEB_WIKI_RESEARCH_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	task := svcbind.NewResearchTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, svcbind.ResearchStateFile),
		svcbind.WorkspaceDir(),
	)
	if scout != nil {
		task.SetPostCycleScout(scout.TriggerForPage)
		// Share the scout's maintenance lock so a scheduled research turn and
		// a scheduled scout turn never rewrite the same rep page concurrently.
		task.SetMaintenanceLock(scout.MaintenanceLock())
	}
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-research task registered",
		"interval", svcbind.ResearchInterval.String(), "scoutTrigger", scout != nil)
}

// registerWikiScoutTask wires the external-scouting twin of wiki-research
// (open questions + WIKI.md brief topics → bounded web turn) only for the
// production state directory. Returns the task (nil when disabled) so the
// research task can wire its immediate post-cycle trigger.
func (s *Server) registerWikiScoutTask(homeDir string) *svcbind.ScoutTask {
	if s.chatHandler == nil || s.wikiStore == nil {
		return nil
	}
	if os.Getenv("DENEB_WIKI_SCOUT_DISABLE") == "1" {
		s.logger.Info("wiki-scout disabled via DENEB_WIKI_SCOUT_DISABLE")
		return nil
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return nil
	}
	task := svcbind.NewScoutTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, svcbind.ScoutStateFile),
		svcbind.WorkspaceDir(),
	)
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-scout task registered", "interval", svcbind.ScoutInterval.String())
	return task
}

// registerNotiDigestTask wires the phone-notification ledger digestion (the
// memory half of phone sensing — the judgment path stays ephemeral) only for
// the production state directory.
func (s *Server) registerNotiDigestTask(homeDir string) {
	if s.chatHandler == nil || s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_NOTI_DIGEST_DISABLE") == "1" {
		s.logger.Info("noti-digest disabled via DENEB_NOTI_DIGEST_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.autonomousSvc.RegisterTask(svcbind.NewNotiDigestTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, svcbind.NotiDigestStateFile),
		filepath.Join(stateDir, svcbind.LedgerDirname),
		svcbind.WorkspaceDir(),
	))
	s.logger.Info("noti-digest task registered", "interval", svcbind.NotiDigestInterval.String())
}

// registerSupernoteDigestTask wires the Supernote (Manta) handwritten-note
// ingestion: poll a Drive folder the device auto-syncs to → extract the note
// text → consolidate into the wiki. Only for the production state dir; the
// task itself no-ops until DENEB_SUPERNOTE_DRIVE_FOLDER_ID and Drive
// credentials are configured.
func (s *Server) registerSupernoteDigestTask(homeDir string) {
	if s.chatHandler == nil || s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_SUPERNOTE_DISABLE") == "1" {
		s.logger.Info("supernote-digest disabled via DENEB_SUPERNOTE_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	s.autonomousSvc.RegisterTask(svcbind.NewSupernoteDigestTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, svcbind.SupernoteStateFile),
		os.Getenv(svcbind.DriveFolderEnv),
		svcbind.WorkspaceDir(),
	))
	s.logger.Info("supernote-digest task registered",
		"interval", svcbind.SupernoteInterval.String(),
		"configured", os.Getenv(svcbind.DriveFolderEnv) != "")
}

// registerWikiReviewTask wires post-write review and deterministic maintenance
// only for the production state directory.
func (s *Server) registerWikiReviewTask(homeDir string) {
	if s.wikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_REVIEW_DISABLE") == "1" {
		s.logger.Info("wiki-review disabled via DENEB_WIKI_REVIEW_DISABLE")
		return
	}
	stateDir, ok := s.productionStateDir(homeDir)
	if !ok {
		return
	}
	autoMerge := os.Getenv("DENEB_WIKI_REVIEW_AUTOMERGE") == "1"
	task := svcbind.NewReviewTask(
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, svcbind.ReviewStateFile),
		autoMerge,
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			return pipebind.CallRoleLLM(ctx, aibind.RoleMain, system, user, maxTokens,
				json.RawMessage(`{"temperature":0,"reasoning_effort":"low"}`))
		},
	)
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-review task registered",
		"interval", svcbind.ReviewInterval.String(), "autoMerge", autoMerge)
}
