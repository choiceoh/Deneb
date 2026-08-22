package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Paging bounds. Content landed in spillover precisely because it blew the
// tool-output budget (24K chars default) — returning it whole just re-overflows
// and re-spills recursively. A page stays safely under that budget; the model
// pages with offset or narrows with grep.
const (
	spillPageDefaultLines = 400
	spillPageMaxChars     = 20000
	spillGrepMaxMatches   = 200
	// spillPageLineHeadroom leaves room for the clip notice appended to a
	// single line that exceeds the whole page budget.
	spillPageLineHeadroom = 160
	// spillGrepLineMaxChars bounds ONE grep match so a single pathological line
	// cannot consume the whole result.
	spillGrepLineMaxChars = 4000
)

// ToolSpilloverRead returns a ToolFunc that reads a previously spilled large
// tool result by its spill ID — paged (offset/limit lines), filtered (grep),
// or answered (question), never the whole blob at once. Access is
// session-scoped: the caller must belong to the same session.
//
// ask is the local-model delegate behind the question path; nil (or a failing
// hub) leaves the tool paging-only.
func ToolSpilloverRead(store tooldeps.SpilloverStore, ask tooldeps.LocalAIFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SpillID  string `json:"spill_id"`
			Offset   int    `json:"offset"`
			Limit    int    `json:"limit"`
			Grep     string `json:"grep"`
			Question string `json:"question"`
		}
		if err := jsonutil.UnmarshalInto("read_spillover params", input, &p); err != nil {
			return "", err
		}
		if p.SpillID == "" {
			return "", fmt.Errorf("spill_id is required")
		}

		sessionKey := toolport.SessionKeyFromContext(ctx)
		content, err := store.Load(p.SpillID, sessionKey)
		if err != nil {
			return "", fmt.Errorf("read_spillover: %w", err)
		}

		lines := strings.Split(content, "\n")
		if q := strings.TrimSpace(p.Question); q != "" && ask != nil {
			if answer, ok := spillAsk(ctx, ask, p.SpillID, lines, q); ok {
				return answer, nil
			}
			// Delegation unavailable or empty — fall through to paging rather
			// than failing the call, and say so instead of silently returning
			// page 1 as if it answered the question.
			page := spillPage(p.SpillID, lines, len(content), p.Offset, p.Limit)
			return "[question 위임 실패 — 로컬 모델 응답 없음. 아래는 원문 페이지이니 grep/offset으로 직접 찾으세요]\n" + page, nil
		}
		if strings.TrimSpace(p.Grep) != "" {
			return spillGrep(p.SpillID, lines, p.Grep), nil
		}
		return spillPage(p.SpillID, lines, len(content), p.Offset, p.Limit), nil
	}
}

// spillPage renders a line window [offset, offset+limit) bounded by the char
// budget, with a header stating the whole blob's size and a continuation hint
// when there is more.
func spillPage(spillID string, lines []string, totalChars, offset, limit int) string {
	totalLines := len(lines)
	if offset < 1 {
		offset = 1
	}
	if offset > totalLines {
		return fmt.Sprintf("[%s: 총 %d줄] offset=%d은 범위 밖입니다. 1–%d 사이로 지정하세요.",
			spillID, totalLines, offset, totalLines)
	}
	if limit <= 0 {
		limit = spillPageDefaultLines
	}

	var b strings.Builder
	chars := 0
	last := offset - 1 // last line actually included (1-based)
	for i := offset - 1; i < totalLines && i < offset-1+limit; i++ {
		line := lines[i]
		// One line can exceed the entire page budget — minified JSON, base64,
		// compact command output. The budget check below only bites once
		// something is already buffered, so an oversized FIRST line would be
		// emitted whole, blow the tool-output cap, and be re-spilled into a
		// fresh pointer: the model would get a new handle instead of a page,
		// and repeating the read would chain spills indefinitely.
		if len(line) > spillPageMaxChars-spillPageLineHeadroom {
			line = textutil.TruncateBytes(line, spillPageMaxChars-spillPageLineHeadroom) +
				fmt.Sprintf(" …[이 줄은 %d자라 잘렸습니다 — grep=\"패턴\"으로 이 줄 안을 검색할 수 있습니다(역시 앞부분만 표시). 줄 전체를 그대로 받는 경로는 없습니다]", len(lines[i]))
		}
		if chars+len(line)+1 > spillPageMaxChars && chars > 0 {
			break // char budget hit — stop before overflowing the page
		}
		b.WriteString(line)
		b.WriteByte('\n')
		chars += len(line) + 1
		last = i + 1
	}

	header := fmt.Sprintf("[%s: %d–%d줄 표시 / 총 %d줄 · %d chars]\n", spillID, offset, last, totalLines, totalChars)
	out := header + b.String()
	if last < totalLines {
		out += fmt.Sprintf("\n[계속: read_spillover(spill_id=%q, offset=%d) · 검색은 grep=\"패턴\"]", spillID, last+1)
	}
	return strings.TrimRight(out, "\n")
}

