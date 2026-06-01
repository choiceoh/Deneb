package server

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

func TestResolveHome(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		activeHome func() int64
		wantKey    string
		wantOK     bool
	}{
		{"non-sentinel passthrough", "telegram:-1003946703971", nil, "telegram:-1003946703971", true},
		{"non-sentinel with thread passthrough", "telegram:-1003946703971:thread:5", nil, "telegram:-1003946703971:thread:5", true},
		{"sentinel resolves to active home", homeSessionKey, func() int64 { return -1003946703971 }, "telegram:-1003946703971", true},
		{"sentinel unresolved (activeHome returns 0)", homeSessionKey, func() int64 { return 0 }, "", false},
		{"sentinel unresolved (no resolver)", homeSessionKey, nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := proactiveRelayDeps{activeHome: tc.activeHome}
			gotKey, gotOK := d.resolveHome(tc.key)
			if gotKey != tc.wantKey || gotOK != tc.wantOK {
				t.Fatalf("resolveHome(%q) = (%q, %v), want (%q, %v)",
					tc.key, gotKey, gotOK, tc.wantKey, tc.wantOK)
			}
		})
	}
}

func TestMirrorsToNativeWork(t *testing.T) {
	cases := []struct {
		name, channel, target string
		want                  bool
	}{
		{"telegram general (no thread)", "telegram", "-1003946703971", true},
		{"telegram general (thread 0)", "telegram", "-1003946703971:thread:0", true},
		{"telegram named topic", "telegram", "-1003946703971:thread:5", false},
		{"non-telegram channel", "client", "main", false},
		{"empty channel", "", "x", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := mirrorsToNativeWork(tc.channel, tc.target); got != tc.want {
				t.Fatalf("mirrorsToNativeWork(%q,%q) = %v, want %v", tc.channel, tc.target, got, tc.want)
			}
		})
	}
}

func TestSplitSessionKey(t *testing.T) {
	cases := []struct {
		name, key, wantCh, wantTgt string
		wantOK                     bool
	}{
		{"telegram ok", "telegram:7074071666", "telegram", "7074071666", true},
		{"empty", "", "", "", false},
		{"no colon", "telegram", "", "", false},
		{"empty channel", ":7074071666", "", "", false},
		{"empty target", "telegram:", "", "", false},
		{"channel only colon", ":", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch, tgt, ok := splitSessionKey(tc.key)
			if ok != tc.wantOK || ch != tc.wantCh || tgt != tc.wantTgt {
				t.Fatalf("splitSessionKey(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.key, ch, tgt, ok, tc.wantCh, tc.wantTgt, tc.wantOK)
			}
		})
	}
}

// recordingTranscriptStore is a TranscriptStore fake that records Append calls.
type recordingTranscriptStore struct {
	appends map[string][]toolctx.ChatMessage
}

func newRecordingTranscriptStore() *recordingTranscriptStore {
	return &recordingTranscriptStore{appends: map[string][]toolctx.ChatMessage{}}
}

func (s *recordingTranscriptStore) Append(sessionKey string, msg toolctx.ChatMessage) error {
	s.appends[sessionKey] = append(s.appends[sessionKey], msg)
	return nil
}
func (s *recordingTranscriptStore) Load(string, int) ([]toolctx.ChatMessage, int, error) {
	return nil, 0, nil
}
func (s *recordingTranscriptStore) Delete(string) error         { return nil }
func (s *recordingTranscriptStore) ListKeys() ([]string, error) { return nil, nil }
func (s *recordingTranscriptStore) Search(string, int) ([]toolctx.SearchResult, error) {
	return nil, nil
}
func (s *recordingTranscriptStore) CloneRecent(string, string, int) error { return nil }

// TestRelayNativeOnly verifies that with nativeOnly set, relay() delivers a
// proactive report to the native 업무 session (client:main) plus a live push,
// and never touches Telegram — even when the call carries a Telegram session key
// and no Telegram plugin is wired.
func TestRelayNativeOnly(t *testing.T) {
	store := newRecordingTranscriptStore()
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	d := proactiveRelayDeps{
		transcriptStore: store,
		pushHub:         hub,
		nativeOnly:      true,
		// telegramPlug intentionally nil: native-only must not require it.
	}

	delivered, err := d.relay(context.Background(), "telegram:-1003946703971", "📬 업무 리포트 본문")
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("native-only relay should report delivered")
	}

	got := store.appends[nativeWorkSessionKey]
	if len(got) != 1 {
		t.Fatalf("want 1 append to %q, got %d (all keys: %v)", nativeWorkSessionKey, len(got), store.appends)
	}
	if got[0].Role != "assistant" {
		t.Errorf("appended role = %q, want assistant", got[0].Role)
	}
	for k := range store.appends {
		if strings.HasPrefix(k, "telegram:") {
			t.Errorf("native-only relay must not write a telegram session, wrote %q", k)
		}
	}

	select {
	case ev := <-events:
		if ev.Title != "Deneb" {
			t.Errorf("push title = %q, want Deneb", ev.Title)
		}
	default:
		t.Error("expected a live push event, got none")
	}
}

// TestRelayNativeOnly_HomeSentinel verifies the "telegram:home" sentinel (wired
// by the gmail-poll and wiki-dreaming notifiers) is delivered natively too,
// without needing an activeHome resolver — native-only short-circuits before
// resolveHome.
func TestRelayNativeOnly_HomeSentinel(t *testing.T) {
	store := newRecordingTranscriptStore()
	d := proactiveRelayDeps{transcriptStore: store, nativeOnly: true}

	delivered, err := d.relay(context.Background(), homeSessionKey, "morning letter body")
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("native-only relay should deliver the home sentinel without an activeHome resolver")
	}
	if len(store.appends[nativeWorkSessionKey]) != 1 {
		t.Fatalf("want 1 append to %q, got %v", nativeWorkSessionKey, store.appends)
	}
}
