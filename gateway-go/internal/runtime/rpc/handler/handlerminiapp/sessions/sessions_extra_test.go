package sessions

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/session"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
)

func TestSessionsRecentExcludesChannel(t *testing.T) {
	mgr := &fakeSessionsLister{
		out: []*session.Session{
			sample("client:main", 300, "client"),
			sample("cron:job", 200, "cron"),
			sample("system:health", 100, "system"),
		},
	}
	h := sessionsRecent(SessionsDeps{Manager: mgr})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", map[string]any{
		"excludeChannel": "client",
	}))
	var got struct {
		Sessions []map[string]any `json:"sessions"`
		Count    int              `json:"count"`
	}
	decode(t, resp, &got)
	if got.Count != 2 {
		t.Fatalf("count = %d, want 2 (client excluded)", got.Count)
	}
	for _, row := range got.Sessions {
		if row["channel"] == "client" {
			t.Errorf("client row leaked into excludeChannel=client: %+v", row)
		}
	}
}

func TestSessionsRecentPinsBeforeRecency(t *testing.T) {
	olderPinned := sample("pinned", 100, "client")
	olderPinned.Pinned = true
	newer := sample("fresh", 900, "client")
	mgr := &fakeSessionsLister{out: []*session.Session{newer, olderPinned}}
	h := sessionsRecent(SessionsDeps{Manager: mgr})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", nil))
	var got struct {
		Sessions []map[string]any `json:"sessions"`
	}
	decode(t, resp, &got)
	if len(got.Sessions) != 2 {
		t.Fatalf("sessions = %d, want 2", len(got.Sessions))
	}
	if got.Sessions[0]["key"] != "pinned" {
		t.Errorf("first key = %v, want pinned conversation first", got.Sessions[0]["key"])
	}
	if got.Sessions[0]["pinned"] != true {
		t.Errorf("pinned flag missing on first row: %+v", got.Sessions[0])
	}
}

func TestSessionsRecentIncludesFocus(t *testing.T) {
	h := sessionsRecent(SessionsDeps{
		Manager: &fakeSessionsLister{out: []*session.Session{sample("client:main", 1, "client")}},
		Focus:   func() string { return "client:main:alpha" },
	})
	resp := h(authedCtx(), reqWith(t, "miniapp.sessions.recent", nil))
	var got struct {
		Focus string `json:"focus"`
	}
	decode(t, resp, &got)
	if got.Focus != "client:main:alpha" {
		t.Errorf("focus = %q, want client:main:alpha", got.Focus)
	}
}

func TestSessionsPinPatchesConversation(t *testing.T) {
	mgr := &fakeSessionsLister{out: []*session.Session{sample("client:main:a", 1, "client")}}
	resp := sessionsPin(SessionsDeps{Manager: mgr})(authedCtx(), reqWith(t, "miniapp.sessions.pin", map[string]any{
		"sessionKey": "client:main:a",
		"pinned":     true,
	}))
	var got struct {
		OK     bool `json:"ok"`
		Pinned bool `json:"pinned"`
	}
	decode(t, resp, &got)
	if !got.OK || !got.Pinned {
		t.Fatalf("got %+v", got)
	}
	if patch, ok := mgr.patched["client:main:a"]; !ok || patch.Pinned == nil || !*patch.Pinned {
		t.Fatalf("patch = %+v, want Pinned=true", mgr.patched["client:main:a"])
	}
	if !mgr.out[0].Pinned {
		t.Fatal("in-memory row was not pinned")
	}
}

func TestSessionsFocusSetsAndReturns(t *testing.T) {
	var stored string
	resp := sessionsFocus(SessionsDeps{
		Manager:  &fakeSessionsLister{},
		SetFocus: func(key string) error { stored = key; return nil },
		Focus:    func() string { return stored },
	})(authedCtx(), reqWith(t, "miniapp.sessions.focus", map[string]any{
		"sessionKey": " client:main:beta ",
	}))
	var got sessionFocusResult
	decode(t, resp, &got)
	if stored != "client:main:beta" {
		t.Errorf("stored = %q", stored)
	}
	if got.SessionKey != "client:main:beta" {
		t.Errorf("focus = %q", got.SessionKey)
	}
}

func TestSessionsSearchMatchesLabelAndTranscript(t *testing.T) {
	labeled := sample("client:main:alpha", 1, "client")
	labeled.Label = "주간 보고"
	mgr := &fakeSessionsLister{out: []*session.Session{labeled}}
	loader := &fakeTranscriptLoader{
		searchFn: func(query string, maxResults int) ([]toolport.SearchResult, error) {
			if query != "계약" {
				return nil, nil
			}
			return []toolport.SearchResult{{
				SessionKey: "client:main:beta",
				Matches: []toolport.MatchedMsg{{
					Message: toolport.ChatMessage{Content: json.RawMessage(`"계약서 초안 검토"`)},
				}},
			}}, nil
		},
	}
	resp := sessionsSearch(SessionsDeps{
		Manager:     mgr,
		Transcripts: func() (TranscriptLoader, error) { return loader, nil },
	})(authedCtx(), reqWith(t, "miniapp.sessions.search", map[string]any{
		"query": "계약",
	}))
	var got sessionSearchResult
	decode(t, resp, &got)
	if len(got.Hits) != 1 {
		t.Fatalf("hits = %+v, want transcript hit only (label is 주간 보고)", got.Hits)
	}
	if got.Hits[0].SessionKey != "client:main:beta" {
		t.Errorf("hit key = %q", got.Hits[0].SessionKey)
	}
	if got.Hits[0].Snippet != "계약서 초안 검토" {
		t.Errorf("snippet = %q", got.Hits[0].Snippet)
	}

	resp = sessionsSearch(SessionsDeps{
		Manager:     mgr,
		Transcripts: func() (TranscriptLoader, error) { return loader, nil },
	})(authedCtx(), reqWith(t, "miniapp.sessions.search", map[string]any{
		"query": "주간",
	}))
	decode(t, resp, &got)
	if len(got.Hits) != 1 || got.Hits[0].SessionKey != "client:main:alpha" {
		t.Fatalf("label hits = %+v", got.Hits)
	}
}
