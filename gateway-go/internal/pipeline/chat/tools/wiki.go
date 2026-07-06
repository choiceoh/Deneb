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
			Paths      []string `json:"paths"`
			Title      string   `json:"title"`
			ID         string   `json:"id"`
			Summary    string   `json:"summary"`
			Category   string   `json:"category"`
			Content    string   `json:"content"`
			Tags       []string `json:"tags"`
			Related    []string `json:"related"`
			Cues       []string `json:"cues"`
			Sites      []string `json:"sites"`
			Kinds      []string `json:"kinds"`
			Supersedes []string `json:"supersedes"`
			Importance float64  `json:"importance"`
			Type       string   `json:"type"`
			Confidence string   `json:"confidence"`
			Due        string   `json:"due"`
			Section    string   `json:"section"`
			Limit      int      `json:"limit"`
			Force      bool     `json:"force"`
			Project    string   `json:"project"`
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
			if len(p.Paths) > 0 {
				return wikiReadBatch(ctx, d.Store, p.Paths, p.Section)
			}
			return wikiRead(ctx, d.Store, p.Query, p.Section)
		case "index":
			return wikiIndex(d.Store, p.Category)
		case "write":
			return wikiWrite(ctx, d.Store, d.Contacts, p.Query, p.Title, p.ID, p.Summary, p.Category, p.Content, p.Tags, p.Related, p.Cues, p.Sites, p.Kinds, p.Supersedes, p.Importance, p.Type, p.Confidence, p.Due, p.Force)
		case "log":
			return wikiLog(workspaceDir, d.Store, p.Content)
		case "daily":
			return wikiDaily(d.Store.DiaryDir(), p.Limit)
		case "status":
			return wikiStatus(d.Store), nil
		case "close":
			return wikiCloseProject(d.Store, p.Query, p.Content)
		case "reopen":
			return wikiReopenProject(d.Store, p.Query)
		case "ingest":
			return wikiIngest(ctx, d.Store, p.Query, p.Project, p.Title, p.Content, p.Force)
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: search, read, index, write, log, daily, status, close, reopen, ingest", p.Action), nil
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
	sb.WriteString("자세한 내용은 `wiki(action=\"read\", query=\"w:...\")` (knowledge read와 동일 ref). 여러 페이지가 필요하면 read 한 번에 `paths=[\"...\", \"...\"]`로 묶어 호출하세요 — 페이지마다 따로 부르지 말 것.")
	return sb.String(), nil
}

