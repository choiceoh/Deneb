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

	StatusUnread = "unread"
	StatusAcked  = "acked"
)

var ErrNotFound = errors.New("workfeed item not found")

type Item struct {
	ID          string `json:"id"`
	Source      string `json:"source"`
	Title       string `json:"title"`
	Summary     string `json:"summary,omitempty"`
	Body        string `json:"body,omitempty"`
	SessionKey  string `json:"sessionKey,omitempty"`
	RefType     string `json:"refType,omitempty"`
	RefID       string `json:"refId,omitempty"`
	Status      string `json:"status"`
	CreatedAtMs int64  `json:"createdAtMs"`
	UpdatedAtMs int64  `json:"updatedAtMs"`
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
		if !includeAcked && item.Status == StatusAcked {
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
