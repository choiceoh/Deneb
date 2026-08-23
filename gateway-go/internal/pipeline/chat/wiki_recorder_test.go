package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/agent"
	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestMaybeRecordRunDiaryWritesDiaryAndFiresDreamTrigger pins the wiring
// shared by the async lifecycle and SendSync/SendSyncStream (the native chat
// path): an eligible turn records the diary — with leaked reasoning
// delimiters stripped — and fires the dream-turn trigger; an excluded
// session prefix does neither.
func TestMaybeRecordRunDiaryWritesDiaryAndFiresDreamTrigger(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	dreamFired := make(chan struct{}, 2)
	deps := runDeps{
		memory:      MemoryDeps{Wiki: store},
		dreamTurnFn: func(context.Context) { dreamFired <- struct{}{} },
	}
	result := &agent.AgentResult{
		Text:       "[thinking]\n내부 추론 노출\n[/thinking]\n계약 협상 단계입니다. 다음 주 공정표 제출 예정.",
		StopReason: "end_turn", Turns: 1,
	}
	maybeRecordRunDiary(deps, RunParams{SessionKey: "client:main", Message: "당진 계약 진행상황 정리해줘"}, result, nil)

	select {
	case <-dreamFired:
	case <-time.After(5 * time.Second):
		t.Fatal("dream-turn trigger did not fire for an eligible recorded turn")
	}
	content := readDiaryDir(t, store.DiaryDir())
	if strings.Contains(content, "[thinking]") || strings.Contains(content, "내부 추론 노출") {
		t.Fatalf("reasoning leak must be stripped from the diary:\n%s", content)
	}
	if !strings.Contains(content, "계약 협상 단계") {
		t.Fatalf("assistant outcome missing from diary:\n%s", content)
	}

	// Excluded prefix: nothing recorded, no dream trigger.
	maybeRecordRunDiary(deps, RunParams{SessionKey: "system:heartbeat", Message: "내부 하트비트 점검 메시지"}, result, nil)
	select {
	case <-dreamFired:
		t.Fatal("excluded session must not fire the dream trigger")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestMaybeRecordRunDiaryEmitsPreferenceSignalBeforeDreamTrigger pins the
// 선호 fast path: a preference-voicing turn nudges the dreamer
// (preferenceSignalFn) BEFORE the dream-turn increment — the increment's
// ShouldDream check must already see the pending signal — and a plain
// factual turn fires the increment only.
func TestMaybeRecordRunDiaryEmitsPreferenceSignalBeforeDreamTrigger(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	events := make(chan string, 4)
	deps := runDeps{
		memory:             MemoryDeps{Wiki: store},
		dreamTurnFn:        func(context.Context) { events <- "dream" },
		preferenceSignalFn: func() { events <- "pref" },
	}
	result := &agent.AgentResult{Text: "알겠습니다. 그 방식으로 정리하겠습니다.", StopReason: "end_turn", Turns: 1}

	maybeRecordRunDiary(deps, RunParams{SessionKey: "client:main", Message: "앞으로 보고는 산문으로 정리해줘"}, result, nil)
	if got := waitDiaryEvent(t, events); got != "pref" {
		t.Fatalf("first event = %q, want pref (must precede the dream-turn increment)", got)
	}
	if got := waitDiaryEvent(t, events); got != "dream" {
		t.Fatalf("second event = %q, want dream", got)
	}

	maybeRecordRunDiary(deps, RunParams{SessionKey: "client:main", Message: "당진 프로젝트 진행상황 알려줘"}, result, nil)
	if got := waitDiaryEvent(t, events); got != "dream" {
		t.Fatalf("non-preference turn: event = %q, want dream only", got)
	}
	select {
	case got := <-events:
		t.Fatalf("unexpected extra event %q after non-preference turn", got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestMaybeRecordRunDiaryEmitsProjectSignalBeforeDreamTrigger pins the
// deal-number fast path: a 견적/단가 turn nudges projectSignalFn BEFORE
// the dream increment, and "당진 프로젝트 진행상황" must not.
func TestMaybeRecordRunDiaryEmitsProjectSignalBeforeDreamTrigger(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	events := make(chan string, 4)
	deps := runDeps{
		memory:          MemoryDeps{Wiki: store},
		dreamTurnFn:     func(context.Context) { events <- "dream" },
		projectSignalFn: func() { events <- "project" },
	}
	result := &agent.AgentResult{Text: "공급가액 기준으로 정리했습니다.", StopReason: "end_turn", Turns: 1}

	maybeRecordRunDiary(deps, RunParams{SessionKey: "client:main", Message: "광주 캐노피 재견적 단가 확인해줘"}, result, nil)
	if got := waitDiaryEvent(t, events); got != "project" {
		t.Fatalf("first event = %q, want project (must precede the dream-turn increment)", got)
	}
	if got := waitDiaryEvent(t, events); got != "dream" {
		t.Fatalf("second event = %q, want dream", got)
	}

	status := &agent.AgentResult{Text: "공정 일정 기준으로 정리했습니다.", StopReason: "end_turn", Turns: 1}
	maybeRecordRunDiary(deps, RunParams{SessionKey: "client:main", Message: "당진 프로젝트 진행상황 알려줘"}, status, nil)
	if got := waitDiaryEvent(t, events); got != "dream" {
		t.Fatalf("bare 프로젝트 근황 must not emit project signal, got %q", got)
	}
	select {
	case got := <-events:
		t.Fatalf("unexpected extra event %q after status-only turn", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func waitDiaryEvent(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case e := <-ch:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for diary side-effect event")
		return ""
	}
}

func TestRecordDiaryWritesOutcomeForShortPrompt(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if recorded, _ := recordDiary(store, nil, "ㄱㄱ", []string{"read", "exec!"}, "구현 완료", "end_turn", 2); !recorded {
		t.Fatal("recordDiary returned false")
	}

	content := readDiaryDir(t, store.DiaryDir())
	for _, want := range []string{
		"사용자: ㄱㄱ",
		"신호: action/tools",
		"도구: read, exec!",
		"결과: 구현 완료",
		"상태: stop=end_turn, turns=2",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("diary content missing %q:\n%s", want, content)
		}
	}
}

// TestClassifyDiarySignalReturnsPreferenceTag pins the behavioral-signal
// capture: a user turn voicing a standing preference or style correction is
// tagged "선호" (and force-recorded as durable), so the dreamer aggregates an
// explicit cue for its working-style abstraction instead of inferring it
// from raw text.
func TestClassifyDiarySignalReturnsPreferenceTag(t *testing.T) {
	cases := []struct {
		msg     string
		wantTag bool
	}{
		{"앞으로 답변은 간결하게 해줘", true},     // standing directive + style
		{"불릿 말고 산문으로 정리해줘", true},     // replacement + style correction
		{"그 표현 그만 쓰고 다르게", true},      // negation directive
		{"항상 숫자 근거를 같이 줘", true},      // standing directive
		{"다음부터는 금액을 억 원으로 표기해", true}, // standing directive (다음부터)
		{"존댓말 쓰지 말고 편하게 해", true},     // tone correction
		{"내 호칭은 실장으로 불러줘", true},      // address correction
		{"이 번호는 기억해둬", true},          // explicit remember-this
		{"현대차 울산 결제기한 언제야?", false},   // factual question, not a preference
		{"이 버그 고쳐줘", false},           // task (durable keyword, not a preference cue)
	}
	for _, c := range cases {
		sig := classifyDiarySignal(c.msg, nil, "ok")
		got := strings.Contains(sig.Reason, "선호")
		if got != c.wantTag {
			t.Errorf("classifyDiarySignal(%q).Reason=%q → 선호-tag=%v, want %v", c.msg, sig.Reason, got, c.wantTag)
		}
	}
}

func TestRecordDiarySkipsShortPromptWithoutOutcome(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if recorded, _ := recordDiary(store, nil, "ㄱㄱ", nil, "", "end_turn", 1); recorded {
		t.Fatal("recordDiary returned true for short prompt without outcome")
	}
}

func TestRecordDiaryIgnoresTrivialAcknowledgement(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if recorded, _ := recordDiary(store, nil, "고마워", nil, "천만에요", "end_turn", 1); recorded {
		t.Fatal("recordDiary returned true for trivial acknowledgement")
	}
}

func TestShouldRecordRunDiaryIgnoresHeartbeatAndSystemSessions(t *testing.T) {
	if shouldRecordRunDiary(RunParams{SessionKey: "telegram:1", Message: "[시스템 하트비트] check", EphemeralUser: true}) {
		t.Fatal("heartbeat trigger should not be recorded to diary")
	}
	if shouldRecordRunDiary(RunParams{SessionKey: "system:diary-heartbeat", Message: "internal"}) {
		t.Fatal("system session should not be recorded to diary")
	}
	// Sync-only surface excluded by design: cron payload prompts (their "user"
	// message is a payload, not the user speaking).
	if shouldRecordRunDiary(RunParams{SessionKey: "cron:morning-letter", Message: "실질적인 내용이 있는 메시지"}) {
		t.Error("cron:morning-letter: should NOT record diary")
	}
	if !shouldRecordRunDiary(RunParams{SessionKey: "client:main:task:123", Message: "서브 대화도 일지에 남는다"}) {
		t.Error("client sub-conversation should record diary")
	}
	// Legacy chat: keys (retired 챗봇 workspace) are absorbed into 업무: a
	// continued old conversation records to the diary like any work session.
	if !shouldRecordRunDiary(RunParams{SessionKey: "chat:legacy-1", Message: "이어서 대화"}) {
		t.Error("legacy chat: session should record diary after the mode's removal")
	}
	if !shouldRecordRunDiary(RunParams{SessionKey: "telegram:1", Message: "기억 체계 개선 계속"}) {
		t.Fatal("normal user turn should be recorded")
	}
}

func readDiaryDir(t *testing.T, dir string) string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read diary dir: %v", err)
	}
	var sb strings.Builder
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatalf("read diary file: %v", err)
		}
		sb.Write(data)
	}
	return sb.String()
}
