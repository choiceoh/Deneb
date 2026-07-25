package wiki

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// wikiLinkRe matches a single Obsidian-style [[wiki-link]] target. The inner
// group excludes brackets so nested/adjacent links each match on their own.
var wikiLinkRe = regexp.MustCompile(`\[\[([^\[\]]+)\]\]`)

// ExtractWikiLinks returns the page targets referenced via Obsidian-style
// [[target]] links in a page body. It understands the [[target|alias]] and
// [[target#section]] forms, returning just the target (the part before any
// '|' or '#'). Targets are trimmed and de-duplicated in first-seen order;
// callers resolve them to pages by path, id, or title.
//
// This closes a loop the wiki already half-implemented: the dreamer emits these
// links into a page's "관련 문서" section (dreamer.go) and the graph resolvers
// already strip "[[ ]]" when matching, but nothing parsed inline links out of a
// body — the graph only read the parallel `related:` frontmatter. Inline links
// are author-intended and high-precision, unlike the fuzzy body-mention pass.
func ExtractWikiLinks(body string) []string {
	if body == "" || !strings.Contains(body, "[[") {
		return nil
	}
	matches := wikiLinkRe.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(matches))
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		target := m[1]
		// [[target|alias]] -> target; [[target#section]] -> target.
		if i := strings.IndexAny(target, "|#"); i >= 0 {
			target = target[:i]
		}
		target = strings.TrimSpace(target)
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		out = append(out, target)
	}
	return out
}

// Page represents a single wiki page with YAML frontmatter and markdown body.
type Page struct {
	Meta Frontmatter
	Body string // markdown content after frontmatter
}

