package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coremedia"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolSendFile implements the send_file tool for delivering files to the user via channel.
func ToolSendFile() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			FilePath string `json:"file_path"`
			Type     string `json:"type"`
			Caption  string `json:"caption"`
			Silent   bool   `json:"silent"`
		}
		if err := jsonutil.UnmarshalInto("send_file params", input, &p); err != nil {
			return "", err
		}
		if p.FilePath == "" {
			return "", fmt.Errorf("file_path is required")
		}

		// Verify file exists and is readable.
		info, err := os.Stat(p.FilePath)
		if err != nil {
			return "", fmt.Errorf("file not found: %w", err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("path is a directory, not a file")
		}

		// Enforce the channel-specific upload limit injected at run start.
		// Falls back to 50 MB when no channel limit is registered.
		maxFileSize := toolctx.MaxUploadBytesFromContext(ctx)
		if maxFileSize == 0 {
			maxFileSize = 50 * 1024 * 1024 // safe default
		}
		if info.Size() > maxFileSize {
			return "", fmt.Errorf("file too large (%d bytes); channel limit is %d MB",
				info.Size(), maxFileSize/(1024*1024))
		}

		// Auto-detect media type from MIME if not specified.
		mediaType := p.Type
		if mediaType == "" {
			mediaType = DetectMediaType(p.FilePath)
		}

		// Get media send function from context.
		sendFn := toolctx.MediaSendFuncFromContext(ctx)
		if sendFn == nil {
			return "", fmt.Errorf("file delivery unavailable: channel not connected; do not claim the file is visible anywhere")
		}

		delivery := toolctx.DeliveryFromContext(ctx)
		if delivery == nil || delivery.Channel == "" || delivery.To == "" {
			return "", fmt.Errorf("file delivery unavailable: no active delivery target; do not claim the file was shown anywhere")
		}

		sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		if err := sendFn(sendCtx, delivery, p.FilePath, mediaType, p.Caption, p.Silent); err != nil {
			return "", fmt.Errorf("file delivery failed and was not confirmed; do not claim the file is visible anywhere: %w", err)
		}

		result := fmt.Sprintf("File sent: %s (%s, %d bytes)", filepath.Base(p.FilePath), mediaType, info.Size())
		if vpath := archiveSentFile(ctx, p.FilePath, info.Size()); vpath != "" {
			result += fmt.Sprintf("; 파일 저장소 보관: %s", vpath)
			// Mint a durable, path-scoped share link so the delivered file is
			// reachable as a persistent URL, not only the one-shot channel upload.
			// Empty when no public base URL / client token is configured (skip).
			if link := fileshare.Link(vpath); link != "" {
				result += fmt.Sprintf("; 공유 링크(7일): %s", link)
			}
		}
		return result, nil
	}
}

// sentFileArchiveMaxBytes caps the size of a sent file copied into the user file
// store. Channel uploads can be larger, but archiving very large files would
// bloat the store; an oversized send is delivered but not archived.
const sentFileArchiveMaxBytes = 25 * 1024 * 1024

// archiveSentFile best-effort saves a copy of a just-delivered file into the
// user file store (/전송/<date>/<name>) so files the agent sends are durable and
// browsable later, not a one-shot channel upload. Non-fatal by design: the send
// already succeeded, so any failure (no store, read error, oversized) returns ""
// and is simply not reported. Disable with DENEB_ARCHIVE_SENT_FILES=0.
func archiveSentFile(ctx context.Context, filePath string, size int64) string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("DENEB_ARCHIVE_SENT_FILES"))) {
	case "0", "false", "no", "off":
		return ""
	}
	if size <= 0 || size > sentFileArchiveMaxBytes {
		return ""
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	store, err := filestore.DefaultLocalStore()
	if err != nil || store == nil {
		return ""
	}
	vpath := path.Join("/전송", time.Now().Format("2006-01-02"), filepath.Base(filePath))
	if _, err := store.Put(ctx, vpath, data, true); err != nil {
		return ""
	}
	return vpath
}

// DetectMediaType infers the media type from the file's MIME type.
// Falls back to "document" for unknown types.
func DetectMediaType(filePath string) string {
	f, err := os.Open(filePath)
	if err != nil {
		return "document"
	}
	defer f.Close()

	// Read first 512 bytes for MIME detection.
	buf := make([]byte, 512)
	n, _ := f.Read(buf)
	if n == 0 {
		return "document"
	}

	mime := coremedia.DetectMIME(buf[:n])

	switch {
	case strings.HasPrefix(mime, "image/"):
		return "photo"
	case strings.HasPrefix(mime, "video/"):
		return "video"
	case strings.HasPrefix(mime, "audio/"):
		// Check extension for voice notes (OGG Opus).
		ext := strings.ToLower(filepath.Ext(filePath))
		if ext == ".ogg" || ext == ".opus" || ext == ".oga" {
			return "voice"
		}
		return "audio"
	default:
		return "document"
	}
}
