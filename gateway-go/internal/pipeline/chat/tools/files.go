package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/filestore"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/fileshare"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// FilesParams holds parsed input for the files tool.
type FilesParams struct {
	Action    string `json:"action"`
	Path      string `json:"path"`       // store path (list/download/share/analyze)
	Query     string `json:"query"`      // search query
	LocalPath string `json:"local_path"` // local file to save into the store (upload)
	DestPath  string `json:"dest_path"`  // store destination for upload
	Overwrite bool   `json:"overwrite"`  // overwrite on upload (default: autorename)
	Extract   bool   `json:"extract"`    // also extract text on download
	Recursive bool   `json:"recursive"`  // recurse into subfolders on list
	Content   bool   `json:"content"`    // search file contents too (not just names)
	Semantic  bool   `json:"semantic"`   // meaning-based (vector) search instead of substring
	Max       int    `json:"max"`        // max results (list/search)
}

// FilesSemanticSearchFunc ranks store files by meaning (BGE-M3 vectors) for the
// search action's semantic=true mode. It is injected from the server (which owns
// the embedding client + index), so the tool stays decoupled from that wiring.
// A nil func — or an unavailable embedding server returning an empty slice —
// falls back to name/content search, so semantic search is optional.
type FilesSemanticSearchFunc func(ctx context.Context, query string, max int) ([]filestore.ScoredEntry, error)

// ToolFiles implements the files tool over Deneb's local file store
// (internal/domain/filestore) — the local-disk replacement for the former
// Dropbox tool. No external API, no OAuth: the store lives under DENEB_FILES_DIR
// (default ~/.deneb/files). Extracted text is returned for the agent to reason
// over (the tool calls no LLM); share links are minted via internal/infra/fileshare.
//
// semanticSearch (optional) powers the search action's semantic=true mode by
// ranking files by meaning (BGE-M3 vectors). Nil disables semantic search
// gracefully — a semantic query then falls back to name/content matching.
func ToolFiles(semanticSearch FilesSemanticSearchFunc) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p FilesParams
		if err := jsonutil.UnmarshalInto("files params", input, &p); err != nil {
			return "", err
		}
		store, err := filestore.DefaultLocalStore()
		if err != nil {
			return fmt.Sprintf("파일 저장소를 열 수 없습니다: %s", err), nil
		}

		switch p.Action {
		case "list":
			return filesList(ctx, store, p)
		case "search", "semantic_search":
			// semantic_search is an alias for search with semantic=true.
			if p.Action == "semantic_search" {
				p.Semantic = true
			}
			return filesSearch(ctx, store, p, semanticSearch)
		case "download":
			return filesDownload(ctx, store, p)
		case "upload", "save":
			return filesUpload(ctx, store, p)
		case "share":
			return filesShare(ctx, store, p)
		case "analyze":
			return filesAnalyze(ctx, store, p)
		case "delete":
			return filesDelete(ctx, store, p)
		case "mkdir":
			return filesMkdir(ctx, store, p)
		case "move", "rename":
			return filesMove(ctx, store, p)
		default:
			return fmt.Sprintf("알 수 없는 files 액션: %q. 지원: list, search, semantic_search, download, upload, share, analyze, delete, mkdir, move", p.Action), nil
		}
	}
}

// --- list ---

func filesList(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	entries, err := store.List(ctx, p.Path, p.Recursive, p.Max)
	if err != nil {
		return "", err
	}
	loc := strings.TrimSpace(p.Path)
	if loc == "" || loc == "/" {
		loc = "/ (루트)"
	}
	return fmt.Sprintf("## 📂 파일 저장소: %s\n\n%s", loc, filestore.FormatEntries(entries)), nil
}

// --- search ---

func filesSearch(ctx context.Context, store *filestore.LocalStore, p FilesParams, semanticSearch FilesSemanticSearchFunc) (string, error) {
	if strings.TrimSpace(p.Query) == "" {
		return "", fmt.Errorf("query는 search 액션에 필수입니다")
	}
	// Semantic (meaning-based) search ranks files by vector similarity rather than
	// literal overlap. It falls back to name/content search when the embedding
	// server is down (nil func or an empty result), so it is never load-bearing.
	if p.Semantic && semanticSearch != nil {
		hits, serr := semanticSearch(ctx, p.Query, p.Max)
		if serr == nil && len(hits) > 0 {
			return formatSemanticHits(p.Query, hits), nil
		}
		// Empty or errored → fall through to lexical search (still useful offline).
	}
	// content=true widens the match to extracted file text (PDF/docx/xlsx/…),
	// not just names. The extractor lives in this package, so we inject it as the
	// callback the domain's SearchContent expects (it must not import tools).
	var entries []filestore.Entry
	var err error
	if p.Content {
		entries, err = store.SearchContent(ctx, p.Query, p.Max, func(ctx context.Context, data []byte, name string) string {
			return extractFileText(ctx, name, data)
		})
	} else {
		entries, err = store.Search(ctx, p.Query, p.Max)
	}
	if err != nil {
		return "", err
	}
	scope := "이름"
	switch {
	case p.Semantic:
		scope = "이름 (시맨틱 폴백)" // requested semantic but embedding server unavailable
	case p.Content:
		scope = "이름+내용"
	}
	if len(entries) == 0 {
		return fmt.Sprintf("검색 결과 없음 (%s): %q", scope, p.Query), nil
	}
	return fmt.Sprintf("## 🔍 파일 검색 (%s): %s\n\n%s", scope, p.Query, filestore.FormatEntries(entries)), nil
}