func wikiRead(ctx context.Context, store *wiki.Store, path, section string) (string, error) {
	if path == "" {
		return "query에 페이지 경로를 지정하세요 (예: 기술/dgx-spark.md).", nil
	}

	// Accept a namespaced "w:" ref so a citation from wiki search or knowledge
	// recall is interchangeable between the two tools' read paths.
	path = strings.TrimPrefix(strings.TrimSpace(path), RefWiki)

	// Escape guard: the store joins this path under the wiki root verbatim, so
	// a "../…" path from a prompt-injected turn would read arbitrary files.
	if err := wiki.ValidateExternalPath(path); err != nil {
		return fmt.Sprintf("잘못된 페이지 경로입니다 (위키 루트 밖 접근 불가): %s", path), nil //nolint:nilerr // tool surface: guidance to the model, not an error
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

	// Return full page, with a compact graph-neighbor footer so the agent sees
	// what this page connects to at the point of reading and can choose to
	// follow it — on-demand graph self-exploration, not every-turn recall.
	out := string(page.Render())
	if conns, err := store.PageConnections(ctx, path, 6); err == nil && conns != "" {
		out += "\n\n---\n연결된 항목: " + conns
	}
	return out, nil
}

// wikiReadBatchMaxPages bounds one batched read. Big enough for "every hit of
// a search" (search defaults to 10 rows, of which a handful matter), small
// enough that one call can't blow the tool-output budget with whole pages.
const wikiReadBatchMaxPages = 8

// wikiReadBatch reads several pages in ONE tool call. Interactive turns were
// dominated by wiki round-trips (3-6 single-page reads per turn, each costing
// a full LLM round on the cloud main model), so read accepts a paths batch:
// per-page output identical to a single read, joined under numbered page
// headers. A missing page fills its own slot instead of failing the batch.
func wikiReadBatch(ctx context.Context, store *wiki.Store, paths []string, section string) (string, error) {
	trimmed := make([]string, 0, len(paths))
	for _, p := range paths {
		if s := strings.TrimSpace(p); s != "" {
			trimmed = append(trimmed, s)
		}
	}
	if len(trimmed) == 0 {
		return "paths가 비어 있습니다. 읽을 페이지 경로 목록을 지정하세요.", nil
	}
	note := ""
	if len(trimmed) > wikiReadBatchMaxPages {
		note = fmt.Sprintf("\n\n(요청 %d개 중 앞 %d개만 읽음 — 나머지는 다음 read 호출로 이어서)",
			len(trimmed), wikiReadBatchMaxPages)
		trimmed = trimmed[:wikiReadBatchMaxPages]
	}
	var sb strings.Builder
	for i, path := range trimmed {
		out, err := wikiRead(ctx, store, path, section)
		if err != nil {
			out = fmt.Sprintf("페이지 읽기 실패: %v", err)
		}
		if i > 0 {
			sb.WriteString("\n\n")
		}
		fmt.Fprintf(&sb, "===== [%d/%d] %s =====\n%s", i+1, len(trimmed), path, out)
	}
	sb.WriteString(note)
	return sb.String(), nil
}

func wikiIndex(store *wiki.Store, category string) (string, error) {
	if category == "" {
		// Return master index — a snapshot, since Render walks the entry map
		// concurrent writers mutate in place.
		return store.SnapshotIndex().Render(), nil
	}

	// Escape guard: ListPages walks filepath.Join(root, category), so a
	// caller-supplied "../…" category would list files outside the wiki.
	if err := wiki.ValidateExternalPath(category); err != nil {
		return fmt.Sprintf("잘못된 카테고리 경로입니다 (위키 루트 밖 접근 불가): %s", category), nil //nolint:nilerr // tool surface: guidance to the model, not an error
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

func wikiWrite(ctx context.Context, store *wiki.Store, contactsStore *contacts.Store, path, title, id, summary, category, content string, tags, related, cues, sites, kinds, supersedes []string, importance float64, pageType, confidence, due string, force bool) (string, error) {
	if title == "" {
		return "title은 필수입니다.", nil
	}
	if category == "" {
		return "category는 필수입니다.", nil
	}
	if !wiki.ValidateCategory(category) {
		return fmt.Sprintf("잘못된 카테고리: %s. 사용 가능: %s", category, strings.Join(wiki.Categories, ", ")), nil
	}

	// Auto-generate path if not provided. Slashes in the title ("6/25 회의")
	// must not become directories — they'd mint phantom nested folders.
	if path == "" {
		slug := strings.ToLower(title)
		slug = strings.NewReplacer(" ", "-", "/", "-", "\\", "-").Replace(slug)
		path = category + "/" + slug + ".md"
	} else if err := wiki.ValidateExternalPath(path); err != nil {
		// Escape guard on caller-supplied paths — a "../…" write target would
		// land a file outside the wiki root (same contract as the read path).
		return fmt.Sprintf("잘못된 페이지 경로입니다 (위키 루트 밖 접근 불가): %s", path), nil //nolint:nilerr // tool surface: guidance to the model, not an error
	}
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}
	// Project layout: a flat 프로젝트/<name>.md is the legacy 대표페이지 form — route
	// it onto the in-folder slot (프로젝트/<name>/대표.md) so agent writes can't
	// resurrect flat pages after the layout migration (see wiki/project_layout.go).
	if np := wiki.NormalizeProjectPagePath(path); np != path {
		path = np
	}
	// Mint-time name hygiene: a NEW project folder must not carry mail-subject
	// debris (trailing dates, 요청/송부 suffixes); existing folders keep their
	// paths, and a cleaned twin routes into the existing clean folder.
	if np := store.CleanNewProjectRepPath(path); np != path {
		path = np
	}

	// Pre-write duplicate guard: creating a page whose subject an existing page
	// already covers is how the wiki splintered (2026-07 cleanup). When the target
	// doesn't exist yet and a near-match does, refuse and point at it — the agent
	// should update that page (or retry with force=true if genuinely distinct).
	if !force {
		if _, err := store.ReadPage(path); err != nil { // create, not update
			if !os.IsNotExist(err) {
				// Transient read failure (permissions, I/O) — routing it through
				// the create guard would give the model wrong guidance; fail the
				// write with the real error instead.
				return fmt.Sprintf("위키 페이지 읽기 실패 (쓰기 중단): %v", err), nil
			}
			hits := store.FindSimilarPages(ctx, wiki.SimilarQuery{
				Path: path, ID: id, Title: title, Category: category,
			}, 3)
			if len(hits) > 0 {
				var sb strings.Builder
				sb.WriteString("⚠️ 새 문서를 만들지 않았습니다 — 같은 주제로 보이는 기존 문서가 있습니다:\n")
				for _, h := range hits {
					fmt.Fprintf(&sb, "- %s — %s", h.Path, h.Title)
					if h.Summary != "" {
						fmt.Fprintf(&sb, " (%s)", h.Summary)
					}
					sb.WriteByte('\n')
				}
				sb.WriteString("기존 문서를 read 후 그 경로로 update 하세요. 정말 별개의 문서라면 force=true로 다시 호출하세요.")
				return sb.String(), nil
			}
		}
	}

	// Project 로그.md slot: the tool contract says events APPEND there (query
	// description: "사건·소식은 여기에 append"), but a body replace would let a model
	// sending only its new entry wipe the whole log. Append it as a dated H2
	// section instead (H2 = RotateProjectLog's rotation unit, mirroring the
	// dreamer's reroute); force=true keeps the raw replace for deliberate
	// rewrites.
	logAppend := false
	if name, ok := wiki.ProjectNameOf(path); ok && path == wiki.LogPagePath(name) && !force {
		logAppend = true
	}

	// Read-modify-write through UpdatePage so a concurrent writer of the same page
	// (the dreamer, the wiki-research turn, mail analysis) can't clobber this edit,
	// and so a content-less "update" preserves the body that's actually on disk at
	// write time rather than a copy read in a separate, earlier call. page/existed
	// are captured for the post-write people enrichment and the result message.
	var page *wiki.Page
	existed := false
	err := store.UpdatePage(path, func(existing *wiki.Page) (*wiki.Page, error) {
		if existing != nil {
			// Update existing page.
			existed = true
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
			if len(cues) > 0 {
				page.Meta.Cues = cues
			}
			if len(sites) > 0 {
				page.Meta.Sites = sites
			}
			if len(kinds) > 0 {
				page.Meta.Kinds = kinds
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
				if logAppend {
					page.Body = appendProjectLogSection(page.Body, content)
				} else {
					page.Body = content
				}
			}
			return page, nil
		}
		// Create new page.
		page = wiki.NewPage(title, category, tags)
		page.Meta.ID = id
		page.Meta.Summary = summary
		page.Meta.Related = related
		page.Meta.Cues = cues
		page.Meta.Sites = sites
		page.Meta.Kinds = kinds
		if importance > 0 {
			page.Meta.Importance = importance
		}
		page.Meta.Type = pageType
		page.Meta.Confidence = confidence
		if due != "" {
			page.Meta.Due = due
		}
		switch {
		case content != "" && logAppend:
			// A fresh 로그.md starts as a dated section so rotation works from
			// the first entry.
			page.Body = appendProjectLogSection("", content)
		case content != "":
			page.Body = content
		default:
			page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성\n",
				title, time.Now().Format("2006-01-02"))
		}
		return page, nil
	})
	if err != nil {
		return fmt.Sprintf("위키 페이지 쓰기 실패: %v", err), nil
	}
	marked, failed := markSupersededPages(store, supersedes, path)

	action := "생성"
	if existed {
		action = "업데이트"
		if logAppend && content != "" {
			action = "업데이트 (로그에 섹션 append — 기존 항목 유지)"
		}
	}
	note := autoRecordPeople(store, contactsStore, page, category)
	if len(marked) > 0 {
		note += fmt.Sprintf(" · 대체 표시 %d건", len(marked))
	}
	if len(failed) > 0 {
		note += fmt.Sprintf(" · 대체 표시 실패: %s", strings.Join(failed, ", "))
	}
	return fmt.Sprintf("위키 페이지 %s: %s (%s)%s", action, path, title, note), nil
}

// appendProjectLogSection appends a write's content to a project 로그.md body
// as a dated H2 section — H2 is the unit RotateProjectLog rotates on, matching
// the dreamer's reroute (dreamer_guards.go appendProjectLog). Content that
// already opens with its own H2 heading is appended verbatim (no empty dated
// shell above it). body may be "" (fresh log page).
func appendProjectLogSection(body, content string) string {
	entry := strings.TrimSpace(content)
	if !strings.HasPrefix(entry, "## ") {
		entry = "## " + time.Now().Format("2006-01-02") + "\n" + entry
	}
	if strings.TrimSpace(body) == "" {
		return entry + "\n"
	}
	return strings.TrimRight(body, "\n") + "\n\n" + entry + "\n"
}

// wikiCloseProject retires a project (종결): closure record on the 대표페이지 +
// the whole folder archived + removed from the active stage (candidates,
// digests, research, reviewer). Nothing moves or is deleted; reopen reverses.
func wikiCloseProject(store *wiki.Store, ref, note string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "query에 종결할 프로젝트 이름(또는 대표페이지 경로)을 지정하세요.", nil
	}
	res, err := store.CloseProject(ref, note, time.Now())
	if err != nil {
		return fmt.Sprintf("프로젝트 종결 실패: %v", err), nil
	}
	msg := fmt.Sprintf("프로젝트 종결 완료: %s — 문서 %d건 보관 처리, 활성 목록(메일 연결 후보·모아보기·리서치)에서 제외됨.",
		res.RepPath, res.Archived)
	if strings.TrimSpace(note) != "" {
		msg += " 결과 기록: " + strings.TrimSpace(note)
	}
	return msg + " 재개하려면 wiki(action=\"reopen\").", nil
}

