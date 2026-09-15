package mailanalysis

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/gmail"
)

// scriptedLLM answers each request with the next scripted step, per model.
//   - "ok:<json>"   a normal stream carrying <json>, then a usage chunk
//   - "overloaded"  HTTP 200 + an in-stream {"error":{"code":502,...}} (OpenRouter)
//   - "nocode"      HTTP 200 + an in-stream error without a code
//   - "http400"     an HTTP 400 before any stream
//   - "badjson"     a normal stream whose content does not parse
type scriptedLLM struct {
	server *httptest.Server
	mu     sync.Mutex
	script map[string][]string
	calls  map[string]int
	bodies []map[string]any
}

func newScriptedLLM(t *testing.T, script map[string][]string) *scriptedLLM {
	t.Helper()
	s := &scriptedLLM{script: script, calls: map[string]int{}}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		model, _ := body["model"].(string)
		s.mu.Lock()
		s.bodies = append(s.bodies, body)
		steps := s.script[model]
		i := s.calls[model]
		s.calls[model]++
		step := "ok:{}"
		if i < len(steps) {
			step = steps[i]
		} else if len(steps) > 0 {
			step = steps[len(steps)-1]
		}
		s.mu.Unlock()
		switch {
		case step == "http400":
			http.Error(w, `{"error":{"message":"bad request"}}`, http.StatusBadRequest)
			return
		case step == "overloaded":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"error\":{\"code\":502,\"message\":\"Upstream error from Nvidia: Service temporarily overloaded\"}}\n\n")
			return
		case step == "nocode":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"error\":{\"message\":\"generation failed\"}}\n\n")
			return
		}
		content := strings.TrimPrefix(step, "ok:")
		if step == "badjson" {
			content = "not json at all"
		}
		chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": content}, "finish_reason": "stop"}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":7}}\n\ndata: [DONE]\n\n", chunk)
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *scriptedLLM) client(opts ...llm.ClientOption) *llm.Client {
	return llm.NewClient(s.server.URL, "k", append([]llm.ClientOption{llm.WithRetry(0, 0, 0)}, opts...)...)
}

func (s *scriptedLLM) callsFor(model string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls[model]
}

type stageProbe struct {
	Name string `json:"name"`
}

func withFastStageRetry(t *testing.T) {
	t.Helper()
	prev := stageRetryPause
	stageRetryPause = time.Millisecond
	t.Cleanup(func() { stageRetryPause = prev })
}

// The engine reported down: the gate refuses the stage-1 model at once, and the
// extraction runs on tiny's fallback without a pointless second attempt.
func TestStageOneMovesOnWhenEngineReportedDown(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{"fallback": {`ok:{"name":"결재"}`}})
	var asked int
	down := llmSrv.client(llm.WithBackendDownCheck(func(model string) bool {
		if model == "tiny" {
			asked++
			return true
		}
		return false
	}))
	targets := []LocalTarget{{Client: down, Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	got, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err != nil || got.Name != "결재" {
		t.Fatalf("result = %+v/%v, want the fallback's extraction", got, err)
	}
	if n := llmSrv.callsFor("tiny"); n != 0 {
		t.Fatalf("tiny was requested %d times although its engine was reported down", n)
	}
	// The refusal is the same on a second attempt; it must not be made.
	if asked != 1 {
		t.Fatalf("liveness gate asked %d times for tiny, want 1 (no second attempt on a refused model)", asked)
	}
}

func TestStageOneMovesOnAfterInBandOverload(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"overloaded", "overloaded"},
		"fallback": {`ok:{"name":"발주"}`},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	got, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err != nil || got.Name != "발주" {
		t.Fatalf("result = %+v/%v, want the fallback's extraction", got, err)
	}
	if want := 1 + llm.InBandRetries; llmSrv.callsFor("tiny") != want {
		t.Fatalf("tiny attempts = %d, want %d (the helper path's in-band retries) before moving on", llmSrv.callsFor("tiny"), want)
	}
}

