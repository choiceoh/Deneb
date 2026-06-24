package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeWorkFeedStore struct {
	items       []workfeed.Item
	lastLimit   int
	lastInclude bool
	lastSince   int64
	lastBefore  int64
	ackID       string
	ackErr      error
	readID      string
	readErr     error
	runItemID   string
	runActionID string
	runErr      error
	// runItem, when its Source is set, is returned as the RunAction result's Item
	// (lets a test exercise deal-question answer routing).
	runItem workfeed.Item
}

func (f *fakeWorkFeedStore) List(limit int, includeAcked bool) ([]workfeed.Item, int, error) {
	f.lastLimit = limit
	f.lastInclude = includeAcked
	out := f.items
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, len(f.items), nil
}

func (f *fakeWorkFeedStore) ListRange(limit int, includeAcked bool, sinceMs, beforeMs int64) ([]workfeed.Item, int, error) {
	f.lastLimit = limit
	f.lastInclude = includeAcked
	f.lastSince = sinceMs
	f.lastBefore = beforeMs
	out := make([]workfeed.Item, 0, len(f.items))
	for _, item := range f.items {
		if sinceMs > 0 && item.CreatedAtMs < sinceMs {
			continue
		}
		if beforeMs > 0 && item.CreatedAtMs >= beforeMs {
			continue
		}
		out = append(out, item)
	}
	total := len(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, total, nil
}

func (f *fakeWorkFeedStore) Ack(id string) (workfeed.Item, error) {
	f.ackID = id
	if f.ackErr != nil {
		return workfeed.Item{}, f.ackErr
	}
	for _, item := range f.items {
		if item.ID == id {
			item.Status = workfeed.StatusAcked
			return item, nil
		}
	}
	return workfeed.Item{}, workfeed.ErrNotFound
}

func (f *fakeWorkFeedStore) MarkRead(id string) (workfeed.Item, error) {
	f.readID = id
	if f.readErr != nil {
		return workfeed.Item{}, f.readErr
	}
	for i := range f.items {
		if f.items[i].ID == id {
			f.items[i].ReadAtMs = 1
			return f.items[i], nil
		}
	}
	return workfeed.Item{}, workfeed.ErrNotFound
}

func (f *fakeWorkFeedStore) RunAction(itemID, actionID string) (workfeed.ActionResult, error) {
	f.runItemID = itemID
	f.runActionID = actionID
	if f.runErr != nil {
		return workfeed.ActionResult{}, f.runErr
	}
	item := workfeed.Item{ID: itemID, SessionKey: "client:main"}
	if f.runItem.Source != "" {
		item = f.runItem
	}
	action := workfeed.Action{ID: actionID, Kind: actionID, Label: "Run"}
	return workfeed.ActionResult{
		Item:       item,
		Action:     action,
		SessionKey: item.SessionKey,
		Prompt:     "follow up",
		Message:    "prompt_created",
	}, nil
}

func TestWorkFeedMethods_NilStoreReturnsNil(t *testing.T) {
	if got := WorkFeedMethods(WorkFeedDeps{}); got != nil {
		t.Fatalf("WorkFeedMethods(nil) = %v, want nil", got)
	}
}

func TestWorkFeedList(t *testing.T) {
	store := &fakeWorkFeedStore{
		items: []workfeed.Item{
			{ID: "a", Title: "A", Status: workfeed.StatusUnread},
			{ID: "b", Title: "B", Status: workfeed.StatusAcked},
		},
	}
	h := workFeedList(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.list", map[string]any{
		"limit":        5000,
		"includeAcked": true,
	}))

	var got struct {
		Items []workfeed.Item `json:"items"`
		Count int             `json:"count"`
		Total int             `json:"total"`
	}
	decode(t, resp, &got)
	if store.lastLimit != maxWorkFeedLimit {
		t.Fatalf("limit = %d, want clamp %d", store.lastLimit, maxWorkFeedLimit)
	}
	if !store.lastInclude {
		t.Fatalf("includeAcked not forwarded")
	}
	if got.Count != 2 || got.Total != 2 {
		t.Fatalf("count/total = %d/%d, want 2/2", got.Count, got.Total)
	}
}

