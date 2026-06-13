package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/dropbox"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// DropboxParams holds parsed input for the dropbox tool.
type DropboxParams struct {
	Action    string `json:"action"`
	Path      string `json:"path"`       // Dropbox path (list/download/share/analyze)
	Query     string `json:"query"`      // search query
	LocalPath string `json:"local_path"` // local file to upload
	DestPath  string `json:"dest_path"`  // Dropbox destination for upload
	Overwrite bool   `json:"overwrite"`  // overwrite on upload (default: autorename)
	Extract   bool   `json:"extract"`    // also extract text on download
	Recursive bool   `json:"recursive"`  // recurse into subfolders on list
	Max       int    `json:"max"`        // max results (list/search)
	Target    string `json:"target"`     // backup target: wiki|weekly|transcripts|all
}

// ToolDropbox implements the dropbox tool for file management, document
// analysis, and artifact backup via the native Dropbox v2 API. Extracted text
// is returned to the agent, which reasons over it — the tool calls no LLM
// itself, so it needs no pipeline deps.
func ToolDropbox() ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p DropboxParams
		if err := jsonutil.UnmarshalInto("dropbox params", input, &p); err != nil {
			return "", err
		}

		client, err := dropbox.DefaultClient()
		if err != nil {
			return fmt.Sprintf("Dropbox 인증 정보를 찾을 수 없습니다: %s\n게이트웨이 호스트에서 `go run ./cmd/deneb-dropbox-auth`로 연동을 1회 설정하세요.", err), nil
		}

		// Normalize the path once so every action sees a leading-slash path (or
		// "" for the list root) — no per-action normalize drift.
		p.Path = normalizeDropboxPath(p.Path)

		switch p.Action {
		case "list":
			return dropboxList(ctx, client, p)
		case "search":
			return dropboxSearch(ctx, client, p)
		case "download":
			return dropboxDownload(ctx, client, p)
		case "upload":
			return dropboxUpload(ctx, client, p)
		case "share":
			return dropboxShare(ctx, client, p)
		case "analyze":
			return dropboxAnalyze(ctx, client, p)
		case "backup":
			return dropboxBackup(ctx, client, p)
		default:
			return fmt.Sprintf("알 수 없는 dropbox 액션: %q. 지원: list, search, download, upload, share, analyze, backup", p.Action), nil
		}
	}
}

// --- list ---

func dropboxList(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	entries, err := client.ListFolder(ctx, p.Path, p.Recursive, p.Max)
	if err != nil {
		return "", err
	}
	loc := p.Path
	if loc == "" || loc == "/" {
		loc = "/ (루트)"
	}
	return fmt.Sprintf("## 📂 Dropbox: %s\n\n%s", loc, dropbox.FormatEntries(entries)), nil
}

// --- search ---

func dropboxSearch(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	if p.Query == "" {
		return "", fmt.Errorf("query는 search 액션에 필수입니다")
	}
	entries, err := client.Search(ctx, p.Query, p.Max)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return fmt.Sprintf("검색 결과 없음: %q", p.Query), nil
	}
	return fmt.Sprintf("## 🔍 Dropbox 검색: %s\n\n%s", p.Query, dropbox.FormatEntries(entries)), nil
}

// --- download (optionally extract text) ---

func dropboxDownload(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	if p.Path == "" {
		return "", fmt.Errorf("path는 download 액션에 필수입니다")
	}
	data, meta, err := client.Download(ctx, p.Path)
	if err != nil {
		return "", err
	}
	name := dropboxBaseName(p.Path, meta)
	localPath, err := saveDropboxFile(name, data)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "✓ 다운로드 완료: **%s** (%s)\n저장 위치: `%s`\n", name, dropbox.HumanSize(int64(len(data))), localPath)
	if p.Extract {
		text := extractDropboxFileText(ctx, name, data)
		if text != "" {
			fmt.Fprintf(&sb, "\n--- 추출된 내용 ---\n%s\n", truncateRunes(text, 50000))
		} else {
			sb.WriteString("\n(텍스트를 추출할 수 없는 형식입니다)\n")
		}
	}
	sb.WriteString("\n사용자에게 파일을 보내려면 send_file 도구에 위 저장 경로를 사용하세요.")
	return sb.String(), nil
}

