// dreamer.go — WikiDreamer: implements autonomous.Dreamer for wiki-based
// memory consolidation. Instead of SQL-based fact verification/merging,
// it scans diary entries and synthesizes them into wiki pages via LLM.
package wiki

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// Dreaming configuration.
const (
	wikiDreamTurnThreshold = 50
	wikiDreamTimeIntervalH = 8
	wikiDreamTimeout       = 10 * time.Minute
	wikiDreamMaxTokens     = 4096
	diaryProcessStateFile  = ".diary-process-state.json"
	dreamProposalFile      = ".dream-last-proposal.json"
	processedCapsuleLimit  = 12
)

// Compile-time interface compliance.
var _ autonomous.Dreamer = (*WikiDreamer)(nil)

type diaryScanResult struct {
	Content    string
	State      diaryProcessState
	LatestDate string
}

type diaryProcessState struct {
	Version   int                       `json:"version"`
	Files     map[string]diaryFileState `json:"files"`
	Recent    []processedDiaryCapsule   `json:"recent,omitempty"`
	UpdatedAt string                    `json:"updatedAt,omitempty"`
}

type diaryFileState struct {
	Offset  int64 `json:"offset"`
	Size    int64 `json:"size,omitempty"`
	ModUnix int64 `json:"modUnix,omitempty"`
}

type processedDiaryCapsule struct {
	At        string   `json:"at"`
	DiaryDate string   `json:"diaryDate,omitempty"`
	Proposed  int      `json:"proposed"`
	Created   int      `json:"created"`
	Updated   int      `json:"updated"`
	Paths     []string `json:"paths,omitempty"`
}

type dreamProposalReport struct {
	GeneratedAt     string               `json:"generatedAt"`
	LatestDiaryDate string               `json:"latestDiaryDate,omitempty"`
	DiaryBytes      int                  `json:"diaryBytes"`
	Proposed        []dreamUpdatePreview `json:"proposed"`
	Applied         dreamApplySummary    `json:"applied,omitempty"`
	PhaseErrors     []string             `json:"phaseErrors,omitempty"`
	DurationMs      int64                `json:"durationMs,omitempty"`
}

