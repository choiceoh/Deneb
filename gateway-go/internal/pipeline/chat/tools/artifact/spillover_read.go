package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tooldeps"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// Paging bounds. Content landed in spillover precisely because it blew the
// tool-output budget (24K chars default) — returning it whole just re-overflows
// and re-spills recursively. A page stays safely under that budget; the model
// pages with offset or narrows with grep.
const (
	spillPageDefaultLines = 400
	spillPageMaxChars     = 20000
	spillGrepMaxMatches   = 200
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
		if chars+len(lines[i])+1 > spillPageMaxChars && chars > 0 {
			break // char budget hit — stop before overflowing the page
		}
		b.WriteString(lines[i])
		b.WriteByte('\n')
		chars += len(lines[i]) + 1
		last = i + 1
	}

	header := fmt.Sprintf("[%s: %d–%d줄 표시 / 총 %d줄 · %d chars]\n", spillID, offset, last, totalLines, totalChars)
	out := header + b.String()
	if last < totalLines {
		out += fmt.Sprintf("\n[계속: read_spillover(spill_id=%q, offset=%d) · 검색은 grep=\"패턴\"]", spillID, last+1)
	}
	return strings.TrimRight(out, "\n")
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
		if shown >= spillGrepMaxMatches || chars+len(line) > spillPageMaxChars {
			continue // keep counting total matches, stop emitting
		}
		fmt.Fprintf(&b, "%d: %s\n", i+1, line)
		shown++
		chars += len(line) + 8
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
