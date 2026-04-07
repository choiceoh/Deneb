package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
)

// ToolProjectsList returns a tool that lists projects from the wiki "프로젝트" category.
func ToolProjectsList(deps *toolctx.WikiDeps) toolctx.ToolFunc {
	return func(ctx context.Context, _ json.RawMessage) (string, error) {
		if deps.Store == nil {
			return "프로젝트 목록 기능이 비활성 상태입니다 (위키 미설정).", nil
		}
		pages, err := deps.Store.ListPages("프로젝트")
		if err != nil {
			return fmt.Sprintf("프로젝트 목록 조회 실패: %v", err), nil
		}
		if len(pages) == 0 {
			return "등록된 프로젝트가 없습니다.", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("프로젝트 %d개:\n\n", len(pages)))
		for _, relPath := range pages {
			page, pErr := deps.Store.ReadPage(relPath)
			if pErr != nil {
				continue
			}
			name := filepath.Base(relPath)
			name = strings.TrimSuffix(name, ".md")
			title := page.Meta.Title
			if title == "" {
				title = name
			}
			sb.WriteString(fmt.Sprintf("- **%s** (id: %s)", title, relPath))
			if len(page.Meta.Tags) > 0 {
				sb.WriteString(fmt.Sprintf(" [%s]", strings.Join(page.Meta.Tags, ", ")))
			}
			if page.Meta.Importance > 0 {
				sb.WriteString(fmt.Sprintf(" importance=%.1f", page.Meta.Importance))
			}
			sb.WriteByte('\n')
		}
		return sb.String(), nil
	}
}

// ToolProjectsGetField returns a tool that retrieves a specific field from a project page.
func ToolProjectsGetField(deps *toolctx.WikiDeps) toolctx.ToolFunc {
	type params struct {
		ProjectID string `json:"project_id"`
		Field     string `json:"field"`
	}
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		if deps.Store == nil {
			return "프로젝트 필드 조회 기능이 비활성 상태입니다 (위키 미설정).", nil
		}
		var p params
		if err := json.Unmarshal(raw, &p); err != nil {
			return "잘못된 파라미터입니다.", nil
		}
		if p.ProjectID == "" || p.Field == "" {
			return "project_id와 field는 필수입니다.", nil
		}

		page, err := deps.Store.ReadPage(p.ProjectID)
		if err != nil {
			return fmt.Sprintf("프로젝트를 찾을 수 없습니다: %s", p.ProjectID), nil
		}

		// Map field name to frontmatter value.
		switch strings.ToLower(p.Field) {
		case "title":
			return page.Meta.Title, nil
		case "category":
			return page.Meta.Category, nil
		case "tags":
			return strings.Join(page.Meta.Tags, ", "), nil
		case "related":
			return strings.Join(page.Meta.Related, ", "), nil
		case "created":
			return page.Meta.Created, nil
		case "updated":
			return page.Meta.Updated, nil
		case "importance":
			return fmt.Sprintf("%.2f", page.Meta.Importance), nil
		case "archived":
			return fmt.Sprintf("%v", page.Meta.Archived), nil
		default:
			// Try reading as a section name.
			section := page.Section(p.Field)
			if section != "" {
				return section, nil
			}
			return fmt.Sprintf("필드를 찾을 수 없습니다: %s", p.Field), nil
		}
	}
}

// ToolProjectsSearch returns a tool that searches projects via wiki FTS.
func ToolProjectsSearch(deps *toolctx.WikiDeps) toolctx.ToolFunc {
	type params struct {
		Query string `json:"query"`
		Limit int    `json:"limit,omitempty"`
	}
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		if deps.Store == nil {
			return "프로젝트 검색 기능이 비활성 상태입니다 (위키 미설정).", nil
		}
		var p params
		if err := json.Unmarshal(raw, &p); err != nil {
			return "잘못된 파라미터입니다.", nil
		}
		if p.Query == "" {
			return "query는 필수입니다.", nil
		}
		limit := p.Limit
		if limit <= 0 {
			limit = 10
		}

		results, err := deps.Store.Search(ctx, p.Query, limit)
		if err != nil {
			return fmt.Sprintf("검색 실패: %v", err), nil
		}

		// Filter to project pages only.
		var sb strings.Builder
		count := 0
		for _, r := range results {
			if !strings.HasPrefix(r.Path, "프로젝트/") {
				continue
			}
			count++
			sb.WriteString(fmt.Sprintf("- **%s** (score: %.2f)\n  %s\n", r.Path, r.Score, r.Content))
		}
		if count == 0 {
			return fmt.Sprintf("'%s' 관련 프로젝트를 찾을 수 없습니다.", p.Query), nil
		}
		return fmt.Sprintf("검색 결과 %d건:\n\n%s", count, sb.String()), nil
	}
}

// ToolProjectsGetDocument returns a tool that retrieves a project page.
// Without a section parameter, returns section headings (table of contents).
// With a section, returns that section's content.
func ToolProjectsGetDocument(deps *toolctx.WikiDeps) toolctx.ToolFunc {
	type params struct {
		ProjectID string `json:"project_id"`
		Section   string `json:"section,omitempty"`
	}
	return func(ctx context.Context, raw json.RawMessage) (string, error) {
		if deps.Store == nil {
			return "프로젝트 문서 조회 기능이 비활성 상태입니다 (위키 미설정).", nil
		}
		var p params
		if err := json.Unmarshal(raw, &p); err != nil {
			return "잘못된 파라미터입니다.", nil
		}
		if p.ProjectID == "" {
			return "project_id는 필수입니다.", nil
		}

		page, err := deps.Store.ReadPage(p.ProjectID)
		if err != nil {
			return fmt.Sprintf("프로젝트를 찾을 수 없습니다: %s", p.ProjectID), nil
		}

		if p.Section == "" {
			// Return table of contents.
			sections := page.Sections()
			if len(sections) == 0 {
				return fmt.Sprintf("**%s** — 섹션 없음 (본문만 존재)\n\n%s",
					page.Meta.Title, truncate(page.Body, 500)), nil
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("**%s** 목차:\n\n", page.Meta.Title))
			for i, s := range sections {
				sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, s))
			}
			return sb.String(), nil
		}

		// Return specific section.
		content := page.Section(p.Section)
		if content == "" {
			return fmt.Sprintf("섹션을 찾을 수 없습니다: %s", p.Section), nil
		}
		return content, nil
	}
}