func TestStageOneRecoversOnSameModelAfterOneOverload(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"overloaded", `ok:{"name":"견적"}`},
		"fallback": {`ok:{"name":"wrong"}`},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	got, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err != nil || got.Name != "견적" {
		t.Fatalf("result = %+v/%v, want tiny's own second attempt", got, err)
	}
	if n := llmSrv.callsFor("fallback"); n != 0 {
		t.Fatalf("fallback was asked %d times although tiny recovered", n)
	}
}

func TestStageOneMovesOnAfterFailedStart(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"http400", "http400"},
		"fallback": {`ok:{"name":"계약"}`},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	got, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err != nil || got.Name != "계약" {
		t.Fatalf("result = %+v/%v, want the fallback's extraction", got, err)
	}
}

// An answer that does not parse is the extraction's result. Sending the mail to
// another model for a second opinion is not what a fallback is for.
func TestStageOneDoesNotMoveOnForUnparseableAnswer(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"badjson", "badjson"},
		"fallback": {`ok:{"name":"must not be used"}`},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	_, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err == nil || !strings.Contains(err.Error(), "JSON parse failed") {
		t.Fatalf("err = %v, want the parse failure", err)
	}
	if n := llmSrv.callsFor("fallback"); n != 0 {
		t.Fatalf("fallback was asked %d times for a content failure", n)
	}
}

func TestStageOneDoesNotMoveOnForDeterministicStreamError(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"nocode", "nocode"},
		"fallback": {`ok:{"name":"must not be used"}`},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	_, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err == nil || !strings.Contains(err.Error(), "LLM stream error: generation failed") {
		t.Fatalf("err = %v, want the stream error surfaced", err)
	}
	if n := llmSrv.callsFor("fallback"); n != 0 {
		t.Fatalf("fallback was asked %d times for a code-less error", n)
	}
}

func TestStageOneAllTargetsFailedSaysSo(t *testing.T) {
	withFastStageRetry(t)
	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {"overloaded"},
		"fallback": {"overloaded"},
	})
	targets := []LocalTarget{{Client: llmSrv.client(), Model: "tiny"}, {Client: llmSrv.client(), Model: "fallback"}}

	_, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil)
	if err == nil || !strings.Contains(err.Error(), "all 2 stage-1 models failed") {
		t.Fatalf("err = %v, want every target named as failed", err)
	}
}

func TestLocalTargetsDeduplicatesAndSkipsEmpty(t *testing.T) {
	c := llm.NewClient("http://127.0.0.1:1", "k")
	deps := PipelineDeps{
		LocalClient: c, LocalModel: "tiny",
		LocalFallbacks: []LocalTarget{{Client: c, Model: "tiny"}, {Client: nil, Model: "x"}, {Client: c, Model: ""}, {Client: c, Model: "free"}},
	}
	var models []string
	for _, target := range deps.localTargets() {
		models = append(models, target.Model)
	}
	if strings.Join(models, ",") != "tiny,free" {
		t.Fatalf("localTargets = %v, want tiny then free", models)
	}
}