// Frontmatter is the YAML metadata at the top of a wiki page.
type Frontmatter struct {
	ID string // short identifier (e.g., "dgx-spark", "gemma4-switch")
	// Code is the frozen composite project identity:
	// [부서]-[고객]-[거래타입]-[순번], all 3-char (e.g. "pl3-tri-mod-001").
	// Unlike the path/folder/title (mutable views), the code never changes once
	// minted, so cross-references that point at the code survive renames and
	// reclassification. Resolved by graph_query's byCode index. Empty for
	// non-project pages (인물/시스템/업무/…).
	Code string
	// PID is the frozen person identity code for 인물 pages: p-[그룹]-[순번]
	// where 그룹 is a lane/거래처 slug (p-pl2-001, p-tri-003, p-nde-002). It is the
	// person analogue of Code — a stable key that survives rename/이직 and lets
	// people be grouped by lane (p-pl2-* = 2팀) — kept in its OWN field so it never
	// leaks into project-code machinery (Code stays project-only). Empty on
	// non-person pages and people not yet coded.
	PID      string
	Title    string
	Summary  string // one-line description for index-level filtering (~80 chars)
	Category string
	Tags     []string
	Related  []string
	// Emails are the person's canonical identity keys (인물 pages only). An email
	// is unique to one identity, so it is what disambiguates 동명이인 (same name,
	// different address) that name matching alone conflates: mail senders, org
	// members, and contacts all resolve to the ONE 인물 page that declares their
	// address. Populated from the address book (which stores homonyms as distinct
	// entries); empty on non-person pages and on people not yet in the address book.
	Emails []string
	// Cues are recall entry points (Memora-style cue anchors): alternate Korean
	// phrasings a future query might use for this page — synonyms, aliases,
	// question forms — deliberately words NOT already in the title/summary/body.
	// They are indexed for lexical search and folded into the semantic embedding,
	// but never rendered as content; they exist purely so a paraphrased question
	// ("계약금 언제 받지?") can find a page written with different vocabulary
	// ("선수금 입금 일정").
	Cues []string
	// Resource is a stable URI/identifier for the concept's underlying asset
	// (e.g. a gmail thread, deal ref, calendar event, file path) — Google OKF's
	// `resource` field. It lets the agent jump from a wiki concept straight to
	// its live source instead of re-deriving it; empty for abstract concepts
	// with no backing asset.
	Resource string
	// Client is the project's 거래처 — the counterparty company the deal is
	// with, as a single-level canonical Korean name at the 계열사 grain
	// (기아·현대차·LG전자·금호타이어 — operator-confirmed 2026-07-07: no group
	// hierarchy, no legal suffixes like ㈜). It is the TOP grouping level above
	// projects: the 모아보기 digest groups by it and the recall project anchor
	// matches it, so "금호타이어 근황?" surfaces every 금호타이어 project. Own-
	// development projects with no counterparty leave it empty. Project
	// 대표페이지 only; empty elsewhere.
	Client string
	// Sites are the project's 현장 locations as canonical administrative paths —
	// "광역약칭 시/군 읍/면/동 [리]" (e.g. "전북 군산시 옥구읍 수산리"), space-
	// separated, no lot numbers, province abbreviated (전라북도→전북; see
	// normalizeSiteName). A solar project's real identity IS its site: calendar
	// events and mail refer to places ("수산리 현장") more often than to project
	// titles, so sites are matching keys for the recall project anchor and the
	// meeting harvest. Multiple entries for multi-site projects (물류센터 3개소).
	// Project 대표페이지 only; empty elsewhere.
	Sites []string
	// Kinds classify what the project IS as business (복수 허용): fixed
	// two-level vocabulary "1차" or "1차/2차" (operator-confirmed 2026-07-06) —
	// 태양광(토지/루프탑/수상/ESS — 구 시공·개발 1차 통합), 기자재(모듈/인버터/
	// 케이블/기타), 풍력(육상/해상), 기타(용역/협력). Values outside the
	// vocabulary are dropped at parse/render (normalizeKinds), legacy flat
	// values auto-upgrade (모듈→기자재/모듈, 시공·개발→태양광), and a bare
	// parent folds away when its child is present. 대표페이지 전용.
	Kinds []string
	// Program groups sibling deal folders that are workstreams of ONE larger
	// venture (예: 비금 130.9MW = 케이블 조달 + 커넥터 + EPC가 별개 폴더) —
	// a cross-cutting axis like Client, one level below it: Client groups by
	// counterparty, Program by venture. Free-form short Korean slug (예:
	// "비금-130mw"); identical values ARE the grouping key, so reuse the
	// existing spelling when joining an existing program (검색으로 확인).
	// Project 대표페이지 전용; empty = standalone deal (대부분의 프로젝트).
	Program string
	// Stage is the project's business lifecycle stage on the 대표페이지 —
	// fixed vocabulary 제안 → 견적 → 입찰 → 개발 → 계약협의 → 시공/납품 → 운영,
	// with 종결/유실 as terminal states (operator-confirmed 2026-07-19/20).
	// 개발 covers own-development permitting (자체개발 인허가·부지); 시공 and
	// 납품 are parallel execution tracks — 시공 for site projects, 납품 for
	// 기자재 (procurement) deals whose "execution" is delivery, not
	// construction (operator: "기자재 전용을 나눠"). Distinct from the
	// 현장 page's Status (후보/계약/개설/준공, per-site map lifecycle): Stage is
	// the DEAL's stage, one per project. Gates how much documentation a
	// project earns — site-detail sections start at 계약협의 (the site-docs
	// stage gate); before that only the sites: matching keys are kept.
	// Values outside the vocabulary are dropped at parse/render
	// (normalizeStage). Project 대표페이지 전용; empty = 미분류.
	Stage string
	// Capacity is the project's (or a 현장 page's) 용량 in MW (megawatts) — the
	// deal's scale. The 현장 지도 sizes each pin by it. 0 = unrecorded (drawn at a
	// base size). On a 대표페이지 it is the project total; on a 현장 page it is that
	// single site's capacity.
	Capacity float64
	// Address is a 현장 page's canonical site location ("광역약칭 시/군 읍/면", same
	// convention as Sites — see normalizeSiteName). 현장 페이지 전용 (the 대표페이지
	// keeps the flat Sites[] matching keys); empty elsewhere.
	Address string
	// Status is a 현장 page's lifecycle stage — 후보 / 계약 / 개설 / 준공 (prospective
	// → contracted → opened → completed). The 현장 지도 filters by it (계약·개설·준공
	// shown by default, 후보 hidden). Free-form; empty = 미분류. 현장 페이지 전용.
	Status string
	// 현장 공정 일정 (site milestone dates) — the standard 현장 lifecycle, aligned with
	// Status: 계약→ContractDate, 개설→ConstructionStart·ModuleDelivery, 준공→
	// PreUseInspection·CompletionInspection. YYYY-MM-DD, except ModuleDelivery which
	// may be a range ("2026-08-01~2026-08-15"). 현장 페이지 전용; empty until reached.
	ContractDate         string // 계약일
	ConstructionStart    string // 공사개시일
	ModuleDelivery       string // 모듈입고(기간 가능)
	PreUseInspection     string // 사용전검사일
	CompletionInspection string // 준공검사일
	Created              string // YYYY-MM-DD
	Updated              string // YYYY-MM-DD
	Due                  string // YYYY-MM-DD — upcoming deadline (payment due, delivery, milestone); empty if none
	// DueDone stamps the Due value the operator marked handled (long-press on a
	// morning-card deadline row). The deadline scan skips the page while
	// DueDone == Due, so a handled deadline stops nagging; a NEW Due (later
	// milestone) no longer matches and resurfaces. Empty = never marked done.
	DueDone    string  // YYYY-MM-DD
	Importance float64 // 0.0-1.0
	Archived   bool
	Type       string // concept, entity, source, comparison, log
	Confidence string // high, medium, low
	// SubjectID scopes personal facts to an identity (HealthClaw M6).
	// "self" / empty = operator; person pages may use PID or a stable slug.
	// Recall drops cross-subject hits when the query does not name them.
	SubjectID string
	// SupersededBy points at the page that replaced this one's facts. Set by
	// the dreamer when new information contradicts/replaces an old page;
	// search demotes superseded pages so stale facts stop surfacing as
	// current (see validityFactor).
	SupersededBy string // relPath of the superseding page; "" = current
	// Sources is the page's episode provenance: the dream-cycle refs
	// (d<diaryDate>#<hash>) whose raw diary spans created or last touched this
	// page's facts. It closes the "citation needed" gap — synthesis used to
	// drop the link back to the source span — so a fact can be traced to (and
	// verified against) the raw text it came from. Bounded to the most recent
	// maxSources episodes (see normalizeSources); newest last. graph_snapshot
	// projects these into the knowledge graph as real provenance.
	Sources []string
}

