// Package filestore is Deneb's self-hosted user file store — the local-disk
// replacement for the former Dropbox integration. A Store exposes
// Dropbox-shaped operations (List/Search/Get/Put/Stat/Delete) over a virtual
// path space ("/메일/foo.pdf") so the chat tool, miniapp RPC, and the mail
// attachment archiver can switch backends without changing their call sites.
//
// Virtual path model (mirrors Dropbox): "" or "/" is the root; every other
// path is "/"-rooted and forward-slashed regardless of OS. Entry mirrors the
// former dropbox.Entry field-for-field so existing RPC projection
// (projectDropboxEntry) and chat formatting keep working during the cutover.
//
// The default backend is LocalStore (single filesystem root, every path
// clamped inside it). The Store interface keeps call sites swappable if a
// remote/object backend is ever added — but the project's "local inference,
// minimize external API" philosophy makes local the intended endpoint.
package filestore

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Entry is a file or folder in the store. Fields mirror the former
// dropbox.Entry so callers (miniapp projection, chat FormatEntries) stay
// backend-agnostic across the cutover.
type Entry struct {
	Tag            string // "file" or "folder"
	Name           string
	PathDisplay    string // virtual path, "/"-rooted, forward-slashed
	PathLower      string // lowercased PathDisplay
	ID             string // stable id; for the local backend == PathDisplay
	Size           int64  // 0 for folders
	ServerModified string // RFC3339 UTC; empty for folders
}

// IsFolder reports whether the entry is a folder.
func (e Entry) IsFolder() bool { return e.Tag == "folder" }

// Store is the backend-agnostic file-store surface.
type Store interface {
	// List returns entries under dir. dir "" or "/" is the root; recursive
	// walks descendants. limit<=0 means a sane default cap.
	List(ctx context.Context, dir string, recursive bool, limit int) ([]Entry, error)
	// Search returns files/folders whose name contains query (case-insensitive).
	Search(ctx context.Context, query string, maxResults int) ([]Entry, error)
	// Get returns the file bytes and its metadata.
	Get(ctx context.Context, path string) ([]byte, *Entry, error)
	// Open returns a read-seekable handle for streaming downloads
	// (http.ServeContent gives Range/resumable support), plus metadata. The
	// caller must Close the returned handle.
	Open(ctx context.Context, path string) (io.ReadSeekCloser, *Entry, error)
	// Put writes data at path. When overwrite is false an existing file is
	// auto-renamed ("name (1).ext") so a write never clobbers — matching the
	// old Dropbox autorename semantics.
	Put(ctx context.Context, path string, data []byte, overwrite bool) (*Entry, error)
	// Stat returns metadata for a single file or folder.
	Stat(ctx context.Context, path string) (*Entry, error)
	// Delete removes a file (or empty folder); removing the root is rejected.
	Delete(ctx context.Context, path string) error
	// Mkdir creates a folder at path (parents included). An existing folder is
	// returned as-is (not an error); the root is rejected.
	Mkdir(ctx context.Context, path string) (*Entry, error)
	// Move renames/moves src to dst. A rename is a move within the same parent.
	// When dst already exists it is auto-renamed (same anti-clobber rule as Put);
	// moving the root, or onto the root, is rejected. The moved Entry is returned.
	Move(ctx context.Context, src, dst string) (*Entry, error)
}

// FormatEntries renders entries as a Markdown list for chat display. Output is
// kept byte-identical to the former dropbox.FormatEntries so the chat tool's
// rendering does not regress when the backend switches.
func FormatEntries(entries []Entry) string {
	if len(entries) == 0 {
		return "(항목 없음)"
	}
	var sb strings.Builder
	for _, e := range entries {
		display := e.PathDisplay
		if display == "" {
			display = e.Name
		}
		if e.IsFolder() {
			fmt.Fprintf(&sb, "- 📁 **%s**  `%s`\n", e.Name, display)
		} else {
			fmt.Fprintf(&sb, "- 📄 %s  `%s`  (%s)\n", e.Name, display, HumanSize(e.Size))
		}
	}
	return sb.String()
}

// HumanSize formats a byte count as a compact human-readable string.
func HumanSize(n int64) string {
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
