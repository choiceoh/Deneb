package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
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
			Type       string   `json:"type"`
			Confidence string   `json:"confidence"`
			Due        string   `json:"due"`
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
			return wikiRead(ctx, d.Store, p.Query, p.Section)
		case "index":
			return wikiIndex(d.Store, p.Category)
		case "write":
			return wikiWrite(d.Store, d.Contacts, p.Query, p.Title, p.ID, p.Summary, p.Category, p.Content, p.Tags, p.Related, p.Importance, p.Type, p.Confidence, p.Due)
		case "log":
			return wikiLog(workspaceDir, d.Store, p.Content)
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
	sb.WriteString(recallHeader(query, len(results), "wiki"))
	for i, r := range results {
		ref := RefWiki + strings.TrimSuffix(r.Path, ".md")
		meta := fmt.Sprintf("L%d · 관련도 %.2f", r.Line, r.Score)
		sb.WriteString(recallRow(i+1, ref, meta, r.Content))
	}
	sb.WriteString("자세한 내용은 `wiki(action=\"read\", query=\"w:...\")` (knowledge read와 동일 ref).")
	return sb.String(), nil
}

func wikiRead(ctx context.Context, store *wiki.Store, path, section string) (string, error) {
	if path == "" {
		return "query에 페이지 경로를 지정하세요 (예: 기술/dgx-spark.md).", nil
	}

	// Accept a namespaced "w:" ref so a citation from wiki search or knowledge
	// recall is interchangeable between the two tools' read paths.
	path = strings.TrimPrefix(strings.TrimSpace(path), RefWiki)

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

	// Return full page, with a compact graph-neighbor footer so the agent sees
	// what this page connects to at the point of reading and can choose to
	// follow it — on-demand graph self-exploration, not every-turn recall.
	out := string(page.Render())
	if conns, err := store.PageConnections(ctx, path, 6); err == nil && conns != "" {
		out += "\n\n---\n연결된 항목: " + conns
	}
	return out, nil
}

