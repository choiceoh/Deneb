package servermail

import (
	"context"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/configresolve"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/phoneevents"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/serverport"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// RegisterWikiResearchTask wires the project-wiki refresh only for the
// production state directory. scout (nil when disabled) receives an immediate
// trigger after each research turn so freshly written open questions go
// external without waiting for the scheduled scout cycle.
func (m *Manager) RegisterWikiResearchTask(homeDir string, scout *wikiwork.ScoutTask) {
	chatHandler := m.Host.ChatHandler()
	if chatHandler == nil || m.WikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_RESEARCH_DISABLE") == "1" {
		m.Host.Logger().Info("wiki-research disabled via DENEB_WIKI_RESEARCH_DISABLE")
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return
	}
	task := wikiwork.NewResearchTask(
		chatHandler,
		m.WikiStore,
		m.Host.Activity(),
		m.Host.Logger(),
		filepath.Join(stateDir, wikiwork.ResearchStateFile),
		configresolve.WorkspaceDir(),
	)
	if scout != nil {
		task.SetPostCycleScout(scout.TriggerForPage)
		// Share the scout's maintenance lock so a scheduled research turn and
		// a scheduled scout turn never rewrite the same rep page concurrently.
		task.SetMaintenanceLock(scout.MaintenanceLock())
	}
	m.Host.AutonomousSvc().RegisterTask(task)
	m.Host.Logger().Info("wiki-research task registered",
		"interval", wikiwork.ResearchInterval.String(), "scoutTrigger", scout != nil)
}

// RegisterWikiScoutTask wires the external-scouting twin of wiki-research
// (open questions + WIKI.md brief topics → bounded web turn) only for the
// production state directory. Returns the task (nil when disabled) so the
// research task can wire its immediate post-cycle trigger.
func (m *Manager) RegisterWikiScoutTask(homeDir string) *wikiwork.ScoutTask {
	chatHandler := m.Host.ChatHandler()
	if chatHandler == nil || m.WikiStore == nil {
		return nil
	}
	if os.Getenv("DENEB_WIKI_SCOUT_DISABLE") == "1" {
		m.Host.Logger().Info("wiki-scout disabled via DENEB_WIKI_SCOUT_DISABLE")
		return nil
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return nil
	}
	task := wikiwork.NewScoutTask(
		chatHandler,
		m.WikiStore,
		m.Host.Activity(),
		m.Host.Logger(),
		filepath.Join(stateDir, wikiwork.ScoutStateFile),
		configresolve.WorkspaceDir(),
	)
	m.Host.AutonomousSvc().RegisterTask(task)
	m.Host.Logger().Info("wiki-scout task registered", "interval", wikiwork.ScoutInterval.String())
	return task
}

// RegisterNotiDigestTask wires the phone-notification ledger digestion (the
// memory half of phone sensing — the judgment path stays ephemeral) only for
// the production state directory.
func (m *Manager) RegisterNotiDigestTask(homeDir string) {
	chatHandler := m.Host.ChatHandler()
	if chatHandler == nil || m.WikiStore == nil {
		return
	}
	if os.Getenv("DENEB_NOTI_DIGEST_DISABLE") == "1" {
		m.Host.Logger().Info("noti-digest disabled via DENEB_NOTI_DIGEST_DISABLE")
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return
	}
	m.Host.AutonomousSvc().RegisterTask(wikiwork.NewNotiDigestTask(
		chatHandler,
		m.WikiStore,
		m.Host.Activity(),
		m.Host.Logger(),
		filepath.Join(stateDir, wikiwork.NotiDigestStateFile),
		filepath.Join(stateDir, phoneevents.LedgerDirname),
		configresolve.WorkspaceDir(),
	))
	m.Host.Logger().Info("noti-digest task registered", "interval", wikiwork.NotiDigestInterval.String())
}

// RegisterSupernoteDigestTask wires the Supernote (Manta) handwritten-note
// ingestion: poll a Drive folder the device auto-syncs to → extract the note
// text → consolidate into the wiki. Only for the production state dir; the
// task itself no-ops until DENEB_SUPERNOTE_DRIVE_FOLDER_ID and Drive
// credentials are configured.
func (m *Manager) RegisterSupernoteDigestTask(homeDir string) {
	chatHandler := m.Host.ChatHandler()
	if chatHandler == nil || m.WikiStore == nil {
		return
	}
	if os.Getenv("DENEB_SUPERNOTE_DISABLE") == "1" {
		m.Host.Logger().Info("supernote-digest disabled via DENEB_SUPERNOTE_DISABLE")
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return
	}
	m.Host.AutonomousSvc().RegisterTask(wikiwork.NewSupernoteDigestTask(
		chatHandler,
		m.WikiStore,
		m.Host.Activity(),
		m.Host.Logger(),
		filepath.Join(stateDir, wikiwork.SupernoteStateFile),
		os.Getenv(wikiwork.DriveFolderEnv),
		configresolve.WorkspaceDir(),
	))
	m.Host.Logger().Info("supernote-digest task registered",
		"interval", wikiwork.SupernoteInterval.String(),
		"configured", os.Getenv(wikiwork.DriveFolderEnv) != "")
}

// RegisterWikiReviewTask wires post-write review and deterministic maintenance
// only for the production state directory.
func (m *Manager) RegisterWikiReviewTask(homeDir string) {
	if m.WikiStore == nil {
		return
	}
	if os.Getenv("DENEB_WIKI_REVIEW_DISABLE") == "1" {
		m.Host.Logger().Info("wiki-review disabled via DENEB_WIKI_REVIEW_DISABLE")
		return
	}
	stateDir, ok := serverport.ProductionStateDir(homeDir)
	if !ok {
		return
	}
	autoMerge := os.Getenv("DENEB_WIKI_REVIEW_AUTOMERGE") == "1"
	task := wikiwork.NewReviewTask(
		m.WikiStore,
		m.Host.Activity(),
		m.Host.Logger(),
		filepath.Join(stateDir, wikiwork.ReviewStateFile),
		autoMerge,
		func(ctx context.Context, system, user string, maxTokens int) (string, error) {
			return pilot.CallRoleLLM(ctx, modelrole.RoleMain, system, user, maxTokens,
				map[string]any{"temperature": 0, "reasoning_effort": "low"})
		},
	)
	m.Host.AutonomousSvc().RegisterTask(task)
	m.Host.Logger().Info("wiki-review task registered",
		"interval", wikiwork.ReviewInterval.String(), "autoMerge", autoMerge)
}
