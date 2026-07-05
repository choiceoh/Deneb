package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseUsageTail(t *testing.T) {
	// OpenAI non-streaming JSON.
	in, out := parseUsageTail([]byte(`{"id":"x","usage":{"prompt_tokens":1200,"completion_tokens":34},"choices":[]}`))
	if in != 1200 || out != 34 {
		t.Errorf("openai json: got in=%d out=%d", in, out)
	}
	// SSE stream, Anthropic dialect, interim + final usage — last one wins.
	sse := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":900,\"output_tokens\":1}}}\n\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"input_tokens\":900,\"output_tokens\":210}}\n\n" +
		"data: [DONE]\n\n"
	in, out = parseUsageTail([]byte(sse))
	if in != 900 || out != 210 {
		t.Errorf("anthropic sse: got in=%d out=%d", in, out)
	}
	// Usage mentioned inside a string value must not confuse the balancer;
	// garbage yields zeros rather than an error.
	if in, out = parseUsageTail([]byte(`data: {"usage":{"prompt_tok`)); in != 0 || out != 0 {
		t.Errorf("truncated usage should yield zeros, got in=%d out=%d", in, out)
	}
	if in, out = parseUsageTail(nil); in != 0 || out != 0 {
		t.Errorf("nil tail should yield zeros")
	}
}

func TestUsageTail_PassthroughAndSingleFire(t *testing.T) {
	payload := strings.Repeat("x", 3000) + `{"usage":{"prompt_tokens":5,"completion_tokens":7}}`
	fired := 0
	var gotTail []byte
	tee := newUsageTail(io.NopCloser(strings.NewReader(payload)), func(tail []byte) {
		fired++
		gotTail = tail
	})
	read, err := io.ReadAll(tee)
	if err != nil {
		t.Fatal(err)
	}
	if string(read) != payload {
		t.Fatal("tee mutated the byte stream")
	}
	_ = tee.Close() // Close after EOF must not re-fire
	if fired != 1 {
		t.Fatalf("done fired %d times, want 1", fired)
	}
	if in, out := parseUsageTail(gotTail); in != 5 || out != 7 {
		t.Errorf("tail parse: got in=%d out=%d", in, out)
	}
}

func TestUsageMeter_RecordAndPersistRoundtrip(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.json")
	m := newUsageMeter(cfgPath)
	m.record("glm-5.2", 1000, 50)
	m.record("glm-5.2", 200, 10)
	m.record("dsv4-nothink", 0, 0) // request with no usage frame still counts

	// Force a flush (bypass the interval gate).
	m.lastFlush = time.Time{}
	m.maybeFlush()
	if _, err := os.Stat(filepath.Join(dir, "usage.json")); err != nil {
		t.Fatalf("usage.json not persisted: %v", err)
	}

	reloaded := newUsageMeter(cfgPath)
	rows := reloaded.snapshot(usageWindowKey(time.Now()))
	if len(rows) != 2 {
		t.Fatalf("reloaded rows = %d, want 2", len(rows))
	}
	if rows[1].Model != "glm-5.2" || rows[1].Requests != 2 || rows[1].InputTokens != 1200 || rows[1].OutputTokens != 60 {
		t.Errorf("glm row wrong: %+v", rows[1])
	}
	if rows[0].Model != "dsv4-nothink" || rows[0].Requests != 1 {
		t.Errorf("dsv4 row wrong: %+v", rows[0])
	}
}

func TestUsageHandler_PricingAndBudget(t *testing.T) {
	rt := quietRouter(config{
		Token:            "",
		MonthlyBudgetUSD: 100,
		Models: []modelEntry{{
			Name: "glm-5.2", URL: "http://127.0.0.1:9", UpstreamModel: "glm-5.2",
			Pricing: &modelPricing{InputPerMTokUSD: 1.0, OutputPerMTokUSD: 4.0},
		}},
	})
	rt.usage.record("glm-5.2", 2_000_000, 500_000) // $2 + $2 = $4

	req := httptest.NewRequest("GET", "/v1/usage", nil)
	rec := httptest.NewRecorder()
	rt.usageHandler(rec, req)

	var resp struct {
		Window     string  `json:"window"`
		EstCostUSD float64 `json:"estCostUsd"`
		Models     []struct {
			Model      string  `json:"model"`
			Requests   int64   `json:"requests"`
			EstCostUSD float64 `json:"estCostUsd"`
		} `json:"models"`
		Budget struct {
			MonthlyUSD  float64 `json:"monthlyUsd"`
			UsedPercent float64 `json:"usedPercent"`
		} `json:"budget"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad json: %v — %s", err, rec.Body.String())
	}
	if resp.Window != usageWindowKey(time.Now()) {
		t.Errorf("window = %q", resp.Window)
	}
	if len(resp.Models) != 1 || resp.Models[0].Requests != 1 {
		t.Fatalf("models: %+v", resp.Models)
	}
	if resp.Models[0].EstCostUSD < 3.99 || resp.Models[0].EstCostUSD > 4.01 {
		t.Errorf("estCost = %v, want ~4", resp.Models[0].EstCostUSD)
	}
	if resp.Budget.MonthlyUSD != 100 || resp.Budget.UsedPercent < 0.039 || resp.Budget.UsedPercent > 0.041 {
		t.Errorf("budget: %+v", resp.Budget)
	}
}

func TestBalancedJSONObject_StringEscapes(t *testing.T) {
	obj := balancedJSONObject([]byte(`{"a":"brace } in \" string","b":{"c":1}} trailing`))
	if obj == nil || !bytes.HasSuffix(obj, []byte(`}}`)) {
		t.Fatalf("balanced parse failed: %s", obj)
	}
}
