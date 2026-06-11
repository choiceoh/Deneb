// dreamer_diary.go — diary-side state of a dream cycle: scanning diary
// files against the processed-state ledger, capsule history formatting, and
// the dream proposal report persisted for operator review. Split from
// dreamer.go (WikiDreamer core).
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

func (wd *WikiDreamer) scanDiaries(_ context.Context) (*diaryScanResult, error) {
	diaryDir := wd.store.DiaryDir()
	if diaryDir == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(diaryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read diary dir: %w", err)
	}

	state := wd.loadDiaryProcessState()
	legacyCutoff := wd.store.Index().LastProcessed
	var diaryFiles []os.DirEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "diary-") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		diaryFiles = append(diaryFiles, e)
	}

	if len(diaryFiles) == 0 {
		return nil, nil
	}

	sort.Slice(diaryFiles, func(i, j int) bool {
		return diaryFiles[i].Name() < diaryFiles[j].Name()
	})

	var sb strings.Builder
	const maxBytes = 30000
	latestDate := ""
	for _, entry := range diaryFiles {
		name := entry.Name()
		info, err := entry.Info()
		if err != nil {
			continue
		}
		date := diaryDateFromName(name)

		fileState, hasState := state.Files[name]
		if !hasState && legacyCutoff != "" && date != "" && date < legacyCutoff {
			state.Files[name] = diaryFileState{
				Offset:  info.Size(),
				Size:    info.Size(),
				ModUnix: info.ModTime().Unix(),
			}
			continue
		}

		offset := fileState.Offset
		if offset < 0 || offset > info.Size() {
			offset = 0
		}
		if offset == info.Size() {
			continue
		}

		data, err := os.ReadFile(filepath.Join(diaryDir, name))
		if err != nil {
			continue
		}
		if offset > int64(len(data)) {
			offset = 0
		}
		remaining := maxBytes - sb.Len()
		if remaining <= 0 {
			break
		}
		chunk := data[offset:]
		nextOffset := info.Size()
		if len(chunk) > remaining {
			// Back the cut up to a rune boundary: diaries are Korean-heavy and
			// a byte-indexed cut can split a 3-byte Hangul rune, feeding the
			// synthesizer invalid UTF-8. The next scan resumes at nextOffset,
			// so the trimmed bytes are not lost, just deferred.
			cut := remaining
			for cut > 0 && !utf8.RuneStart(chunk[cut]) {
				cut--
			}
			chunk = chunk[:cut]
			nextOffset = offset + int64(len(chunk))
		}
		if len(chunk) == 0 {
			state.Files[name] = diaryFileState{
				Offset:  nextOffset,
				Size:    info.Size(),
				ModUnix: info.ModTime().Unix(),
			}
			continue
		}

		fmt.Fprintf(&sb, "--- %s @%d ---\n", name, offset)
		sb.Write(chunk)
		sb.WriteByte('\n')
		state.Files[name] = diaryFileState{
			Offset:  nextOffset,
			Size:    info.Size(),
			ModUnix: info.ModTime().Unix(),
		}
		if date > latestDate {
			latestDate = date
		}
		if sb.Len() >= maxBytes {
			break
		}
	}

	if sb.Len() == 0 {
		return nil, nil
	}
	return &diaryScanResult{
		Content:    sb.String(),
		State:      state,
		LatestDate: latestDate,
	}, nil
}

func diaryDateFromName(name string) string {
	date := strings.TrimPrefix(name, "diary-")
	return strings.TrimSuffix(date, ".md")
}

func (wd *WikiDreamer) diaryProcessStatePath() string {
	return filepath.Join(wd.store.Dir(), diaryProcessStateFile)
}

func (wd *WikiDreamer) loadDiaryProcessState() diaryProcessState {
	state := diaryProcessState{
		Version: 1,
		Files:   make(map[string]diaryFileState),
	}
	data, err := os.ReadFile(wd.diaryProcessStatePath())
	if err != nil {
		return state
	}
	if err := json.Unmarshal(data, &state); err != nil && wd.logger != nil {
		wd.logger.Warn("wiki-dream: diary state parse failed", "error", err)
	}
	if state.Version == 0 {
		state.Version = 1
	}
	if state.Files == nil {
		state.Files = make(map[string]diaryFileState)
	}
	return state
}