// wikiReopenProject reverses a closure (재개).
func wikiReopenProject(store *wiki.Store, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "query에 재개할 프로젝트 이름(또는 대표페이지 경로)을 지정하세요.", nil
	}
	res, err := store.ReopenProject(ref, time.Now())
	if err != nil {
		return fmt.Sprintf("프로젝트 재개 실패: %v", err), nil
	}
	return fmt.Sprintf("프로젝트 재개 완료: %s — 문서 %d건 복원, 활성 목록에 다시 포함됨.", res.RepPath, res.Restored), nil
}

func markSupersededPages(store *wiki.Store, oldPaths []string, newPath string) (marked, failed []string) {
	if store == nil || newPath == "" {
		return nil, nil
	}
	for _, old := range oldPaths {
		old = strings.TrimSpace(old)
		if old == "" {
			continue
		}
		if err := store.MarkSuperseded(old, newPath); err != nil {
			failed = append(failed, old)
			continue
		}
		marked = append(marked, old)
	}
	return marked, failed
}

// autoRecordPeople ties a wiki write to the device address book. After a page is
// saved it (1) fills the page's own "## 연락처" when it is an 인물 page, and
// (2) creates/enriches 인물 pages for every inline [[link]] target that matches
// a contact. Returns a short Korean suffix for the write confirmation, or "".
//
// Runs after UpdatePage released writeMu; the wiki Store methods it calls
// (EnrichPeople → enrich/createPersonPage → UpdatePage) take writeMu themselves,
// so there is no nested locking. Best-effort: a nil/empty address book or any
// enrichment error degrades to no note, never a failed write.
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

	sb.WriteString(memorySystemStatus(store))
	return sb.String()
}

