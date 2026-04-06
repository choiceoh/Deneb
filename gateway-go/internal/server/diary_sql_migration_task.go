// diary_sql_migration_task.go — periodic task that migrates matured diary
// entries (2+ days old) into structured SQL facts via LLM extraction.
//
// Diary files capture rich narrative context in real-time. After 2 days the
// entries have "cooled down" and can be distilled into durable, searchable
// SQL facts (importance-scored, categorized, entity-linked).
//
// Flow:
//  1. Scan memory/diary/ for diary-YYYY-MM-DD.md files dated ≥2 days ago.
//  2. Skip dates already migrated (marker file in memory/diary/.migrated/).
//  3. Send diary content to the LLM with extraction instructions.
//  4. LLM calls memory(action=set, ...) to create structured facts.
//  5. Write marker file on success to prevent re-processing.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat"
	"github.com/choiceoh/deneb/gateway-go/internal/chat/tools"
)

// diarySQLMigrationTask implements autonomous.PeriodicTask.
// Every 12 hours it checks for diary files that are ≥2 days old and have not
// yet been migrated, then asks the LLM to extract structured facts from them.
type diarySQLMigrationTask struct {
	chatHandler  *chat.Handler
	workspaceDir string
	logger       *slog.Logger
}

func (t *diarySQLMigrationTask) Name() string            { return "diary-sql-migration" }
func (t *diarySQLMigrationTask) Interval() time.Duration { return 12 * time.Hour }

// maturityDays is the minimum age (in days) before a diary file is eligible
// for SQL migration. This gives time for the narrative to settle before
// distilling it into terse facts.
const maturityDays = 2

// maxMigrationBatch limits how many diary files are processed per run to avoid
// long-running LLM sessions.
const maxMigrationBatch = 3

// diarySQLMigrationPromptTmpl is sent to the LLM with the diary content.
// The LLM is expected to call memory(action=set, ...) for each extracted fact.
const diarySQLMigrationPromptTmpl = `[시스템 다이어리 → SQL 마이그레이션]

아래는 %s 날짜의 다이어리 내용입니다. 이 서술형 기록에서 장기 보존할 가치가 있는 구조화된 팩트를 추출하여 memory(action=set, ...) 로 저장하세요.

## 추출 가이드라인

1. **중요한 결정/판단**: 왜 그런 선택을 했는지 (category=decision, importance=0.7~0.9)
2. **해결된 문제/버그**: 원인과 해결책 (category=solution, importance=0.7~0.85)
3. **사용자 선호/패턴**: 반복되는 요청이나 선호 (category=preference, importance=0.75~0.9)
4. **프로젝트 맥락**: 아키텍처 변경, 새 기능 추가 등 (category=context, importance=0.6~0.8)
5. **사용자 모델**: 사용자에 대해 새로 알게 된 것 (category=user_model, importance=0.7~0.85)

## 규칙

- 각 팩트는 한 문장으로, 구체적이고 검색 가능하게 작성
- "활동 없음" / "대기 상태" 같은 무의미한 항목은 건너뛰기
- 이미 SQL에 있을 법한 중복 팩트는 건너뛰기
- 최소 importance 0.6 이상인 것만 추출
- 팩트가 하나도 없으면 아무 것도 호출하지 않아도 됨

## 다이어리 내용

%s`

// diaryDateRe matches diary filenames like "diary-2026-03-30.md".
var diaryDateRe = regexp.MustCompile(`^diary-(\d{4}-\d{2}-\d{2})\.md$`)

func (t *diarySQLMigrationTask) Run(ctx context.Context) error {
	if t.chatHandler == nil {
		return fmt.Errorf("diary-sql-migration: chat handler not available")
	}

	diaryDir := filepath.Join(t.workspaceDir, "memory", tools.DiaryDir)

	// Find unmigrated diary files that are old enough.
	candidates, err := t.findCandidates(diaryDir)
	if err != nil {
		return fmt.Errorf("diary-sql-migration: scan candidates: %w", err)
	}
	if len(candidates) == 0 {
		return nil // nothing to do
	}

	// Cap to batch size.
	if len(candidates) > maxMigrationBatch {
		candidates = candidates[:maxMigrationBatch]
	}

	migrated := 0
	for _, c := range candidates {
		if err := t.migrateOne(ctx, diaryDir, c); err != nil {
			t.logger.Warn("diary-sql-migration: failed to migrate",
				"date", c, "error", err)
			continue
		}
		migrated++
	}

	if migrated > 0 {
		t.logger.Info("diary-sql-migration completed",
			"migrated", migrated,
			"total_candidates", len(candidates),
		)
	}
	return nil
}

