// supernote_digest_task.go — ingest Supernote Manta handwritten notes into the
// wiki via a Google Drive folder the device auto-syncs to.
//
// The Manta exports handwritten pages as searchable PDFs (its on-device HWR
// engine embeds recognized text) into a Drive folder. This task is the pull +
// synthesis half, modeled on noti-digest (runtime/wikiwork/noti_digest_task.go)
// and the mail-poll pipeline: every cycle it lists new files in the configured
// folder, extracts their text (no OCR needed — the PDF text layer is the
// device's own recognition), and runs ONE agent turn that consolidates the
// memorable content into the wiki (project logs, commitments, people).
//
// Unlike the notification digest, a Supernote note is the USER's own
// first-party content (meeting notes, to-dos, sketches of a plan), not hostile
// third-party text — so this uses the internal-research preset (wiki + memory
// stores, no web) and does not arm the untrusted-tool gate: matching a note to
// its project benefits from mail_archive/people/graphify, exactly like the
// dreamer consolidating a diary.
//
// Not configured = safe no-op: an absent Drive credential or unset folder ID
// logs once and returns, so the task ships dormant until the operator wires it.
package wikiwork

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/autonomous"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/monitoring"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/document"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/toolpreset"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/googledrive"
	"github.com/choiceoh/deneb/gateway-go/pkg/atomicfile"
)

// Compile-time interface compliance.
var _ autonomous.PeriodicTask = (*supernoteDigestTask)(nil)

const (
	// supernoteInterval is the poll cadence. Handwritten notes are not
	// time-critical; a few hours keeps Drive API calls sparse.
	supernoteInterval = 6 * time.Hour
	// supernoteTurnTimeout mirrors the other wikiwork agent turns.
	supernoteTurnTimeout = 12 * time.Minute
	// supernoteMaxFilesPerCycle bounds how many new notes one cycle ingests;
	// the rest wait for the next cycle (cursor only advances past processed
	// files).
	supernoteMaxFilesPerCycle = 5
	// supernoteMaxTextRunesPerFile bounds one note's extracted text.
	supernoteMaxTextRunesPerFile = 8000
	// supernoteStateFile persists the modifiedTime cursor + processed IDs.
	supernoteStateFile = "supernote-digest-state.json"
	// supernoteSessionKey isolates these background turns.
	supernoteSessionKey = "supernote-digest"
	// supernoteSeenIDCap bounds the processed-ID dedup set (same-timestamp
	// guard); oldest entries drop once the cap is exceeded.
	supernoteSeenIDCap = 500
	// DriveFolderEnv names the env var holding the Drive folder ID the Manta
	// syncs into. Empty disables the task.
	DriveFolderEnv = "DENEB_SUPERNOTE_DRIVE_FOLDER_ID"
)

const (
	// SupernoteStateFile is the exported state filename.
	SupernoteStateFile = supernoteStateFile
	// SupernoteInterval is the exported poll cadence.
	SupernoteInterval = supernoteInterval
)

// driveClient is the read-only Drive surface this task needs (googledrive.Client
// satisfies it). Indirected so tests inject a fake without credentials.
type driveClient interface {
	ListFolderFiles(ctx context.Context, folderID, modifiedAfter string) ([]googledrive.File, error)
	DownloadFile(ctx context.Context, fileID string) ([]byte, error)
}

// supernoteDigestState persists ingestion progress.
type supernoteDigestState struct {
	Version int      `json:"version"`
	Cursor  string   `json:"cursor"`  // max modifiedTime consumed (RFC3339)
	SeenIDs []string `json:"seenIds"` // recently processed file IDs (same-timestamp dedup)
	Updated string   `json:"updated,omitempty"`
}

// supernoteDigestTask implements autonomous.PeriodicTask.
type supernoteDigestTask struct {
	chatHandler  chatport.SyncRunner
	wikiStore    *wiki.Store
	activity     *monitoring.ActivityTracker
	logger       *slog.Logger
	statePath    string
	folderID     string
	workspaceDir string
	loggedOff    bool // one-shot "not configured" log guard

	// newDrive builds the Drive client; indirected for tests. extractText
	// pulls readable text from a downloaded file; indirected so tests avoid
	// the pdftotext CLI dependency.
	newDrive    func() (driveClient, error)
	extractText func(ctx context.Context, data []byte, name, mime string) (string, error)
}

// SupernoteDigestTask ingests Supernote notes from Drive into the wiki.
type SupernoteDigestTask = supernoteDigestTask

