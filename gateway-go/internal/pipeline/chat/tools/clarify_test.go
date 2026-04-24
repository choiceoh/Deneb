package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// fakeReply captures the last reply sent by the clarify tool so tests can
// inspect both routing and payload shape without touching a real channel.
type fakeReply struct {
	delivery *toolctx.DeliveryContext
	text     string
	err      error
	called   int
}

func newFakeReply() *fakeReply {
	return &fakeReply{}
}

func (f *fakeReply) replyFn(_ context.Context, d *toolctx.DeliveryContext, text string) error {
	f.called++
	f.delivery = d
	f.text = text
	return f.err
}

// ctxWithReply wires the delivery + replyFn the clarify tool expects.
// Channel defaults to "telegram" because that is the only supported channel
// (all other channels are rejected up front by the tool).
func ctxWithReply(t *testing.T, channel string, f *fakeReply) context.Context {
	t.Helper()
	ctx := toolctx.WithDeliveryContext(context.Background(), &toolctx.DeliveryContext{
		Channel: channel,
		To:      "123",
	})
	return toolctx.WithReplyFunc(ctx, f.replyFn)
}

func TestClarifyRejectsFewerThanTwoOptions(t *testing.T) {
	tool := ToolClarify()
	_, err := tool(context.Background(), []byte(`{"question":"어느쪽?","options":["A"]}`))
	if err == nil {
		t.Fatal("expected error for single option")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Fatalf("got %q, want minimum-options error", err)
	}
}

func TestClarifyRejectsMoreThanFiveOptions(t *testing.T) {
	tool := ToolClarify()
	input := []byte(`{"question":"q","options":["a","b","c","d","e","f"]}`)
	_, err := tool(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for six options")
	}
	if !strings.Contains(err.Error(), "at most 5") {
		t.Fatalf("got %q, want maximum-options error", err)
	}
}

func TestClarifyRejectsEmptyQuestion(t *testing.T) {
	tool := ToolClarify()
	_, err := tool(context.Background(), []byte(`{"question":"   ","options":["A","B"]}`))
	if err == nil {
		t.Fatal("expected error for empty question")
	}
	if !strings.Contains(err.Error(), "question is required") {
		t.Fatalf("got %q, want question-required error", err)
	}
}

func TestClarifyRejectsOversizedOption(t *testing.T) {
	tool := ToolClarify()
	// Build a 41-char option (just over the 40-char cap).
	long := strings.Repeat("가", 41)
	payload, _ := json.Marshal(map[string]any{
		"question": "q",
		"options":  []string{"OK", long},
	})
	_, err := tool(context.Background(), payload)
	if err == nil {
		t.Fatal("expected error for oversized option")
	}
	if !strings.Contains(err.Error(), "exceeds 40 characters") {
		t.Fatalf("got %q, want exceeds-40 error", err)
	}
}

func TestClarifyAcceptsKoreanAtRuneBoundary(t *testing.T) {
	tool := ToolClarify()
	// Exactly 40 Korean runes (each 3 bytes in UTF-8; would fail a byte
	// check but must pass our rune-count check).
	fit := strings.Repeat("가", 40)
	payload, _ := json.Marshal(map[string]any{
		"question": "q",
		"options":  []string{"OK", fit},
	})
	fr := newFakeReply()
	ctx := ctxWithReply(t, "telegram", fr)
	if _, err := tool(ctx, payload); err != nil {
		t.Fatalf("40 Korean chars should be accepted, got %v", err)
	}
}

func TestClarifyRejectsNonTelegramChannel(t *testing.T) {
	tool := ToolClarify()
	fr := newFakeReply()
	ctx := ctxWithReply(t, "whatsapp", fr)
	_, err := tool(ctx, []byte(`{"question":"q","options":["A","B"]}`))
	if err == nil {
		t.Fatal("expected error on non-telegram channel")
	}
	if !strings.Contains(err.Error(), "only supported on Telegram") {
		t.Fatalf("got %q, want telegram-only error", err)
	}
	if fr.called != 0 {
		t.Fatal("replyFn must not be invoked when channel is unsupported")
	}
}

func TestClarifyRejectsMissingReplyFunc(t *testing.T) {
	tool := ToolClarify()
	_, err := tool(context.Background(), []byte(`{"question":"q","options":["A","B"]}`))
	if err == nil {
		t.Fatal("expected error when reply function is unavailable")
	}
	if !strings.Contains(err.Error(), "channel not connected") {
		t.Fatalf("got %q, want channel-not-connected error", err)
	}
}

