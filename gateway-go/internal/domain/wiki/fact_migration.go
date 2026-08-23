package wiki

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
)

const legacyActiveContextMarker = "<!-- deneb:generated-legacy-active-context"

// ImportLegacyFactFiles converts bullet-sized MEMORY.md/USER.md entries into
// legacy-authority facts before those files become generated compatibility
// views. It is idempotent by source+value. Bullet rows and induction-metadata
// prose become facts; unmatched prose remains an ordinary, high-importance wiki
// page so it stays in the Tier1 prompt after MEMORY.md/USER.md are replaced. The
// full original bytes are still preserved separately as *.legacy.md.
func (s *Store) ImportLegacyFactFiles(workspaceDir string) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("wiki: nil store")
	}
	workspaceDir = strings.TrimSpace(workspaceDir)
	if workspaceDir == "" {
		return 0, nil
	}
	files := []string{"MEMORY.md", "USER.md"}
	var candidates []legacyFactCandidate
	var activeContexts []legacyActiveContext
	for _, name := range files {
		path := filepath.Join(workspaceDir, name)
		legacyFile, err := readLegacyFactFileWithContext(path, name)
		if err != nil {
			return 0, err
		}
		// Preserve file and line order. legacy_import uses an explicit latest-row
		// policy, so earlier values become superseded deny history while the last
		// row is the one current cutover baseline (never a peer conflict).
		candidates = append(candidates, legacyFile.candidates...)
		if legacyFile.activeContext != "" {
			activeContexts = append(activeContexts, legacyActiveContext{label: name, content: legacyFile.activeContext})
		}
	}

	imported := 0
	var skipped []string
	for _, candidate := range candidates {
		input := candidate.input
		source := input.Sources[0]
		if s.hasFactSourceValue(input.Subject, input.Key, source, input.Value) {
			continue
		}
		result, err := s.upsertFact(input, false)
		if err != nil {
			// One unconvertible bullet must not abort the cutover.
			//
			// This is a best-effort read of years of hand-written prose: two bullets
			// can legitimately describe the same subject+key with different kinds
			// (an "identity" line and a "preference" line about the same trait), and
			// upsertFact refuses the second. Aborting there returned an error all the
			// way up to server init, which exits — and because the offending line is
			// the same on every boot, the retry the caller counts on can never
			// succeed. That is a permanent crash loop from one line of MEMORY.md
			// (observed 2026-08-23, ~200 restarts).
			//
			// Nothing is lost by skipping: the row stays in the preserved
			// *.legacy.md copy, exactly like the prose this importer already
			// declines to promote.
			skipped = append(skipped, fmt.Sprintf("%s line %d: %v", candidate.label, candidate.lineNo, err))
			continue
		}
		if result.Committed {
			imported++
		}
	}
	if imported > 0 {
		if err := s.syncFactDerived(); err != nil {
			return imported, err
		}
	}
	for _, activeContext := range activeContexts {
		if err := s.preserveLegacyActiveContext(activeContext); err != nil {
			return imported, fmt.Errorf("preserve unmatched %s active context: %w", activeContext.label, err)
		}
	}
	s.legacyImportSkips = skipped
	return imported, nil
}

// legacyImportSkipLimit bounds how many skip reasons a caller prints at once;
// the point is to notice the pattern, not to reprint the file.
const legacyImportSkipLimit = 10

// LegacyFactImportSkips describes bullets the last [ImportLegacyFactFiles] could
// not convert. Reported this way rather than as an error because the caller
// treats an error as "do not serve", and a handful of unconvertible legacy lines
// is not a reason to refuse service.
func (s *Store) LegacyFactImportSkips() []string {
	if s == nil {
		return nil
	}
	skips := s.legacyImportSkips
	if len(skips) > legacyImportSkipLimit {
		skips = skips[:legacyImportSkipLimit]
	}
	return append([]string(nil), skips...)
}

type legacyFactCandidate struct {
	input  FactInput
	label  string
	lineNo int
}

type legacyFactFileRead struct {
	candidates    []legacyFactCandidate
	activeContext string
}

