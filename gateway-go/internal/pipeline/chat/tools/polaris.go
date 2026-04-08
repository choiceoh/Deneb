package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

// ToolPolaris creates the unified polaris tool with action dispatch (search/describe/expand).
func ToolPolaris(store *polaris.Store, localAI LocalAIFunc) toolctx.ToolFunc {
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
		switch p.Action {
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
func toolPolarisSearch(store *polaris.Store) toolctx.ToolFunc {
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

		sessionKey := toolctx.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "세션 키를 확인할 수 없습니다.", nil
		}

		hits, err := store.SearchMessages(sessionKey, p.Query, p.MaxResults)
		if err != nil {
			return fmt.Sprintf("검색 실패: %v", err), nil
		}
		if len(hits) == 0 {
			return fmt.Sprintf("'%s' 검색 결과가 없습니다.", p.Query), nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("'%s' 검색 결과 (%d건):\n\n", p.Query, len(hits)))
		for i, h := range hits {
			ts := time.UnixMilli(h.Timestamp).Format("2006-01-02 15:04")
			sb.WriteString(fmt.Sprintf("%d. [%s] msg#%d (%s)\n   %s\n\n",
				i+1, h.Role, h.MsgIndex, ts, h.Snippet))
		}
		return sb.String(), nil
	}
}

// toolPolarisDescribe is the describe sub-action: overview of summary DAG structure.
func toolPolarisDescribe(store *polaris.Store) toolctx.ToolFunc {
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

		sessionKey := toolctx.SessionKeyFromContext(ctx)
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
		sb.WriteString(fmt.Sprintf("## 세션 대화 이력 구조\n\n"))
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
			// Show first 200 chars of content as preview.
			preview := n.Content
			if len(preview) > 200 {
				preview = preview[:200] + "..."
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
func toolPolarisExpand(store *polaris.Store, localAI LocalAIFunc) toolctx.ToolFunc {
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

		sessionKey := toolctx.SessionKeyFromContext(ctx)
		if sessionKey == "" {
			return "세션 키를 확인할 수 없습니다.", nil
		}

		// Find the summary node by ID.
		target, err := store.SummaryByID(int64(p.SummaryID))
		if err != nil {
			return fmt.Sprintf("ID %d인 요약 노드를 찾을 수 없습니다.", p.SummaryID), nil
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

		// Serialize raw messages.
		serialized := serializeExpandMessages(msgs, 8000)

		// If question provided and local AI available, delegate to AI for a focused answer.
		if p.Question != "" && localAI != nil {
			system := "아래 대화 원본을 바탕으로 질문에 정확히 답하라. 한국어로 답변."
			user := fmt.Sprintf("## 질문\n%s\n\n## 대화 원본 (메시지 %d-%d)\n%s",
				p.Question, target.MsgStart, target.MsgEnd, serialized)
			answer, err := localAI(ctx, system, user, 2048)
			if err == nil && answer != "" {
				return fmt.Sprintf("## 요약 ID %d 답변\n\n**질문:** %s\n\n%s",
					p.SummaryID, p.Question, answer), nil
			}
			// Fall through to raw dump on AI failure.
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("## 요약 ID %d 원본 (메시지 %d-%d, %d건)\n\n",
			p.SummaryID, target.MsgStart, target.MsgEnd, len(msgs)))
		if p.Question != "" {
			sb.WriteString(fmt.Sprintf("**질문:** %s\n\n", p.Question))
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
func serializeExpandMessages(msgs []toolctx.ChatMessage, maxChars int) string {
	var sb strings.Builder
	totalChars := 0
	for _, m := range msgs {
		text := m.TextContent()
		entry := fmt.Sprintf("[%s]: %s\n\n", m.Role, text)
		if totalChars+len(entry) > maxChars {
			sb.WriteString(fmt.Sprintf("... (나머지 %d건 생략)\n", len(msgs)))
			break
		}
		sb.WriteString(entry)
		totalChars += len(entry)
	}
	return sb.String()
}
