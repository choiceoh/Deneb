package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"
	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
	evalbriefcase "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase"
	closedloop "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase/closedloop"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat"
	runtimebriefcase "github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase"
)

func setRunProvenanceForTest(t *testing.T, result *runtimebriefcase.RunResult) {
	t.Helper()
	result.Sampling = runtimebriefcase.SamplingProfile{Temperature: 0, TopP: 1}
	promptDigests := make([]string, 0, len(result.Episodes))
	for _, episode := range result.Episodes {
		if episode.SystemPromptSHA256 != "" {
			promptDigests = append(promptDigests, episode.SystemPromptSHA256)
		}
	}
	var err error
	result.SystemPromptSequenceSHA256, err = runcontract.SystemPromptSequenceDigest(promptDigests)
	if err != nil {
		t.Fatal(err)
	}
	result.ExecutionProfileSHA256, err = runcontract.ExecutionProfileDigest(
		result.Model,
		result.APIMode,
		result.ToolSchemaSHA256,
		result.EndpointSHA256,
		result.BuildSHA256,
		result.Sampling,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestAuthorizeModelEndpointRejectsUnsafeEndpoints(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		mode        casepack.PrivacyMode
		allowRemote bool
		wantErr     bool
	}{
		{name: "portable loopback", url: "http://127.0.0.1:8000/v1", mode: casepack.PrivacyPortable},
		{name: "vault loopback disabled", url: "http://localhost:8000/v1", mode: casepack.PrivacyVault, wantErr: true},
		{name: "portable remote explicit", url: "https://models.example/v1", mode: casepack.PrivacyPortable, allowRemote: true},
		{name: "portable remote plaintext", url: "http://models.example/v1", mode: casepack.PrivacyPortable, allowRemote: true, wantErr: true},
		{name: "portable remote implicit", url: "https://models.example/v1", mode: casepack.PrivacyPortable, wantErr: true},
		{name: "vault remote always denied", url: "https://models.example/v1", mode: casepack.PrivacyVault, allowRemote: true, wantErr: true},
		{name: "credential in URL", url: "https://token@models.example/v1", mode: casepack.PrivacyPortable, allowRemote: true, wantErr: true},
		{name: "bad scheme", url: "file:///tmp/model", mode: casepack.PrivacyPortable, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := authorizeModelEndpoint(tt.url, tt.mode, tt.allowRemote)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDoctorCommandReturnsReadyStatusWithNetworkDenied(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"doctor", "--parent", t.TempDir()}, &stdout, &stderr); err != nil {
		t.Fatalf("doctor: %v (%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "READY"`) || !strings.Contains(stdout.String(), `"network": "DENIED"`) {
		t.Fatalf("doctor output = %s", stdout.String())
	}
}

func TestPortableSmokeCaseLoadsWithValidDigest(t *testing.T) {
	caseDir := portableSmokeCaseDir()
	pack, err := casepack.LoadDir(caseDir)
	if err != nil {
		t.Fatalf("load checked-in Portable case: %v", err)
	}
	if pack.Manifest.CaseID != "budget-supersession-v1" || pack.Digest == "" {
		t.Fatalf("portable case = %+v", pack.Manifest)
	}
}

func TestScoreCommandReturnsPassOrFailWhenAnswerChanges(t *testing.T) {
	dir := t.TempDir()
	caseDir := portableSmokeCaseDir()
	pack, err := casepack.LoadDir(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	result := runtimebriefcase.RunResult{
		SchemaVersion: "deneb-briefcase-run/v1", RunID: "score-test", CaseID: pack.Manifest.CaseID,
		CasepackSHA256: pack.Digest, Seed: pack.Manifest.Seed, Model: "test-model", ProviderModel: "served-test-model",
		APIMode: llm.APIModeOpenAI, SeedForwarded: true, RecallMode: "enabled",
		Arm: runtimebriefcase.ArmMemoryAssisted, ToolSchemaSHA256: strings.Repeat("1", 64), ArtifactRoot: dir,
		EndpointSHA256: strings.Repeat("2", 64), BuildSHA256: strings.Repeat("3", 64),
		ExecutionProfileSHA256: strings.Repeat("4", 64), SystemPromptSequenceSHA256: strings.Repeat("5", 64),
		Episodes: []runtimebriefcase.EpisodeResult{scoreTimelineEpisode(t, pack, "개정 메일이 이전 승인 예산 100을 대체했으며, Project Aurora의 최신 승인 예산은 120입니다.")},
		State:    json.RawMessage(`{"ok":true}`),
	}
	setRunProvenanceForTest(t, &result)
	runPath := filepath.Join(dir, "run.json")
	writeTestJSON(t, runPath, result)
	planPath := filepath.Join(caseDir, "sealed", "grader-plan.json")

	var stdout, stderr bytes.Buffer
	if err := run([]string{"score", "--case", caseDir, "--plan", planPath, "--run", runPath}, &stdout, &stderr); err != nil {
		t.Fatalf("score: %v (%s)", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"status": "PASS"`) {
		t.Fatalf("score output = %s", stdout.String())
	}

	result.Episodes[0].Text = "메일 개정에 따르면 120이 아니다."
	writeTestJSON(t, runPath, result)
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"score", "--case", caseDir, "--plan", planPath, "--run", runPath}, &stdout, &stderr); err == nil {
		t.Fatal("negated answer unexpectedly passed")
	}
	if !strings.Contains(stdout.String(), `"status": "FAIL"`) {
		t.Fatalf("negated answer score output = %s", stdout.String())
	}
}

func TestRunOutputPreservesArtifactRootAfterMove(t *testing.T) {
	const answer = "개정 메일이 이전 승인 예산 100을 대체했으며, Project Aurora의 최신 승인 예산은 120입니다."
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, cliTextSSE(answer))
	}))
	defer server.Close()

	original := t.TempDir()
	runPath := filepath.Join(original, "run.json")
	if err := run([]string{
		"run", "--case", portableSmokeCaseDir(), "--base-url", server.URL, "--model", "test-model",
		"--arm", string(runtimebriefcase.ArmMemoryAssisted), "--output", runPath,
	}, io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	var result runtimebriefcase.RunResult
	if err := decodeJSONFile(runPath, &result); err != nil {
		t.Fatal(err)
	}
	if result.ArtifactRoot != "run.json.artifacts" {
		t.Fatalf("portable artifactRoot = %q, want run.json.artifacts", result.ArtifactRoot)
	}

	moved := t.TempDir()
	movedRun := filepath.Join(moved, "run.json")
	if err := os.Rename(runPath, movedRun); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(runPath+".artifacts", filepath.Join(moved, "run.json.artifacts")); err != nil {
		t.Fatal(err)
	}
	planPath := filepath.Join(portableSmokeCaseDir(), "sealed", "grader-plan.json")
	if err := run([]string{
		"score", "--case", portableSmokeCaseDir(), "--plan", planPath, "--run", movedRun,
	}, io.Discard, io.Discard); err != nil {
		t.Fatalf("score moved portable bundle: %v", err)
	}

	result.ArtifactRoot = "../escape"
	if err := resolvePortableArtifactRoot(movedRun, &result); err == nil {
		t.Fatal("escaping relative artifactRoot was accepted")
	}
}

func TestClosedLoopCommand(t *testing.T) {
	caseDir := portableSmokeCaseDir()
	responses := []string{
		"공개 기록을 아직 충분히 확인하지 못했습니다.",
		"개정 메일이 이전 승인 예산 100을 대체했으며, Project Aurora의 최신 승인 예산은 120입니다.",
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		index := requests
		requests++
		if index >= len(responses) {
			index = len(responses) - 1
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, cliTextSSE(responses[index]))
	}))
	defer server.Close()

	var stdout, stderr bytes.Buffer
	err := run([]string{
		"loop",
		"--case", caseDir,
		"--base-url", server.URL,
		"--model", "test-model",
		"--arm", string(runtimebriefcase.ArmRawPrimary),
		"--supervisor-plan", filepath.Join(caseDir, "sealed", "supervisor-plan.json"),
		"--user-plan", filepath.Join(caseDir, "sealed", "user-simulator-plan.json"),
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("loop: %v (%s)", err, stderr.String())
	}
	var result closedloop.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode loop result: %v\n%s", err, stdout.String())
	}
	if result.Decision != evalbriefcase.SupervisorPass || result.Termination != closedloop.TerminationPass || len(result.Cycles) != 2 {
		t.Fatalf("loop result = %+v", result)
	}
	if requests != 2 || result.Cycles[0].Feedback == "" || len(result.Run.Episodes) != 2 {
		t.Fatalf("closed-loop execution: requests=%d cycles=%+v run=%+v", requests, result.Cycles, result.Run)
	}
}

func TestRunExportFailureWritesUnscoreablePartialAndRetainsEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, cliTextSSE("completed answer"))
	}))
	defer server.Close()
	existing := t.TempDir()
	var stdout, stderr bytes.Buffer
	err := run([]string{
		"run", "--case", portableSmokeCaseDir(), "--base-url", server.URL, "--model", "test-model",
		"--arm", string(runtimebriefcase.ArmRawPrimary), "--artifact-dir", existing,
	}, &stdout, &stderr)
	if err == nil {
		t.Fatal("existing artifact destination did not fail")
	}
	var partial partialRunOutput
	if decodeErr := json.Unmarshal(stdout.Bytes(), &partial); decodeErr != nil {
		t.Fatalf("decode partial: %v\n%s", decodeErr, stdout.String())
	}
	if partial.Complete || partial.SchemaVersion != "deneb-briefcase-partial/v1" || partial.Run == nil {
		t.Fatalf("partial = %+v", partial)
	}
	if !strings.Contains(stderr.String(), "retained failed run root") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	root := filepath.Dir(partial.Run.ArtifactRoot)
	if _, statErr := os.Stat(root); statErr != nil {
		t.Fatalf("retained root missing: %v", statErr)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
}

func TestRunAutoLoadsSignedDevicePlanAndScoringRequiresIt(t *testing.T) {
	caseDir, pack, planDigest := writeDevicePlanCase(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, cliTextSSE("completed answer"))
	}))
	defer server.Close()

	artifactDir := filepath.Join(t.TempDir(), "artifacts")
	var stdout, stderr bytes.Buffer
	if err := run([]string{
		"run", "--case", caseDir, "--base-url", server.URL, "--model", "test-model",
		"--arm", string(runtimebriefcase.ArmRawPrimary), "--artifact-dir", artifactDir,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("run with auto-loaded device plan: %v (%s)", err, stderr.String())
	}
	var result runtimebriefcase.RunResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode run: %v\n%s", err, stdout.String())
	}
	if result.DevicePlanSourceSHA256 == "" || result.DevicePlanSHA256 != planDigest {
		t.Fatalf("device plan provenance = source:%q plan:%q, want canonical %q", result.DevicePlanSourceSHA256, result.DevicePlanSHA256, planDigest)
	}
	if err := validateDevicePlanBinding(pack, result); err != nil {
		t.Fatalf("valid signed device plan binding: %v", err)
	}

	result.DevicePlanSHA256 = ""
	result.DevicePlanSourceSHA256 = ""
	if err := validateDevicePlanBinding(pack, result); err == nil || !strings.Contains(err.Error(), "source digest") {
		t.Fatalf("omitted device plan binding error = %v", err)
	}
}

func TestSupervisorPlanDigestCommandReturnsExpectedDigest(t *testing.T) {
	caseDir := portableSmokeCaseDir()
	var stdout, stderr bytes.Buffer
	if err := run([]string{
		"supervisor-plan-digest", "--plan", filepath.Join(caseDir, "sealed", "supervisor-plan.json"),
	}, &stdout, &stderr); err != nil {
		t.Fatalf("supervisor-plan-digest: %v (%s)", err, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "d22797a8d10e421e54838302099388f4897242402175a15201c4bd5946062af7" {
		t.Fatalf("supervisor plan digest = %q", stdout.String())
	}
}

func TestScoreRejectsUnboundRunAndPlan(t *testing.T) {
	caseDir := portableSmokeCaseDir()
	pack, err := casepack.LoadDir(caseDir)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	result := runtimebriefcase.RunResult{
		SchemaVersion: "deneb-briefcase-run/v1", RunID: "unbound-test", CaseID: pack.Manifest.CaseID,
		CasepackSHA256: strings.Repeat("0", 64), Seed: pack.Manifest.Seed,
		Model: "test-model", ProviderModel: "served-test-model", APIMode: llm.APIModeOpenAI, SeedForwarded: true, RecallMode: "disabled",
		Arm: runtimebriefcase.ArmRawPrimary, ToolSchemaSHA256: strings.Repeat("1", 64), ArtifactRoot: dir,
		EndpointSHA256: strings.Repeat("2", 64), BuildSHA256: strings.Repeat("3", 64),
		ExecutionProfileSHA256: strings.Repeat("4", 64), SystemPromptSequenceSHA256: strings.Repeat("5", 64),
		Episodes: []runtimebriefcase.EpisodeResult{scoreTimelineEpisode(t, pack, "120")}, State: json.RawMessage(`{}`),
	}
	setRunProvenanceForTest(t, &result)
	runPath := filepath.Join(dir, "run.json")
	writeTestJSON(t, runPath, result)
	planPath := filepath.Join(caseDir, "sealed", "grader-plan.json")
	if err := run([]string{"score", "--case", caseDir, "--plan", planPath, "--run", runPath}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("unbound run error = %v", err)
	}

	outsidePlan := filepath.Join(dir, "plan.json")
	writeTestJSON(t, outsidePlan, evalbriefcase.Plan{
		Fingerprint: evalbriefcase.Fingerprint{CaseID: pack.Manifest.CaseID},
		Checks:      []evalbriefcase.Check{{ID: "answer", Type: evalbriefcase.CheckContains, Weight: 1, Needle: "120"}},
	})
	result.CasepackSHA256 = pack.Digest
	writeTestJSON(t, runPath, result)
	if err := verifyRunBinding(pack, evalbriefcase.Fingerprint{
		CaseID: pack.Manifest.CaseID, Arm: string(runtimebriefcase.ArmMemoryAssisted),
	}, result); err == nil || !strings.Contains(err.Error(), "arm") {
		t.Fatalf("arm binding error = %v", err)
	}
	if err := run([]string{"score", "--case", caseDir, "--plan", outsidePlan, "--run", runPath}, io.Discard, io.Discard); err == nil || !strings.Contains(err.Error(), "sealed source") {
		t.Fatalf("unsealed plan error = %v", err)
	}
}

func TestValidateCompletedRunRejectsOverflowSizedEpisodeBudget(t *testing.T) {
	pack, err := casepack.LoadDir(portableSmokeCaseDir())
	if err != nil {
		t.Fatal(err)
	}
	episode := scoreTimelineEpisode(t, pack, "answer")
	episode.Turns = int(^uint(0) >> 1)
	episode.OutputTokens = int(^uint(0) >> 1)
	err = validateCompletedRun(pack, runtimebriefcase.RunResult{
		Arm: runtimebriefcase.ArmMemoryAssisted, Episodes: []runtimebriefcase.EpisodeResult{episode},
	})
	if err == nil || !strings.Contains(err.Error(), "signed cumulative model budget") {
		t.Fatalf("error = %v, want cumulative budget rejection", err)
	}
}

func TestValidateCompletedRunPreservesValidationOrder(t *testing.T) {
	pack, err := casepack.LoadDir(portableSmokeCaseDir())
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*runtimebriefcase.RunResult)
		want   string
	}{
		{
			name: "device binding precedes timeline size",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.DevicePlanSHA256 = strings.Repeat("a", 64)
				result.DevicePlanSourceSHA256 = strings.Repeat("b", 64)
				result.Episodes = nil
			},
			want: "run result declares a device plan that is absent from the signed casepack",
		},
		{
			name:   "partial timeline precedes episode validation",
			mutate: func(result *runtimebriefcase.RunResult) { result.Episodes = nil },
			want:   "run result is a partial timeline and cannot be scored",
		},
		{
			name: "follow-up count precedes timeline validation",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].EpisodeID = "wrong"
				result.Episodes = append(result.Episodes, runtimebriefcase.EpisodeResult{}, runtimebriefcase.EpisodeResult{}, runtimebriefcase.EpisodeResult{})
			},
			want: "run result exceeds the signed follow-up budget",
		},
		{
			name: "timeline identity precedes timestamp",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].EpisodeID = "wrong"
				result.Episodes[0].At = "bad"
			},
			want: `run result timeline episode 1 does not match signed episode "budget-update"`,
		},
		{
			name: "timeline timestamp precedes release outcome",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].At = "bad"
				result.Episodes[0].ReleasedSource = nil
			},
			want: `run result episode "budget-update" timestamp does not match the signed timeline`,
		},
		{
			name: "release outcome precedes budget accounting",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].ReleasedSource = nil
				result.Episodes[0].Turns = 0
			},
			want: `run result episode "budget-update" release outcome does not match the signed arm`,
		},
		{
			name: "budget accounting precedes input and provenance",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].Turns = 0
				result.Episodes[0].InputSHA256 = "bad"
				result.Episodes[0].Model = "wrong"
			},
			want: `run result episode "budget-update" has invalid budget accounting`,
		},
		{
			name: "input digest precedes executor provenance",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].InputSHA256 = "bad"
				result.Episodes[0].Model = "wrong"
			},
			want: `run result episode "budget-update" normalized input digest does not match`,
		},
		{
			name: "timeline provenance precedes follow-up validation",
			mutate: func(result *runtimebriefcase.RunResult) {
				result.Episodes[0].Model = "wrong"
				followUp := scoreFollowUpEpisode(t, pack, 1)
				followUp.EpisodeID = "wrong"
				result.Episodes = append(result.Episodes, followUp)
			},
			want: `run result episode "budget-update" executor provenance does not match the run`,
		},
		{
			name: "follow-up identity precedes timestamp",
			mutate: func(result *runtimebriefcase.RunResult) {
				followUp := scoreFollowUpEpisode(t, pack, 1)
				followUp.EpisodeID = "wrong"
				followUp.At = "bad"
				result.Episodes = append(result.Episodes, followUp)
			},
			want: "run result follow-up 1 has invalid phase or cycle",
		},
		{
			name: "follow-up timestamp precedes provenance",
			mutate: func(result *runtimebriefcase.RunResult) {
				followUp := scoreFollowUpEpisode(t, pack, 1)
				followUp.At = "bad"
				followUp.InputSHA256 = "bad"
				result.Episodes = append(result.Episodes, followUp)
			},
			want: "run result follow-up 1 has an invalid timestamp",
		},
		{
			name: "follow-up provenance precedes budget accounting",
			mutate: func(result *runtimebriefcase.RunResult) {
				followUp := scoreFollowUpEpisode(t, pack, 1)
				followUp.InputSHA256 = "bad"
				followUp.Turns = 0
				result.Episodes = append(result.Episodes, followUp)
			},
			want: "run result follow-up 1 provenance is invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runtimebriefcase.RunResult{
				Arm:   runtimebriefcase.ArmMemoryAssisted,
				Model: "test-model", ProviderModel: "served-test-model",
				Episodes: []runtimebriefcase.EpisodeResult{scoreTimelineEpisode(t, pack, "answer")},
			}
			tt.mutate(&result)
			err := validateCompletedRun(pack, result)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeJSONFileRejectsUnknownAndSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(path, []byte(`{"checks":[],"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var plan evalbriefcase.Plan
	if err := decodeJSONFile(path, &plan); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown-field error = %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"checks":[],"checks":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decodeJSONFile(path, &plan); err == nil || !strings.Contains(err.Error(), "duplicate JSON object key") {
		t.Fatalf("duplicate-field error = %v", err)
	}
	link := filepath.Join(dir, "link.json")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := decodeJSONFile(link, &plan); err == nil || !strings.Contains(err.Error(), "non-symlink") {
		t.Fatalf("symlink error = %v", err)
	}
}

func scoreTimelineEpisode(t *testing.T, pack *casepack.Pack, text string) runtimebriefcase.EpisodeResult {
	t.Helper()
	episode := pack.Manifest.Episodes[0]
	input, err := pack.ReadFile(episode.Input.Path)
	if err != nil {
		t.Fatal(err)
	}
	return runtimebriefcase.EpisodeResult{
		EpisodeID: episode.ID, At: episode.At.UTC().Format(time.RFC3339Nano), Phase: "timeline",
		InputSHA256:        casepack.DigestBytes([]byte(chat.SanitizeInput(string(input)))),
		SystemPromptSHA256: strings.Repeat("6", 64), Text: text,
		Model: "test-model", ProviderModel: "served-test-model", StopReason: "end_turn",
		Turns:          1,
		OutputTokens:   10,
		ReleasedSource: append([]string(nil), episode.ReleaseSourceIDs...),
	}
}

func scoreFollowUpEpisode(t *testing.T, pack *casepack.Pack, cycle int) runtimebriefcase.EpisodeResult {
	t.Helper()
	plan, err := loadSignedUserSimulatorPlan(pack)
	if err != nil {
		t.Fatal(err)
	}
	if cycle <= 0 || cycle > len(plan.FollowUps) {
		t.Fatalf("follow-up cycle %d outside plan of %d", cycle, len(plan.FollowUps))
	}
	message := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(plan.FollowUps[cycle-1].Message, "\r\n", "\n"), "\r", "\n"))
	lastAt := pack.Manifest.FrozenNow
	if episodes := pack.Manifest.Episodes; len(episodes) > 0 {
		lastAt = episodes[len(episodes)-1].At
	}
	return runtimebriefcase.EpisodeResult{
		EpisodeID: fmt.Sprintf("simulator-followup-%d", cycle),
		Phase:     "follow-up", Cycle: cycle, At: lastAt.UTC().Format(time.RFC3339Nano),
		InputSHA256:        casepack.DigestBytes([]byte(chat.SanitizeInput(message))),
		SystemPromptSHA256: strings.Repeat("6", 64),
		Text:               "follow-up answer",
		Model:              "test-model",
		ProviderModel:      "served-test-model",
		StopReason:         "end_turn",
		Turns:              1,
		OutputTokens:       10,
	}
}

func writeTestJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func portableSmokeCaseDir() string {
	return filepath.Join("..", "..", "..", "bench", "briefcase", "portable", "budget-supersession-v1")
}

func writeDevicePlanCase(t *testing.T) (string, *casepack.Pack, string) {
	t.Helper()
	sourceRoot := portableSmokeCaseDir()
	destination := t.TempDir()
	if err := filepath.Walk(sourceRoot, func(current string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceRoot, current)
		if err != nil || relative == "." {
			return err
		}
		target := filepath.Join(destination, relative)
		if info.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(current)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	}); err != nil {
		t.Fatal(err)
	}

	pack, err := casepack.LoadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	manifest := pack.Manifest
	planData := []byte(`{"plans":[{"actionId":"notify-1","kind":"notify","payload":{"text":"briefcase"},"status":"confirmed","result":{"receipt":"local"}}]}`)
	planPath := "sealed/device-plan.json"
	if err := os.WriteFile(filepath.Join(destination, filepath.FromSlash(planPath)), planData, 0o600); err != nil {
		t.Fatal(err)
	}
	manifest.Sources = append(manifest.Sources, casepack.Source{
		ID: "sealed-device-plan", Kind: casepack.SourceFile, Origin: casepack.SourceOriginSynthetic, Access: casepack.SourceAccessSealed,
		Path: planPath, SHA256: casepack.DigestBytes(planData), EventAt: manifest.FrozenNow, AvailableAt: manifest.FrozenNow,
		CapturedAt: manifest.FrozenNow, SourceRef: devicePlanSourceRef,
	})
	digest, err := casepack.CanonicalDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestDigest = digest
	writeTestJSON(t, filepath.Join(destination, casepack.ManifestFile), manifest)
	pack, err = casepack.LoadDir(destination)
	if err != nil {
		t.Fatal(err)
	}
	plans, err := runtimebriefcase.DecodeDevicePlanSource(planData)
	if err != nil {
		t.Fatal(err)
	}
	planDigest, err := runtimebriefcase.DevicePlansDigest(plans)
	if err != nil {
		t.Fatal(err)
	}
	return destination, pack, planDigest
}

func cliTextSSE(text string) string {
	encoded, _ := json.Marshal(text)
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-cli\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":null}]}\n\n"+
		"data: {\"id\":\"chatcmpl-cli\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
		"data: [DONE]\n\n", encoded)
}
