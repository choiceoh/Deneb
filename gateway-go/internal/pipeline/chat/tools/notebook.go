package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

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
// collections for grounded, cited synthesis (the "이 자료 위주로" path).
//
// Actions: create / list / show / add_source / remove_source / delete / brief
// plus the session-grounding trio open / close / mode. open binds the current
// session to a notebook so every following turn is grounded primarily in its
// pinned sources (the chat pipeline injects them and suppresses broad recall);
// close returns to ordinary chat; mode toggles soft/strict grounding. brief is
// the one-shot variant: it gathers every source's content and returns JSON for
// the LLM to compose a grounded briefing that cites each claim inline ([S1]…).
func ToolNotebook(d *toolctx.NotebookDeps) toolctx.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action      string `json:"action"`
			ID          string `json:"id"`
			Name        string `json:"name"`
			Description string `json:"description"`
			DealRef     string `json:"deal_ref"`  // deal/project anchor (for_deal, pin_to_deal)
			DealName    string `json:"deal_name"` // notebook name when auto-creating for a deal
			Kind        string `json:"kind"`
			Ref         string `json:"ref"`
			Title       string `json:"title"`
			Text        string `json:"text"`
			Source      string `json:"source"` // cite tag for remove_source
			Focus       string `json:"focus"`  // optional brief focus
			Mode        string `json:"mode"`   // soft/strict grounding (mode action)
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if d == nil || d.Store == nil {
			return "노트북이 비활성 상태입니다.", nil
		}

		// id may be given directly, or resolved from a deal_ref for the
		// deal-anchored actions (a deal has at most one notebook).
		id := p.ID
		if id == "" && p.DealRef != "" {
			if nb, ok := d.Store.GetByDealRef(p.DealRef); ok {
				id = nb.ID
			}
		}

		switch p.Action {
		case "create":
			return notebookCreate(d, p.Name, p.Description)
		case "for_deal":
			return notebookForDeal(d, p.DealRef, p.DealName)
		case "pin_to_deal":
			return notebookPinToDeal(ctx, d, p.DealRef, p.DealName, p.Kind, p.Ref, p.Title, p.Text)
		case "list":
			return notebookList(d), nil
		case "show":
			return notebookShow(d, id)
		case "add_source":
			return notebookAddSource(ctx, d, id, p.Kind, p.Ref, p.Title, p.Text)
		case "remove_source":
			return notebookRemoveSource(d, id, p.Source)
		case "delete":
			return notebookDelete(d, id)
		case "brief":
			return notebookBrief(d, id, p.Focus)
		case "open":
			return notebookOpen(ctx, d, id)
		case "close":
			return notebookClose(ctx), nil
		case "mode":
			return notebookSetMode(d, id, p.Mode)
		default:
			return fmt.Sprintf("알 수 없는 액션: %s. 사용 가능: create, for_deal, pin_to_deal, list, show, add_source, remove_source, delete, brief, open, close, mode", p.Action), nil
		}
	}
}

// notebookForDeal returns (creating if needed) the deal's notebook and shows it.
// This is how the agent resolves "탑솔라 딜 노트북" without tracking its id.
func notebookForDeal(d *toolctx.NotebookDeps, dealRef, dealName string) (string, error) {
	if strings.TrimSpace(dealRef) == "" {
		return "deal_ref를 지정하세요 (딜/프로젝트 식별자).", nil
	}
	nb, err := d.Store.EnsureForDeal(dealRef, dealName, "")
	if err != nil {
		return fmt.Sprintf("딜 노트북 준비 실패: %v", err), nil
	}
	return notebookShow(d, nb.ID)
}

