package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
)

// parseSSEEvents splits an SSE body into (event, dataJSON) pairs, skipping
// comment (keepalive) lines. Mirrors the minimal parser the native client uses.
func parseSSEEvents(t *testing.T, body string) []struct{ Event, Data string } {
	t.Helper()
	var out []struct{ Event, Data string }
	var event string
	var data strings.Builder
	flush := func() {
		if event == "" && data.Len() == 0 {
			return
		}
		out = append(out, struct{ Event, Data string }{event, data.String()})
		event = ""
		data.Reset()
	}
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, ":"):
			// comment / keepalive — ignore
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		case line == "":
			flush()
		}
	}
	flush()
	return out
}

func TestWriteChatStreamSSE_DeltasThenDone(t *testing.T) {
	rec := httptest.NewRecorder()
	run := func(_ context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		sinks.Delta("안녕")
		sinks.Delta("하세요")
		return &chatStreamResult{Text: "안녕하세요", Model: "step3p7", FellBack: true}, nil
	}
	writeChatStreamSSE(context.Background(), context.Background(), rec, "client:test", run, nil, nil)

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}
	events := parseSSEEvents(t, rec.Body.String())
	if len(events) != 4 {
		t.Fatalf("event count = %d, want 4 (progress + 2 delta + done): %q", len(events), rec.Body.String())
	}
	if events[0].Event != "progress" || events[1].Event != "delta" || events[2].Event != "delta" {
		t.Errorf("first events = %q/%q/%q, want progress/delta/delta", events[0].Event, events[1].Event, events[2].Event)
	}
	var progress progressStreamFrame
	if err := json.Unmarshal([]byte(events[0].Data), &progress); err != nil {
		t.Fatalf("progress payload: %v", err)
	}
	if progress.Phase != "writing" || progress.Label != "답변을 작성하고 있습니다" ||
		progress.SoftDeadlineMS != chatStreamSoftDeadline.Milliseconds() ||
		progress.HardDeadlineMS != chatStreamTurnDeadline.Milliseconds() || progress.StartedAtMS <= 0 {
		t.Errorf("progress = %+v, want writing label + server deadlines", progress)
	}
	var d0 struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal([]byte(events[1].Data), &d0); err != nil || d0.Delta != "안녕" {
		t.Errorf("delta[0] = %q (err %v), want 안녕", d0.Delta, err)
	}
	if events[3].Event != "done" {
		t.Fatalf("last event = %q, want done", events[3].Event)
	}
	var done struct {
		Text     string `json:"text"`
		Model    string `json:"model"`
		FellBack bool   `json:"fellBack"`
	}
	if err := json.Unmarshal([]byte(events[3].Data), &done); err != nil {
		t.Fatalf("done payload: %v", err)
	}
	if done.Text != "안녕하세요" || done.Model != "step3p7" || !done.FellBack {
		t.Errorf("done = %+v, want {안녕하세요 step3p7 true}", done)
	}
}

// The turn is detached from the client connection: a client disconnect (native
// app backgrounded → SSE socket closed → connCtx cancelled) must NOT cancel the
// run. The turn completes on its own (live) context and produces the terminal
// frame, so the answer lands in the session transcript the client re-fetches on
// resume. This is the fix for "background a chat mid-answer → answer lost".
func TestWriteChatStreamSSE_RunSurvivesClientDisconnect(t *testing.T) {
	rec := httptest.NewRecorder()
	// The client already disconnected: connCtx is cancelled up front.
	connCtx, cancelConn := context.WithCancel(context.Background())
	cancelConn()

	ran := false
	run := func(ctx context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		ran = true
		// The run's own context must stay live despite the dead connection.
		if ctx.Err() != nil {
			t.Errorf("run ctx cancelled by client disconnect: %v", ctx.Err())
		}
		sinks.Delta("응답")
		return &chatStreamResult{Text: "완성된 응답", Model: "step3p7"}, nil
	}
	// runCtx = Background (live); connCtx = cancelled (client gone).
	writeChatStreamSSE(context.Background(), connCtx, rec, "client:test", run, nil, nil)

	if !ran {
		t.Fatal("detached run did not execute after client disconnect")
	}
	var done *struct{ Event, Data string }
	for _, ev := range parseSSEEvents(t, rec.Body.String()) {
		if ev.Event == "done" {
			e := ev
			done = &e
		}
	}
	if done == nil {
		t.Fatalf("no done frame — the detached run must complete: %q", rec.Body.String())
	}
	if !strings.Contains(done.Data, "완성된 응답") {
		t.Errorf("done frame missing completed text: %q", done.Data)
	}
}

