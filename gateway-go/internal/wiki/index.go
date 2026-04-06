package wiki

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Index is the master wiki index (index.md).
// It maps page paths to metadata for fast LLM navigation.
type Index struct {
	Entries         map[string]IndexEntry // relPath -> entry
	LastProcessed   string                // last processed diary date (YYYY-MM-DD)
	GeneratedAt     string                // ISO timestamp of last generation
}

// IndexEntry is a single entry in the master index.
type IndexEntry struct {
	Title      string
	Category   string
	Tags       []string
	Importance float64
	Updated    string // YYYY-MM-DD
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
		Title:      page.Meta.Title,
		Category:   page.Meta.Category,
		Tags:       page.Meta.Tags,
		Importance: page.Meta.Importance,
		Updated:    page.Meta.Updated,
	}
}

// RemoveEntry removes a page from the index.
func (idx *Index) RemoveEntry(relPath string) {
	delete(idx.Entries, relPath)
}

// Render produces the index.md content.
func (idx *Index) Render() string {
	var sb strings.Builder
	sb.WriteString("# 위키 인덱스\n\n")
	sb.WriteString(fmt.Sprintf("_자동 생성: %s_\n\n", time.Now().Format("2006-01-02 15:04")))

	if idx.LastProcessed != "" {
		sb.WriteString(fmt.Sprintf("마지막 일지 처리: %s\n\n", idx.LastProcessed))
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
			// High importance first, then alphabetical.
			if entries[i].entry.Importance != entries[j].entry.Importance {
				return entries[i].entry.Importance > entries[j].entry.Importance
			}
			return entries[i].path < entries[j].path
		})

		sb.WriteString(fmt.Sprintf("## %s\n\n", cat))
		for _, e := range entries {
			tags := ""
			if len(e.entry.Tags) > 0 {
				tags = " [" + strings.Join(e.entry.Tags, ", ") + "]"
			}
			// Encode importance and updated date as parseable metadata.
			var metaParts []string
			if e.entry.Importance > 0 {
				metaParts = append(metaParts, fmt.Sprintf("i:%.2f", e.entry.Importance))
			}
			if e.entry.Updated != "" {
				metaParts = append(metaParts, "u:"+e.entry.Updated)
			}
			meta := ""
			if len(metaParts) > 0 {
				meta = " (" + strings.Join(metaParts, ", ") + ")"
			}
			sb.WriteString(fmt.Sprintf("- [[%s]] — %s%s%s\n", e.path, e.entry.Title, tags, meta))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// Save writes the index to disk.
func (idx *Index) Save(path string) error {
	idx.GeneratedAt = time.Now().Format(time.RFC3339)
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
// The index file is primarily for LLM consumption, but we parse
// the [[path]] links to reconstruct the entry map.
func ParseIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	idx := NewIndex()
	lines := strings.Split(string(data), "\n")
	currentCategory := ""

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Parse category headers.
		if strings.HasPrefix(line, "## ") {
			currentCategory = strings.TrimPrefix(line, "## ")
			continue
		}

		// Parse entries: - [[path]] — title [tags] *
		if strings.HasPrefix(line, "- [[") {
			entry := parseIndexLine(line, currentCategory)
			if entry.path != "" {
				idx.Entries[entry.path] = entry.entry
			}
		}

		// Parse last processed diary date.
		if strings.HasPrefix(line, "마지막 일지 처리:") {
			idx.LastProcessed = strings.TrimSpace(strings.TrimPrefix(line, "마지막 일지 처리:"))
		}
	}

	return idx, nil
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
					importance, _ = strconv.ParseFloat(strings.TrimPrefix(part, "i:"), 64)
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
