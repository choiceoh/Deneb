package pilot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/localai"
	"github.com/choiceoh/deneb/gateway-go/internal/ai/modelrole"
)

var pilotHarness struct {
	server   *httptest.Server
	registry *modelrole.Registry
	mu       sync.Mutex
	requests []map[string]any
	modes    map[string]string
}

func TestMain(m *testing.M) {
	pilotHarness.modes = make(map[string]string)
	pilotHarness.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		model, _ := req["model"].(string)
		pilotHarness.mu.Lock()
		pilotHarness.requests = append(pilotHarness.requests, req)
		mode := pilotHarness.modes[model]
		pilotHarness.mu.Unlock()
		switch mode {
		case "http-error":
			// Use a permanent request error so this fallback-chain fixture tests
			// role routing without also paying the client's transient-error retries.
			http.Error(w, "model unavailable", http.StatusBadRequest)
			return
		case "stream-error":
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"error\":{\"message\":\"generation failed\"}}\n\n")
			return
		}
		content := "reply:" + model
		if mode == "empty" {
			content = ""
		}
		w.Header().Set("Content-Type", "text/event-stream")
		chunk := map[string]any{
			"id": "chatcmpl-test",
			"choices": []any{map[string]any{
				"index": 0,
				"delta": map[string]any{"role": "assistant", "content": content},
			}},
		}
		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", data)
	}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	providers := map[string]modelrole.ProviderResolved{
		"test": {BaseURL: pilotHarness.server.URL, APIKey: "test-key"},
	}
	pilotHarness.registry = modelrole.NewRegistryWithOptions(logger, modelrole.RegistryOptions{
		MainModel:        "test/main",
		TinyModel:        "test/tiny",
		LightweightModel: "test/light",
		FallbackModel:    "test/fallback",
		VisionModel:      "test/vision",
		Providers:        providers,
	})
	// A nil initialization attempt must not consume the once-only registry slot.
	SetModelRoleRegistry(nil)
	SetModelRoleRegistry(pilotHarness.registry)
	code := m.Run()
	pilotHarness.server.Close()
	os.Exit(code)
}

func resetPilotHarness() {
	pilotHarness.mu.Lock()
	pilotHarness.requests = nil
	pilotHarness.modes = make(map[string]string)
	pilotHarness.mu.Unlock()
	SetLocalAIHub(nil)
}

func setPilotMode(model, mode string) {
	pilotHarness.mu.Lock()
	pilotHarness.modes[model] = mode
	pilotHarness.mu.Unlock()
}

func pilotRequests() []map[string]any {
	pilotHarness.mu.Lock()
	defer pilotHarness.mu.Unlock()
	return append([]map[string]any(nil), pilotHarness.requests...)
}

func TestRoleRegistryReturnsModelAndClientPerRole(t *testing.T) {
	resetPilotHarness()
	if pkgRegistry != pilotHarness.registry {
		t.Fatalf("package registry = %p, want %p", pkgRegistry, pilotHarness.registry)
	}
	if LightweightModel() != "light" {
		t.Fatalf("LightweightModel = %q", LightweightModel())
	}
	for _, tt := range []struct {
		role  modelrole.Role
		model string
	}{
		{modelrole.RoleMain, "main"},
		{modelrole.RoleTiny, "tiny"},
		{modelrole.RoleLightweight, "light"},
		{modelrole.RoleFallback, "fallback"},
		{modelrole.RoleVision, "vision"},
	} {
		if got := getRoleModel(tt.role, "default"); got != tt.model {
			t.Errorf("role %s model = %q, want %q", tt.role, got, tt.model)
		}
		if got := getRoleClient(tt.role, "http://unused", "key"); got == nil {
			t.Errorf("role %s client is nil", tt.role)
		}
	}
}

