package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

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