func TestWriteChatStreamSSE_ErrorFrame(t *testing.T) {
	rec := httptest.NewRecorder()
	run := func(_ context.Context, _ chatStreamSinks) (*chatStreamResult, error) {
		return nil, errors.New("boom")
	}
	writeChatStreamSSE(context.Background(), context.Background(), rec, "client:test", run, nil, nil)

	events := parseSSEEvents(t, rec.Body.String())
	if len(events) != 1 || events[0].Event != "error" {
		t.Fatalf("events = %+v, want single error frame", events)
	}
	var e struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(events[0].Data), &e); err != nil || e.Error != "boom" {
		t.Errorf("error payload = %q (err %v), want boom", e.Error, err)
	}
}

// TestWriteChatStreamSSE_ToolAndThinkingFrames covers the live-progress frames:
// a turn that thinks, runs a tool (with a detail hint), fails one, then answers
// must interleave thinking/tool frames with deltas in arrival order, ending
// with done. Also pins the wire shape: detail/isError omitted when zero.
func TestWriteChatStreamSSE_ToolAndThinkingFrames(t *testing.T) {
	rec := httptest.NewRecorder()
	run := func(_ context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		sinks.Thinking("발신인 이력을 대조")
		sinks.Thinking("")
		sinks.Tool(chatport.ToolStreamEvent{State: "started", Tool: "gmail", ToolUseID: "tu_1", Detail: "아르고에너지"})
		sinks.Tool(chatport.ToolStreamEvent{State: "completed", Tool: "gmail", ToolUseID: "tu_1", IsError: true})
		sinks.Delta("메일 3통이 도착했습니다")
		sinks.Tool(chatport.ToolStreamEvent{}) // empty tool name must be dropped, not framed
		return &chatStreamResult{Text: "메일 3통이 도착했습니다", Model: "step3p7"}, nil
	}
	writeChatStreamSSE(context.Background(), context.Background(), rec, "client:test", run, nil, nil)

	events := parseSSEEvents(t, rec.Body.String())
	wantOrder := []string{
		"progress", "thinking", "thinking",
		"progress", "tool", "tool", "progress",
		"progress", "delta", "done",
	}
	if len(events) != len(wantOrder) {
		t.Fatalf("event count = %d, want %d: %q", len(events), len(wantOrder), rec.Body.String())
	}
	for i, want := range wantOrder {
		if events[i].Event != want {
			t.Errorf("event[%d] = %q, want %q", i, events[i].Event, want)
		}
	}
	var thinking thinkingStreamFrame
	if err := json.Unmarshal([]byte(events[1].Data), &thinking); err != nil || thinking.Preview != "발신인 이력을 대조" {
		t.Errorf("thinking payload = %+v (err %v), want preview passthrough", thinking, err)
	}
	if strings.Contains(events[2].Data, "preview") {
		t.Errorf("empty preview should be omitted from the frame: %q", events[2].Data)
	}
	var tool toolStreamFrame
	if err := json.Unmarshal([]byte(events[4].Data), &tool); err != nil {
		t.Fatalf("tool payload: %v", err)
	}
	if tool.State != "started" || tool.Tool != "gmail" || tool.ToolUseID != "tu_1" || tool.Detail != "아르고에너지" || tool.IsError {
		t.Errorf("tool[started] = %+v, want {started gmail tu_1 아르고에너지 false}", tool)
	}
	if strings.Contains(events[4].Data, "isError") {
		t.Errorf("started frame should omit isError: %q", events[4].Data)
	}
	if err := json.Unmarshal([]byte(events[5].Data), &tool); err != nil || tool.State != "completed" || !tool.IsError {
		t.Errorf("tool[completed] = %+v (err %v), want state=completed isError=true", tool, err)
	}
	if strings.Contains(events[5].Data, "detail") {
		t.Errorf("completed frame should omit empty detail: %q", events[5].Data)
	}
	var phases []string
	for _, ev := range events {
		if ev.Event != "progress" {
			continue
		}
		var frame progressStreamFrame
		if err := json.Unmarshal([]byte(ev.Data), &frame); err != nil {
			t.Fatalf("progress frame: %v", err)
		}
		phases = append(phases, frame.Phase)
	}
	if got, want := strings.Join(phases, ","), "thinking,working,reviewing,writing"; got != want {
		t.Errorf("progress phases = %q, want %q", got, want)
	}
}