// notebookPinToDeal is the one-shot save path: ensure the deal's notebook exists,
// then pin a source to it. The mail pipeline (deal extraction) and the native
// "save to deal" action both flow through this — same deal_ref the wiki deal
// page uses, so raw evidence and curated facts share one identity.
func notebookPinToDeal(ctx context.Context, d *toolctx.NotebookDeps, dealRef, dealName, kind, ref, title, text string) (string, error) {
	if strings.TrimSpace(dealRef) == "" {
		return "deal_ref를 지정하세요 (딜/프로젝트 식별자).", nil
	}
	nb, err := d.Store.EnsureForDeal(dealRef, dealName, "")
	if err != nil {
		return fmt.Sprintf("자료 추가 실패: %v", err), nil
	}
	return notebookAddSource(ctx, d, nb.ID, kind, ref, title, text)
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

func notebookAddSource(ctx context.Context, d *toolctx.NotebookDeps, id, kind, ref, title, text string) (string, error) {
	kind = strings.TrimSpace(kind)
	// File + external sources are ingested into text at add time (snapshot), so
	// the briefing/grounding reads them like a note and never re-fetches per turn.
	switch kind {
	case notebook.KindFile:
		extracted, err := ingestFileSource(ctx, ref)
		if err != nil {
			return fmt.Sprintf("파일 자료 추가 실패: %v", err), nil
		}
		text = extracted
		if strings.TrimSpace(title) == "" {
			title = filepath.Base(strings.TrimSpace(ref))
		}
	case notebook.KindURL:
		ingested, msg := ingestViaReader(ctx, d.FetchURL, ref, "URL", "웹 fetch")
		if msg != "" {
			return msg, nil
		}
		text = ingested
		if strings.TrimSpace(title) == "" {
			title = strings.TrimSpace(ref)
		}
	case notebook.KindMail:
		ingested, msg := ingestViaReader(ctx, d.ReadMail, ref, "메일", "메일 아카이브")
		if msg != "" {
			return msg, nil
		}
		text = ingested
	case notebook.KindDiary:
		ingested, msg := ingestViaReader(ctx, d.ReadDiary, ref, "일기", "일기 저장소")
		if msg != "" {
			return msg, nil
		}
		text = ingested
	}
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

// notebookOpen binds the current session to a notebook so every following turn
// is grounded primarily in its pinned sources. The binding lives in toolctx
// (read by the chat run pipeline, which injects the grounding block on the tail
// of the user message and suppresses the broad memory recall for bound turns).
func notebookOpen(ctx context.Context, d *toolctx.NotebookDeps, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "열 노트북의 id를 지정하세요 (또는 deal_ref). `notebook(action=\"list\")` 로 확인.", nil
	}
	nb, ok := d.Store.Get(id)
	if !ok {
		return notebookNotFound(id), nil
	}
	sk := toolctx.SessionKeyFromContext(ctx)
	if sk == "" {
		return "세션을 식별할 수 없어 노트북을 열 수 없습니다.", nil
	}
	// A dedicated "notebook:<id>" session is fixed to its own notebook by the
	// session key — open cannot switch it (ActiveNotebook derives from the key).
	// Guard so we don't report a false success for a different notebook.
	if dedicated := toolctx.DedicatedNotebookID(sk); dedicated != "" && dedicated != nb.ID {
		return fmt.Sprintf("이 대화는 노트북 전용 세션이라 다른 노트북(%q)을 열 수 없습니다 — 그 노트북은 해당 노트북 화면에서 여세요.", nb.Name), nil
	}
	toolctx.SetActiveNotebook(sk, nb.ID)
	if len(nb.Sources) == 0 {
		return fmt.Sprintf("📓 노트북 %q 을(를) 열었지만 아직 핀된 자료가 없습니다. `notebook(action=\"add_source\", id=%q, ...)` 로 자료를 추가하세요.", nb.Name, nb.ID), nil
	}
	return fmt.Sprintf("📓 노트북 %q 열림 — 이제 이 노트북의 자료 %d건을 1차 근거로 답합니다 (%s). 닫으려면 `notebook(action=\"close\")`.", nb.Name, len(nb.Sources), notebookModeLabel(nb.Mode)), nil
}

// notebookClose unbinds the current session, returning to ordinary chat.
func notebookClose(ctx context.Context) string {
	sk := toolctx.SessionKeyFromContext(ctx)
	if sk == "" {
		return "세션을 식별할 수 없습니다."
	}
	// A dedicated "notebook:<id>" session derives its grounding from the session
	// key itself, so it cannot be unbound from within — the user leaves it by
	// navigating away in the app.
	if toolctx.DedicatedNotebookID(sk) != "" {
		return "이 대화는 노트북 전용 세션이라 여기서 닫을 수 없습니다 — 앱에서 다른 화면으로 나가면 일반 대화로 돌아갑니다."
	}
	toolctx.ClearActiveNotebook(sk)
	return "노트북을 닫았습니다 — 일반 모드로 돌아갑니다 (전체 메모리 회상 복귀)."
}

// notebookSetMode toggles a notebook's grounding strictness (soft/strict).
func notebookSetMode(d *toolctx.NotebookDeps, id, mode string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "그라운딩 모드를 바꿀 노트북 id를 지정하세요 (또는 deal_ref).", nil
	}
	if err := d.Store.SetMode(id, mode); err != nil {
		if errors.Is(err, notebook.ErrNotFound) {
			return notebookNotFound(id), nil
		}
		return fmt.Sprintf("그라운딩 모드 변경 실패: %v", err), nil
	}
	nb, _ := d.Store.Get(id)
	name, m := id, ""
	if nb != nil {
		name, m = nb.Name, nb.Mode
	}
	return fmt.Sprintf("노트북 %q 그라운딩 모드: %s", name, notebookModeLabel(m)), nil
}