// parsePage parses a wiki page from raw bytes.
func parsePage(data []byte) (*Page, error) {
	meta, body, err := splitFrontmatter(data)
	if err != nil {
		// No frontmatter — treat entire content as body.
		return &Page{Body: string(data)}, nil //nolint:nilerr // missing frontmatter is valid, not an error
	}

	fm := parseFrontmatterFields(string(meta))
	return &Page{Meta: fm, Body: body}, nil
}

// ParsePageFile reads and parses a wiki page from disk.
func ParsePageFile(path string) (*Page, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePage(data)
}

// Render produces the full page content: frontmatter + body.
//
// Every frontmatter value passes through sanitizeScalar/sanitizeFlowItems:
// the values are LLM-/caller-supplied free text, and a raw newline inside one
// (e.g. a title of "A\nB") would terminate its line early, shred the rest of
// the frontmatter into the body, and drop all metadata on the next parse.
func (p *Page) Render() []byte {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	if p.Meta.ID != "" {
		buf.WriteString("id: " + sanitizeScalar(p.Meta.ID) + "\n")
	}
	if p.Meta.Code != "" {
		buf.WriteString("code: " + sanitizeScalar(p.Meta.Code) + "\n")
	}
	if p.Meta.PID != "" {
		buf.WriteString("pid: " + sanitizeScalar(p.Meta.PID) + "\n")
	}
	buf.WriteString("title: " + sanitizeScalar(p.Meta.Title) + "\n")
	if p.Meta.Summary != "" {
		buf.WriteString("summary: " + sanitizeScalar(p.Meta.Summary) + "\n")
	}
	if p.Meta.Category != "" {
		buf.WriteString("category: " + sanitizeScalar(p.Meta.Category) + "\n")
	}
	if len(p.Meta.Tags) > 0 {
		buf.WriteString("tags: [" + strings.Join(sanitizeFlowItems(p.Meta.Tags), ", ") + "]\n")
	}
	if len(p.Meta.Related) > 0 {
		buf.WriteString("related: [" + strings.Join(sanitizeFlowItems(p.Meta.Related), ", ") + "]\n")
	}
	if len(p.Meta.Emails) > 0 {
		buf.WriteString("emails: [" + strings.Join(sanitizeFlowItems(p.Meta.Emails), ", ") + "]\n")
	}
	if len(p.Meta.Cues) > 0 {
		buf.WriteString("cues: [" + strings.Join(sanitizeFlowItems(p.Meta.Cues), ", ") + "]\n")
	}
	if p.Meta.Resource != "" {
		buf.WriteString("resource: " + sanitizeScalar(p.Meta.Resource) + "\n")
	}
	if client := normalizeClientName(p.Meta.Client); client != "" {
		buf.WriteString("client: " + sanitizeScalar(client) + "\n")
	}
	if sites := normalizeSites(p.Meta.Sites); len(sites) > 0 {
		buf.WriteString("sites: [" + strings.Join(sanitizeFlowItems(sites), ", ") + "]\n")
	}
	if kinds := normalizeKinds(p.Meta.Kinds); len(kinds) > 0 {
		buf.WriteString("kinds: [" + strings.Join(sanitizeFlowItems(kinds), ", ") + "]\n")
	}
	if stage := normalizeStage(p.Meta.Stage); stage != "" {
		buf.WriteString("stage: " + stage + "\n")
	}
	if program := strings.TrimSpace(p.Meta.Program); program != "" {
		buf.WriteString("program: " + sanitizeScalar(program) + "\n")
	}
	if p.Meta.Capacity > 0 {
		buf.WriteString("capacity: " + strconv.FormatFloat(p.Meta.Capacity, 'g', -1, 64) + "\n")
	}
	if addr := normalizeSiteName(p.Meta.Address); addr != "" {
		buf.WriteString("address: " + sanitizeScalar(addr) + "\n")
	}
	if p.Meta.Status != "" {
		buf.WriteString("status: " + sanitizeScalar(p.Meta.Status) + "\n")
	}
	for _, m := range []struct{ key, val string }{
		{"contract_date", p.Meta.ContractDate},
		{"construction_start", p.Meta.ConstructionStart},
		{"module_delivery", p.Meta.ModuleDelivery},
		{"pre_use_inspection", p.Meta.PreUseInspection},
		{"completion_inspection", p.Meta.CompletionInspection},
	} {
		if strings.TrimSpace(m.val) != "" {
			buf.WriteString(m.key + ": " + sanitizeScalar(strings.TrimSpace(m.val)) + "\n")
		}
	}
	if p.Meta.Created != "" {
		buf.WriteString("created: " + sanitizeScalar(p.Meta.Created) + "\n")
	}
	if p.Meta.Updated != "" {
		buf.WriteString("updated: " + sanitizeScalar(p.Meta.Updated) + "\n")
	}
	if p.Meta.Due != "" {
		buf.WriteString("due: " + sanitizeScalar(p.Meta.Due) + "\n")
	}
	if p.Meta.DueDone != "" {
		buf.WriteString("due_done: " + sanitizeScalar(p.Meta.DueDone) + "\n")
	}
	if p.Meta.Importance > 0 {
		fmt.Fprintf(&buf, "importance: %.2f\n", p.Meta.Importance)
	}
	if p.Meta.Archived {
		buf.WriteString("archived: true\n")
	}
	// OKF v0.1 requires every concept document to carry a non-empty type, so
	// untyped pages default by category at render (인물 = entity, else
	// concept) instead of omitting the field.
	pageType := strings.TrimSpace(p.Meta.Type)
	if pageType == "" {
		if p.Meta.Category == "인물" {
			pageType = "entity"
		} else {
			pageType = "concept"
		}
	}
	buf.WriteString("type: " + sanitizeScalar(pageType) + "\n")
	if p.Meta.Confidence != "" {
		buf.WriteString("confidence: " + sanitizeScalar(p.Meta.Confidence) + "\n")
	}
	if p.Meta.SubjectID != "" {
		buf.WriteString("subject_id: " + sanitizeScalar(p.Meta.SubjectID) + "\n")
	}
	if p.Meta.SupersededBy != "" {
		buf.WriteString("superseded_by: " + sanitizeScalar(p.Meta.SupersededBy) + "\n")
	}
	if len(p.Meta.Sources) > 0 {
		buf.WriteString("sources: [" + strings.Join(sanitizeFlowItems(p.Meta.Sources), ", ") + "]\n")
	}
	buf.WriteString("---\n\n")

	buf.WriteString(p.Body)
	return buf.Bytes()
}