// --- upload ---

func dropboxUpload(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	if p.LocalPath == "" {
		return "", fmt.Errorf("local_path는 upload 액션에 필수입니다")
	}
	dest := normalizeDropboxPath(p.DestPath)
	if dest == "" {
		dest = "/" + filepath.Base(p.LocalPath)
	}
	data, err := os.ReadFile(p.LocalPath)
	if err != nil {
		return fmt.Sprintf("로컬 파일을 읽을 수 없습니다: %s", err), nil
	}
	meta, err := client.Upload(ctx, dest, data, p.Overwrite)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✓ 업로드 완료: `%s` (%s)", meta.PathDisplay, dropbox.HumanSize(meta.Size)), nil
}

// --- share ---

func dropboxShare(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	if p.Path == "" {
		return "", fmt.Errorf("path는 share 액션에 필수입니다")
	}
	link, err := client.CreateSharedLink(ctx, p.Path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("🔗 공유 링크: %s", link), nil
}

// --- analyze: download + extract text for the agent to reason over ---

func dropboxAnalyze(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	if p.Path == "" {
		return "", fmt.Errorf("path는 analyze 액션에 필수입니다")
	}
	data, meta, err := client.Download(ctx, p.Path)
	if err != nil {
		return "", err
	}
	name := dropboxBaseName(p.Path, meta)
	text := extractDropboxFileText(ctx, name, data)
	if text == "" {
		return fmt.Sprintf("**%s**에서 텍스트를 추출할 수 없습니다 (지원: PDF/이미지/Excel/Word/PowerPoint/텍스트).", name), nil
	}
	return fmt.Sprintf("## 📄 %s\n\n%s", name, truncateRunes(text, 50000)), nil
}

// --- backup: upload Deneb artifacts to /Deneb-Backup/<date>/ ---

const maxBackupFiles = 1000

type backupItem struct {
	localPath string
	relDest   string // path relative to the dated backup folder
}

func dropboxBackup(ctx context.Context, client *dropbox.Client, p DropboxParams) (string, error) {
	target := strings.ToLower(strings.TrimSpace(p.Target))
	if target == "" {
		target = "all"
	}

	var items []backupItem
	switch target {
	case "wiki":
		items = collectBackupFiles(wiki.ConfigFromEnv().Dir, "wiki", []string{".md"})
	case "weekly":
		items = collectBackupFiles(weeklyOutputDir(), "weekly", []string{".pdf", ".png", ".html", ".json"})
	case "transcripts":
		items = collectBackupFiles(transcriptsDir(), "transcripts", []string{".jsonl"})
	case "all":
		items = append(items, collectBackupFiles(wiki.ConfigFromEnv().Dir, "wiki", []string{".md"})...)
		items = append(items, collectBackupFiles(weeklyOutputDir(), "weekly", []string{".pdf", ".png", ".html", ".json"})...)
		items = append(items, collectBackupFiles(transcriptsDir(), "transcripts", []string{".jsonl"})...)
	default:
		return fmt.Sprintf("알 수 없는 backup 대상: %q. 지원: wiki, weekly, transcripts, all", target), nil
	}

	if len(items) == 0 {
		return fmt.Sprintf("백업할 파일이 없습니다 (대상: %s).", target), nil
	}

	baseDest := "/Deneb-Backup/" + time.Now().Format("2006-01-02")
	uploaded, skipped, truncated := 0, 0, false
	var failures []string
	for _, it := range items {
		if uploaded >= maxBackupFiles {
			truncated = true
			break
		}
		data, err := os.ReadFile(it.localPath)
		if err != nil {
			skipped++
			continue
		}
		dest := baseDest + "/" + it.relDest
		if _, err := client.Upload(ctx, dest, data, true); err != nil {
			skipped++
			if len(failures) < 5 {
				failures = append(failures, fmt.Sprintf("%s: %s", it.relDest, err))
			}
			continue
		}
		uploaded++
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "## ☁️ Dropbox 백업 완료 (%s)\n\n", target)
	fmt.Fprintf(&sb, "- 대상 폴더: `%s`\n", baseDest)
	fmt.Fprintf(&sb, "- 업로드: %d개", uploaded)
	if skipped > 0 {
		fmt.Fprintf(&sb, ", 건너뜀: %d개", skipped)
	}
	sb.WriteString("\n")
	if truncated {
		fmt.Fprintf(&sb, "- ⚠️ 파일이 %d개 상한을 초과해 일부만 백업했습니다.\n", maxBackupFiles)
	}
	for _, f := range failures {
		fmt.Fprintf(&sb, "  - 실패: %s\n", f)
	}
	return sb.String(), nil
}

// collectBackupFiles walks root and returns files matching exts (empty = all),
// each tagged with a destination path of "<label>/<relative path>".
func collectBackupFiles(root, label string, exts []string) []backupItem {
	if root == "" {
		return nil
	}
	var items []backupItem
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil //nolint:nilerr // skip unreadable entries, keep walking
		}
		if len(exts) > 0 && !hasAnyExt(path, exts) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = filepath.Base(path)
		}
		items = append(items, backupItem{localPath: path, relDest: label + "/" + filepath.ToSlash(rel)})
		return nil
	})
	return items
}

