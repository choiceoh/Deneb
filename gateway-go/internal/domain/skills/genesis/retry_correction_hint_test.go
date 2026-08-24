package genesis

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeHintLedger persists a ledger the index can read.
func writeHintLedger(t *testing.T, dir string, records []RetryCorrectionRecord) {
	t.Helper()
	data, err := json.Marshal(retryLedger{Version: 1, Records: records})
	if err != nil {
		t.Fatalf("marshal ledger: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, retryLedgerFileName), data, 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
}

func hintRecord(tool, errHead, failed, success string, changed []string, at time.Time) RetryCorrectionRecord {
	return RetryCorrectionRecord{
		Tool:          tool,
		Signature:     retrySignature(tool, changed, errHead),
		ChangedFields: changed,
		ErrorHead:     errHead,
		FailedArgs:    failed,
		SuccessArgs:   success,
		AtMs:          at.UnixMilli(),
	}
}

const hintTestError = "gmail: label not found for query scope 42"

// gmailScopeFix is the recurring correction most cases below build on.
func gmailScopeFix(at time.Time) RetryCorrectionRecord {
	return hintRecord("gmail", hintTestError, `{"scope":"label"}`, `{"scope":"thread"}`, []string{"scope"}, at)
}

// A recurring correction must come back at error time, naming the changed
// field and both argument sides — the field name alone never says what to
// change it to.
func TestRetryHintIndexAdvisesRecurringCorrection(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeHintLedger(t, dir, []RetryCorrectionRecord{
		gmailScopeFix(now.Add(-48 * time.Hour)),
		gmailScopeFix(now.Add(-2 * time.Hour)),
	})

	got := NewRetryHintIndex(dir, nil).Advice("gmail", hintTestError+" (id 91)")
	if got == "" {
		t.Fatal("recurring correction produced no advice")
	}
	for _, want := range []string{"scope", `"thread"`, "2회"} {
		if !strings.Contains(got, want) {
			t.Errorf("advice %q missing %q", got, want)
		}
	}
}

// Silence obligations: a hint that fires on thin or unrelated evidence steers
// the retry wrongly, which is worse than no hint at all.
func TestRetryHintIndexStaysSilentWithoutMatchingEvidence(t *testing.T) {
	now := time.Now()
	stale := now.Add(-retryEvidenceTTL - 24*time.Hour)

	cases := []struct {
		name    string
		records []RetryCorrectionRecord
		tool    string
		errText string
	}{
		{
			name:    "single occurrence is situational, not a lesson",
			records: []RetryCorrectionRecord{gmailScopeFix(now.Add(-time.Hour))},
			tool:    "gmail", errText: hintTestError,
		},
		{
			name:    "another tool's correction never transfers",
			records: []RetryCorrectionRecord{gmailScopeFix(now.Add(-3 * time.Hour)), gmailScopeFix(now.Add(-time.Hour))},
			tool:    "web", errText: hintTestError,
		},
		{
			name:    "same tool, different failure mode",
			records: []RetryCorrectionRecord{gmailScopeFix(now.Add(-3 * time.Hour)), gmailScopeFix(now.Add(-time.Hour))},
			tool:    "gmail", errText: "gmail: rate limited, retry after 30s",
		},
		{
			name:    "evidence past the cluster TTL",
			records: []RetryCorrectionRecord{gmailScopeFix(stale), gmailScopeFix(stale)},
			tool:    "gmail", errText: hintTestError,
		},
		{
			name: "error head too short to identify a failure mode",
			records: []RetryCorrectionRecord{
				hintRecord("web", "timeout", `{"url":"a"}`, `{"url":"b"}`, []string{"url"}, now.Add(-2*time.Hour)),
				hintRecord("web", "timeout", `{"url":"a"}`, `{"url":"b"}`, []string{"url"}, now.Add(-time.Hour)),
			},
			tool: "web", errText: "timeout",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeHintLedger(t, dir, tc.records)
			if got := NewRetryHintIndex(dir, nil).Advice(tc.tool, tc.errText); got != "" {
				t.Errorf("advised %q, want silence", got)
			}
		})
	}
}

// Degraded inputs must no-op rather than fail: the hint is an optional extra
// on an already-failing tool call.
func TestRetryHintIndexInertOnMissingState(t *testing.T) {
	if got := NewRetryHintIndex(t.TempDir(), nil).Advice("gmail", hintTestError); got != "" {
		t.Errorf("missing ledger advised %q, want silence", got)
	}
	if got := NewRetryHintIndex("", nil).Advice("gmail", hintTestError); got != "" {
		t.Errorf("empty state dir advised %q, want silence", got)
	}
	var nilIdx *RetryHintIndex
	if got := nilIdx.Advice("gmail", hintTestError); got != "" {
		t.Errorf("nil index advised %q, want silence", got)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, retryLedgerFileName), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt ledger: %v", err)
	}
	if got := NewRetryHintIndex(dir, nil).Advice("gmail", hintTestError); got != "" {
		t.Errorf("corrupt ledger advised %q, want silence", got)
	}
}

// The sweep rewrites the ledger while the gateway runs; the index must pick
// that up without a restart.
func TestRetryHintIndexReloadsChangedLedger(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeHintLedger(t, dir, nil)
	idx := NewRetryHintIndex(dir, nil)
	if got := idx.Advice("gmail", hintTestError); got != "" {
		t.Fatalf("empty ledger advised %q", got)
	}

	writeHintLedger(t, dir, []RetryCorrectionRecord{
		gmailScopeFix(now.Add(-3 * time.Hour)),
		gmailScopeFix(now.Add(-time.Hour)),
	})
	// Force the stat window open; production reloads within hintReloadInterval.
	idx.mu.Lock()
	idx.checkedAt = time.Time{}
	idx.mu.Unlock()

	if got := idx.Advice("gmail", hintTestError); got == "" {
		t.Error("rewritten ledger produced no advice")
	}
}

// The ledger stores the tool_result CONTENT ("Error: …"), but the executor
// looks up with the raw error string. This fixture is copied from a real
// production record (gmail, support 2) — the shape that made every hint miss
// before stripToolErrorPrefix existed.
func TestRetryHintIndexMatchesLedgerErrorPrefixAgainstRawError(t *testing.T) {
	const stored = `Error: Gmail API 오류 (HTTP 400): { "error": { "code": 400, "message": "Invalid id value" } }`
	dir := t.TempDir()
	now := time.Now()
	rec := func(at time.Time) RetryCorrectionRecord {
		return hintRecord("gmail", stored, `{"message_id":"abc"}`, `{"message_id":"19a2f"}`, []string{"message_id"}, at)
	}
	writeHintLedger(t, dir, []RetryCorrectionRecord{rec(now.Add(-30 * time.Hour)), rec(now.Add(-2 * time.Hour))})

	raw := strings.TrimPrefix(stored, "Error: ")
	got := NewRetryHintIndex(dir, nil).Advice("gmail", raw)
	if got == "" {
		t.Fatalf("raw error %q found no advice for stored head %q", raw, stored)
	}
	if !strings.Contains(got, "message_id") {
		t.Errorf("advice %q lost the changed field", got)
	}
}