func TestWorkFeedListRange(t *testing.T) {
	store := &fakeWorkFeedStore{
		items: []workfeed.Item{
			{ID: "old", Title: "Old", Status: workfeed.StatusUnread, CreatedAtMs: 1_000},
			{ID: "today", Title: "Today", Status: workfeed.StatusUnread, CreatedAtMs: 10_000},
		},
	}
	h := workFeedList(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.list", map[string]any{
		"limit":    20,
		"sinceMs":  9_000,
		"beforeMs": 11_000,
	}))

	var got struct {
		Items []workfeed.Item `json:"items"`
		Count int             `json:"count"`
		Total int             `json:"total"`
	}
	decode(t, resp, &got)
	if store.lastSince != 9_000 || store.lastBefore != 11_000 {
		t.Fatalf("range = %d/%d, want 9000/11000", store.lastSince, store.lastBefore)
	}
	if got.Count != 1 || got.Total != 1 || got.Items[0].ID != "today" {
		t.Fatalf("payload = %+v, want today only", got)
	}
}

func TestWorkFeedListRejectsInvalidRange(t *testing.T) {
	h := workFeedList(WorkFeedDeps{Store: &fakeWorkFeedStore{}})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.list", map[string]any{
		"sinceMs":  11_000,
		"beforeMs": 9_000,
	}))
	if resp.OK {
		t.Fatalf("expected invalid params")
	}
	if resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrInvalidRequest)
	}
}

