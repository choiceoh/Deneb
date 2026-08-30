package recall

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	wiki "github.com/choiceoh/deneb/gateway-go/internal/domain/wikiport"
)

// The demand ledger answers "what did the USER ask that memory could not".
// A machine-originated turn carries an injected template, not a question, and
// counting it lets the system read its own scaffolding back as user demand.
//
// Live case, 2026-08-30: the ledger held exactly one line — a phone-event
// prompt on session "phone-event:e_0000" — and that week's memory digest
// reported "기억 공백(자주 물은 주제): 실시간, 스마트폰, 이벤트, 알림, 출처",
// which are the words of the template header.
func TestRecordRecallMissIsNotWrittenForMachineTurns(t *testing.T) {
	phoneEventPrompt := "[실시간 스마트폰 이벤트 — 앱 알림]\n출처: 갤러리 스토리\n" +
		"내용:\n새 스토리\n\n위는 사용자 스마트폰에서 방금 발생한 이벤트다."

	t.Run("사람 턴은 기록된다", func(t *testing.T) {
		store, dir := demandStore(t)
		recordRecallMissIfHuman(store, false, "client:main", "새만금 지체상금 어떻게 됐지", nil)
		if n := demandLines(t, dir); n != 1 {
			t.Fatalf("human cue recorded %d lines, want 1", n)
		}
	})

	t.Run("기계 턴은 기록되지 않는다", func(t *testing.T) {
		store, dir := demandStore(t)
		recordRecallMissIfHuman(store, true, "phone-event:e_0000", phoneEventPrompt, nil)
		if n := demandLines(t, dir); n != 0 {
			t.Fatalf("machine turn recorded %d lines, want 0", n)
		}
	})

	// The point is attribution, not censorship: the same text typed by a person
	// is real demand and must count.
	t.Run("같은 문구도 사람이 물으면 수요다", func(t *testing.T) {
		store, dir := demandStore(t)
		recordRecallMissIfHuman(store, false, "client:main", phoneEventPrompt, nil)
		if n := demandLines(t, dir); n != 1 {
			t.Fatalf("human turn recorded %d lines, want 1", n)
		}
	})
}

// A machine turn must still get recall — only the demand count is withheld.
func TestMachineTurnStillGetsRecall(t *testing.T) {
	store, _ := demandStore(t)
	page := wiki.NewPage("새만금 태양광", "프로젝트", nil)
	page.Body = "# 새만금 태양광\n\n지체상금 협의 진행 중.\n"
	if err := store.WritePage("프로젝트/새만금/대표.md", page); err != nil {
		t.Fatal(err)
	}
	res, err := store.Search(context.Background(), "새만금 지체상금", 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("recall must still work for the turn whose demand we do not count")
	}
}

func demandStore(t *testing.T) (*wiki.Store, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := wiki.NewStore(dir, filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("wiki store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dir
}

func demandLines(t *testing.T, dir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".recall-misses.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0
		}
		t.Fatalf("read ledger: %v", err)
	}
	n := 0
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(ln) != "" {
			n++
		}
	}
	_ = time.Now
	return n
}
