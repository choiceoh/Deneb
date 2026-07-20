package codesearch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	contextDetailedHits = 5
	contextSourceLines  = 16
	contextRelations    = 4
	contextMaxBytes     = 18_000
	contextMaxDocs      = 4
	contextDocBytes     = 1800
)

// Relation is one bounded, high-confidence CodeGraph edge adjacent to a hit.
// Calls/references are limited to the same file because unresolved global names
// can create cross-package false edges; implements/extends remain cross-file.
type Relation struct {
	Kind      string
	Direction string
	Qualified string
	File      string
}

// RepositoryDoc is an applicable rule/reference document selected from the
// source path hierarchy. Content is a query-relevant section projection, not a
// blind file head.
type RepositoryDoc struct {
	Path    string
	Role    string
	Content string
}

// BuildContextPack turns ranked paths into evidence the model can act on in a
// single tool round-trip: source excerpts, safe structural relations, and the
// applicable repository documentation hierarchy. It is deliberately emitted
// as a tool result (dynamic tail context), never as a per-turn system-prompt
// mutation, preserving prompt-cache reuse.
func BuildContextPack(ctx context.Context, repo, dir, query string, hits []Hit) string {
	if len(hits) == 0 {
		return ""
	}
	detailed := min(contextDetailedHits, len(hits))
	relations := relatedForHits(ctx, dir, hits[:detailed])
	paths := make([]string, 0, detailed)
	var b strings.Builder
	b.WriteString("## 자동 코드 컨텍스트\n")
	b.WriteString("검색 상위 결과의 실제 소스와 검증 가능한 인접 관계다. 경로와 줄 번호를 근거로 사용한다.\n")
	for i, hit := range hits[:detailed] {
		paths = append(paths, hit.File)
		fmt.Fprintf(&b, "\n### %d. %s `%s`\n", i+1, hit.Kind, hit.Qualified)
		fmt.Fprintf(&b, "`%s:%d-%d` · cosine %.3f\n", hit.File, hit.StartLine, max(hit.StartLine, hit.EndLine), hit.Cosine)
		if source := sourceExcerpt(repo, hit.Entry, contextSourceLines); source != "" {
			b.WriteString("\n~~~")
			b.WriteString(fenceLanguage(hit.Language))
			b.WriteByte('\n')
			b.WriteString(source)
			b.WriteString("\n~~~\n")
		}
		if adjacent := relations[hit.ID]; len(adjacent) > 0 {
			b.WriteString("관계: ")
			for j, relation := range adjacent {
				if j > 0 {
					b.WriteString("; ")
				}
				fmt.Fprintf(&b, "%s %s `%s` (%s)", relation.Direction, relation.Kind, relation.Qualified, relation.File)
			}
			b.WriteByte('\n')
		}
	}
	if len(hits) > detailed {
		b.WriteString("\n### 추가 후보\n")
		for _, hit := range hits[detailed:] {
			fmt.Fprintf(&b, "- %s `%s` — `%s:%d`\n", hit.Kind, hit.Qualified, hit.File, hit.StartLine)
		}
	}

	docs := ApplicableRepositoryDocs(repo, expandQuery(query), paths, os.ReadFile, contextMaxDocs, contextDocBytes)
	if len(docs) > 0 {
		b.WriteString("\n## 적용 문서 컨텍스트\n")
		b.WriteString("검색 결과 경로에 적용되는 규칙과 질의 관련 문서 섹션이다. `rules`는 작업 제약, `reference`는 근거 자료로 취급한다.\n")
		for _, doc := range docs {
			fmt.Fprintf(&b, "\n### `%s` (%s)\n%s\n", doc.Path, doc.Role, doc.Content)
		}
	}
	return capUTF8WithNote(b.String(), contextMaxBytes, "\n…(자동 컨텍스트 예산 초과로 나머지 생략)")
}