type dreamUpdatePreview struct {
	Action      string   `json:"action"`
	Path        string   `json:"path"`
	Title       string   `json:"title,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Category    string   `json:"category,omitempty"`
	Type        string   `json:"type,omitempty"`
	Confidence  string   `json:"confidence,omitempty"`
	Importance  float64  `json:"importance,omitempty"`
	Related     []string `json:"related,omitempty"`
	ContentHint string   `json:"contentHint,omitempty"`
}

type dreamApplySummary struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// WikiDreamer implements autonomous.Dreamer for wiki-based knowledge consolidation.
// Phases:
//  1. Scan unprocessed diary entries
//  2. LLM synthesis: identify which wiki pages to create/update
//  3. Apply page updates
//  4. Rebuild index
type WikiDreamer struct {
	store  *Store
	config Config
	client *llm.Client
	model  string
	logger *slog.Logger

	turnCount int
	lastDream time.Time

	// polarisContextFn optionally returns formatted recent polaris compression
	// summaries to inject into the synthesis prompt as a higher-density fact
	// source alongside raw diary entries. Wired by the chat pipeline; the wiki
	// package does not import polaris directly.
	polarisContextFn func() string
}

// NewWikiDreamer creates a new wiki dreamer.
func NewWikiDreamer(store *Store, client *llm.Client, model string, cfg Config, logger *slog.Logger) *WikiDreamer {
	return &WikiDreamer{
		store:  store,
		config: cfg,
		client: client,
		model:  model,
		logger: logger,
	}
}

// IncrementTurn records a conversation turn for threshold tracking.
func (wd *WikiDreamer) IncrementTurn(_ context.Context) {
	wd.turnCount++
}

// SetPolarisContextFn wires a closure that returns formatted recent polaris
// compression summaries. nil-safe; passing nil disables polaris injection.
func (wd *WikiDreamer) SetPolarisContextFn(fn func() string) {
	wd.polarisContextFn = fn
}

// ShouldDream checks if dreaming conditions are met.
func (wd *WikiDreamer) ShouldDream(_ context.Context) bool {
	if wd.turnCount >= wikiDreamTurnThreshold {
		wd.logger.Info("wiki-dream: turn threshold reached", "turns", wd.turnCount)
		return true
	}
	if !wd.lastDream.IsZero() && time.Since(wd.lastDream).Hours() >= float64(wikiDreamTimeIntervalH) {
		wd.logger.Info("wiki-dream: time threshold reached", "elapsed", time.Since(wd.lastDream).Round(time.Minute))
		return true
	}
	return false
}

// RunDream executes the wiki consolidation cycle.
func (wd *WikiDreamer) RunDream(ctx context.Context) (*autonomous.DreamReport, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, wikiDreamTimeout)
	defer cancel()

	report := &autonomous.DreamReport{}
	var phaseErrors []string

	// Phase 1: Scan unprocessed diary entries.
	scan, err := wd.scanDiaries(ctx)
	if err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("diary-scan: %v", err))
	}
	diaryContent := ""
	if scan != nil {
		diaryContent = scan.Content
	}

	if diaryContent == "" {
		wd.logger.Info("wiki-dream: no new diary entries to process")
		wd.resetCounters()
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	// Phase 2: LLM synthesis — determine which wiki pages to update.
	if wd.client == nil {
		phaseErrors = append(phaseErrors, "synthesis: LLM client not available")
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}

	updates, err := wd.synthesize(ctx, diaryContent, scan.State)
	if err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("synthesis: %v", err))
		report.PhaseErrors = phaseErrors
		report.DurationMs = time.Since(start).Milliseconds()
		return report, nil
	}
	report.WikiUpdatesProposed = len(updates)
	proposal := buildDreamProposalReport(scan, updates)
	proposalPath := wd.dreamProposalPath()
	report.WikiProposalPath = proposalPath
	if err := wd.saveDreamProposalReport(proposal); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("proposal-save: %v", err))
	}

	// Phase 3: Apply page updates.
	created, updated, oversized := wd.applyUpdates(ctx, updates)
	report.WikiPagesCreated = created
	report.WikiPagesUpdated = updated
	if len(oversized) > 0 {
		phaseErrors = append(phaseErrors, fmt.Sprintf("oversized pages: %s", strings.Join(oversized, ", ")))
	}

	// Phase 4: Rebuild index.
	if err := wd.rebuildIndex(); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-rebuild: %v", err))
	}

	// Phase 5: Verify existing pages (duplicate detection + misclassification).
	findings := wd.verifyPages(ctx)
	if len(findings) > 0 {
		for _, f := range findings {
			report.VerifyFindings = append(report.VerifyFindings, f.Detail)
		}
		wd.logger.Info("wiki-dream: verification findings", "count", len(findings))
	}

	// Phase 6: Project the wiki into a graphify-compatible graph.json so the
	// `graphify` tool can query, traverse, and cluster wiki concepts. No LLM
	// call here — synthesize() already curates Related[], we just serialize.
	if outDir, ok := graphSnapshotOutDir(); ok {
		snap, snapErr := BuildGraphSnapshot(ctx, wd.store, outDir, true)
		if snapErr != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("graph-snapshot: %v", snapErr))
		} else {
			report.WikiGraphNodes = snap.Nodes
			report.WikiGraphEdges = snap.Edges
			report.WikiGraphClustered = snap.Clustered
			if snap.ClusterError != "" {
				wd.logger.Warn("wiki-dream: graph cluster step failed",
					"error", snap.ClusterError)
			}
			wd.logger.Info("wiki-dream: graph snapshot",
				"nodes", snap.Nodes, "edges", snap.Edges,
				"clustered", snap.Clustered, "out", snap.GraphPath)
		}
	}

	// Persist diary high-water state only after synthesis/apply/index work has
	// completed. LastProcessed remains for display and legacy migration, but
	// scanDiaries uses per-file offsets as the primary source of truth.
	idx := wd.store.Index()
	if scan != nil && scan.LatestDate != "" {
		idx.LastProcessed = scan.LatestDate
	} else {
		idx.LastProcessed = time.Now().Format("2006-01-02")
	}
	indexPath := filepath.Join(wd.store.Dir(), "index.md")
	if err := idx.Save(indexPath); err != nil {
		phaseErrors = append(phaseErrors, fmt.Sprintf("index-save: %v", err))
	}
	if scan != nil {
		scan.State.Recent = appendProcessedDiaryCapsule(scan.State.Recent, processedDiaryCapsule{
			At:        time.Now().Format(time.RFC3339),
			DiaryDate: scan.LatestDate,
			Proposed:  len(updates),
			Created:   created,
			Updated:   updated,
			Paths:     updatePaths(updates),
		})
		if err := wd.saveDiaryProcessState(scan.State); err != nil {
			phaseErrors = append(phaseErrors, fmt.Sprintf("diary-state-save: %v", err))
		}
	}

	wd.resetCounters()
	report.PhaseErrors = phaseErrors
	report.DurationMs = time.Since(start).Milliseconds()
	proposal.Applied = dreamApplySummary{Created: created, Updated: updated}
	proposal.PhaseErrors = phaseErrors
	proposal.DurationMs = report.DurationMs
	if err := wd.saveDreamProposalReport(proposal); err != nil {
		report.PhaseErrors = append(report.PhaseErrors, fmt.Sprintf("proposal-save-final: %v", err))
	}

	wd.logger.Info("wiki-dream: cycle complete",
		"created", created, "updated", updated,
		"duration", time.Since(start).Round(time.Millisecond))

	return report, nil
}

// scanDiaries reads diary bytes that have not yet been consolidated. The
// primary cursor is a per-file byte offset; index.LastProcessed is only a
// legacy migration hint for old diaries that predate the cursor file.
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
			chunk = chunk[:remaining]
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

// wikiUpdate represents a single page update instruction from the LLM.
type wikiUpdate struct {
	Action     string   `json:"action"` // "create" or "update"
	Path       string   `json:"path"`   // e.g., "기술/dgx-spark.md"
	Title      string   `json:"title"`
	ID         string   `json:"id"`      // short kebab-case identifier (e.g., "dgx-spark")
	Summary    string   `json:"summary"` // one-line description (~80 chars)
	Category   string   `json:"category"`
	Tags       []string `json:"tags"`
	Related    []string `json:"related"` // existing page paths semantically related
	Content    string   `json:"content"` // markdown body or section to append
	Importance float64  `json:"importance"`
	Type       string   `json:"type"`       // concept, entity, source, comparison, log
	Confidence string   `json:"confidence"` // high, medium, low
	Due        string   `json:"due"`        // YYYY-MM-DD upcoming deadline (거래 category)
}

// synthesize calls the LLM to determine which wiki pages should be updated.
func (wd *WikiDreamer) synthesize(ctx context.Context, diaryContent string, state diaryProcessState) ([]wikiUpdate, error) {
	// Build existing wiki context.
	idx := wd.store.Index()
	indexContent := idx.Render()
	processedHistory := formatProcessedDiaryCapsules(state.Recent)

	polarisSection := ""
	if wd.polarisContextFn != nil {
		if ctx := wd.polarisContextFn(); ctx != "" {
			polarisSection = "\n## 최근 Polaris 압축 요약 (사전 추출된 사실)\n" + ctx + "\n"
		}
	}

	prompt := fmt.Sprintf(`당신은 위키 지식베이스 관리자입니다. 아래 일지 내용을 분석하여 위키 페이지를 생성하거나 업데이트할 지시사항을 JSON 배열로 반환하세요.

## 현재 위키 인덱스
%s

## 최근 처리 이력
%s
%s
## 새 일지 내용
%s

## 규칙
- 일시적인 내용(인사, 잡담)은 무시
- 중요한 결정, 새로운 사실, 인물 정보, 프로젝트 진행 등만 위키에 반영
- 기존 페이지가 있으면 action:"update", 없으면 action:"create"
- 최근 처리 이력에 이미 반영된 주제/경로는 새 사실이 추가된 경우에만 update하고, 같은 내용을 반복 생성하지 마라
- 카테고리: 사람, 프로젝트, 거래, 기술, 업무, 결정, 선호
- 거래 카테고리: 거래처·금액·납기가 걸린 건별 트랜잭션. 가장 임박한 결제기한/마감일은 frontmatter의 due 필드(YYYY-MM-DD)에 기록
- content는 마크다운 형식. create 시 전체 본문, update 시 추가할 섹션/내용
- importance: 0.5(일반) ~ 0.9(핵심 결정)
- type: 페이지 유형 — concept(개념), entity(인물/조직), source(출처), comparison(비교), log(이력)
- confidence: 정보 신뢰도 — high(검증됨), medium(합리적 추론), low(불확실)
- due: 거래의 임박한 결제기한·마감일 (YYYY-MM-DD). 거래 카테고리에서만 사용, 없으면 생략
- id: 짧은 kebab-case 식별자 (예: "dgx-spark", "gemma4-switch", "peter-kim")
- summary: 한 줄 요약 (~80자, 한국어)
- related: 의미적으로 관련된 기존 위키 페이지 경로 목록 (인덱스에서 선택)
- 업데이트가 불필요하면 빈 배열 [] 반환

JSON 배열만 반환하세요. 다른 텍스트 없이.`, indexContent, processedHistory, polarisSection, diaryContent)

	systemJSON, _ := json.Marshal("You are a wiki knowledge base maintainer. Respond only with a JSON array.")
	resp, err := wd.client.Complete(ctx, llm.ChatRequest{
		Model:     wd.model,
		System:    systemJSON,
		Messages:  []llm.Message{llm.NewTextMessage("user", prompt)},
		MaxTokens: wikiDreamMaxTokens,
	})
	if err != nil {
		return nil, fmt.Errorf("LLM call: %w", err)
	}

	// Extract JSON from response.
	text := resp
	text = strings.TrimSpace(text)

	// Strip markdown code fences if present.
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text[3:], "\n"); idx >= 0 {
			text = text[3+idx+1:]
		}
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}

	var updates []wikiUpdate
	if err := json.Unmarshal([]byte(text), &updates); err != nil {
		return nil, fmt.Errorf("parse LLM response: %w (raw: %.200s)", err, text)
	}

	// Defense in depth: even if Site 1 (transcript) redacted raw tool output,
	// the LLM may still paraphrase or quote a secret into its wiki synthesis
	// ("the user's API key starts with sk-proj…"). Redact every free-text
	// field on the proposed updates before they flow into the store.
	for i := range updates {
		updates[i].Title = redact.String(updates[i].Title)
		updates[i].Summary = redact.String(updates[i].Summary)
		updates[i].Content = redact.String(updates[i].Content)
	}

	return updates, nil
}

// applyUpdates creates or updates wiki pages based on LLM instructions.
// Returns (created, updated) counts and paths of oversized pages.
func (wd *WikiDreamer) applyUpdates(_ context.Context, updates []wikiUpdate) (created, updated int, oversized []string) {
	maxBytes := wd.config.MaxPageBytes

	for _, u := range updates {
		if u.Path == "" || u.Title == "" {
			continue
		}
		if !strings.HasSuffix(u.Path, ".md") {
			u.Path += ".md"
		}
		// Validate category; remap invalid ones to "업무" as fallback.
		if u.Category != "" && !ValidateCategory(u.Category) {
			wd.logger.Warn("wiki-dream: invalid category, remapping to 업무",
				"category", u.Category, "path", u.Path)
			u.Category = "업무"
			// Fix path prefix to match corrected category.
			parts := strings.SplitN(u.Path, "/", 2)
			if len(parts) == 2 {
				u.Path = u.Category + "/" + parts[1]
			}
		}

		// Duplicate prevention: if creating, check for existing similar pages.
		if u.Action == "create" {
			if existing := wd.findExistingPage(u); existing != "" {
				wd.logger.Info("wiki-dream: duplicate detected, converting to update",
					"proposed", u.Path, "existing", existing)
				u.Action = "update"
				u.Path = existing
			}
		}

		switch u.Action {
		case "create":
			page := NewPage(u.Title, u.Category, u.Tags)
			if u.Importance > 0 {
				page.Meta.Importance = u.Importance
			}
			if u.ID != "" {
				page.Meta.ID = u.ID
			}
			if u.Summary != "" {
				page.Meta.Summary = u.Summary
			}
			if len(u.Related) > 0 {
				page.Meta.Related = u.Related
			}
			if u.Type != "" {
				page.Meta.Type = u.Type
			}
			if u.Confidence != "" {
				page.Meta.Confidence = u.Confidence
			}
			if u.Due != "" {
				page.Meta.Due = u.Due
			}
			if u.Content != "" {
				page.Body = u.Content
			} else {
				page.Body = fmt.Sprintf("# %s\n\n## 요약\n\n\n## 핵심 사실\n\n\n## 변경 이력\n- %s: 페이지 생성 (dreaming)\n",
					u.Title, time.Now().Format("2006-01-02"))
			}
			// Append a related-docs section if related pages are provided.
			if len(u.Related) > 0 {
				page.Body += "\n\n## 관련 문서\n"
				for _, r := range u.Related {
					page.Body += fmt.Sprintf("- [[%s]]\n", r)
				}
			}
			if err := wd.store.WritePage(u.Path, page); err != nil {
				wd.logger.Warn("wiki-dream: create page failed", "path", u.Path, "error", err)
				continue
			}
			created++

		case "update":
			existing, err := wd.store.ReadPage(u.Path)
			if err != nil {
				// Page doesn't exist — create it instead.
				page := NewPage(u.Title, u.Category, u.Tags)
				if u.Importance > 0 {
					page.Meta.Importance = u.Importance
				}
				if u.ID != "" {
					page.Meta.ID = u.ID
				}
				if u.Summary != "" {
					page.Meta.Summary = u.Summary
				}
				if len(u.Related) > 0 {
					page.Meta.Related = u.Related
				}
				if u.Type != "" {
					page.Meta.Type = u.Type
				}
				if u.Confidence != "" {
					page.Meta.Confidence = u.Confidence
				}
				if u.Due != "" {
					page.Meta.Due = u.Due
				}
				page.Body = u.Content
				if err := wd.store.WritePage(u.Path, page); err != nil {
					wd.logger.Warn("wiki-dream: create-on-update failed", "path", u.Path, "error", err)
					continue
				}
				created++
				continue
			}

			// Append content to existing page.
			if u.Content != "" {
				existing.Body += "\n\n" + u.Content
			}
			if len(u.Tags) > 0 {
				existing.Meta.Tags = mergeTags(existing.Meta.Tags, u.Tags)
			}
			if u.Importance > existing.Meta.Importance {
				existing.Meta.Importance = u.Importance
			}
			if u.ID != "" {
				existing.Meta.ID = u.ID
			}
			if u.Summary != "" {
				existing.Meta.Summary = u.Summary
			}
			if len(u.Related) > 0 {
				existing.Meta.Related = mergeRelated(existing.Meta.Related, u.Related)
			}
			if u.Type != "" {
				existing.Meta.Type = u.Type
			}
			if u.Confidence != "" {
				existing.Meta.Confidence = u.Confidence
			}
			if u.Due != "" {
				existing.Meta.Due = u.Due
			}
			existing.Meta.Updated = time.Now().Format("2006-01-02")

			if err := wd.store.WritePage(u.Path, existing); err != nil {
				wd.logger.Warn("wiki-dream: update page failed", "path", u.Path, "error", err)
				continue
			}
			updated++
		}

		// Check page size and split if needed.
		if maxBytes > 0 {
			abs := filepath.Join(wd.store.Dir(), u.Path)
			if info, err := os.Stat(abs); err == nil && info.Size() > int64(maxBytes) {
				subPaths, splitErr := wd.store.SplitPage(u.Path, maxBytes)
				if splitErr != nil {
					wd.logger.Warn("wiki-dream: split failed",
						"path", u.Path, "error", splitErr)
					oversized = append(oversized, u.Path)
				} else if len(subPaths) > 0 {
					wd.logger.Info("wiki-dream: page split",
						"path", u.Path, "subPages", len(subPaths))
					created += len(subPaths)
				} else {
					wd.logger.Warn("wiki-dream: page oversized but cannot split",
						"path", u.Path, "size", info.Size())
					oversized = append(oversized, u.Path)
				}
			}
		}
	}

	return created, updated, oversized
}

// rebuildIndex scans all wiki pages and rebuilds the master index.
func (wd *WikiDreamer) rebuildIndex() error {
	pages, err := wd.store.ListPages("")
	if err != nil {
		return fmt.Errorf("list pages: %w", err)
	}

	idx := wd.store.Index()
	// Preserve LastProcessed from the old index.
	lastProcessed := idx.LastProcessed

	newIdx := NewIndex()
	newIdx.LastProcessed = lastProcessed

	for _, relPath := range pages {
		page, err := wd.store.ReadPage(relPath)
		if err != nil {
			continue
		}
		newIdx.UpdateEntry(relPath, page)
	}

	wd.store.mu.Lock()
	wd.store.index = newIdx
	err = newIdx.Save(filepath.Join(wd.store.Dir(), "index.md"))
	wd.store.mu.Unlock()

	return err
}

// findExistingPage checks if a similar page already exists by ID match,
// slug prefix match, or FTS title search. Returns the existing path or "".
func (wd *WikiDreamer) findExistingPage(u wikiUpdate) string {
	idx := wd.store.Index()

	// 1. Exact ID match in the same category.
	if u.ID != "" {
		for path, entry := range idx.Entries {
			if entry.ID == u.ID {
				return path
			}
		}
	}

	// 2. Slug prefix match: normalize both and compare.
	proposedSlug := normalizeSlug(u.Path)
	for path := range idx.Entries {
		if normalizeSlug(path) == proposedSlug {
			return path
		}
	}

	// 3. FTS title search: if a result in the same category scores well.
	if u.Title != "" && wd.store.fts != nil {
		results, err := wd.store.fts.search(context.Background(), u.Title, 3)
		if err == nil {
			for _, r := range results {
				if r.Score < 0.6 {
					continue
				}
				// Same category check.
				if u.Category != "" && strings.HasPrefix(r.Path, u.Category+"/") {
					return r.Path
				}
			}
		}
	}

	return ""
}

// normalizeSlug reduces a wiki path to a comparable slug form.
// "사람/에코프로-담당자---석문호,-표과장.md" -> "사람/에코프로담당자석문호표과장"
func normalizeSlug(path string) string {
	path = strings.TrimSuffix(path, ".md")
	path = strings.ToLower(path)
	var sb strings.Builder
	for _, r := range path {
		if r == '/' {
			sb.WriteRune(r)
		} else if r == '-' || r == '_' || r == ',' || r == ' ' || r == '(' || r == ')' {
			continue
		} else {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func (wd *WikiDreamer) resetCounters() {
	wd.turnCount = 0
	wd.lastDream = time.Now()
}

// mergeTags merges two tag lists, deduplicating.
func mergeTags(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, t := range existing {
		seen[t] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, t := range added {
		if _, ok := seen[t]; !ok {
			result = append(result, t)
			seen[t] = struct{}{}
		}
	}
	return result
}

// mergeRelated merges two related-page lists, deduplicating (union).
func mergeRelated(existing, added []string) []string {
	seen := map[string]struct{}{}
	for _, r := range existing {
		seen[r] = struct{}{}
	}
	result := append([]string{}, existing...)
	for _, r := range added {
		if _, ok := seen[r]; !ok {
			result = append(result, r)
			seen[r] = struct{}{}
		}
	}
	return result
}
