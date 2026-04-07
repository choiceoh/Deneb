// Package media — media understanding pipeline for inbound Telegram messages.
//
// Extracts images, videos, animations, and documents from Telegram messages,
// downloads them via the Bot API, and produces ChatAttachment objects ready for
// LLM consumption.
//
// Video handling: downloads the video file, extracts representative frames
// using ffmpeg, and returns each frame as a separate image attachment so the
// LLM can reason about the video content.
//
// Document handling: PDFs, Office files (DOCX/XLSX/PPTX), and OpenDocument
// formats are parsed via the LiteParse CLI (lit) to extract text content.
package media

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/liteparse"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// Attachment mirrors chat.ChatAttachment but lives in the media package to
// avoid an import cycle. The caller (inbound processor) converts these to
// chat.ChatAttachment before dispatching to chat.send.
type Attachment struct {
	Type     string // "image", "video", "document_text"
	MimeType string
	Data     string // base64-encoded for images; plain text for document_text
	Name     string
	Size     int64
}

// maxImageDownloadSize is the maximum image size we'll download for vision (20 MB).
const maxImageDownloadSize = 20 * 1024 * 1024

// maxVideoDownloadSize is the maximum video size we'll download for frame extraction (50 MB).
const maxVideoDownloadSize = 50 * 1024 * 1024

// ExtractAttachments extracts downloadable media from a Telegram message and
// returns attachments suitable for LLM consumption.
//
// Supported media types:
//   - Photo ([]PhotoSize) — picks the highest-resolution variant
//   - Video — downloads and extracts representative frames via ffmpeg
//   - Animation (GIF) — extracts a single representative frame
//   - Document (image/*) — downloaded as image attachment
//   - Document (PDF, Office, OpenDocument) — text extracted via LiteParse
//
// Audio, voice, stickers, and video notes are skipped (per user requirements:
// audio transcription not needed).
func ExtractAttachments(ctx context.Context, client *telegram.Client, msg *telegram.Message, logger *slog.Logger) []Attachment {
	if client == nil || msg == nil {
		return nil
	}

	var attachments []Attachment

	// 1. Photos — pick the largest variant (last element in the array).
	if len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		if photo.FileSize > 0 && photo.FileSize > maxImageDownloadSize {
			logger.Warn("skipping oversized photo", "fileId", photo.FileID, "size", photo.FileSize)
		} else {
			att := downloadImage(ctx, client, photo.FileID, "image/jpeg", logger)
			if att != nil {
				attachments = append(attachments, *att)
			}
		}
	}

	// 2. Video — download and extract frames.
	if msg.Video != nil {
		v := msg.Video
		if v.FileSize > 0 && v.FileSize > maxVideoDownloadSize {
			logger.Warn("skipping oversized video", "fileId", v.FileID, "size", v.FileSize)
		} else {
			frames := extractVideoAttachments(ctx, client, v, logger)
			attachments = append(attachments, frames...)
		}
	}

	// 3. Animation (GIF) — extract a single frame.
	if msg.Animation != nil {
		a := msg.Animation
		if a.FileSize > 0 && a.FileSize > maxVideoDownloadSize {
			logger.Warn("skipping oversized animation", "fileId", a.FileID, "size", a.FileSize)
		} else {
			frames := extractAnimationAttachments(ctx, client, a, logger)
			attachments = append(attachments, frames...)
		}
	}

	// 4. Document — image or parseable document (PDF, Office, etc.).
	if msg.Document != nil {
		d := msg.Document
		switch {
		case strings.HasPrefix(d.MimeType, "image/"):
			if d.FileSize > 0 && d.FileSize > maxImageDownloadSize {
				logger.Warn("skipping oversized document image", "fileId", d.FileID, "size", d.FileSize)
			} else {
				mime := d.MimeType
				if mime == "" {
					mime = "image/jpeg"
				}
				att := downloadImage(ctx, client, d.FileID, mime, logger)
				if att != nil {
					att.Name = d.FileName
					attachments = append(attachments, *att)
				}
			}

		case liteparse.Available() && liteparse.SupportedMIME(d.MimeType):
			if d.FileSize > 0 && d.FileSize > maxVideoDownloadSize {
				logger.Warn("skipping oversized document", "fileId", d.FileID, "size", d.FileSize)
			} else {
				att := parseDocument(ctx, client, d, logger)
				if att != nil {
					attachments = append(attachments, *att)
				}
			}
		}
	}

	return attachments
}

// HasMedia returns true if the Telegram message contains any media that
// ExtractAttachments would process.
func HasMedia(msg *telegram.Message) bool {
	if msg == nil {
		return false
	}
	if len(msg.Photo) > 0 {
		return true
	}
	if msg.Video != nil {
		return true
	}
	if msg.Animation != nil {
		return true
	}
	if msg.Document != nil {
		mime := msg.Document.MimeType
		if strings.HasPrefix(mime, "image/") {
			return true
		}
		if liteparse.Available() && liteparse.SupportedMIME(mime) {
			return true
		}
	}
	return false
}

