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
	rel, _, _, err := s.SaveCaptureAt(kind, context, text)
	return rel, err
}

// NormalizeCaptureText is the exact normalization SaveCaptureAt applies to the
// body before writing. Exposed so a caller that maps line numbers into the
// saved file (the oversized-document digest) can normalize its own copy
// identically — otherwise redaction/trim would shift every mapped line.
func NormalizeCaptureText(text string) string {
	return strings.TrimSpace(redact.String(text))
}

// SaveCaptureAt is SaveCapture returning richer coordinates: the absolute
// saved path and the 1-based line at which the capture BODY starts inside the
// file (after the metadata header). An oversized-document digest uses them to
// map its chunk summaries to file line numbers the agent can open directly.
func (s *Store) SaveCaptureAt(kind, context, text string) (rel, abs string, bodyStartLine int, err error) {
	text = NormalizeCaptureText(text)
	if text == "" {
		return "", "", 0, fmt.Errorf("wiki: empty capture text")
	}
	if s.diaryDir == "" {
		return "", "", 0, fmt.Errorf("wiki: no diary dir; captures disabled")
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		kind = "capture"
	}

	dir := filepath.Join(filepath.Dir(s.diaryDir), "captures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", 0, fmt.Errorf("wiki: captures dir: %w", err)
	}
	now := time.Now()
	name := fmt.Sprintf("capture-%s-%s.md", now.Format("20060102-150405"), kind)

	var header strings.Builder
	fmt.Fprintf(&header, "# 캡처 원문 (%s)\n\n- 종류: %s\n- 시각: %s\n", kind, kind, now.Format("2006-01-02 15:04"))
	if c := strings.TrimSpace(redact.String(context)); c != "" {
		fmt.Fprintf(&header, "- 맥락: %s\n", c)
	}
	header.WriteString("\n---\n\n")
	// The body begins on the line right after the header's final newline —
	// counting the header's newlines keeps this correct if the format changes.
	bodyStartLine = strings.Count(header.String(), "\n") + 1

	abs = filepath.Join(dir, name)
	// Same-second, same-kind captures collide on the second-resolution timestamp —
	// a multi-file batch calls SaveCapture per file in a tight loop, so two docs
	// saved in the same second would map to one path and the second's atomic rename
	// would silently overwrite the first (the agent then reads one file's content
	// for both pointers). Bump a numeric suffix until the path is free so every
	// captured file survives with its own agent-openable path. Bounded so a stat
	// error (e.g. permission) can't spin — the write below surfaces real failures.
	for seq := 2; seq < 1000; seq++ {
		if _, statErr := os.Stat(abs); statErr != nil {
			break
		}
		name = fmt.Sprintf("capture-%s-%d-%s.md", now.Format("20060102-150405"), seq, kind)
		abs = filepath.Join(dir, name)
	}
	tmp := abs + ".tmp"
	if err := writeFileSync(tmp, []byte(header.String()+text+"\n"), 0o644); err != nil {
		return "", "", 0, fmt.Errorf("wiki: write capture: %w", err)
	}
	if err := os.Rename(tmp, abs); err != nil {
		os.Remove(tmp)
		return "", "", 0, fmt.Errorf("wiki: rename capture: %w", err)
	}

	rel = filepath.Join("captures", name)
	// Breadcrumb: the preview makes the capture diary-searchable (and feeds
	// dreaming); the path lets the agent open the full original on demand.
	preview := []rune(text)
	if len(preview) > captureBreadcrumbRunes {
		preview = preview[:captureBreadcrumbRunes]
	}
	entry := fmt.Sprintf("[캡처:%s] 원문 보관 %s — %s", kind, rel, string(preview))
	if err := s.AppendDiary(entry); err != nil {
		return rel, abs, bodyStartLine, fmt.Errorf("wiki: capture breadcrumb: %w", err)
	}
	return rel, abs, bodyStartLine, nil
}