// NewSupernoteDigestTask constructs the Supernote ingestion worker. folderID
// (from DENEB_SUPERNOTE_DRIVE_FOLDER_ID) empty ⇒ the task no-ops.
func NewSupernoteDigestTask(
	chatHandler chatport.SyncRunner,
	wikiStore *wiki.Store,
	activity *monitoring.ActivityTracker,
	logger *slog.Logger,
	statePath string,
	folderID string,
	workspaceDir string,
) *SupernoteDigestTask {
	return &supernoteDigestTask{
		chatHandler:  chatHandler,
		wikiStore:    wikiStore,
		activity:     activity,
		logger:       logger,
		statePath:    statePath,
		folderID:     strings.TrimSpace(folderID),
		workspaceDir: workspaceDir,
		newDrive:     func() (driveClient, error) { return googledrive.NewClient() },
		extractText: func(ctx context.Context, data []byte, name, mime string) (string, error) {
			text, _, err := document.ExtractText(ctx, data, name, mime)
			return text, err
		},
	}
}

// Name returns the component's stable scheduler name.
func (t *supernoteDigestTask) Name() string { return "supernote-digest" }

// Interval returns the component's scheduling cadence.
func (t *supernoteDigestTask) Interval() time.Duration { return supernoteInterval }

// Run executes one ingestion cycle.
func (t *supernoteDigestTask) Run(ctx context.Context) error {
	if t.chatHandler == nil || !t.chatHandler.ChatReady() || t.wikiStore == nil {
		return fmt.Errorf("supernote-digest: chat handler or wiki store not available")
	}
	if t.folderID == "" {
		if !t.loggedOff {
			t.logger.Info("supernote-digest: no Drive folder configured (" + DriveFolderEnv + "), idle")
			t.loggedOff = true
		}
		return nil
	}
	if t.activity != nil {
		idle := time.Duration(time.Now().UnixMilli()-t.activity.LastActivityAt()) * time.Millisecond
		if idle < 5*time.Minute {
			t.logger.Info("supernote-digest: skipped, user active", "idle", idle.Round(time.Second))
			return nil
		}
	}

	client, err := t.newDrive()
	if err != nil {
		if !t.loggedOff {
			t.logger.Info("supernote-digest: Drive credentials not available, idle", "error", err)
			t.loggedOff = true
		}
		return nil
	}

	state := t.loadState()
	files, err := client.ListFolderFiles(ctx, t.folderID, state.Cursor)
	if err != nil {
		return fmt.Errorf("supernote-digest: Drive list failed: %w", err)
	}
	fresh := t.selectFresh(files, state)
	if len(fresh) == 0 {
		t.logger.Debug("supernote-digest: no new notes")
		return nil
	}

	runCtx, cancel := context.WithTimeout(ctx, supernoteTurnTimeout)
	defer cancel()

	notes := make([]ingestedNote, 0, len(fresh))
	for _, f := range fresh {
		data, derr := client.DownloadFile(runCtx, f.ID)
		if derr != nil {
			t.logger.Warn("supernote-digest: download failed, skipping", "file", f.Name, "error", derr)
			continue
		}
		text, xerr := t.extractText(runCtx, data, f.Name, f.MimeType)
		if xerr != nil || strings.TrimSpace(text) == "" {
			t.logger.Warn("supernote-digest: no extractable text, skipping", "file", f.Name, "error", xerr)
			continue
		}
		notes = append(notes, ingestedNote{name: f.Name, modified: f.ModifiedTime, text: truncateRunesNote(text, supernoteMaxTextRunesPerFile)})
	}
	if len(notes) == 0 {
		// Nothing usable, but advance the cursor past these files so a batch of
		// unreadable notes doesn't re-download every cycle.
		t.commitCursor(state, fresh)
		if serr := t.saveState(state); serr != nil {
			t.logger.Warn("supernote-digest: failed to persist state", "error", serr)
		}
		return nil
	}

	_, serr := t.chatHandler.RunSync(runCtx, chatport.SyncRequest{
		SessionKey:         supernoteSessionKey,
		Message:            t.buildPrompt(notes),
		ToolPreset:         string(toolpreset.PresetWikiResearch),
		MaxHistoryTokens:   20_000,
		EphemeralUser:      true,
		EphemeralAssistant: true,
		SkipRecall:         true,
	})
	if serr != nil {
		// Keep the cursor so the batch is retried next cycle.
		return fmt.Errorf("supernote-digest: agent turn failed: %w", serr)
	}

	t.commitCursor(state, fresh)
	if err := t.saveState(state); err != nil {
		t.logger.Warn("supernote-digest: failed to persist state", "error", err)
	}
	t.wikiStore.SnapshotGit(ctx, "supernote-digest: handwritten notes consolidation")
	t.logger.Info("supernote-digest cycle completed", "notes", len(notes), "listed", len(fresh))
	return nil
}

// ingestedNote is one downloaded, text-extracted note.
type ingestedNote struct {
	name     string
	modified string
	text     string
}

