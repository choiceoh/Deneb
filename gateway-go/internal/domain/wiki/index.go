package wiki

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/dentime"
)

// Index is the master wiki index (index.md).
// It maps page paths to metadata for fast LLM navigation.
type Index struct {
	Entries       map[string]IndexEntry // relPath -> entry
	LastProcessed string                // last processed diary date (YYYY-MM-DD)
	GeneratedAt   string                // ISO timestamp of last generation
}

// IndexEntry is a single entry in the master index.
type IndexEntry struct {
	ID         string
	Title      string
	Summary    string
	Category   string
	Tags       []string
	Related    []string
	Importance float64
	Updated    string // YYYY-MM-DD
	Type       string // concept, entity, source, comparison, log
	Confidence string // high, medium, low
}

// NewIndex creates an empty index.
func NewIndex() *Index {
	return &Index{
		Entries: make(map[string]IndexEntry),
	}
}

// UpdateEntry adds or updates an index entry from a page.
func (idx *Index) UpdateEntry(relPath string, page *Page) {
	idx.Entries[relPath] = IndexEntry{
		ID:         page.Meta.ID,
		Title:      page.Meta.Title,
		Summary:    page.Meta.Summary,
		Category:   page.Meta.Category,
		Tags:       page.Meta.Tags,
		Related:    page.Meta.Related,
		Importance: page.Meta.Importance,
		Updated:    page.Meta.Updated,
		Type:       page.Meta.Type,
		Confidence: page.Meta.Confidence,
	}
}

// RemoveEntry removes a page from the index.
func (idx *Index) RemoveEntry(relPath string) {
	delete(idx.Entries, relPath)
}

// Render produces the index.md content in TSV format for machine parsing.
func (idx *Index) Render() string {
	var sb strings.Builder
	sb.WriteString("# 위키 인덱스\n\n")
	fmt.Fprintf(&sb, "_자동 생성: %s_\n\n", dentime.Now().Format("2006-01-02 15:04"))

	if idx.LastProcessed != "" {
		sb.WriteString(fmt.Sprintf("마지막 일지 처리: %s\n\n", idx.LastProcessed))
	}

	// Build backlink counts: for each path, count how many entries reference it.
	backlinkCount := map[string]int{}
	for _, entry := range idx.Entries {
		for _, rel := range entry.Related {
			backlinkCount[rel]++
		}
	}

	// Group entries by category.
	byCategory := map[string][]indexRenderEntry{}
	for path, entry := range idx.Entries {
		cat := entry.Category
		if cat == "" {
			cat = "(기타)"
		}
		byCategory[cat] = append(byCategory[cat], indexRenderEntry{path: path, entry: entry})
	}

	// Sort categories.
	cats := make([]string, 0, len(byCategory))
	for c := range byCategory {
		cats = append(cats, c)
	}
	sort.Strings(cats)

	for _, cat := range cats {
		entries := byCategory[cat]
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].entry.Importance != entries[j].entry.Importance {
				return entries[i].entry.Importance > entries[j].entry.Importance
			}
			return entries[i].path < entries[j].path
		})

		sb.WriteString(fmt.Sprintf("## %s\n\n", cat))
		sb.WriteString("id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks\n")
		for _, e := range entries {
			tags := strings.Join(e.entry.Tags, ",")
			imp := ""
			if e.entry.Importance > 0 {
				imp = fmt.Sprintf("%.2f", e.entry.Importance)
			}
			bl := backlinkCount[e.path]
			sb.WriteString(fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
				sanitizeTSV(e.entry.ID),
				e.path,
				sanitizeTSV(e.entry.Title),
				sanitizeTSV(e.entry.Summary),
				sanitizeTSV(tags),
				imp,
				e.entry.Updated,
				sanitizeTSV(e.entry.Type),
				sanitizeTSV(e.entry.Confidence),
				bl,
			))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// sanitizeTSV replaces tabs and newlines with spaces to keep TSV rows intact.
func sanitizeTSV(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
}

// Save writes the index to disk.
func (idx *Index) Save(path string) error {
	idx.GeneratedAt = dentime.Now().Format(time.RFC3339)
	data := idx.Render()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o644); err != nil {
		return fmt.Errorf("wiki: write index tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("wiki: rename index: %w", err)
	}
	return nil
}