// postMiniappChatStream drives the streaming handler with a client token.
func postMiniappChatStream(t *testing.T, s *Handler, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/v1/miniapp/chat/stream", bytes.NewReader(raw))
	req.Header.Set(clientauth.Header, token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ChatStream(rec, req)
	return rec
}

func TestHandleMiniappChatStream_GuardPaths(t *testing.T) {
	t.Setenv("DENEB_STATE_DIR", t.TempDir())
	token, err := clientauth.Generate()
	if err != nil {
		t.Fatalf("generate client token: %v", err)
	}
	s := New(Config{})

	// Bad token → 401 (handled before any SSE bytes).
	rec := postMiniappChatStream(t, s, token+"x", map[string]any{"message": "hi"})
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("bad token: code = %d, want 401", rec.Code)
	}

	// Empty message → 400.
	rec = postMiniappChatStream(t, s, token, map[string]any{"message": "   "})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("empty message: code = %d, want 400", rec.Code)
	}

	// Valid request but chat handler not wired → 503 (not a stream). Null it
	// explicitly so the guard is exercised without driving a real LLM turn.
	rec = postMiniappChatStream(t, s, token, map[string]any{"message": "hi"})
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("nil chat handler: code = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "chat handler not ready") {
		t.Errorf("nil chat handler: body = %q, want 'chat handler not ready'", rec.Body.String())
	}
}

func TestChatStreamDoneFrameCarriesTranslatedReasoning(t *testing.T) {
	// The native client overwrites its expandable reasoning block with the done
	// frame, so this is the surface where English reasoning actually becomes
	// Korean. The live `reasoning` deltas stay in the model's own language.
	run := func(_ context.Context, sinks chatStreamSinks) (*chatStreamResult, error) {
		sinks.Reasoning("thinking in english")
		return &chatStreamResult{Text: "답", Model: "k3", Reasoning: "thinking in english"}, nil
	}
	rec := httptest.NewRecorder()
	writeChatStreamSSE(context.Background(), context.Background(), rec, "client:test", run, nil,
		func(_ context.Context, text string) (string, bool) {
			if text != "thinking in english" {
				t.Fatalf("translator got %q", text)
			}
			return "영어로 사고함", true
		})

	var doneReasoning, liveReasoning string
	for _, ev := range parseSSEEvents(t, rec.Body.String()) {
		var payload struct {
			Reasoning string `json:"reasoning"`
		}
		if json.Unmarshal([]byte(ev.Data), &payload) != nil {
			continue
		}
		switch ev.Event {
		case "done":
			doneReasoning = payload.Reasoning
		case "reasoning":
			liveReasoning = payload.Reasoning
		}
	}
	if doneReasoning != "영어로 사고함" {
		t.Fatalf("done frame reasoning = %q, want the translation", doneReasoning)
	}
	if liveReasoning != "thinking in english" {
		t.Fatalf("live reasoning was rewritten (%q) — the stream must stay untouched", liveReasoning)
	}
}

func TestChatStreamDoneFrameFailsOpenOnTranslatorRefusal(t *testing.T) {
	run := func(_ context.Context, _ chatStreamSinks) (*chatStreamResult, error) {
		return &chatStreamResult{Text: "답", Reasoning: "thinking in english"}, nil
	}
	rec := httptest.NewRecorder()
	writeChatStreamSSE(context.Background(), context.Background(), rec, "client:test", run, nil,
		func(context.Context, string) (string, bool) { return "", false })
	if !strings.Contains(rec.Body.String(), "thinking in english") {
		t.Fatal("a refused translation must ship the original reasoning, not drop it")
	}
}
