package wiki

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
)

// ImportLegacyFactFiles converts bullet-sized MEMORY.md/USER.md entries into
// legacy-authority facts before those files become generated compatibility
// views. It is idempotent by source+value. Bullet rows and induction-metadata
// prose become facts; unrelated prose remains available in the preserved
// *.legacy.md copy rather than being promoted without an identity contract.
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
	for _, name := range files {
		path := filepath.Join(workspaceDir, name)
		fileCandidates, err := readLegacyFactFile(path, name)
		if err != nil {
			return 0, err
		}
		// Preserve file and line order. legacy_import uses an explicit latest-row
		// policy, so earlier values become superseded deny history while the last
		// row is the one current cutover baseline (never a peer conflict).
		candidates = append(candidates, fileCandidates...)
	}

	imported := 0
	for _, candidate := range candidates {
		input := candidate.input
		source := input.Sources[0]
		if s.hasFactSourceValue(input.Subject, input.Key, source, input.Value) {
			continue
		}
		result, err := s.upsertFact(input, false)
		if err != nil {
			if imported > 0 {
				if syncErr := s.syncFactDerived(); syncErr != nil {
					return imported, errors.Join(
						fmt.Errorf("import %s line %d: %w", candidate.label, candidate.lineNo, err),
						fmt.Errorf("sync derived facts: %w", syncErr),
					)
				}
			}
			return imported, fmt.Errorf("import %s line %d: %w", candidate.label, candidate.lineNo, err)
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
	return imported, nil
}

type legacyFactCandidate struct {
	input  FactInput
	label  string
	lineNo int
}

func readLegacyFactFile(path, label string) ([]legacyFactCandidate, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if bytesContainGeneratedMarker(raw) {
		return nil, nil
	}

	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	section := ""
	var candidates []legacyFactCandidate
	for i := 0; i < len(lines); i++ {
		lineNo := i + 1
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#") {
			section = strings.TrimSpace(strings.TrimLeft(line, "#"))
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
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(line, "- "))
		if value == "" {
			continue
		}
		candidates = append(candidates, makeLegacyFactCandidate(value, section, label, lineNo, "self", ""))
	}
	return candidates, nil
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
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "identity.address":
		return FactKindIdentity
	case "communication.language":
		return FactKindPreference
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
