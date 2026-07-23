package wikitool

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/contacts"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// ToolWiki returns the unified wiki knowledge base tool.
// It replaces the memory tool when DENEB_WIKI_ENABLED is true.
func ToolWiki(d *tooldeps.WikiDeps, workspaceDir string) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action   string   `json:"action"`
			Query    string   `json:"query"`
			Plan     string   `json:"plan"`
			Paths    []string `json:"paths"`
			Scopes   []string `json:"scopes"`
			Intent   string   `json:"intent"`
			Title    string   `json:"title"`
			ID       string   `json:"id"`
			Summary  string   `json:"summary"`
			Category string   `json:"category"`
			Content  string   `json:"content"`
			Tags     []string `json:"tags"`
			Related  []string `json:"related"`
			Cues     []string `json:"cues"`
			Client   string   `json:"client"`
			Sites    []string `json:"sites"`
			Stage    string   `json:"stage"`
			Program  string   `json:"program"`
			// 현장(site) authoring fields — used by action="write-site".
			Address              string   `json:"address"`
			Status               string   `json:"status"`
			Capacity             float64  `json:"capacity"`
			ContractDate         string   `json:"contract_date"`
			ConstructionStart    string   `json:"construction_start"`
			ModuleDelivery       string   `json:"module_delivery"`
			PreUseInspection     string   `json:"pre_use_inspection"`
			CompletionInspection string   `json:"completion_inspection"`
			Kinds                []string `json:"kinds"`
			Supersedes           []string `json:"supersedes"`
			Importance           float64  `json:"importance"`
			Type                 string   `json:"type"`
			Confidence           string   `json:"confidence"`
			Due                  string   `json:"due"`
			Section              string   `json:"section"`
			FromLine             int      `json:"from_line"`
			MaxLines             int      `json:"max_lines"`
			Limit                int      `json:"limit"`
			Date                 string   `json:"date"`
			Force                bool     `json:"force"`
			Explain              bool     `json:"explain"`
			Rerank               bool     `json:"rerank"`
			Project              string   `json:"project"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}

		if d.Store == nil {
			return "위키가 비활성 상태입니다. DENEB_WIKI_ENABLED=true 로 활성화하세요.", nil
		}

		switch p.Action {
		case "search":
			return wikiSearchWithPlan(ctx, d.Store, p.Query, p.Plan, p.Intent, p.Scopes, p.Limit, p.Explain, p.Rerank)
		case "read":
			if len(p.Paths) > 0 {
				return wikiReadBatchRange(ctx, d.Store, p.Paths, p.Section, p.FromLine, p.MaxLines)
			}
			return wikiReadRange(ctx, d.Store, p.Query, p.Section, p.FromLine, p.MaxLines)
		case "index":
			return wikiIndex(d.Store, p.Category)
		case "write":
			return wikiWrite(ctx, d.Store, d.Contacts, p.Query, p.Title, p.ID, p.Summary, p.Category, p.Content, p.Tags, p.Related, p.Cues, p.Client, p.Sites, p.Stage, p.Program, p.Kinds, p.Supersedes, p.Importance, p.Type, p.Confidence, p.Due, p.Force)
		case "write-site":
			return wikiWriteSite(d.Store, p.Project, p.Title, wiki.SiteFields{
				Client: p.Client, Address: p.Address, Status: p.Status, Capacity: p.Capacity,
				Kinds:                p.Kinds,
				ContractDate:         p.ContractDate,
				ConstructionStart:    p.ConstructionStart,
				ModuleDelivery:       p.ModuleDelivery,
				PreUseInspection:     p.PreUseInspection,
				CompletionInspection: p.CompletionInspection,
				Summary:              p.Summary,
				Note:                 p.Content,
			})
		case "seed-sites":
			return wikiSeedSites(d.Store, p.Project)
		case "log":
			return wikiLog(workspaceDir, d.Store, p.Content)
		case "daily":
			if strings.TrimSpace(p.Date) != "" {
				return wikiDailyByDate(d.Store.DiaryDir(), p.Date, p.FromLine, p.MaxLines)
			}
			return wikiDaily(d.Store.DiaryDir(), p.Limit)
		case "status":
			return wikiStatusWithDoctor(ctx, d.Store), nil
		case "close":
			return wikiCloseProject(d.Store, p.Query, p.Content)
		case "reopen":
			return wikiReopenProject(d.Store, p.Query)
		case "ingest":
			return wikiIngest(ctx, d.Store, p.Query, p.Project, p.Title, p.Content, p.Force)
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: search, read, index, write, write-site, seed-sites, log, daily, status, close, reopen, ingest", p.Action), nil
		}
	}
}

// wikiWriteSite creates or edits a 현장 page in the 현장 공통 포맷
// (프로젝트/<project>/현장/<name>.md). Partial edits preserve unset fields, so the
// agent can advance a site's 상태 + fill a milestone (계약일→준공검사일) over time.
func wikiWriteSite(store *wiki.Store, project, name string, f wiki.SiteFields) (string, error) {
	if store == nil {
		return "위키가 비활성 상태입니다.", nil
	}
	if strings.TrimSpace(project) == "" || strings.TrimSpace(name) == "" {
		return "write-site 에는 project(프로젝트 폴더명)와 title(현장명)이 필요합니다.", nil
	}
	path, err := store.UpsertSitePage(project, name, f)
	if err != nil {
		return "", fmt.Errorf("현장 페이지 저장 실패: %w", err)
	}
	msg := fmt.Sprintf("현장 페이지 저장됨: %s (상태 %s)", path, orDash(f.Status))
	if notes := wiki.DroppedEnumNotes("", f.Kinds); len(notes) > 0 {
		msg += "\n⚠️ " + strings.Join(notes, "\n⚠️ ")
	}
	return msg, nil
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "미분류"
	}
	return s
}

// wikiSeedSites bootstraps 현장 page stubs from projects' 대표페이지 Meta.Sites so
// existing projects enter the 현장 공통 포맷 in one shot. project 미지정이면 전체.
func wikiSeedSites(store *wiki.Store, project string) (string, error) {
	if store == nil {
		return "위키가 비활성 상태입니다.", nil
	}
	created, err := store.SeedSitePages(strings.TrimSpace(project))
	if err != nil {
		return "", fmt.Errorf("현장 시드 실패: %w", err)
	}
	if len(created) == 0 {
		return "새로 만든 현장 페이지 없음 — 이미 모두 있거나 대표페이지 sites가 없습니다.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "현장 페이지 %d개 생성 (상태·용량·공정 일정은 비어 있으니 write-site로 채우세요):\n", len(created))
	for _, p := range created {
		b.WriteString("- " + p + "\n")
	}
	return b.String(), nil
}

func wikiSearch(ctx context.Context, store *wiki.Store, query string, limit int) (string, error) {
	return wikiSearchWithPlan(ctx, store, query, "", "", nil, limit, false, false)
}

func wikiSearchWithPlan(ctx context.Context, store *wiki.Store, query, planText, intent string, scopes []string, limit int, explain, forceRerank bool) (string, error) {
	if strings.TrimSpace(planText) == "" && query == "" {
		return "query 또는 plan은 필수입니다.", nil
	}
	if query == "" {
		query = planText
	}
	if limit <= 0 {
		limit = 10
	}

	var (
		results []wiki.SearchResult
		err     error
	)
	if strings.TrimSpace(planText) != "" || len(scopes) > 0 || strings.TrimSpace(intent) != "" || explain || forceRerank {
		plan := wiki.ParseQueryPlan(planText)
		if len(plan.Clauses) == 0 {
			plan = wiki.ParseQueryPlan(query)
		}
		if strings.TrimSpace(intent) != "" {
			plan.Intent = strings.TrimSpace(intent)
		}
		plan.Scopes = append(plan.Scopes, scopes...)
		plan.Explain = explain
		plan.ForceRerank = forceRerank
		report, searchErr := store.SearchPlan(ctx, plan, limit)
		results, err = report.Results, searchErr
	} else {
		results, err = store.Search(ctx, query, limit)
	}
	if err != nil {
		return fmt.Sprintf("위키 검색 실패: %v", err), nil
	}
	if len(results) == 0 {
		return "검색 결과 없음.", nil
	}

	var sb strings.Builder
	sb.WriteString(toolport.RecallHeader(query, len(results), "wiki"))
	for i, r := range results {
		ref := toolport.RefWiki + strings.TrimSuffix(r.Path, ".md")
		lineRef := fmt.Sprintf("L%d", r.Line)
		if r.EndLine > r.Line {
			lineRef = fmt.Sprintf("L%d-L%d", r.Line, r.EndLine)
		}
		meta := fmt.Sprintf("%s · 관련도 %.2f", lineRef, r.Score)
		if len(r.Context) > 0 {
			meta += " · " + strings.Join(r.Context, " › ")
		}
		sb.WriteString(toolport.RecallRow(i+1, ref, meta, r.Content))
	}
	sb.WriteString("자세한 내용은 `wiki(action=\"read\", query=\"w:...\", from_line=N, max_lines=M)`로 정확한 줄 범위를 읽으세요. 여러 페이지가 필요하면 read 한 번에 `paths=[\"...\", \"...\"]`로 묶어 호출하세요 — 페이지마다 따로 부르지 말 것.")
	return sb.String(), nil
}