// sanitizeScalar makes a value safe to emit as a single-line "key: value"
// frontmatter scalar: newlines collapse to spaces (mirroring index.go's
// sanitizeTSV — a raw "\n" would prematurely end the line and shred every
// following field into the body).
func sanitizeScalar(s string) string {
	if !strings.ContainsAny(s, "\r\n") {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// sanitizeFlowItems makes list items safe for the "[a, b, c]" flow-array form:
// newlines collapse like any scalar, and a comma INSIDE an item becomes "·" —
// parseFlowArray splits on commas, so a cue like "계약금, 선수금 일정" would
// otherwise reparse as two items. Deliberately a lossy-but-stable substitution
// rather than an escape scheme: it is idempotent across render/parse cycles
// and immune to escape-state bugs, at the cost of the literal comma glyph.
func sanitizeFlowItems(items []string) []string {
	out := make([]string, len(items))
	for i, it := range items {
		it = sanitizeScalar(it)
		if strings.Contains(it, ",") {
			it = strings.TrimSpace(strings.ReplaceAll(it, ",", "·"))
		}
		out[i] = it
	}
	return out
}

// writePageFile writes a page to disk atomically (via temp file + rename).
//
// Free-text fields on the page (body, title, summary) pass through pkg/redact
// before serialization so any secret that slipped into LLM-synthesized wiki
// content never reaches disk. Structural metadata (category, tags, dates,
// importance) is left alone — categories are from a fixed allow-list and tags
// are keyword-sized.
func writePageFile(path string, page *Page) error {
	// Single choke point for cue hygiene: every producer (dreamer create/update,
	// the agent wiki tool, duplicate folds) funnels through here, so trim/dedupe
	// and the 10-cue cap hold regardless of which path set the field — without
	// this, the cap only held on the dreamer's merge branch. Redaction runs
	// FIRST: two distinct cues can redact to the same placeholder, and
	// normalizing the pre-redaction list would let those duplicates back in.
	redactPage(page)
	page.Meta.Cues = normalizeCues(page.Meta.Cues)
	// Episode provenance is bounded here for the same reason cues are: every
	// producer (dreamer create/merge) funnels through this choke point, so the
	// most-recent-N window holds regardless of which path appended a ref.
	page.Meta.Sources = normalizeSources(page.Meta.Sources)
	data := page.Render()
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, data, 0o644); err != nil { //nolint:gosec // G306 — world-readable is intentional
		return fmt.Errorf("wiki: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("wiki: rename: %w", err)
	}
	return nil
}

// writeFileSync writes data and fsyncs before close. Wiki pages and the master
// index are the agent's long-term memory; tmp+rename alone leaves write/rename
// ordering to the kernel, so a power loss right after the rename could surface
// a truncated file. The fsync closes that window at the cost of ~1ms per write.
func writeFileSync(path string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// redactPage masks secret patterns in a Page's free-text fields before write.
// No-op when redaction is disabled. Title, Summary, and Cues are free-text
// strings that the Dreamer/agent populate from LLM output; Body is the main
// leak surface. Other frontmatter fields (Category, Tags, dates, Importance,
// Archived, Type, Confidence, Resource) are structural and unaffected —
// Resource is an asset identifier/URI, not free-text prose, so redacting it
// would corrupt the ref.
func redactPage(p *Page) {
	if p == nil || !redact.Enabled() {
		return
	}
	p.Body = redact.String(p.Body)
	p.Meta.Title = redact.String(p.Meta.Title)
	p.Meta.Summary = redact.String(p.Meta.Summary)
	// Cues are LLM-supplied free text that gets indexed and embedded — a
	// credential-looking value slipping in here would persist unmasked.
	for i := range p.Meta.Cues {
		p.Meta.Cues[i] = redact.String(p.Meta.Cues[i])
	}
}

// normalizeCues trims, drops empties, dedupes (first occurrence wins), and caps
// the cue-anchor list. Enforced at write time (writePageFile) so every producer
// lands within the same bounds — one over-eager LLM emission must not turn a
// page into a match-everything BM25 magnet.
func normalizeCues(cues []string) []string {
	const maxCues = 10
	seen := make(map[string]struct{}, len(cues))
	out := make([]string, 0, len(cues))
	for _, c := range cues {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		if _, dup := seen[c]; dup {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
		if len(out) == maxCues {
			break
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NewPage creates a page with sensible defaults.
func NewPage(title, category string, tags []string) *Page {
	today := time.Now().Format("2006-01-02")
	return &Page{
		Meta: Frontmatter{
			Title:    title,
			Category: category,
			Tags:     tags,
			Created:  today,
			Updated:  today,
		},
	}
}

// Section extracts the content of a named markdown section (## heading).
// Returns empty string if the section is not found.
func (p *Page) Section(name string) string {
	nameLower := strings.ToLower(strings.TrimSpace(name))
	scanner := bufio.NewScanner(strings.NewReader(p.Body))

	var capturing bool
	var result strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "## ") {
			heading := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(line, "## ")))
			if heading == nameLower {
				capturing = true
				continue
			}
			if capturing {
				break // reached next section
			}
			continue
		}

		if capturing {
			result.WriteString(line)
			result.WriteByte('\n')
		}
	}

	return strings.TrimSpace(result.String())
}

// Sections returns all section headings (## level) in the body.
func (p *Page) Sections() []string {
	var headings []string
	scanner := bufio.NewScanner(strings.NewReader(p.Body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			headings = append(headings, heading)
		}
	}
	return headings
}

// H2Section is an ordered section extracted from the page body.
type H2Section struct {
	Heading string // section heading (without "## " prefix)
	Content string // full content including sub-headings
}

// SplitByH2 splits the page body into a preamble (content before first H2)
// and an ordered list of H2 sections. Each section includes everything up to
// the next H2 heading.
//
// Lines inside fenced code blocks (```) are never treated as headings: log
// entries and captured tool output legitimately contain "## " lines, and
// splitting on them would shred a fenced entry across sections (log rotation
// re-assembles pages from these sections).
func (p *Page) SplitByH2() (preamble string, sections []H2Section) {
	scanner := bufio.NewScanner(strings.NewReader(p.Body))
	var current *H2Section
	var preambleBuf, sectionBuf strings.Builder
	inFence := false

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
		}

		if !inFence && strings.HasPrefix(line, "## ") {
			// Flush previous section.
			if current != nil {
				current.Content = strings.TrimSpace(sectionBuf.String())
				sections = append(sections, *current)
				sectionBuf.Reset()
			} else {
				preamble = strings.TrimSpace(preambleBuf.String())
			}
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			current = &H2Section{Heading: heading}
			continue
		}

		if current != nil {
			sectionBuf.WriteString(line)
			sectionBuf.WriteByte('\n')
		} else {
			preambleBuf.WriteString(line)
			preambleBuf.WriteByte('\n')
		}
	}

	// Flush last section or preamble.
	if current != nil {
		current.Content = strings.TrimSpace(sectionBuf.String())
		sections = append(sections, *current)
	} else {
		preamble = strings.TrimSpace(preambleBuf.String())
	}

	return preamble, sections
}

// splitFrontmatter separates YAML frontmatter from the body.
// Frontmatter is delimited by "---" on its own line.
func splitFrontmatter(data []byte) (meta []byte, body string, err error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return nil, "", fmt.Errorf("no frontmatter")
	}

	// Find closing "---".
	rest := s[4:] // skip opening "---\n"
	idx := strings.Index(rest, "\n---\n")
	if idx < 0 {
		// Try with \r\n.
		idx = strings.Index(rest, "\r\n---\r\n")
		if idx < 0 {
			return nil, "", fmt.Errorf("unclosed frontmatter")
		}
		return []byte(rest[:idx]), strings.TrimLeft(rest[idx+6:], "\r\n"), nil
	}

	return []byte(rest[:idx]), strings.TrimLeft(rest[idx+5:], "\r\n"), nil
}