func sourceExcerpt(repo string, entry Entry, maxLines int) string {
	full, ok := safeRepoPath(repo, entry.File)
	if !ok {
		return ""
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	start := max(0, entry.StartLine-1)
	if start >= len(lines) {
		return ""
	}
	end := entry.EndLine
	if end <= start {
		end = start + maxLines
	}
	end = min(end, start+maxLines, len(lines))
	var b strings.Builder
	for i := start; i < end; i++ {
		fmt.Fprintf(&b, "%d | %s", i+1, lines[i])
		if i+1 < end {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func safeRepoPath(repo, rel string) (string, bool) {
	if repo == "" || rel == "" || filepath.IsAbs(rel) {
		return "", false
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	full := filepath.Join(repo, clean)
	back, err := filepath.Rel(repo, full)
	if err != nil || back == ".." || strings.HasPrefix(back, ".."+string(filepath.Separator)) {
		return "", false
	}
	return full, true
}

func fenceLanguage(language string) string {
	switch language {
	case "golang":
		return "go"
	case "typescript", "tsx":
		return "ts"
	case "javascript":
		return "js"
	case "shell":
		return "bash"
	default:
		return language
	}
}

type relationRow struct {
	Source          string `json:"source"`
	Target          string `json:"target"`
	Kind            string `json:"kind"`
	SourceQualified string `json:"sourceQualified"`
	TargetQualified string `json:"targetQualified"`
	SourceFile      string `json:"sourceFile"`
	TargetFile      string `json:"targetFile"`
}

func relatedForHits(ctx context.Context, dir string, hits []Hit) map[string][]Relation {
	out := make(map[string][]Relation)
	if len(hits) == 0 {
		return out
	}
	ids := make([]string, 0, len(hits))
	wanted := make(map[string]bool, len(hits))
	for _, hit := range hits {
		if strings.HasPrefix(hit.ID, "repo:") {
			continue
		}
		ids = append(ids, sqlString(hit.ID))
		wanted[hit.ID] = true
	}
	if len(ids) == 0 {
		return out
	}
	joined := strings.Join(ids, ",")
	query := fmt.Sprintf(`SELECT e.source AS source, e.target AS target, e.kind AS kind,
       s.qualified_name AS sourceQualified, t.qualified_name AS targetQualified,
       s.file_path AS sourceFile, t.file_path AS targetFile
FROM edges e
JOIN nodes s ON s.id=e.source
JOIN nodes t ON t.id=e.target
WHERE (e.source IN (%s) OR e.target IN (%s))
  AND (e.kind IN ('implements','extends')
       OR (e.kind IN ('calls','references') AND s.file_path=t.file_path))
ORDER BY CASE e.kind WHEN 'implements' THEN 0 WHEN 'extends' THEN 1 WHEN 'calls' THEN 2 ELSE 3 END,
         s.qualified_name, t.qualified_name
LIMIT 200`, joined, joined)
	rows, err := queryJSON[relationRow](ctx, filepath.Join(dir, "codegraph.db"), query)
	if err != nil {
		return out
	}
	seen := make(map[string]map[string]bool)
	for _, row := range rows {
		appendRelation := func(id string, relation Relation) {
			if !wanted[id] || len(out[id]) >= contextRelations || relation.Qualified == "" {
				return
			}
			if seen[id] == nil {
				seen[id] = make(map[string]bool)
			}
			key := relation.Direction + "\x00" + relation.Kind + "\x00" + relation.Qualified + "\x00" + relation.File
			if seen[id][key] {
				return
			}
			seen[id][key] = true
			out[id] = append(out[id], relation)
		}
		appendRelation(row.Source, Relation{Kind: row.Kind, Direction: "→", Qualified: row.TargetQualified, File: row.TargetFile})
		appendRelation(row.Target, Relation{Kind: row.Kind, Direction: "←", Qualified: row.SourceQualified, File: row.SourceFile})
	}
	return out
}

func sqlString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ApplicableRepositoryDocs selects the instruction hierarchy and a relevant
// nearby README for ranked source paths. Rules are specific-first; Markdown is
// section-ranked against the coherent query and source anchors.
func ApplicableRepositoryDocs(
	root, query string,
	sourcePaths []string,
	readFile func(string) ([]byte, error),
	maxDocs, perDocBytes int,
) []RepositoryDoc {
	if root == "" || readFile == nil || maxDocs <= 0 || perDocBytes <= 0 {
		return nil
	}
	type candidate struct {
		rel   string
		role  string
		depth int
		order int
	}
	seen := make(map[string]bool)
	var candidates []candidate
	add := func(rel, role string, depth, order int) {
		rel = filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
		if rel == "." || rel == "" || strings.Contains(rel, "..") || seen[rel] {
			return
		}
		seen[rel] = true
		candidates = append(candidates, candidate{rel: rel, role: role, depth: depth, order: order})
	}
	for order, source := range sourcePaths {
		if _, ok := safeRepoPath(root, source); !ok {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(source)))
		for current := dir; ; current = filepath.ToSlash(filepath.Dir(filepath.FromSlash(current))) {
			depth := strings.Count(strings.Trim(current, "/."), "/") + 1
			prefix := ""
			if current != "." && current != "" {
				prefix = current + "/"
			}
			add(prefix+"CLAUDE.md", "rules", depth, order)
			add(prefix+"AGENTS.md", "rules", depth, order)
			if current == "." || current == "" {
				break
			}
		}
		// README is reference material, not an instruction source. Only the
		// nearest one is considered and it must score against the query below.
		prefix := ""
		if dir != "." && dir != "" {
			prefix = dir + "/"
		}
		add(prefix+"README.md", "reference", strings.Count(strings.Trim(dir, "/."), "/")+1, order)
	}
	// Higher-ranked source paths first, then more specific rules. CLAUDE wins
	// ties over AGENTS; references are considered after applicable rules.
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].order != candidates[j].order {
			return candidates[i].order < candidates[j].order
		}
		if candidates[i].role != candidates[j].role {
			return candidates[i].role == "rules"
		}
		if candidates[i].depth != candidates[j].depth {
			return candidates[i].depth > candidates[j].depth
		}
		return candidates[i].rel < candidates[j].rel
	})
	anchors := strings.Join(sourcePaths, " ")
	var out []RepositoryDoc
	for _, candidate := range candidates {
		full, ok := safeRepoPath(root, candidate.rel)
		if !ok {
			continue
		}
		body, err := readFile(full)
		if err != nil || len(body) == 0 || !utf8.Valid(body) {
			continue
		}
		content, score := relevantMarkdownSections(string(body), query+" "+anchors, perDocBytes)
		if candidate.role == "reference" && score == 0 {
			continue
		}
		out = append(out, RepositoryDoc{Path: candidate.rel, Role: candidate.role, Content: content})
		if len(out) >= maxDocs {
			break
		}
	}
	return out
}

type markdownSection struct {
	index     int
	text      string
	score     int
	mandatory bool
}

func relevantMarkdownSections(content, query string, capBytes int) (string, int) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var preamble []string
	var sections []markdownSection
	start := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		isHeading := strings.HasPrefix(trimmed, "#") && strings.Contains(trimmed, " ")
		if !isHeading {
			continue
		}
		if start < 0 {
			preamble = lines[:i]
		} else {
			sections = append(sections, markdownSection{index: start, text: strings.Join(lines[start:i], "\n")})
		}
		start = i
	}
	if start >= 0 {
		sections = append(sections, markdownSection{index: start, text: strings.Join(lines[start:], "\n")})
	} else {
		return capUTF8WithNote(strings.TrimSpace(content), capBytes, "\n…(문서 생략)"), sectionOverlap(content, query)
	}
	bestScore := 0
	for i := range sections {
		sections[i].score = sectionOverlap(sections[i].text, query)
		sections[i].mandatory = mandatoryRuleSection(sections[i].text)
		bestScore = max(bestScore, sections[i].score)
	}
	sort.SliceStable(sections, func(i, j int) bool {
		if sections[i].score != sections[j].score {
			return sections[i].score > sections[j].score
		}
		return sections[i].index < sections[j].index
	})
	var selected []markdownSection
	if sections[0].score > 0 {
		selected = append(selected, sections[0])
	}
	// Always carry one safety/invariant section from an applicable instruction
	// document even when lexical relevance points elsewhere. Query projection
	// must not silently erase repository safety rules.
	for _, section := range sections {
		if !section.mandatory || (len(selected) > 0 && section.index == selected[0].index) {
			continue
		}
		selected = append(selected, section)
		break
	}
	if len(selected) == 0 {
		selected = append(selected, sections[0])
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].index < selected[j].index })
	var b strings.Builder
	intro := strings.TrimSpace(strings.Join(preamble, "\n"))
	if intro != "" {
		b.WriteString(capUTF8WithNote(intro, min(320, capBytes/4), "\n…"))
		b.WriteString("\n\n")
	}
	for i, section := range selected {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(strings.TrimSpace(section.text))
	}
	return capUTF8WithNote(strings.TrimSpace(b.String()), capBytes, "\n…(관련 섹션 외 생략)"), bestScore
}

func mandatoryRuleSection(text string) bool {
	heading := text
	if line := strings.IndexByte(heading, '\n'); line >= 0 {
		heading = heading[:line]
	}
	heading = strings.ToLower(heading)
	for _, marker := range []string{"안전", "safety", "항상 적용", "always apply", "불변", "invariant", "금지"} {
		if strings.Contains(heading, marker) {
			return true
		}
	}
	return false
}

func sectionOverlap(content, query string) int {
	terms := uniqueSearchTerms(query)
	if len(terms) == 0 {
		return 0
	}
	tokens := make(map[string]int)
	for _, token := range lexicalTokens(content) {
		tokens[token]++
	}
	score := 0
	for _, term := range terms {
		score += min(3, tokens[term])
	}
	return score
}

func capUTF8WithNote(text string, maxBytes int, note string) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(text) <= maxBytes {
		return strings.TrimRight(text, "\n")
	}
	reserve := len(note)
	if reserve >= maxBytes {
		reserve = 0
		note = ""
	}
	cut := text[:maxBytes-reserve]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	if line := strings.LastIndexByte(cut, '\n'); line > len(cut)/2 {
		cut = cut[:line]
	}
	return strings.TrimRight(cut, "\n") + note
}
