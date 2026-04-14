// ZIP archive handling for inbound Telegram documents.
//
// When a user sends a .zip file, we extract the contents, classify each entry
// (image / plain text / parseable document / opaque binary), and return one
// Attachment per useful inner file plus a tree summary header. Binary entries
// that can't be rendered as text or image are surfaced as metadata only in the
// tree summary.
//
// Safety: ZIP archives are a common vector for resource-exhaustion attacks
// (zip bombs, path traversal, symlink tricks). Every limit below is tuned to
// reject malicious archives early without crashing the gateway.
package media

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/core/coremedia"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/liteparse"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

// ZIP safety limits. Tuned for typical project/source archives (<= few hundred
// files, source + small assets). Malicious archives exceeding these bounds are
// truncated with a warning included in the tree summary.
const (
	maxZipDownloadSize        = 50 * 1024 * 1024  // 50 MB compressed (Telegram bot max)
	maxZipEntries             = 1000              // entry count cap
	maxZipExtractedTotalBytes = 200 * 1024 * 1024 // 200 MB uncompressed total
	maxZipPerEntryBytes       = 50 * 1024 * 1024  // 50 MB per entry
	maxZipCompressionRatio    = 100               // uncompressed/compressed > 100 is bomb-like
	maxZipAttachments         = 20                // attachments returned to the LLM
	maxZipPerTextAttachment   = 64 * 1024         // 64 KB per text entry
	maxZipTotalTextBytes      = 512 * 1024        // 512 KB total text across all entries
	maxZipTreeListing         = 200               // entries listed in the tree summary body
)

// isZipDocument returns true when the Telegram Document is a ZIP archive we
// should try to open. Some clients send Documents with no MIME or the generic
// application/octet-stream, so we also fall back to the .zip extension.
func isZipDocument(d *telegram.Document) bool {
	if d == nil {
		return false
	}
	mime := strings.ToLower(strings.TrimSpace(d.MimeType))
	switch mime {
	case "application/zip",
		"application/x-zip",
		"application/x-zip-compressed",
		"application/zip-compressed":
		return true
	}
	return strings.HasSuffix(strings.ToLower(d.FileName), ".zip")
}

// extractZipAttachments downloads a ZIP document, classifies each entry, and
// returns a slice of Attachments ready for LLM consumption. The first entry is
// always a tree summary describing the archive layout.
//
// Never returns an error: any failure produces a single tree-summary attachment
// that explains what went wrong so the agent can tell the user.
func extractZipAttachments(ctx context.Context, client *telegram.Client, d *telegram.Document, logger *slog.Logger) []Attachment {
	if d.FileSize > 0 && d.FileSize > maxZipDownloadSize {
		logger.Warn("skipping oversized zip", "fileId", d.FileID, "size", d.FileSize)
		return []Attachment{zipErrorSummary(d.FileName, d.FileSize, fmt.Sprintf("압축 파일이 너무 큽니다 (%s, 최대 %s).",
			humanSize(d.FileSize), humanSize(maxZipDownloadSize)))}
	}

	data, _, err := client.DownloadFile(ctx, d.FileID)
	if err != nil {
		logger.Warn("failed to download zip", "fileId", d.FileID, "error", err)
		return []Attachment{zipErrorSummary(d.FileName, d.FileSize, "압축 파일을 다운로드하지 못했습니다.")}
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		logger.Warn("failed to open zip", "fileName", d.FileName, "error", err)
		return []Attachment{zipErrorSummary(d.FileName, d.FileSize, "압축 파일을 열지 못했습니다 (손상되었거나 지원되지 않는 형식).")}
	}

	return processZipEntries(ctx, d.FileName, int64(len(data)), reader, logger)
}

// zipEntryRecord captures metadata + any extracted attachment for a single
// zip entry. Used to build the tree summary after classification.
type zipEntryRecord struct {
	path          string
	size          int64 // uncompressed
	note          string
	isDir         bool
	hasAttachment bool
}

