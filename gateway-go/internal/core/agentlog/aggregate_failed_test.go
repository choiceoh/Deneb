package agentlog

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeSessionLog writes raw LogEntry lines for one session file — the JSONL
// wire contract is the fixture, not the Writer's append path.
func writeSessionLog(t *testing.T, dir, session string, entries []LogEntry) {
	t.Helper()
	var buf []byte
	for _, e := range entries {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
	}
	if err := os.WriteFile(filepath.Join(dir, session+".jsonl"), buf, 0o644); err != nil {
		t.Fatal(err)
	}
}

func startEntry(t *testing.T, session, runID, message string, ts int64) LogEntry {
	t.Helper()
	data, err := json.Marshal(RunStartData{Model: "m", Message: message})
	if err != nil {
		t.Fatal(err)
	}
	return LogEntry{Ts: ts, Type: TypeRunStart, RunID: runID, Session: session, Data: data}
}

func errorEntry(t *testing.T, session, runID, msg string, ts int64) LogEntry {
	t.Helper()
	data, err := json.Marshal(RunErrorData{Error: msg})
	if err != nil {
		t.Fatal(err)
	}
	return LogEntry{Ts: ts, Type: TypeRunError, RunID: runID, Session: session, Data: data}
}

// FailedUserRequests joins run.error to its run.start message within real
// client sessions only: live-test synthetic and system sessions are excluded,
// window and cap are honored, retries dedup to the newest, and an error with
// no joinable message is dropped (unquotable = unusable demand evidence).
func TestFailedUserRequests(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)

	writeSessionLog(t, dir, "client:main", []LogEntry{
		startEntry(t, "client:main", "r1", "위키에서 발주서 초안 뽑아줘", 100),
		errorEntry(t, "client:main", "r1", "stream stall", 110),
		startEntry(t, "client:main", "r2", "위키에서 발주서 초안 뽑아줘", 200), // retry of the same ask
		errorEntry(t, "client:main", "r2", "stream stall again", 210),
		startEntry(t, "client:main", "r3", "지난달 매출 정리해줘", 300),
		errorEntry(t, "client:main", "r3", "tool crashed", 310),
		errorEntry(t, "client:main", "orphan", "no start recorded", 320), // unjoinable → dropped
		startEntry(t, "client:main", "r4", "오래된 요청", 10),
		errorEntry(t, "client:main", "r4", "ancient failure", 20), // before window → dropped
	})
	writeSessionLog(t, dir, "client:lt-123", []LogEntry{ // live-test synthetic → excluded
		startEntry(t, "client:lt-123", "r1", "smoke ping", 300),
		errorEntry(t, "client:lt-123", "r1", "synthetic", 310),
	})
	writeSessionLog(t, dir, "system:background", []LogEntry{ // not client:* → excluded
		startEntry(t, "system:background", "r1", "cron ask", 300),
		errorEntry(t, "system:background", "r1", "cron fail", 310),
	})

	got := w.FailedUserRequests(50, 10)
	if len(got) != 2 {
		t.Fatalf("failed requests = %+v, want 2 (dedup + filters)", got)
	}
	if got[0].Message != "지난달 매출 정리해줘" || got[0].Error != "tool crashed" {
		t.Fatalf("newest-first order wrong: %+v", got[0])
	}
	if got[1].Message != "위키에서 발주서 초안 뽑아줘" || got[1].Ts != 210 {
		t.Fatalf("dedup must keep the newest retry: %+v", got[1])
	}

	if capped := w.FailedUserRequests(50, 1); len(capped) != 1 {
		t.Fatalf("cap not honored: %+v", capped)
	}
	if none := w.FailedUserRequests(1000, 10); len(none) != 0 {
		t.Fatalf("window not honored: %+v", none)
	}
	var nilWriter *Writer
	if r := nilWriter.FailedUserRequests(0, 5); r != nil {
		t.Fatal("nil writer must yield nil")
	}
}
