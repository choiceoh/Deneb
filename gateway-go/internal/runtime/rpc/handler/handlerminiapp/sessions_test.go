package handlerminiapp

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

type fakeSessionsLister struct {
	out []*session.Session
}

func (f *fakeSessionsLister) List() []*session.Session { return f.out }

func sample(key string, updatedAt int64, channel string) *session.Session {
	return &session.Session{
		Key:       key,
		Kind:      session.KindDirect,
		Status:    session.StatusRunning,
		Channel:   channel,
		Model:     "qwen3.6-35b",
		UpdatedAt: updatedAt,
	}
}

func TestSessionsRecent_SortsNewestFirst(t *testing.T) {
	mgr := &fakeSessionsLister{
		out: []*session.Session{
			sample("old", 1_000, "telegram"),
			sample("new", 9_000, "telegram"),
			sample("mid", 5_000, "telegram"),
		},
	}
	h := sessionsRecent(SessionsDeps{Manager: mgr})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", nil))

	var got struct {
		Sessions []map[string]any `json:"sessions"`
		Count    int              `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != 3 {
		t.Fatalf("count = %d, want 3", got.Count)
	}
	keys := []string{}
	for _, s := range got.Sessions {
		keys = append(keys, s["key"].(string))
	}
	want := []string{"new", "mid", "old"}
	for i, k := range want {
		if keys[i] != k {
			t.Errorf("position %d: key = %q, want %q (full=%v)", i, keys[i], k, keys)
		}
	}
}

func TestSessionsRecent_Limit(t *testing.T) {
	out := make([]*session.Session, 0, 30)
	for i := range 30 {
		out = append(out, sample("s", int64(i), "telegram"))
	}
	h := sessionsRecent(SessionsDeps{Manager: &fakeSessionsLister{out: out}})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", map[string]any{"limit": 5}))

	var got struct {
		Sessions []map[string]any `json:"sessions"`
		Count    int              `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != 5 {
		t.Errorf("count = %d, want 5 (limit honored)", got.Count)
	}
}

func TestSessionsRecent_LimitClamp(t *testing.T) {
	out := make([]*session.Session, 0, 200)
	for i := range 200 {
		out = append(out, sample("s", int64(i), "telegram"))
	}
	h := sessionsRecent(SessionsDeps{Manager: &fakeSessionsLister{out: out}})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", map[string]any{"limit": 5000}))

	var got struct {
		Count int `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != maxSessionsLimit {
		t.Errorf("count = %d, want %d (clamp)", got.Count, maxSessionsLimit)
	}
}

func TestSessionsRecent_ChannelFilter(t *testing.T) {
	mgr := &fakeSessionsLister{
		out: []*session.Session{
			sample("a", 100, "telegram"),
			sample("b", 200, "openai"),
			sample("c", 300, "telegram"),
		},
	}
	h := sessionsRecent(SessionsDeps{Manager: mgr})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", map[string]any{"channel": "telegram"}))

	var got struct {
		Sessions []map[string]any `json:"sessions"`
		Count    int              `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != 2 {
		t.Fatalf("count = %d, want 2 (filtered)", got.Count)
	}
	for _, s := range got.Sessions {
		if s["channel"] != "telegram" {
			t.Errorf("unfiltered row: %+v", s)
		}
	}
}

func TestSessionsRecent_RequiresAuth(t *testing.T) {
	h := sessionsRecent(SessionsDeps{Manager: &fakeSessionsLister{}})
	resp := h(context.Background(), reqWith(t, "miniapp.sessions.recent", nil))
	if resp.OK {
		t.Fatalf("expected unauthorized, got OK")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s, want %s", resp.Error.Code, protocol.ErrUnauthorized)
	}
}

func TestSessionsRecent_EmptyManager(t *testing.T) {
	h := sessionsRecent(SessionsDeps{Manager: &fakeSessionsLister{out: nil}})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", nil))

	var got struct {
		Sessions []map[string]any `json:"sessions"`
		Count    int              `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != 0 || len(got.Sessions) != 0 {
		t.Errorf("empty manager → count=%d sessions=%v, want 0/empty", got.Count, got.Sessions)
	}
}

func TestSessionsMethods_NilManagerReturnsNil(t *testing.T) {
	if got := SessionsMethods(SessionsDeps{Manager: nil}); got != nil {
		t.Errorf("SessionsMethods(nil) = %v, want nil", got)
	}
}
