package recallops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// ToolPolaris creates the unified polaris tool with action dispatch (search/describe/expand).
func ToolPolaris(store *polaris.Store, localAI LocalAIFunc) toolport.ToolFunc {
	searchFn := toolPolarisSearch(store)
	describeFn := toolPolarisDescribe(store)
	expandFn := toolPolarisExpand(store, localAI)
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		switch normalizePolarisAction(p.Action) {
		case "search":
			return searchFn(ctx, input)
		case "describe":
			return describeFn(ctx, input)
		case "expand":
			return expandFn(ctx, input)
		default:
			return "action은 search, describe, expand 중 하나를 지정하세요.", nil
		}
	}
}

// toolPolarisSearch is the search sub-action: keyword search over compressed history.
func toolPolarisSearch(store *polaris.Store) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Query      string `json:"query"`
			MaxResults int    `json:"max_results"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.Query == "" {
			return "query가 비어 있습니다.", nil
		}
		if p.MaxResults <= 0 {
			p.MaxResults = 10
		}

		sessionKey := toolport.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "세션 키를 확인할 수 없습니다.", nil
		}

		hits, err := store.SearchMessages(sessionKey, p.Query, p.MaxResults)
		if err != nil {
			return fmt.Sprintf("검색 실패: %v", err), nil
		}
		if len(hits) == 0 {
			// Zero hits are usually a scope mismatch, not a bad query: polaris
			// searches ONLY the current session, and fresh cron/one-shot
			// sessions have almost nothing to find (production 2026-07-05:
			// most polaris zero-hits came from young sessions). Say so and
			// point at the cross-conversation / durable-memory tools instead
			// of returning a bare miss the model retries with synonyms.
			msgCount, _ := store.MessageCount(sessionKey)
			return fmt.Sprintf("'%s' 검색 결과가 없습니다. polaris는 현재 세션의 대화(메시지 %d개)만 검색합니다 — 과거·다른 대화는 `sessions`(action=\"search\"), 문서·사실은 `wiki`로 검색하세요.",
				p.Query, msgCount), nil
		}

		var sb strings.Builder
		sb.WriteString(toolport.RecallHeader(p.Query, len(hits), "polaris/세션"))
		for i, h := range hits {
			ts := time.UnixMilli(h.Timestamp).Format("2006-01-02 15:04")
			ref := fmt.Sprintf("%smsg%d", toolport.RefSession, h.MsgIndex)
			meta := fmt.Sprintf("%s · %s", h.Role, ts)
			sb.WriteString(toolport.RecallRow(i+1, ref, meta, h.Snippet))
		}
		sb.WriteString("원문 복원: `polaris(action=\"describe\")` 로 요약 ID 확인 후 `expand`.")
		return sb.String(), nil
	}
}

