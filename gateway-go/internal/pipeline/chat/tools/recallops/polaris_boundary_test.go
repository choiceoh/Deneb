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
	tests := []struct {
		name string
		max  int
		want string
	}{
		{name: "large budget", max: 1000, want: full},
		{name: "exact full budget", max: len(full), want: full},
		{name: "first only", max: len("[user]: hello\n\n"), want: "[user]: hello\n\n... (나머지 3건 생략)\n"},
		{name: "zero budget", max: 0, want: "... (나머지 3건 생략)\n"},
		{name: "negative budget", max: -1, want: "... (나머지 3건 생략)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serializeExpandMessages(msgs, tt.max); got != tt.want {
				t.Fatalf("serializeExpandMessages(max=%d) = %q, want %q", tt.max, got, tt.want)
			}
		})
	}
	if got := serializeExpandMessages(nil, 100); got != "" {
		t.Fatalf("nil messages = %q", got)
	}
}

func TestBoundaryToolPolarisRejectsMalformedAndUnknownActions(t *testing.T) {
	tool := ToolPolaris(nil, nil)
	if _, err := tool(context.Background(), json.RawMessage(`{`)); err == nil {
		t.Fatal("malformed input accepted")
	}
	for _, input := range []string{`{}`, `{"action":""}`, `{"action":"unknown"}`, `{"action":"SEARCH"}`} {
		got, err := tool(context.Background(), json.RawMessage(input))
		if err != nil {
			t.Fatalf("input %s: %v", input, err)
		}
		if !strings.Contains(got, "search, describe, expand") {
			t.Fatalf("input %s output = %q", input, got)
		}
	}
}