// stripLeadingFrontmatter removes any YAML frontmatter block(s) at the very
// start of s and returns the remaining body.
//
// LLM-synthesized page content (WikiDreamer) and agent-supplied bodies
// sometimes begin with their own "---\nkey: value\n---" block — the model
// mimics the page format it saw in the index. If that text is stored as a
// Page.Body it round-trips into a *second* on-disk frontmatter, since Render
// always prepends one more from Page.Meta. Repeated dream/merge passes then
// stack the blocks, and parsePage (which only strips the first) mis-reads the
// rest as body. Stripping at the content boundary keeps every page to exactly
// one frontmatter.
//
// Only leading, frontmatter-shaped blocks are removed (one or more, stacked):
// a "---" horizontal rule mid-body, or one whose first line is not a "key:"
// pair, is left untouched. Metadata in a stripped block is intentionally
// dropped — callers populate Page.Meta from their own structured fields, so the
// embedded copy is redundant duplication.
func stripLeadingFrontmatter(s string) string {
	for {
		trimmed := strings.TrimLeft(s, "\r\n")
		meta, body, err := splitFrontmatter([]byte(trimmed))
		if err != nil || !looksLikeFrontmatter(string(meta)) {
			return s
		}
		s = body
	}
}

// looksLikeFrontmatter reports whether the block's first non-empty line is a
// "key:" pair, which real frontmatter always opens with. This guards against
// stripping a horizontal-rule-delimited prose section that happens to be fenced
// by "---" lines.
func looksLikeFrontmatter(meta string) bool {
	for _, line := range strings.Split(meta, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		colon := strings.IndexByte(line, ':')
		if colon <= 0 {
			return false
		}
		for i, r := range line[:colon] {
			isAlpha := r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
			isDigitOrDash := (r >= '0' && r <= '9') || r == '-'
			if i == 0 && !isAlpha {
				return false
			}
			if !isAlpha && !isDigitOrDash {
				return false
			}
		}
		return true
	}
	return false
}