// processZipEntries walks the archive, classifies entries, and assembles the
// final Attachment slice (tree summary first, then each extracted entry).
func processZipEntries(ctx context.Context, archiveName string, archiveSize int64, reader *zip.Reader, logger *slog.Logger) []Attachment {
	var (
		records      []zipEntryRecord
		attachments  []Attachment
		totalBytes   int64
		totalText    int
		processedAtt int
		skipped      int
	)

	entries := reader.File
	if len(entries) > maxZipEntries {
		logger.Warn("zip has too many entries, truncating", "fileName", archiveName, "entries", len(entries))
		entries = entries[:maxZipEntries]
		skipped += len(reader.File) - maxZipEntries
	}

	// Deterministic order: callers (and tests) shouldn't depend on zip
	// central-directory ordering.
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	for _, f := range entries {
		rec := zipEntryRecord{path: cleanZipPath(f.Name), size: safeUint64ToInt64(f.UncompressedSize64)}

		// Zip spec: directory entries end with "/". Cheaper than f.FileInfo().IsDir()
		// which allocates a headerFileInfo value.
		if strings.HasSuffix(f.Name, "/") {
			rec.isDir = true
			records = append(records, rec)
			continue
		}
		if rec.path == "" {
			skipped++
			continue // path traversal or empty after cleaning
		}
		// archive/zip doesn't expose an IsEncrypted helper; the general-purpose
		// bit flag (bit 0) is set on encrypted entries per PKZIP APPNOTE.TXT.
		if f.Flags&0x1 != 0 {
			rec.note = "암호 보호"
			records = append(records, rec)
			skipped++
			continue
		}
		if rec.size > maxZipPerEntryBytes {
			rec.note = fmt.Sprintf("너무 큼 %s", humanSize(rec.size))
			records = append(records, rec)
			skipped++
			continue
		}
		if totalBytes+rec.size > maxZipExtractedTotalBytes {
			rec.note = "전체 해제 한도 초과"
			records = append(records, rec)
			skipped++
			continue
		}

		// Bomb heuristic: refuse entries whose declared expansion ratio is
		// absurd. f.CompressedSize64 is 0 for the entry's local header when
		// uncompressed (STORE) — skip the ratio check in that case.
		if f.CompressedSize64 > 0 {
			ratio := rec.size / safeUint64ToInt64(f.CompressedSize64)
			if ratio > maxZipCompressionRatio {
				rec.note = fmt.Sprintf("압축률 의심 %d:1", ratio)
				records = append(records, rec)
				skipped++
				continue
			}
		}

		if processedAtt >= maxZipAttachments {
			rec.note = "첨부 한도 초과"
			records = append(records, rec)
			skipped++
			continue
		}

		// Budget pre-check: if this entry is almost certainly text (by
		// extension) and the cumulative text cap is already blown, skip the
		// read entirely instead of expanding 64 KB just to throw it away.
		if totalText >= maxZipTotalTextBytes && hasTextExt(rec.path) {
			rec.note = "텍스트 한도 초과"
			records = append(records, rec)
			skipped++
			continue
		}

		body, readErr := readZipEntry(f, rec.size)
		if readErr != nil {
			rec.note = "읽기 실패"
			records = append(records, rec)
			skipped++
			logger.Warn("zip entry read failed", "entry", rec.path, "error", readErr)
			continue
		}
		totalBytes += int64(len(body))

		att, note := classifyZipEntry(ctx, archiveName, rec.path, body, logger)
		if att != nil {
			if att.Type == AttachmentTypeDocumentText {
				// Cumulative text cap: the first few entries fit verbatim;
				// later ones get truncated or skipped so one huge README
				// can't starve the rest of the archive's context budget.
				remaining := maxZipTotalTextBytes - totalText
				if remaining <= 0 {
					rec.note = "텍스트 한도 초과"
					records = append(records, rec)
					skipped++
					continue
				}
				if len(att.Data) > remaining {
					att.Data = truncateUTF8(att.Data, remaining) + "\n\n[... 압축파일 전체 텍스트 한도 도달]"
				}
				totalText += len(att.Data)
			}
			rec.hasAttachment = true
			if note != "" {
				rec.note = note
			}
			attachments = append(attachments, *att)
			processedAtt++
		} else {
			rec.note = note
			skipped++
		}
		records = append(records, rec)
	}

	summary := buildZipTreeSummary(archiveName, archiveSize, records, skipped)
	out := make([]Attachment, 0, len(attachments)+1)
	out = append(out, summary)
	out = append(out, attachments...)
	return out
}