func wikiIndex(store *wiki.Store, category string) (string, error) {
	if category == "" {
		// Return master index.
		idx := store.Index()
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
	fmt.Fprintf(&sb, "## %s 카테고리 (%d 페이지)\n\n", category, len(pages))
	for _, p := range pages {
		page, err := store.ReadPage(p)
		if err != nil {
			fmt.Fprintf(&sb, "- %s (읽기 실패)\n", p)
			continue
		}
		tags := ""
		if len(page.Meta.Tags) > 0 {
			tags = " [" + strings.Join(page.Meta.Tags, ", ") + "]"
		}
		fmt.Fprintf(&sb, "- [[%s]] — %s%s\n", p, page.Meta.Title, tags)
	}
	return sb.String(), nil
}

func wikiWrite(store *wiki.Store, contactsStore *contacts.Store, path, title, id, summary, category, content string, tags, related []string, importance float64, pageType, confidence, due string) (string, error) {
	if title == "" {
		return "title은 필수입니다.", nil
	}
	if category == "" {
		return "category는 필수입니다.", nil
	}
	if !wiki.ValidateCategory(category) {
		return fmt.Sprintf("잘못된 카테고리: %s. 사용 가능: %s", category, strings.Join(wiki.Categories, ", ")), nil
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
		if pageType != "" {
			page.Meta.Type = pageType
		}
		if confidence != "" {
			page.Meta.Confidence = confidence
		}
		if due != "" {
			page.Meta.Due = due
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
		page.Meta.Type = pageType
		page.Meta.Confidence = confidence
		if due != "" {
			page.Meta.Due = due
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
	note := autoRecordPeople(store, contactsStore, page, category)
	return fmt.Sprintf("위키 페이지 %s: %s (%s)%s", action, path, title, note), nil
}

// autoRecordPeople ties a wiki write to the device address book. After a page is
// saved it (1) fills the page's own "## 연락처" when it is an 인물 page, and
// (2) creates/enriches 인물 pages for every inline [[link]] target that matches
// a contact. Returns a short Korean suffix for the write confirmation, or "".
//
// Runs after WritePage released its lock; the wiki Store methods it calls take
// the lock themselves, so there is no nested locking. Best-effort: a nil/empty
// address book or any enrichment error degrades to no note, never a failed write.
func autoRecordPeople(store *wiki.Store, contactsStore *contacts.Store, page *wiki.Page, category string) string {
	if store == nil || contactsStore == nil || contactsStore.Count() == 0 || page == nil {
		return ""
	}
	book := contactsToWiki(contactsStore.All())
	if len(book) == 0 {
		return ""
	}

	var notes []string
	// (1) The page is itself a person: record their contact details in place.
	if category == "인물" {
		if res, err := store.EnrichPeople([]string{page.Meta.Title}, book, false); err == nil && len(res.Updated) > 0 {
			notes = append(notes, "연락처 기록")
		}
	}
	// (2) People explicitly linked from the body: create or enrich their pages.
	if links := wiki.ExtractWikiLinks(page.Body); len(links) > 0 {
		if res, err := store.EnrichPeople(links, book, true); err == nil {
			if len(res.Created) > 0 {
				notes = append(notes, "인물 생성: "+strings.Join(res.Created, ", "))
			}
			if len(res.Updated) > 0 {
				notes = append(notes, "인물 연락처: "+strings.Join(res.Updated, ", "))
			}
		}
	}
	if len(notes) == 0 {
		return ""
	}
	return " · " + strings.Join(notes, " · ")
}

// contactsToWiki adapts address-book entries to the wiki package's own Contact
// shape (the two packages keep separate types to stay decoupled).
func contactsToWiki(in []contacts.Contact) []wiki.Contact {
	out := make([]wiki.Contact, 0, len(in))
	for _, c := range in {
		out = append(out, wiki.Contact{Name: c.Name, Phones: c.Phones, Emails: c.Emails, Org: c.Org})
	}
	return out
}

func wikiLog(_ string, store *wiki.Store, content string) (string, error) {
	if content == "" {
		return "content에 일지 내용을 입력하세요.", nil
	}

	now := time.Now()
	// Route through Store.AppendDiary so the diary FTS index sees the new
	// entry immediately — otherwise the agent's just-written entry would
	// only be recallable after the next gateway restart.
	if err := store.AppendDiary(content); err != nil {
		return fmt.Sprintf("일지 쓰기 실패: %v", err), nil
	}

	path := filepath.Join(store.DiaryDir(), "diary-"+now.Format("2006-01-02")+".md")
	return fmt.Sprintf("일지 기록 완료: %s (%s)", path, now.Format("15:04")), nil
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
	fmt.Fprintf(&sb, "## 최근 일지 (%d일)\n\n", len(diaryFiles))
	for _, name := range diaryFiles {
		path := filepath.Join(diaryDir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(&sb, "### %s\n(읽기 실패)\n\n", name)
			continue
		}
		content := string(data)
		if len([]rune(content)) > 2000 {
			content = string([]rune(content)[:2000]) + "\n...(잘림)"
		}
		fmt.Fprintf(&sb, "### %s\n%s\n\n", name, content)
	}

	return sb.String(), nil
}

func wikiStatus(store *wiki.Store) string {
	stats := store.Stats()

	var sb strings.Builder
	sb.WriteString("## 위키 상태\n\n")
	fmt.Fprintf(&sb, "- 총 페이지: %d\n", stats.TotalPages)
	fmt.Fprintf(&sb, "- 총 크기: %s\n", formatBytes(stats.TotalBytes))
	sb.WriteString("\n### 카테고리별\n\n")

	for cat, count := range stats.CategoryCount {
		fmt.Fprintf(&sb, "- %s: %d 페이지\n", cat, count)
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
