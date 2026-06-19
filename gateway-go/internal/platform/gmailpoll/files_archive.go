package gmailpoll

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// archivableExts are attachment extensions substantive enough to keep in the
// file store. Inline images, signatures, and calendar invites are skipped.
var archivableExts = []string{".pdf", ".xlsx", ".xls", ".docx", ".doc", ".pptx", ".ppt", ".hwp", ".hwpx", ".zip", ".csv"}

// minArchiveSize skips tiny inline bits (1px trackers, signature glyphs).
const minArchiveSize = 1024

// archiveAttachments saves substantive attachments of the analyzed emails to the
// local file store under ArchiveFolder/<date>/. Best-effort: opening the store or
// any per-file failure is logged and skipped, never failing the poll cycle.
// Returns the archived store paths for inclusion in the consolidated report.
// Unlike the old Dropbox path there is no token gate — the local store is always
// available, so attachments are always archived.
func (s *Service) archiveAttachments(ctx context.Context, client *gmail.Client, details []*gmail.MessageDetail) []string {
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		s.log.Warn("첨부 아카이브 건너뜀 — 파일 저장소 열기 실패", "error", err)
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
			meta, err := store.Put(ctx, dest, data, false)
			if err != nil {
				s.log.Warn("첨부 저장 실패", "dest", dest, "error", err)
				continue
			}
			archived = append(archived, meta.PathDisplay)
		}
	}
	return archived
}

// archiveInlineAttachments saves the substantive attachments of an LMTP-delivered
// message to the local file store from their inline bytes (no Gmail fetch),
// mirroring archiveAttachments' policy (isArchivable + dated, sender-tagged path).
func (s *Service) archiveInlineAttachments(ctx context.Context, msg *gmail.MessageDetail, bytesByID map[string][]byte) []string {
	if len(bytesByID) == 0 {
		return nil
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil {
		s.log.Warn("첨부 아카이브 건너뜀 — 파일 저장소 열기 실패", "error", err)
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
		meta, err := store.Put(ctx, dest, data, false)
		if err != nil {
			s.log.Warn("첨부 저장 실패", "dest", dest, "error", err)
			continue
		}
		archived = append(archived, meta.PathDisplay)
	}
	return archived
}

func isArchivable(att gmail.AttachmentInfo) bool {
	if att.Truncated {
		return false
	}
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

// sanitizePathComponent reduces a sender or filename to a safe single path
// component (no separators, no angle brackets).
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