// memorySystemStatus appends the wider memory-system panel to the wiki status:
// dreaming liveness, diary/captures, MEMORY.md budget pressure, polaris and
// transcript footprints, last backup. One glance answers "기억 상태 어때?" —
// the question that previously took a manual filesystem dig (the very dig the
// 2026-06-10 memory audit had to do to discover dead stores).
func memorySystemStatus(store *wiki.Store) string {
	var sb strings.Builder
	sb.WriteString("\n## 기억 시스템\n\n")

	// Dreaming liveness from the persisted state file.
	if last := dreamLastRun(store.Dir()); !last.IsZero() {
		fmt.Fprintf(&sb, "- 드리밍: 마지막 사이클 %s (%s 전)\n",
			last.Format("01-02 15:04"), humanAge(time.Since(last)))
	} else {
		sb.WriteString("- 드리밍: 실행 기록 없음\n")
	}

	// Diary + captures live under the memory root.
	memRoot := filepath.Dir(store.DiaryDir())
	if n, size := dirFootprint(store.DiaryDir(), ".md"); n > 0 {
		fmt.Fprintf(&sb, "- 다이어리: %d파일 %s\n", n, formatBytes(size))
	}
	if n, size := dirFootprint(filepath.Join(memRoot, "captures"), ".md"); n > 0 {
		fmt.Fprintf(&sb, "- 캡처 원문: %d건 %s\n", n, formatBytes(size))
	}

	stateDir := memoryStateDir()
	if stateDir == "" {
		return sb.String()
	}

	// MEMORY.md budget pressure (32K loads into the prompt; the rest waits
	// for dream curation).
	if info, err := os.Stat(filepath.Join(stateDir, "workspace", "MEMORY.md")); err == nil {
		line := fmt.Sprintf("- MEMORY.md: %s", formatBytes(info.Size()))
		if over := info.Size() - 32_000; over > 0 {
			line += fmt.Sprintf(" (프롬프트 예산 32KB 초과분 %s — 드림 큐레이션 대기)", formatBytes(over))
		}
		sb.WriteString(line + "\n")
	}

	if n, size := dirFootprint(filepath.Join(stateDir, "polaris", "messages"), ".jsonl"); n > 0 {
		fmt.Fprintf(&sb, "- Polaris 세션 메모리: %d세션 %s\n", n, formatBytes(size))
	}
	if n, size := dirFootprint(filepath.Join(stateDir, "transcripts"), ".jsonl"); n > 0 {
		fmt.Fprintf(&sb, "- 트랜스크립트: %d세션 %s\n", n, formatBytes(size))
	}
	if last := backupLastRun(stateDir); !last.IsZero() {
		fmt.Fprintf(&sb, "- 오프사이트 백업: 마지막 %s (%s 전)\n",
			last.Format("01-02 15:04"), humanAge(time.Since(last)))
	}
	return sb.String()
}

