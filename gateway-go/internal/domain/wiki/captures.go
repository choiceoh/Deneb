// captures.go — durable storage for captured raw content (OCR text, ASR
// transcripts).
//
// A shared meeting recording or document photo used to live only inside the
// chat turn that processed it: the agent's *summary* reached the wiki, but
// the full transcript/extracted text was unrecoverable once the transcript
// aged out. Captures are primary records — the thing you go back to when the
// summary turns out to have dropped the one number that mattered.
//
// Each capture becomes a markdown file under {memory}/captures/ plus a diary
// breadcrumb, which makes it (a) recallable via diary search, (b) distillable
// by the dreaming cycle, and (c) included in the daily offsite backup. The
// wiki proper stays curated — raw dumps don't belong in its categories.
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/redact"
)

// captureBreadcrumbRunes bounds the preview embedded in the diary entry.
const captureBreadcrumbRunes = 200

// SaveCapture persists raw captured text and drops a searchable diary
// breadcrumb pointing at it. kind is a short tag ("audio", "image"); context
// is optional origin info (caption, app/sender). Returns the saved file's
// path relative to the memory root (e.g. "captures/capture-...-audio.md").
func (s *Store) SaveCapture(kind, context, text string) (string, error) {
	text = strings.TrimSpace(redact.String(text))
	if text == "" {
		return "", fmt.Errorf("wiki: empty capture text")
	}
	if s.diaryDir == "" {
		return "", fmt.Errorf("wiki: no diary dir; captures disabled")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "capture"
	}

	dir := filepath.Join(filepath.Dir(s.diaryDir), "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("wiki: captures dir: %w", err)
	}
	now := time.Now()
	name := fmt.Sprintf("capture-%s-%s.md", now.Format("20060102-150405"), kind)

	var sb strings.Builder
	fmt.Fprintf(&sb, "# 캡처 원문 (%s)\n\n- 종류: %s\n- 시각: %s\n", kind, kind, now.Format("2006-01-02 15:04"))
	if c := strings.TrimSpace(redact.String(context)); c != "" {
		fmt.Fprintf(&sb, "- 맥락: %s\n", c)
	}
	sb.WriteString("\n---\n\n")
	sb.WriteString(text)
	sb.WriteString("\n")

	abs := filepath.Join(dir, name)
	tmp := abs + ".tmp"
	if err := writeFileSync(tmp, []byte(sb.String()), 0o644); err != nil {
		return "", fmt.Errorf("wiki: write capture: %w", err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("wiki: rename capture: %w", err)
	}

	rel := filepath.Join("captures", name)
	// Breadcrumb: the preview makes the capture diary-searchable (and feeds
	// dreaming); the path lets the agent open the full original on demand.
	preview := []rune(text)
	if len(preview) > captureBreadcrumbRunes {
		preview = preview[:captureBreadcrumbRunes]
	}
	entry := fmt.Sprintf("[캡처:%s] 원문 보관 %s — %s", kind, rel, string(preview))
	if err := s.AppendDiary(entry); err != nil {
		return rel, fmt.Errorf("wiki: capture breadcrumb: %w", err)
	}
	return rel, nil
}
