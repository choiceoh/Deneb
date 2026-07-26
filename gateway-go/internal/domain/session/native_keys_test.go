package session

import (
	"strings"
	"testing"
)

func TestRestorableTranscriptChannelParsesValidAndInvalidKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		key string
		ok  bool
	}{
		{key: "client:main", ok: true},
		{key: "client:main:project", ok: true},
		{key: "chat:legacy", ok: true},
		// Per-conversation keys the clients mint stay restorable…
		{key: "client:main:6c4385b5-6124-4224-afc2-492017824ef2", ok: true},
		{key: "client:main:k3x9ab12", ok: true},
		{key: "client:main:wf-abc123", ok: true},
		{key: "client:main:dream", ok: true},
		// …while delegated sub-agent runs do not: they are work the agent
		// spawned, not a conversation to restore into the drawer. Marked shape
		// first, then the pre-marker keys still sitting in the transcript dir.
		{key: "client:main:sub-verify-docs-1784342370344", ok: false},
		{key: "client:main:sub-a-1784342370344:sub-b-1784342999999", ok: false},
		{key: "client:main:subagent:1784342370344", ok: false},
		{key: "client:main:gmail-large-attachment-fix:1782276675257", ok: false},
		{key: "client:topic:old", ok: false},
		{key: "client:uuid", ok: false},
		{key: "chat:", ok: false},
		{key: "", ok: false},
	} {
		channel, ok := RestorableTranscriptChannel(tc.key)
		if ok != tc.ok {
			t.Errorf("RestorableTranscriptChannel(%q) ok = %v, want %v", tc.key, ok, tc.ok)
		}
		if ok && channel != "client" {
			t.Errorf("RestorableTranscriptChannel(%q) channel = %q, want client", tc.key, channel)
		}
	}
}

// Regression: the usage report buckets sessions through WorkTypeForKey. Both
// clients mint a fresh client:main:<id> per conversation, so a bare-prefix
// subagent test filed the user's own chats as delegated work and left the chat
// bucket holding only the client:main home session.
func TestWorkTypeForKeySeparatesConversationsFromSpawnedChildren(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{
		"client:main": "chat",
		"client:main:6c4385b5-6124-4224-afc2-492017824ef2":     "chat", // 폰 새 대화
		"client:main:k3x9ab12":                                 "chat", // 데스크톱 새 대화
		"client:main:wf-abc123":                                "chat", // 카드 곁대화
		"client:main:dream":                                    "dream",
		"client:main:subagent:1784342370344":                   "subagent",
		"client:main:gmail-large-attachment-fix:1782276675257": "subagent",
		"cron:email-analysis:1785052800110":                    "cron",
		"submain:heartbeat":                                    "heartbeat",
	} {
		if got := WorkTypeForKey(key); got != want {
			t.Errorf("WorkTypeForKey(%q) = %q, want %q", key, got, want)
		}
	}
}

// The minter and the classifier must never drift: every key SpawnedChildKey
// produces has to read back as a spawned child, including labels that carry
// characters a raw key would have split into extra segments.
func TestSpawnedChildKeyRoundTripsThroughClassifier(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"verify docs", "gmail:large:attachment", "계약서 검토", "", "a-very-long-label-that-exceeds-the-forty-character-budget-by-a-lot"} {
		key := SpawnedChildKey(NativeWorkSessionKey, label, 1784342370344)
		if !IsSpawnedChildKey(key) {
			t.Errorf("SpawnedChildKey(%q) = %q, not classified as a spawned child", label, key)
		}
		if _, ok := RestorableTranscriptChannel(key); ok {
			t.Errorf("key %q (label %q) is restorable into the conversation drawer", key, label)
		}
		if got := WorkTypeForKey(key); got != "subagent" {
			t.Errorf("WorkTypeForKey(%q) = %q, want subagent", key, got)
		}
		if strings.Count(key, ":") != strings.Count(NativeWorkSessionKey, ":")+1 {
			t.Errorf("key %q (label %q) grew extra segments", key, label)
		}
	}
}