// readZipEntry opens and reads a single file entry with a size-limited reader.
// size is the declared uncompressed size from the central directory.
func readZipEntry(f *zip.File, size int64) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()

	// LimitReader guards against compressed-size lying about the real payload;
	// +1 lets us detect an overflow.
	limit := size
	if limit <= 0 || limit > maxZipPerEntryBytes {
		limit = maxZipPerEntryBytes
	}
	return io.ReadAll(io.LimitReader(rc, limit+1))
}

// classifyZipEntry turns one entry's bytes into an Attachment or explains why
// it was skipped. The returned note is non-empty only when classification
// decided to emit a tree-summary note (either on success or on skip).
func classifyZipEntry(ctx context.Context, archiveName, entryPath string, body []byte, logger *slog.Logger) (att *Attachment, note string) {
	if len(body) == 0 {
		return nil, "빈 파일"
	}

	displayName := entryPath
	if archiveName != "" {
		displayName = archiveName + "/" + entryPath
	}

	// Fast path: if the extension strongly implies text, skip magic-byte
	// sniffing — common in source-heavy archives where 90%+ of entries are
	// .go/.py/.md and DetectMIME would scan each body for no reason.
	if hasTextExt(entryPath) && !looksBinary(body) {
		return newTextAttachment(displayName, "text/plain", string(body), int64(len(body)), "파일")
	}

	mime := coremedia.DetectMIME(body)

	if strings.HasPrefix(mime, "image/") {
		if int64(len(body)) > maxImageDownloadSize {
			return nil, fmt.Sprintf("이미지 너무 큼 %s", humanSize(int64(len(body))))
		}
		return &Attachment{
			Type:     AttachmentTypeImage,
			MimeType: mime,
			Data:     base64.StdEncoding.EncodeToString(body),
			Name:     displayName,
			Size:     int64(len(body)),
		}, ""
	}

	// Unknown-extension entries that still look textual (e.g. a Dockerfile
	// without extension). looksBinary + isUTF8 only fire on this branch so
	// typical source archives pay for them zero times.
	if !looksBinary(body) && isUTF8(body) {
		return newTextAttachment(displayName, "text/plain", string(body), int64(len(body)), "파일")
	}

	if liteparse.Available() && liteparse.SupportedMIME(mime) {
		text, err := liteparse.Parse(ctx, body, path.Base(entryPath))
		if err != nil {
			logger.Warn("liteparse failed on zip entry", "entry", entryPath, "error", err)
			return nil, "파싱 실패"
		}
		if strings.TrimSpace(text) == "" {
			return nil, "빈 문서"
		}
		return newTextAttachment(displayName, mime, text, int64(len(body)), "문서")
	}

	return nil, fmt.Sprintf("바이너리 %s", shortMIME(mime))
}