// Every stage-1 extractor reaches the fallback when the stage-1 model's engine is
// reported down. extractDealInfo once still called its model directly — the one
// extraction that kept failing with the engine down.
func TestEveryStageOneExtractorMovesOnWhenTheEngineIsDown(t *testing.T) {
	withFastStageRetry(t)
	const text = "한빛솔라 견적서: 모듈 500kW, 단가 310원/W, 계약금 30%."
	cases := map[string]func(context.Context, PipelineDeps){
		"status signal": func(ctx context.Context, d PipelineDeps) { extractStatusSignal(ctx, d, text) },
		"wiki facts":    func(ctx context.Context, d PipelineDeps) { extractFactsForWiki(ctx, d, text) },
		"action items":  func(ctx context.Context, d PipelineDeps) { extractActionItems(ctx, d, text) },
		"deal info":     func(ctx context.Context, d PipelineDeps) { extractDealInfo(ctx, d, text) },
		"deal facts":    func(ctx context.Context, d PipelineDeps) { extractDealFacts(ctx, d, text) },
		"attachment gate": func(ctx context.Context, d PipelineDeps) {
			judgeAttachments(ctx, d, &gmail.MessageDetail{Subject: "견적"}, []extractedAttachment{{
				att:  gmail.AttachmentInfo{Filename: "견적서.pdf", MimeType: "application/pdf"},
				text: text,
			}})
		},
		"thread context": func(ctx context.Context, d PipelineDeps) {
			d.ThreadSource = threadSourceFunc(func(context.Context, *gmail.MessageDetail) ([]*gmail.MessageDetail, error) {
				return []*gmail.MessageDetail{{Subject: "이전 메일", Body: text}}, nil
			})
			_, _ = extractThreadContext(ctx, d, &gmail.MessageDetail{Subject: "회신", Body: text})
		},
		"approval cost": func(ctx context.Context, d PipelineDeps) {
			ExtractApprovalCostFacts(ctx, d.LocalClient, d.LocalModel, quietMailLogger(), text, d.LocalFallbacks...)
		},
	}
	for name, run := range cases {
		t.Run(name, func(t *testing.T) {
			llmSrv := newScriptedLLM(t, nil)
			down := llmSrv.client(llm.WithBackendDownCheck(func(model string) bool { return model == "tiny" }))
			deps := PipelineDeps{
				LocalClient:    down,
				LocalModel:     "tiny",
				LocalFallbacks: []LocalTarget{{Client: llmSrv.client(), Model: "fallback"}},
			}
			run(context.Background(), deps)
			if n := llmSrv.callsFor("fallback"); n == 0 {
				t.Fatalf("%s never asked the fallback with the stage-1 engine down", name)
			}
		})
	}
}

// A fallback's tokens are booked under the fallback's provider. The usage tag
// installed at startup names the stage-1 model's provider; left alone, an
// OpenRouter answer would be recorded as the local router's.
func TestStageOneUsageNamesTheProviderThatAnswered(t *testing.T) {
	withFastStageRetry(t)
	dir := t.TempDir()
	prev := pkgLocalUsage.Load()
	t.Cleanup(func() { pkgLocalUsage.Store(prev) })
	SetLocalUsageLog(agentlog.NewWriter(dir), "tiny", "wormhole")

	llmSrv := newScriptedLLM(t, map[string][]string{
		"tiny":     {`ok:{"name":"견적"}`, "http400"},
		"fallback": {`ok:{"name":"계약"}`},
	})
	targets := []LocalTarget{
		{Client: llmSrv.client(), Model: "tiny"},
		{Client: llmSrv.client(), Model: "fallback", Provider: "openrouter"},
	}
	for range 2 { // first answered by tiny, second by the fallback
		if _, err := callLocalTargetsJSON[stageProbe](context.Background(), targets, "sys", "user", 64, nil); err != nil {
			t.Fatal(err)
		}
	}

	var got []agentlog.HelperLLMData
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		raw, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var entry agentlog.LogEntry
			if json.Unmarshal([]byte(line), &entry) != nil || entry.Type != agentlog.TypeHelperLLM {
				continue
			}
			var data agentlog.HelperLLMData
			if err := json.Unmarshal(entry.Data, &data); err != nil {
				t.Fatal(err)
			}
			got = append(got, data)
		}
	}
	if len(got) != 2 {
		t.Fatalf("helper usage events = %+v, want two", got)
	}
	if got[0].Model != "tiny" || got[0].Provider != "wormhole" || got[0].Role != "tiny" || got[0].OutputTokens != 7 {
		t.Errorf("stage-1 model's event = %+v, want tiny under the startup provider", got[0])
	}
	if got[1].Model != "fallback" || got[1].Provider != "openrouter" || got[1].Role != "tiny" {
		t.Errorf("fallback's event = %+v, want it under openrouter", got[1])
	}
}
