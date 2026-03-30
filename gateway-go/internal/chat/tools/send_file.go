package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/chat/toolctx"
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
		// Falls back to 50 MB (Telegram's limit) when no channel limit is registered.
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
			return "send_file: no media send function available (channel not connected).", nil
		}

		delivery := toolctx.DeliveryFromContext(ctx)
		if delivery == nil {
			return "send_file: no delivery context available.", nil
		}

		sendCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		if err := sendFn(sendCtx, delivery, p.FilePath, mediaType, p.Caption, p.Silent); err != nil {
			return fmt.Sprintf("Failed to send file: %s", err.Error()), nil
		}

		return fmt.Sprintf("File sent: %s (%s, %d bytes)", filepath.Base(p.FilePath), mediaType, info.Size()), nil
	}
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

	mime := http.DetectContentType(buf[:n])

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
