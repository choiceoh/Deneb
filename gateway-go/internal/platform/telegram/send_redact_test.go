package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

// captureBodies is a test helper that records the JSON body of every request
// the mock Telegram server receives, so assertions can inspect the outgoing
// payload for leaked secrets.
type captureBodies struct {
	mu     atomic.Pointer[[]map[string]any]
	counts atomic.Int32
}

// newCaptureServer returns a test client that records every inbound JSON
// body and replies with a generic success envelope. Callers inspect the
// captured bodies after the send call to assert on the outgoing text.
func newCaptureServer(t *testing.T) (*Client, *captureBodies, func()) {
	t.Helper()
	rec := &captureBodies{}
	initial := make([]map[string]any, 0)
	rec.mu.Store(&initial)

	c, srv := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)

		for {
			prev := rec.mu.Load()
			next := make([]map[string]any, len(*prev)+1)
			copy(next, *prev)
			next[len(*prev)] = req
			if rec.mu.CompareAndSwap(prev, &next) {
				break
			}
		}
		rec.counts.Add(1)

		// Extract the chat_id so the response echoes correctly.
		chatID := int64(0)
		if v, ok := req["chat_id"].(float64); ok {
			chatID = int64(v)
		}
		msgID := rec.counts.Load()
		resp := APIResponse{
			OK: true,
			Result: json.RawMessage(
				`{"message_id":` + itoaInt32(msgID) +
					`,"chat":{"id":` + itoaInt64(chatID) +
					`,"type":"private"},"text":"ok"}`),
		}
		_ = json.NewEncoder(w).Encode(resp)
	})
	return c, rec, func() { srv.Close() }
}

