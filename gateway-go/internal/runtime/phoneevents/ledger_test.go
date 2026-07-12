package phoneevents

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testLedgerLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLedgerNilAndDisabled(t *testing.T) {
	if l := NewLedger("", testLedgerLogger()); l != nil {
		t.Error("empty dir should disable the ledger")
	}
	var nilLedger *Ledger
	nilLedger.Append("notification", "kakao", "must not panic") // no-op
}

func TestLedgerAppendAndReadTail(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger(dir, testLedgerLogger())
	l.Append("notification", "카카오톡/업무방", "발주 다음 주로 밀렸어요")
	l.Append("sms", "010-1234", "회의 3시로 변경")

	tail, err := ReadLedgerTail(dir, nil, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 || tail.Truncated {
		t.Fatalf("tail=%+v", tail)
	}
	if tail.Entries[0].Text != "발주 다음 주로 밀렸어요" || tail.Entries[0].Source != "카카오톡/업무방" {
		t.Errorf("entry[0]=%+v", tail.Entries[0])
	}

	// Committing offsets consumes: a second read returns only new lines.
	l.Append("notification", "카카오톡/업무방", "추가 메시지")
	tail2, err := ReadLedgerTail(dir, tail.NextOffsets, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail2.Entries) != 1 || tail2.Entries[0].Text != "추가 메시지" {
		t.Fatalf("tail2=%+v", tail2.Entries)
	}
}

func TestLedgerReadTailBudgetStopsAtLineBoundary(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger(dir, testLedgerLogger())
	l.Append("notification", "a", strings.Repeat("가", 100))
	l.Append("notification", "b", strings.Repeat("나", 100))
	l.Append("notification", "c", strings.Repeat("다", 100))

	tail, err := ReadLedgerTail(dir, nil, 250) // fits 2 entries, not 3
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 || !tail.Truncated {
		t.Fatalf("entries=%d truncated=%v, want 2/true", len(tail.Entries), tail.Truncated)
	}
	// The committed offset must resume exactly at the third entry.
	rest, err := ReadLedgerTail(dir, tail.NextOffsets, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(rest.Entries) != 1 || rest.Entries[0].Source != "c" {
		t.Fatalf("rest=%+v", rest.Entries)
	}
}

func TestLedgerRedactsAndBounds(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger(dir, testLedgerLogger())
	l.Append("notification", "mail", "api key sk-proj-abcdefghijklmnopqrstuvwxyz012345 유출 주의")
	l.Append("notification", "big", strings.Repeat("x", ledgerMaxTextRunes+500))

	tail, err := ReadLedgerTail(dir, nil, 100_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(tail.Entries) != 2 {
		t.Fatalf("entries=%d", len(tail.Entries))
	}
	if strings.Contains(tail.Entries[0].Text, "sk-proj-abcdefghijklmnopqrstuvwxyz012345") {
		t.Error("secret persisted unredacted")
	}
	if n := len([]rune(tail.Entries[1].Text)); n > ledgerMaxTextRunes+20 {
		t.Errorf("oversize entry not bounded: %d runes", n)
	}
}

func TestLedgerPruneOldFiles(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().AddDate(0, 0, -(ledgerRetentionDays + 5)).Format("2006-01-02")
	if err := os.WriteFile(filepath.Join(dir, old+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLedger(dir, testLedgerLogger())
	l.Append("notification", "s", "t") // first append of the day triggers prune

	if _, err := os.Stat(filepath.Join(dir, old+".jsonl")); !os.IsNotExist(err) {
		t.Error("expired day file survived prune")
	}
}
