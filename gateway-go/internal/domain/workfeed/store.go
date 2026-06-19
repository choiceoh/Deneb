package workfeed

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/choiceoh/deneb/gateway-go/pkg/jsonlstore"
)

const (
	SourceProactive       = "proactive"
	SourceMailReport      = "mail_report" // proactive mail analysis — gets the envelope card icon
	SourceCaptureImage    = "capture_image"
	SourceCaptureAudio    = "capture_audio"
	SourceCaptureDocument = "capture_document"
	SourceCaptureContacts = "capture_contacts"

	StatusUnread  = "unread"
	StatusAcked   = "acked"
	StatusSnoozed = "snoozed"

	ActionOpen     = "open"
	ActionFollowUp = "followup"
	ActionSnooze   = "snooze"
	ActionAck      = "ack"
	// ActionTrash permanently deletes a card. It is a UNIVERSAL action handled in
	// RunAction before the per-item action lookup, so it works on every card —
	// including legacy items and captures whose stored action list predates it —
	// without a feed-wide migration. The native client renders it as 휴지통.
	ActionTrash = "trash"

	// Priority levels — higher surfaces first in the feed. Inferred from the
	// item's urgency markers/keywords when the caller doesn't set one, so the
	// feed reads like a chief-of-staff briefing (what's urgent first) instead of
	// a reverse-chronological log.
	PriorityLow    = 1
	PriorityNormal = 2
	PriorityHigh   = 3
	PriorityUrgent = 4
)

var (
	ErrNotFound       = errors.New("workfeed item not found")
	ErrActionNotFound = errors.New("workfeed action not found")
)

// snoozeDuration is how long a snoozed work-feed item stays hidden before it
// re-surfaces. "나중에" (snooze) defers an item for "later today" rather than
// dismissing it like "완료" (ack); List brings it back near the top once this
// window elapses, restoring the distinction between the two actions.
const snoozeDuration = 3 * time.Hour

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
	Priority    int      `json:"priority,omitempty"`
	Actions     []Action `json:"actions,omitempty"`
	CreatedAtMs int64    `json:"createdAtMs"`
	UpdatedAtMs int64    `json:"updatedAtMs"`
	// SnoozedUntilMs, when set, is the wall-clock time a snoozed item re-surfaces.
	SnoozedUntilMs int64 `json:"snoozedUntilMs,omitempty"`
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

// Append adds item to the feed and returns the stored item. Thin wrapper over
// AppendIfNew for callers that don't need the created flag.
func (s *Store) Append(item Item) (Item, error) {
	out, _, err := s.AppendIfNew(item)
	return out, err
}

// AppendIfNew adds item unless it duplicates the most recent card (same source +
// same body fingerprint). This guards against the same proactive analysis being
// re-emitted — e.g. by a restart catch-up — and piling up as a duplicate card.
// On a duplicate it writes nothing and returns the existing card with
// created=false; callers (native sync) then skip the "created" event. Otherwise
// it returns the stored item with created=true.
func (s *Store) AppendIfNew(item Item) (Item, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item = normalizeNew(item)
	// Load + rewrite so retention can bound the file. The feed is appended to
	// infrequently (once per proactive report / capture), so the O(n) rewrite is
	// cheap and keeps the file — and every List — from growing without bound as
	// the feed ages. Fall back to a plain append if the file can't be read, so a
	// transient read error never drops the new item (dedup is skipped on that
	// rare path — losing a card is worse than an occasional duplicate).
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		if aerr := jsonlstore.Append(s.path, item); aerr != nil {
			return Item{}, false, aerr
		}
		return item, true, nil
	}
	if n := len(items); n > 0 && isDuplicateCard(items[n-1], item) {
		return items[n-1], false, nil
	}
	items = append(items, item)
	items = pruneRetention(items)
	if err := jsonlstore.Snapshot(s.path, items); err != nil {
		return Item{}, false, err
	}
	return item, true, nil
}

// isDuplicateCard reports whether cur duplicates prev: same source and the same
// non-empty body fingerprint. Only the body matters (title/priority ignored), so
// a re-emitted analysis dedupes even if its priority was re-inferred. An empty
// body never dedupes — distinct cards with no body (e.g. a capture whose OCR
// came back empty) must not collapse into one.
func isDuplicateCard(prev, cur Item) bool {
	if prev.Source != cur.Source {
		return false
	}
	fp := fingerprint(cur.Body)
	if fp == "" {
		return false
	}
	return fingerprint(prev.Body) == fp
}

// fingerprint normalizes a body for duplicate comparison: leading/trailing and
// internal whitespace runs collapse to single spaces, so newline/trailing-space
// differences don't defeat dedup while any real content difference is kept.
func fingerprint(body string) string {
	return strings.Join(strings.Fields(body), " ")
}

