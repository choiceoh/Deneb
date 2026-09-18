package chat

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
)

// The language/mode anchor is the LAST tail addition on every persisted user
// turn, whatever else rides the tail — it is the last thing the model reads
// before its own turn marker (incident 2026-09-17, stream_0043).
func TestBuildTailAdditionsAnchorIsAlwaysLast(t *testing.T) {
	cases := []struct {
		name   string
		params RunParams
		recall string
		nb     string
		hints  string
		board  string
	}{
		{name: "bare interactive turn", params: RunParams{}},
		{name: "auto-delivered with recall", params: RunParams{AutoDeliveredOutput: true}, recall: "회상"},
		{name: "notebook grounded", params: RunParams{AutoDeliveredOutput: true, FeedContext: "feed"}, recall: "회상", nb: "노트북"},
		{name: "everything", params: RunParams{AutoDeliveredOutput: true, FeedContext: "feed"}, recall: "회상", hints: "힌트", board: "보드"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adds := buildTailAdditions(tc.params, tc.recall, tc.nb, tc.hints, tc.board)
			if len(adds) == 0 || adds[len(adds)-1] != responseLanguageAnchor {
				t.Fatalf("anchor is not the last addition: %#v", adds)
			}
			for _, a := range adds[:len(adds)-1] {
				if a == responseLanguageAnchor {
					t.Fatalf("anchor duplicated in %#v", adds)
				}
			}
		})
	}
	// The anchor must select the language and forbid continuing the records,
	// without forbidding tool calls (it rides the user message mid-run too).
	for _, want := range []string{"한국어", "이어쓰", "기록"} {
		if !strings.Contains(responseLanguageAnchor, want) {
			t.Errorf("anchor lacks %q: %q", want, responseLanguageAnchor)
		}
	}
	if strings.Contains(responseLanguageAnchor, "도구를 호출하지") {
		t.Errorf("anchor must not forbid tool calls: %q", responseLanguageAnchor)
	}
}

// Ephemeral autonomous turns (heartbeat, boot self-triggers, notifier) keep
// their own NO_REPLY / "## status" reply contract and do not carry the anchor.
func TestBuildTailAdditionsAnchorSkippedForEphemeralUser(t *testing.T) {
	adds := buildTailAdditions(RunParams{EphemeralUser: true, AutoDeliveredOutput: true}, "회상", "", "", "")
	for _, a := range adds {
		if a == responseLanguageAnchor {
			t.Fatalf("ephemeral turn carries the anchor: %#v", adds)
		}
	}
}

// A1′ of the change request, product side: the anchor is a strict SUFFIX of
// the otherwise-identical wire message. Arm A (no anchor) and arm B (anchor)
// share a byte-identical prefix up to the insertion offset, and B is exactly
// A + separator + anchor — so a replay that differs between the arms differs
// because of the anchor and nothing else. (Replaying "the same token ids" with
// the anchor applied is impossible by construction; this is the control that
// replaces it. The engine-side replay script is
// scripts/dev/telemachus_ab_replay.py.)
func TestTailAnchorIsAControlledSuffixOfTheWireMessage(t *testing.T) {
	base := []llm.Message{
		llm.NewTextMessage("user", "지난달 키아 EPC 건 진행 상황 정리해줘"),
	}
	shared := []string{"<recall-context>증거</recall-context>", autoDeliveryDirective}
	armA, okA := injectTailAdditions(base, shared)
	armB, okB := injectTailAdditions(base, append(append([]string{}, shared...), responseLanguageAnchor))
	if !okA || !okB {
		t.Fatal("injection failed")
	}
	a, b := messageText(t, armA[0]), messageText(t, armB[0])
	if !strings.HasPrefix(b, a) {
		t.Fatalf("arm B does not extend arm A byte-for-byte:\nA=%q\nB=%q", a, b)
	}
	if got, want := b[len(a):], "\n\n"+responseLanguageAnchor; got != want {
		t.Fatalf("arm B suffix = %q, want separator + anchor", got)
	}
	sum := func(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
	if sum(a) != sum(b[:len(a)]) {
		t.Fatal("prefix hash guard failed: arms diverge before the insertion offset")
	}
	// Byte-stable across turns: the anchor is a constant, so the tail
	// register re-attaches identical bytes and APC prefixes stay intact.
	armB2, _ := injectTailAdditions(base, append(append([]string{}, shared...), responseLanguageAnchor))
	if messageText(t, armB2[0]) != b {
		t.Fatal("anchor bytes are not stable across assemblies")
	}
}
