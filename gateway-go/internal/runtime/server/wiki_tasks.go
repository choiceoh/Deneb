package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/phoneledger"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// registerWikiResearchTask wires the project-wiki refresh only for the
// production state directory. scout (nil when disabled) receives an immediate
// trigger after each research turn so freshly written open questions go
// external without waiting for the scheduled scout cycle.
func (s *Server) registerWikiResearchTask(homeDir string, scout *wikiwork.ScoutTask) {
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
	task := wikiwork.NewResearchTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.ResearchStateFile),
		configresolve.WorkspaceDir(),
	)
	if scout != nil {
		task.SetPostCycleScout(scout.TriggerForPage)
		// Share the scout's maintenance lock so a scheduled research turn and
		// a scheduled scout turn never rewrite the same rep page concurrently.
		task.SetMaintenanceLock(scout.MaintenanceLock())
	}
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-research task registered",
		"interval", wikiwork.ResearchInterval.String(), "scoutTrigger", scout != nil)
}

// registerWikiScoutTask wires the external-scouting twin of wiki-research
// (open questions + WIKI.md brief topics → bounded web turn) only for the
// production state directory. Returns the task (nil when disabled) so the
// research task can wire its immediate post-cycle trigger.
func (s *Server) registerWikiScoutTask(homeDir string) *wikiwork.ScoutTask {
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
	task := wikiwork.NewScoutTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.ScoutStateFile),
		configresolve.WorkspaceDir(),
	)
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-scout task registered", "interval", wikiwork.ScoutInterval.String())
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
	s.autonomousSvc.RegisterTask(wikiwork.NewNotiDigestTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.NotiDigestStateFile),
		filepath.Join(stateDir, phoneledger.Dirname),
		configresolve.WorkspaceDir(),
	))
	s.logger.Info("noti-digest task registered", "interval", wikiwork.NotiDigestInterval.String())
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
	s.autonomousSvc.RegisterTask(wikiwork.NewSupernoteDigestTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.SupernoteStateFile),
		os.Getenv(wikiwork.DriveFolderEnv),
		configresolve.WorkspaceDir(),
	))
	s.logger.Info("supernote-digest task registered",
		"interval", wikiwork.SupernoteInterval.String(),
		"configured", os.Getenv(wikiwork.DriveFolderEnv) != "")
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
	task := wikiwork.NewReviewTask(
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.ReviewStateFile),
		autoMerge,
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			return pilot.CallRoleLLM(ctx, modelrole.RoleMain, system, user, maxTokens,
				json.RawMessage(`{"temperature":0,"reasoning_effort":"low"}`))
		},
	)
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-review task registered",
		"interval", wikiwork.ReviewInterval.String(), "autoMerge", autoMerge)
}