type ListOptions struct {
	Limit        int
	IncludeAcked bool
	SinceMs      int64
	BeforeMs     int64
}

func (s *Store) List(limit int, includeAcked bool) ([]Item, int, error) {
	return s.ListFiltered(ListOptions{Limit: limit, IncludeAcked: includeAcked})
}

func (s *Store) ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]Item, int, error) {
	return s.ListFiltered(ListOptions{Limit: limit, IncludeAcked: includeAcked, SinceMs: sinceMs, BeforeMs: beforeMs})
}

func (s *Store) ListFiltered(opts ListOptions) ([]Item, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now().UnixMilli()
	for i := range items {
		items[i] = normalizeExisting(items[i])
	}
	// A snoozed item whose window has elapsed sorts by its wake time, so it
	// re-surfaces near the top (fresh) instead of buried at its original slot.
	effectiveTime := func(it Item) int64 {
		if it.Status == StatusSnoozed && it.SnoozedUntilMs > 0 && it.SnoozedUntilMs <= now {
			return it.SnoozedUntilMs
		}
		return it.CreatedAtMs
	}
	// Priority first (urgent stays on top until handled), then recency / wake time.
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority > items[j].Priority
		}
		ti, tj := effectiveTime(items[i]), effectiveTime(items[j])
		if ti == tj {
			return items[i].ID > items[j].ID
		}
		return ti > tj
	})

	filtered := make([]Item, 0, len(items))
	for _, item := range items {
		if !opts.IncludeAcked {
			if item.Status == StatusAcked {
				continue
			}
			if item.Status == StatusSnoozed {
				if item.SnoozedUntilMs > now {
					continue // still snoozed — hidden until the window elapses
				}
				item.Status = StatusUnread // snooze elapsed — re-surface as actionable
			}
		}
		if opts.SinceMs > 0 && item.CreatedAtMs < opts.SinceMs {
			continue
		}
		if opts.BeforeMs > 0 && item.CreatedAtMs >= opts.BeforeMs {
			continue
		}
		filtered = append(filtered, item)
	}
	total := len(filtered)
	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}
	return filtered, total, nil
}

// EngagementStat summarizes how delivered proactive cards fared. A card the user
// acked or snoozed counts as engaged (they interacted — snooze is "later", not
// dismissed); a card still unread past the stale window counts as ignored
// (delivered, no engagement — the over-intervention / interruption-cost signal);
// a fresh unread card is pending (too new to judge). FTR is the over-intervention
// proxy from the proactive-agent literature (ProAgentBench precision = interruption
// cost): the fraction of judged cards that were ignored.
type EngagementStat struct {
	Total    int            `json:"total"`
	Engaged  int            `json:"engaged"`
	Ignored  int            `json:"ignored"`
	Pending  int            `json:"pending"`
	BySource map[string]int `json:"ignoredBySource"` // ignored count per card source
}

// FTR is the fraction of judged (non-pending) cards that were ignored. 0 when
// nothing has been judged yet.
func (e EngagementStat) FTR() float64 {
	judged := e.Engaged + e.Ignored
	if judged == 0 {
		return 0
	}
	return float64(e.Ignored) / float64(judged)
}

// Engagement rolls up the retained cards' engagement as of `now`, treating an
// unread card older than staleWindowMs as ignored. It reads raw stored status
// (snoozed stays engaged rather than re-surfacing as unread), so it conservatively
// under-counts ignored rather than flagging a just-resurfaced snooze. Reflects
// retained history only (old cards are pruned), so it is a recent-engagement view.
func (s *Store) Engagement(now, staleWindowMs int64) (EngagementStat, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return EngagementStat{BySource: map[string]int{}}, err
	}
	stat := EngagementStat{BySource: map[string]int{}}
	for _, it := range items {
		stat.Total++
		switch it.Status {
		case StatusAcked, StatusSnoozed:
			stat.Engaged++
		default: // unread (incl. legacy empty status)
			if staleWindowMs > 0 && now-it.CreatedAtMs > staleWindowMs {
				stat.Ignored++
				src := it.Source
				if src == "" {
					src = "unknown"
				}
				stat.BySource[src]++
			} else {
				stat.Pending++
			}
		}
	}
	return stat, nil
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

