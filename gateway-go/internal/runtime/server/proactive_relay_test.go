package server

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
)

// recordingWorkFeed is a work-feed fake that records Append calls.
type recordingWorkFeed struct{ items []workfeed.Item }

func (w *recordingWorkFeed) Append(it workfeed.Item) (workfeed.Item, error) {
	w.items = append(w.items, it)
	return it, nil
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

// TestRelay verifies that relay() always delivers to the native 업무 session
// (client:main) plus a live push, regardless of the session key argument.
func TestRelay(t *testing.T) {
	store := newRecordingTranscriptStore()
	hub := newClientPushHub()
	events, unsub := hub.subscribe()
	defer unsub()

	d := proactiveRelayDeps{
		transcriptStore: store,
		pushHub:         hub,
	}

	delivered, err := d.relay(context.Background(), "ignored-session-key", "📬 업무 리포트 본문")
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("relay should report delivered when transcript store is wired")
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
			t.Errorf("relay must not write a telegram session, wrote %q", k)
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

// TestRelay_SuppressesContentless verifies that "nothing to report" proactive
// bodies (an email-check cron's "없습니다" ping, a dreaming "변경 없음", an analysis
// stub) are dropped entirely: no transcript bubble, no work-feed card, no push.
func TestRelay_SuppressesContentless(t *testing.T) {
	cases := []string{
		"읽지 않은 카카오메일 알림이 없습니다.",
		"읽지 않은 카카오메일 알림이 없어요. 분석할 새 메일이 없습니다 🐾",
		"읽지 않은 카카오메일 알림이 없어요. 분석할 새 메일이 없으니 패스할게요. 🐾",
		"검색 결과 없음 — 읽지 않은 카카오메일 알림이 없습니다.",
		"읽지 않은 카카오메일 알림 없음.",
		"분석할 메일 없어요 🐾",
		"(분석 실패)",
		"🌙 Aurora Dream 완료: 변경 없음 (1.2s)",
	}
	for _, body := range cases {
		store := newRecordingTranscriptStore()
		feed := &recordingWorkFeed{}
		hub := newClientPushHub()
		events, unsub := hub.subscribe()

		d := proactiveRelayDeps{transcriptStore: store, pushHub: hub, workFeed: feed}
		delivered, err := d.relay(context.Background(), "ignored-session-key", body)

		if err != nil {
			t.Fatalf("relay(%q): %v", body, err)
		}
		if delivered {
			t.Errorf("relay(%q) delivered=true, want suppressed", body)
		}
		if n := len(store.appends[nativeWorkSessionKey]); n != 0 {
			t.Errorf("relay(%q) wrote %d transcript append(s), want 0", body, n)
		}
		if n := len(feed.items); n != 0 {
			t.Errorf("relay(%q) wrote %d work-feed item(s), want 0", body, n)
		}
		// Check the push channel while it is still open (empty → default);
		// unsub closes it, and a closed channel would read as a false event.
		select {
		case <-events:
			t.Errorf("relay(%q) emitted a push, want none", body)
		default:
		}
		unsub()
	}
}

// TestRelay_SuppressesSilentToken verifies the proactive relay honors the
// NO_REPLY silent-reply contract: a turn that signals "nothing to report" with
// the bare token is dropped entirely instead of leaking a literal "NO_REPLY"
// transcript bubble + work-feed card + push.
func TestRelay_SuppressesSilentToken(t *testing.T) {
	for _, body := range []string{"NO_REPLY", "NO_REPLY 🐾", "  NO_REPLY  ", "**NO_REPLY**"} {
		store := newRecordingTranscriptStore()
		feed := &recordingWorkFeed{}
		hub := newClientPushHub()
		events, unsub := hub.subscribe()

		d := proactiveRelayDeps{transcriptStore: store, pushHub: hub, workFeed: feed}
		delivered, err := d.relay(context.Background(), "ignored-session-key", body)
		if err != nil {
			t.Fatalf("relay(%q): %v", body, err)
		}
		if delivered {
			t.Errorf("relay(%q) delivered=true, want suppressed", body)
		}
		if n := len(store.appends[nativeWorkSessionKey]); n != 0 {
			t.Errorf("relay(%q) wrote %d transcript append(s), want 0", body, n)
		}
		if n := len(feed.items); n != 0 {
			t.Errorf("relay(%q) wrote %d work-feed item(s), want 0", body, n)
		}
		select {
		case <-events:
			t.Errorf("relay(%q) emitted a push, want none", body)
		default:
		}
		unsub()
	}
}

// TestRelay_StripsTrailingSilentToken verifies a real report that merely ends
// with a NO_REPLY token is still delivered — with the token stripped — rather
// than suppressed wholesale.
func TestRelay_StripsTrailingSilentToken(t *testing.T) {
	store := newRecordingTranscriptStore()
	feed := &recordingWorkFeed{}
	hub := newClientPushHub()

	d := proactiveRelayDeps{transcriptStore: store, pushHub: hub, workFeed: feed}
	delivered, err := d.relay(context.Background(), "ignored-session-key", "긴급: 계약서 서명 필요\nNO_REPLY")
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("relay delivered=false, want delivered (real content precedes the token)")
	}
	if n := len(feed.items); n != 1 {
		t.Fatalf("got %d work-feed item(s), want 1", n)
	}
	if got := feed.items[0].Body; strings.Contains(got, "NO_REPLY") {
		t.Errorf("work-feed body still contains the token: %q", got)
	}
}

func TestIsContentlessProactive(t *testing.T) {
	contentless := []string{
		"",
		"   ",
		"읽지 않은 카카오메일 알림이 없습니다.",
		"읽지 않은 카카오메일 알림이 없어요. 분석할 새 메일이 없으니 패스할게요. 🐾",
		"검색 결과 없음 — 읽지 않은 카카오메일 알림이 없습니다.",
		"읽지 않은 카카오메일 알림 없음.",
		"분석할 메일 없어요 🐾",
		"(분석 실패)",
		"🌙 Aurora Dream 완료: 변경 없음 (1.2s)",
		// Multi-line "nothing to report" wrapped in headers / blank lines / rules
		// — the single-line check missed these, so they piled up as cards.
		"## 알림\n\n- 읽지 않은 카카오메일 알림이 없습니다",
		"### 결과\n\n변경 없음\n\n---",
		"🌙 Aurora Dream\n\n변경 없음",
	}
	for _, s := range contentless {
		if !isContentlessProactive(s) {
			t.Errorf("isContentlessProactive(%q) = false, want true", s)
		}
	}

	substantive := []string{
		"📬 업무 리포트 본문",
		"⏰ 15분 후: 대한전선 착수보고회 (회의실 A)",
		"📬 **메일 분석 보고** (6/3 기준, 업무 7건 분석)\n\n**🔴 긴급 — 대한전선 당진 2차**\n• 6/5 착수보고회",
		// A multi-section brief that merely mentions "없음" must survive.
		"오늘 업무 요약\n- 긴급 메일 없음, 단 대한전선 건 확인 필요\n- 회의 2건",
		// A long multi-section report keeps real substance even after markers
		// and emoji are stripped, so the multi-line check must leave it alone.
		"## 📧 메일 분석 보고\n\n### 🔴 긴급\n- 대한전선 당진 2차 6/5 착수보고회 참석 확인 필요\n- 무림 울산공장 풍력 검토안 회신 필요\n\n### 🟡 확인\n- JOCA 케이블 가격 재확인 후 발주 수량 결정",
	}
	for _, s := range substantive {
		if isContentlessProactive(s) {
			t.Errorf("isContentlessProactive(%q) = true, want false", s)
		}
	}
}

// TestRelay_ExtractsTitleAndSummary verifies the proactive relay derives a human
// title + summary from the body (not the fixed "업무 리포트" + first-line slice)
// and never leaks markdown markers into either field.
func TestRelay_ExtractsTitleAndSummary(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantTitle  string
		summaryHas string
	}{
		{
			"atx heading plus sub",
			"## 📧 최신 메일 분석 보고\n\n**분석 대상**: fred@jocacable.com → 2026-06-08",
			"📧 최신 메일 분석 보고",
			"분석 대상",
		},
		{
			"leading hrule",
			"---\n\n## 📧 JOCA Cable 최신 메일 분석 보고\n\n**발신**: fred@jocacable.com",
			"📧 JOCA Cable 최신 메일 분석 보고",
			"발신",
		},
		{
			"generic heading folds sub-heading",
			"## 분석\n\n### 왜 지금 왔는가\n\n이 메일은 무림 울산공장 풍력 사업 검토안이다.",
			"분석 — 왜 지금 왔는가",
			"무림 울산공장",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			feed := &recordingWorkFeed{}
			d := proactiveRelayDeps{transcriptStore: newRecordingTranscriptStore(), workFeed: feed}
			if _, err := d.relayNative(tc.body); err != nil {
				t.Fatalf("relayNative: %v", err)
			}
			if len(feed.items) != 1 {
				t.Fatalf("got %d work-feed item(s), want 1", len(feed.items))
			}
			it := feed.items[0]
			if it.Title != tc.wantTitle {
				t.Errorf("title = %q, want %q", it.Title, tc.wantTitle)
			}
			if !strings.Contains(it.Summary, tc.summaryHas) {
				t.Errorf("summary %q missing %q", it.Summary, tc.summaryHas)
			}
			for _, marker := range []string{"##", "**", "---"} {
				if strings.Contains(it.Title, marker) {
					t.Errorf("title leaked marker %q: %q", marker, it.Title)
				}
				if strings.Contains(it.Summary, marker) {
					t.Errorf("summary leaked marker %q: %q", marker, it.Summary)
				}
			}
		})
	}
}

// TestRelay_EmptyTitleWhenUnextractable verifies that a body with no extractable
// title (markers only) is still delivered with an empty Title — the store's
// normalizeNew then supplies the "업무 리포트" default. It must not be dropped as
// contentless (no "없음" fragment present).
func TestRelay_EmptyTitleWhenUnextractable(t *testing.T) {
	feed := &recordingWorkFeed{}
	d := proactiveRelayDeps{transcriptStore: newRecordingTranscriptStore(), workFeed: feed}
	if _, err := d.relayNative("---\n***\n___"); err != nil {
		t.Fatalf("relayNative: %v", err)
	}
	if len(feed.items) != 1 {
		t.Fatalf("got %d work-feed item(s), want 1", len(feed.items))
	}
	if feed.items[0].Title != "" {
		t.Errorf("title = %q, want empty (store fallback supplies default)", feed.items[0].Title)
	}
}
