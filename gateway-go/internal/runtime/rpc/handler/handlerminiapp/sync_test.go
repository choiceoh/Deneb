package handlerminiapp

import (
	"context"
	"errors"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/nativesync"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeNativeSyncStore struct {
	cursor int64
	limit  int
	err    error
}

func (f *fakeNativeSyncStore) Pull(afterSeq int64, limit int) (nativesync.PullResult, error) {
	f.cursor = afterSeq
	f.limit = limit
	if f.err != nil {
		return nativesync.PullResult{}, f.err
	}
	return nativesync.PullResult{
		Events: []nativesync.Event{
			{Seq: afterSeq + 1, Type: nativesync.TypeWorkFeedCreated, WorkFeedItemID: "wf_1"},
		},
		Cursor:    afterSeq + 1,
		LatestSeq: afterSeq + 1,
	}, nil
}

func TestSyncMethodsNilStoreReturnsNil(t *testing.T) {
	if got := SyncMethods(SyncDeps{}); got != nil {
		t.Fatalf("SyncMethods(nil) = %v, want nil", got)
	}
}

func TestSyncPull(t *testing.T) {
	store := &fakeNativeSyncStore{}
	h := syncPull(SyncDeps{Store: store})
	resp := h(authedCtx(), reqWith(t, "miniapp.sync.pull", map[string]any{
		"cursor": 4,
		"limit":  9999,
	}))

	var got struct {
		Events    []nativesync.Event `json:"events"`
		Cursor    int64              `json:"cursor"`
		LatestSeq int64              `json:"latestSeq"`
		Count     int                `json:"count"`
	}
	decode(t, resp, &got)
	if store.cursor != 4 || store.limit != maxSyncLimit {
		t.Fatalf("pull args = cursor %d limit %d", store.cursor, store.limit)
	}
	if got.Cursor != 5 || got.LatestSeq != 5 || got.Count != 1 || len(got.Events) != 1 {
		t.Fatalf("payload = %+v", got)
	}
}

func TestSyncPullRequiresAuth(t *testing.T) {
	h := syncPull(SyncDeps{Store: &fakeNativeSyncStore{}})
	resp := h(context.Background(), reqWith(t, "miniapp.sync.pull", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestSyncPullUnavailable(t *testing.T) {
	h := syncPull(SyncDeps{Store: &fakeNativeSyncStore{err: errors.New("disk down")}})
	resp := h(authedCtx(), reqWith(t, "miniapp.sync.pull", nil))
	if resp.OK {
		t.Fatalf("expected unavailable")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Fatalf("code = %s, want %s", resp.Error.Code, protocol.ErrUnavailable)
	}
}