// formatSemanticHits renders semantic search results as a Markdown list with the
// best-matching snippet under each file, so the agent sees why each file matched.
func formatSemanticHits(query string, hits []filestore.ScoredEntry) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 🧠 시맨틱 파일 검색: %s\n\n", query)
	for _, h := range hits {
		display := h.Entry.PathDisplay
		if display == "" {
			display = h.Entry.Name
		}
		fmt.Fprintf(&sb, "- 📄 %s  `%s`  (%s, 유사도 %.2f)\n", h.Entry.Name, display, filestore.HumanSize(h.Entry.Size), h.Score)
		if s := strings.TrimSpace(h.Snippet); s != "" {
			fmt.Fprintf(&sb, "  > %s\n", truncateRunes(s, 200))
		}
	}
	return sb.String()
}

// --- download: resolve to an absolute path for send_file (no temp copy — the
// file already lives on local disk), optionally extracting text ---

func filesDownload(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path는 download 액션에 필수입니다")
	}
	abs, err := store.AbsPath(p.Path)
	if err != nil {
		return "", err
	}
	meta, err := store.Stat(ctx, p.Path)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "✓ 파일: **%s** (%s)\n경로: `%s`\n", meta.Name, filestore.HumanSize(meta.Size), abs)
	if p.Extract {
		if data, _, gerr := store.Get(ctx, p.Path); gerr == nil {
			if text := extractFileText(ctx, meta.Name, data); text != "" {
				fmt.Fprintf(&sb, "\n--- 추출된 내용 ---\n%s\n", truncateRunes(text, 50000))
			} else {
				sb.WriteString("\n(텍스트를 추출할 수 없는 형식입니다)\n")
			}
		}
	}
	sb.WriteString("\n사용자에게 파일을 보내려면 send_file 도구에 위 경로를 사용하세요.")
	return sb.String(), nil
}

// --- upload: save a local file into the store ---

func filesUpload(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.LocalPath) == "" {
		return "", fmt.Errorf("local_path는 upload 액션에 필수입니다")
	}
	dest := strings.TrimSpace(p.DestPath)
	if dest == "" {
		dest = "/" + filepath.Base(p.LocalPath)
	}
	data, err := os.ReadFile(p.LocalPath)
	if err != nil {
		return fmt.Sprintf("로컬 파일을 읽을 수 없습니다: %s", err), nil
	}
	meta, err := store.Put(ctx, dest, data, p.Overwrite)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✓ 저장 완료: `%s` (%s)", meta.PathDisplay, filestore.HumanSize(meta.Size)), nil
}

// --- share: mint a time-limited, path-scoped download link for external sharing ---

func filesShare(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path는 share 액션에 필수입니다")
	}
	meta, err := store.Stat(ctx, p.Path)
	if err != nil {
		return "", err
	}
	if meta.IsFolder() {
		return "폴더는 공유 링크를 만들 수 없습니다. 파일 경로를 지정하세요.", nil
	}
	link := fileshare.Link(meta.PathDisplay)
	if link == "" {
		return "공유 링크를 만들 수 없습니다 (게이트웨이 공개 URL 또는 클라이언트 토큰 미설정). 파일을 직접 전달하려면 send_file 도구를 사용하세요.", nil
	}
	return fmt.Sprintf("🔗 공유 링크 (7일 유효): %s", link), nil
}

// --- analyze: extract text for the agent to reason over ---

func filesAnalyze(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path는 analyze 액션에 필수입니다")
	}
	data, meta, err := store.Get(ctx, p.Path)
	if err != nil {
		return "", err
	}
	text := extractFileText(ctx, meta.Name, data)
	if text == "" {
		return fmt.Sprintf("**%s**에서 텍스트를 추출할 수 없습니다 (지원: PDF/이미지/Excel/Word/PowerPoint/텍스트).", meta.Name), nil
	}
	return fmt.Sprintf("## 📄 %s\n\n%s", meta.Name, truncateRunes(text, 50000)), nil
}

// --- delete: remove a file or empty folder ---

func filesDelete(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path는 delete 액션에 필수입니다")
	}
	if err := store.Delete(ctx, p.Path); err != nil {
		return "", err
	}
	return fmt.Sprintf("✓ 삭제 완료: `%s`", strings.TrimSpace(p.Path)), nil
}

// --- mkdir: create a folder (parents included) ---

func filesMkdir(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path는 mkdir 액션에 필수입니다")
	}
	meta, err := store.Mkdir(ctx, p.Path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✓ 폴더 생성: `%s`", meta.PathDisplay), nil
}

// --- move/rename: move src (Path) to dst (DestPath); a rename is a same-parent move ---

func filesMove(ctx context.Context, store *filestore.LocalStore, p FilesParams) (string, error) {
	if strings.TrimSpace(p.Path) == "" {
		return "", fmt.Errorf("path(원본)는 move 액션에 필수입니다")
	}
	if strings.TrimSpace(p.DestPath) == "" {
		return "", fmt.Errorf("dest_path(대상)는 move 액션에 필수입니다")
	}
	meta, err := store.Move(ctx, p.Path, p.DestPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✓ 이동 완료: `%s` → `%s`", strings.TrimSpace(p.Path), meta.PathDisplay), nil
}

// --- shared helpers ---

// extractFileText extracts text from file bytes via the shared document
// dispatcher (document_extract.go). An empty MIME type degrades to filename-only
// classification. Returns "" when the format is unsupported or extraction fails
// — except CSV, which falls back to the raw bytes (an empty/garbled CSV is still
// worth showing the agent).
func extractFileText(ctx context.Context, name string, data []byte) string {
	r := extractDocument(ctx, data, name, "")
	if r.kind == docCSV && r.err != nil {
		return string(data)
	}
	return r.text
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