// parseFrontmatterFields parses simple YAML key-value pairs.
// Supports: scalar strings, YAML flow arrays [a, b, c], BLOCK lists
// (key:\n  - a\n  - b), booleans, floats.
//
// Block lists matter because this package WRITES flow arrays but humans and
// external scripts write the block form that every YAML tool emits. Reading only
// flow arrays made such an edit parse as an EMPTY list — valid YAML on disk,
// silently zero values in the index (found 2026-07-25: a cues backfill written in
// block form scored byte-identical on recall-bench because every cue was dropped).
func parseFrontmatterFields(raw string) Frontmatter {
	var fm Frontmatter
	lines := strings.Split(raw, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := parseKV(line)
		if !ok {
			continue
		}
		// A key with no inline value may be followed by an indented block list.
		// Collect it as a real []string — folding it back into "[a, b]" text and
		// re-splitting on commas would have to DROP any item containing a comma
		// to avoid splitting one value into two, which is the same silent-loss
		// class this parser exists to prevent.
		var blockItems []string
		if strings.TrimSpace(val) == "" {
			if items, next := collectBlockList(lines, i+1); len(items) > 0 {
				blockItems = items
				i = next - 1
			}
		}
		// listVal resolves a list-valued key. Only one form can be present: an
		// inline flow array on the key line, or an indented block beneath it.
		listVal := func() []string {
			if blockItems != nil {
				return blockItems
			}
			return parseFlowArray(val)
		}

		switch key {
		case "id":
			fm.ID = val
		case "code":
			fm.Code = normalizeProjectCode(val)
		case "pid":
			fm.PID = strings.ToLower(strings.TrimSpace(val))
		case "title":
			fm.Title = val
		case "summary":
			fm.Summary = val
		case "category":
			fm.Category = normalizeCategory(val)
		case "tags":
			fm.Tags = listVal()
		case "related":
			fm.Related = listVal()
		case "emails":
			fm.Emails = listVal()
		case "cues":
			fm.Cues = listVal()
		case "resource":
			fm.Resource = val
		case "client":
			fm.Client = normalizeClientName(val)
		case "sites":
			fm.Sites = normalizeSites(listVal())
		case "kinds":
			fm.Kinds = normalizeKinds(listVal())
		case "stage":
			fm.Stage = normalizeStage(val)
		case "program":
			fm.Program = strings.TrimSpace(val)
		case "capacity":
			fm.Capacity, _ = strconv.ParseFloat(strings.TrimSpace(val), 64) // best-effort: defaults to zero
		case "address":
			fm.Address = normalizeSiteName(val)
		case "status":
			fm.Status = strings.TrimSpace(val)
		case "contract_date":
			fm.ContractDate = strings.TrimSpace(val)
		case "construction_start":
			fm.ConstructionStart = strings.TrimSpace(val)
		case "module_delivery":
			fm.ModuleDelivery = strings.TrimSpace(val)
		case "pre_use_inspection":
			fm.PreUseInspection = strings.TrimSpace(val)
		case "completion_inspection":
			fm.CompletionInspection = strings.TrimSpace(val)
		case "created":
			fm.Created = val
		case "updated":
			fm.Updated = val
		case "due":
			fm.Due = val
		case "due_done":
			fm.DueDone = val
		case "importance":
			fm.Importance, _ = strconv.ParseFloat(val, 64) // best-effort: defaults to zero
		case "archived":
			fm.Archived = val == "true"
		case "type":
			fm.Type = val
		case "confidence":
			fm.Confidence = val
		case "subject_id":
			fm.SubjectID = strings.TrimSpace(val)
		case "superseded_by":
			fm.SupersededBy = val
		case "sources":
			fm.Sources = normalizeSources(listVal())
		}
	}
	return fm
}

