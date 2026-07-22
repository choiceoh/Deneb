package groupware

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// ReadApprovalByDocID fetches one 전자결재 document body by Amaranth doc id
// (searches pending→done→cc→total via folder=all, matching id === query).
func ReadApprovalByDocID(ctx context.Context, cfg Config, docID string) (string, error) {
	return ReadApprovalByDocIDIn(ctx, cfg, docID, "")
}

// ReadApprovalByDocIDIn is ReadApprovalByDocID with a folder hint: a caller
// that knows the doc's box (the list rows carry it) skips the 4-folder scan —
// the dominant cost of a cold detail open. Unknown/blank folder falls back to
// the full scan.
func ReadApprovalByDocIDIn(ctx context.Context, cfg Config, docID, folder string) (string, error) {
	docID = strings.TrimSpace(docID)
	if docID == "" {
		return "", fmt.Errorf("doc id required")
	}
	f := strings.ToLower(strings.TrimSpace(folder))
	switch f {
	case "pending", "done", "cc", "total":
	default:
		f = "all"
	}
	out, err := Run(ctx, cfg, Request{
		Area:   AreaApproval,
		Action: ActionRead,
		Folder: f,
		Query:  docID,
	})
	// A stale folder hint (doc moved boxes after the list snapshot) retries
	// with the full scan before giving up.
	if f != "all" && (err != nil || out == "" || strings.HasPrefix(out, "그룹웨어")) {
		out, err = Run(ctx, cfg, Request{
			Area:   AreaApproval,
			Action: ActionRead,
			Folder: "all",
			Query:  docID,
		})
	}
	if err != nil {
		// Keep the reader's stdout/stderr diagnostic on the error (Run stashes it
		// in out) — the radar otherwise logs a bare "exit status 1" that hides why
		// a specific approval cannot be read. Mirrors the list paths.
		return "", wrapGroupwareRunError(out, err)
	}
	if out == "" || strings.HasPrefix(out, "그룹웨어 리더:") || strings.HasPrefix(out, "그룹웨어 읽기 실패") {
		return "", fmt.Errorf("approval %s not found", docID)
	}
	return out, nil
}

// ListERP returns a human-readable list for stock|po|receive|ship|price|sales|people|board.
func ListERP(ctx context.Context, cfg Config, area, folder, query string, limit int) (string, error) {
	area = strings.ToLower(strings.TrimSpace(area))
	switch area {
	case "stock", "po", "receive", "ship", "price", "sales", "people", "board":
	default:
		return "", fmt.Errorf("area must be stock|po|receive|ship|price|sales|people|board")
	}
	action := ActionList
	if area == "sales" && strings.TrimSpace(folder) != "" {
		action = ActionSummary
	}
	return Run(ctx, cfg, Request{
		Area:   Area(area),
		Action: action,
		Folder: folder,
		Query:  query,
		Limit:  limit,
	})
}

// ReadApprovalAttachment downloads one Amaranth attachment (by 1-based index or
// filename) and returns the reader text (OCR/extracted body or a calm note).
func ReadApprovalAttachment(ctx context.Context, cfg Config, docID, selector string) (string, error) {
	docID = strings.TrimSpace(docID)
	selector = strings.TrimSpace(selector)
	if docID == "" {
		return "", fmt.Errorf("doc id required")
	}
	if selector == "" {
		return "", fmt.Errorf("attachment selector required")
	}
	out, err := Run(ctx, cfg, Request{
		Area:       AreaApproval,
		Action:     ActionAttachment,
		DocID:      docID,
		Attachment: selector,
	})
	if err != nil {
		return "", wrapGroupwareRunError(out, err)
	}
	if out == "" || strings.HasPrefix(out, "그룹웨어 리더:") || strings.HasPrefix(out, "그룹웨어 읽기 실패") {
		return "", fmt.Errorf("attachment %s not found for doc %s", selector, docID)
	}
	return out, nil
}

// ApprovalAttachmentFile is one ECM file downloaded for HTTP streaming.
type ApprovalAttachmentFile struct {
	Filename string
	MimeType string
	Data     []byte
}

type attachmentDownloadJSON struct {
	Filename string `json:"filename"`
	MimeType string `json:"mimeType"`
	Size     int    `json:"size"`
	Base64   string `json:"base64"`
	Error    string `json:"error,omitempty"`
}

// DownloadApprovalAttachment fetches raw ECM bytes for one selected file
// (1-based index or filename). Used by the miniapp HTTP download route — not
// the text-extract RPC.
func DownloadApprovalAttachment(ctx context.Context, cfg Config, docID, selector string) (ApprovalAttachmentFile, error) {
	docID = strings.TrimSpace(docID)
	selector = strings.TrimSpace(selector)
	if docID == "" {
		return ApprovalAttachmentFile{}, fmt.Errorf("doc id required")
	}
	if selector == "" {
		return ApprovalAttachmentFile{}, fmt.Errorf("attachment selector required")
	}
	out, err := Run(ctx, cfg, Request{
		Area:       AreaApproval,
		Action:     ActionAttachmentDownload,
		DocID:      docID,
		Attachment: selector,
	})
	if err != nil {
		return ApprovalAttachmentFile{}, err
	}
	var envelope attachmentDownloadJSON
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		return ApprovalAttachmentFile{}, fmt.Errorf("parse attachment download: %w", err)
	}
	if envelope.Error != "" {
		return ApprovalAttachmentFile{}, fmt.Errorf("%s", envelope.Error)
	}
	if envelope.Base64 == "" {
		return ApprovalAttachmentFile{}, fmt.Errorf("attachment empty for doc %s", docID)
	}
	data, err := base64.StdEncoding.DecodeString(envelope.Base64)
	if err != nil {
		return ApprovalAttachmentFile{}, fmt.Errorf("decode attachment: %w", err)
	}
	name := strings.TrimSpace(envelope.Filename)
	if name == "" {
		name = "attachment"
	}
	mime := strings.TrimSpace(envelope.MimeType)
	if mime == "" {
		mime = "application/octet-stream"
	}
	return ApprovalAttachmentFile{Filename: name, MimeType: mime, Data: data}, nil
}
