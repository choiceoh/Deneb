package server

import (
	"context"
	"os"
	"path/filepath"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/wikiwork"
)

// registerWikiResearchTask wires the project-wiki refresh only for the
// production state directory.
func (s *Server) registerWikiResearchTask(homeDir string) {
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
	s.autonomousSvc.RegisterTask(wikiwork.NewResearchTask(
		s.chatHandler,
		s.wikiStore,
		s.activity,
		s.logger,
		filepath.Join(stateDir, wikiwork.ResearchStateFile),
	))
	s.logger.Info("wiki-research task registered", "interval", wikiwork.ResearchInterval.String())
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
				map[string]any{"temperature": 0, "reasoning_effort": "low"})
		},
	)
	s.autonomousSvc.RegisterTask(task)
	s.logger.Info("wiki-review task registered",
		"interval", wikiwork.ReviewInterval.String(), "autoMerge", autoMerge)
}
