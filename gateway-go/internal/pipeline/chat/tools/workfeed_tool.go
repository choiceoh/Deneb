// workfeed_tool.go — the agent's read/settle surface over its OWN proactive
// card feed (업무 피드). The agent (mail analysis, dreams, heartbeat) produces
// these cards, but until this tool only the native client could see them —
// the agent could not answer "이번 주에 뭘 능동적으로 알렸지", check whether a
// question card ever got answered, or mark one handled. observe(action=
// proactive) stays the aggregate funnel view; this is the per-card view.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// ToolWorkFeed returns the workfeed tool. store is the server's native-sync
// teeing wrapper (toolctx.WorkFeedRW) so agent-side read/ack mirrors to the
// phone exactly like a tap in the app would; nil is guarded at registration.
func ToolWorkFeed(store toolctx.WorkFeedRW) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action       string `json:"action"`
			ID           string `json:"id"`
			Limit        int    `json:"limit"`
			IncludeAcked bool   `json:"include_acked"`
		}
		if err := jsonutil.UnmarshalInto("workfeed params", input, &p); err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(p.Action)) {
		case "", "list":
			return workFeedList(store, p.Limit, p.IncludeAcked)
		case "read":
			return workFeedRead(store, p.ID)
		case "ack":
			return workFeedAck(store, p.ID)
		default:
			return "", fmt.Errorf("workfeed: unknown action %q (list|read|ack)", p.Action)
		}
	}
}

func workFeedList(store toolctx.WorkFeedRW, limit int, includeAcked bool) (string, error) {
	if limit <= 0 {
		limit = 20
	}
	items, total, err := store.List(limit, includeAcked)
	if err != nil {
		return "", fmt.Errorf("workfeed list: %w", err)
	}
	if len(items) == 0 {
		if includeAcked {
			return "업무 피드가 비어 있습니다.", nil
		}
		return "미처리(unread/snoozed) 카드가 없습니다. 처리된 카드까지 보려면 include_acked=true.", nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "업무 피드 %d건 (전체 %d건)\n", len(items), total)
	for _, it := range items {
		fmt.Fprintf(&b, "- [%s] %s · %s · %s", it.Status, it.ID, it.Source, workFeedTitle(it))
		if it.Question {
			b.WriteString(" · ❓질문")
		}
		if it.ReadAtMs == 0 {
			b.WriteString(" · 미열람")
		}
		fmt.Fprintf(&b, " · %s\n", workFeedAge(it.CreatedAtMs))
	}
	b.WriteString("\n카드 본문은 workfeed(action=read, id=...), 처리 완료 표시는 action=ack.")
	return b.String(), nil
}

func workFeedRead(store toolctx.WorkFeedRW, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("workfeed read: id가 필요합니다")
	}
	// MarkRead returns the item and stamps ReadAtMs (idempotent) — reading a
	// card through the agent counts as reading it in the app.
	item, err := store.MarkRead(id)
	if err != nil {
		return "", fmt.Errorf("workfeed read %q: %w", id, err)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s · %s · %s\n", item.Status, item.ID, item.Source, workFeedTitle(item))
	fmt.Fprintf(&b, "생성: %s", time.UnixMilli(item.CreatedAtMs).Format("2006-01-02 15:04"))
	if item.RefType != "" {
		fmt.Fprintf(&b, " · ref: %s/%s", item.RefType, item.RefID)
	}
	b.WriteString("\n\n")
	body := strings.TrimSpace(item.Body)
	if body == "" {
		body = strings.TrimSpace(item.Summary)
	}
	if body == "" {
		body = "(본문 없음)"
	}
	b.WriteString(body)
	if len(item.Actions) > 0 {
		b.WriteString("\n\n액션:")
		for _, a := range item.Actions {
			fmt.Fprintf(&b, " %s(%s)", a.Label, a.ID)
		}
	}
	return b.String(), nil
}

func workFeedAck(store toolctx.WorkFeedRW, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("workfeed ack: id가 필요합니다")
	}
	item, err := store.Ack(id)
	if err != nil {
		return "", fmt.Errorf("workfeed ack %q: %w", id, err)
	}
	return fmt.Sprintf("처리 완료로 표시했습니다: [%s] %s · %s", item.Status, item.ID, workFeedTitle(item)), nil
}

// workFeedTitle picks the best one-line label for a card.
func workFeedTitle(it workfeed.Item) string {
	if t := strings.TrimSpace(it.Title); t != "" {
		return t
	}
	if s := strings.TrimSpace(it.Summary); s != "" {
		return workFeedFirstLine(s)
	}
	return workFeedFirstLine(strings.TrimSpace(it.Body))
}

func workFeedFirstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	r := []rune(s)
	if len(r) > 60 {
		return string(r[:60]) + "…"
	}
	return s
}

// workFeedAge renders a coarse Korean age label ("3시간 전", "2일 전").
func workFeedAge(createdMs int64) string {
	if createdMs <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(createdMs))
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%d분 전", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d시간 전", int(d.Hours()))
	default:
		return fmt.Sprintf("%d일 전", int(d.Hours()/24))
	}
}