// MessageText returns the effective text body of a Telegram message.
// For media messages, this is the Caption; for text messages, it's Text.
func MessageText(msg *telegram.Message) string {
	if msg == nil {
		return ""
	}
	if msg.Text != "" {
		return msg.Text
	}
	return msg.Caption
}

// downloadImage downloads a file from Telegram and returns it as a base64-
// encoded image attachment. Returns nil on failure (logged but not fatal).
func downloadImage(ctx context.Context, client *telegram.Client, fileID, mimeType string, logger *slog.Logger) *Attachment {
	data, filePath, err := client.DownloadFile(ctx, fileID)
	if err != nil {
		logger.Warn("failed to download image", "fileId", fileID, "error", err)
		return nil
	}

	// Detect MIME type from content if not provided.
	if mimeType == "" || mimeType == "application/octet-stream" {
		detected := http.DetectContentType(data)
		if strings.HasPrefix(detected, "image/") {
			mimeType = detected
		} else {
			mimeType = "image/jpeg" // Telegram photos are always JPEG
		}
	}

	return &Attachment{
		Type:     "image",
		MimeType: mimeType,
		Data:     base64.StdEncoding.EncodeToString(data),
		Name:     filePath,
		Size:     int64(len(data)),
	}
}

// extractVideoAttachments downloads a video and extracts representative frames.
// Falls back to downloading just the thumbnail if ffmpeg is unavailable or
// the video is too large.
func extractVideoAttachments(ctx context.Context, client *telegram.Client, v *telegram.Video, logger *slog.Logger) []Attachment {
	// Try to extract frames from the actual video.
	data, _, err := client.DownloadFile(ctx, v.FileID)
	if err != nil {
		logger.Warn("failed to download video, trying thumbnail", "fileId", v.FileID, "error", err)
		return videoThumbnailFallback(ctx, client, v.Thumbnail, logger)
	}

	frames, err := ExtractFrames(data, v.Duration)
	if err != nil {
		logger.Warn("ffmpeg frame extraction failed, trying thumbnail", "error", err)
		return videoThumbnailFallback(ctx, client, v.Thumbnail, logger)
	}

	var attachments []Attachment
	for i, frame := range frames {
		attachments = append(attachments, Attachment{
			Type:     "image",
			MimeType: "image/jpeg",
			Data:     base64.StdEncoding.EncodeToString(frame),
			Name:     fmt.Sprintf("video_frame_%d.jpg", i+1),
			Size:     int64(len(frame)),
		})
	}
	return attachments
}

// extractAnimationAttachments downloads a GIF/animation and extracts a single frame.
func extractAnimationAttachments(ctx context.Context, client *telegram.Client, a *telegram.Animation, logger *slog.Logger) []Attachment {
	data, _, err := client.DownloadFile(ctx, a.FileID)
	if err != nil {
		logger.Warn("failed to download animation", "fileId", a.FileID, "error", err)
		return nil
	}

	// Extract just one frame from the animation.
	frames, err := ExtractFrames(data, a.Duration)
	if err != nil {
		logger.Warn("ffmpeg frame extraction failed for animation", "error", err)
		return nil
	}

	var attachments []Attachment
	for i, frame := range frames {
		attachments = append(attachments, Attachment{
			Type:     "image",
			MimeType: "image/jpeg",
			Data:     base64.StdEncoding.EncodeToString(frame),
			Name:     fmt.Sprintf("animation_frame_%d.jpg", i+1),
			Size:     int64(len(frame)),
		})
	}
	return attachments
}

// parseDocument downloads a document from Telegram and extracts its text
// content using the LiteParse CLI. Returns nil on failure (logged but not fatal).
func parseDocument(ctx context.Context, client *telegram.Client, d *telegram.Document, logger *slog.Logger) *Attachment {
	data, _, err := client.DownloadFile(ctx, d.FileID)
	if err != nil {
		logger.Warn("failed to download document", "fileId", d.FileID, "error", err)
		return nil
	}

	text, err := liteparse.Parse(ctx, data, d.FileName)
	if err != nil {
		logger.Warn("liteparse failed", "fileName", d.FileName, "error", err)
		return nil
	}

	if strings.TrimSpace(text) == "" {
		logger.Info("liteparse returned empty text", "fileName", d.FileName)
		return nil
	}

	return &Attachment{
		Type:     "document_text",
		MimeType: d.MimeType,
		Data:     text, // plain text, not base64
		Name:     d.FileName,
		Size:     int64(len(data)),
	}
}

// videoThumbnailFallback downloads the video thumbnail as a single-frame fallback.
func videoThumbnailFallback(ctx context.Context, client *telegram.Client, thumb *telegram.PhotoSize, logger *slog.Logger) []Attachment {
	if thumb == nil {
		return nil
	}
	att := downloadImage(ctx, client, thumb.FileID, "image/jpeg", logger)
	if att != nil {
		att.Name = "video_thumbnail.jpg"
		return []Attachment{*att}
	}
	return nil
}
