package wiki

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// AppendDiary appends a timestamped entry to today's diary file and updates
// the in-memory diary search index so the new entry is immediately recallable.
// Safe to call from any goroutine.
//
// Callers that go through the package-level AppendDiaryTo (mailanalysis,
// morning_letter, etc.) bypass this indexing — their entries will only be
// searchable after the next gateway restart, when rebuildFromDir picks them
// up. Prefer Store.AppendDiary whenever a Store handle is available.
func (s *Store) AppendDiary(content string) error {
	if err := AppendDiaryTo(s.diaryDir, content); err != nil {
		return err
	}
	if s.diaryFTS != nil && content != "" {
		// Recreate the same (file, header, redacted-content, timestamp)
		// AppendDiaryTo just persisted, then push it into the index. Using
		// time.Now() once here can drift a microsecond from AppendDiaryTo,
		// but both round to the same HH:MM doc ID so the index is correct.
		now := time.Now()
		file := "diary-" + now.Format("2006-01-02") + ".md"
		header := now.Format("15:04")
		s.diaryFTS.upsertEntry(file, header, redact.String(content), now.UnixMilli())
	}
	return nil
}

// SearchDiary runs a full-text query over indexed diary entries, returning
// recency-weighted hits sorted best-first. Returns nil if no diary store is
// configured or the query is empty.
func (s *Store) SearchDiary(ctx context.Context, query string, limit int) ([]DiaryHit, error) {
	if s.diaryFTS == nil {
		return nil, nil
	}
	return s.diaryFTS.search(ctx, query, limit)
}

// RecentDiaryEntries returns the N most recent diary entries regardless of
// any query. Used as a fallback when the user's recall cue has no specific
// signal terms.
func (s *Store) RecentDiaryEntries(limit int) []DiaryHit {
	if s.diaryFTS == nil {
		return nil
	}
	return s.diaryFTS.recentEntries(limit)
}

// AppendDiaryTo appends a timestamped entry to today's diary file in the given directory.
// Standalone function usable without a Store instance.
//
// Diary content is the main input fed to the Wiki Dreamer, so any secret that
// makes it in here will later be paraphrased into synthesized wiki pages.
// Redacting at the write boundary closes that leak path at its source.
func AppendDiaryTo(diaryDir, content string) error {
	if content == "" || diaryDir == "" {
		return nil
	}
	content = redact.String(content)
	if err := os.MkdirAll(diaryDir, 0o755); err != nil {
		return fmt.Errorf("diary mkdir: %w", err)
	}
	now := time.Now()
	path := filepath.Join(diaryDir, "diary-"+now.Format("2006-01-02")+".md")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("diary open: %w", err)
	}
	defer f.Close()

	entry := fmt.Sprintf("\n## %s\n\n%s\n", now.Format("15:04"), content)
	_, err = f.WriteString(entry)
	return err
}

const (
	// wikiLogMaxBytes caps log.md before rotation. The audit log is pure
	// O_APPEND on every WritePage/DeletePage/MergePage/MarkSuperseded, so it
	// grows without bound; mirror the cron run log's 2MB ceiling.
	wikiLogMaxBytes = 2_000_000
	// wikiLogKeepBytes is the target size of the rotated tail. Keeping a
	// fraction of the cap (not the cap itself) means rotation is rare —
	// roughly once per (cap-keep) bytes — rather than on nearly every append
	// once the file first crosses the cap.
	wikiLogKeepBytes = 1_000_000
)

// appendLog appends a timestamped operation entry to log.md in the wiki root.
// Tracks all wiki mutations for temporal awareness (Karpathy wiki concept).
//
// The details string often echoes page titles or user-provided content, so it
// is redacted before persistence for the same reason writePageFile redacts the
// page body.
//
// log.md is size-capped: once it exceeds wikiLogMaxBytes the oldest entries are
// rolled off, keeping the newest ~wikiLogKeepBytes worth of complete sections.
func (s *Store) appendLog(operation, details string) error {
	s.logMu.Lock()
	defer s.logMu.Unlock()

	details = redact.String(details)
	logPath := filepath.Join(s.dir, "log.md")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("wiki: open log: %w", err)
	}
	ts := time.Now().Format("2006-01-02 15:04")
	entry := fmt.Sprintf("## [%s] %s\n%s\n\n", ts, operation, details)
	if _, err := f.WriteString(entry); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	s.rotateLogIfNeeded(logPath)
	return nil
}

