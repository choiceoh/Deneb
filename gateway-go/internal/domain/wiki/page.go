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
	Code     string
	Title    string
	Summary  string // one-line description for index-level filtering (~80 chars)
	Category string
	Tags     []string
	Related  []string
	// Resource is a stable URI/identifier for the concept's underlying asset
	// (e.g. a gmail thread, deal ref, calendar event, file path) — Google OKF's
	// `resource` field. It lets the agent jump from a wiki concept straight to
	// its live source instead of re-deriving it; empty for abstract concepts
	// with no backing asset.
	Resource   string
	Created    string  // YYYY-MM-DD
	Updated    string  // YYYY-MM-DD
	Due        string  // YYYY-MM-DD — upcoming deadline (payment due, delivery, milestone); empty if none
	Importance float64 // 0.0-1.0
	Archived   bool
	Type       string // concept, entity, source, comparison, log
	Confidence string // high, medium, low
	// SupersededBy points at the page that replaced this one's facts. Set by
	// the dreamer when new information contradicts/replaces an old page;
	// search demotes superseded pages so stale facts stop surfacing as
	// current (see validityFactor).
	SupersededBy string // relPath of the superseding page; "" = current
}

// ParsePage parses a wiki page from raw bytes.
func ParsePage(data []byte) (*Page, error) {
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
	return ParsePage(data)
}

// Render produces the full page content: frontmatter + body.
func (p *Page) Render() []byte {
	var buf bytes.Buffer

	buf.WriteString("---\n")
	if p.Meta.ID != "" {
		buf.WriteString("id: " + p.Meta.ID + "\n")
	}
	if p.Meta.Code != "" {
		buf.WriteString("code: " + p.Meta.Code + "\n")
	}
	buf.WriteString("title: " + p.Meta.Title + "\n")
	if p.Meta.Summary != "" {
		buf.WriteString("summary: " + p.Meta.Summary + "\n")
	}
	if p.Meta.Category != "" {
		buf.WriteString("category: " + p.Meta.Category + "\n")
	}
	if len(p.Meta.Tags) > 0 {
		buf.WriteString("tags: [" + strings.Join(p.Meta.Tags, ", ") + "]\n")
	}
	if len(p.Meta.Related) > 0 {
		buf.WriteString("related: [" + strings.Join(p.Meta.Related, ", ") + "]\n")
	}
	if p.Meta.Resource != "" {
		buf.WriteString("resource: " + p.Meta.Resource + "\n")
	}
	if p.Meta.Created != "" {
		buf.WriteString("created: " + p.Meta.Created + "\n")
	}
	if p.Meta.Updated != "" {
		buf.WriteString("updated: " + p.Meta.Updated + "\n")
	}
	if p.Meta.Due != "" {
		buf.WriteString("due: " + p.Meta.Due + "\n")
	}
	if p.Meta.Importance > 0 {
		fmt.Fprintf(&buf, "importance: %.2f\n", p.Meta.Importance)
	}
	if p.Meta.Archived {
		buf.WriteString("archived: true\n")
	}
	if p.Meta.Type != "" {
		buf.WriteString("type: " + p.Meta.Type + "\n")
	}
	if p.Meta.Confidence != "" {
		buf.WriteString("confidence: " + p.Meta.Confidence + "\n")
	}
	if p.Meta.SupersededBy != "" {
		buf.WriteString("superseded_by: " + p.Meta.SupersededBy + "\n")
	}
	buf.WriteString("---\n\n")

	buf.WriteString(p.Body)
	return buf.Bytes()
}

// WritePageFile writes a page to disk atomically (via temp file + rename).
//
// Free-text fields on the page (body, title, summary) pass through pkg/redact
// before serialization so any secret that slipped into LLM-synthesized wiki
// content never reaches disk. Structural metadata (category, tags, dates,
// importance) is left alone — categories are from a fixed allow-list and tags
// are keyword-sized.
func WritePageFile(path string, page *Page) error {
	redactPage(page)
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
// No-op when redaction is disabled. Title and Summary are user-visible strings
// that the Dreamer populates from LLM output; Body is the main leak surface.
// Other frontmatter fields (Category, Tags, dates, Importance, Archived, Type,
// Confidence, Resource) are structural and unaffected — Resource is an asset
// identifier/URI, not free-text prose, so redacting it would corrupt the ref.
func redactPage(p *Page) {
	if p == nil || !redact.Enabled() {
		return
	}
	p.Body = redact.String(p.Body)
	p.Meta.Title = redact.String(p.Meta.Title)
	p.Meta.Summary = redact.String(p.Meta.Summary)
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
func (p *Page) SplitByH2() (preamble string, sections []H2Section) {
	scanner := bufio.NewScanner(strings.NewReader(p.Body))
	var current *H2Section
	var preambleBuf, sectionBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "## ") {
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

// StripLeadingFrontmatter removes any YAML frontmatter block(s) at the very
// start of s and returns the remaining body.
//
// LLM-synthesized page content (WikiDreamer) and agent-supplied bodies
// sometimes begin with their own "---\nkey: value\n---" block — the model
// mimics the page format it saw in the index. If that text is stored as a
// Page.Body it round-trips into a *second* on-disk frontmatter, since Render
// always prepends one more from Page.Meta. Repeated dream/merge passes then
// stack the blocks, and ParsePage (which only strips the first) mis-reads the
// rest as body. Stripping at the content boundary keeps every page to exactly
// one frontmatter.
//
// Only leading, frontmatter-shaped blocks are removed (one or more, stacked):
// a "---" horizontal rule mid-body, or one whose first line is not a "key:"
// pair, is left untouched. Metadata in a stripped block is intentionally
// dropped — callers populate Page.Meta from their own structured fields, so the
// embedded copy is redundant duplication.
func StripLeadingFrontmatter(s string) string {
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
// Supports: scalar strings, YAML flow arrays [a, b, c], booleans, floats.
func parseFrontmatterFields(raw string) Frontmatter {
	var fm Frontmatter
	scanner := bufio.NewScanner(strings.NewReader(raw))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := parseKV(line)
		if !ok {
			continue
		}

		switch key {
		case "id":
			fm.ID = val
		case "code":
			fm.Code = normalizeProjectCode(val)
		case "title":
			fm.Title = val
		case "summary":
			fm.Summary = val
		case "category":
			fm.Category = normalizeCategory(val)
		case "tags":
			fm.Tags = parseFlowArray(val)
		case "related":
			fm.Related = parseFlowArray(val)
		case "resource":
			fm.Resource = val
		case "created":
			fm.Created = val
		case "updated":
			fm.Updated = val
		case "due":
			fm.Due = val
		case "importance":
			fm.Importance, _ = strconv.ParseFloat(val, 64) // best-effort: defaults to zero
		case "archived":
			fm.Archived = val == "true"
		case "type":
			fm.Type = val
		case "confidence":
			fm.Confidence = val
		case "superseded_by":
			fm.SupersededBy = val
		}
	}
	return fm
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