// notebookModeLabel renders a human-readable description of a grounding mode.
func notebookModeLabel(mode string) string {
	if mode == notebook.ModeStrict {
		return "strict — 자료에만 근거, 없으면 '자료에 없음'"
	}
	return "soft — 자료 우선, 부족하면 일반 지식으로 보충하고 (자료 밖) 표시"
}

// notebookGroundingBudget bounds the session-grounding tail block. It rides
// EVERY turn of a notebook-bound session (wire-only on the last user message),
// so it is tighter than the one-shot brief budget — it competes with the
// conversation history for the turn's input budget, it is not a standalone tool
// result. Large notebooks get per-source excerpts shrunk, then trailing sources
// dropped (with a visible "생략" note), mirroring notebookBrief.
const (
	notebookGroundingBudget   = 12000
	notebookGroundingMaxBytes = 14000
)

// BuildNotebookGrounding renders a bound session's active notebook as a
// wire-only tail grounding block: a header, the soft/strict instruction, and
// each pinned source's content tagged [S#] (wiki pages read live, so the block
// reflects the current page). Returns ("", false) when the notebook is gone or
// has no sources, so the caller injects nothing. The chat run pipeline calls
// this each turn for a notebook-bound session (run_exec.go) — it is the session
// analogue of notebookBrief's one-shot JSON.
func BuildNotebookGrounding(d *toolctx.NotebookDeps, notebookID string) (string, bool) {
	if d == nil || d.Store == nil || strings.TrimSpace(notebookID) == "" {
		return "", false
	}
	nb, ok := d.Store.Get(notebookID)
	if !ok || len(nb.Sources) == 0 {
		return "", false
	}
	sources := nb.Sources
	perSource := notebookGroundingBudget / len(sources)
	if perSource < notebookMinSourceBytes {
		perSource = notebookMinSourceBytes
	}
	var out string
	for {
		var sb strings.Builder
		fmt.Fprintf(&sb, "[노트북 그라운딩 — 이번 세션: %q]\n", nb.Name)
		sb.WriteString(notebookGroundingInstruction(nb.Mode))
		if omitted := len(nb.Sources) - len(sources); omitted > 0 {
			fmt.Fprintf(&sb, " (자료 %d건은 길이 제한으로 생략 — `notebook(action=\"show\")` 로 전체 확인)", omitted)
		}
		sb.WriteString("\n\n핀된 자료:\n")
		for _, src := range sources {
			bs := buildBriefSource(d, src, perSource)
			fmt.Fprintf(&sb, "\n[%s] %s%s\n%s\n", bs.Cite, bs.Kind, groundingLabelSuffix(firstNonEmpty(bs.Title, bs.Ref)), strings.TrimSpace(bs.Text))
			if bs.Note != "" {
				fmt.Fprintf(&sb, "(%s)\n", bs.Note)
			}
		}
		out = sb.String()
		if len(out) <= notebookGroundingMaxBytes {
			break
		}
		switch {
		case perSource > notebookMinSourceBytes:
			perSource = perSource * 3 / 4 // shrink excerpts first
			if perSource < notebookMinSourceBytes {
				perSource = notebookMinSourceBytes // never below the floor
			}
		case len(sources) > 1:
			sources = sources[:len(sources)-1] // then drop a trailing source
		default:
			return out, true // single minimal source — nothing more to trim
		}
	}
	return out, true
}

// notebookGroundingInstruction is the soft/strict directive prepended to a
// session-grounding block. Soft (default) keeps the sources primary but lets the
// agent supplement, visibly marked; strict refuses anything not in the sources.
func notebookGroundingInstruction(mode string) string {
	if mode == notebook.ModeStrict {
		return "답변은 아래 핀된 자료에만 근거하라. 각 사실 끝에 [S#]로 인용하고, 자료에 없으면 '자료에 없음'이라고 답하며 추측하거나 외부 지식으로 보충하지 마라."
	}
	return "답변은 아래 핀된 자료를 1차 근거로 삼아라. 자료에서 나온 사실은 문장 끝에 [S#]로 인용하라. 자료에 없는 내용을 보충해야 하면 일반 지식·메모리를 써도 되되, 그 부분은 인용 없이 쓰고 (자료 밖)으로 표시하라. 자료와 일반 지식이 충돌하면 자료를 우선하고 충돌을 밝혀라."
}