// diaryCandidate holds a date string and its parsed time.
type diaryCandidate struct {
	dateStr string
	date    time.Time
}

// findCandidates returns date strings of diary files eligible for migration,
// sorted oldest-first.
func (t *diarySQLMigrationTask) findCandidates(diaryDir string) ([]string, error) {
	entries, err := os.ReadDir(diaryDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().In(tools.SeoulLoc())
	cutoff := now.AddDate(0, 0, -maturityDays)

	migratedDir := filepath.Join(diaryDir, ".migrated")

	var candidates []diaryCandidate
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := diaryDateRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		dateStr := m[1]
		date, err := time.ParseInLocation("2006-01-02", dateStr, tools.SeoulLoc())
		if err != nil {
			continue
		}
		// Must be at least maturityDays old.
		if date.After(cutoff) {
			continue
		}
		// Skip already migrated.
		markerPath := filepath.Join(migratedDir, dateStr)
		if _, err := os.Stat(markerPath); err == nil {
			continue
		}
		candidates = append(candidates, diaryCandidate{dateStr: dateStr, date: date})
	}

	// Sort oldest first so we process in chronological order.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].date.Before(candidates[j].date)
	})

	result := make([]string, len(candidates))
	for i, c := range candidates {
		result[i] = c.dateStr
	}
	return result, nil
}

// migrateOne reads a single diary file, sends it to the LLM for fact
// extraction, and writes a marker file on success.
func (t *diarySQLMigrationTask) migrateOne(ctx context.Context, diaryDir, dateStr string) error {
	diaryPath := tools.DiaryPath(t.workspaceDir, dateStr)

	content, err := os.ReadFile(diaryPath)
	if err != nil {
		return fmt.Errorf("read diary: %w", err)
	}

	trimmed := strings.TrimSpace(string(content))
	if trimmed == "" || isEmptyDiary(trimmed) {
		// Empty or trivial diary — mark as migrated and skip.
		return t.writeMarker(diaryDir, dateStr)
	}

	prompt := fmt.Sprintf(diarySQLMigrationPromptTmpl, dateStr, trimmed)

	runCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// Use SendLite: diary migration only needs the memory tool.
	// Avoids the full pipeline (191 msgs, 20 tools, 504K tokens).
	result, err := t.chatHandler.SendLite(runCtx, "", prompt,
		[]string{"memory"},
		&chat.LiteOptions{MaxTurns: 3},
	)
	if err != nil {
		return fmt.Errorf("agent turn: %w", err)
	}

	t.logger.Info("diary-sql-migration: extracted facts",
		"date", dateStr,
		"output_len", len(result.Text),
	)

	return t.writeMarker(diaryDir, dateStr)
}

// writeMarker creates the .migrated marker for a given date.
func (t *diarySQLMigrationTask) writeMarker(diaryDir, dateStr string) error {
	migratedDir := filepath.Join(diaryDir, ".migrated")
	if err := os.MkdirAll(migratedDir, 0o755); err != nil {
		return fmt.Errorf("create migrated dir: %w", err)
	}
	markerPath := filepath.Join(migratedDir, dateStr)
	return os.WriteFile(markerPath, []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
}

// isEmptyDiary returns true if the diary content contains only the date
// header and/or trivial "no activity" heartbeat entries.
func isEmptyDiary(content string) bool {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Skip date header (# YYYY-MM-DD 일지).
		if strings.HasPrefix(line, "# ") {
			continue
		}
		// Skip heartbeat section headers.
		if strings.HasPrefix(line, "### ") && strings.Contains(line, "하트비트") {
			continue
		}
		// Skip trivial content.
		if strings.Contains(line, "활동 없음") || strings.Contains(line, "대기 상태") {
			continue
		}
		// Found a non-trivial line.
		return false
	}
	return true
}