// memoryStateDir resolves the gateway state dir (env override, else ~/.deneb).
func memoryStateDir() string {
	if d := strings.TrimSpace(os.Getenv("DENEB_STATE_DIR")); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb")
}

// dreamLastRun reads LastDreamMs from the dreamer's persisted state.
func dreamLastRun(wikiDir string) time.Time {
	data, err := os.ReadFile(filepath.Join(wikiDir, ".diary-process-state.json"))
	if err != nil {
		return time.Time{}
	}
	var st struct {
		LastDreamMs int64 `json:"lastDreamMs"`
	}
	if json.Unmarshal(data, &st) != nil || st.LastDreamMs <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(st.LastDreamMs)
}

// backupLastRun reads the memory-backup task's LastRunAt from the autonomous
// service state file (a flat {"task-name": unixMillis} map).
func backupLastRun(stateDir string) time.Time {
	data, err := os.ReadFile(filepath.Join(stateDir, "autonomous_state.json"))
	if err != nil {
		return time.Time{}
	}
	var st map[string]int64
	if json.Unmarshal(data, &st) != nil {
		return time.Time{}
	}
	if ms := st["memory-backup"]; ms > 0 {
		return time.UnixMilli(ms)
	}
	return time.Time{}
}

// dirFootprint counts files with the given extension directly under dir and
// sums their sizes.
func dirFootprint(dir, ext string) (int, int64) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	var n int
	var size int64
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ext) {
			continue
		}
		if info, err := e.Info(); err == nil {
			n++
			size += info.Size()
		}
	}
	return n, size
}

// humanAge renders a duration as a compact Korean age ("3시간", "2일").
func humanAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "방금"
	case d < time.Hour:
		return fmt.Sprintf("%d분", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d시간", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일", int(d.Hours()/24))
	}
}
