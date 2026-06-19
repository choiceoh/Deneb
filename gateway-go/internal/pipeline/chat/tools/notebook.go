package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/notebook"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// notebookBriefByteBudget is the starting total bytes the brief spends on source
// texts combined; notebookBriefMaxBytes is the hard cap on the FINAL marshaled
// JSON (kept under the tool's 24KB byte cap, which is enforced by head/tail
// truncation that would corrupt the JSON the model must parse). notebookBrief
// re-encodes under a shrinking budget until the marshaled output fits the cap.
const (
	notebookBriefByteBudget = 18000
	notebookBriefMaxBytes   = 23000
	notebookMinSourceBytes  = 200
)

// briefSource is one source as presented to the model in a brief.
type briefSource struct {
	Cite  string `json:"cite"`
	Kind  string `json:"kind"`
	Ref   string `json:"ref,omitempty"`
	Title string `json:"title,omitempty"`
	Text  string `json:"text"`
	Note  string `json:"note,omitempty"` // read error / staleness / truncation marker
}

// ToolNotebook returns the notebook tool — NotebookLM-style scoped source
// collections for grounded, cited synthesis (the "이 딜 자료만으로 브리핑" path).
//
// Actions: create / list / show / add_source / remove_source / delete / brief.
// brief gathers every pinned source's content and returns structured JSON for
// the LLM to compose a grounded briefing that cites each claim inline ([S1]…).
func ToolNotebook(d *toolctx.NotebookDeps) toolctx.ToolFunc {
	return func(_ context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action      string `json:"action"`
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Kind        string `json:"kind"`
			Ref         string `json:"ref"`
			Title       string `json:"title"`
			Text        string `json:"text"`
			Source      string `json:"source"` // cite tag for remove_source
			Focus       string `json:"focus"`  // optional brief focus
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if d == nil || d.Store == nil {
			return "노트북이 비활성 상태입니다.", nil
		}

		switch p.Action {
		case "create":
			return notebookCreate(d, p.Name, p.Description)
		case "list":
			return notebookList(d), nil
		case "show":
			return notebookShow(d, p.ID)
		case "add_source":
			return notebookAddSource(d, p.ID, p.Kind, p.Ref, p.Title, p.Text)
		case "remove_source":
			return notebookRemoveSource(d, p.ID, p.Source)
		case "delete":
			return notebookDelete(d, p.ID)
		case "brief":
			return notebookBrief(d, p.ID, p.Focus)
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: create, list, show, add_source, remove_source, delete, brief", p.Action), nil
		}
	}
}

func notebookCreate(d *toolctx.NotebookDeps, name, description string) (string, error) {
	nb, err := d.Store.Create(name, description)
	if err != nil {
		return fmt.Sprintf("노트북 생성 실패: %v", err), nil
	}
	return fmt.Sprintf("노트북 생성: %q (id=%s). `notebook(action=\"add_source\", id=%q, ...)` 로 자료를 핀하세요.", nb.Name, nb.ID, nb.ID), nil
}

