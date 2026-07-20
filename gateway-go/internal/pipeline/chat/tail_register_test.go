package chat

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// resetTailRegister isolates each test from the process-global register.
func resetTailRegister(t *testing.T, dir string) {
	t.Helper()
	messageTails.mu.Lock()
	messageTails.sessions = nil
	messageTails.dir = ""
	messageTails.mu.Unlock()
	ConfigureTailRegister(dir, nil)
	t.Cleanup(func() {
		messageTails.mu.Lock()
		messageTails.sessions = nil
		messageTails.dir = ""
		messageTails.mu.Unlock()
	})
}

// TestTailRegisterRoundTripByteIdentical is the load-bearing contract: the
// re-attached history message must be byte-identical to the wire form the
// recording run sent, otherwise content-prefix provider caches (kimi) miss.
func TestTailRegisterRoundTripByteIdentical(t *testing.T) {
	resetTailRegister(t, "")
	const session = "client:main"
	clean := llm.NewTextMessage("user", "[2026-07-20T21:00:00+09:00] 안녕")
	adds := []string{"<recall-context>증거</recall-context>", "[전달 정책 — 이번 턴]\n- 정책"}

	// The recording run's wire form.
	injected, ok, cleanContent := injectTailAdditionsTracked([]llm.Message{clean}, adds)
	if !ok || cleanContent == nil {
		t.Fatal("injection failed")
	}
	recordPersistedTail(session, cleanContent, adds)

	// The next run reloads the clean message from the transcript.
	attached := attachPersistedTails(session, []llm.Message{clean})
	if got, want := string(attached[0].Content.Bytes()), string(injected[0].Content.Bytes()); got != want {
		t.Fatalf("re-attached bytes diverge from recorded wire form:\n got %q\nwant %q", got, want)
	}
	// The input message is never mutated (it aliases transcript state).
	if string(clean.Content.Bytes()) != string(llm.NewTextMessage("user", "[2026-07-20T21:00:00+09:00] 안녕").Content.Bytes()) {
		t.Fatal("attach mutated the input message")
	}
}

func TestTailRegisterRoundTripBlockContent(t *testing.T) {
	resetTailRegister(t, "")
	const session = "client:main"
	clean := llm.NewBlockMessage("user", []llm.ContentBlock{
		{Type: "text", Text: "질문"},
		{Type: "image", Source: &llm.ImageSource{Type: "base64", MediaType: "image/png", Data: "aGk="}},
	})
	adds := []string{"recall"}

	injected, ok, cleanContent := injectTailAdditionsTracked([]llm.Message{clean}, adds)
	if !ok {
		t.Fatal("injection failed")
	}
	recordPersistedTail(session, cleanContent, adds)

	attached := attachPersistedTails(session, []llm.Message{clean})
	if got, want := string(attached[0].Content.Bytes()), string(injected[0].Content.Bytes()); got != want {
		t.Fatalf("block-content round trip diverged:\n got %q\nwant %q", got, want)
	}
}

func TestTailRegisterUnknownMessagesPassThrough(t *testing.T) {
	resetTailRegister(t, "")
	recordPersistedTail("client:main", []byte(`"other"`), []string{"tail"})

	msgs := []llm.Message{llm.NewTextMessage("user", "no tail recorded")}
	if attached := attachPersistedTails("client:main", msgs); &attached[0] != &msgs[0] && string(attached[0].Content.Bytes()) != string(msgs[0].Content.Bytes()) {
		t.Fatal("message without a recorded tail must pass through unchanged")
	}
	// Unknown session: no-op.
	if attached := attachPersistedTails("client:other", msgs); string(attached[0].Content.Bytes()) != string(msgs[0].Content.Bytes()) {
		t.Fatal("unknown session must pass through unchanged")
	}
}

func TestTailRegisterEvictsOldestBeyondCap(t *testing.T) {
	resetTailRegister(t, "")
	const session = "client:main"
	for i := 0; i < tailRegisterMaxPerSession+10; i++ {
		recordPersistedTail(session, []byte{byte(i), byte(i >> 8)}, []string{"t"})
	}
	messageTails.mu.Lock()
	n := len(messageTails.sessions[session])
	first := messageTails.sessions[session][0]
	messageTails.mu.Unlock()
	if n != tailRegisterMaxPerSession {
		t.Fatalf("entries = %d, want capped at %d", n, tailRegisterMaxPerSession)
	}
	if first.Hash == tailContentHash([]byte{0, 0}) {
		t.Fatal("oldest entry survived eviction")
	}
}

func TestTailRegisterPersistsRestorableSessionsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	resetTailRegister(t, dir)
	recordPersistedTail("client:main", []byte(`"persisted"`), []string{"tail-main"})
	recordPersistedTail("system:cron", []byte(`"volatile"`), []string{"tail-cron"})

	if _, err := os.Stat(filepath.Join(dir, tailRegisterFileName)); err != nil {
		t.Fatalf("register file not written: %v", err)
	}

	// Simulate restart: fresh in-memory state, same dir.
	messageTails.mu.Lock()
	messageTails.sessions = nil
	messageTails.dir = ""
	messageTails.mu.Unlock()
	ConfigureTailRegister(dir, nil)

	msgs := []llm.Message{{Role: "user", Content: llm.FlexibleFromRaw([]byte(`"persisted"`))}}
	attached := attachPersistedTails("client:main", msgs)
	if string(attached[0].Content.Bytes()) == `"persisted"` {
		t.Fatal("client:main tail did not survive restart")
	}
	// Non-restorable sessions are in-memory only.
	cronMsgs := []llm.Message{{Role: "user", Content: llm.FlexibleFromRaw([]byte(`"volatile"`))}}
	if got := attachPersistedTails("system:cron", cronMsgs); string(got[0].Content.Bytes()) != `"volatile"` {
		t.Fatal("non-restorable session unexpectedly persisted across restart")
	}
}

func TestTailRegisterClearDropsSession(t *testing.T) {
	resetTailRegister(t, "")
	recordPersistedTail("client:main", []byte(`"m"`), []string{"tail"})
	clearPersistedTails("client:main")

	msgs := []llm.Message{{Role: "user", Content: llm.FlexibleFromRaw([]byte(`"m"`))}}
	if got := attachPersistedTails("client:main", msgs); string(got[0].Content.Bytes()) != `"m"` {
		t.Fatal("cleared session still attaches tails")
	}
}
