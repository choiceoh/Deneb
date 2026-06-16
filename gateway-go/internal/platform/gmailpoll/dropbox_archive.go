package gmailpoll

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// archivableExts are attachment extensions substantive enough to keep in
// Dropbox. Inline images, signatures, and calendar invites are skipped.
var archivableExts = []string{".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".zip", ".csv"}

// minArchiveSize skips tiny inline bits (1px trackers, signature glyphs).
const minArchiveSize = 1024

// archiveAttachments uploads substantive attachments of the analyzed emails to
// Dropbox under ArchiveFolder/<date>/. Best-effort: a token-less host or any
// per-file failure is logged and skipped, never failing the poll cycle. Returns
// the archived destination paths for inclusion in the consolidated report.
func (s *Service) archiveAttachments(ctx context.Context, client *gmail.Client, details []*gmail.MessageDetail) []string {
	// Re-check per cycle (cheap file stat) so connecting Dropbox after startup
	// activates archiving without a restart — no startup-latched bool.
	if !dropbox.HasToken() {
		return nil
	}
	dbx, err := s.ensureDropbox()
	if err != nil {
		// No Dropbox token yet (user hasn't run deneb-dropbox-auth) → skip.
		s.log.Debug("첨부 아카이브 건너뜀 — Dropbox 미연동", "error", err)
		return nil
	}

	day := time.Now().Format("2006-01-02")
	var archived []string
	for _, d := range details {
		for _, att := range d.Attachments {
			if !isArchivable(att) {
				continue
			}
			data, err := client.GetAttachment(ctx, d.ID, att.AttachmentID)
			if err != nil {
				s.log.Warn("첨부 다운로드 실패(아카이브)", "msg", d.ID, "file", att.Filename, "error", err)
				continue
			}
			dest := fmt.Sprintf("%s/%s/%s_%s", s.cfg.ArchiveFolder, day,
				sanitizePathComponent(d.From), sanitizePathComponent(att.Filename))
			if _, err := dbx.Upload(ctx, dest, data, false); err != nil {
				s.log.Warn("첨부 Dropbox 업로드 실패", "dest", dest, "error", err)
				continue
			}
			archived = append(archived, dest)
		}
	}
	return archived
}

// archiveInlineAttachments uploads the substantive attachments of an
// LMTP-delivered message to Dropbox from their inline bytes (no Gmail fetch),
// mirroring archiveAttachments' policy (isArchivable + dated, sender-tagged path).
// Best-effort: a token-less host or any per-file failure is logged and skipped.
func (s *Service) archiveInlineAttachments(ctx context.Context, msg *gmail.MessageDetail, bytesByID map[string][]byte) []string {
	if len(bytesByID) == 0 || !dropbox.HasToken() {
		return nil
	}
	dbx, err := s.ensureDropbox()
	if err != nil {
		s.log.Debug("첨부 아카이브 건너뜀 — Dropbox 미연동", "error", err)
		return nil
	}

	day := time.Now().Format("2006-01-02")
	var archived []string
	for _, att := range msg.Attachments {
		if !isArchivable(att) {
			continue
		}
		data := bytesByID[att.AttachmentID]
		if len(data) == 0 {
			continue
		}
		dest := fmt.Sprintf("%s/%s/%s_%s", s.cfg.ArchiveFolder, day,
			sanitizePathComponent(msg.From), sanitizePathComponent(att.Filename))
		if _, err := dbx.Upload(ctx, dest, data, false); err != nil {
			s.log.Warn("첨부 Dropbox 업로드 실패", "dest", dest, "error", err)
			continue
		}
		archived = append(archived, dest)
	}
	return archived
}

// ensureDropbox lazily resolves the singleton Dropbox client (retries each call
// if a previous init failed, e.g. token added after startup).
func (s *Service) ensureDropbox() (*dropbox.Client, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropboxClient != nil {
		return s.dropboxClient, nil
	}
	c, err := dropbox.DefaultClient()
	if err != nil {
		return nil, err
	}
	s.dropboxClient = c
	return c, nil
}

func isArchivable(att gmail.AttachmentInfo) bool {
	if att.Size < minArchiveSize {
		return false
	}
	lower := strings.ToLower(att.Filename)
	for _, ext := range archivableExts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// sanitizePathComponent reduces a sender or filename to a safe single Dropbox
// path component (no separators, no angle brackets).
func sanitizePathComponent(s string) string {
	s = strings.TrimSpace(strings.Trim(strings.TrimSpace(s), "<>"))
	s = strings.ReplaceAll(s, "/", "_")
	s = strings.ReplaceAll(s, "\\", "_")
	s = path.Base(s)
	if s == "" || s == "." {
		return "unknown"
	}
	return s
}
