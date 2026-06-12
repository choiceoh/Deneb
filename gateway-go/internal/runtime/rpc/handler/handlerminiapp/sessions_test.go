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
	out     []*session.Session
	deleted []string // keys passed to Delete, in order
}

func (f *fakeSessionsLister) List() []*session.Session { return f.out }

func (f *fakeSessionsLister) Get(key string) *session.Session {
	for _, s := range f.out {
		if s.Key == key {
			return s
		}
	}
	return nil
}

func (f *fakeSessionsLister) Delete(key string) bool {
	f.deleted = append(f.deleted, key)
	for i, s := range f.out {
		if s.Key == key {
			f.out = append(f.out[:i], f.out[i+1:]...)
			return true
		}
	}
	return false
}

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

// --- sessions.delete tests ------------------------------------------------

func terminalSample(key string) *session.Session {
	return &session.Session{
		Key:       key,
		Kind:      session.KindDirect,
		Status:    session.StatusDone,
		UpdatedAt: 1_000,
	}
}

func TestSessionsDelete_RemovesSessionAndTranscript(t *testing.T) {
	mgr := &fakeSessionsLister{out: []*session.Session{terminalSample("client:main:abc")}}
	tr := &fakeTranscriptLoader{}
	deps := SessionsDeps{
		Manager:     mgr,
		Transcripts: func() (TranscriptLoader, error) { return tr, nil },
	}
	resp := sessionsDelete(deps)(authedCtx(), reqWith(t, "miniapp.sessions.delete", map[string]any{
		"sessionKey": "client:main:abc",
	}))

	var got struct {
		Deleted bool `json:"deleted"`
	}
	decode(t, resp, &got)
	if !got.Deleted {
		t.Errorf("deleted = false, want true")
	}
	if len(mgr.deleted) != 1 || mgr.deleted[0] != "client:main:abc" {
		t.Errorf("manager.Delete keys = %v, want [client:main:abc]", mgr.deleted)
	}
	if len(tr.deleted) != 1 || tr.deleted[0] != "client:main:abc" {
		t.Errorf("transcript.Delete keys = %v, want [client:main:abc]", tr.deleted)
	}
}

func TestSessionsDelete_RunningProtected(t *testing.T) {
	// sample() builds a StatusRunning session; without force we must not delete
	// it — the in-flight turn would re-Set it on completion and resurrect the row.
	mgr := &fakeSessionsLister{out: []*session.Session{sample("client:main:live", 1_000, "")}}
	tr := &fakeTranscriptLoader{}
	deps := SessionsDeps{
		Manager:     mgr,
		Transcripts: func() (TranscriptLoader, error) { return tr, nil },
	}
	resp := sessionsDelete(deps)(authedCtx(), reqWith(t, "miniapp.sessions.delete", map[string]any{
		"sessionKey": "client:main:live",
	}))

	var got struct {
		Deleted bool `json:"deleted"`
	}
	decode(t, resp, &got)
	if got.Deleted {
		t.Errorf("deleted = true, want false (running session protected)")
	}
	if len(mgr.deleted) != 0 {
		t.Errorf("manager.Delete called on running session: %v", mgr.deleted)
	}
	if len(tr.deleted) != 0 {
		t.Errorf("transcript.Delete called on running session: %v", tr.deleted)
	}
}

func TestSessionsDelete_ForceDeletesRunning(t *testing.T) {
	mgr := &fakeSessionsLister{out: []*session.Session{sample("client:main:live", 1_000, "")}}
	resp := sessionsDelete(SessionsDeps{Manager: mgr})(authedCtx(), reqWith(t, "miniapp.sessions.delete", map[string]any{
		"sessionKey": "client:main:live", "force": true,
	}))

	var got struct {
		Deleted bool `json:"deleted"`
	}
	decode(t, resp, &got)
	if !got.Deleted {
		t.Errorf("deleted = false, want true (force overrides running guard)")
	}
	if len(mgr.deleted) != 1 {
		t.Errorf("manager.Delete keys = %v, want exactly one", mgr.deleted)
	}
}

func TestSessionsDelete_NonexistentReturnsFalse(t *testing.T) {
	mgr := &fakeSessionsLister{out: nil}
	resp := sessionsDelete(SessionsDeps{Manager: mgr})(authedCtx(), reqWith(t, "miniapp.sessions.delete", map[string]any{
		"sessionKey": "ghost",
	}))

	var got struct {
		Deleted bool `json:"deleted"`
	}
	decode(t, resp, &got)
	if got.Deleted {
		t.Errorf("deleted = true, want false (no such session)")
	}
}

func TestSessionsDelete_MissingKey(t *testing.T) {
	resp := sessionsDelete(SessionsDeps{Manager: &fakeSessionsLister{}})(authedCtx(), reqWith(t, "miniapp.sessions.delete", map[string]any{}))
	if resp.OK {
		t.Fatalf("expected error")
	}
	if resp.Error.Code != protocol.ErrMissingParam {
		t.Errorf("code = %s, want MISSING_PARAM", resp.Error.Code)
	}
}