// --- shared helpers ---

// extractDropboxFileText extracts text from downloaded bytes by extension,
// reusing the gmail attachment extractors (same package). Returns "" when the
// format is unsupported or extraction fails.
func extractDropboxFileText(ctx context.Context, name string, data []byte) string {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".pdf"):
		if text, err := pdfToText(ctx, data); err == nil && strings.TrimSpace(text) != "" {
			return text
		}
		// Scanned PDF: fall back to per-page OCR.
		if text, err := pdfOCR(ctx, data); err == nil {
			return text
		}
		return ""
	case strings.HasSuffix(lower, ".xlsx"):
		if text, err := xlsxToText(data); err == nil {
			return text
		}
		return ""
	case strings.HasSuffix(lower, ".docx"):
		if text, err := docxToText(data); err == nil {
			return text
		}
		return ""
	case strings.HasSuffix(lower, ".pptx"):
		if text, err := pptxToText(data); err == nil {
			return text
		}
		return ""
	case hasImageExt(lower):
		if text, err := imageOCR(ctx, data); err == nil {
			return text
		}
		return ""
	case strings.HasSuffix(lower, ".csv"):
		if text, err := csvToMarkdown(data); err == nil {
			return text
		}
		return string(data)
	case isTextFile(lower):
		return string(data)
	default:
		return ""
	}
}

// saveDropboxFile writes downloaded bytes to a temp file so the agent can hand
// the path to send_file. The name is sanitized to its base component.
func saveDropboxFile(name string, data []byte) (string, error) {
	dir := filepath.Join(os.TempDir(), "deneb-dropbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "download"
	}
	path := filepath.Join(dir, base)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// normalizeDropboxPath ensures a non-empty path starts with "/". Empty stays
// empty (the list root).
func normalizeDropboxPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" || p == "/" {
		return strings.TrimSpace(p)
	}
	if !strings.HasPrefix(p, "/") {
		return "/" + p
	}
	return p
}

// dropboxBaseName picks a display filename from the path or downloaded metadata.
func dropboxBaseName(path string, meta *dropbox.Entry) string {
	if meta != nil && meta.Name != "" {
		return meta.Name
	}
	return filepath.Base(path)
}

// transcriptsDir returns ~/.deneb/transcripts (session transcript root).
func transcriptsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".deneb", "transcripts")
}

// hasAnyExt reports whether path ends with one of the lowercase extensions.
func hasAnyExt(path string, exts []string) bool {
	lower := strings.ToLower(path)
	for _, ext := range exts {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// truncateRunes caps s to maxRunes on a rune boundary so Korean text is never
// split mid-character.
func truncateRunes(s string, maxRunes int) string {
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes]) + "\n... (이하 생략)"
}
