package workfeed

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

// idCounter disambiguates ids minted within the same millisecond. Combined with
// the wall-clock millis prefix below, ids stay unique across restarts — unlike
// the old shortid counter, which reset to 0 on every restart and recycled ids
// (e.g. wf_0003). That recycling made an acked item reappear once a new proactive
// item reused its id, and also produced duplicate ids in the same feed.
var idCounter atomic.Uint64

// Urgency markers/keywords used to infer an item's priority from its content.
// Proactive reports tag lines with 🔴 긴급 / 🟠 중요 / 🟡 일반 / 🔵 참고; captures and
// free-form bodies use the keyword forms. Highest match wins.
var (
	urgentMarkers = []string{"🔴", "긴급", "urgent", "asap", "즉시", "당장", "critical"}
	highMarkers   = []string{"🟠", "중요", "마감", "deadline", "important", "오늘까지", "내일까지"}
	lowMarkers    = []string{"🔵", "참고", "fyi"}
)

// inferPriority scans an item's title/summary/body for urgency markers and
// returns the highest matching level, defaulting to PriorityNormal.
func inferPriority(item Item) int {
	text := strings.ToLower(item.Title + "\n" + item.Summary + "\n" + item.Body)
	containsAny := func(markers []string) bool {
		for _, m := range markers {
			if strings.Contains(text, strings.ToLower(m)) {
				return true
			}
		}
		return false
	}
	switch {
	case containsAny(urgentMarkers):
		return PriorityUrgent
	case containsAny(highMarkers):
		return PriorityHigh
	case containsAny(lowMarkers):
		return PriorityLow
	default:
		return PriorityNormal
	}
}

// maxRetained caps how many items the feed keeps on disk. Once exceeded, the
// oldest acked items are dropped so the jsonl can't grow without bound and List
// stays fast; active items (unread, or still-snoozed and due to re-surface) are
// never dropped.
const maxRetained = 1000

func pruneRetention(items []Item) []Item {
	if len(items) <= maxRetained {
		return items
	}
	sort.SliceStable(items, func(i, j int) bool {
		return retentionRecency(items[i]) > retentionRecency(items[j])
	})
	kept := make([]Item, 0, maxRetained)
	for i, it := range items {
		if i < maxRetained || it.Status == StatusUnread || it.Status == StatusSnoozed {
			kept = append(kept, it)
		}
	}
	return kept
}

func retentionRecency(it Item) int64 {
	if it.UpdatedAtMs > 0 {
		return it.UpdatedAtMs
	}
	return it.CreatedAtMs
}

func normalizeNew(item Item) Item {
	now := time.Now().UnixMilli()
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = fmt.Sprintf("wf_%d_%04d", now, idCounter.Add(1)%10000)
	}
	item.Summary = Preview(item.Summary, 240)
	item = normalizeItem(item)
	if item.CreatedAtMs <= 0 {
		item.CreatedAtMs = now
	}
	item.UpdatedAtMs = now
	return item
}

func normalizeExisting(item Item) Item {
	item.Summary = strings.TrimSpace(item.Summary)
	item = normalizeItem(item)
	if item.UpdatedAtMs <= 0 {
		item.UpdatedAtMs = item.CreatedAtMs
	}
	return item
}

func normalizeItem(item Item) Item {
	item.ID = strings.TrimSpace(item.ID)
	item.Source = strings.TrimSpace(item.Source)
	item.Title = strings.TrimSpace(item.Title)
	item = rewriteLegacyLogSource(item)
	item.Body = strings.TrimSpace(item.Body)
	item.SessionKey = strings.TrimSpace(item.SessionKey)
	item.RefType = strings.TrimSpace(item.RefType)
	item.RefID = strings.TrimSpace(item.RefID)
	item.Status = strings.TrimSpace(item.Status)
	if item.Status == "" {
		item.Status = StatusUnread
	}
	if item.Title == "" {
		item.Title = defaultTitle(item.Source)
	}
	if item.Summary == "" {
		item.Summary = Preview(item.Body, 240)
	}
	if item.Priority <= 0 {
		item.Priority = inferPriority(item)
	}
	item.Actions = normalizeActions(item)
	return item
}

func defaultTitle(source string) string {
	switch source {
	case SourceSystemLog:
		return "시스템 기록"
	case SourceProactive:
		return "업무 리포트"
	case SourceMailReport:
		return "메일 리포트"
	case SourceCaptureImage:
		return "공유 이미지"
	case SourceCaptureAudio:
		return "공유 녹음"
	case SourceCaptureDocument:
		return "공유 문서"
	case SourceCaptureContacts:
		return "주소록 동기화"
	default:
		return "업무 항목"
	}
}

func normalizeActions(item Item) []Action {
	actions := item.Actions
	if len(actions) == 0 {
		actions = defaultActions(item)
	}
	out := make([]Action, 0, len(actions))
	seen := map[string]struct{}{}
	for _, action := range actions {
		action.ID = strings.TrimSpace(action.ID)
		action.Kind = strings.TrimSpace(action.Kind)
		action.Label = strings.TrimSpace(action.Label)
		action.Status = strings.TrimSpace(action.Status)
		action.Prompt = strings.TrimSpace(action.Prompt)
		if action.Kind == "" {
			action.Kind = action.ID
		}
		if action.ID == "" {
			action.ID = action.Kind
		}
		if action.Label == "" {
			action.Label = actionLabel(action.Kind, item.Source)
		}
		if _, ok := seen[action.ID]; ok || action.ID == "" {
			continue
		}
		seen[action.ID] = struct{}{}
		out = append(out, action)
	}
	return out
}

