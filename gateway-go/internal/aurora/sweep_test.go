package aurora

import (
	"encoding/json"
	"log/slog"
	"testing"
)

// cmdType extracts the "type" field from a RawMessage for use in handleCommand tests.
func cmdType(raw json.RawMessage) string {
	var h struct {
		Type string `json:"type"`
	}
	json.Unmarshal(raw, &h)
	return h.Type
}

func TestEvaluateCompaction_PureGo(t *testing.T) {
	cfg := DefaultSweepConfig()

	// Should not compact when under threshold.
	shouldCompact, _, err := EvaluateCompaction(cfg, 50_000, 50_000, 100_000)
	if err != nil {
		t.Fatalf("EvaluateCompaction: %v", err)
	}
	if shouldCompact {
		t.Error("expected no compaction when under threshold")
	}

	// Should compact when over threshold (0.80 × 100K = 80K boundary).
	shouldCompact, reason, err := EvaluateCompaction(cfg, 81_000, 81_000, 100_000)
	if err != nil {
		t.Fatalf("EvaluateCompaction: %v", err)
	}
	if !shouldCompact {
		t.Error("expected compaction when over threshold")
	}
	if reason == "" {
		t.Error("expected non-empty reason")
	}
}

func TestSweepCommandHandlers(t *testing.T) {
	s := tempStore(t)

	// Populate some data.
	s.SyncMessage(1, "user", "hello world", 10)
	s.SyncMessage(1, "assistant", "goodbye", 8)

	// Test fetchTokenCount handler.
	cmd := json.RawMessage(`{"type":"fetchTokenCount","conversationId":1}`)
	resp, err := handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("fetchTokenCount: %v", err)
	}
	respJSON, _ := json.Marshal(resp)
	var tokenResp struct {
		Type  string `json:"type"`
		Count uint64 `json:"count"`
	}
	json.Unmarshal(respJSON, &tokenResp)
	if tokenResp.Count != 18 {
		t.Errorf("expected 18 tokens, got %d", tokenResp.Count)
	}

	// Test fetchContextItems handler.
	cmd = json.RawMessage(`{"type":"fetchContextItems","conversationId":1}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("fetchContextItems: %v", err)
	}
	respJSON, _ = json.Marshal(resp)
	var itemsResp struct {
		Type  string        `json:"type"`
		Items []ContextItem `json:"items"`
	}
	json.Unmarshal(respJSON, &itemsResp)
	if len(itemsResp.Items) != 2 {
		t.Errorf("expected 2 context items, got %d", len(itemsResp.Items))
	}

	// Test fetchMessages handler.
	cmd = json.RawMessage(`{"type":"fetchMessages","messageIds":[0,1]}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("fetchMessages: %v", err)
	}
	respJSON, _ = json.Marshal(resp)
	var msgsResp struct {
		Messages map[string]MessageRecord `json:"messages"`
	}
	json.Unmarshal(respJSON, &msgsResp)
	if len(msgsResp.Messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgsResp.Messages))
	}

	// Test fetchSummaries handler (empty).
	cmd = json.RawMessage(`{"type":"fetchSummaries","summaryIds":["nonexistent"]}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("fetchSummaries: %v", err)
	}
	respJSON, _ = json.Marshal(resp)
	var sumsResp struct {
		Summaries map[string]SummaryRecord `json:"summaries"`
	}
	json.Unmarshal(respJSON, &sumsResp)
	if len(sumsResp.Summaries) != 0 {
		t.Errorf("expected 0 summaries, got %d", len(sumsResp.Summaries))
	}

	// Test fetchDistinctDepths handler (empty).
	cmd = json.RawMessage(`{"type":"fetchDistinctDepths","conversationId":1,"maxOrdinal":999}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("fetchDistinctDepths: %v", err)
	}

	// Test persistEvent handler.
	cmd = json.RawMessage(`{"type":"persistEvent","input":{"conversationId":1,"pass":"leaf","level":"normal","tokensBefore":100,"tokensAfter":50,"createdSummaryId":"sum_001"}}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("persistEvent: %v", err)
	}
	respJSON, _ = json.Marshal(resp)
	if string(respJSON) != `{"type":"persistOk"}` {
		t.Errorf("expected persistOk, got %s", string(respJSON))
	}

	// Test summarize handler with mock summarizer.
	mockSummarizer := func(text string, aggressive bool, opts *SummarizeOptions) (string, error) {
		return "mocked summary of: " + text[:10], nil
	}
	cmd = json.RawMessage(`{"type":"summarize","text":"This is a long conversation that needs summarizing","aggressive":false}`)
	resp, err = handleCommand(s, cmdType(cmd), cmd, mockSummarizer, nil, slog.Default())
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	respJSON, _ = json.Marshal(resp)
	var sumResp struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	json.Unmarshal(respJSON, &sumResp)
	if sumResp.Type != "summaryText" {
		t.Errorf("expected summaryText type, got %s", sumResp.Type)
	}
	if sumResp.Text == "" {
		t.Error("expected non-empty summary text")
	}
}

func TestPersistLeafSummaryHandler(t *testing.T) {
	s := tempStore(t)

	s.SyncMessage(1, "user", "msg1", 10)
	s.SyncMessage(1, "user", "msg2", 10)

	cmd := json.RawMessage(`{
		"type": "persistLeafSummary",
		"input": {
			"summaryId": "sum_leaf_test",
			"conversationId": 1,
			"content": "leaf summary",
			"tokenCount": 5,
			"fileIds": [],
			"sourceMessageTokenCount": 20,
			"messageIds": [0, 1],
			"startOrdinal": 0,
			"endOrdinal": 1
		}
	}`)

	resp, err := handleCommand(s, cmdType(cmd), cmd, nil, nil, slog.Default())
	if err != nil {
		t.Fatalf("persistLeafSummary: %v", err)
	}

	respJSON, _ := json.Marshal(resp)
	var result struct {
		Type string `json:"type"`
	}
	json.Unmarshal(respJSON, &result)
	if result.Type != "persistOk" {
		t.Errorf("expected persistOk, got %s", result.Type)
	}

	// Verify the summary exists.
	sums, _ := s.FetchSummaries([]string{"sum_leaf_test"})
	if len(sums) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(sums))
	}
}

func TestDeterministicFallback(t *testing.T) {
	short := "short text"
	result := deterministicFallback(short)
	if result != short {
		t.Errorf("expected short text unchanged, got %q", result)
	}

	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'a'
	}
	result = deterministicFallback(string(long))
	if len(result) > len(long) {
		t.Error("fallback should not be longer than input")
	}
	if len(result) < 100 {
		t.Error("fallback should preserve reasonable content")
	}
}
