package chat

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agent"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestMaybeRecordRunDiary_SyncPathSideEffects pins the wiring shared by the
// async lifecycle and SendSync/SendSyncStream (the native chat path): an
// eligible turn records the diary — with leaked reasoning delimiters stripped
// — and fires the dream-turn trigger; an excluded session prefix does neither.
func TestMaybeRecordRunDiary_SyncPathSideEffects(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	dreamFired := make(chan struct{}, 2)
	deps := runDeps{
		wikiStore:   store,
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

func TestRecordDiaryIncludesOutcomeForShortPrompt(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if recorded := recordDiary(store, nil, "ㄱㄱ", []string{"read", "exec!"}, "구현 완료", "end_turn", 2); !recorded {
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

// TestClassifyDiarySignalTagsPreference pins the behavioral-signal capture: a user
// turn voicing a standing preference or style correction is tagged "선호" (and
// force-recorded as durable), so the dreamer aggregates an explicit cue for its
// working-style abstraction instead of inferring it from raw text.
func TestClassifyDiarySignalTagsPreference(t *testing.T) {
	cases := []struct {
		msg     string
		wantTag bool
	}{
		{"앞으로 답변은 간결하게 해줘", true},   // standing directive + style
		{"불릿 말고 산문으로 정리해줘", true},   // replacement + style correction
		{"그 표현 그만 쓰고 다르게", true},    // negation directive
		{"항상 숫자 근거를 같이 줘", true},    // standing directive
		{"현대차 울산 결제기한 언제야?", false}, // factual question, not a preference
		{"이 버그 고쳐줘", false},         // task (durable keyword, not a preference cue)
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

	if recorded := recordDiary(store, nil, "ㄱㄱ", nil, "", "end_turn", 1); recorded {
		t.Fatal("recordDiary returned true for short prompt without outcome")
	}
}

func TestRecordDiarySkipsTrivialBriefOutcome(t *testing.T) {
	dir := t.TempDir()
	store := testutil.Must(wiki.NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	defer store.Close()

	if recorded := recordDiary(store, nil, "고마워", nil, "천만에요", "end_turn", 1); recorded {
		t.Fatal("recordDiary returned true for trivial acknowledgement")
	}
}

func TestShouldRecordRunDiarySkipsHeartbeatAndSystemSessions(t *testing.T) {
	if shouldRecordRunDiary(RunParams{SessionKey: "telegram:1", Message: "[시스템 하트비트] check", EphemeralUser: true}) {
		t.Fatal("heartbeat trigger should not be recorded to diary")
	}
	if shouldRecordRunDiary(RunParams{SessionKey: "system:diary-heartbeat", Message: "internal"}) {
		t.Fatal("system session should not be recorded to diary")
	}
	// Sync-only surfaces excluded by design: 챗봇 (work-memory cut both ways),
	// coding worktrees (checkpoints are their history), cron payload prompts.
	for _, key := range []string{"chat:main", "code:task-0703-1", "cron:morning-letter"} {
		if shouldRecordRunDiary(RunParams{SessionKey: key, Message: "실질적인 내용이 있는 메시지"}) {
			t.Errorf("%s: should NOT record diary", key)
		}
	}
	if !shouldRecordRunDiary(RunParams{SessionKey: "client:main:task:123", Message: "서브 대화도 일지에 남는다"}) {
		t.Error("client sub-conversation should record diary")
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