// grepWindow returns a bounded slice of line centred on loc (the match), with
// markers showing how much was dropped on each side. Bounds are backed off to
// rune boundaries so a multi-byte character never splits into U+FFFD.
func grepWindow(line string, loc []int) string {
	start := 0
	if loc != nil {
		start = loc[0] - spillGrepLineMaxChars/2
	}
	if start < 0 {
		start = 0
	}
	end := start + spillGrepLineMaxChars
	if end > len(line) {
		end = len(line)
		if start = end - spillGrepLineMaxChars; start < 0 {
			start = 0
		}
	}
	for start > 0 && !utf8.RuneStart(line[start]) {
		start--
	}
	for end < len(line) && !utf8.RuneStart(line[end]) {
		end++
	}

	var b strings.Builder
	if start > 0 {
		fmt.Fprintf(&b, "…[앞 %d자 생략]", start)
	}
	b.WriteString(line[start:end])
	if end < len(line) {
		fmt.Fprintf(&b, "…[뒤 %d자 생략]", len(line)-end)
	}
	return b.String()
}

// spillGrep renders regex-matching lines with their line numbers so the model
// can jump straight to the relevant region (offset=N) instead of paging blind.
func spillGrep(spillID string, lines []string, pattern string) string {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Sprintf("grep 패턴이 잘못됐습니다 (%s). Go 정규식 문법으로 다시 시도하세요.", err)
	}

	var b strings.Builder
	matched, shown, chars := 0, 0, 0
	for i, line := range lines {
		if !re.MatchString(line) {
			continue
		}
		matched++
		if shown >= spillGrepMaxMatches {
			continue // keep counting total matches, stop emitting
		}
		// Clip rather than skip. Skipping an oversized match made the page
		// renderer's "grep으로 찾으세요" hint a dead end: the only route into a
		// long line's content refused to emit it.
		shownLine := line
		if len(shownLine) > spillGrepLineMaxChars {
			// Window around the MATCH, not the head. Head-truncating while
			// matching the full line reports a hit whose text the model cannot
			// see — and searching inside a long line is the whole reason this
			// path exists.
			shownLine = grepWindow(line, re.FindStringIndex(line))
		}
		if chars+len(shownLine) > spillPageMaxChars && shown > 0 {
			continue // page budget spent — keep counting, stop emitting
		}
		fmt.Fprintf(&b, "%d: %s\n", i+1, shownLine)
		shown++
		chars += len(shownLine) + 8
	}
	if matched == 0 {
		return fmt.Sprintf("[%s: 총 %d줄] %q 매치 없음.", spillID, len(lines), pattern)
	}
	out := fmt.Sprintf("[%s: %q 매치 %d줄", spillID, pattern, matched)
	if shown < matched {
		out += fmt.Sprintf(" 중 %d줄 표시 — 패턴을 좁히거나 offset으로 해당 구간을 여세요", shown)
	}
	out += "]\n" + b.String()
	return strings.TrimRight(out, "\n")
}