// groundingLabelSuffix renders ": <label>" or "" so a source line stays clean
// when the source has neither a title nor a ref.
func groundingLabelSuffix(label string) string {
	if strings.TrimSpace(label) == "" {
		return ""
	}
	return ": " + label
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

// notebookIngestMaxBytes caps the text snapshotted for an ingested source
// (file/url/mail/diary) so a large document does not bloat the notebook JSON or
// the per-turn grounding budget.
const notebookIngestMaxBytes = 100_000

const notebookIngestOmitMarker = "\n…(길이 제한으로 일부 생략)"

// clampIngestedText trims and caps ingested source text to notebookIngestMaxBytes
// INCLUDING the omission marker, so the stored text never exceeds the cap.
func clampIngestedText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= notebookIngestMaxBytes {
		return s
	}
	budget := notebookIngestMaxBytes - len(notebookIngestOmitMarker)
	if budget < 0 {
		budget = 0
	}
	t, _ := truncateBytesRuneSafe(s, budget)
	return strings.TrimSpace(t) + notebookIngestOmitMarker
}

// ingestViaReader runs an optional source reader (url/mail/diary), returning
// (text, "") on success or ("", userMessage) when ref is empty, the reader is
// unwired, errors, or yields nothing — the caller returns userMessage to the
// agent and pins nothing.
func ingestViaReader(ctx context.Context, read toolctx.SourceReader, ref, label, backend string) (text, userMsg string) {
	if strings.TrimSpace(ref) == "" {
		return "", fmt.Sprintf("%s 자료는 ref가 필요합니다.", label)
	}
	if read == nil {
		return "", fmt.Sprintf("%s 자료 인입이 비활성입니다 (%s 미연결).", label, backend)
	}
	got, err := read(ctx, ref)
	if err != nil {
		return "", fmt.Sprintf("%s 자료 추가 실패: %v", label, err)
	}
	if got = clampIngestedText(got); got == "" {
		return "", fmt.Sprintf("%s에서 내용을 추출하지 못했습니다.", label)
	}
	return got, ""
}

// ingestFileSource reads the file at path and returns its text: PDF and image
// files are OCR'd (PaddleOCR-VL with a tesseract fallback — errors when no OCR
// backend is reachable), other files are read as UTF-8 text. The content is
// snapshotted into the source at add time so it is not re-read every turn.
func ingestFileSource(ctx context.Context, ref string) (string, error) {
	path := strings.TrimSpace(ref)
	if path == "" {
		return "", errors.New("file 자료는 ref에 파일 경로가 필요합니다")
	}
	info, err := os.Stat(path) //nolint:gosec // single-user host; operator/agent-supplied path
	if err != nil {
		return "", fmt.Errorf("파일을 찾을 수 없습니다: %s", path)
	}
	if info.IsDir() {
		return "", fmt.Errorf("디렉터리는 자료로 추가할 수 없습니다: %s", path)
	}
	data, err := os.ReadFile(path) //nolint:gosec // single-user host; operator/agent-supplied path
	if err != nil {
		return "", fmt.Errorf("파일 읽기 실패: %w", err)
	}
	if len(data) == 0 {
		return "", errors.New("빈 파일입니다")
	}
	var text string
	switch detectFileKind(path, data) {
	case "pdf":
		text, err = pdfOCR(ctx, data)
	case "image":
		text, err = ocrImageBytes(ctx, data)
	default:
		if !utf8.Valid(data) {
			return "", errors.New("텍스트로 읽을 수 없는 파일입니다 (PDF·이미지가 아니면 OCR 대상이 아님)")
		}
		text = string(data)
	}
	if err != nil {
		return "", fmt.Errorf("내용 추출 실패 (OCR 백엔드 없음일 수 있음): %w", err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("추출된 텍스트가 없습니다")
	}
	return clampIngestedText(text), nil
}

// detectFileKind classifies a file as "pdf", "image", or "text" by extension,
// falling back to content sniffing for unknown extensions.
func detectFileKind(path string, data []byte) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "pdf"
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".tif", ".tiff":
		return "image"
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".log", ".yaml", ".yml", ".xml", ".html", ".htm", ".go", ".py", ".js", ".ts":
		return "text"
	}
	switch ct := http.DetectContentType(data); {
	case strings.HasPrefix(ct, "application/pdf"):
		return "pdf"
	case strings.HasPrefix(ct, "image/"):
		return "image"
	default:
		return "text"
	}
}

// buildBriefSource resolves one source's content for a brief, truncated to
// maxBytes and annotated with any read/staleness/truncation note.
func buildBriefSource(d *toolctx.NotebookDeps, src notebook.Source, maxBytes int) briefSource {
	bs := briefSource{Cite: src.Cite, Kind: src.Kind, Ref: src.Ref, Title: src.Title}
	switch src.Kind {
	case notebook.KindNote, notebook.KindFile, notebook.KindURL, notebook.KindMail, notebook.KindDiary:
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
