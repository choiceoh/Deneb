package handlerminiapp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
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

func TestSessionsMethods_TranscriptOptional(t *testing.T) {
	// Without Transcripts factory: only sessions.recent registers.
	got := SessionsMethods(SessionsDeps{Manager: &fakeSessionsLister{}})
	if _, ok := got["miniapp.sessions.recent"]; !ok {
		t.Errorf("sessions.recent missing")
	}
	if _, ok := got["miniapp.sessions.transcript"]; ok {
		t.Errorf("transcript should not register when factory missing")
	}

	// With Transcripts factory: both register.
	got = SessionsMethods(SessionsDeps{
		Manager:     &fakeSessionsLister{},
		Transcripts: func() (TranscriptLoader, error) { return &fakeTranscriptLoader{}, nil },
	})
	if _, ok := got["miniapp.sessions.transcript"]; !ok {
		t.Errorf("transcript should register when factory set")
	}
}

// --- transcript tests -----------------------------------------------------

type fakeTranscriptLoader struct {
	loadFn func(key string, limit int) ([]toolctx.ChatMessage, int, error)
}

func (f *fakeTranscriptLoader) Load(key string, limit int) ([]toolctx.ChatMessage, int, error) {
	if f.loadFn == nil {
		return nil, 0, errors.New("Load not stubbed")
	}
	return f.loadFn(key, limit)
}

func transcriptDeps(loader TranscriptLoader) SessionsDeps {
	return SessionsDeps{
		Manager:     &fakeSessionsLister{},
		Transcripts: func() (TranscriptLoader, error) { return loader, nil },
	}
}

func TestSessionsTranscript_HappyPath(t *testing.T) {
	loader := &fakeTranscriptLoader{
		loadFn: func(key string, limit int) ([]toolctx.ChatMessage, int, error) {
			if key != "telegram:123" {
				t.Errorf("key = %q", key)
			}
			if limit != defaultTranscriptLimit {
				t.Errorf("limit = %d, want %d", limit, defaultTranscriptLimit)
			}
			return []toolctx.ChatMessage{
				{ID: "m1", Role: "user", Content: jsonRaw(`"안녕"`), Timestamp: 1_700_000_000_000},
				{ID: "m2", Role: "assistant", Content: jsonRaw(`"안녕하세요"`), Timestamp: 1_700_000_001_000},
			}, 42, nil
		},
	}
	h := sessionsTranscript(transcriptDeps(loader))
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{
		"sessionKey": "telegram:123",
	}))

	var got struct {
		SessionKey string `json:"sessionKey"`
		Messages   []struct {
			ID          string `json:"id"`
			Role        string `json:"role"`
			Content     string `json:"content"`
			TimestampMs int64  `json:"timestampMs"`
		} `json:"messages"`
		Total int `json:"total"`
	}
	decode(t, resp, &got)
	if got.SessionKey != "telegram:123" || got.Total != 42 {
		t.Errorf("payload header wrong: %+v", got)
	}
	if len(got.Messages) != 2 || got.Messages[0].Content != "안녕" {
		t.Errorf("messages decoded wrong: %+v", got.Messages)
	}
	if got.Messages[1].Role != "assistant" {
		t.Errorf("role wrong: %+v", got.Messages[1])
	}
}

func TestSessionsTranscript_DecodesBlocks(t *testing.T) {
	loader := &fakeTranscriptLoader{
		loadFn: func(_ string, _ int) ([]toolctx.ChatMessage, int, error) {
			// Content as an array of ContentBlock-like objects.
			content := jsonRaw(`[
				{"type": "text", "text": "Hello"},
				{"type": "tool_use", "name": "gmail"},
				{"type": "text", "text": "After tool"}
			]`)
			return []toolctx.ChatMessage{
				{ID: "m1", Role: "assistant", Content: content},
			}, 1, nil
		},
	}
	h := sessionsTranscript(transcriptDeps(loader))
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{
		"sessionKey": "k",
	}))
	var got struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	decode(t, resp, &got)
	if !strings.Contains(got.Messages[0].Content, "Hello") {
		t.Errorf("text block missing: %q", got.Messages[0].Content)
	}
	if !strings.Contains(got.Messages[0].Content, "⚙️ gmail") {
		t.Errorf("tool_use marker missing: %q", got.Messages[0].Content)
	}
	if !strings.Contains(got.Messages[0].Content, "After tool") {
		t.Errorf("second text block missing: %q", got.Messages[0].Content)
	}
}

func TestSessionsTranscript_LimitClamp(t *testing.T) {
	var seenLimit int
	loader := &fakeTranscriptLoader{
		loadFn: func(_ string, limit int) ([]toolctx.ChatMessage, int, error) {
			seenLimit = limit
			return nil, 0, nil
		},
	}
	h := sessionsTranscript(transcriptDeps(loader))
	h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{
		"sessionKey": "k", "limit": 9999,
	}))
	if seenLimit != maxTranscriptLimit {
		t.Errorf("limit = %d, want clamped to %d", seenLimit, maxTranscriptLimit)
	}
}

func TestSessionsTranscript_MissingKey(t *testing.T) {
	h := sessionsTranscript(transcriptDeps(&fakeTranscriptLoader{}))
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestSessionsTranscript_RequiresAuth(t *testing.T) {
	h := sessionsTranscript(transcriptDeps(&fakeTranscriptLoader{}))
	resp := h(context.Background(), reqWith(t, "miniapp.sessions.transcript", map[string]any{
		"sessionKey": "k",
	}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestSessionsTranscript_LoaderError(t *testing.T) {
	loader := &fakeTranscriptLoader{
		loadFn: func(_ string, _ int) ([]toolctx.ChatMessage, int, error) {
			return nil, 0, errors.New("io broken")
		},
	}
	h := sessionsTranscript(transcriptDeps(loader))
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{"sessionKey": "k"}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrUnavailable {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestDecodeChatContent(t *testing.T) {
	cases := []struct {
		name string
		in   json.RawMessage
		want string
	}{
		{"empty", nil, ""},
		{"string", jsonRaw(`"hello"`), "hello"},
		{"text block", jsonRaw(`[{"type":"text","text":"hi"}]`), "hi"},
		{"unknown block", jsonRaw(`[{"type":"weird"}]`), "[weird]"},
	}
	for _, c := range cases {
		got := decodeChatContent(c.in)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func jsonRaw(s string) json.RawMessage {
	return json.RawMessage(s)
}
