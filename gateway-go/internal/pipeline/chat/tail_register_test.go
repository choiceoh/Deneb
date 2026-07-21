package chat

import (
	"os"
	"path/filepath"
	"strings"
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
	injected, ok, cleanContent, targetIdx := injectTailAdditionsTracked([]llm.Message{clean}, adds)
	if !ok || cleanContent == nil {
		t.Fatal("injection failed")
	}
	recordPersistedTail(session, cleanContent, userMessageHashOrdinal([]llm.Message{clean}, targetIdx), adds)

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

	injected, ok, cleanContent, targetIdx := injectTailAdditionsTracked([]llm.Message{clean}, adds)
	if !ok {
		t.Fatal("injection failed")
	}
	recordPersistedTail(session, cleanContent, userMessageHashOrdinal([]llm.Message{clean}, targetIdx), adds)

	attached := attachPersistedTails(session, []llm.Message{clean})
	if got, want := string(attached[0].Content.Bytes()), string(injected[0].Content.Bytes()); got != want {
		t.Fatalf("block-content round trip diverged:\n got %q\nwant %q", got, want)
	}
}

func TestTailRegisterUnknownMessagesPassThrough(t *testing.T) {
	resetTailRegister(t, "")
	recordPersistedTail("client:main", []byte(`"other"`), 0, []string{"tail"})

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
		recordPersistedTail(session, []byte{byte(i), byte(i >> 8)}, 0, []string{"t"})
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
	recordPersistedTail("client:main", []byte(`"persisted"`), 0, []string{"tail-main"})
	recordPersistedTail("system:cron", []byte(`"volatile"`), 0, []string{"tail-cron"})

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
	recordPersistedTail("client:main", []byte(`"m"`), 0, []string{"tail"})
	clearPersistedTails("client:main")

	msgs := []llm.Message{{Role: "user", Content: llm.FlexibleFromRaw([]byte(`"m"`))}}
	if got := attachPersistedTails("client:main", msgs); string(got[0].Content.Bytes()) != `"m"` {
		t.Fatal("cleared session still attaches tails")
	}
}

// Duplicate user utterances must not share one recorded tail — the second
// "확인" carries different recall evidence than the first.
func TestTailRegisterDuplicateUserMessagesGetDistinctTails(t *testing.T) {
	resetTailRegister(t, "")
	const session = "client:main"
	first := llm.NewTextMessage("user", "확인")
	second := llm.NewTextMessage("user", "확인")
	history := []llm.Message{
		first,
		llm.NewTextMessage("assistant", "첫 답변"),
		second,
	}

	firstAdds := []string{"<recall-context>계약 A</recall-context>"}
	secondAdds := []string{"<recall-context>계약 B</recall-context>"}

	_, ok, clean1, idx1 := injectTailAdditionsTracked([]llm.Message{first}, firstAdds)
	if !ok {
		t.Fatal("first injection failed")
	}
	recordPersistedTail(session, clean1, userMessageHashOrdinal([]llm.Message{first}, idx1), firstAdds)

	_, ok, clean2, idx2 := injectTailAdditionsTracked(history, secondAdds)
	if !ok {
		t.Fatal("second injection failed")
	}
	recordPersistedTail(session, clean2, userMessageHashOrdinal(history, idx2), secondAdds)

	attached := attachPersistedTails(session, history)
	firstBody := string(attached[0].Content.Bytes())
	secondBody := string(attached[2].Content.Bytes())
	if !strings.Contains(firstBody, "계약 A") || strings.Contains(firstBody, "계약 B") {
		t.Fatalf("first duplicate got wrong tail: %q", firstBody)
	}
	if !strings.Contains(secondBody, "계약 B") || strings.Contains(secondBody, "계약 A") {
		t.Fatalf("second duplicate got wrong tail: %q", secondBody)
	}
}

// TestTailRegisterOrdinalUsesCleanTranscript pins run_exec ordering: historical
// tails are re-attached before the current turn's tail is recorded, so the
// hash-ordinal for record must come from the clean transcript — not from
// messages that already carry persisted suffixes.
func TestTailRegisterOrdinalUsesCleanTranscript(t *testing.T) {
	resetTailRegister(t, "")
	const session = "client:main"
	first := llm.NewTextMessage("user", "확인")
	second := llm.NewTextMessage("user", "확인")
	history := []llm.Message{
		first,
		llm.NewTextMessage("assistant", "첫 답변"),
		second,
	}

	firstAdds := []string{"<recall-context>계약 A</recall-context>"}
	secondAdds := []string{"<recall-context>계약 B</recall-context>"}

	_, ok, clean1, idx1 := injectTailAdditionsTracked([]llm.Message{first}, firstAdds)
	if !ok {
		t.Fatal("first injection failed")
	}
	recordPersistedTail(session, clean1, userMessageHashOrdinal([]llm.Message{first}, idx1), firstAdds)

	cleanForOrdinal := history
	attached := attachPersistedTails(session, history)
	_, ok, clean2, idx2 := injectTailAdditionsTracked(attached, secondAdds)
	if !ok {
		t.Fatal("second injection failed")
	}
	recordPersistedTail(session, clean2, userMessageHashOrdinal(cleanForOrdinal, idx2), secondAdds)

	final := attachPersistedTails(session, history)
	firstBody := string(final[0].Content.Bytes())
	secondBody := string(final[2].Content.Bytes())
	if !strings.Contains(firstBody, "계약 A") || strings.Contains(firstBody, "계약 B") {
		t.Fatalf("first duplicate got wrong tail: %q", firstBody)
	}
	if !strings.Contains(secondBody, "계약 B") || strings.Contains(secondBody, "계약 A") {
		t.Fatalf("second duplicate got wrong tail: %q", secondBody)
	}
}