func wikiRead(ctx context.Context, store *wiki.Store, path, section string) (string, error) {
	return wikiReadRange(ctx, store, path, section, 0, 0)
}

func wikiReadRange(ctx context.Context, store *wiki.Store, path, section string, fromLine, maxLines int) (string, error) {
	if path == "" {
		return "query에 페이지 경로를 지정하세요 (예: 기술/dgx-spark.md).", nil
	}

	// Accept a namespaced "w:" ref so a citation from wiki search or knowledge
	// recall is interchangeable between the two tools' read paths.
	path = strings.TrimPrefix(strings.TrimSpace(path), toolport.RefWiki)

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

	// 효용 접지: a model-driven page open is observed USE — the strongest recall
	// utility signal, unlike mere evidence injection (bridge-evidence adoption).
	// Best-effort derived telemetry by ledger contract; must never fail a read.
	_ = store.RecordRecallEvents([]wiki.RecallEvent{{
		Path:    path,
		Event:   wiki.RecallEventRead,
		Session: toolport.SessionKeyFromContext(ctx),
	}})

	// If section specified, return just that section. Partial reads (section and
	// line-range) still carry the provenance footer — search steers the agent to
	// range reads, so the cite→locate→fetch loop must work there too, not only on
	// a whole-page reopen.
	if section != "" {
		content := page.Section(section)
		if content == "" {
			sections := page.Sections()
			return fmt.Sprintf("섹션 '%s' 없음. 사용 가능한 섹션: %s",
				section, strings.Join(sections, ", ")), nil
		}
		out := fmt.Sprintf("## %s — %s\n\n%s", page.Meta.Title, section, content)
		return withProvenanceFooter(out, store, page.Meta.Sources), nil
	}
	if fromLine > 0 || maxLines > 0 {
		return withProvenanceFooter(formatWikiLineRange(path, page, fromLine, maxLines), store, page.Meta.Sources), nil
	}

	// Return full page, with a compact graph-neighbor footer so the agent sees
	// what this page connects to at the point of reading and can choose to
	// follow it — on-demand graph self-exploration, not every-turn recall.
	out := string(page.Render())
	if conns, err := store.PageConnections(ctx, path, 6); err == nil && conns != "" {
		out += "\n\n---\n연결된 항목: " + conns
	}
	return withProvenanceFooter(out, store, page.Meta.Sources), nil
}