// newTextAttachment builds a document_text Attachment, truncating at the
// per-entry cap and appending a Korean notice describing what was clipped.
// label distinguishes the truncation message ("파일" for raw text, "문서" for
// liteparse output) so the LLM knows how the content was produced.
func newTextAttachment(displayName, mime, text string, rawSize int64, label string) (att *Attachment, note string) {
	if len(text) > maxZipPerTextAttachment {
		text = truncateUTF8(text, maxZipPerTextAttachment) + "\n\n[... " + label + "이 너무 길어 잘렸습니다]"
	}
	if strings.TrimSpace(text) == "" {
		return nil, "빈 " + label
	}
	return &Attachment{
		Type:     AttachmentTypeDocumentText,
		MimeType: mime,
		Data:     text,
		Name:     displayName,
		Size:     rawSize,
	}, ""
}

// hasTextExt reports whether a path ends in an extension that is almost
// always plain text (source code, config, markup). Used to short-circuit
// MIME detection and the cumulative text budget check.
func hasTextExt(entryPath string) bool {
	return textExtensions[strings.ToLower(path.Ext(entryPath))]
}

// textExtensions enumerates file extensions that are almost always text.
// Kept explicit so we don't accidentally render, say, .exe as text just
// because the first few hundred bytes pass the UTF-8 check.
var textExtensions = map[string]bool{
	".txt": true, ".md": true, ".markdown": true, ".rst": true,
	".csv": true, ".tsv": true, ".log": true,
	".json": true, ".json5": true, ".yaml": true, ".yml": true,
	".toml": true, ".ini": true, ".conf": true, ".cfg": true, ".env": true,
	".go": true, ".py": true, ".rb": true, ".rs": true, ".js": true, ".mjs": true,
	".ts": true, ".tsx": true, ".jsx": true, ".java": true, ".kt": true, ".kts": true,
	".c": true, ".cc": true, ".cpp": true, ".cxx": true, ".h": true, ".hpp": true,
	".m": true, ".mm": true, ".swift": true, ".scala": true, ".clj": true,
	".sh": true, ".bash": true, ".zsh": true, ".fish": true, ".ps1": true,
	".sql": true, ".lua": true, ".pl": true, ".r": true, ".php": true,
	".html": true, ".htm": true, ".css": true, ".scss": true, ".sass": true, ".less": true,
	".xml": true, ".svg": true, ".vue": true, ".svelte": true,
	".gradle": true, ".mk": true, ".makefile": true,
	".dockerfile": true, ".gitignore": true, ".editorconfig": true,
}

// looksBinary is a fast heuristic: a NUL byte in the first 4 KB is a strong
// signal the file is binary.
func looksBinary(data []byte) bool {
	limit := len(data)
	if limit > 4096 {
		limit = 4096
	}
	for i := range limit {
		if data[i] == 0 {
			return true
		}
	}
	return false
}

// safeUint64ToInt64 converts a uint64 field (zip central-directory sizes) to
// int64 with saturation. Zip headers are capped at ZIP64 max, so the real-world
// values we see here fit in int64 easily, but gosec flags the naked conversion.
func safeUint64ToInt64(n uint64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	if n > uint64(maxInt64) {
		return maxInt64
	}
	return int64(n)
}

// isUTF8 checks whether the first 4 KB decodes as valid UTF-8.
func isUTF8(data []byte) bool {
	limit := len(data)
	if limit > 4096 {
		limit = 4096
	}
	// If we hard-cut at 4 KB, the final bytes may be an incomplete multibyte
	// rune. Walk back past any trailing continuation bytes AND the leading
	// byte that started them so utf8.Valid doesn't false-negative on a
	// perfectly-valid file that happened to split a rune at the boundary.
	if limit < len(data) {
		for limit > 0 {
			b := data[limit-1]
			if b < 0x80 {
				break // ASCII — safe boundary
			}
			limit--
			if b&0xC0 == 0xC0 {
				break // leading byte removed; the rune is now fully dropped
			}
		}
	}
	return utf8.Valid(data[:limit])
}

// truncateUTF8 returns s clipped to at most n bytes without splitting a
// multibyte rune at the boundary. Unlike a naive slice, this never produces
// invalid UTF-8 even when the cut point falls inside a rune.
func truncateUTF8(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	end := n
	for end > 0 && !utf8.RuneStart(s[end]) {
		end--
	}
	return s[:end]
}

