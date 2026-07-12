package briefcase

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGrade_AllCheckTypesPass(t *testing.T) {
	root := t.TempDir()
	content := []byte("briefcase artifact\n")
	if err := os.WriteFile(filepath.Join(root, "report.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)

	plan := Plan{
		Fingerprint:   Fingerprint{RunID: "run-1", CaseID: "case-1", BuildSHA256: strings.Repeat("a", 64), Seed: 7},
		PassThreshold: 1,
		Checks: []Check{
			{ID: "exact", Type: CheckExactText, Critical: true, Weight: 2, ExpectedText: "Hello, Deneb"},
			{ID: "contains", Type: CheckContains, Weight: 1, Needle: "Deneb"},
			{ID: "forbidden", Type: CheckForbidden, Critical: true, Weight: 1, Needle: "secret"},
			{ID: "artifact", Type: CheckArtifact, Critical: true, Weight: 3, ArtifactPath: "report.txt", ExpectedSHA256: hex.EncodeToString(sum[:])},
			{ID: "state", Type: CheckStateJSONEqual, Weight: 3, ExpectedState: json.RawMessage(`{"count":1,"nested":{"ok":true}}`)},
		},
	}
	evidence := Evidence{
		Text:         "Hello, Deneb",
		ArtifactRoot: root,
		State:        json.RawMessage(`{"nested":{"ok":true},"count":1.0}`),
	}

	report := Grade(plan, evidence)
	if report.Status != StatusPass || report.Score != 1 || !report.CriticalPassed {
		t.Fatalf("report = %+v", report)
	}
	if report.PassedChecks != 5 || report.FailedChecks != 0 || report.InvalidChecks != 0 {
		t.Fatalf("check counts = pass %d fail %d invalid %d", report.PassedChecks, report.FailedChecks, report.InvalidChecks)
	}
	if report.WeightedPassed != 10 || report.WeightedTotal != 10 {
		t.Fatalf("weights = %v/%v", report.WeightedPassed, report.WeightedTotal)
	}
	if report.FingerprintSHA256 == "" || report.FingerprintSHA256 != plan.Fingerprint.Digest() {
		t.Fatalf("fingerprint digest = %q", report.FingerprintSHA256)
	}
}

func TestGrade_WeightedThresholdAndCriticalGate(t *testing.T) {
	base := Plan{
		PassThreshold: 0.75,
		Checks: []Check{
			{ID: "pass", Type: CheckContains, Weight: 3, Needle: "ok"},
			{ID: "fail", Type: CheckContains, Weight: 1, Needle: "missing"},
		},
	}
	report := Grade(base, Evidence{Text: "ok"})
	if report.Status != StatusPass || report.Score != 0.75 {
		t.Fatalf("weighted threshold report = %+v", report)
	}

	base.Checks[1].Critical = true
	report = Grade(base, Evidence{Text: "ok"})
	if report.Status != StatusFail || report.CriticalPassed {
		t.Fatalf("critical gate report = %+v", report)
	}
}

func TestGrade_DefaultThresholdIsStrict(t *testing.T) {
	report := Grade(Plan{Checks: []Check{
		{ID: "pass", Type: CheckContains, Weight: 1, Needle: "ok"},
		{ID: "fail", Type: CheckContains, Weight: 1, Needle: "missing"},
	}}, Evidence{Text: "ok"})
	if report.PassThreshold != 1 || report.Status != StatusFail || report.Score != 0.5 {
		t.Fatalf("report = %+v", report)
	}
}

func TestGradeContainsTokenRejectsNumericSupersets(t *testing.T) {
	plan := Plan{Checks: []Check{{ID: "budget", Type: CheckContainsToken, Weight: 1, Needle: "120"}}}
	for _, text := range []string{"120", "최신 예산은 120입니다.", "budget: $120."} {
		if report := Grade(plan, Evidence{Text: text}); report.Status != StatusPass {
			t.Errorf("text %q status = %s", text, report.Status)
		}
	}
	for _, text := range []string{
		"1200", "0120", "-120", "120.0", "120,000", "1,120", "0.120",
		"1，120", "0．120", "－120",
		"1 120", "1 120", "1'120", "1’120", "- 120", "− 120", "+ 120",
		"120e3", "120E+4", "0x120",
	} {
		if report := Grade(plan, Evidence{Text: text}); report.Status != StatusFail {
			t.Errorf("numeric superset %q status = %s", text, report.Status)
		}
	}
}

func TestGrade_InvalidInputsFailClosed(t *testing.T) {
	tests := []struct {
		name     string
		plan     Plan
		evidence Evidence
	}{
		{name: "no checks", plan: Plan{}},
		{name: "bad threshold", plan: Plan{PassThreshold: 1.1, Checks: []Check{{ID: "x", Type: CheckExactText, Weight: 1}}}},
		{name: "nan threshold", plan: Plan{PassThreshold: math.NaN(), Checks: []Check{{ID: "x", Type: CheckExactText, Weight: 1}}}},
		{name: "missing id", plan: Plan{Checks: []Check{{Type: CheckExactText, Weight: 1}}}},
		{name: "duplicate id", plan: Plan{Checks: []Check{{ID: "x", Type: CheckExactText, Weight: 1}, {ID: "x", Type: CheckExactText, Weight: 1}}}},
		{name: "zero weight", plan: Plan{Checks: []Check{{ID: "x", Type: CheckExactText}}}},
		{name: "nan weight", plan: Plan{Checks: []Check{{ID: "x", Type: CheckExactText, Weight: math.NaN()}}}},
		{name: "unknown type", plan: Plan{Checks: []Check{{ID: "x", Type: "llm", Weight: 1}}}},
		{name: "empty contains", plan: Plan{Checks: []Check{{ID: "x", Type: CheckContains, Weight: 1}}}},
		{name: "empty contains token", plan: Plan{Checks: []Check{{ID: "x", Type: CheckContainsToken, Weight: 1}}}},
		{name: "missing actual state", plan: Plan{Checks: []Check{{ID: "x", Type: CheckStateJSONEqual, Weight: 1, ExpectedState: json.RawMessage(`{}`)}}}},
		{name: "malformed expected state", plan: Plan{Checks: []Check{{ID: "x", Type: CheckStateJSONEqual, Weight: 1, ExpectedState: json.RawMessage(`{`)}}}, evidence: Evidence{State: json.RawMessage(`{}`)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Grade(tt.plan, tt.evidence)
			if report.Status != StatusInvalid {
				t.Fatalf("status = %s, report = %+v", report.Status, report)
			}
			if len(tt.plan.Checks) == 0 || tt.plan.PassThreshold > 1 || math.IsNaN(tt.plan.PassThreshold) {
				if len(report.Errors) == 0 {
					t.Fatalf("plan-level invalid report has no diagnostic: %+v", report)
				}
			}
			if _, err := json.Marshal(report); err != nil {
				t.Fatalf("invalid report must remain JSON-serializable: %v", err)
			}
		})
	}
}

func TestGradeRejectsCumulativeWeightOverflow(t *testing.T) {
	report := Grade(Plan{Checks: []Check{
		{ID: "one", Type: CheckExactText, Weight: 1e308, ExpectedText: "ok"},
		{ID: "two", Type: CheckExactText, Weight: 1e308, ExpectedText: "ok"},
		{ID: "miss", Type: CheckExactText, Weight: 1, ExpectedText: "no"},
	}}, Evidence{Text: "ok"})
	if report.Status != StatusInvalid || report.InvalidChecks == 0 {
		t.Fatalf("overflow report = %+v", report)
	}
	if _, err := json.Marshal(report); err != nil {
		t.Fatalf("overflow report must remain JSON-serializable: %v", err)
	}
}

func TestGradeContextKeepsCheckDiagnosticsInPlanOrder(t *testing.T) {
	plan := Plan{Checks: []Check{
		{ID: "first", Type: CheckExactText, Weight: 1, ExpectedText: "missing"},
		{ID: "first", Type: CheckExactText, Weight: 1, ExpectedText: "actual"},
		{ID: "third", Type: CheckExactText, Weight: 1, ExpectedText: "actual"},
	}}
	report, err := GradeContext(context.Background(), plan, Evidence{Text: "actual"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != StatusInvalid || report.PassedChecks != 1 || report.FailedChecks != 1 || report.InvalidChecks != 1 {
		t.Fatalf("report summary = %+v", report)
	}
	want := []struct {
		id     string
		status Status
		detail string
	}{
		{id: "first", status: StatusFail, detail: "text did not exactly match"},
		{id: "first", status: StatusInvalid, detail: "duplicate check id"},
		{id: "third", status: StatusPass, detail: "text exactly matched"},
	}
	for i, expected := range want {
		got := report.Checks[i]
		if got.ID != expected.id || got.Status != expected.status || got.Detail != expected.detail {
			t.Fatalf("check %d = %+v, want %+v", i, got, expected)
		}
	}
}

func TestGradeUsesExactWeightRatioForVerdict(t *testing.T) {
	report := Grade(Plan{PassThreshold: 1, Checks: []Check{
		{ID: "dominant", Type: CheckExactText, Weight: MaxCheckWeight, ExpectedText: "ok"},
		{ID: "tiny-miss", Type: CheckExactText, Weight: MinCheckWeight, ExpectedText: "no"},
	}}, Evidence{Text: "ok"})
	if report.Status != StatusFail || report.FailedChecks != 1 {
		t.Fatalf("scale-loss report = %+v", report)
	}
}

func TestGrade_ArtifactOutcomes(t *testing.T) {
	root := t.TempDir()
	content := []byte("actual")
	if err := os.WriteFile(filepath.Join(root, "actual.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256([]byte("expected"))
	digest := hex.EncodeToString(want[:])

	tests := []struct {
		name  string
		check Check
		want  Status
	}{
		{name: "missing", check: Check{ID: "x", Type: CheckArtifact, Weight: 1, ArtifactPath: "missing.txt", ExpectedSHA256: digest}, want: StatusFail},
		{name: "hash mismatch", check: Check{ID: "x", Type: CheckArtifact, Weight: 1, ArtifactPath: "actual.txt", ExpectedSHA256: digest}, want: StatusFail},
		{name: "bad digest", check: Check{ID: "x", Type: CheckArtifact, Weight: 1, ArtifactPath: "actual.txt", ExpectedSHA256: "abcd"}, want: StatusInvalid},
		{name: "path traversal", check: Check{ID: "x", Type: CheckArtifact, Weight: 1, ArtifactPath: "../actual.txt", ExpectedSHA256: digest}, want: StatusInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Grade(Plan{Checks: []Check{tt.check}}, Evidence{ArtifactRoot: root})
			if report.Status != tt.want {
				t.Fatalf("status = %s, report = %+v", report.Status, report)
			}
		})
	}

	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink test unavailable: %v", err)
	}
	outsideSum := sha256.Sum256([]byte("outside"))
	report := Grade(Plan{Checks: []Check{{
		ID: "symlink", Type: CheckArtifact, Weight: 1,
		ArtifactPath: "link.txt", ExpectedSHA256: hex.EncodeToString(outsideSum[:]),
	}}}, Evidence{ArtifactRoot: root})
	if report.Status != StatusInvalid {
		t.Fatalf("symlink artifact status = %s, report = %+v", report.Status, report)
	}
}

func TestGradeContextCancelsArtifactHashing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	reader := &cancelAfterRead{cancel: cancel}
	err := hashReaderContext(ctx, sha256.New(), reader)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("hash error = %v", err)
	}
}

type cancelAfterRead struct {
	cancel context.CancelFunc
	read   bool
}

func (r *cancelAfterRead) Read(p []byte) (int, error) {
	if r.read {
		return 0, io.EOF
	}
	r.read = true
	p[0] = 'x'
	r.cancel()
	return 1, nil
}

func TestGrade_StateJSONUsesSemanticEquality(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		actual   string
		want     Status
	}{
		{name: "key order and numeric spelling", expected: `{"n":1,"a":[true,null]}`, actual: `{"a":[true,null],"n":1.00}`, want: StatusPass},
		{name: "array order matters", expected: `[1,2]`, actual: `[2,1]`, want: StatusFail},
		{name: "trailing value invalid", expected: `{}`, actual: `{} {}`, want: StatusInvalid},
		{name: "duplicate key invalid", expected: `{"approved":true}`, actual: `{"approved":false,"approved":true}`, want: StatusInvalid},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			report := Grade(Plan{Checks: []Check{{
				ID: "state", Type: CheckStateJSONEqual, Weight: 1,
				ExpectedState: json.RawMessage(tt.expected),
			}}}, Evidence{State: json.RawMessage(tt.actual)})
			if report.Status != tt.want {
				t.Fatalf("status = %s, report = %+v", report.Status, report)
			}
		})
	}
}

func TestFingerprintDigestIsStableAndSensitive(t *testing.T) {
	f := Fingerprint{CaseID: "case-1", BuildSHA256: strings.Repeat("a", 64), Model: "model-a", Seed: 42}
	first, second := f.Digest(), f.Digest()
	if first == "" || first != second || len(first) != 64 {
		t.Fatalf("unstable digest: %q %q", first, second)
	}
	f.Model = "model-b"
	if f.Digest() == first {
		t.Fatal("digest did not change with fingerprint")
	}
}