// Correct annotates the card identified by id with a user correction, appending
// note to the body as a dated "사용자 정정" erratum block and bumping UpdatedAtMs.
// The card stays in the feed, now visibly carrying the correction so a wrong
// analysis is never shown unqualified. Applies to every item sharing the id
// (legacy id twins), mirroring Ack. The durable knowledge fix (wiki) is the
// caller's separate agent turn; this is only the on-card erratum.
func (s *Store) Correct(id, note string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	note = strings.TrimSpace(note)
	if id == "" {
		return Item{}, ErrNotFound
	}
	items, err := jsonlstore.Load[Item](s.path)
	if err != nil {
		return Item{}, err
	}
	now := time.Now().UnixMilli()
	block := formatCorrection(note, now)
	var out Item
	found := false
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID == id {
			if note != "" {
				items[i].Body = strings.TrimRight(items[i].Body, "\n") + block
			}
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

// formatCorrection renders a user correction as a dated block appended to a card
// body, kept visually distinct from the original analysis by a rule + marker.
func formatCorrection(note string, atMs int64) string {
	date := time.UnixMilli(atMs).Format("2006-01-02")
	return "\n\n---\n\n✏️ **사용자 정정** (" + date + ")\n" + note
}

// Rewrite replaces the body of the card identified by id with newBody (a freshly
// regenerated analysis), re-derives the glance priority from the new content, and
// bumps UpdatedAtMs. Title and summary are left intact so the row preview stays
// stable; the regenerated analysis shows when the card is expanded. Applies to
// every item sharing the id (legacy twins), mirroring Ack/Correct. The native
// "다시 작성" path: the agent rewrites the analysis and the result lands here. A
// blank newBody is rejected so a failed regeneration never wipes the card.
func (s *Store) Rewrite(id, newBody string) (Item, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	id = strings.TrimSpace(id)
	newBody = strings.TrimSpace(newBody)
	if id == "" || newBody == "" {
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
			items[i].Body = newBody
			items[i].Priority = inferPriority(items[i])
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
	// Normalize every item, then locate the first one carrying itemID. The ack /
	// snooze status changes below are applied to ALL items sharing the id, not
	// just the first match. Legacy feeds minted by the old restart-resetting id
	// counter can hold duplicate ids; settling only the first twin left the rest
	// unread, so the card "came back" on the next List (a zombie work-feed item).
	// Ack/snooze are id-scoped state changes, so resolve the whole id at once —
	// this mirrors Ack(), which already settles every item with the id.
	first := -1
	for i := range items {
		items[i] = normalizeExisting(items[i])
		if items[i].ID == itemID && first < 0 {
			first = i
		}
	}
	if first < 0 {
		return ActionResult{}, ErrNotFound
	}
	// 휴지통 — universal hard delete. Handled before the per-item action lookup so it
	// works on every card regardless of its stored action list: drop every item
	// carrying itemID (legacy feeds can hold id twins) and persist.
	if actionID == ActionTrash {
		deleted := items[first]
		kept := items[:0]
		for _, it := range items {
			if it.ID != itemID {
				kept = append(kept, it)
			}
		}
		if err := jsonlstore.Snapshot(s.path, kept); err != nil {
			return ActionResult{}, err
		}
		return ActionResult{
			Item:           deleted,
			Action:         Action{ID: ActionTrash, Kind: ActionTrash, Label: "휴지통"},
			SessionKey:     deleted.SessionKey,
			Message:        "deleted",
			RemoveFromFeed: true,
		}, nil
	}
	action, ok := findAction(items[first], actionID)
	if !ok {
		return ActionResult{}, ErrActionNotFound
	}
	result := ActionResult{
		Item:       items[first],
		Action:     action,
		SessionKey: items[first].SessionKey,
	}
	switch action.Kind {
	case ActionOpen:
		// Read-only: surface the item's context as a prompt, no state change.
		result.Prompt = actionPrompt(action, openPrompt(items[first]))
		result.Message = "opened"
		return result, nil
	case ActionFollowUp:
		result.Prompt = actionPrompt(action, followUpPrompt(items[first]))
		result.Message = "prompt_created"
		return result, nil
	case ActionSnooze:
		for i := range items {
			if items[i].ID != itemID {
				continue
			}
			items[i].Status = StatusSnoozed
			items[i].UpdatedAtMs = now
			items[i].SnoozedUntilMs = now + snoozeDuration.Milliseconds()
		}
		// Snooze is non-terminal — leave the action available so the user can
		// snooze again after the item re-surfaces (unlike ack, which is done).
		result.Item = items[first]
		result.Message = "snoozed"
		result.RemoveFromFeed = true
	case ActionAck:
		for i := range items {
			if items[i].ID != itemID {
				continue
			}
			items[i].Status = StatusAcked
			items[i].UpdatedAtMs = now
			markActionDone(&items[i], action.ID)
		}
		result.Item = items[first]
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
	if item.Priority <= 0 {
		item.Priority = inferPriority(item)
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
	if item.Priority <= 0 {
		item.Priority = inferPriority(item)
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
