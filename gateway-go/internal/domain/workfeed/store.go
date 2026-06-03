package workfeed

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/shortid"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SourceProactive       = "proactive"
	SourceCaptureImage    = "capture_image"
	SourceCaptureAudio    = "capture_audio"
	SourceCaptureContacts = "capture_contacts"

	StatusUnread  = "unread"
	StatusAcked   = "acked"
	StatusSnoozed = "snoozed"

	ActionOpen     = "open"
	ActionFollowUp = "followup"
	ActionSnooze   = "snooze"
	ActionAck      = "ack"
)

var (
	ErrNotFound       = errors.New("workfeed item not found")
	ErrActionNotFound = errors.New("workfeed action not found")
)

type Action struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Label  string `json:"label"`
	Status string `json:"status,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

type Item struct {
	ID          string   `json:"id"`
	Source      string   `json:"source"`
	Title       string   `json:"title"`
	Summary     string   `json:"summary,omitempty"`
	Body        string   `json:"body,omitempty"`
	SessionKey  string   `json:"sessionKey,omitempty"`
	RefType     string   `json:"refType,omitempty"`
	RefID       string   `json:"refId,omitempty"`
	Status      string   `json:"status"`
	Actions     []Action `json:"actions,omitempty"`
	CreatedAtMs int64    `json:"createdAtMs"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
}

type ActionResult struct {
	Item           Item   `json:"item"`
	Action         Action `json:"action"`
	SessionKey     string `json:"sessionKey,omitempty"`
	Prompt         string `json:"prompt,omitempty"`
	Message        string `json:"message,omitempty"`
	RemoveFromFeed bool   `json:"removeFromFeed,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store {
	return &Store{path: path}
}

func (s *Store) Append(item Item) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item = normalizeNew(item)
	if err := jsonlstore.Append(s.path, item); err != nil {
		return Item{}, err
	}
	return item, nil
}

func (s *Store) List(limit int, includeAcked bool) ([]Item, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return nil, 0, err
	}
	for i := range items {
		items[i] = normalizeExisting(items[i])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].CreatedAtMs == items[j].CreatedAtMs {
			return items[i].ID > items[j].ID
		}
		return items[i].CreatedAtMs > items[j].CreatedAtMs
	})

	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if !includeAcked && (item.Status == StatusAcked || item.Status == StatusSnoozed) {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return filtered, total, nil
}

func (s *Store) Ack(id string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	if id == "" {
		return Item{}, ErrNotFound
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return Item{}, err
	}
	now := time.Now().UnixMilli()
	var out Item
	found := false
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID == id {
			items[i].Status = StatusAcked
			items[i].UpdatedAtMs = now
			out = items[i]
			found = true
		}
	}
	if !found {
		return Item{}, ErrNotFound
	}
	if err := jsonlstore.Snapshot(s.path, items); err != nil {
		return Item{}, err
	}
	return out, nil
}

func (s *Store) RunAction(itemID, actionID string) (ActionResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	itemID = strings.TrimSpace(itemID)
	actionID = strings.TrimSpace(actionID)
	if itemID == "" {
		return ActionResult{}, ErrNotFound
	}
	if actionID == "" {
		return ActionResult{}, ErrActionNotFound
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return ActionResult{}, err
	}
	now := time.Now().UnixMilli()
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID != itemID {
			continue
		}
		action, ok := findAction(items[i], actionID)
		if !ok {
			return ActionResult{}, ErrActionNotFound
		}
		result := ActionResult{
			Item:       items[i],
			Action:     action,
			SessionKey: items[i].SessionKey,
		}
		switch action.Kind {
		case ActionOpen:
			result.Message = "opened"
			return result, nil
		case ActionFollowUp:
			result.Prompt = followUpPrompt(items[i])
			result.Message = "prompt_created"
			return result, nil
		case ActionSnooze:
			items[i].Status = StatusSnoozed
			items[i].UpdatedAtMs = now
			markActionDone(&items[i], action.ID)
			result.Item = items[i]
			result.Message = "snoozed"
			result.RemoveFromFeed = true
		case ActionAck:
			items[i].Status = StatusAcked
			items[i].UpdatedAtMs = now
			markActionDone(&items[i], action.ID)
			result.Item = items[i]
			result.Message = "acked"
			result.RemoveFromFeed = true
		default:
			return ActionResult{}, ErrActionNotFound
		}
		if err := jsonlstore.Snapshot(s.path, items); err != nil {
			return ActionResult{}, err
		}
		return result, nil
	}
	return ActionResult{}, ErrNotFound
}

func normalizeNew(item Item) Item {
	now := time.Now().UnixMilli()
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		item.ID = shortid.New("wf")
	}
	item.Source = strings.TrimSpace(item.Source)
	item.Title = strings.TrimSpace(item.Title)
	item.Summary = Preview(item.Summary, 240)
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
	item.Actions = normalizeActions(item)
	if item.CreatedAtMs <= 0 {
		item.CreatedAtMs = now
	}
	item.UpdatedAtMs = now
	return item
}

func normalizeExisting(item Item) Item {
	item.ID = strings.TrimSpace(item.ID)
	item.Source = strings.TrimSpace(item.Source)
	item.Title = strings.TrimSpace(item.Title)
	item.Summary = strings.TrimSpace(item.Summary)
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
	item.Actions = normalizeActions(item)
	if item.UpdatedAtMs <= 0 {
		item.UpdatedAtMs = item.CreatedAtMs
	}
	return item
}

func defaultTitle(source string) string {
	switch source {
	case SourceProactive:
		return "업무 리포트"
	case SourceCaptureImage:
		return "공유 이미지"
	case SourceCaptureAudio:
		return "공유 녹음"
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

func followUpPrompt(item Item) string {
	body := strings.TrimSpace(item.Body)
	if body == "" {
		body = strings.TrimSpace(item.Summary)
	}
	switch item.Source {
	case SourceCaptureAudio:
		return "이 녹음/회의 내용을 업무 관점에서 다시 정리해줘. 결정사항, 액션아이템(담당/기한), 리스크, 다음 후속을 분리하고 빠진 정보는 질문으로 남겨줘.\n\n## 내용\n" + body
	case SourceCaptureImage:
		return "이 공유 이미지/OCR 결과를 업무 문서로 정리해줘. 중요한 사실, 해야 할 일, 확인해야 할 리스크를 분리하고 필요하면 위키에 남길 초안도 제안해줘.\n\n## 내용\n" + body
	case SourceCaptureContacts:
		return "방금 동기화한 주소록 결과를 바탕으로 지금 확인할 점이나 활용 가능한 후속 작업을 짧게 정리해줘.\n\n## 내용\n" + body
	default:
		return "이 업무 리포트를 바탕으로 지금 바로 처리할 다음 행동을 3개 이하로 정리해줘. 막힌 항목은 질문으로 남기고, 필요한 경우 후속 작업으로 쪼개줘.\n\n## 리포트\n" + body
	}
}

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