// withProvenanceFooter appends the resolved provenance block to a read output
// when the page carries episode refs, so section/range/full reads all cite
// their source the same way. A no-op when there is no provenance.
func withProvenanceFooter(out string, store *wiki.Store, sources []string) string {
	if footer := formatProvenanceFooter(store, sources); footer != "" {
		return out + "\n\n" + footer
	}
	return out
}

// formatProvenanceFooter turns a page's raw episode refs into an actionable
// citation block: each ref resolved to the diary day it came from, plus how to
// pull that day (wiki daily date=…) to verify the fact against its source. The
// refs are already visible in the rendered frontmatter, but as opaque tokens;
// this labels them and closes the cite→locate→fetch loop. Empty when the page
// carries no provenance.
func formatProvenanceFooter(store *wiki.Store, sources []string) string {
	resolved := store.ResolveEpisodes(sources)
	if len(resolved) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("---\n출처(provenance) — 이 페이지 사실이 합성된 다이어리 에피소드:")
	for _, ep := range resolved {
		switch {
		case ep.Malformed:
			fmt.Fprintf(&sb, "\n- %s (형식 불명 — 해석 불가)", ep.Ref)
		case ep.Date == "":
			fmt.Fprintf(&sb, "\n- %s (날짜 미상 · 내용 해시로만 식별)", ep.Ref)
		case ep.Exists:
			fmt.Fprintf(&sb, "\n- %s 다이어리 — 원문 확인: wiki action=daily date=%s", ep.Date, ep.Date)
		default:
			fmt.Fprintf(&sb, "\n- %s 다이어리 (원본 정리됨 — 파일 없음)", ep.Date)
		}
	}
	return sb.String()
}

