package wiki

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Page represents a single wiki page with YAML frontmatter and markdown body.
type Page struct {
	Meta Frontmatter
	Body string // markdown content after frontmatter
}

// Frontmatter is the YAML metadata at the top of a wiki page.
type Frontmatter struct {
	ID         string // short identifier (e.g., "dgx-spark", "gemma4-switch")
	Title      string
	Summary    string // one-line description for index-level filtering (~80 chars)
	Category   string
	Tags       []string
	Related    []string
	Created    string  // YYYY-MM-DD
	Updated    string  // YYYY-MM-DD
	Importance float64 // 0.0-1.0
	Archived   bool
	Type       string // concept, entity, source, comparison, log
	Confidence string // high, medium, low
}

// ParsePage parses a wiki page from raw bytes.
func ParsePage(data []byte) (*Page, error) {
	meta, body, err := splitFrontmatter(data)
	if err != nil {
		// No frontmatter — treat entire content as body.
		return &Page{Body: string(data)}, nil
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
	if p.Meta.Created != "" {
		buf.WriteString("created: " + p.Meta.Created + "\n")
	}
	if p.Meta.Updated != "" {
		buf.WriteString("updated: " + p.Meta.Updated + "\n")
	}
	if p.Meta.Importance > 0 {
		buf.WriteString(fmt.Sprintf("importance: %.2f\n", p.Meta.Importance))
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
	buf.WriteString("---\n\n")

	buf.WriteString(p.Body)
	return buf.Bytes()
}

// WritePageFile writes a page to disk atomically (via temp file + rename).
func WritePageFile(path string, page *Page) error {
	data := page.Render()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("wiki: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("wiki: rename: %w", err)
	}
	return nil
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
		case "title":
			fm.Title = val
		case "summary":
			fm.Summary = val
		case "category":
			fm.Category = val
		case "tags":
			fm.Tags = parseFlowArray(val)
		case "related":
			fm.Related = parseFlowArray(val)
		case "created":
			fm.Created = val
		case "updated":
			fm.Updated = val
		case "importance":
			fm.Importance, _ = strconv.ParseFloat(val, 64) // best-effort: defaults to zero
		case "archived":
			fm.Archived = val == "true"
		case "type":
			fm.Type = val
		case "confidence":
			fm.Confidence = val
		}
	}
	return fm
}

// parseKV splits "key: value" into (key, value, true).
func parseKV(line string) (string, string, bool) {
	idx := strings.Index(line, ":")
	if idx < 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	val := strings.TrimSpace(line[idx+1:])
	return key, val, true
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