// rotateLogIfNeeded truncates log.md to its newest entries once it exceeds
// wikiLogMaxBytes. Best-effort: a rotation failure leaves the (oversized but
// valid) log in place and is logged, never returned — the audit log is
// non-critical and must not fail a page write.
//
// The tail is snapped forward to the next "## " section header so the rotated
// file starts at a clean entry boundary instead of mid-block. Rewrite is atomic
// (tmp+rename) to mirror the cron run log prune.
func (s *Store) rotateLogIfNeeded(logPath string) {
	stat, err := os.Stat(logPath)
	if err != nil || stat.Size() <= int64(wikiLogMaxBytes) {
		return
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		slog.Warn("wiki: read log for rotation failed", "path", logPath, "error", err)
		return
	}
	if len(data) <= wikiLogKeepBytes {
		return
	}

	tail := data[len(data)-wikiLogKeepBytes:]
	// Snap forward to the start of the first complete "## " section so the
	// rotated file does not begin in the middle of a prior entry's body.
	if idx := bytes.Index(tail, []byte("\n## ")); idx >= 0 {
		tail = tail[idx+1:]
	} else if idx := bytes.Index(tail, []byte("## ")); idx > 0 {
		tail = tail[idx:]
	}

	tmp := logPath + ".tmp"
	if err := os.WriteFile(tmp, tail, 0o644); err != nil { //nolint:gosec // G306 — world-readable matches the log file
		slog.Warn("wiki: write rotated log failed", "path", tmp, "error", err)
		return
	}
	if err := os.Rename(tmp, logPath); err != nil {
		slog.Warn("wiki: rename rotated log failed", "from", tmp, "to", logPath, "error", err)
		_ = os.Remove(tmp)
	}
}

// Close stops the background semantic refresh (waiting for any in-flight
// re-embed so its cache write cannot land after teardown) and releases the FTS
// search database.
func (s *Store) Close() error {
	if s.sem != nil {
		s.sem.shutdown()
	}
	if s.diaryFTS != nil {
		s.diaryFTS.closeSemantic()
	}
	if s.fts != nil {
		return s.fts.close()
	}
	return nil
}

func (s *Store) loadOrCreateIndex() (*Index, error) {
	indexPath := filepath.Join(s.dir, "index.md")
	idx, err := parseIndex(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			idx = newIndex()
			err = idx.Save(indexPath)
			return idx, err
		}
		return nil, err
	}
	return idx, nil
}

// reconcileIndexFromDisk rebuilds every index entry from the Markdown
// canonical state while preserving the diary high-water cursor. It repairs all
// three restart drift classes: ghost paths, orphan paths, and existing paths
// whose metadata changed outside this process or survived an index-save failure.
// NewStore calls it before concurrency starts, so no Store lock is required.
func (s *Store) reconcileIndexFromDisk() error {
	pages, err := s.ListPages("")
	if err != nil {
		return err
	}
	refreshed := newIndex()
	refreshed.LastProcessed = s.index.LastProcessed
	for _, rel := range pages {
		rel = filepath.ToSlash(rel)
		page, perr := s.ReadPage(rel)
		if perr != nil {
			continue // unreadable/parse error: leave it out, same as rebuildIndex
		}
		refreshed.updateEntry(rel, page)
	}
	oldCount := len(s.index.Entries)
	changed := s.index.LastProcessed != refreshed.LastProcessed || !reflect.DeepEqual(s.index.Entries, refreshed.Entries)
	s.index = refreshed
	if changed {
		if err := refreshed.Save(filepath.Join(s.dir, "index.md")); err != nil {
			return err
		}
	}
	if changed {
		slog.Info("wiki: reconciled master index from markdown", "before", oldCount, "after", len(refreshed.Entries))
	}
	return nil
}

func ensureDirs(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, cat := range Categories {
		if err := os.MkdirAll(filepath.Join(dir, cat), 0o755); err != nil {
			return err
		}
	}
	return nil
}