const wikiReadMaxLines = 400

func formatWikiLineRange(path string, page *wiki.Page, fromLine, maxLines int) string {
	lines := strings.Split(string(page.Render()), "\n")
	if fromLine <= 0 {
		fromLine = 1
	}
	if fromLine > len(lines) {
		return fmt.Sprintf("페이지 '%s'는 %d줄입니다. from_line=%d는 범위를 벗어납니다.", path, len(lines), fromLine)
	}
	if maxLines <= 0 {
		maxLines = 120
	}
	maxLines = min(maxLines, wikiReadMaxLines)
	end := min(len(lines), fromLine-1+maxLines)
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s — %s L%d-L%d\n\n", page.Meta.Title, path, fromLine, end)
	for i := fromLine - 1; i < end; i++ {
		fmt.Fprintf(&sb, "L%d: %s\n", i+1, lines[i])
	}
	if end < len(lines) {
		fmt.Fprintf(&sb, "\n[계속: from_line=%d, max_lines=%d]", end+1, maxLines)
	}
	return strings.TrimRight(sb.String(), "\n")
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
	return wikiReadBatchRange(ctx, store, paths, section, 0, 0)
}

func wikiReadBatchRange(ctx context.Context, store *wiki.Store, paths []string, section string, fromLine, maxLines int) (string, error) {
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
		out, err := wikiReadRange(ctx, store, path, section, fromLine, maxLines)
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

type wikiWriteRequest struct {
	title      string
	id         string
	summary    string
	category   string
	content    string
	tags       []string
	related    []string
	cues       []string
	client     string
	sites      []string
	stage      string
	program    string
	kinds      []string
	supersedes []string
	importance float64
	pageType   string
	confidence string
	due        string
	force      bool
}

func wikiWrite(ctx context.Context, store *wiki.Store, contactsBook tooldeps.ContactsBook, path, title, id, summary, category, content string, tags, related, cues []string, client string, sites []string, stage, program string, kinds, supersedes []string, importance float64, pageType, confidence, due string, force bool) (string, error) {
	req := wikiWriteRequest{
		title: title, id: id, summary: summary, category: category, content: content,
		tags: tags, related: related, cues: cues, client: client, sites: sites,
		stage: stage, program: program, kinds: kinds, supersedes: supersedes, importance: importance,
		pageType: pageType, confidence: confidence, due: due, force: force,
	}
	if guidance := validateWikiWrite(req); guidance != "" {
		return guidance, nil
	}

	path, guidance := resolveWikiWritePath(store, path, req)
	if guidance != "" {
		return guidance, nil
	}
	if guidance := duplicateWikiWriteGuidance(ctx, store, path, req); guidance != "" {
		return guidance, nil
	}

	logAppend := shouldAppendProjectLog(path, req.force)
	page, existed, err := persistWikiWrite(store, path, req, logAppend)
	if err != nil {
		return fmt.Sprintf("위키 페이지 쓰기 실패: %v", err), nil
	}
	marked, failed := markSupersededPages(store, req.supersedes, path)
	note := autoRecordPeople(store, contactsBook, page, req.category)
	return formatWikiWriteResult(path, req, note, existed, logAppend, marked, failed), nil
}

func validateWikiWrite(req wikiWriteRequest) string {
	if req.title == "" {
		return "title은 필수입니다."
	}
	if req.category == "" {
		return "category는 필수입니다."
	}
	if !wiki.ValidateCategory(req.category) {
		return fmt.Sprintf("잘못된 카테고리: %s. 사용 가능: %s", req.category, strings.Join(wiki.Categories(), ", "))
	}
	return ""
}

// resolveWikiWritePath owns the write target policy: generated slugs, escape
// rejection, project representative-page layout, and mint-time name cleanup.
func resolveWikiWritePath(store *wiki.Store, path string, req wikiWriteRequest) (string, string) {
	if path == "" {
		slug := strings.ToLower(req.title)
		slug = strings.NewReplacer(" ", "-", "/", "-", "\\", "-").Replace(slug)
		path = req.category + "/" + slug + ".md"
	} else if err := wiki.ValidateExternalPath(path); err != nil {
		return "", fmt.Sprintf("잘못된 페이지 경로입니다 (위키 루트 밖 접근 불가): %s", path)
	}
	if !strings.HasSuffix(path, ".md") {
		path += ".md"
	}
	path = wiki.NormalizeProjectPagePath(path)
	path = store.CleanNewProjectRepPath(path)
	return path, ""
}

// duplicateWikiWriteGuidance applies only to creates. Existing targets are
// updates, while a forced create deliberately bypasses similarity checks.
func duplicateWikiWriteGuidance(ctx context.Context, store *wiki.Store, path string, req wikiWriteRequest) string {
	if req.force {
		return ""
	}
	_, err := store.ReadPage(path)
	if err == nil {
		return ""
	}
	if !os.IsNotExist(err) {
		return fmt.Sprintf("위키 페이지 읽기 실패 (쓰기 중단): %v", err)
	}

	hits := store.FindSimilarPages(ctx, wiki.SimilarQuery{
		Path: path, ID: req.id, Title: req.title, Category: req.category,
	}, 3)
	if len(hits) == 0 {
		return ""
	}
	return formatDuplicateWikiWriteGuidance(hits)
}

func formatDuplicateWikiWriteGuidance(hits []wiki.SimilarHit) string {
	var sb strings.Builder
	sb.WriteString("⚠️ 새 문서를 만들지 않았습니다 — 같은 주제로 보이는 기존 문서가 있습니다:\n")
	for _, hit := range hits {
		fmt.Fprintf(&sb, "- %s — %s", hit.Path, hit.Title)
		if hit.Summary != "" {
			fmt.Fprintf(&sb, " (%s)", hit.Summary)
		}
		sb.WriteByte('\n')
	}
	sb.WriteString("기존 문서를 read 후 그 경로로 update 하세요. 정말 별개의 문서라면 force=true로 다시 호출하세요.")
	return sb.String()
}

func shouldAppendProjectLog(path string, force bool) bool {
	name, ok := wiki.ProjectNameOf(path)
	return ok && path == wiki.LogPagePath(name) && !force
}

// persistWikiWrite keeps the read-modify-write inside Store.UpdatePage so a
// concurrent writer cannot clobber the page between a separate read and write.
func persistWikiWrite(store *wiki.Store, path string, req wikiWriteRequest, logAppend bool) (*wiki.Page, bool, error) {
	var page *wiki.Page
	var existed bool
	err := store.UpdatePage(path, func(existing *wiki.Page) (*wiki.Page, error) {
		page, existed = mergeWikiWrite(existing, req, logAppend)
		return page, nil
	})
	return page, existed, err
}

func mergeWikiWrite(existing *wiki.Page, req wikiWriteRequest, logAppend bool) (*wiki.Page, bool) {
	if existing == nil {
		return newWikiWritePage(req, logAppend), false
	}
	updateWikiWritePage(existing, req, logAppend)
	return existing, true
}

func updateWikiWritePage(page *wiki.Page, req wikiWriteRequest, logAppend bool) {
	page.Meta.Title = req.title
	if req.id != "" {
		page.Meta.ID = req.id
	}
	if req.summary != "" {
		page.Meta.Summary = req.summary
	}
	if len(req.tags) > 0 {
		page.Meta.Tags = req.tags
	}
	if len(req.related) > 0 {
		page.Meta.Related = req.related
	}
	if len(req.cues) > 0 {
		page.Meta.Cues = req.cues
	}
	if strings.TrimSpace(req.client) != "" {
		page.Meta.Client = req.client
	}
	if len(req.sites) > 0 {
		page.Meta.Sites = req.sites
	}
	if req.stage != "" {
		page.Meta.Stage = req.stage
	}
	if strings.TrimSpace(req.program) != "" {
		page.Meta.Program = req.program
	}
	if len(req.kinds) > 0 {
		page.Meta.Kinds = req.kinds
	}
	if req.importance > 0 {
		page.Meta.Importance = req.importance
	}
	if req.pageType != "" {
		page.Meta.Type = req.pageType
	}
	if req.confidence != "" {
		page.Meta.Confidence = req.confidence
	}
	if req.due != "" {
		page.Meta.Due = req.due
	}
	page.Meta.Updated = time.Now().Format("2006-01-02")
	if req.content != "" {
		page.Body = wikiWriteBody(page.Body, req.content, logAppend)
	}
}

func newWikiWritePage(req wikiWriteRequest, logAppend bool) *wiki.Page {
	page := wiki.NewPage(req.title, req.category, req.tags)
	page.Meta.ID = req.id
	page.Meta.Summary = req.summary
	page.Meta.Related = req.related
	page.Meta.Cues = req.cues
	page.Meta.Client = req.client
	page.Meta.Sites = req.sites
	page.Meta.Stage = req.stage
	page.Meta.Program = req.program
	page.Meta.Kinds = req.kinds
	if req.importance > 0 {
		page.Meta.Importance = req.importance
	}
	page.Meta.Type = req.pageType
	page.Meta.Confidence = req.confidence
	if req.due != "" {
		page.Meta.Due = req.due
	}
	page.Body = newWikiWriteBody(req.title, req.content, logAppend)
	return page
}

func newWikiWriteBody(title, content string, logAppend bool) string {
	if content != "" {
		return wikiWriteBody("", content, logAppend)
	}
	return fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성\n",
		title, time.Now().Format("2006-01-02"))
}

