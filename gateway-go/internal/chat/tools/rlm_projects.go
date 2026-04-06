package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/vega"
)

// ToolProjectsList returns a tool that lists projects with metadata only.
// No document content is returned — just identifiers and key properties.
func ToolProjectsList(d *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.Backend == nil {
			return "Vega 백엔드가 비활성 상태입니다.", nil
		}

		var p struct {
			Filter string `json:"filter"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}

		args := map[string]any{}
		if p.Filter != "" {
			args["filter"] = p.Filter
		}

		result, err := d.Backend.Execute(ctx, "list", args)
		if err != nil {
			return fmt.Sprintf("프로젝트 목록 조회 실패: %v", err), nil
		}

		return string(result), nil
	}
}

// ToolProjectsGetField returns a tool that retrieves specific fields from a project.
func ToolProjectsGetField(d *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.Backend == nil {
			return "Vega 백엔드가 비활성 상태입니다.", nil
		}

		var p struct {
			ProjectID string   `json:"project_id"`
			Fields    []string `json:"fields"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.ProjectID == "" {
			return "project_id는 필수입니다.", nil
		}
		if len(p.Fields) == 0 {
			return "fields 배열이 비어있습니다.", nil
		}

		result, err := d.Backend.Execute(ctx, "show", map[string]any{
			"id": p.ProjectID,
		})
		if err != nil {
			return fmt.Sprintf("프로젝트 조회 실패: %v", err), nil
		}

		// Parse result and filter to requested fields only.
		var full map[string]any
		if err := json.Unmarshal(result, &full); err != nil {
			// If the result isn't a JSON object, return it as-is.
			return string(result), nil
		}

		filtered := map[string]any{
			"project_id": p.ProjectID,
		}
		if name, ok := full["name"]; ok {
			filtered["name"] = name
		}
		for _, f := range p.Fields {
			if val, ok := full[f]; ok {
				filtered[f] = val
			} else {
				filtered[f] = nil
			}
		}

		b, _ := json.MarshalIndent(filtered, "", "  ")
		return string(b), nil
	}
}

// ToolProjectsSearch returns a tool that performs natural-language search across projects.
func ToolProjectsSearch(d *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.Backend == nil {
			return "Vega 백엔드가 비활성 상태입니다.", nil
		}

		var p struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.Query == "" {
			return "query는 필수입니다.", nil
		}
		if p.MaxResults <= 0 {
			p.MaxResults = 5
		}

		results, err := d.Backend.Search(ctx, p.Query, vega.SearchOpts{
			Limit: p.MaxResults,
		})
		if err != nil {
			return fmt.Sprintf("검색 실패: %v", err), nil
		}

		if len(results) == 0 {
			return "검색 결과 없음.", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## 검색 결과 (%d건)\n\n", len(results)))
		for _, r := range results {
			snippet := r.Content
			if len([]rune(snippet)) > 200 {
				snippet = string([]rune(snippet)[:200]) + "..."
			}
			sb.WriteString(fmt.Sprintf("- **%s** (ID: %d, 섹션: %s, 관련도: %.2f)\n  %s\n\n",
				r.ProjectName, r.ProjectID, r.Section, r.Score, snippet))
		}

		return sb.String(), nil
	}
}