func (wd *WikiDreamer) saveDiaryProcessState(state diaryProcessState) error {
	if state.Files == nil {
		state.Files = make(map[string]diaryFileState)
	}
	state.Version = 1
	state.UpdatedAt = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal diary state: %w", err)
	}
	path := wd.diaryProcessStatePath()
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write diary state: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace diary state: %w", err)
	}
	return nil
}

func formatProcessedDiaryCapsules(capsules []processedDiaryCapsule) string {
	if len(capsules) == 0 {
		return "최근 처리 이력 없음."
	}
	var sb strings.Builder
	start := len(capsules) - processedCapsuleLimit
	if start < 0 {
		start = 0
	}
	for _, c := range capsules[start:] {
		fmt.Fprintf(&sb, "- date=%s proposed=%d created=%d updated=%d",
			c.DiaryDate, c.Proposed, c.Created, c.Updated)
		if len(c.Paths) > 0 {
			sb.WriteString(" paths=")
			sb.WriteString(strings.Join(c.Paths, ", "))
		}
		if c.At != "" {
			sb.WriteString(" at=")
			sb.WriteString(c.At)
		}
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

func appendProcessedDiaryCapsule(capsules []processedDiaryCapsule, next processedDiaryCapsule) []processedDiaryCapsule {
	if next.At == "" && next.DiaryDate == "" && next.Proposed == 0 && next.Created == 0 && next.Updated == 0 && len(next.Paths) == 0 {
		return capProcessedDiaryCapsules(capsules)
	}
	next.Paths = dedupeStringList(next.Paths, 16)
	capsules = append(capsules, next)
	return capProcessedDiaryCapsules(capsules)
}

func capProcessedDiaryCapsules(capsules []processedDiaryCapsule) []processedDiaryCapsule {
	if len(capsules) <= processedCapsuleLimit {
		return capsules
	}
	out := make([]processedDiaryCapsule, processedCapsuleLimit)
	copy(out, capsules[len(capsules)-processedCapsuleLimit:])
	return out
}

func updatePaths(updates []wikiUpdate) []string {
	paths := make([]string, 0, len(updates))
	for _, u := range updates {
		if strings.TrimSpace(u.Path) == "" {
			continue
		}
		paths = append(paths, u.Path)
	}
	return dedupeStringList(paths, 16)
}

func dedupeStringList(values []string, max int) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
		if max > 0 && len(out) >= max {
			break
		}
	}
	return out
}

func (wd *WikiDreamer) dreamProposalPath() string {
	return filepath.Join(wd.store.Dir(), dreamProposalFile)
}

func buildDreamProposalReport(scan *diaryScanResult, updates []wikiUpdate) dreamProposalReport {
	report := dreamProposalReport{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Proposed:    make([]dreamUpdatePreview, 0, len(updates)),
	}
	if scan != nil {
		report.LatestDiaryDate = scan.LatestDate
		report.DiaryBytes = len(scan.Content)
	}
	for _, update := range updates {
		report.Proposed = append(report.Proposed, dreamUpdatePreview{
			Action:      update.Action,
			Path:        update.Path,
			Title:       update.Title,
			Summary:     update.Summary,
			Category:    update.Category,
			Type:        update.Type,
			Confidence:  update.Confidence,
			Importance:  update.Importance,
			Related:     dedupeStringList(update.Related, 12),
			ContentHint: truncateDreamReportText(update.Content, 320),
		})
	}
	return report
}

func (wd *WikiDreamer) saveDreamProposalReport(report dreamProposalReport) error {
	path := wd.dreamProposalPath()
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal proposal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write proposal: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace proposal: %w", err)
	}
	return nil
}

func truncateDreamReportText(text string, maxRunes int) string {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	text = redact.String(text)
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

// flexStringList is a []string that tolerates an LLM emitting a single JSON
// string (often comma-separated) where the schema calls for an array — a common
// drift that otherwise fails the whole synthesis unmarshal and discards an
// entire dream cycle. An array is taken as-is; a string is split on ',', ';',
// and newlines (not spaces, since a tag or path may itself contain spaces).