func wikiWriteBody(body, content string, logAppend bool) string {
	if logAppend {
		return appendProjectLogSection(body, content)
	}
	return content
}

func formatWikiWriteResult(path string, req wikiWriteRequest, note string, existed, logAppend bool, marked, failed []string) string {
	action := "생성"
	if existed {
		action = "업데이트"
		if logAppend && req.content != "" {
			action = "업데이트 (로그에 섹션 append — 기존 항목 유지)"
		}
	}
	if len(marked) > 0 {
		note += fmt.Sprintf(" · 대체 표시 %d건", len(marked))
	}
	if len(failed) > 0 {
		note += fmt.Sprintf(" · 대체 표시 실패: %s", strings.Join(failed, ", "))
	}
	result := fmt.Sprintf("위키 페이지 %s: %s (%s)%s", action, path, req.title, note)
	// Out-of-vocabulary stage/kinds are silently dropped at render (enum
	// discipline); surface the drop so the model can correct it this turn.
	if notes := wiki.DroppedEnumNotes(req.stage, req.kinds); len(notes) > 0 {
		result += "\n⚠️ " + strings.Join(notes, "\n⚠️ ")
	}
	return result
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
func autoRecordPeople(store *wiki.Store, contactsBook tooldeps.ContactsBook, page *wiki.Page, category string) string {
	if store == nil || contactsBook == nil || contactsBook.Count() == 0 || page == nil {
		return ""
	}
	book := toDomainContacts(contactsBook.All())
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

func toDomainContacts(in []tooldeps.Contact) []contacts.Contact {
	out := make([]contacts.Contact, len(in))
	for i, c := range in {
		out[i] = contacts.Contact{Name: c.Name, Phones: c.Phones, Emails: c.Emails, Org: c.Org}
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

// wikiDailyByDate reads one diary day by date — the target of a provenance
// citation. The date must be a well-formed YYYY-MM-DD (path-traversal guard)
// since it names a file under the diary dir.
//
// A busy day can exceed one tool output, so it pages by line (from_line/
// max_lines, same params as a page range read) and emits a continuation hint
// when more remains — otherwise a fact cited late in a long diary would be
// unreachable through the very command the provenance footer advertises.
func wikiDailyByDate(diaryDir, date string, fromLine, maxLines int) (string, error) {
	if !wiki.IsDiaryDate(date) {
		return fmt.Sprintf("잘못된 날짜 형식: %q (YYYY-MM-DD 이어야 함)", date), nil
	}
	path := filepath.Join(diaryDir, "diary-"+date+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Sprintf("%s 일지 없음 (해당 날짜 일지가 없거나 이미 정리됨)", date), nil
		}
		return fmt.Sprintf("일지 읽기 실패: %v", err), nil
	}

	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if fromLine <= 0 {
		fromLine = 1
	}
	if fromLine > len(lines) {
		return fmt.Sprintf("%s 일지는 %d줄입니다. from_line=%d는 범위를 벗어납니다.", date, len(lines), fromLine), nil
	}
	if maxLines <= 0 {
		maxLines = 300
	}
	maxLines = min(maxLines, wikiReadMaxLines)
	end := min(len(lines), fromLine-1+maxLines)

	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s 일지 (L%d-L%d / 총 %d줄)\n%s", date, fromLine, end, len(lines),
		strings.Join(lines[fromLine-1:end], "\n"))
	if end < len(lines) {
		fmt.Fprintf(&sb, "\n\n[계속: wiki action=daily date=%s from_line=%d]", date, end+1)
	}
	return sb.String(), nil
}

func wikiStatus(store *wiki.Store) string {
	stats := store.Stats()

	var sb strings.Builder
	sb.WriteString("## 위키 상태\n\n")
	fmt.Fprintf(&sb, "- 총 페이지: %d\n", stats.TotalPages)
	fmt.Fprintf(&sb, "- 총 크기: %s\n", textutil.FormatBytes(stats.TotalBytes))
	sb.WriteString("\n### 카테고리별\n\n")

	for cat, count := range stats.CategoryCount {
		fmt.Fprintf(&sb, "- %s: %d 페이지\n", cat, count)
	}

	sb.WriteString(memorySystemStatus(store))
	return sb.String()
}

func wikiStatusWithDoctor(ctx context.Context, store *wiki.Store) string {
	out := wikiStatus(store)
	doctor := store.SearchDoctor(ctx)
	var sb strings.Builder
	sb.WriteString(out)
	sb.WriteString("\n\n## 검색 Doctor\n\n")
	fmt.Fprintf(&sb, "- 전체 상태: %t\n", doctor.Healthy)
	fmt.Fprintf(&sb, "- 어휘 인덱스: %d 문서\n", doctor.LexicalDocuments)
	fmt.Fprintf(&sb, "- 벡터 캐시: %d/%d (pending %d, stale %d)\n", doctor.Semantic.Indexed, doctor.Semantic.Expected, doctor.Semantic.Pending, doctor.Semantic.Stale)
	if doctor.SemanticProbe.Attempted {
		fmt.Fprintf(&sb, "- 임베딩 실측: healthy=%t, %dms, %d차원\n", doctor.SemanticProbe.Healthy, doctor.SemanticProbe.LatencyMS, doctor.SemanticProbe.Dimensions)
	}
	fmt.Fprintf(&sb, "- reranker: enabled=%t, healthy=%t", doctor.Reranker.Enabled, doctor.Reranker.Healthy)
	if doctor.Reranker.Identity != "" {
		fmt.Fprintf(&sb, " (%s)", doctor.Reranker.Identity)
	}
	if len(doctor.Recommendations) > 0 {
		sb.WriteString("\n- 권장 조치: " + strings.Join(doctor.Recommendations, ", "))
	}
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
		fmt.Fprintf(&sb, "- 다이어리: %d파일 %s\n", n, textutil.FormatBytes(size))
	}
	if n, size := dirFootprint(filepath.Join(memRoot, "captures"), ".md"); n > 0 {
		fmt.Fprintf(&sb, "- 캡처 원문: %d건 %s\n", n, textutil.FormatBytes(size))
	}

	stateDir := memoryStateDir()
	if stateDir == "" {
		return sb.String()
	}

	// MEMORY.md budget pressure (32K loads into the prompt; the rest waits
	// for dream curation).
	if info, err := os.Stat(filepath.Join(stateDir, "workspace", "MEMORY.md")); err == nil {
		line := fmt.Sprintf("- MEMORY.md: %s", textutil.FormatBytes(info.Size()))
		if over := info.Size() - 32_000; over > 0 {
			line += fmt.Sprintf(" (프롬프트 예산 32KB 초과분 %s — 드림 큐레이션 대기)", textutil.FormatBytes(over))
		}
		sb.WriteString(line + "\n")
	}

	if n, size := dirFootprint(filepath.Join(stateDir, "polaris", "messages"), ".jsonl"); n > 0 {
		fmt.Fprintf(&sb, "- Polaris 세션 메모리: %d세션 %s\n", n, textutil.FormatBytes(size))
	}
	if n, size := dirFootprint(filepath.Join(stateDir, "transcripts"), ".jsonl"); n > 0 {
		fmt.Fprintf(&sb, "- 트랜스크립트: %d세션 %s\n", n, textutil.FormatBytes(size))
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