// cleanZipPath rejects absolute paths and path-traversal ("..") entries.
// Returns the cleaned path or "" if the entry should be skipped.
func cleanZipPath(name string) string {
	// Normalize separators: some Windows zips use backslashes.
	name = strings.ReplaceAll(name, "\\", "/")
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") {
		return ""
	}
	cleaned := path.Clean(name)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return ""
	}
	return cleaned
}

// buildZipTreeSummary constructs the header attachment that shows the archive
// layout, per-entry size, and any skip reasons. This is what the LLM reads to
// understand the full shape of the archive even when not all entries were
// extracted.
func buildZipTreeSummary(archiveName string, archiveSize int64, records []zipEntryRecord, skipped int) Attachment {
	var b strings.Builder

	name := archiveName
	if name == "" {
		name = "archive.zip"
	}
	fmt.Fprintf(&b, "압축 파일: %s\n", name)
	fmt.Fprintf(&b, "크기: %s (압축)\n", humanSize(archiveSize))

	totalFiles := 0
	var totalUncompressed int64
	for _, r := range records {
		if r.isDir {
			continue
		}
		totalFiles++
		totalUncompressed += r.size
	}
	fmt.Fprintf(&b, "항목: %d개, 해제 후 %s\n", totalFiles, humanSize(totalUncompressed))
	if skipped > 0 {
		fmt.Fprintf(&b, "건너뜀: %d개\n", skipped)
	}
	b.WriteString("\n내용:\n")

	// Simple flat listing is more reliable than a tree renderer for deeply
	// nested archives and still reads naturally for the LLM.
	shown := 0
	for _, r := range records {
		if r.isDir {
			continue
		}
		if shown >= maxZipTreeListing {
			fmt.Fprintf(&b, "... %d개 항목 더 있음\n", totalFiles-shown)
			break
		}
		shown++
		fmt.Fprintf(&b, "- %s (%s)", r.path, humanSize(r.size))
		switch {
		case r.hasAttachment:
			b.WriteString(" [본문 첨부]")
		case r.note != "":
			b.WriteString(" [")
			b.WriteString(r.note)
			b.WriteString("]")
		}
		b.WriteByte('\n')
	}

	return Attachment{
		Type:     AttachmentTypeDocumentText,
		MimeType: "text/plain",
		Data:     b.String(),
		Name:     name + " (목록)",
		Size:     archiveSize,
	}
}

// zipErrorSummary builds a minimal tree-summary Attachment describing why the
// archive couldn't be processed at all. Lets the agent apologize to the user
// in Korean instead of silently dropping the message.
func zipErrorSummary(archiveName string, archiveSize int64, reason string) Attachment {
	name := archiveName
	if name == "" {
		name = "archive.zip"
	}
	body := fmt.Sprintf("압축 파일: %s\n처리 실패: %s\n", name, reason)
	return Attachment{
		Type:     AttachmentTypeDocumentText,
		MimeType: "text/plain",
		Data:     body,
		Name:     name + " (오류)",
		Size:     archiveSize,
	}
}

// humanSize formats a byte count as a compact human-readable string
// (e.g. "3.2 KB", "12 MB"). Used in the tree summary.
func humanSize(n int64) string {
	switch {
	case n <= 0:
		return "0 B"
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	case n < 1024*1024*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
	default:
		return fmt.Sprintf("%.1f GB", float64(n)/(1024*1024*1024))
	}
}

// shortMIME returns the subtype portion of a MIME string for compact display
// in the tree summary ("application/octet-stream" -> "octet-stream").
func shortMIME(mime string) string {
	if mime == "" {
		return "binary"
	}
	if idx := strings.IndexByte(mime, '/'); idx >= 0 && idx+1 < len(mime) {
		return mime[idx+1:]
	}
	return mime
}
