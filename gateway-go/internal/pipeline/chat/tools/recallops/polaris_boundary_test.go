package recallops

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/polaris"
)

func TestBoundaryPolarisTimeRangeMatrix(t *testing.T) {
	location := time.FixedZone("KST", 9*60*60)
	now := time.Date(2026, 7, 11, 15, 30, 0, 0, location)
	nodes := []polaris.SummaryNode{
		{ID: 1, CreatedAt: now.AddDate(0, 0, -8).UnixMilli()},
		{ID: 2, CreatedAt: now.AddDate(0, 0, -7).UnixMilli()},
		{ID: 3, CreatedAt: time.Date(2026, 7, 11, 0, 0, 0, 0, location).UnixMilli()},
		{ID: 4, CreatedAt: now.UnixMilli()},
		{ID: 5, CreatedAt: now.Add(time.Hour).UnixMilli()},
	}
	tests := []struct {
		name      string
		rangeName string
		want      []int64
	}{
		{name: "empty means all", rangeName: "", want: []int64{1, 2, 3, 4, 5}},
		{name: "all", rangeName: "all", want: []int64{1, 2, 3, 4, 5}},
		{name: "unknown means all", rangeName: "month", want: []int64{1, 2, 3, 4, 5}},
		{name: "today inclusive midnight", rangeName: "today", want: []int64{3, 4, 5}},
		{name: "week inclusive cutoff", rangeName: "this_week", want: []int64{2, 3, 4, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterByTimeRange(nodes, tt.rangeName, now)
			ids := make([]int64, len(got))
			for i := range got {
				ids[i] = got[i].ID
			}
			if !reflect.DeepEqual(ids, tt.want) {
				t.Fatalf("IDs = %v, want %v", ids, tt.want)
			}
		})
	}
}

func TestBoundarySerializeExpandMessagesBudgetMatrix(t *testing.T) {
	msgs := []toolport.ChatMessage{
		toolport.NewTextChatMessage("user", "hello", 1),
		toolport.NewTextChatMessage("assistant", "안녕하세요", 2),
		toolport.NewTextChatMessage("tool", "result", 3),
	}
	full := "[user]: hello\n\n[assistant]: 안녕하세요\n\n[tool]: result\n\n"
	// wantOmitted is the count of messages that did NOT fit — the remaining
	// ones, not the total. Reporting the total tells the model nothing about
	// what it is missing, and the delegate answers from this excerpt.
	tests := []struct {
		name        string
		max         int
		want        string
		wantOmitted int
	}{
		{name: "large budget", max: 1000, want: full, wantOmitted: 0},
		{name: "exact full budget", max: len(full), want: full, wantOmitted: 0},
		{name: "first only", max: len("[user]: hello\n\n"), want: "[user]: hello\n\n... (나머지 2건 생략)\n", wantOmitted: 2},
		{name: "zero budget", max: 0, want: "... (나머지 3건 생략)\n", wantOmitted: 3},
		{name: "negative budget", max: -1, want: "... (나머지 3건 생략)\n", wantOmitted: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, omitted := serializeExpandMessages(msgs, tt.max)
			if got != tt.want {
				t.Fatalf("serializeExpandMessages(max=%d) = %q, want %q", tt.max, got, tt.want)
			}
			if omitted != tt.wantOmitted {
				t.Fatalf("serializeExpandMessages(max=%d) omitted = %d, want %d", tt.max, omitted, tt.wantOmitted)
			}
		})
	}
	if got, omitted := serializeExpandMessages(nil, 100); got != "" || omitted != 0 {
		t.Fatalf("nil messages = %q, omitted %d", got, omitted)
	}
}

func TestBoundaryToolPolarisRejectsMalformedAndUnknownActions(t *testing.T) {
	tool := ToolPolaris(nil, nil)
	if _, err := tool(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed input accepted")
	}
	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"unknown"}`, `{"action":"LOOKUP"}`} {
		got, err := tool(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("input %s: %v", input, err)
		}
		if !strings.Contains(got, "search, describe, expand") {
			t.Fatalf("input %s output = %q", input, got)
		}
	}
	got, err := tool(context.Background(), json.RawMessage(`{"action":"SEARCH"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "query") {
		t.Fatalf("SEARCH alias did not reach search: %q", got)
	}
}

// Expand shows the conversation AROUND a search hit, where a tool call is
// content — "what happened here" is the question. Rendering it through
// TextContent ("what was said") produced an empty [assistant]: row that cost a
// line and told the model nothing; before the raw-JSON hatch was narrowed it
// pasted the whole block array, signature included.
func TestSerializeExpandRendersToolCallsWithoutSignatures(t *testing.T) {
	msgs := []toolport.ChatMessage{
		toolport.NewTextChatMessage("user", "위키 확인해줘", 1),
		{Role: "assistant", Content: json.RawMessage(
			`[{"type":"thinking","thinking":"위키를 보자","signature":"ZZZZSIGNATUREZZZZ"},` +
				`{"type":"tool_use","name":"wiki","input":{"action":"search"}}]`,
		)},
		toolport.NewTextChatMessage("assistant", "3건 찾았어요", 3),
	}

	got, omitted := serializeExpandMessages(msgs, 10_000)
	if omitted != 0 {
		t.Fatalf("omitted = %d, want 0", omitted)
	}
	if !strings.Contains(got, "[도구 wiki]") {
		t.Errorf("tool call missing from the expanded conversation:\n%s", got)
	}
	if strings.Contains(got, "ZZZZSIGNATURE") {
		t.Errorf("thinking signature leaked into the expansion:\n%s", got)
	}
	if strings.Contains(got, "[assistant]: \n") {
		t.Errorf("an empty role row was rendered:\n%s", got)
	}
	if !strings.Contains(got, "3건 찾았어요") {
		t.Errorf("plain assistant text lost:\n%s", got)
	}
}

// A message that yields nothing at all is skipped, not rendered as a blank row.
func TestSerializeExpandSkipsEmptyMessages(t *testing.T) {
	msgs := []toolport.ChatMessage{
		{Role: "assistant"},
		toolport.NewTextChatMessage("assistant", "본문", 2),
	}
	got, _ := serializeExpandMessages(msgs, 10_000)
	if strings.Contains(got, "[assistant]: \n") {
		t.Errorf("empty message rendered as a blank row:\n%s", got)
	}
	if !strings.Contains(got, "본문") {
		t.Errorf("real message lost:\n%s", got)
	}
}
