package anomalywatch

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// TestKeepGroundedRejectsInventedEvidence is the gate the whole ledger rests
// on: a finding whose quote is not in the window is a finding the model wrote,
// and it costs the reader exactly the investigation the ledger exists to save.
func TestKeepGroundedRejectsInventedEvidence(t *testing.T) {
	window := "[ERROR]×4 mail: LMTP 배달 실패 | code=451\n[WARN] wiki: 인덱스 재구축 지연\n"
	findings := []Finding{
		{Severity: "high", Summary: "메일 배달 반복 실패", Evidence: "mail: LMTP 배달 실패"},
		{Severity: "high", Summary: "임베더가 죽었다", Evidence: "embedder: connection refused on :8002"},
	}
	kept := keepGrounded(findings, window)
	if len(kept) != 1 {
		t.Fatalf("kept %d findings, want only the grounded one: %+v", len(kept), kept)
	}
	if kept[0].Summary != "메일 배달 반복 실패" {
		t.Errorf("kept the wrong finding: %+v", kept[0])
	}
}

// TestKeepGroundedDropsEvidencelessAndUnlabeled: a finding with no quote or no
// summary is not a finding, and an unlabeled severity reads DOWN.
func TestKeepGroundedDropsEvidencelessAndUnlabeled(t *testing.T) {
	window := "[WARN] gateway: 무언가 이상함\n"
	kept := keepGrounded([]Finding{
		{Summary: "근거 없음", Evidence: ""},
		{Summary: "", Evidence: "gateway: 무언가 이상함"},
		{Summary: "라벨 없음", Evidence: "gateway: 무언가 이상함", Severity: "확실하지 않음"},
	}, window)
	if len(kept) != 1 {
		t.Fatalf("kept %d, want 1: %+v", len(kept), kept)
	}
	if kept[0].Severity != "low" {
		t.Errorf("unknown severity = %q, want low — an unlabeled finding is not an urgent one", kept[0].Severity)
	}
}

// TestInspectRecordsGapRatherThanSilence: a model outage must arrive in the
// ledger as a gap, never as a clean pass. The two mean opposite things.
func TestInspectRecordsGapRatherThanSilence(t *testing.T) {
	d := Digest{Text: "[ERROR] something\n"}
	failing := func(context.Context, string, string, int) (string, error) {
		return "", errors.New("connection refused")
	}
	findings, gap := Inspect(context.Background(), failing, d)
	if len(findings) != 0 {
		t.Errorf("a failed call must yield no findings, got %+v", findings)
	}
	if gap == "" {
		t.Fatal("a failed call must record a gap — a silent clean pass would claim the runtime was checked")
	}
	if _, nilGap := Inspect(context.Background(), nil, d); nilGap == "" {
		t.Error("an unwired judge must also record a gap")
	}
}

// TestParseFindingsToleratesFencedReply pins the tolerance every local model
// eventually needs, regardless of instruction.
func TestParseFindingsToleratesFencedReply(t *testing.T) {
	reply := "판정 결과입니다:\n```json\n{\"findings\":[{\"severity\":\"medium\",\"summary\":\"s\",\"evidence\":\"e\"}]}\n```\n"
	got, err := parseFindings(reply)
	if err != nil {
		t.Fatalf("parseFindings: %v", err)
	}
	if len(got) != 1 || got[0].Summary != "s" {
		t.Errorf("parsed = %+v", got)
	}
}

// TestBuildDigestGroupsRepeatsRatherThanListing: recurrence is the strongest
// signal in the window, and a flat dump destroys it.
func TestBuildDigestGroupsRepeatsRatherThanListing(t *testing.T) {
	var lines []observe.LogLine
	for i := 0; i < 5; i++ {
		lines = append(lines, observe.LogLine{Level: "error", Msg: "mail: LMTP 배달 실패"})
	}
	lines = append(lines, observe.LogLine{Level: "warn", Msg: "wiki: 인덱스 지연"})

	d := BuildDigest(lines)
	if d.Examined.LogLines != 6 {
		t.Errorf("examined lines = %d, want 6", d.Examined.LogLines)
	}
	if d.Examined.DistinctMessages != 2 {
		t.Errorf("distinct = %d, want 2 — 6 lines of 2 messages is not 6 anomalies", d.Examined.DistinctMessages)
	}
	if !strings.Contains(d.Text, "×5") {
		t.Errorf("repeat count missing from digest:\n%s", d.Text)
	}
	// The loud one leads, so a truncated read still meets it.
	if !strings.HasPrefix(strings.TrimSpace(d.Text), "[ERROR]×5") {
		t.Errorf("loudest group must lead:\n%s", d.Text)
	}
}

// TestBuildDigestSaysEmptyWindowsAreEmpty: a window with nothing in it is a
// real reading, and the model must be told so rather than left to explain
// silence it was handed.
func TestBuildDigestSaysEmptyWindowsAreEmpty(t *testing.T) {
	d := BuildDigest(nil)
	if d.Text == "" {
		t.Fatal("an empty window must still produce a statement, not an empty prompt")
	}
	if d.Examined.LogLines != 0 {
		t.Errorf("examined = %d, want 0", d.Examined.LogLines)
	}
}

// TestLedgerRecordsCleanPassesToo: without the clean passes, "the window was
// clean" and "the watcher never ran" are the same empty ledger.
func TestLedgerRecordsCleanPassesToo(t *testing.T) {
	dir := t.TempDir()
	clean := Entry{At: "2026-08-26T01:00:00Z", Examined: Examined{LogLines: 12}}
	if err := Append(dir, clean, 10); err != nil {
		t.Fatalf("Append: %v", err)
	}
	got, err := Read(dir, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("read %d entries, want 1", len(got))
	}
	if got[0].Examined.LogLines != 12 {
		t.Error("a clean pass must still record what it examined — zero findings over zero lines is not a healthy runtime")
	}
}

// TestLedgerTrimKeepsNewest: an always-on lane must not fill the disk, and it
// must drop the OLD end.
func TestLedgerTrimKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 8; i++ {
		e := Entry{At: time.Date(2026, 8, 26, i, 0, 0, 0, time.UTC).Format(time.RFC3339)}
		if err := Append(dir, e, 3); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := Read(dir, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("kept %d, want 3", len(got))
	}
	if got[0].At != "2026-08-26T07:00:00Z" {
		t.Errorf("newest kept = %q, want the last written", got[0].At)
	}
}

// TestReadSkipsCorruptLine: a half-written tail must not hide every pass
// before it.
func TestReadSkipsCorruptLine(t *testing.T) {
	dir := t.TempDir()
	if err := Append(dir, Entry{At: "2026-08-26T01:00:00Z"}, 0); err != nil {
		t.Fatalf("Append: %v", err)
	}
	f, err := openAppend(LedgerPath(dir))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, _ = f.WriteString("{truncated\n")
	_ = f.Close()

	got, err := Read(dir, 0)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("read %d entries, want the 1 intact pass", len(got))
	}
}

// TestSinceReturnsOnlyNewerPasses pins the read shape a reader actually wants.
func TestSinceReturnsOnlyNewerPasses(t *testing.T) {
	dir := t.TempDir()
	for _, at := range []string{"2026-08-25T01:00:00Z", "2026-08-26T01:00:00Z"} {
		if err := Append(dir, Entry{At: at}, 0); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got, err := Since(dir, time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	if len(got) != 1 || got[0].At != "2026-08-26T01:00:00Z" {
		t.Errorf("Since = %+v, want only the newer pass", got)
	}
}
