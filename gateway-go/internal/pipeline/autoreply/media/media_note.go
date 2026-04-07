package media

import (
	"fmt"
	"path/filepath"
	"strings"
)

// MediaAttachment describes an inbound media attachment.
type MediaAttachment struct {
	MimeType string `json:"mimeType,omitempty"`
	Path     string `json:"path,omitempty"`
	Name     string `json:"name,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

// audioExtensions is used to detect audio files by extension.
var audioExtensions = map[string]bool{
	".mp3": true, ".wav": true, ".ogg": true, ".oga": true,
	".m4a": true, ".aac": true, ".flac": true, ".opus": true,
	".wma": true, ".amr": true, ".spx": true,
}

// IsAudioAttachment returns true if the attachment appears to be audio.
func IsAudioAttachment(a MediaAttachment) bool {
	if strings.HasPrefix(a.MimeType, "audio/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(a.Name))
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(a.Path))
	}
	return audioExtensions[ext]
}

// BuildInboundMediaNote generates a "[media attached: ...]" annotation for
// inbound messages with media attachments. This helps the agent understand
// what media was included without seeing the raw file paths.
func BuildInboundMediaNote(attachments []MediaAttachment, opts MediaNoteOptions) string {
	if len(attachments) == 0 {
		return ""
	}

	// Filter out transcribed audio if requested.
	filtered := attachments
	if opts.StripTranscribedAudio {
		var kept []MediaAttachment
		for _, a := range attachments {
			if !IsAudioAttachment(a) {
				kept = append(kept, a)
			}
		}
		filtered = kept
	}

	if len(filtered) == 0 {
		return ""
	}

	if opts.SuppressMediaUnderstanding {
		return fmt.Sprintf("[%d media attached]", len(filtered))
	}

	if len(filtered) == 1 {
		return formatSingleMediaNote(filtered[0])
	}
	return formatMultiMediaNote(filtered)
}

// MediaNoteOptions controls media note generation.
type MediaNoteOptions struct {
	StripTranscribedAudio      bool
	SuppressMediaUnderstanding bool
}

func formatSingleMediaNote(a MediaAttachment) string {
	kind := classifyMedia(a)
	name := a.Name
	if name == "" {
		name = filepath.Base(a.Path)
	}
	if name == "" || name == "." {
		return fmt.Sprintf("[%s attached]", kind)
	}
	return fmt.Sprintf("[%s attached: %s]", kind, name)
}

func formatMultiMediaNote(attachments []MediaAttachment) string {
	counts := make(map[string]int)
	for _, a := range attachments {
		kind := classifyMedia(a)
		counts[kind]++
	}

	var parts []string
	for kind, count := range counts {
		if count == 1 {
			parts = append(parts, fmt.Sprintf("1 %s", kind))
		} else {
			parts = append(parts, fmt.Sprintf("%d %ss", count, kind))
		}
	}
	return fmt.Sprintf("[media attached: %s]", strings.Join(parts, ", "))
}

func classifyMedia(a MediaAttachment) string {
	mime := strings.ToLower(a.MimeType)
	if strings.HasPrefix(mime, "image/") {
		return "image"
	}
	if strings.HasPrefix(mime, "audio/") {
		return "audio"
	}
	if strings.HasPrefix(mime, "video/") {
		return "video"
	}
	if strings.HasPrefix(mime, "application/pdf") {
		return "document"
	}
	if IsAudioAttachment(a) {
		return "audio"
	}
	return "file"
}