// ParseIndex reads and parses an existing index.md.
// Supports both TSV format (new) and markdown list format (legacy).
func ParseIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	idx := NewIndex()
	lines := strings.Split(string(data), "\n")
	currentCategory := ""

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Parse category headers.
		if strings.HasPrefix(trimmed, "## ") {
			currentCategory = strings.TrimPrefix(trimmed, "## ")
			continue
		}

		// Skip TSV header rows.
		if strings.HasPrefix(trimmed, "id\t") {
			continue
		}

		// TSV data row: contains tabs and doesn't start with "- [[".
		if strings.Contains(trimmed, "\t") && !strings.HasPrefix(trimmed, "- [[") {
			entry := parseTSVLine(trimmed, currentCategory)
			if entry.path != "" {
				idx.Entries[entry.path] = entry.entry
			}
			continue
		}

		// Legacy format: - [[path]] — title [tags] (i:0.90, u:2026-04-06)
		if strings.HasPrefix(trimmed, "- [[") {
			entry := parseIndexLine(trimmed, currentCategory)
			if entry.path != "" {
				idx.Entries[entry.path] = entry.entry
			}
			continue
		}

		// Parse last processed diary date.
		if strings.HasPrefix(trimmed, "마지막 일지 처리:") {
			idx.LastProcessed = strings.TrimSpace(strings.TrimPrefix(trimmed, "마지막 일지 처리:"))
		}
	}

	return idx, nil
}

// parseTSVLine parses a TSV data row:
// id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks
// Backward-compatible: old 8-field format (without type/confidence) still parses correctly.
func parseTSVLine(line, category string) indexRenderEntry {
	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return indexRenderEntry{}
	}

	var e IndexEntry
	e.Category = category

	if len(fields) > 0 {
		e.ID = fields[0]
	}
	path := ""
	if len(fields) > 1 {
		path = fields[1]
	}
	if len(fields) > 2 {
		e.Title = fields[2]
	}
	if len(fields) > 3 {
		e.Summary = fields[3]
	}
	if len(fields) > 4 && fields[4] != "" {
		for _, t := range strings.Split(fields[4], ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				e.Tags = append(e.Tags, t)
			}
		}
	}
	if len(fields) > 5 {
		e.Importance, _ = strconv.ParseFloat(fields[5], 64) // best-effort: defaults to zero
	}
	if len(fields) > 6 {
		e.Updated = fields[6]
	}
	// New format: type (field 7), confidence (field 8), backlinks (field 9).
	// Old format: backlinks (field 7) — numeric, so won't match valid type values.
	if len(fields) > 7 {
		if isValidPageType(fields[7]) {
			e.Type = fields[7]
		}
	}
	if len(fields) > 8 {
		if isValidConfidence(fields[8]) {
			e.Confidence = fields[8]
		}
	}
	// backlinks (last field) is computed at render time, not stored.

	return indexRenderEntry{path: path, entry: e}
}

func isValidPageType(s string) bool {
	switch s {
	case "concept", "entity", "source", "comparison", "log":
		return true
	}
	return false
}

func isValidConfidence(s string) bool {
	switch s {
	case "high", "medium", "low":
		return true
	}
	return false
}

type indexRenderEntry struct {
	path  string
	entry IndexEntry
}

func parseIndexLine(line, category string) indexRenderEntry {
	// Format: - [[path]] — title [tags] (i:0.90, u:2026-04-06)
	// Legacy:  - [[path]] — title [tags] *
	start := strings.Index(line, "[[")
	end := strings.Index(line, "]]")
	if start < 0 || end < 0 || end <= start {
		return indexRenderEntry{}
	}

	path := line[start+2 : end]
	rest := strings.TrimSpace(line[end+2:])
	rest = strings.TrimPrefix(rest, "—")
	rest = strings.TrimSpace(rest)

	var importance float64
	var updated string

	// Parse metadata suffix: (i:0.90, u:2026-04-06)
	if metaStart := strings.LastIndex(rest, "("); metaStart >= 0 {
		if metaEnd := strings.LastIndex(rest, ")"); metaEnd > metaStart {
			metaStr := rest[metaStart+1 : metaEnd]
			rest = strings.TrimSpace(rest[:metaStart])
			for _, part := range strings.Split(metaStr, ",") {
				part = strings.TrimSpace(part)
				if strings.HasPrefix(part, "i:") {
					importance, _ = strconv.ParseFloat(strings.TrimPrefix(part, "i:"), 64) // best-effort: defaults to zero
				} else if strings.HasPrefix(part, "u:") {
					updated = strings.TrimPrefix(part, "u:")
				}
			}
		}
	}

	// Legacy: trailing " *" means importance >= 0.8.
	if importance == 0 && strings.HasSuffix(rest, " *") {
		importance = 0.85
		rest = strings.TrimSuffix(rest, " *")
		rest = strings.TrimSpace(rest)
	}

	// Extract tags from [tag1, tag2].
	var tags []string
	if tagStart := strings.LastIndex(rest, "["); tagStart >= 0 {
		if tagEnd := strings.LastIndex(rest, "]"); tagEnd > tagStart {
			tagStr := rest[tagStart+1 : tagEnd]
			for _, t := range strings.Split(tagStr, ",") {
				t = strings.TrimSpace(t)
				if t != "" {
					tags = append(tags, t)
				}
			}
			rest = strings.TrimSpace(rest[:tagStart])
		}
	}

	return indexRenderEntry{
		path: path,
		entry: IndexEntry{
			Title:      rest,
			Category:   category,
			Tags:       tags,
			Importance: importance,
			Updated:    updated,
		},
	}
}