func notebookList(d *toolctx.NotebookDeps) string {
	nbs := d.Store.List()
	if len(nbs) == 0 {
		return "노트북 없음. `notebook(action=\"create\", name=\"...\")` 로 만드세요."
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## 노트북 (%d)\n\n", len(nbs))
	for _, nb := range nbs {
		desc := ""
		if nb.Description != "" {
			desc = " — " + nb.Description
		}
		fmt.Fprintf(&sb, "- %s (id=%s, 자료 %d건)%s\n", nb.Name, nb.ID, len(nb.Sources), desc)
	}
	return sb.String()
}

func notebookShow(d *toolctx.NotebookDeps, id string) (string, error) {
	nb, ok := d.Store.Get(id)
	if !ok {
		return notebookNotFound(id), nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "## %s (id=%s)\n\n", nb.Name, nb.ID)
	if nb.Description != "" {
		sb.WriteString(nb.Description + "\n\n")
	}
	if len(nb.Sources) == 0 {
		sb.WriteString("아직 핀된 자료가 없습니다.\n")
		return sb.String(), nil
	}
	fmt.Fprintf(&sb, "### 자료 (%d건)\n\n", len(nb.Sources))
	for _, src := range nb.Sources {
		label := src.Title
		if label == "" {
			label = src.Ref
		}
		switch src.Kind {
		case notebook.KindWiki:
			fmt.Fprintf(&sb, "- [%s] wiki: %s\n", src.Cite, firstNonEmpty(label, src.Ref))
		case notebook.KindNote:
			fmt.Fprintf(&sb, "- [%s] note: %s\n", src.Cite, firstNonEmpty(label, snippet(src.Text, 60)))
		default:
			fmt.Fprintf(&sb, "- [%s] %s: %s\n", src.Cite, src.Kind, label)
		}
	}
	sb.WriteString("\n`notebook(action=\"brief\", id=\"" + nb.ID + "\")` 로 근거 기반 브리핑을 생성하세요.")
	return sb.String(), nil
}

func notebookAddSource(d *toolctx.NotebookDeps, id, kind, ref, title, text string) (string, error) {
	kind = strings.TrimSpace(kind)
	// For a wiki source with no explicit title, resolve the page title so the
	// listing/brief is readable without a second read.
	if kind == notebook.KindWiki && strings.TrimSpace(title) == "" && d.Wiki != nil {
		if page, err := d.Wiki.ReadPage(normalizeWikiRef(ref)); err == nil && page != nil && page.Meta.Title != "" {
			title = page.Meta.Title
		}
	}
	src, err := d.Store.AddSource(id, notebook.Source{Kind: kind, Ref: ref, Title: title, Text: text})
	if err != nil {
		if errors.Is(err, notebook.ErrNotFound) {
			return notebookNotFound(id), nil
		}
		return fmt.Sprintf("자료 추가 실패: %v", err), nil
	}
	label := firstNonEmpty(src.Title, src.Ref, snippet(src.Text, 60))
	return fmt.Sprintf("자료 핀 완료: [%s] %s (%s)", src.Cite, label, src.Kind), nil
}

func notebookRemoveSource(d *toolctx.NotebookDeps, id, cite string) (string, error) {
	if strings.TrimSpace(cite) == "" {
		return "source에 제거할 자료의 인용 태그(예: S2)를 지정하세요.", nil
	}
	if err := d.Store.RemoveSource(id, cite); err != nil {
		if errors.Is(err, notebook.ErrNotFound) {
			return notebookNotFound(id), nil
		}
		return fmt.Sprintf("자료 제거 실패: %v", err), nil
	}
	return fmt.Sprintf("자료 제거 완료: %s", cite), nil
}

func notebookDelete(d *toolctx.NotebookDeps, id string) (string, error) {
	if err := d.Store.Delete(id); err != nil {
		if errors.Is(err, notebook.ErrNotFound) {
			return notebookNotFound(id), nil
		}
		return fmt.Sprintf("노트북 삭제 실패: %v", err), nil
	}
	return fmt.Sprintf("노트북 삭제 완료: %s", id), nil
}

// notebookBrief gathers all pinned source contents and returns structured JSON.
// The model composes the actual briefing on its turn — grounded ONLY in these
// sources, citing each claim with the source's [S#] tag (the morning_letter
// pattern: tool returns data, LLM synthesizes).
func notebookBrief(d *toolctx.NotebookDeps, id, focus string) (string, error) {
	nb, ok := d.Store.Get(id)
	if !ok {
		return notebookNotFound(id), nil
	}
	if len(nb.Sources) == 0 {
		return fmt.Sprintf("노트북 %q에 핀된 자료가 없어 브리핑을 만들 수 없습니다. add_source로 먼저 자료를 추가하세요.", nb.Name), nil
	}

	out := struct {
		Notebook    map[string]string `json:"notebook"`
		Focus       string            `json:"focus,omitempty"`
		Sources     []briefSource     `json:"sources"`
		Instruction string            `json:"instruction"`
	}{
		Notebook: map[string]string{"id": nb.ID, "name": nb.Name, "description": nb.Description},
		Focus:    strings.TrimSpace(focus),
		Instruction: "아래 sources에 담긴 내용에만 근거해 한국어 브리핑을 작성하라. " +
			"각 사실 끝에 출처를 [S1] 형식으로 인라인 인용하고, 자료에 없는 내용은 추측하지 말고 '자료에 없음'이라고 밝혀라. " +
			"note 필드(자료 누락·대체·보관 경고)가 있으면 신뢰도에 반영하라. " +
			"구성: 핵심 요약 → 주요 사실 → 리스크/확인 필요 → 다음 액션.",
	}
	// Enforce the budget on the ENCODED output, not just raw source text: the
	// tool's 24KB cap is applied by byte-length head/tail truncation after this
	// returns, which would corrupt the JSON. JSON quoting/indentation expands the
	// payload, and with very many sources the per-source budget floor alone can
	// overshoot — so we encode, and if it's over cap, first shrink the per-source
	// text budget, then (last resort) drop trailing sources, until it fits.
	baseInstruction := out.Instruction
	sources := nb.Sources
	perSource := notebookBriefByteBudget / len(sources)
	if perSource < notebookMinSourceBytes {
		perSource = notebookMinSourceBytes
	}
	var data []byte
	for {
		out.Sources = out.Sources[:0]
		out.Instruction = baseInstruction
		if omitted := len(nb.Sources) - len(sources); omitted > 0 {
			out.Instruction += fmt.Sprintf(" (자료 %d건은 출력 길이 제한으로 생략됨 — notebook show로 전체 확인)", omitted)
		}
		for _, src := range sources {
			out.Sources = append(out.Sources, buildBriefSource(d, src, perSource))
		}
		var err error
		data, err = json.MarshalIndent(out, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal notebook brief: %w", err)
		}
		if len(data) <= notebookBriefMaxBytes {
			break
		}
		switch {
		case perSource > notebookMinSourceBytes:
			perSource = perSource * 3 / 4 // JSON expansion / large pages: shrink excerpts
		case len(sources) > 1:
			sources = sources[:len(sources)-1] // metadata-heavy: drop a trailing source
		default:
			return string(data), nil // single minimal source: nothing more to trim
		}
	}
	return string(data), nil
}

// buildBriefSource resolves one source's content for a brief, truncated to
// maxBytes and annotated with any read/staleness/truncation note.
func buildBriefSource(d *toolctx.NotebookDeps, src notebook.Source, maxBytes int) briefSource {
	bs := briefSource{Cite: src.Cite, Kind: src.Kind, Ref: src.Ref, Title: src.Title}
	switch src.Kind {
	case notebook.KindNote:
		var truncated bool
		bs.Text, truncated = truncateBytesRuneSafe(src.Text, maxBytes)
		if truncated {
			bs.Note = appendNote(bs.Note, "⚠ 길이 초과로 일부만 표시(잘림)")
		}
	case notebook.KindWiki:
		bs.Text, bs.Note = readWikiSource(d.Wiki, src.Ref, maxBytes)
	default:
		bs.Note = "지원하지 않는 자료 유형"
	}
	return bs
}

// readWikiSource reads a pinned wiki page live, returning its grounding text
// (capped to maxBytes) and an optional note (read failure, staleness, or
// truncation). Reading live — rather than snapshotting at add time — means the
// brief always reflects the current page.
func readWikiSource(store *wiki.Store, ref string, maxBytes int) (text, note string) {
	if store == nil {
		return "", "위키 비활성 — 이 자료를 읽을 수 없음"
	}
	page, err := store.ReadPage(normalizeWikiRef(ref))
	if err != nil || page == nil {
		return "", fmt.Sprintf("위키 페이지 %q 읽기 실패 (이동/삭제됐을 수 있음)", ref)
	}
	switch {
	case page.Meta.SupersededBy != "":
		note = "⚠ 대체됨(최신 사실은 " + page.Meta.SupersededBy + " 참조 — 옛 값일 수 있음)"
	case page.Meta.Archived:
		note = "⚠ 보관됨(비활성 문서 — 현행이 아닐 수 있음)"
	}
	var sb strings.Builder
	if page.Meta.Summary != "" {
		sb.WriteString("요약: " + page.Meta.Summary + "\n\n")
	}
	sb.WriteString(strings.TrimSpace(page.Body))
	text, truncated := truncateBytesRuneSafe(sb.String(), maxBytes)
	if truncated {
		note = appendNote(note, "⚠ 길이 초과로 일부만 표시(잘림)")
	}
	return text, note
}

// truncateBytesRuneSafe caps s to maxBytes, cutting on a UTF-8 rune boundary so
// Korean text is never split mid-character. The byte cap (not a rune cap) is
// what keeps the marshaled brief under the byte-enforced tool output budget.
func truncateBytesRuneSafe(s string, maxBytes int) (string, bool) {
	if len(s) <= maxBytes {
		return s, false
	}
	cut := 0
	for i := range s { // i is the byte offset of each rune start
		if i > maxBytes {
			break
		}
		cut = i
	}
	return s[:cut], true
}

// appendNote joins a base note with an additional clause.
func appendNote(base, extra string) string {
	if base == "" {
		return extra
	}
	return base + " · " + extra
}

// normalizeWikiRef accepts a bare path or a "w:" namespaced ref and returns a
// "*.md" page path the wiki store can read (mirrors the wiki tool's read path).
func normalizeWikiRef(ref string) string {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), RefWiki)
	if ref != "" && !strings.HasSuffix(ref, ".md") {
		ref += ".md"
	}
	return ref
}

func notebookNotFound(id string) string {
	return fmt.Sprintf("노트북 %q 없음. `notebook(action=\"list\")` 로 목록을 확인하세요.", id)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func snippet(s string, maxRunes int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	return truncateRunes(s, maxRunes)
}
