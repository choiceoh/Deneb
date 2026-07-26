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
	// Created is the page's frontmatter creation date (YYYY-MM-DD). Unlike
	// Updated it is immutable across moves/reclassification, so recency
	// windows that must not re-trigger on metadata churn key off it (e.g.
	// ActiveCounterpartyDomains). Persisted as the trailing TSV column so it
	// survives a gateway restart (NewStore restores from index.md); empty only
	// for entries parsed from a pre-created-column index.md — callers fall
	// back to Updated.
	Created    string // YYYY-MM-DD
	Type       string // concept, entity, source, comparison, log
	Confidence string // high, medium, low
}

// newIndex creates an empty index.
func newIndex() *Index {
	return &Index{
		Entries: make(map[string]IndexEntry),
	}
}

// clone returns a deep copy of the index (entry map and slice fields
// included). Backs Store.SnapshotIndex — the copy is what makes lock-free
// walking/rendering safe while writers mutate the live index in place.
func (idx *Index) clone() *Index {
	if idx == nil {
		return nil
	}
	return &Index{
		Entries:       cloneIndexEntries(idx.Entries),
		LastProcessed: idx.LastProcessed,
		GeneratedAt:   idx.GeneratedAt,
	}
}

// cloneIndexEntries deep-copies an entry map, duplicating the Tags/Related
// slices so the copy shares no backing arrays with the live entries.
func cloneIndexEntries(in map[string]IndexEntry) map[string]IndexEntry {
	out := make(map[string]IndexEntry, len(in))
	for k, e := range in {
		e.Tags = append([]string(nil), e.Tags...)
		e.Related = append([]string(nil), e.Related...)
		out[k] = e
	}
	return out
}

// updateEntry adds or updates an index entry from a page. Slice fields are
// copied — storing the caller's live Tags/Related slices would alias the index
// entry to memory the caller may keep mutating after the write returns.
func (idx *Index) updateEntry(relPath string, page *Page) {
	idx.Entries[relPath] = IndexEntry{
		ID:         page.Meta.ID,
		Title:      page.Meta.Title,
		Summary:    page.Meta.Summary,
		Category:   page.Meta.Category,
		Tags:       append([]string(nil), page.Meta.Tags...),
		Related:    append([]string(nil), page.Meta.Related...),
		Importance: page.Meta.Importance,
		Updated:    page.Meta.Updated,
		Created:    page.Meta.Created,
		Type:       page.Meta.Type,
		Confidence: page.Meta.Confidence,
	}
}

// removeEntry removes a page from the index.
func (idx *Index) removeEntry(relPath string) {
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
		sb.WriteString("id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks\tcreated\trelated\n")
		for _, e := range entries {
			tags := strings.Join(e.entry.Tags, ",")
			imp := ""
			if e.entry.Importance > 0 {
				imp = fmt.Sprintf("%.2f", e.entry.Importance)
			}
			bl := backlinkCount[e.path]
			// created and related ride as the LAST columns (after the
			// render-computed backlinks) so every older field keeps its
			// position — old parsers and old files stay compatible; parseIndex
			// tolerates their absence. Without the related column the
			// in-memory Related lists evaporate on every restart (index.md is
			// what NewStore reloads), leaving backlink diffs, reference
			// repointing, and the backlinks count blind until the next full
			// rebuildIndex.
			sb.WriteString(fmt.Sprintf(
				"%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
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
				sanitizeTSV(e.entry.Created),
				renderRelatedTSV(e.entry.Related),
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

// relatedTSVSep separates related items inside the single TSV column. "|" is
// safe because renderRelatedTSV strips it from items: related entries are page
// paths / project codes / occasionally titles, none of which legitimately
// carry a pipe (the wikilink alias form "[[a|b]]" never reaches Related — the
// frontmatter list stores bare targets).
const relatedTSVSep = "|"

// renderRelatedTSV joins a Related list into one TSV field. Items are
// TSV-sanitized and have any inner separator replaced by a space so one item
// can never split into two on reparse.
func renderRelatedTSV(related []string) string {
	if len(related) == 0 {
		return ""
	}
	items := make([]string, 0, len(related))
	for _, r := range related {
		r = strings.TrimSpace(strings.ReplaceAll(sanitizeTSV(r), relatedTSVSep, " "))
		if r != "" {
			items = append(items, r)
		}
	}
	return strings.Join(items, relatedTSVSep)
}

// parseRelatedTSV splits the related TSV field back into a list.
func parseRelatedTSV(field string) []string {
	if strings.TrimSpace(field) == "" {
		return nil
	}
	var out []string
	for _, r := range strings.Split(field, relatedTSVSep) {
		if r = strings.TrimSpace(r); r != "" {
			out = append(out, r)
		}
	}
	return out
}

// Save writes the index to disk.
func (idx *Index) Save(path string) error {
	idx.GeneratedAt = dentime.Now().Format(time.RFC3339)
	data := idx.Render()
	tmp := path + ".tmp"
	if err := writeFileSync(tmp, []byte(data), 0o644); err != nil {
		return fmt.Errorf("wiki: write index tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("wiki: rename index: %w", err)
	}
	return nil
}

// parseIndex reads and parses an existing index.md.
// Supports both TSV format (new) and markdown list format (legacy).
func parseIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	idx := newIndex()
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
		// Parse the line with only the line ending trimmed, NOT TrimSpace:
		// an entry with an empty ID renders as "\tpath\t…", and trimming the
		// leading tab would shift every field left one column (path read as
		// the ID, title as the path — the whole row corrupts on reload).
		if strings.Contains(trimmed, "\t") && !strings.HasPrefix(trimmed, "- [[") {
			entry := parseTSVLine(strings.TrimRight(line, "\r\n"), currentCategory)
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
// id\tpath\ttitle\tsummary\ttags\timportance\tupdated\ttype\tconfidence\tbacklinks\tcreated\trelated
// Backward-compatible: the old 11-field format (without related), the old
// 10-field format (without created), and the old 8-field format (without
// type/confidence) still parse correctly — missing trailing columns simply
// stay zero (Created "" falls back to Updated at the call sites; Related nil
// self-heals on the next rebuildIndex).
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
	// backlinks (field 9) is computed at render time, not stored.
	// created (field 10) is absent in pre-created-column files → stays "".
	if len(fields) > 10 {
		e.Created = fields[10]
	}
	// related (field 11) is absent in pre-related-column files → stays nil.
	if len(fields) > 11 {
		e.Related = parseRelatedTSV(fields[11])
	}

	return indexRenderEntry{path: path, entry: e}
}

// isValidPageType gates the type column when the index is restored from
// index.md, so a numeric backlinks count from a pre-type file is not mistaken
// for a type. It must therefore list every value this package actually WRITES —
// otherwise the restore silently blanks the field and the next Save persists
// the loss.
//
// The domain writers drifted ahead of this list: deals.go writes "deal",
// sites.go "site", and project_status.go/restructure.go "project". Measured on
// the live wiki 2026-07-26, that was 87 of 626 pages losing their type on every
// restart. The guard still rejects free-form values (an LLM once wrote
// "preference"), which is what keeps the old-format numeric column out.
func isValidPageType(s string) bool {
	switch s {
	case "concept", "entity", "source", "comparison", "log", "deal", "site", "project":
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