func TestWorkFeedListRequiresAuth(t *testing.T) {
	h := workFeedList(WorkFeedDeps{Store: &fakeWorkFeedStore{}})
	resp := h(context.Background(), reqWith(t, "miniapp.workfeed.list", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestWorkFeedAck(t *testing.T) {
	store := &fakeWorkFeedStore{items: []workfeed.Item{{ID: "a", Title: "A"}}}
	h := workFeedAck(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.ack", map[string]any{"id": "a"}))

	var got struct {
		OK   bool          `json:"ok"`
		Item workfeed.Item `json:"item"`
	}
	decode(t, resp, &got)
	if store.ackID != "a" || !got.OK || got.Item.Status != workfeed.StatusAcked {
		t.Fatalf("ack result store=%q got=%+v", store.ackID, got)
	}
}

func TestWorkFeedAckMissing(t *testing.T) {
	store := &fakeWorkFeedStore{ackErr: workfeed.ErrNotFound}
	h := workFeedAck(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.ack", map[string]any{"id": "missing"}))
	if resp.OK {
		t.Fatalf("expected not found")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

func TestWorkFeedAckUnavailable(t *testing.T) {
	store := &fakeWorkFeedStore{ackErr: errors.New("disk down")}
	h := workFeedAck(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.ack", map[string]any{"id": "a"}))
	if resp.OK {
		t.Fatalf("expected unavailable")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnavailable)
	}
}

func TestWorkFeedRead(t *testing.T) {
	store := &fakeWorkFeedStore{items: []workfeed.Item{{ID: "a", Title: "A", Status: workfeed.StatusUnread}}}
	h := workFeedRead(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.read", map[string]any{"itemId": "a"}))

	var got struct {
		OK   bool          `json:"ok"`
		Item workfeed.Item `json:"item"`
	}
	decode(t, resp, &got)
	if store.readID != "a" || !got.OK || got.Item.ReadAtMs == 0 {
		t.Fatalf("read result store=%q got=%+v", store.readID, got)
	}
	// Reading is softer than ack — the card must stay unread (still in the feed).
	if got.Item.Status != workfeed.StatusUnread {
		t.Fatalf("status = %q, want unread (read must not settle)", got.Item.Status)
	}
}

func TestWorkFeedReadMissingItemID(t *testing.T) {
	h := workFeedRead(WorkFeedDeps{Store: &fakeWorkFeedStore{}})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.read", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected missing param")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrMissingParam)
	}
}

func TestWorkFeedReadNotFound(t *testing.T) {
	store := &fakeWorkFeedStore{readErr: workfeed.ErrNotFound}
	h := workFeedRead(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.read", map[string]any{"itemId": "missing"}))
	if resp.OK {
		t.Fatalf("expected not found")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

func TestWorkFeedActionRun(t *testing.T) {
	store := &fakeWorkFeedStore{}
	h := workFeedActionRun(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.action.run", map[string]any{
		"itemId":   "item",
		"actionId": "followup",
	}))

	var got struct {
		OK         bool            `json:"ok"`
		Item       workfeed.Item   `json:"item"`
		Action     workfeed.Action `json:"action"`
		SessionKey string          `json:"sessionKey"`
		Prompt     string          `json:"prompt"`
	}
	decode(t, resp, &got)
	if store.runItemID != "item" || store.runActionID != "followup" {
		t.Fatalf("run args = %q/%q", store.runItemID, store.runActionID)
	}
	if !got.OK || got.Prompt == "" || got.SessionKey != "client:main" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestWorkFeedActionRunMissingAction(t *testing.T) {
	store := &fakeWorkFeedStore{runErr: workfeed.ErrActionNotFound}
	h := workFeedActionRun(WorkFeedDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.workfeed.action.run", map[string]any{
		"itemId":   "item",
		"actionId": "missing",
	}))
	if resp.OK {
		t.Fatalf("expected not found")
	}
	if resp.Error.Code != protocol.ErrNotFound {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrNotFound)
	}
}

// A deal-question card's "dept:*" answer routes to OnAnswer with the settled item
// (carrying RefID) and the tapped action — so the server can record the team onto
// the deal wiki page.
func TestWorkFeedActionRun_DealQuestionRoutesAnswer(t *testing.T) {
	store := &fakeWorkFeedStore{runItem: workfeed.Item{
		ID: "q1", Source: "deal_question", RefType: "wiki", RefID: "프로젝트/완도.md",
	}}
	var gotItem workfeed.Item
	var gotAction string
	calls := 0
	deps := WorkFeedDeps{Store: store, OnAnswer: func(item workfeed.Item, actionID string) {
		calls++
		gotItem = item
		gotAction = actionID
	}}
	resp := workFeedActionRun(deps)(authedCtx(), reqWith(t, "miniapp.workfeed.action.run", map[string]any{
		"itemId": "q1", "actionId": "dept:pl1",
	}))
	if !resp.OK {
		t.Fatalf("expected ok: %+v", resp.Error)
	}
	if calls != 1 {
		t.Fatalf("OnAnswer called %d times, want 1", calls)
	}
	if gotAction != "dept:pl1" || gotItem.RefID != "프로젝트/완도.md" {
		t.Errorf("OnAnswer args = action %q, refID %q", gotAction, gotItem.RefID)
	}
}

// OnAnswer must NOT fire for an ordinary card, nor for a non-"dept:" action on a
// deal-question card — only a real team answer records.
func TestWorkFeedActionRun_NonAnswerSkipsOnAnswer(t *testing.T) {
	cases := []struct {
		name     string
		item     workfeed.Item
		actionID string
	}{
		{"ordinary card", workfeed.Item{ID: "m1", Source: "mail"}, "ack"},
		{"deal question, non-dept action", workfeed.Item{ID: "q2", Source: "deal_question", RefID: "x.md"}, "trash"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			deps := WorkFeedDeps{
				Store:    &fakeWorkFeedStore{runItem: tc.item},
				OnAnswer: func(workfeed.Item, string) { calls++ },
			}
			resp := workFeedActionRun(deps)(authedCtx(), reqWith(t, "miniapp.workfeed.action.run", map[string]any{
				"itemId": tc.item.ID, "actionId": tc.actionID,
			}))
			if !resp.OK {
				t.Fatalf("expected ok: %+v", resp.Error)
			}
			if calls != 0 {
				t.Errorf("OnAnswer fired %d times, want 0", calls)
			}
		})
	}
}