// ToolProjectsGetDocument returns a tool that retrieves project documents.
// Without a section parameter, returns the table of contents.
// With a section, returns that section's content.
func ToolProjectsGetDocument(d *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.Backend == nil {
			return "Vega 백엔드가 비활성 상태입니다.", nil
		}

		var p struct {
			ProjectID string `json:"project_id"`
			Section   string `json:"section"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.ProjectID == "" {
			return "project_id는 필수입니다.", nil
		}

		result, err := d.Backend.Execute(ctx, "show", map[string]any{
			"id": p.ProjectID,
		})
		if err != nil {
			return fmt.Sprintf("프로젝트 문서 조회 실패: %v", err), nil
		}

		// If no section requested, extract and return table of contents.
		if p.Section == "" {
			return extractTableOfContents(p.ProjectID, result), nil
		}

		// Section requested: try to extract that section.
		return extractSection(p.ProjectID, p.Section, result), nil
	}
}

// extractTableOfContents parses the document result and returns section names.
func extractTableOfContents(projectID string, data json.RawMessage) string {
	// Try to parse as object with sections.
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Sprintf("프로젝트 %s 문서 구조를 파싱할 수 없습니다.", projectID)
	}

	var sections []string
	totalChars := len(data)

	// Look for "sections" key or iterate top-level keys.
	if secs, ok := doc["sections"].([]any); ok {
		for _, s := range secs {
			if name, ok := s.(string); ok {
				sections = append(sections, name)
			}
		}
	} else {
		// Use top-level keys as section names.
		for key := range doc {
			if key != "id" && key != "name" && key != "project_id" {
				sections = append(sections, key)
			}
		}
		sort.Strings(sections)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 프로젝트 %s 문서 목차\n\n", projectID))
	for i, s := range sections {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
	}
	sb.WriteString(fmt.Sprintf("\n총 문서 크기: %d자\n", totalChars))
	sb.WriteString("특정 섹션을 조회하려면 section 파라미터를 지정하세요.")
	return sb.String()
}

// extractSection pulls a named section from the document data.
func extractSection(projectID, section string, data json.RawMessage) string {
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return string(data)
	}

	// Exact match first.
	if val, ok := doc[section]; ok {
		b, err := json.MarshalIndent(val, "", "  ")
		if err != nil {
			return fmt.Sprintf("%v", val)
		}
		return fmt.Sprintf("## %s — %s\n\n%s", projectID, section, string(b))
	}

	// Fall back to case-insensitive match.
	sectionLower := strings.ToLower(section)
	for key, val := range doc {
		if strings.ToLower(key) == sectionLower {
			b, err := json.MarshalIndent(val, "", "  ")
			if err != nil {
				return fmt.Sprintf("%v", val)
			}
			return fmt.Sprintf("## %s — %s\n\n%s", projectID, key, string(b))
		}
	}

	return fmt.Sprintf("섹션 '%s'을(를) 찾을 수 없습니다. projects_get_document에서 section 없이 목차를 먼저 확인하세요.", section)
}

// ToolMemoryRecall returns an RLM-specific memory search tool.
// Lighter than the full memory tool — search-only with compact output.
func ToolMemoryRecall(d *toolctx.VegaDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		if d.MemoryStore == nil {
			return "메모리 스토어가 비활성 상태입니다.", nil
		}

		var p struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.Query == "" {
			return "query는 필수입니다.", nil
		}
		if p.MaxResults <= 0 {
			p.MaxResults = 3
		}

		// Embed query for semantic search if embedder is available.
		var queryVec []float32
		if d.MemoryEmbedder != nil {
			vec, err := d.MemoryEmbedder.EmbedQuery(ctx, p.Query)
			if err == nil {
				queryVec = vec
			}
		}

		opts := memory.SearchOpts{
			Limit: p.MaxResults,
		}
		if d.MemoryEmbedder == nil {
			opts.MinImportance = 0.6
		}

		results, err := d.MemoryStore.SearchFacts(ctx, p.Query, queryVec, opts)
		if err != nil {
			return fmt.Sprintf("메모리 검색 실패: %v", err), nil
		}

		if len(results) == 0 {
			return "관련 기억 없음.", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## 메모리 검색 결과 (%d건)\n\n", len(results)))
		for _, sr := range results {
			timeLabel := formatFactTime(sr.Fact)
			if timeLabel != "" {
				sb.WriteString(fmt.Sprintf("- [%.2f] {%s} (%s) %s\n",
					sr.Score, sr.Fact.Category, timeLabel, sr.Fact.Content))
			} else {
				sb.WriteString(fmt.Sprintf("- [%.2f] {%s} %s\n",
					sr.Score, sr.Fact.Category, sr.Fact.Content))
			}
		}

		return sb.String(), nil
	}
}