func TestCallRoleLLMFormatsRequestWithMergedExtras(t *testing.T) {
	resetPilotHarness()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	got, err := CallRoleLLM(
		ctx, modelrole.RoleMain, "system prompt", "user prompt", 321,
		mustJSON(t, map[string]any{"temperature": 0.2, "first": true}),
		mustJSON(t, map[string]any{"top_p": 0.8, "first": false}),
	)
	if err != nil || got != "reply:main" {
		t.Fatalf("CallRoleLLM = %q/%v", got, err)
	}
	reqs := pilotRequests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %d", len(reqs))
	}
	req := reqs[0]
	if req["model"] != "main" || req["max_tokens"] != float64(321) || req["temperature"] != 0.2 || req["top_p"] != 0.8 || req["first"] != false {
		t.Fatalf("request = %#v", req)
	}
	if timeout, ok := req["timeout"].(float64); !ok || timeout <= 1 || timeout > 20 {
		t.Fatalf("server timeout = %#v", req["timeout"])
	}
	if req["stream"] != true {
		t.Fatalf("stream flag = %#v", req["stream"])
	}
}

func TestCallRoleLLMFallbackChainAndAllFailed(t *testing.T) {
	resetPilotHarness()
	setPilotMode("tiny", "http-error")
	got, err := CallRoleLLM(context.Background(), modelrole.RoleTiny, "system", "user", 100)
	if err != nil || got != "reply:light" {
		t.Fatalf("fallback result = %q/%v", got, err)
	}
	reqs := pilotRequests()
	if len(reqs) != 2 || reqs[0]["model"] != "tiny" || reqs[1]["model"] != "light" {
		t.Fatalf("fallback requests = %#v", reqs)
	}

	resetPilotHarness()
	setPilotMode("tiny", "http-error")
	setPilotMode("light", "http-error")
	setPilotMode("fallback", "http-error")
	if got, err := CallRoleLLM(context.Background(), modelrole.RoleTiny, "system", "user", 100); err == nil || got != "" || !strings.Contains(err.Error(), "all models failed") {
		t.Fatalf("all failed = %q/%v", got, err)
	}
	models := make([]any, 0, 3)
	for _, req := range pilotRequests() {
		models = append(models, req["model"])
	}
	if !reflect.DeepEqual(models, []any{"tiny", "light", "fallback"}) {
		t.Fatalf("all-failed models = %#v", models)
	}
}

func TestCallRoleLLMEmptyAndStreamErrorContracts(t *testing.T) {
	resetPilotHarness()
	setPilotMode("main", "empty")
	got, err := CallRoleLLM(context.Background(), modelrole.RoleMain, "system", "user", 50)
	if err != nil || got != "(no response from local model)" {
		t.Fatalf("empty response = %q/%v", got, err)
	}

	resetPilotHarness()
	setPilotMode("main", "stream-error")
	got, err = CallRoleLLM(context.Background(), modelrole.RoleMain, "system", "user", 50)
	if err == nil || got != "" || !strings.Contains(err.Error(), "generation failed") {
		t.Fatalf("stream error = %q/%v", got, err)
	}
}

func TestLocalAndTinyWrappersReturnRoleSpecificReplies(t *testing.T) {
	resetPilotHarness()
	local, err := CallLocalLLM(context.Background(), "system", "user", 20)
	if err != nil || local != "reply:light" {
		t.Fatalf("local wrapper = %q/%v", local, err)
	}
	tiny, err := CallTinyLLM(context.Background(), "system", "user", 20)
	if err != nil || tiny != "reply:tiny" {
		t.Fatalf("tiny wrapper = %q/%v", tiny, err)
	}
	reqs := pilotRequests()
	if len(reqs) != 2 || reqs[0]["model"] != "light" || reqs[1]["model"] != "tiny" {
		t.Fatalf("wrapper requests = %#v", reqs)
	}
}