func defaultActions(item Item) []Action {
	return []Action{
		{ID: ActionOpen, Kind: ActionOpen, Label: actionLabel(ActionOpen, item.Source)},
		{ID: ActionFollowUp, Kind: ActionFollowUp, Label: actionLabel(ActionFollowUp, item.Source)},
		{ID: ActionSnooze, Kind: ActionSnooze, Label: actionLabel(ActionSnooze, item.Source)},
		{ID: ActionAck, Kind: ActionAck, Label: actionLabel(ActionAck, item.Source)},
	}
}

func actionLabel(kind, source string) string {
	switch kind {
	case ActionOpen:
		return "열기"
	case ActionFollowUp:
		switch source {
		case SourceCaptureAudio:
			return "액션 정리"
		case SourceCaptureImage:
			return "문서화"
		case SourceCaptureDocument:
			return "정리"
		case SourceCaptureContacts:
			return "확인"
		default:
			return "후속 정리"
		}
	case ActionSnooze:
		return "나중에"
	case ActionAck:
		return "완료"
	default:
		return "실행"
	}
}

func findAction(item Item, actionID string) (Action, bool) {
	for _, action := range item.Actions {
		if action.ID == actionID || action.Kind == actionID {
			return action, true
		}
	}
	return Action{}, false
}

func markActionDone(item *Item, actionID string) {
	for i := range item.Actions {
		if item.Actions[i].ID == actionID || item.Actions[i].Kind == actionID {
			item.Actions[i].Status = "done"
			return
		}
	}
}

func actionPrompt(action Action, fallback string) string {
	if prompt := strings.TrimSpace(action.Prompt); prompt != "" {
		return prompt
	}
	return fallback
}

func openPrompt(item Item) string {
	body := contextBody(item)
	var b strings.Builder
	b.WriteString("이 업무 항목을 열었어. 아래 내용을 기준으로 핵심을 짧게 요약하고, 지금 바로 할 다음 행동을 3개 이하로 제안해줘. 내가 답해야 할 질문이 있으면 마지막에 모아줘.\n\n")
	b.WriteString("## 업무 항목\n")
	if title := strings.TrimSpace(item.Title); title != "" {
		b.WriteString("- 제목: ")
		b.WriteString(title)
		b.WriteByte('\n')
	}
	if source := strings.TrimSpace(item.Source); source != "" {
		b.WriteString("- 출처: ")
		b.WriteString(source)
		b.WriteByte('\n')
	}
	if refType := strings.TrimSpace(item.RefType); refType != "" {
		b.WriteString("- 참조: ")
		b.WriteString(refType)
		if refID := strings.TrimSpace(item.RefID); refID != "" {
			b.WriteString(" / ")
			b.WriteString(refID)
		}
		b.WriteByte('\n')
	}
	if summary := strings.TrimSpace(item.Summary); summary != "" {
		b.WriteString("- 요약: ")
		b.WriteString(summary)
		b.WriteByte('\n')
	}
	if body != "" {
		b.WriteString("\n## 내용\n")
		b.WriteString(body)
	}
	return strings.TrimSpace(b.String())
}

func followUpPrompt(item Item) string {
	body := contextBody(item)
	switch item.Source {
	case SourceCaptureAudio:
		return "이 녹음/회의 내용을 업무 관점에서 다시 정리해줘. 결정사항, 액션아이템(담당/기한), 리스크, 다음 후속을 분리하고 빠진 정보는 질문으로 남겨줘.\n\n## 내용\n" + body
	case SourceCaptureImage:
		return "이 공유 이미지/OCR 결과를 업무 문서로 정리해줘. 중요한 사실, 해야 할 일, 확인해야 할 리스크를 분리하고 필요하면 위키에 남길 초안도 제안해줘.\n\n## 내용\n" + body
	case SourceCaptureDocument:
		return "이 공유 문서를 업무 관점에서 정리해줘. 핵심 내용, 해야 할 일(담당/기한), 확인해야 할 리스크를 분리하고 필요하면 위키에 남길 초안도 제안해줘.\n\n## 내용\n" + body
	case SourceCaptureContacts:
		return "방금 동기화한 주소록 결과를 바탕으로 지금 확인할 점이나 활용 가능한 후속 작업을 짧게 정리해줘.\n\n## 내용\n" + body
	default:
		return "이 업무 리포트를 바탕으로 지금 바로 처리할 다음 행동을 3개 이하로 정리해줘. 막힌 항목은 질문으로 남기고, 필요한 경우 후속 작업으로 쪼개줘.\n\n## 리포트\n" + body
	}
}

func contextBody(item Item) string {
	body := strings.TrimSpace(item.Body)
	if body == "" {
		body = strings.TrimSpace(item.Summary)
	}
	return body
}

// Preview truncates text to maxRunes without splitting Unicode code points.
func Preview(text string, maxRunes int) string {
	s := strings.TrimSpace(strings.ReplaceAll(text, "\x00", ""))
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