func TestClarifyRejectsMissingDelivery(t *testing.T) {
	tool := ToolClarify()
	ctx := toolctx.WithReplyFunc(context.Background(), func(_ context.Context, _ *toolctx.DeliveryContext, _ string) error {
		t.Fatal("replyFn must not be called without delivery")
		return nil
	})
	_, err := tool(ctx, []byte(`{"question":"q","options":["A","B"]}`))
	if err == nil {
		t.Fatal("expected error without delivery target")
	}
	if !strings.Contains(err.Error(), "no active delivery target") {
		t.Fatalf("got %q, want no-delivery error", err)
	}
}

func TestClarifyStripsEmptyOptionsThenValidates(t *testing.T) {
	// Two real options + one whitespace-only → should still succeed
	// because the whitespace option is trimmed out BEFORE the count check.
	tool := ToolClarify()
	fr := newFakeReply()
	ctx := ctxWithReply(t, "telegram", fr)
	payload := []byte(`{"question":"q","options":["A","   ","B"]}`)
	out, err := tool(ctx, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "선택지 2개") {
		t.Fatalf("got %q, want status mentioning 2 options", out)
	}
}

func TestClarifySuccessBuildsButtonDirective(t *testing.T) {
	tool := ToolClarify()
	fr := newFakeReply()
	ctx := ctxWithReply(t, "telegram", fr)
	payload := []byte(`{"question":"어느 파일을 고칠까요?","options":["alpha.yaml","beta.yaml"]}`)

	out, err := tool(ctx, payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fr.called != 1 {
		t.Fatalf("replyFn called %d times, want 1", fr.called)
	}
	if fr.delivery == nil || fr.delivery.Channel != "telegram" {
		t.Fatalf("delivery routed wrong: %#v", fr.delivery)
	}
	// Question should appear in the rendered message.
	if !strings.Contains(fr.text, "어느 파일을 고칠까요?") {
		t.Fatalf("question missing from reply: %q", fr.text)
	}
	// Numbered options present in body.
	if !strings.Contains(fr.text, "1. alpha.yaml") || !strings.Contains(fr.text, "2. beta.yaml") {
		t.Fatalf("numbered options missing: %q", fr.text)
	}
	// Button directive present with clarify callback prefix.
	if !strings.Contains(fr.text, "<!-- buttons:") {
		t.Fatalf("button directive missing: %q", fr.text)
	}
	if !strings.Contains(fr.text, "clarify:0") || !strings.Contains(fr.text, "clarify:1") {
		t.Fatalf("callback indices missing: %q", fr.text)
	}
	// Agent-facing status mentions turn-ending semantics.
	if !strings.Contains(out, "턴을 종료") {
		t.Fatalf("status should instruct the agent to end its turn, got %q", out)
	}
}

func TestClarifyButtonDirectiveParsesAsJSON(t *testing.T) {
	// Extract the JSON rows from the directive and confirm it round-trips,
	// matching the shape parseReplyButtons expects on the server side.
	tool := ToolClarify()
	fr := newFakeReply()
	ctx := ctxWithReply(t, "telegram", fr)
	payload := []byte(`{"question":"?","options":["a","b","c"]}`)
	if _, err := tool(ctx, payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	const prefix = "<!-- buttons: "
	i := strings.Index(fr.text, prefix)
	if i < 0 {
		t.Fatalf("no directive in %q", fr.text)
	}
	j := strings.Index(fr.text[i:], " -->")
	if j < 0 {
		t.Fatalf("unterminated directive in %q", fr.text)
	}
	jsonPart := fr.text[i+len(prefix) : i+j]
	var rows [][]string
	if err := json.Unmarshal([]byte(jsonPart), &rows); err != nil {
		t.Fatalf("directive JSON malformed: %v (%q)", err, jsonPart)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	for idx, row := range rows {
		if len(row) != 1 {
			t.Fatalf("row %d: got %d buttons, want 1", idx, len(row))
		}
		// Spec is "label|callback_data" — the callback must begin with
		// clarify: and match the expected index.
		sep := strings.Index(row[0], "|")
		if sep < 0 {
			t.Fatalf("row %d: no separator in %q", idx, row[0])
		}
		wantCB := "clarify:" + string(rune('0'+idx))
		if row[0][sep+1:] != wantCB {
			t.Fatalf("row %d: got callback %q, want %q", idx, row[0][sep+1:], wantCB)
		}
	}
}

func TestClarifyPropagatesReplyFailure(t *testing.T) {
	tool := ToolClarify()
	fr := newFakeReply()
	fr.err = errContextError("simulated send failure")
	ctx := ctxWithReply(t, "telegram", fr)
	_, err := tool(ctx, []byte(`{"question":"q","options":["A","B"]}`))
	if err == nil {
		t.Fatal("expected error when send fails")
	}
	if !strings.Contains(err.Error(), "simulated send failure") {
		t.Fatalf("got %q, want wrapped send failure", err)
	}
}

// errContextError is a tiny local error helper so the test does not introduce
// an extra imports block just for errors.New.
type errContextError string

func (e errContextError) Error() string { return string(e) }
