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
		"client:sub:verify-docs-1784342370344":                 "subagent", // 현행 네임스페이스
		"client:main:sub-verify-docs-1784342370344":            "subagent", // 레거시 표식
		"client:main:subagent:1784342370344":                   "subagent", // 레거시 epoch 꼬리
		"client:main:gmail-large-attachment-fix:1782276675257": "subagent",
		"cron:email-analysis:1785052800110":                    "cron",
		"submain:heartbeat":                                    "heartbeat",
	} {
		if got := WorkTypeForKey(key); got != want {
			t.Errorf("WorkTypeForKey(%q) = %q, want %q", key, got, want)
		}
	}
}

// Live-test transcripts sit inside client: and are otherwise indistinguishable
// from real chats, so every consumer that mines "real use" needs one predicate
// to tell them apart — the smoke corpus outnumbered real sessions 15:1 in the
// skill-review pool while each one looked like a user session.
func TestIsLiveTestSessionSeparatesHarnessRunsFromRealUse(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]bool{
		"client:lt-3902894":     true, // mock_native_client.py, bare pid
		"client:lt-3902894-6":   true, // …with the per-message counter
		"lt-verify-card-3":      true, // ad-hoc probe
		"livetest:think-ko":     true,
		"client:main":           false,
		"client:main:lt-report": false, // "lt-" only marks the harness at the front
		"cron:morning-letter:1": false,
	} {
		if got := IsLiveTestSession(key); got != want {
			t.Errorf("IsLiveTestSession(%q) = %v, want %v", key, got, want)
		}
	}
}

// The minter and the classifier must never drift: every key SpawnedChildKey
// produces has to read back as a spawned child, including labels that carry
// characters a raw key would have split into extra segments.
func TestSpawnedChildKeyRoundTripsThroughClassifier(t *testing.T) {
	t.Parallel()
	for _, label := range []string{"verify docs", "gmail:large:attachment", "계약서 검토", "", "a-very-long-label-that-exceeds-the-forty-character-budget-by-a-lot"} {
		key := SpawnedChildKey(label, 1784342370344)
		if !IsSpawnedChildKey(key) {
			t.Errorf("SpawnedChildKey(%q) = %q, not classified as a spawned child", label, key)
		}
		if _, ok := RestorableTranscriptChannel(key); ok {
			t.Errorf("key %q (label %q) is restorable into the conversation drawer", key, label)
		}
		if got := WorkTypeForKey(key); got != "subagent" {
			t.Errorf("WorkTypeForKey(%q) = %q, want subagent", key, got)
		}
		if strings.Count(key, ":") != strings.Count(SpawnedChildPrefix, ":") {
			t.Errorf("key %q (label %q) grew extra segments", key, label)
		}
		// The prompt-facing channel, auto-resume eligibility and chat-activity
		// recording all key off the client: prefix — a sub-agent must keep it.
		if !strings.HasPrefix(key, "client:") {
			t.Errorf("key %q left the client: namespace (splits the APC prefix family)", key)
		}
	}
}

// The heartbeat continues the user's last conversation — but a delegated run is
// the agent's own scratch work, not a conversation to wake up inside.
func TestHeartbeatTargetSessionSkipsSpawnedChildren(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{
		"client:main:6c4385b5-6124-4224-afc2-492017824ef2": "client:main:6c4385b5-6124-4224-afc2-492017824ef2",
		"client:sub:verify-docs-1784342370344":             NativeWorkSessionKey,
		"client:main:subagent:1784342370344":               NativeWorkSessionKey,
		"cron:mailpoll":                                    NativeWorkSessionKey,
	} {
		if got := HeartbeatTargetSession(key); got != want {
			t.Errorf("HeartbeatTargetSession(%q) = %q, want %q", key, got, want)
		}
	}
}