// itoaInt32 is a tiny stand-in for strconv.Itoa to avoid an extra import
// above the fixture section (keeps this file's import block minimal).
func itoaInt32(n int32) string {
	if n == 0 {
		return "0"
	}
	var buf [11]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func itoaInt64(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

func (cb *captureBodies) bodies() []map[string]any {
	return *cb.mu.Load()
}

// -- Synthetic fixtures: built at runtime so no literal secret ever lives in
// -- source (prevents repo-scanner false positives and avoids committing a
// -- real-looking key that might be confused for a leak).

func fakeOpenAIToken() string {
	// Shape: sk-proj-<24 random chars>; we use 'Z' for determinism.
	return "sk-proj-" + strings.Repeat("Z", 24)
}

func fakeJWT() string {
	// Three base64-ish segments separated by dots; length chosen so the JWT
	// pattern matches in redact (>=18 chars per segment keeps maskToken
	// on the happy path).
	seg := "eyJ" + strings.Repeat("A", 32)
	return seg + "." + strings.Repeat("B", 40) + "." + strings.Repeat("C", 40)
}

// ---- Test cases ----------------------------------------------------------

func TestSendText_RedactsSecretBeforeEgress(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	text := "here is my key: " + secret + " — please use it"

	_, err := SendText(context.Background(), c, 123, text, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	bodies := rec.bodies()
	if len(bodies) != 1 {
		t.Fatalf("expected 1 outbound call, got %d", len(bodies))
	}
	got, _ := bodies[0]["text"].(string)
	if strings.Contains(got, secret) {
		t.Errorf("outbound body leaked full token:\n  got: %q", got)
	}
	// Should still contain the masked prefix so the user gets a hint.
	if !strings.Contains(got, "sk-pro") {
		t.Errorf("expected masked prefix 'sk-pro' in outbound, got: %q", got)
	}
}

func TestSendText_DoesNotMutateCaller(t *testing.T) {
	c, _, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	original := "key=" + secret
	// Pass a fresh string copy so we can compare byte-for-byte afterwards.
	passed := string([]byte(original))

	_, err := SendText(context.Background(), c, 123, passed, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if passed != original {
		t.Errorf("SendText mutated caller's string:\n  before: %q\n   after: %q", original, passed)
	}
}

func TestSendText_KoreanPassesThrough(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	korean := "안녕하세요, 오선택님. 오늘의 작업은 무엇인가요?"
	_, err := SendText(context.Background(), c, 123, korean, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	if got != korean {
		t.Errorf("Korean text should pass through unchanged:\n  want: %q\n   got: %q", korean, got)
	}
}

func TestSendText_RedactsInsideCodeBlock(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	// Realistic agent-emitted HTML code block: the content ends with a newline
	// before </code></pre>. Redaction must mask the token while leaving the
	// surrounding fence structure intact.
	text := "Here's the command:\n<pre><code>export OPENAI_API_KEY=" + secret + "\n</code></pre>"

	_, err := SendText(context.Background(), c, 123, text, SendOptions{ParseMode: "HTML"})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	if strings.Contains(got, secret) {
		t.Errorf("code-block content leaked full token:\n  got: %q", got)
	}
	if !strings.Contains(got, "<pre><code>") || !strings.Contains(got, "</code></pre>") {
		t.Errorf("code fence not intact after redaction:\n  got: %q", got)
	}
}

func TestSendText_ChunkingRedactsEachChunk(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	// Build text that definitely exceeds a single chunk. Place a secret near
	// the start and another near the end so both chunks must redact independently.
	secretA := fakeOpenAIToken()
	secretB := "github_pat_" + strings.Repeat("Y", 60)

	chunkSize := TextChunkLimit
	middle := strings.Repeat("a", chunkSize) + "\n" + strings.Repeat("b", chunkSize)
	text := secretA + "\n" + middle + "\n" + secretB

	_, err := SendText(context.Background(), c, 123, text, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	bodies := rec.bodies()
	if len(bodies) < 2 {
		t.Fatalf("expected chunking to produce >=2 outbound calls, got %d", len(bodies))
	}
	for i, body := range bodies {
		got, _ := body["text"].(string)
		if strings.Contains(got, secretA) {
			t.Errorf("chunk %d leaked secretA", i)
		}
		if strings.Contains(got, secretB) {
			t.Errorf("chunk %d leaked secretB", i)
		}
	}
}

func TestSendText_RedactsKeyboardButtonLabels(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	keyboard := &InlineKeyboardMarkup{
		InlineKeyboard: [][]InlineKeyboardButton{
			{
				{
					Text:         "copy key: " + secret,
					CallbackData: "action:session-abc-" + secret, // routing key — keep as-is
				},
				{Text: "정상 버튼", CallbackData: "action:normal"},
			},
		},
	}

	_, err := SendText(context.Background(), c, 123, "hello", SendOptions{Keyboard: keyboard})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}

	bodies := rec.bodies()
	body := bodies[0]
	rmRaw, ok := body["reply_markup"]
	if !ok {
		t.Fatalf("reply_markup missing in outbound body")
	}
	rmJSON, _ := json.Marshal(rmRaw)
	rmStr := string(rmJSON)

	// Button label must be redacted.
	if strings.Contains(rmStr, secret) {
		// Allow a callback_data match; check the text specifically.
		var marshaled struct {
			InlineKeyboard [][]map[string]any `json:"inline_keyboard"`
		}
		_ = json.Unmarshal(rmJSON, &marshaled)
		for _, row := range marshaled.InlineKeyboard {
			for _, btn := range row {
				txt, _ := btn["text"].(string)
				if strings.Contains(txt, secret) {
					t.Errorf("button label leaked secret: %q", txt)
				}
			}
		}
	}

	// callback_data must NOT be mutated (routing key integrity).
	if !strings.Contains(rmStr, "action:session-abc-"+secret) {
		t.Errorf("callback_data was mutated — button routing broken.\n  body: %s", rmStr)
	}

	// Untouched button with normal label should survive intact.
	if !strings.Contains(rmStr, "정상 버튼") {
		t.Errorf("Korean button label lost in round-trip:\n  body: %s", rmStr)
	}
}

func TestEditMessageText_RedactsBody(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeJWT()
	text := "streaming update with " + secret

	_, err := EditMessageText(context.Background(), c, 123, 456, text, "HTML", nil)
	if err != nil {
		t.Fatalf("EditMessageText returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	if strings.Contains(got, secret) {
		t.Errorf("editMessageText leaked JWT:\n  got: %q", got)
	}
}

func TestEditMessageText_DoesNotMutateText(t *testing.T) {
	c, _, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	original := "draft with key " + secret
	passed := string([]byte(original))

	_, err := EditMessageText(context.Background(), c, 123, 456, passed, "HTML", nil)
	if err != nil {
		t.Fatalf("EditMessageText returned error: %v", err)
	}
	if passed != original {
		t.Errorf("EditMessageText mutated caller's string:\n  before: %q\n   after: %q", original, passed)
	}
}

func TestSendPhoto_RedactsCaption(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	caption := "see key " + secret

	_, err := SendPhoto(context.Background(), c, 123, "AgAD-stub", caption, SendOptions{})
	if err != nil {
		t.Fatalf("SendPhoto returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["caption"].(string)
	if strings.Contains(got, secret) {
		t.Errorf("caption leaked secret:\n  got: %q", got)
	}
	// Media file_id is structural — MUST not be touched.
	photo, _ := rec.bodies()[0]["photo"].(string)
	if photo != "AgAD-stub" {
		t.Errorf("photo file_id mutated:\n  got: %q", photo)
	}
}

func TestAnswerCallbackQuery_RedactsToast(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	secret := fakeOpenAIToken()
	toast := "error: " + secret

	if err := AnswerCallbackQuery(context.Background(), c, "query-id-123", toast); err != nil {
		t.Fatalf("AnswerCallbackQuery returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	if strings.Contains(got, secret) {
		t.Errorf("toast leaked secret:\n  got: %q", got)
	}
	// query_id is a routing key — MUST remain intact.
	qid, _ := rec.bodies()[0]["callback_query_id"].(string)
	if qid != "query-id-123" {
		t.Errorf("callback_query_id mutated: %q", qid)
	}
}

func TestSendText_MultipleSecretsAllRedacted(t *testing.T) {
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	s1 := fakeOpenAIToken()
	s2 := "ghp_" + strings.Repeat("X", 36)
	s3 := "AIzaSy" + strings.Repeat("Q", 33)
	text := "a=" + s1 + " b=" + s2 + " c=" + s3

	_, err := SendText(context.Background(), c, 123, text, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	for i, s := range []string{s1, s2, s3} {
		if strings.Contains(got, s) {
			t.Errorf("secret #%d leaked: %q", i+1, s)
		}
	}
}

func TestSendText_RedactionIdempotent(t *testing.T) {
	// Defense-in-depth layering: if an upstream caller has already passed the
	// text through redact.String once, the second pass inside SendText must
	// not double-mask or corrupt the output.
	c, rec, closeFn := newCaptureServer(t)
	defer closeFn()

	// Masked token (13 chars, "prefix6...suffix4") looks like this:
	pre := "sk-pro...ZZZZ"
	text := "already redacted: " + pre

	_, err := SendText(context.Background(), c, 123, text, SendOptions{})
	if err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	got, _ := rec.bodies()[0]["text"].(string)
	if got != text {
		t.Errorf("already-masked text was altered by second redaction:\n  want: %q\n   got: %q",
			text, got)
	}
}
