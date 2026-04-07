// Package rlm — Service exposes RLM state and wiki-backed queries via RPC.
package rlm

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// Service provides RLM status, wiki-backed project/memory queries,
// and knowledge write-back for the RPC layer.
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
	WikiConnected bool `json:"wiki_connected"`

	FreshTailCount   int `json:"fresh_tail_count"`
	TotalTokenBudget int `json:"total_token_budget"`
	MaxSubSpawns     int `json:"max_sub_spawns"`
	MaxIterations    int `json:"max_iterations"`
	REPLTimeoutSec   int `json:"repl_timeout_sec"`

	// Wiki stats (nil when wiki is not connected).
	WikiStats *wiki.StoreStats `json:"wiki_stats,omitempty"`
}

// Status returns the current RLM service status.
func (s *Service) Status() ServiceStatus {
	st := ServiceStatus{
		WikiConnected:    s.wiki != nil,
		FreshTailCount:   s.cfg.FreshTailCount,
		TotalTokenBudget: s.cfg.TotalTokenBudget,
		MaxSubSpawns:     s.cfg.MaxSubSpawns,
		MaxIterations:    s.cfg.MaxIterations,
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

// WriteResult is the response for write operations.
type WriteResult struct {
	Path   string `json:"path"`
	Action string `json:"action"` // "created" or "updated"
}

// WriteProject creates or updates a project page in the "프로젝트" category.
// If path is empty, it is auto-generated from the title.
func (s *Service) WriteProject(path, title, content string, tags []string, importance float64) (*WriteResult, error) {
	if s.wiki == nil {
		return nil, fmt.Errorf("wiki not configured")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}

	const category = "프로젝트"
	if path == "" {
		slug := strings.ReplaceAll(strings.ToLower(title), " ", "-")
		path = category + "/" + slug + ".md"
	}
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}
	// Ensure path is scoped to the project category.
	if !strings.HasPrefix(path, category+"/") {
		path = category + "/" + path
	}

	return s.writePage(path, title, category, content, tags, importance)
}

// StoreMemory creates or updates a wiki page in the given category.
// This is the general write-back path for persisting knowledge to the wiki.
func (s *Service) StoreMemory(path, title, category, content string, tags []string, importance float64) (*WriteResult, error) {
	if s.wiki == nil {
		return nil, fmt.Errorf("wiki not configured")
	}
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if category == "" {
		return nil, fmt.Errorf("category is required")
	}

	if path == "" {
		slug := strings.ReplaceAll(strings.ToLower(title), " ", "-")
		path = category + "/" + slug + ".md"
	}
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}

	return s.writePage(path, title, category, content, tags, importance)
}

// writePage is the shared implementation for WriteProject and StoreMemory.
func (s *Service) writePage(path, title, category, content string, tags []string, importance float64) (*WriteResult, error) {
	existing, _ := s.wiki.ReadPage(path)

	action := "created"
	var page *wiki.Page
	if existing != nil {
		action = "updated"
		page = existing
		page.Meta.Title = title
		if len(tags) > 0 {
			page.Meta.Tags = mergeUnique(page.Meta.Tags, tags)
		}
		if importance > 0 {
			page.Meta.Importance = importance
		}
		page.Meta.Updated = time.Now().Format("2006-01-02")
		if content != "" {
			page.Body = content
		}
	} else {
		page = wiki.NewPage(title, category, tags)
		if importance > 0 {
			page.Meta.Importance = importance
		}
		if content != "" {
			page.Body = content
		} else {
			page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성\n",
				title, time.Now().Format("2006-01-02"))
		}
	}

	if err := s.wiki.WritePage(path, page); err != nil {
		return nil, fmt.Errorf("write page: %w", err)
	}

	s.logger.Info("rlm: wiki page written", "path", path, "action", action)
	return &WriteResult{Path: path, Action: action}, nil
}

// mergeUnique merges two string slices, deduplicating entries.
func mergeUnique(a, b []string) []string {
	seen := make(map[string]bool, len(a))
	for _, s := range a {
		seen[s] = true
	}
	result := append([]string{}, a...)
	for _, s := range b {
		if !seen[s] {
			result = append(result, s)
			seen[s] = true
		}
	}
	return result
}
