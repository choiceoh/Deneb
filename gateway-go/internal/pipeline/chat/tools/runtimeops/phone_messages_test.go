package runtimeops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func seedLedger(t *testing.T, day time.Time, entries ...string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DENEB_STATE_DIR", dir)
	events := filepath.Join(dir, "phone-events")
	if err := os.MkdirAll(events, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(events, day.Format("2006-01-02")+".jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(entries, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	return dir
}

func entry(ts, source, text string) string {
	b, _ := json.Marshal(phoneLedgerEntry{TS: ts, Type: "notification", Source: source, Text: text})
	return string(b)
}

func TestPhoneMessages_GroupsKakaoRoomsAndKeepsEachMessage(t *testing.T) {
	now := time.Now()
	stamp := now.Format("2006-01-02") + "T13:32:49+09:00"
	seedLedger(t, now,
		entry(stamp, "카카오톡", "대화방: 선향란 과장\n메시지:\n- 선향란 과장: 네. 진행하겠습니다.\n- 선향란 과장: 자료는 내일 드리겠습니다."),
	)
	threads := readPhoneMessageThreads(now, 1)
	if len(threads) != 1 || threads[0].room != "선향란 과장" {
		t.Fatalf("room grouping failed: %+v", threads)
	}
	// Each `- ` line is a real separate message and must survive as one.
	if len(threads[0].lines) != 2 {
		t.Fatalf("want 2 messages, got %d: %v", len(threads[0].lines), threads[0].lines)
	}
}

func TestPhoneMessages_CumulativeSmsStaysOneEntry(t *testing.T) {
	now := time.Now()
	stamp := now.Format("2006-01-02") + "T14:59:00+09:00"
	// A card-approval SMS re-carries the whole thread on every update. Splitting
	// it by newline turned one alert into six "messages" and crowded a real
	// conversation out of the digest.
	seedLedger(t, now,
		entry(stamp, "메시지", "1588-8900\n1588-8900: [Web발신]\n삼성6483승인 오*택\n1,400원 일시불\n07/26 14:58 전남대학교소\n누적11,328,099원"),
	)
	threads := readPhoneMessageThreads(now, 1)
	if len(threads) != 1 {
		t.Fatalf("want 1 room, got %d", len(threads))
	}
	if len(threads[0].lines) != 1 {
		t.Fatalf("cumulative SMS must stay one entry, got %d: %v", len(threads[0].lines), threads[0].lines)
	}
	if !strings.Contains(threads[0].lines[0], "1,400원") {
		t.Errorf("content lost: %q", threads[0].lines[0])
	}
}

func TestPhoneMessages_MostRecentRoomFirstAndCapped(t *testing.T) {
	now := time.Now()
	day := now.Format("2006-01-02")
	var rows []string
	for i := range phoneMessagesRooms + 4 {
		rows = append(rows, entry(fmt.Sprintf("%sT%02d:00:00+09:00", day, i%24), "카카오톡",
			fmt.Sprintf("대화방: 방%02d\n메시지:\n- 사람: 안녕 %d", i, i)))
	}
	seedLedger(t, now, rows...)
	threads := readPhoneMessageThreads(now, 1)
	if len(threads) > phoneMessagesRooms {
		t.Fatalf("room cap ignored: %d", len(threads))
	}
	for i := 1; i < len(threads); i++ {
		if threads[i-1].last.Before(threads[i].last) {
			t.Fatalf("threads not newest-first at %d", i)
		}
	}
}

func TestPhoneRead_MessagesServesTheLedger(t *testing.T) {
	now := time.Now()
	stamp := now.Format("2006-01-02") + "T09:00:00+09:00"
	seedLedger(t, now, entry(stamp, "카카오톡", "대화방: 고건 주임\n메시지:\n- 고건 주임: 모듈 입고 일정 확인 부탁드립니다."))
	out, err := ToolPhoneRead(nil)(context.Background(), json.RawMessage(`{"what":"messages"}`))
	if err != nil {
		t.Fatalf("messages read: %v", err)
	}
	for _, want := range []string{"고건 주임", "모듈 입고"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
}

func TestPhoneMessages_EmptyLedgerIsNotAnError(t *testing.T) {
	seedLedger(t, time.Now())
	out, err := ToolPhoneRead(nil)(context.Background(), json.RawMessage(`{"what":"talk"}`))
	if err != nil {
		t.Fatalf("empty ledger must not error: %v", err)
	}
	if !strings.Contains(out, "없습니다") {
		t.Errorf("expected an empty-state message, got %q", out)
	}
}
