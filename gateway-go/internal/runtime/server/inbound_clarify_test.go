package server

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
	"github.com/choiceoh/deneb/gateway-go/internal/platform/telegram"
)

func clarifyMsg(body string) *telegram.CallbackQuery {
	return &telegram.CallbackQuery{
		ID:   "q-1",
		Data: "clarify:1",
		Message: &telegram.Message{
			MessageID: 10,
			Chat:      telegram.Chat{ID: 42, Type: "private"},
			Text:      body,
		},
	}
}

func TestFormatClarifyCallbackNilReturnsEmpty(t *testing.T) {
	if got := formatClarifyCallback(nil); got != "" {
		t.Fatalf("got %q, want empty for nil callback", got)
	}
}

func TestFormatClarifyCallbackNonClarifyReturnsEmpty(t *testing.T) {
	cb := &telegram.CallbackQuery{
		Data: "commit:session-abc",
		Message: &telegram.Message{
			MessageID: 1,
			Chat:      telegram.Chat{ID: 1, Type: "private"},
		},
	}
	if got := formatClarifyCallback(cb); got != "" {
		t.Fatalf("got %q, want empty for non-clarify callback", got)
	}
}

func TestFormatClarifyCallbackRecoversOptionText(t *testing.T) {
	body := strings.Join([]string{
		"어느 파일을 고칠까요?",
		"",
		"1. alpha.yaml",
		"2. beta.yaml",
		"3. gamma.yaml",
	}, "\n")
	got := formatClarifyCallback(clarifyMsg(body))
	want := `[유저 응답 (버튼): 선택지 2번 — "beta.yaml"]`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatClarifyCallbackFallsBackWhenLineMissing(t *testing.T) {
	// idx=1 means we look for "2. ..." — omit that line to force fallback.
	body := "1. only-one.yaml\n"
	got := formatClarifyCallback(clarifyMsg(body))
	want := "[유저 응답 (버튼): 선택지 2번]"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestFormatClarifyCallbackRejectsNonNumericPayload(t *testing.T) {
	cb := clarifyMsg("1. a\n2. b\n")
	cb.Data = "clarify:nope"
	if got := formatClarifyCallback(cb); got != "" {
		t.Fatalf("got %q, want empty for non-numeric index", got)
	}
}

func TestExtractClarifyOptionTextHandlesIndentedLines(t *testing.T) {
	body := "  1. first\n\t2. second\n"
	if got := extractClarifyOptionText(body, 0); got != "first" {
		t.Fatalf("got %q, want first", got)
	}
	if got := extractClarifyOptionText(body, 1); got != "second" {
		t.Fatalf("got %q, want second", got)
	}
}

func TestExtractClarifyOptionTextNoMatchReturnsEmpty(t *testing.T) {
	if got := extractClarifyOptionText("no numbered lines here", 0); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestBuildClarifyResolvedTextStripsDirectiveAndAppendsMarker(t *testing.T) {
	body := strings.Join([]string{
		"어느 쪽?",
		"",
		"1. A",
		"2. B",
		`<!-- buttons: [["A|clarify:0"],["B|clarify:1"]] -->`,
	}, "\n")
	got := buildClarifyResolvedText(body, 0)
	if strings.Contains(got, "<!-- buttons:") {
		t.Fatalf("directive should be stripped, got %q", got)
	}
	if !strings.Contains(got, "(선택됨: 1번 — A)") {
		t.Fatalf("got %q, want marker with option text", got)
	}
	// Original lines must survive.
	if !strings.Contains(got, "1. A") || !strings.Contains(got, "2. B") {
		t.Fatalf("option lines missing: %q", got)
	}
}

func TestBuildClarifyResolvedTextFallsBackWhenOptionMissing(t *testing.T) {
	body := "only a question, no numbered options"
	got := buildClarifyResolvedText(body, 2)
	if !strings.Contains(got, "(선택됨: 3번)") {
		t.Fatalf("got %q, want index-only marker", got)
	}
	// Option-text form must not sneak in when extraction fails.
	if strings.Contains(got, "선택됨: 3번 —") {
		t.Fatalf("got %q, should not include option-text marker", got)
	}
}

func TestBuildClarifyResolvedTextEmptyBody(t *testing.T) {
	if got := buildClarifyResolvedText("", 0); got != "" {
		t.Fatalf("got %q, want empty for empty body", got)
	}
}

// TestClarifyToolOutputRoundTripsThroughParseReplyButtons guards the
// contract between tools.ToolClarify and server.parseReplyButtons: the
// directive emitted by the tool must parse into a keyboard whose callback
// data the server's own callback dispatcher can then interpret.
func TestClarifyToolOutputRoundTripsThroughParseReplyButtons(t *testing.T) {
	tool := tools.ToolClarify()
	var sent string
	ctx := toolctx.WithDeliveryContext(context.Background(), &toolctx.DeliveryContext{
		Channel: "telegram",
		To:      "123",
	})
	ctx = toolctx.WithReplyFunc(ctx, func(_ context.Context, _ *toolctx.DeliveryContext, text string) error {
		sent = text
		return nil
	})

	payload, _ := json.Marshal(map[string]any{
		"question": "어느 파일?",
		"options":  []string{"alpha.yaml", "beta.yaml"},
	})
	if _, err := tool(ctx, payload); err != nil {
		t.Fatalf("clarify tool error: %v", err)
	}

	cleanText, kb := parseReplyButtons(sent)
	if kb == nil {
		t.Fatalf("parseReplyButtons returned nil keyboard for %q", sent)
	}
	if strings.Contains(cleanText, "<!-- buttons:") {
		t.Fatalf("directive not stripped: %q", cleanText)
	}
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("got %d rows, want 2", len(kb.InlineKeyboard))
	}
	for i, row := range kb.InlineKeyboard {
		if len(row) != 1 {
			t.Fatalf("row %d: got %d buttons, want 1", i, len(row))
		}
		btn := row[0]
		wantCB := "clarify:" + string(rune('0'+i))
		if btn.CallbackData != wantCB {
			t.Fatalf("row %d: got callback %q, want %q", i, btn.CallbackData, wantCB)
		}
	}
	// Labels must match the original options so users can see them on buttons.
	if kb.InlineKeyboard[0][0].Text != "alpha.yaml" {
		t.Fatalf("row 0 label: got %q, want alpha.yaml", kb.InlineKeyboard[0][0].Text)
	}
	if kb.InlineKeyboard[1][0].Text != "beta.yaml" {
		t.Fatalf("row 1 label: got %q, want beta.yaml", kb.InlineKeyboard[1][0].Text)
	}

	// Simulate the subsequent click on option 1 and verify we produce a
	// clean user-facing message the agent can consume.
	cb := &telegram.CallbackQuery{
		Data: kb.InlineKeyboard[1][0].CallbackData,
		Message: &telegram.Message{
			MessageID: 7,
			Chat:      telegram.Chat{ID: 9, Type: "private"},
			Text:      cleanText,
		},
	}
	formatted := formatClarifyCallback(cb)
	if !strings.Contains(formatted, `beta.yaml`) || !strings.Contains(formatted, "선택지 2번") {
		t.Fatalf("round-trip click produced %q, want choice + option text", formatted)
	}
}