func TestSessionsDelete_RequiresAuth(t *testing.T) {
	resp := sessionsDelete(SessionsDeps{Manager: &fakeSessionsLister{}})(context.Background(), reqWith(t, "miniapp.sessions.delete", map[string]any{
		"sessionKey": "k",
	}))
	if resp.OK {
		t.Fatalf("expected unauthorized")
	}
	if resp.Error.Code != protocol.ErrUnauthorized {
		t.Errorf("code = %s", resp.Error.Code)
	}
}

func TestSessionsMethods_NilManagerReturnsNil(t *testing.T) {
	if got := SessionsMethods(SessionsDeps{Manager: nil}); got != nil {
		t.Errorf("SessionsMethods(nil) = %v, want nil", got)
	}
}

func TestSessionsMethods_TranscriptOptional(t *testing.T) {
	// Without Transcripts factory: sessions.recent + sessions.delete register
	// (both need only the Manager); sessions.transcript does not.
	got := SessionsMethods(SessionsDeps{Manager: &fakeSessionsLister{}})
	if _, ok := got["miniapp.sessions.recent"]; !ok {
		t.Errorf("sessions.recent missing")
	}
	if _, ok := got["miniapp.sessions.delete"]; !ok {
		t.Errorf("sessions.delete missing")
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
	loadFn    func(key string, limit int) ([]toolctx.ChatMessage, int, error)
	deleted   []string // keys passed to Delete, in order
	deleteErr error
}

func (f *fakeTranscriptLoader) Load(key string, limit int) ([]toolctx.ChatMessage, int, error) {
	if f.loadFn == nil {
		return nil, 0, errors.New("Load not stubbed")
	}
	return f.loadFn(key, limit)
}

func (f *fakeTranscriptLoader) Delete(key string) error {
	f.deleted = append(f.deleted, key)
	return f.deleteErr
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
	if strings.Contains(got.Messages[0].Content, "gmail") {
		t.Errorf("tool_use must not render in a bubble: %q", got.Messages[0].Content)
	}
	if !strings.Contains(got.Messages[0].Content, "After tool") {
		t.Errorf("second text block missing: %q", got.Messages[0].Content)
	}
}

// Regression for the ps-dump leak: tool results are persisted as user-role
// messages, and the old decode rendered them as "↩️ <raw stdout>" bubbles the
// user could quote. The transcript RPC must drop tool_result messages, hide
// thinking/tool_use machinery, and trim link-enrichment appendages — the same
// display sanitation chat.history applies.
func TestSessionsTranscript_HidesToolMachineryAndEnrichment(t *testing.T) {
	enriched := "이 링크 봐줘 https://example.com\n\n---\n" +
		toolctx.LinkEnrichmentHeader + "\n\npage dump\n---"
	loader := &fakeTranscriptLoader{
		loadFn: func(_ string, _ int) ([]toolctx.ChatMessage, int, error) {
			return []toolctx.ChatMessage{
				toolctx.NewTextChatMessage("user", enriched, 0),
				{Role: "assistant", Content: jsonRaw(`[
					{"type": "thinking", "thinking": "고민"},
					{"type": "text", "text": "재시작해볼게요"},
					{"type": "tool_use", "id": "t1", "name": "exec", "input": {"command": "ps aux"}}
				]`)},
				{Role: "user", Content: jsonRaw(`[
					{"type": "tool_result", "tool_use_id": "t1", "content": "choiceoh 2495893 ... /home/choiceoh/.claude/remote/srv/... ps dump"}
				]`)},
				toolctx.NewTextChatMessage("assistant", "완료했습니다.", 0),
			}, 4, nil
		},
	}
	h := sessionsTranscript(transcriptDeps(loader))
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.transcript", map[string]any{
		"sessionKey": "k",
	}))
	var got struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decode(t, resp, &got)

	if len(got.Messages) != 3 {
		t.Fatalf("tool_result-only message must be dropped, got %d rows: %+v", len(got.Messages), got.Messages)
	}
	if got.Messages[0].Content != "이 링크 봐줘 https://example.com" {
		t.Errorf("enrichment not trimmed from user bubble: %q", got.Messages[0].Content)
	}
	if got.Messages[1].Content != "재시작해볼게요" {
		t.Errorf("assistant bubble must be text-only: %q", got.Messages[1].Content)
	}
	for _, m := range got.Messages {
		if strings.Contains(m.Content, "ps dump") || strings.Contains(m.Content, "↩️") {
			t.Errorf("raw tool output leaked into transcript view: %q", m.Content)
		}
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
		{"tool_use hidden", jsonRaw(`[{"type":"tool_use","name":"exec"}]`), ""},
		{"tool_result hidden", jsonRaw(`[{"type":"tool_result","content":"raw stdout"}]`), ""},
		{"thinking hidden", jsonRaw(`[{"type":"thinking","thinking":"hmm"}]`), ""},
		{
			"text joined across hidden blocks",
			jsonRaw(`[{"type":"thinking","thinking":"x"},{"type":"text","text":"a"},{"type":"tool_use","name":"exec"},{"type":"text","text":"b"}]`),
			"a\n\nb",
		},
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
