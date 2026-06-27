package server

import (
	"context"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/denebui"
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

// TestStripProactiveMetaPreamble covers the real leak cases observed in
// ~/.deneb/workfeed.jsonl (a model opening a cron/morning-letter report with
// working narration) and the legit reports that must pass through untouched.
func TestStripProactiveMetaPreamble(t *testing.T) {
	// strip[i] = {body, wantPrefix}: the preamble (+ divider) must be removed and
	// the remainder must begin with wantPrefix and no longer contain the preamble.
	strip := []struct{ body, wantPrefix, gone string }{
		{
			"이제 충분한 맥락을 확보했다. 모닝레터를 작성한다.\n\n---\n\n**☀️ 모닝레터 — 2026.06.07 (일)**\n\n오늘의 시장 현황과 주요 일정을 아래에 정리합니다.",
			"**☀️ 모닝레터", "충분한 맥락을 확보",
		},
		{
			"메일 분석 완료. 위키 업데이트까지 끝났습니다.\n\n---\n\n## 📧 6/3(수) 메일 종합 분석 — 7건\n\n### 🔴 긴급 — 대한전선 당진 2차 착수보고회 D-2",
			"## 📧", "위키 업데이트까지 끝났습니다",
		},
		{
			"전체 맥락 파악됐습니다. 분석 결과 정리합니다.\n\n---\n\n## 📨 2026-06-08 (월) 수신 메일 종합 분석\n\n총 2건 수신 — 대한전선 당진 2차 관련.",
			"## 📨", "분석 결과 정리합니다",
		},
		{
			"메일 도착 감지 → Gmail inbox 최신 1건 분석 완료\n\n🟡 **[탑솔라(주)] 광명역 B환승주차장 가배치 요청**\n발신자: 김대희 과장. 가배치도 검토 요청 건입니다.",
			"🟡 **[탑솔라", "메일 도착 감지",
		},
		{
			"좋아요, 이번 주 첫날 모닝레터입니다.\n\n---\n\n**☀️ 네브 모닝레터 — 2026년 6월 8일 (월)**\n\n시장 현황과 오늘의 핵심 일정입니다.",
			"**☀️ 네브 모닝레터", "좋아요",
		},
		{
			"모닝레터, 6/4 월요일 발송 내용입니다.\n\n<pre>\n📋 모닝레터 (2026-06-04)\n\n1) [대한전선] 당진 3MW 2차 사업 — 내일 착수보고회",
			"<pre>", "발송 내용입니다",
		},
		{
			"메일 분석 완료. 최근 수신 메일 중 업무 관련 핵심만 정리해서 보고드릴게요.\n\n---\n\n📬 **메일 분석 보고 (6/3 16:30 기준)**\n대한전선 당진 2차 착수보고회 관련 메일입니다.",
			"📬 **메일 분석 보고", "보고드릴게요",
		},
	}
	for _, c := range strip {
		got := stripProactiveMetaPreamble(c.body)
		if !strings.HasPrefix(got, c.wantPrefix) {
			t.Errorf("strip(%.30q…)\n  got:  %.60q…\n  want prefix: %q", c.body, got, c.wantPrefix)
		}
		if strings.Contains(got, c.gone) {
			t.Errorf("strip(%.30q…) still contains preamble fragment %q:\n  %.80q", c.body, c.gone, got)
		}
	}

	// keep[i]: real reports whose first line is a header, greeting, or direct
	// subject analysis — never working narration. Must be returned byte-for-byte.
	keep := []string{
		// emoji-led titled headers (one contains "분석 완료" but is structural)
		"📬 메일 분석 보고 (6/3 16:00 기준)\n\n━━━━━━━━━━━━━━━━━━━━\n\n🔴 **긴급 — 대한전선 당진 2차 착수보고회**\n6/5 착수보고회 자료 검토 요청.",
		"📊 **당일 메일 종합 분석 완료** (2026-06-05)\n\n---\n\n## 🔴 긴급 / 임박 (즉시 확인 필요)\n대한전선 당진 건.",
		// markdown / bold headers
		"## 분석\n\n### 왜 지금 왔는지\n오늘 오전 10:49에 김대희 과장이 다원건축에 보낸 메일의 후속입니다.",
		"## 핵심: 이틀 앞으로 다가온 착수보고회, 타이한에 최종 자료 검토 요청\n\n### 왜 지금 왔는지\n6/2 합의에 따른 후속입니다.",
		"**발신 시점과 배경**\n고건이 6/4 18:00에 보낸 가배치도 요청 메일에 대한 답변으로, 6/8 15:22에 양도현이 회신했다.",
		"### 분석 결과\n\n총 3건의 메일이 수신되었으며 핵심은 대한전선 건입니다.",
		"# 통합 리포트 — 2026년 6월 8일 월요일 수신 메일 분석\n\n## 🔴 최우선: 네이버 계정 보안 변경 연쇄",
		"🔴 / 🟡 / 🟢 분류 보고\n\n🟡 **확인 필요** — 현대자동차 울산공장 생기센터 재건축 태양광 설비공사 200kW 견적요청",
		// direct subject analysis — describes the email, not the agent's process
		"이 이메일은 Google의 정기적인 정책 변경 안내로, 특정 사전 합의나 사용자의 요청에 따른 응답이 아니라 서비스 업데이트 공지입니다.\n\n발신자는 Google입니다.",
		"이 이메일은 서림철강의 공장 태양광발전사업 타당성 검토를 위해 탑솔라 기획조정실 김대희 과장이 1차 검토 결과를 회신한 것입니다.\n\n### 왜 지금",
		// morning-letter greeting (a persona opener, not narration)
		"2026-06-05(금) 아침입니다 🐾\n오늘은 대한전선 2차 태양광 착수보고회 당일이라 분주하실 거예요. 먼저 잠시 중요 이슈부터 짚고, 아래에 전체를 적을게요.\n\n시장 현황입니다.",
		// a real short first line that is content, not narration
		"긴급: 계약서 서명 필요\n\n대한전선 당진 2차 계약서에 오늘 중 서명이 필요합니다. 법무팀 검토는 완료되었습니다.",
		// single-paragraph body (no blank line) — nothing to strip toward
		"📬 업무 리포트 본문",
	}
	for _, body := range keep {
		if got := stripProactiveMetaPreamble(body); got != body {
			t.Errorf("strip must NOT alter a real report:\n  in:  %.60q\n  out: %.60q", body, got)
		}
	}

	// Degenerate cases: a preamble with no real body left must keep the original
	// rather than reduce the card to near-empty.
	for _, body := range []string{
		"메일 분석 완료. 위키 업데이트까지 끝났습니다.\n\n---", // divider only after preamble
		"전체 맥락 파악됐습니다. 분석 결과 정리합니다.\n\n네.",  // remainder too short
	} {
		if got := stripProactiveMetaPreamble(body); got != body {
			t.Errorf("strip must keep original when no substantial body remains:\n  in:  %q\n  out: %q", body, got)
		}
	}
}

// TestRelay_StripsMetaPreamble verifies the work-feed card body delivered for a
// proactive report has the leading working-narration preamble removed end-to-end
// (not just the standalone helper), and that the feed-backed main session leaves
// the chat transcript untouched (PR #2448 feed-only delivery).
func TestRelay_StripsMetaPreamble(t *testing.T) {
	store := newRecordingTranscriptStore()
	feed := &recordingWorkFeed{}
	hub := newClientPushHub()

	d := proactiveRelayDeps{transcriptStore: store, pushHub: hub, workFeed: feed}
	body := "전체 맥락 파악됐습니다. 분석 결과 정리합니다.\n\n---\n\n## 📨 2026-06-08 수신 메일 종합 분석\n\n총 2건 수신 — 대한전선 당진 2차 관련 건입니다."
	delivered, err := d.relay(context.Background(), "ignored", body)
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if !delivered {
		t.Fatal("relay delivered=false, want delivered")
	}
	if n := len(feed.items); n != 1 {
		t.Fatalf("got %d work-feed item(s), want 1", n)
	}
	got := feed.items[0].Body
	if !strings.HasPrefix(got, "## 📨") {
		t.Errorf("work-feed body should start with the real header, got: %.50q", got)
	}
	if strings.Contains(got, "분석 결과 정리합니다") {
		t.Errorf("work-feed body still carries the preamble: %.80q", got)
	}
	if msgs := store.appends[nativeWorkSessionKey]; len(msgs) != 0 {
		t.Errorf("feed-only main session must not mirror into the transcript, got: %+v", msgs)
	}
}

// TestRelay_WorkModelFooter verifies the bare model name is appended at the foot
// of a 업무 feed report (client:main) when the resolver yields a name, that it sits
// at the end without polluting the card title/summary, and that it is absent when
// the resolver is unwired or returns "". The report body never contains the model
// name, so Contains(...) is a clean discriminator for the stamp.
func TestRelay_WorkModelFooter(t *testing.T) {
	const model = "deepseek-v4-flash"
	body := "📊 일일 브리핑\n\n오늘 처리할 핵심 안건이 있습니다.\n- 대한전선 당진 2차 착수보고회 참석 확인\n- 무림 울산공장 풍력 검토안 회신"

	t.Run("stamped on the feed card", func(t *testing.T) {
		feed := &recordingWorkFeed{}
		d := proactiveRelayDeps{workFeed: feed, workModel: func() string { return model }}
		delivered, err := d.relayNative(body)
		if err != nil || !delivered {
			t.Fatalf("relayNative: delivered=%v err=%v", delivered, err)
		}
		if n := len(feed.items); n != 1 {
			t.Fatalf("got %d work-feed item(s), want 1", n)
		}
		card := feed.items[0]
		if !strings.HasSuffix(strings.TrimRight(card.Body, "\n"), model) {
			t.Errorf("feed body must end with the bare model name %q, got:\n%s", model, card.Body)
		}
		// The stamp is meta — it must not leak into the card's title or 2-line summary.
		if strings.Contains(card.Title, model) || strings.Contains(card.Summary, model) {
			t.Errorf("model name leaked into title/summary: title=%q summary=%q", card.Title, card.Summary)
		}
		// The real report content must survive ahead of the stamp.
		if !strings.Contains(card.Body, "일일 브리핑") || !strings.Contains(card.Body, "착수보고회") {
			t.Errorf("report content lost from feed body:\n%s", card.Body)
		}
	})

	t.Run("absent when resolver unwired or empty", func(t *testing.T) {
		for name, resolver := range map[string]func() string{
			"nil":   nil,
			"empty": func() string { return "" },
		} {
			feed := &recordingWorkFeed{}
			d := proactiveRelayDeps{workFeed: feed, workModel: resolver}
			if _, err := d.relayNative(body); err != nil {
				t.Fatalf("%s resolver: relayNative: %v", name, err)
			}
			if n := len(feed.items); n != 1 {
				t.Fatalf("%s resolver: got %d work-feed item(s), want 1", name, n)
			}
			if got := feed.items[0].Body; got != body {
				t.Errorf("%s resolver: feed body must equal the unstamped report, got:\n%s", name, got)
			}
		}
	})

	t.Run("scoped to the main feed — sub-sessions unstamped", func(t *testing.T) {
		store := newRecordingTranscriptStore()
		d := proactiveRelayDeps{transcriptStore: store, workModel: func() string { return model }}
		if _, err := d.relayNativeTo(dreamWorkSessionKey, body); err != nil {
			t.Fatalf("relayNativeTo(dream): %v", err)
		}
		msgs := store.appends[dreamWorkSessionKey]
		if len(msgs) != 1 {
			t.Fatalf("got %d dream append(s), want 1", len(msgs))
		}
		if got := msgs[0].TextContent(); strings.Contains(got, model) {
			t.Errorf("dream sub-session must not be stamped, got:\n%s", got)
		}
	})
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

// TestRelayCollapsed verifies the collapsed mail-analysis delivery. For the
// feed-backed main session (client:main) the report lands in the 업무 feed only:
// the work-feed card carries the raw prose body (read inline in the 피드 screen),
// the chat transcript is left untouched (PR #2448), and the push preview keeps the
// raw prose. The accordion-fence path remains exercised for feed-less sessions
// (the subtests below that omit workFeed). Suppression gates still apply.
func TestRelayCollapsed(t *testing.T) {
	body := "## 📧 JOCA Cable 최신 메일 분석 보고\n\n**발신**: fred@jocacable.com\n\n- 회신 기한: 6/13"

	t.Run("feed-backed main session: feed keeps prose, transcript untouched", func(t *testing.T) {
		store := newRecordingTranscriptStore()
		feed := &recordingWorkFeed{}
		hub := newClientPushHub()
		events, unsub := hub.subscribe()
		defer unsub()
		d := proactiveRelayDeps{transcriptStore: store, workFeed: feed, pushHub: hub}

		delivered, err := d.relayCollapsed(context.Background(), "ignored", body)
		if err != nil || !delivered {
			t.Fatalf("relayCollapsed: delivered=%v err=%v", delivered, err)
		}

		if got := len(store.appends[nativeWorkSessionKey]); got != 0 {
			t.Errorf("feed-only main session must not mirror into the transcript, got %d appends", got)
		}

		if len(feed.items) != 1 {
			t.Fatalf("want 1 work-feed item, got %d", len(feed.items))
		}
		if feed.items[0].Body != body {
			t.Errorf("work-feed body must stay raw prose, got %q", feed.items[0].Body)
		}
		if feed.items[0].Title != "📧 JOCA Cable 최신 메일 분석 보고" {
			t.Errorf("work-feed title = %q", feed.items[0].Title)
		}

		select {
		case ev := <-events:
			if strings.Contains(ev.Body, "deneb-ui") || strings.Contains(ev.Body, `"accordion"`) {
				t.Errorf("push preview leaked fence JSON: %q", ev.Body)
			}
		default:
			t.Error("expected a live push event, got none")
		}
	})

	t.Run("unextractable title falls back to plain prose", func(t *testing.T) {
		store := newRecordingTranscriptStore()
		d := proactiveRelayDeps{transcriptStore: store}
		if _, err := d.relayCollapsed(context.Background(), "ignored", "---\n***\n___"); err != nil {
			t.Fatalf("relayCollapsed: %v", err)
		}
		got := store.appends[nativeWorkSessionKey]
		if len(got) != 1 {
			t.Fatalf("want 1 transcript append, got %d", len(got))
		}
		if denebui.HasFence(got[0].TextContent()) {
			t.Errorf("titleless body must deliver plain, got fence: %s", got[0].TextContent())
		}
	})

	t.Run("folded title keeps body intact", func(t *testing.T) {
		// "## 분석" + "### 왜 지금 왔는가" folds into "분석 — 왜 지금 왔는가": no
		// single body line equals the title, so nothing is stripped.
		folded := "## 분석\n\n### 왜 지금 왔는가\n\n이 메일은 무림 울산공장 풍력 사업 검토안이다."
		store := newRecordingTranscriptStore()
		d := proactiveRelayDeps{transcriptStore: store}
		if _, err := d.relayCollapsed(context.Background(), "ignored", folded); err != nil {
			t.Fatalf("relayCollapsed: %v", err)
		}
		text := store.appends[nativeWorkSessionKey][0].TextContent()
		if !strings.Contains(text, `## 분석`) || !strings.Contains(text, "왜 지금 왔는가") {
			t.Errorf("folded-title body must stay intact, got: %s", text)
		}
	})

	t.Run("suppression gates still apply", func(t *testing.T) {
		store := newRecordingTranscriptStore()
		d := proactiveRelayDeps{transcriptStore: store}
		delivered, err := d.relayCollapsed(context.Background(), "ignored", "분석할 새 메일이 없습니다.")
		if err != nil {
			t.Fatalf("relayCollapsed: %v", err)
		}
		if delivered || len(store.appends) != 0 {
			t.Errorf("contentless body must stay suppressed, delivered=%v appends=%v", delivered, store.appends)
		}
	})
}