// provinceAbbrev collapses full 광역 names to the fixed abbreviations the site
// convention uses, so "전라북도 군산시…" and "전북 군산시…" are one value.
var provinceAbbrev = map[string]string{
	"전라북도": "전북", "전북특별자치도": "전북",
	"전라남도": "전남", "경상북도": "경북", "경상남도": "경남",
	"충청북도": "충북", "충청남도": "충남",
	"강원도": "강원", "강원특별자치도": "강원",
	"경기도": "경기", "제주도": "제주", "제주특별자치도": "제주",
	"서울특별시": "서울", "부산광역시": "부산", "대구광역시": "대구",
	"인천광역시": "인천", "광주광역시": "광주", "대전광역시": "대전",
	"울산광역시": "울산", "세종특별자치시": "세종",
}

// normalizeSiteName enforces the site writing convention on one entry: trim,
// collapse whitespace, drop a trailing period, abbreviate the leading province.
func normalizeSiteName(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSuffix(strings.TrimSpace(s), ".")), " ")
	if s == "" {
		return ""
	}
	first, rest, cut := strings.Cut(s, " ")
	if abbr, ok := provinceAbbrev[first]; ok {
		if cut {
			return abbr + " " + rest
		}
		return abbr
	}
	return s
}

// normalizeClientName cleans a 거래처 value: trims, strips a wikilink wrapper
// (LLM writers emit "[[금호타이어]]"), and collapses inner whitespace. It does
// NOT touch legal suffixes or spelling — the canonical single-level 계열사
// name is a writer-side rule (see the wiki tool schema / dreamer prompt), not
// something safely inferable here.
func normalizeClientName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return strings.Join(strings.Fields(s), " ")
}