type legacyActiveContext struct {
	label   string
	content string
}

func readLegacyFactFile(path, label string) ([]legacyFactCandidate, error) {
	legacyFile, err := readLegacyFactFileWithContext(path, label)
	return legacyFile.candidates, err
}

func readLegacyFactFileWithContext(path, label string) (legacyFactFileRead, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return legacyFactFileRead{}, nil
		}
		return legacyFactFileRead{}, err
	}
	if bytesContainGeneratedMarker(raw) {
		return legacyFactFileRead{}, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return legacyFactFileRead{}, err
	}

	section := ""
	var candidates []legacyFactCandidate
	retained := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimLeft(line, "#"))
			retained = append(retained, lines[i])
			continue
		}
		if meta, ok := parseLegacyInductionMeta(line); ok {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}
			bodyStart := j
			var bodyLines []string
			for j < len(lines) {
				bodyLine := strings.TrimSpace(lines[j])
				if bodyLine == "" || strings.HasPrefix(bodyLine, "#") {
					break
				}
				if _, nextMeta := parseLegacyInductionMeta(bodyLine); nextMeta {
					break
				}
				bodyLines = append(bodyLines, bodyLine)
				j++
			}
			value := strings.TrimSpace(strings.Join(bodyLines, "\n"))
			if value != "" {
				candidateLine := lineNo
				if bodyStart < len(lines) {
					candidateLine = bodyStart + 1
				}
				candidates = append(candidates, makeLegacyFactCandidate(
					value, section, label, candidateLine, meta.subject, meta.key,
				))
			}
			i = j - 1
			continue
		}
		if !strings.HasPrefix(line, "- ") {
			retained = append(retained, lines[i])
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if value == "" {
			continue
		}
		candidates = append(candidates, makeLegacyFactCandidate(value, section, label, lineNo, "self", ""))
	}
	activeContext := strings.TrimSpace(strings.Join(retained, "\n"))
	if !legacyContextHasNonHeadingContent(activeContext) {
		activeContext = ""
	}
	return legacyFactFileRead{candidates: candidates, activeContext: activeContext}, nil
}

func legacyContextHasNonHeadingContent(content string) bool {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func legacyActiveContextPagePath(label string) string {
	switch strings.ToUpper(strings.TrimSpace(label)) {
	case "MEMORY.MD":
		return "사용자/legacy-memory-context.md"
	case "USER.MD":
		return "사용자/legacy-user-context.md"
	default:
		return ""
	}
}

func (s *Store) preserveLegacyActiveContext(context legacyActiveContext) error {
	path := legacyActiveContextPagePath(context.label)
	if path == "" || strings.TrimSpace(context.content) == "" {
		return nil
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	marker := fmt.Sprintf("%s source=%s; migration-copy -->", legacyActiveContextMarker, context.label)
	page := NewPage("Legacy "+context.label+" active context", "사용자", []string{"legacy", "migration"})
	page.Meta.ID = id
	page.Meta.Importance = 1
	page.Meta.Type = "reference"
	page.Meta.Summary = "Fact identity가 없는 기존 컨텍스트의 cutover 보존본"
	page.Meta.Sources = []string{"workspace:" + legacyProjectionName(context.label)}
	page.Body = marker + "\n# Legacy " + context.label + " active context\n\n" +
		"> 기존 파일에서 fact-sized 행만 제외한 보존 컨텍스트다. 충돌 시 append-only fact plane의 현행 사실이 우선한다.\n\n" +
		strings.TrimSpace(context.content) + "\n"

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	current, err := s.ReadPage(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if current != nil {
		if !strings.HasPrefix(strings.TrimSpace(current.Body), legacyActiveContextMarker) {
			return fmt.Errorf("reserved legacy context path already contains a manual page: %s", path)
		}
		if current.Body != page.Body {
			return fmt.Errorf("existing generated legacy context differs from %s", context.label)
		}
		if current.Meta.ID == page.Meta.ID && current.Meta.Importance == page.Meta.Importance && current.Meta.Type == page.Meta.Type {
			return nil
		}
	}
	return s.writePageLocked(path, page)
}

type legacyInductionMeta struct {
	subject string
	key     string
}

func parseLegacyInductionMeta(line string) (legacyInductionMeta, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "<!--") || !strings.HasSuffix(line, "-->") {
		return legacyInductionMeta{}, false
	}
	inside := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "<!--"), "-->"))
	fields := strings.Fields(inside)
	if len(fields) == 0 || fields[0] != "induction" {
		return legacyInductionMeta{}, false
	}
	values := make(map[string]string)
	for _, field := range fields[1:] {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			values[key] = value
		}
	}
	if values["write_target"] != "profile" {
		return legacyInductionMeta{}, false
	}
	return legacyInductionMeta{
		subject: memory.NormalizeSubject(values["subject_id"]),
		key:     strings.TrimSpace(values["fact_key"]),
	}, true
}

