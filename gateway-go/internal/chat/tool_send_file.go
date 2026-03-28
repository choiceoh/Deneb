package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// MediaSendFunc delivers a file to the originating channel (e.g., Telegram).
// mediaType is one of: photo, document, video, audio, voice (empty = auto-detect).
type MediaSendFunc func(ctx context.Context, delivery *DeliveryContext, filePath, mediaType, caption string, silent bool) error

// sendFileToolSchema returns the JSON Schema for the send_file tool.
func sendFileToolSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "Path to the file to send",
			},
			"type": map[string]any{
				"type":        "string",
				"description": "Media type. Auto-detected from MIME if omitted",
				"enum":        []string{"photo", "document", "video", "audio", "voice"},
			},
			"caption": map[string]any{
				"type":        "string",
				"description": "Caption text (optional)",
			},
			"silent": map[string]any{
				"type":        "boolean",
				"description": "Send without notification sound",
			},
		},
		"required": []string{"file_path"},
	}
}

// toolSendFile implements the send_file tool for delivering files to the user via channel.
func toolSendFile() ToolFunc {
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

		// Enforce Telegram's 50 MB upload limit.
		const maxFileSize = 50 * 1024 * 1024
		if info.Size() > maxFileSize {
			return "", fmt.Errorf("file too large (%d bytes); Telegram limit is 50 MB", info.Size())
		}

		// Auto-detect media type from MIME if not specified.
		mediaType := p.Type
		if mediaType == "" {
			mediaType = detectMediaType(p.FilePath)
		}

		// Get media send function from context.
		sendFn := MediaSendFuncFromContext(ctx)
		if sendFn == nil {
			return "send_file: no media send function available (channel not connected).", nil
		}

		delivery := DeliveryFromContext(ctx)
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

// detectMediaType infers the media type from the file's MIME type.
// Falls back to "document" for unknown types.
func detectMediaType(filePath string) string {
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