// normalizeSites applies the convention to a list, dropping empties.
func normalizeSites(sites []string) []string {
	var out []string
	for _, s := range sites {
		if n := normalizeSiteName(s); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// projectKinds is the fixed two-level 특성 vocabulary (see Frontmatter.Kinds),
// written as "1차" or "1차/2차". Keys are lowercase lookups; the map folds
// synonyms/EN/code-segment spellings AND bare 2차 values onto their canonical
// hierarchical form ("모듈" → "기자재/모듈", "루프탑" → "태양광/루프탑") so any
// writer — and every pre-hierarchy stored value — converges on the enum.
var projectKinds = map[string]string{
	// 태양광 = 발전소 사업 (구 시공·개발 1차 통합 — 운영자 확정 2026-07-06)
	// + 설치·플랜트 유형 2차. ESS 사업도 기자재가 아니라 여기(운영자 확정).
	"태양광": "태양광", "solar": "태양광", "pv": "태양광",
	"태양광/토지": "태양광/토지", "토지": "태양광/토지", "지상": "태양광/토지", "시공/토지": "태양광/토지",
	"태양광/루프탑": "태양광/루프탑", "루프탑": "태양광/루프탑", "지붕": "태양광/루프탑", "옥상": "태양광/루프탑", "시공/루프탑": "태양광/루프탑",
	"태양광/수상": "태양광/수상", "수상": "태양광/수상", "시공/수상": "태양광/수상",
	"태양광/ess": "태양광/ESS", "ess": "태양광/ESS", "bess": "태양광/ESS", "bes": "태양광/ESS", "시공/ess": "태양광/ESS", "시공/bess": "태양광/ESS",
	// 기자재 + 품목 2차
	"기자재":    "기자재",
	"기자재/모듈": "기자재/모듈", "모듈": "기자재/모듈", "module": "기자재/모듈", "mod": "기자재/모듈",
	"기자재/인버터": "기자재/인버터", "인버터": "기자재/인버터", "inverter": "기자재/인버터", "inv": "기자재/인버터",
	"기자재/케이블": "기자재/케이블", "케이블": "기자재/케이블", "cable": "기자재/케이블", "cbl": "기자재/케이블",
	"기자재/기타": "기자재/기타",
	// 풍력 + 육상/해상 2차
	"풍력": "풍력", "wind": "풍력", "wnd": "풍력",
	"풍력/육상": "풍력/육상", "육상풍력": "풍력/육상",
	"풍력/해상": "풍력/해상", "해상풍력": "풍력/해상",
	// 기타 + 용역/협력 2차 (독립 1차에서 기타 밑으로 — 운영자 확정)
	"기타":    "기타",
	"기타/용역": "기타/용역", "용역": "기타/용역", "대행": "기타/용역",
	"기타/협력": "기타/협력", "협력": "기타/협력", "nda": "기타/협력", "제휴": "기타/협력",
}

// kindStageWords are legacy business-stage spellings (구 1차 시공·개발과 그
// 동의어) that no longer name a 발전원. They default-fold to 태양광 (the
// dominant business line), EXCEPT when the same list explicitly names 풍력 —
// a wind development project tagged [풍력, 개발] must stay 풍력, not gain a
// phantom 태양광.
var kindStageWords = map[string]bool{
	"시공": true, "epc": true, "턴키": true,
	"개발": true, "dev": true, "인허가": true,
}

// normalizeKinds folds synonyms, bare sub-kinds, and legacy flat values onto
// the canonical hierarchical vocabulary, drops values outside it (enum
// discipline keeps aggregation clean), dedupes, drops a bare parent when one
// of its children is present ("태양광" + "태양광/루프탑" → just "태양광/루프탑"
// — the child implies the parent, and prefix matching recovers parent-level
// queries), and drops stage-word-sourced 태양광 next to explicit 풍력 (see
// kindStageWords).
// projectStages is the fixed stage: vocabulary in pipeline order. Kept as a
// list (not just a set) so consumers can compare progression.
var projectStages = []string{"제안", "견적", "입찰", "개발", "계약협의", "시공", "납품", "운영", "종결", "유실"}

// normalizeStage canonicalizes a stage: value — trims, and drops anything
// outside the fixed vocabulary (same discipline as normalizeKinds: bad values
// disappear rather than accrete).
func normalizeStage(stage string) string {
	s := strings.TrimSpace(stage)
	for _, v := range projectStages {
		if s == v {
			return v
		}
	}
	return ""
}

func normalizeKinds(kinds []string) []string {
	var canon []string
	seen := map[string]bool{}
	stageOnly := map[string]bool{} // canon value produced only by stage words
	hasWind := false
	for _, k := range kinds {
		key := strings.ToLower(strings.TrimSpace(k))
		c, ok := projectKinds[key]
		fromStage := false
		if !ok {
			if !kindStageWords[key] {
				continue
			}
			c, fromStage = "태양광", true
		}
		if strings.HasPrefix(c, "풍력") {
			hasWind = true
		}
		if seen[c] {
			if !fromStage {
				stageOnly[c] = false
			}
			continue
		}
		seen[c] = true
		stageOnly[c] = fromStage
		canon = append(canon, c)
	}
	var out []string
	for _, c := range canon {
		if c == "태양광" && stageOnly[c] && hasWind {
			continue // stage word next to explicit 풍력 — not a solar signal
		}
		if !strings.Contains(c, "/") {
			hasChild := false
			for _, other := range canon {
				if strings.HasPrefix(other, c+"/") {
					hasChild = true
					break
				}
			}
			if hasChild {
				continue // parent subsumed by its child
			}
		}
		out = append(out, c)
	}
	return out
}

// normalizeCategory collapses a category value that leaked a wikilink form down
// to its plain name so one bucket doesn't split into phantom categories in the
// browser. The auto-categorizer sometimes wrote a category as a wiki ref —
// "w:프로젝트" (knowledge-router namespace) or "[[프로젝트]]" — which the count
// treated as distinct from "프로젝트". Path categories ("프로젝트/영산고") are
// intentional sub-buckets and kept as-is; a plain name is returned unchanged.
func normalizeCategory(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[[") && strings.HasSuffix(s, "]]") {
		s = strings.TrimSpace(s[2 : len(s)-2])
	}
	s = strings.TrimPrefix(s, "w:")
	return strings.TrimSpace(s)
}

// parseKV splits "key: value" into (key, value, true).
func parseKV(line string) (key, value string, ok bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(line[:idx])
	value = strings.TrimSpace(line[idx+1:])
	return key, value, true
}

// collectBlockList reads a YAML block-list body starting at lines[start]
// ("  - item" entries) and returns the items plus the index of the first line
// after the block. Items are returned verbatim — a comma inside an item is an
// ordinary character here, because the caller keeps the []string instead of
// re-encoding it as a comma-joined flow array.
func collectBlockList(lines []string, start int) ([]string, int) {
	var items []string
	i := start
	for ; i < len(lines); i++ {
		raw := lines[i]
		if strings.TrimSpace(raw) == "" {
			break
		}
		// Must be indented (a block entry) and start with a dash.
		if raw == strings.TrimLeft(raw, " \t") {
			break
		}
		item := strings.TrimSpace(raw)
		if !strings.HasPrefix(item, "-") {
			break
		}
		item = strings.TrimSpace(strings.TrimPrefix(item, "-"))
		item = strings.Trim(item, `"'`)
		if item != "" {
			items = append(items, item)
		}
	}
	return items, i
}

// parseFlowArray parses "[a, b, c]" into []string{"a", "b", "c"}.
func parseFlowArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	if s == "" {
		return nil
	}
	var result []string
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}