func makeLegacyFactCandidate(value, section, label string, lineNo int, subject, key string) legacyFactCandidate {
	if strings.TrimSpace(key) == "" {
		key = legacyFactKey(value)
	}
	return legacyFactCandidate{
		input: FactInput{
			Subject: memory.NormalizeSubject(subject), Key: key, Value: value,
			Kind: legacyFactKind(key, section, value), Authority: FactAuthorityLegacyImport,
			Sources: []string{fmt.Sprintf("workspace:%s#L%d", legacyProjectionName(label), lineNo)}, Actor: "legacy-migration",
			Reason: "기존 " + label + " 호환 이관",
		},
		label: label, lineNo: lineNo,
	}
}

func legacyFactKey(value string) string {
	if key := legacyCanonicalLabelKey(value); key != "" {
		return key
	}
	return memory.FactKeyFromText(value)
}

// legacyCanonicalLabelKey recognizes only the structured profile labels that
// have a live induction identity. This deliberately does not alias arbitrary
// prose containing "이름" or "언어".
func legacyCanonicalLabelKey(text string) string {
	label := strings.TrimSpace(text)
	for _, separator := range []string{":", "：", "="} {
		if before, _, ok := strings.Cut(label, separator); ok {
			label = before
			break
		}
	}
	label = strings.Trim(strings.TrimSpace(label), "*_` ")
	switch label {
	case "이름", "호칭":
		return "identity.address"
	case "언어":
		return "communication.language"
	default:
		return ""
	}
}

func legacyProjectionName(name string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + ".legacy" + ext
}

func legacyFactKind(key, section, value string) FactKind {
	// A profile-axis key carries its own kind; the section heading must not
	// override it. Deriving the two independently is what produced the
	// "kind is preference, not identity" conflict: a bullet under "## 사용자 모델"
	// mentioning 장황한 설명 got key communication.response_length (a preference
	// axis) but kind identity from its heading, and the store refused it.
	if axisKind, ok := memory.FactKindForKey(key); ok {
		switch axisKind {
		case "identity":
			return FactKindIdentity
		case "preference":
			return FactKindPreference
		}
	}
	lower := strings.ToLower(section + " " + value)
	switch {
	case strings.Contains(section, "선호") || strings.Contains(lower, "선호") || strings.Contains(lower, "답변"):
		return FactKindPreference
	case strings.Contains(section, "사용자 모델") || strings.Contains(section, "맥락") || strings.Contains(section, "상호 인식"):
		return FactKindIdentity
	default:
		return FactKindGeneric
	}
}

func (s *Store) hasFactSourceValue(subject, key, source, value string) bool {
	identity := factIdentity(subject, key)
	s.factMu.RLock()
	defer s.factMu.RUnlock()
	for _, claim := range s.factState.Facts[identity] {
		if !sameFactValue(claim.Value, value) {
			continue
		}
		for _, existingSource := range claim.Sources {
			if existingSource == source {
				// A previous version of the importer produced peer conflicts for
				// MEMORY/USER duplicates. Reassert the final row once to heal that
				// old baseline; every other lifecycle state remains idempotent.
				return claim.Status != FactStatusConflicted
			}
		}
	}
	return false
}