// toolPolarisDescribe is the describe sub-action: overview of summary DAG structure.
func toolPolarisDescribe(store *polaris.Store) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			TimeRange string `json:"time_range"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.TimeRange == "" {
			p.TimeRange = "all"
		}

		sessionKey := toolport.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "세션 키를 확인할 수 없습니다.", nil
		}

		msgCount, _ := store.MessageCount(sessionKey)
		coverage, _ := store.LatestSummaryCoverage(sessionKey)
		nodes, err := store.LoadSummaries(sessionKey, 0)
		if err != nil {
			return fmt.Sprintf("요약 조회 실패: %v", err), nil
		}

		// Filter by time range.
		now := time.Now()
		filtered := filterByTimeRange(nodes, p.TimeRange, now)

		var sb strings.Builder
		sb.WriteString("## 세션 대화 이력 구조\n\n")
		sb.WriteString(fmt.Sprintf("- 총 메시지: %d\n", msgCount))
		sb.WriteString(fmt.Sprintf("- 요약 커버: 메시지 0~%d\n", coverage))
		sb.WriteString(fmt.Sprintf("- 요약 노드: %d개\n\n", len(filtered)))

		if len(filtered) == 0 {
			sb.WriteString("요약된 구간이 없습니다 (아직 컴팩션이 발생하지 않음).\n")
			return sb.String(), nil
		}

		sb.WriteString("### 요약 노드 목록\n\n")
		for _, n := range filtered {
			ts := time.UnixMilli(n.CreatedAt).Format("2006-01-02 15:04")
			// Show the first ~200 bytes of content as preview. TruncateBytes backs
			// off to a rune boundary so a multi-byte char (Korean) never splits into
			// a U+FFFD replacement char in the preview.
			preview := n.Content
			if len(preview) > 200 {
				preview = textutil.TruncateBytes(preview, 200) + "..."
			}
			sb.WriteString(fmt.Sprintf("- **ID %d** (level %d, 메시지 %d-%d, %s, ~%d토큰)\n  %s\n\n",
				n.ID, n.Level, n.MsgStart, n.MsgEnd, ts, n.TokenEst, preview))
		}
		return sb.String(), nil
	}
}

// LocalAIFunc calls local AI for sub-agent delegation. Injected to avoid import cycles.
type LocalAIFunc func(ctx context.Context, system, user string, maxTokens int) (string, error)

// toolPolarisExpand is the expand sub-action: restore raw messages from a summary.
func toolPolarisExpand(store *polaris.Store, localAI LocalAIFunc) toolport.ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			SummaryID int    `json:"summary_id"`
			Question  string `json:"question"`
		}
		if err := json.Unmarshal(input, &p); err != nil {
			return "", fmt.Errorf("parse input: %w", err)
		}
		if p.SummaryID <= 0 {
			return "summary_id가 필요합니다. polaris(action=describe)로 먼저 ID를 확인하세요.", nil
		}

		sessionKey := toolport.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "세션 키를 확인할 수 없습니다.", nil
		}

		// Find the summary node by ID.
		target, err := store.SummaryByID(int64(p.SummaryID))
		if err != nil {
			return fmt.Sprintf("ID %d인 요약 노드를 찾을 수 없습니다: %v", p.SummaryID, err), nil
		}
		if target.SessionKey != sessionKey {
			return "해당 요약 노드는 현재 세션에 속하지 않습니다.", nil
		}

		// Load the raw messages covered by this summary.
		msgs, err := store.LoadMessages(sessionKey, target.MsgStart, target.MsgEnd)
		if err != nil {
			return fmt.Sprintf("원본 메시지 로드 실패: %v", err), nil
		}
		if len(msgs) == 0 {
			return "해당 구간의 원본 메시지가 없습니다.", nil
		}

		// Serialize raw messages. Serialization is capped, so omitted counts
		// the messages that did not fit — the caller must surface that rather
		// than let a partial excerpt read as the whole range.
		serialized, omitted := serializeExpandMessages(msgs, 8000)

		// If question provided and local AI available, delegate to AI for a focused answer.
		if p.Question != "" && localAI != nil {
			system := "아래 대화 원본을 바탕으로 질문에 정확히 답하라. 한국어로 답변."
			user := fmt.Sprintf("## 질문\n%s\n\n## 대화 원본 (메시지 %d-%d)\n%s",
				p.Question, target.MsgStart, target.MsgEnd, serialized)
			answer, err := localAI(ctx, system, user, 2048)
			if err == nil && answer != "" {
				// The delegate answered from a truncated excerpt: the root
				// gets the answer only, so the truncation has to travel with
				// it or the root will treat a partial reading as complete.
				return fmt.Sprintf("## 요약 ID %d 답변\n\n**질문:** %s\n%s\n%s",
					p.SummaryID, p.Question, expandCoverageNote(len(msgs), omitted), answer), nil
			}
			// Fall through to raw dump on AI failure.
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## 요약 ID %d 원본 (메시지 %d-%d, %d건)\n\n",
			p.SummaryID, target.MsgStart, target.MsgEnd, len(msgs)))
		if p.Question != "" {
			sb.WriteString(fmt.Sprintf("**질문:** %s\n\n", p.Question))
		}
		if note := expandCoverageNote(len(msgs), omitted); note != "" {
			sb.WriteString(strings.TrimPrefix(note, "\n") + "\n")
		}
		sb.WriteString(serialized)
		return sb.String(), nil
	}
}

// filterByTimeRange filters summary nodes by time range.
func filterByTimeRange(nodes []polaris.SummaryNode, timeRange string, now time.Time) []polaris.SummaryNode {
	if timeRange == "all" || timeRange == "" {
		return nodes
	}

	var cutoff time.Time
	switch timeRange {
	case "today":
		y, m, d := now.Date()
		cutoff = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	case "this_week":
		cutoff = now.AddDate(0, 0, -7)
	default:
		return nodes
	}

	cutoffMs := cutoff.UnixMilli()
	var filtered []polaris.SummaryNode
	for _, n := range nodes {
		if n.CreatedAt >= cutoffMs {
			filtered = append(filtered, n)
		}
	}
	return filtered
}

// serializeExpandMessages converts ChatMessages to readable text, capped at maxChars.
// serializeExpandMessages renders msgs up to maxChars and reports how many
// messages did not fit. The omitted count is the remaining messages, not the
// total — a caller that reports the total tells the model nothing about what
// it is missing.
func serializeExpandMessages(msgs []toolport.ChatMessage, maxChars int) (string, int) {
	var sb strings.Builder
	totalChars := 0
	for i, m := range msgs {
		text := m.TextContent()
		entry := fmt.Sprintf("[%s]: %s\n\n", m.Role, text)
		if totalChars+len(entry) > maxChars {
			omitted := len(msgs) - i
			sb.WriteString(fmt.Sprintf("... (나머지 %d건 생략)\n", omitted))
			return sb.String(), omitted
		}
		sb.WriteString(entry)
		totalChars += len(entry)
	}
	return sb.String(), 0
}

// expandCoverageNote states how much of the range the caller actually saw, or
// "" when nothing was dropped. Truncation that only appears inside the
// delegate's prompt never reaches the root model, which is how a partial
// excerpt gets mistaken for the full range.
func expandCoverageNote(total, omitted int) string {
	if omitted <= 0 {
		return ""
	}
	return fmt.Sprintf("\n**근거 범위:** 메시지 %d건 중 %d건만 읽음 (%d건은 길이 제한으로 생략) — 생략분이 중요하면 polaris(action=\"search\")로 좁혀 찾으세요.\n",
		total, total-omitted, omitted)
}
