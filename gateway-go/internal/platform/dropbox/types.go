package dropbox

import (
	"fmt"
	"strings"
)

// Entry is a Dropbox file or folder metadata entry (the subset Deneb needs).
type Entry struct {
	Tag            string // "file" or "folder"
	Name           string
	PathDisplay    string
	PathLower      string
	ID             string
	Size           int64
	ServerModified string
}

// IsFolder reports whether the entry is a folder.
func (e Entry) IsFolder() bool { return e.Tag == "folder" }

// rawMetadata is the wire shape of a Dropbox file/folder metadata object.
// The ".tag" field discriminates file vs folder in Dropbox's union types.
type rawMetadata struct {
	Tag            string `json:".tag"`
	Name           string `json:"name"`
	PathLower      string `json:"path_lower"`
	PathDisplay    string `json:"path_display"`
	ID             string `json:"id"`
	Size           int64  `json:"size"`
	ServerModified string `json:"server_modified"`
}

func (m rawMetadata) toEntry() Entry {
	return Entry{
		Tag:            m.Tag,
		Name:           m.Name,
		PathDisplay:    m.PathDisplay,
		PathLower:      m.PathLower,
		ID:             m.ID,
		Size:           m.Size,
		ServerModified: m.ServerModified,
	}
}

// FormatEntries renders entries as a Markdown list for chat display.
func FormatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return "(항목 없음)"
	}
	var sb strings.Builder
	for _, e := range entries {
		path := e.PathDisplay
		if path == "" {
			path = e.Name
		}
		if e.IsFolder() {
			fmt.Fprintf(&sb, "- 📁 **%s**  `%s`\n", e.Name, path)
		} else {
			fmt.Fprintf(&sb, "- 📄 %s  `%s`  (%s)\n", e.Name, path, humanSize(e.Size))
		}
	}
	return sb.String()
}

// humanSize formats a byte count as a compact human-readable string.
func humanSize(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1fGB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}