// selectFresh returns files not already processed (by ID) and readable-looking,
// oldest first, capped at supernoteMaxFilesPerCycle. Only PDF/text kinds are
// considered — the Manta's HWR export is a searchable PDF; a raw .note file
// carries no extractable text and is skipped.
func (t *supernoteDigestTask) selectFresh(files []googledrive.File, state *supernoteDigestState) []googledrive.File {
	seen := make(map[string]struct{}, len(state.SeenIDs))
	for _, id := range state.SeenIDs {
		seen[id] = struct{}{}
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].ModifiedTime < files[j].ModifiedTime })
	var out []googledrive.File
	for _, f := range files {
		if _, ok := seen[f.ID]; ok {
			continue
		}
		if !isIngestibleNote(f.Name, f.MimeType) {
			continue
		}
		out = append(out, f)
		if len(out) >= supernoteMaxFilesPerCycle {
			break
		}
	}
	return out
}

// commitCursor advances the modifiedTime cursor to the newest processed file
// and records the processed IDs (bounded) for same-timestamp dedup.
func (t *supernoteDigestTask) commitCursor(state *supernoteDigestState, processed []googledrive.File) {
	for _, f := range processed {
		if f.ModifiedTime > state.Cursor {
			state.Cursor = f.ModifiedTime
		}
		state.SeenIDs = append(state.SeenIDs, f.ID)
	}
	if len(state.SeenIDs) > supernoteSeenIDCap {
		state.SeenIDs = state.SeenIDs[len(state.SeenIDs)-supernoteSeenIDCap:]
	}
}

// isIngestibleNote reports whether a Drive file carries extractable note text.
func isIngestibleNote(name, mime string) bool {
	lower := strings.ToLower(name)
	if strings.Contains(mime, "pdf") || strings.HasSuffix(lower, ".pdf") {
		return true
	}
	for _, ext := range []string{".txt", ".md"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return strings.HasPrefix(mime, "text/")
}

func truncateRunesNote(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + " (이하 생략)"
}

// buildPrompt renders the batch of notes plus the consolidation rules.
func (t *supernoteDigestTask) buildPrompt(notes []ingestedNote) string {
	var b strings.Builder
	b.WriteString("[자율 슈퍼노트 다이제스트 — 백그라운드 메모리 유지보수 턴]\n\n")
	b.WriteString(fmt.Sprintf("아래는 사용자가 슈퍼노트(태블릿)에 손으로 쓴 노트 %d건입니다 (기기 필기인식으로 텍스트화됨, 시간순).\n", len(notes)))
	b.WriteString(wiki.WikiBriefSection(wiki.LoadWikiBrief(t.workspaceDir)))
	for i, n := range notes {
		b.WriteString(fmt.Sprintf("\n=== 노트 %d: %s (%s) ===\n", i+1, n.name, n.modified))
		b.WriteString(n.text)
		b.WriteString("\n")
	}
	b.WriteString(`

이 노트들은 사용자 본인이 직접 쓴 1차 자료입니다 (회의 필기·아이디어·할 일 메모 등). 필기인식 텍스트라 오탈자가 있을 수 있으니 문맥으로 보정해 이해하세요. 절차:

1. 각 노트에서 **기억할 가치가 있는 업무 사실**을 추립니다: 회의 결정·논의, 프로젝트 진행/이슈, 거래처·인물 정보, 할 일·후속조치, 업무 지식.
2. 어느 프로젝트/인물에 속하는지 내부 소스(wiki read·mail_archive·contacts·graphify)로 확인한 뒤 반영합니다 — 불확실하면 추측하지 말고 넘어갑니다.
3. 반영 방법:
   - 회의 필기·진행 소식 → 해당 프로젝트 로그.md에 '## [YYYY-MM-DD] <op> | <주제>' 섹션 append (op: 회의/메모/결정/이슈 등)
   - 할 일·후속조치 → 해당 프로젝트 로그에 남기되 이미 있는 항목과 중복 금지
   - 인물/거래처의 지속적 사실 → 기존 인물 페이지 update
   - 업무 도메인 지식 → 관련 업무/프로젝트 페이지에 통합
   - **새 대표페이지를 만들거나 대표페이지 본문을 직접 재작성하지 마세요** — 로그 append와 기존 페이지 update 위주
4. 출처 표기: 각 반영 항목에 근거를 병기하세요 (예: "슈퍼노트 <노트명>, YYYY-MM-DD").
5. 손글씨 오인식으로 의미가 불분명한 부분은 확정 사실로 쓰지 말고, 정말 남길 게 없으면 조용히 종료하세요.

이것은 사용자에게 보내는 응답이 아니라 백그라운드 메모리 유지보수 작업입니다. 사용자에게 알리지 마세요.`)
	return b.String()
}

func (t *supernoteDigestTask) loadState() *supernoteDigestState {
	st := &supernoteDigestState{Version: 1}
	data, err := os.ReadFile(t.statePath)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, st); err != nil {
		t.logger.Warn("supernote-digest: corrupt state, starting fresh", "error", err)
		return &supernoteDigestState{Version: 1}
	}
	return st
}

func (t *supernoteDigestTask) saveState(st *supernoteDigestState) error {
	st.Updated = time.Now().Format(time.RFC3339)
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.WriteFile(t.statePath, data, &atomicfile.Options{Perm: 0o600})
}
