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
	ackID       string
	ackErr      error
	runItemID   string
	runActionID string
	runErr      error
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

func (f *fakeWorkFeedStore) RunAction(itemID, actionID string) (workfeed.ActionResult, error) {
	f.runItemID = itemID
	f.runActionID = actionID
	if f.runErr != nil {
		return workfeed.ActionResult{}, f.runErr
	}
	item := workfeed.Item{ID: itemID, SessionKey: "client:main"}
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
