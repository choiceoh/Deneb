// Package rlm — Service exposes RLM state and wiki-backed queries via RPC.
package rlm

import (
	"context"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// Service provides RLM status and wiki-backed project/memory queries
// for the RPC layer. Read-only; configuration changes require a restart.
type Service struct {
	cfg    Config
	wiki   *wiki.Store
	logger *slog.Logger
}

// NewService creates an RLM service. wiki may be nil (tools return unavailable).
func NewService(cfg Config, wikiStore *wiki.Store, logger *slog.Logger) *Service {
	return &Service{cfg: cfg, wiki: wikiStore, logger: logger}
}

// ServiceStatus is the RPC response for rlm.status.
type ServiceStatus struct {
	Enabled       bool `json:"enabled"`
	SubLLMEnabled bool `json:"sub_llm_enabled"`
	WikiConnected bool `json:"wiki_connected"`

	FreshTailCount   int `json:"fresh_tail_count"`
	TotalTokenBudget int `json:"total_token_budget"`
	MaxSubSpawns     int `json:"max_sub_spawns"`
	REPLTimeoutSec   int `json:"repl_timeout_sec"`

	// Wiki stats (nil when wiki is not connected).
	WikiStats *wiki.StoreStats `json:"wiki_stats,omitempty"`
}

// Status returns the current RLM service status.
func (s *Service) Status() ServiceStatus {
	st := ServiceStatus{
		Enabled:          s.cfg.Enabled,
		SubLLMEnabled:    s.cfg.SubLLMEnabled,
		WikiConnected:    s.wiki != nil,
		FreshTailCount:   s.cfg.FreshTailCount,
		TotalTokenBudget: s.cfg.TotalTokenBudget,
		MaxSubSpawns:     s.cfg.MaxSubSpawns,
		REPLTimeoutSec:   s.cfg.REPLTimeoutSec,
	}
	if s.wiki != nil {
		stats := s.wiki.Stats()
		st.WikiStats = &stats
	}
	return st
}

// Config returns the cached RLM config.
func (s *Service) Config() Config { return s.cfg }

// WikiStore returns the backing wiki store (may be nil).
func (s *Service) WikiStore() *wiki.Store { return s.wiki }

// ProjectResult is a single project listing entry.
type ProjectResult struct {
	Path       string   `json:"path"`
	Title      string   `json:"title"`
	Tags       []string `json:"tags,omitempty"`
	Importance float64  `json:"importance,omitempty"`
}

// ListProjects returns all pages in the wiki "프로젝트" category.
func (s *Service) ListProjects() ([]ProjectResult, error) {
	if s.wiki == nil {
		return nil, nil
	}
	pages, err := s.wiki.ListPages("프로젝트")
	if err != nil {
		return nil, err
	}
	results := make([]ProjectResult, 0, len(pages))
	for _, relPath := range pages {
		page, pErr := s.wiki.ReadPage(relPath)
		if pErr != nil {
			continue
		}
		results = append(results, ProjectResult{
			Path:       relPath,
			Title:      page.Meta.Title,
			Tags:       page.Meta.Tags,
			Importance: page.Meta.Importance,
		})
	}
	return results, nil
}

// SearchResult wraps wiki.SearchResult for RPC responses.
type SearchResult struct {
	Path    string  `json:"path"`
	Content string  `json:"content"`
	Score   float64 `json:"score"`
}

// SearchProjects searches wiki pages scoped to the "프로젝트" category.
func (s *Service) SearchProjects(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s.wiki == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	hits, err := s.wiki.Search(ctx, query, limit*2) // over-fetch, then filter
	if err != nil {
		return nil, err
	}
	const projectPrefix = "프로젝트/"
	var results []SearchResult
	for _, h := range hits {
		if !strings.HasPrefix(h.Path, projectPrefix) {
			continue
		}
		results = append(results, SearchResult{Path: h.Path, Content: h.Content, Score: h.Score})
		if len(results) >= limit {
			break
		}
	}
	return results, nil
}

// RecallMemory searches across all wiki categories.
func (s *Service) RecallMemory(ctx context.Context, query string, limit int) ([]SearchResult, error) {
	if s.wiki == nil {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	hits, err := s.wiki.Search(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	results := make([]SearchResult, len(hits))
	for i, h := range hits {
		results[i] = SearchResult{Path: h.Path, Content: h.Content, Score: h.Score}
	}
	return results, nil
}
