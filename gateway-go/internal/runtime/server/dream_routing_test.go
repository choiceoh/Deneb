package server

import (
	"context"
	"testing"
)

// Aurora Dream notifications must land in the dedicated client:main:dream
// sub-session, never the primary 업무 feed (client:main) — and must NOT raise a
// 업무 work-feed card (the feed lists items globally, so a card would resurface in
// the main feed). Regression guard for "dreaming entering the main work session".
func TestDreamNotifierRoutesToSubSession(t *testing.T) {
	store := newRecordingTranscriptStore()
	feed := &recordingWorkFeed{}
	d := proactiveRelayDeps{transcriptStore: store, workFeed: feed}

	n := d.notifierForSession(dreamWorkSessionKey)
	if err := n.Notify(context.Background(), "📖 Wiki Dream 완료: 제안 2, 생성 1, 수정 0 (3.2s)"); err != nil {
		t.Fatalf("notify: %v", err)
	}

	if got := len(store.appends[dreamWorkSessionKey]); got != 1 {
		t.Fatalf("want 1 append to %q, got %d (all keys: %v)", dreamWorkSessionKey, got, store.appends)
	}
	if got := len(store.appends[nativeWorkSessionKey]); got != 0 {
		t.Errorf("dream must not touch the main session %q, got %d appends", nativeWorkSessionKey, got)
	}
	if len(feed.items) != 0 {
		t.Errorf("dream must not raise a work-feed card, got %d", len(feed.items))
	}
}

// A notifier bound to the main session (the gmail/dropbox wiring) delivers to the
// 업무 FEED only — the work-feed card carries the full body and the chat transcript
// is left untouched, so the 업무 chat stays a place to ask rather than a wall of
// pushed reports (PR #2448 feed-first home). The dream sub-session above still
// mirrors into its transcript because it has no feed surface.
func TestMainNotifierCardsFeedOnly(t *testing.T) {
	store := newRecordingTranscriptStore()
	feed := &recordingWorkFeed{}
	d := proactiveRelayDeps{transcriptStore: store, workFeed: feed}

	n := d.notifierForSession(nativeWorkSessionKey)
	if err := n.Notify(context.Background(), "📬 새 메일 3건 분석 완료"); err != nil {
		t.Fatalf("notify: %v", err)
	}
	if got := len(store.appends[nativeWorkSessionKey]); got != 0 {
		t.Errorf("feed-only main session must not mirror into the transcript, got %d appends", got)
	}
	if len(feed.items) != 1 {
		t.Fatalf("want 1 work-feed card for the main session, got %d", len(feed.items))
	}
	if feed.items[0].SessionKey != nativeWorkSessionKey {
		t.Errorf("card SessionKey = %q, want %q", feed.items[0].SessionKey, nativeWorkSessionKey)
	}
	if feed.items[0].Body != "📬 새 메일 3건 분석 완료" {
		t.Errorf("feed card must carry the full body, got %q", feed.items[0].Body)
	}
}