func TestLocalAIHubPointerAndLegacyHealthStateAreRaceSafe(t *testing.T) {
	resetPilotHarness()
	localAIHealthy.Store(false)
	localAILastCheck.Store(0)
	if LocalAIRecentlyDown() {
		t.Fatal("never-checked local AI reported recently down")
	}
	localAILastCheck.Store(time.Now().Unix())
	if !LocalAIRecentlyDown() {
		t.Fatal("known unhealthy local AI was not reported down")
	}
	localAIHealthy.Store(true)
	if LocalAIRecentlyDown() {
		t.Fatal("healthy local AI reported down")
	}

	hubs := []*localai.Hub{new(localai.Hub), new(localai.Hub)}
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				SetLocalAIHub(hubs[(i+worker)%len(hubs)])
				if got := LocalAIHub(); got != hubs[0] && got != hubs[1] {
					t.Errorf("unexpected hub %p", got)
					return
				}
				_ = LocalAIRecentlyDown()
			}
		}(worker)
	}
	wg.Wait()
	SetLocalAIHub(nil)
	if LocalAIHub() != nil {
		t.Fatal("hub did not clear")
	}
}

func TestCollectStreamNilClosedUnknownAndCancellation(t *testing.T) {
	if got, err := CollectStream(context.Background(), nil); err == nil || got != "" || !strings.Contains(err.Error(), "nil") {
		t.Fatalf("nil stream = %q/%v", got, err)
	}
	closed := make(chan llm.StreamEvent)
	close(closed)
	if got, err := CollectStream(context.Background(), closed); err != nil || got != "" {
		t.Fatalf("closed stream = %q/%v", got, err)
	}
	unknown := make(chan llm.StreamEvent, 2)
	unknown <- llm.StreamEvent{Type: "ping", Payload: llm.FlexibleFromRaw([]byte(`{"ignored":true}`))}
	close(unknown)
	if got, err := CollectStream(context.Background(), unknown); err != nil || got != "" {
		t.Fatalf("unknown event = %q/%v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocking := make(chan llm.StreamEvent)
	if got, err := CollectStream(ctx, blocking); !errors.Is(err, context.Canceled) || got != "" {
		t.Fatalf("cancelled stream = %q/%v", got, err)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCollectStreamDeltaAndErrorShapeMatrix(t *testing.T) {
	for _, tt := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "plain", payload: `{"delta":{"text":"hello"}}`, want: "hello"},
		{name: "escaped newline", payload: `{"delta":{"text":"line1\nline2"}}`, want: "line1\nline2"},
		{name: "escaped quote", payload: `{"delta":{"text":"say \"hi\""}}`, want: `say "hi"`},
		{name: "unicode", payload: `{"delta":{"text":"안녕하세요"}}`, want: "안녕하세요"},
		{name: "missing", payload: `{"delta":{"type":"text_delta"}}`},
		{name: "malformed", payload: `{"delta":{"text":"unterminated}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := ExtractDeltaText([]byte(tt.payload)); got != tt.want {
				t.Fatalf("ExtractDeltaText = %q, want %q", got, tt.want)
			}
		})
	}

	for _, tt := range []struct {
		name    string
		payload string
		want    string
	}{
		{name: "top level", payload: `{"message":"top failed"}`, want: "top failed"},
		{name: "nested", payload: `{"error":{"message":"nested failed"}}`, want: "nested failed"},
		{name: "raw fallback", payload: `{"code":503}`, want: `"code":503`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ch := make(chan llm.StreamEvent, 2)
			ch <- llm.StreamEvent{Type: "content_block_delta", Payload: llm.FlexibleFromRaw([]byte(`{"delta":{"text":"partial"}}`))}
			ch <- llm.StreamEvent{Type: "error", Payload: llm.FlexibleFromRaw([]byte(tt.payload))}
			close(ch)
			got, err := CollectStream(context.Background(), ch)
			if err == nil || got != "partial" || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("CollectStream = %q/%v", got, err)
			}
		})
	}
}

func TestTruncateHeadRuneBoundariesAndLimits(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		max  int
		want string
	}{
		{name: "under", in: "가나다", max: 4, want: "가나다"},
		{name: "exact", in: "가나다", max: 3, want: "가나다"},
		{name: "unicode cut", in: "가나다라마바사", max: 3, want: "가나다\n\n[... truncated at 3 chars]"},
		{name: "zero", in: "abc", max: 0, want: "\n\n[... truncated at 0 chars]"},
		{name: "negative", in: "abc", max: -3, want: "\n\n[... truncated at 0 chars]"},
		{name: "empty", in: "", max: 0, want: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateHead(tt.in, tt.max)
			if got != tt.want || !utf8.ValidString(got) {
				t.Fatalf("TruncateHead = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCallVisionLLMRejectsInvalidFramesAndSelectsConfiguredRole(t *testing.T) {
	resetPilotHarness()
	if got, err := CallVisionLLM(context.Background(), "system", "user", nil, 100); err == nil || got != "" || !strings.Contains(err.Error(), "no frames") {
		t.Fatalf("no frames = %q/%v", got, err)
	}
	if got, err := CallVisionLLM(context.Background(), "system", "user", []VisionFrame{{MimeType: "image/png"}}, 100); err == nil || got != "" || !strings.Contains(err.Error(), "frame 0") {
		t.Fatalf("empty frame = %q/%v", got, err)
	}
	client, model := visionClientAndModel()
	if client == nil || model != "vision" {
		t.Fatalf("vision selection = %p/%q", client, model)
	}
	pilotHarness.registry.ClearRole(modelrole.RoleVision)
	client, model = visionClientAndModel()
	if client == nil || model != "main" {
		t.Fatalf("main fallback selection = %p/%q", client, model)
	}
	pilotHarness.registry.SetRoleModelID(modelrole.RoleVision, "test/vision")
}

func TestCallVisionLLMFormatsMultimodalRequestBlocks(t *testing.T) {
	resetPilotHarness()
	frames := []VisionFrame{
		{MimeType: "image/png", Base64: "cG5n"},
		{Base64: "anBlZw=="},
	}
	got, err := CallVisionLLM(context.Background(), "vision system", "describe frames", frames, 777)
	if err != nil || got != "reply:vision" {
		t.Fatalf("CallVisionLLM = %q/%v", got, err)
	}
	reqs := pilotRequests()
	if len(reqs) != 1 {
		t.Fatalf("requests = %#v", reqs)
	}
	req := reqs[0]
	if req["model"] != "vision" || req["max_tokens"] != float64(777) {
		t.Fatalf("vision request = %#v", req)
	}
	messages, ok := req["messages"].([]any)
	if !ok || len(messages) != 2 {
		t.Fatalf("messages = %#v", req["messages"])
	}
	if systemMessage, ok := messages[0].(map[string]any); !ok || systemMessage["role"] != "system" || systemMessage["content"] != "vision system" {
		t.Fatalf("system message = %#v", messages[0])
	}
	message, ok := messages[1].(map[string]any)
	if !ok || message["role"] != "user" {
		t.Fatalf("user message = %#v", messages[1])
	}
	blocks, ok := message["content"].([]any)
	if !ok || len(blocks) != 3 {
		t.Fatalf("blocks = %#v", message["content"])
	}
	if blocks[0].(map[string]any)["text"] != "describe frames" {
		t.Fatalf("text block = %#v", blocks[0])
	}
	firstImage := blocks[1].(map[string]any)["image_url"].(map[string]any)
	secondImage := blocks[2].(map[string]any)["image_url"].(map[string]any)
	if firstImage["url"] != "data:image/png;base64,cG5n" {
		t.Fatalf("first image = %#v", firstImage)
	}
	if secondImage["url"] != "data:image/jpeg;base64,anBlZw==" {
		t.Fatalf("default image = %#v", secondImage)
	}
}

func TestCallVisionLLMEmptyAndStreamErrors(t *testing.T) {
	resetPilotHarness()
	setPilotMode("vision", "empty")
	if got, err := CallVisionLLM(context.Background(), "system", "", []VisionFrame{{Base64: "eA=="}}, 50); err == nil || got != "" || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("empty vision = %q/%v", got, err)
	}
	resetPilotHarness()
	setPilotMode("vision", "stream-error")
	if got, err := CallVisionLLM(context.Background(), "system", "", []VisionFrame{{Base64: "eA=="}}, 50); err == nil || got != "" || !strings.Contains(err.Error(), "generation failed") {
		t.Fatalf("vision stream error = %q/%v", got, err)
	}
}
