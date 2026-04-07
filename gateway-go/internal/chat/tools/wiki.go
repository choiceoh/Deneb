package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/wiki"
)

// ToolWiki returns the unified wiki knowledge base tool.
// It replaces the memory tool when DENEB_WIKI_ENABLED is true.
func ToolWiki(d *toolctx.WikiDeps, workspaceDir string) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action     string   `json:"action"`
			Query      string   `json:"query"`
			Title      string   `json:"title"`
			ID         string   `json:"id"`
			Summary    string   `json:"summary"`
			Category   string   `json:"category"`
			Content    string   `json:"content"`
			Tags       []string `json:"tags"`
			Related    []string `json:"related"`
			Importance float64  `json:"importance"`
			Section    string   `json:"section"`
			Limit      int      `json:"limit"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}

		if d.Store == nil {
			return "위키가 비활성 상태입니다. DENEB_WIKI_ENABLED=true 로 활성화하세요.", nil
		}

		switch p.Action {
		case "search":
			return wikiSearch(ctx, d.Store, p.Query, p.Limit)
		case "read":
			return wikiRead(d.Store, p.Query, p.Section)
		case "index":
			return wikiIndex(d.Store, p.Category)
		case "write":
			return wikiWrite(d.Store, p.Query, p.Title, p.ID, p.Summary, p.Category, p.Content, p.Tags, p.Related, p.Importance)
		case "log":
			return wikiLog(workspaceDir, d.Store.DiaryDir(), p.Content)
		case "daily":
			return wikiDaily(d.Store.DiaryDir(), p.Limit)
		case "status":
			return wikiStatus(d.Store), nil
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: search, read, index, write, log, daily, status", p.Action), nil
		}
	}
}

func wikiSearch(ctx context.Context, store *wiki.Store, query string, limit int) (string, error) {
	if query == "" {
		return "query는 필수입니다.", nil
	}
	if limit <= 0 {
		limit = 10
	}

	results, err := store.Search(ctx, query, limit)
	if err != nil {
		return fmt.Sprintf("위키 검색 실패: %v", err), nil
	}
	if len(results) == 0 {
		return "검색 결과 없음.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 위키 검색 결과 (%d건)\n\n", len(results)))
	for _, r := range results {
		sb.WriteString(fmt.Sprintf("- **%s** (L%d, 관련도: %.2f)\n  %s\n\n",
			r.Path, r.Line, r.Score, truncate(r.Content, 200)))
	}
	return sb.String(), nil
}

func wikiRead(store *wiki.Store, path, section string) (string, error) {
	if path == "" {
		return "query에 페이지 경로를 지정하세요 (예: 기술/dgx-spark.md).", nil
	}

	// Ensure .md extension.
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}

	page, err := store.ReadPage(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("페이지 '%s' 없음. wiki index로 목록을 확인하세요.", path), nil
		}
		return fmt.Sprintf("페이지 읽기 실패: %v", err), nil
	}

	// If section specified, return just that section.
	if section != "" {
		content := page.Section(section)
		if content == "" {
			sections := page.Sections()
			return fmt.Sprintf("섹션 '%s' 없음. 사용 가능한 섹션: %s",
				section, strings.Join(sections, ", ")), nil
		}
		return fmt.Sprintf("## %s — %s\n\n%s", page.Meta.Title, section, content), nil
	}

	// Return full page.
	return string(page.Render()), nil
}

func wikiIndex(store *wiki.Store, category string) (string, error) {
	if category == "" {
		// Return master index.
		idx := store.GetIndex()
		return idx.Render(), nil
	}

	// Return category listing.
	pages, err := store.ListPages(category)
	if err != nil {
		return fmt.Sprintf("카테고리 '%s' 목록 실패: %v", category, err), nil
	}
	if len(pages) == 0 {
		return fmt.Sprintf("카테고리 '%s'에 페이지 없음.", category), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## %s 카테고리 (%d 페이지)\n\n", category, len(pages)))
	for _, p := range pages {
		page, err := store.ReadPage(p)
		if err != nil {
			sb.WriteString(fmt.Sprintf("- %s (읽기 실패)\n", p))
			continue
		}
		tags := ""
		if len(page.Meta.Tags) > 0 {
			tags = " [" + strings.Join(page.Meta.Tags, ", ") + "]"
		}
		sb.WriteString(fmt.Sprintf("- [[%s]] — %s%s\n", p, page.Meta.Title, tags))
	}
	return sb.String(), nil
}

func wikiWrite(store *wiki.Store, path, title, id, summary, category, content string, tags, related []string, importance float64) (string, error) {
	if title == "" {
		return "title은 필수입니다.", nil
	}
	if category == "" {
		return "category는 필수입니다.", nil
	}

	// Auto-generate path if not provided.
	if path == "" {
		slug := strings.ReplaceAll(strings.ToLower(title), " ", "-")
		path = category + "/" + slug + ".md"
	}
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}

	// Check if page already exists (update vs create).
	existing, _ := store.ReadPage(path)

	var page *wiki.Page
	if existing != nil {
		// Update existing page.
		page = existing
		page.Meta.Title = title
		if id != "" {
			page.Meta.ID = id
		}
		if summary != "" {
			page.Meta.Summary = summary
		}
		if len(tags) > 0 {
			page.Meta.Tags = tags
		}
		if len(related) > 0 {
			page.Meta.Related = related
		}
		if importance > 0 {
			page.Meta.Importance = importance
		}
		page.Meta.Updated = time.Now().Format("2006-01-02")
		if content != "" {
			page.Body = content
		}
	} else {
		// Create new page.
		page = wiki.NewPage(title, category, tags)
		page.Meta.ID = id
		page.Meta.Summary = summary
		page.Meta.Related = related
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

	if err := store.WritePage(path, page); err != nil {
		return fmt.Sprintf("위키 페이지 쓰기 실패: %v", err), nil
	}

	action := "생성"
	if existing != nil {
		action = "업데이트"
	}
	return fmt.Sprintf("위키 페이지 %s: %s (%s)", action, path, title), nil
}

func wikiLog(workspaceDir, diaryDir, content string) (string, error) {
	if content == "" {
		return "content에 일지 내용을 입력하세요.", nil
	}

	// Ensure diary directory exists.
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return fmt.Sprintf("일지 디렉토리 생성 실패: %v", err), nil
	}

	today := time.Now().Format("2006-01-02")
	path := filepath.Join(diaryDir, "diary-"+today+".md")

	// Append to today's diary file.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Sprintf("일지 파일 열기 실패: %v", err), nil
	}
	defer f.Close()

	now := time.Now().Format("15:04")
	entry := fmt.Sprintf("\n## %s\n\n%s\n", now, content)
	if _, err := f.WriteString(entry); err != nil {
		return fmt.Sprintf("일지 쓰기 실패: %v", err), nil
	}

	return fmt.Sprintf("일지 기록 완료: %s (%s)", path, now), nil
}

func wikiDaily(diaryDir string, limit int) (string, error) {
	if limit <= 0 {
		limit = 3
	}

	entries, err := os.ReadDir(diaryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "일지 없음. wiki log로 첫 일지를 작성하세요.", nil
		}
		return fmt.Sprintf("일지 디렉토리 읽기 실패: %v", err), nil
	}

	// Filter diary files and sort by name (date) descending.
	var diaryFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "diary-") && strings.HasSuffix(e.Name(), ".md") {
			diaryFiles = append(diaryFiles, e.Name())
		}
	}

	// Reverse sort (most recent first).
	for i, j := 0, len(diaryFiles)-1; i < j; i, j = i+1, j-1 {
		diaryFiles[i], diaryFiles[j] = diaryFiles[j], diaryFiles[i]
	}

	if len(diaryFiles) == 0 {
		return "일지 없음.", nil
	}
	if len(diaryFiles) > limit {
		diaryFiles = diaryFiles[:limit]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## 최근 일지 (%d일)\n\n", len(diaryFiles)))
	for _, name := range diaryFiles {
		path := filepath.Join(diaryDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			sb.WriteString(fmt.Sprintf("### %s\n(읽기 실패)\n\n", name))
			continue
		}
		content := string(data)
		if len([]rune(content)) > 2000 {
			content = string([]rune(content)[:2000]) + "\n...(잘림)"
		}
		sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", name, content))
	}

	return sb.String(), nil
}

func wikiStatus(store *wiki.Store) string {
	stats := store.Stats()

	var sb strings.Builder
	sb.WriteString("## 위키 상태\n\n")
	sb.WriteString(fmt.Sprintf("- 총 페이지: %d\n", stats.TotalPages))
	sb.WriteString(fmt.Sprintf("- 총 크기: %s\n", formatBytes(stats.TotalBytes)))
	sb.WriteString("\n### 카테고리별\n\n")

	for cat, count := range stats.CategoryCount {
		sb.WriteString(fmt.Sprintf("- %s: %d 페이지\n", cat, count))
	}

	return sb.String()
}

func truncate(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func formatBytes(b int64) string {
	switch {
	case b >= 1024*1024:
		return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	default:
		return fmt.Sprintf("%d B", b)
	}
}
