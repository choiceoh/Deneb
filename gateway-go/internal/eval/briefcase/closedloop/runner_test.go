package closedloop

import (
	"context"
	"encoding/json"
	"errors"
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
	feedbackcontract "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/feedback"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase/runcontract"
	evalbriefcase "github.com/choiceoh/deneb/gateway-go/internal/eval/briefcase"
	runtimebriefcase "github.com/choiceoh/deneb/gateway-go/internal/runtime/briefcase"
)

var _ Executor = (*runtimebriefcase.ChatHarness)(nil)

func TestRunnerExecutesThreeRoleClosedLoop(t *testing.T) {
	fixture := newLoopFixture(t, []string{
		"I found a draft but have not verified the latest value.",
		"I checked the update. The approved budget is 120.",
	}, "Please re-check the public records and finish the answer.")
	defer fixture.close()

	result, err := fixture.runner.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Decision != evalbriefcase.SupervisorPass || result.Termination != TerminationPass {
		t.Fatalf("loop verdict = %q/%q", result.Decision, result.Termination)
	}
	if len(result.Cycles) != 2 || result.BestCycle != 2 || result.BestScore != 1 {
		t.Fatalf("loop cycles/best = %+v cycle=%d score=%v", result.Cycles, result.BestCycle, result.BestScore)
	}
	first := result.Cycles[0]
	if first.Supervisor.Decision != evalbriefcase.SupervisorContinue || first.Feedback == "" || first.FeedbackSHA256 == "" || len(first.Handoff) == 0 {
		t.Fatalf("first cycle = %+v", first)
	}
	if strings.Contains(string(first.Handoff), "120") || strings.Contains(string(first.Handoff), "latest-budget") {
		t.Fatalf("coarse handoff leaked hidden rubric: %s", first.Handoff)
	}
	if len(result.Run.Episodes) != 2 || result.Run.Episodes[1].Phase != "follow-up" || result.Run.Episodes[1].Cycle != 1 {
		t.Fatalf("continued run = %+v", result.Run.Episodes)
	}
	if len(result.SupervisorAudit.Cycles) != 2 || result.SupervisorAudit.Cycles[1].Report.Status != evalbriefcase.StatusPass {
		t.Fatalf("supervisor audit = %+v", result.SupervisorAudit)
	}
	if len(fixture.requests) != 2 || !strings.Contains(fixture.requests[1], first.Feedback) {
		t.Fatalf("executor requests = %v", fixture.requests)
	}
	if _, err := fixture.runner.Run(t.Context()); !errors.Is(err, ErrInvalidClosedLoop) {
		t.Fatalf("second loop run error = %v", err)
	}
}

func TestRunnerRejectsScriptedSimulatorAnswerLeakBeforeExecutor(t *testing.T) {
	fixture, err := newLoopFixtureAllowRunnerError(t, []string{
		"I found a draft but have not verified the latest value.",
	}, "Use 120 as the answer.")
	defer fixture.close()
	if !errors.Is(err, feedbackcontract.ErrFeedbackLeak) {
		t.Fatalf("error = %v, want ErrFeedbackLeak", err)
	}
	if fixture.runner != nil || len(fixture.requests) != 0 {
		t.Fatalf("leaking feedback reached runner/executor: runner=%v requests=%d", fixture.runner != nil, len(fixture.requests))
	}
	if strings.Contains(strings.ToLower(err.Error()), "120") {
		t.Fatalf("rejected answer leaked into error: %v", err)
	}
}

func TestRunnerPreservesLastCompletedRunWhenFollowUpExecutorFails(t *testing.T) {
	fixture := newLoopFixture(t, []string{
		"I found a draft but have not verified the latest value.",
		"__HTTP_400__",
	}, "Please re-check the public records.", func(fixture *loopFixture, index int) error {
		if index != 1 {
			return nil
		}
		paths, err := fixture.root.Paths()
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(paths.Workspace, "output", "report.txt"), []byte("mutated-after-best"), 0o600)
	})
	defer fixture.close()
	paths, err := fixture.root.Paths()
	if err != nil {
		t.Fatal(err)
	}
	liveArtifact := filepath.Join(paths.Workspace, "output", "report.txt")
	if err := os.WriteFile(liveArtifact, []byte("best-cycle"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := fixture.runner.Run(t.Context())
	if err == nil {
		t.Fatal("executor failure was returned as success")
	}
	if result == nil || result.Termination != TerminationExecutorError || result.Decision != evalbriefcase.SupervisorFail {
		t.Fatalf("partial result = %+v, err=%v", result, err)
	}
	if result.Run == nil || len(result.Run.Episodes) != 1 || result.Run.Episodes[0].Text == "" {
		t.Fatalf("last completed run was lost: %+v", result.Run)
	}
	if len(result.Cycles) != 1 || result.Cycles[0].Supervisor.Decision != evalbriefcase.SupervisorContinue {
		t.Fatalf("completed supervisor cycle was lost: %+v", result.Cycles)
	}
	if result.BestRun == nil || result.BestCycle != 1 || len(result.BestRun.Episodes) != 1 {
		t.Fatalf("best completed run was lost: bestRun=%+v bestCycle=%d", result.BestRun, result.BestCycle)
	}
	if filepath.Clean(result.BestRun.ArtifactRoot) == filepath.Clean(paths.Workspace) {
		t.Fatalf("best run points at mutable workspace: %q", result.BestRun.ArtifactRoot)
	}
	bestArtifact := filepath.Join(result.BestRun.ArtifactRoot, "output", "report.txt")
	if data, readErr := os.ReadFile(bestArtifact); readErr != nil || string(data) != "best-cycle" {
		t.Fatalf("best-run artifact = %q, %v; want isolated cycle bytes", data, readErr)
	}
	if data, readErr := os.ReadFile(liveArtifact); readErr != nil || string(data) != "mutated-after-best" {
		t.Fatalf("live artifact mutation = %q, %v; failure hook did not run", data, readErr)
	}
}

func TestTerminationDistinguishesTurnTimeoutGlobalTimeoutAndCancellation(t *testing.T) {
	if got := terminationForExecutorError(context.Background(), context.DeadlineExceeded); got != TerminationTurnTimeout {
		t.Fatalf("local deadline termination = %q", got)
	}
	global, cancelGlobal := context.WithTimeout(context.Background(), 0)
	defer cancelGlobal()
	if got := terminationForExecutorError(global, context.DeadlineExceeded); got != TerminationGlobalTimeout {
		t.Fatalf("global deadline termination = %q", got)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if got := terminationForExecutorError(canceled, context.Canceled); got != TerminationCanceled {
		t.Fatalf("caller cancellation termination = %q", got)
	}
}

func TestRunnerRejectsSupervisorBytesThatDoNotMatchSignedSource(t *testing.T) {
	fixture := newLoopFixture(t, []string{"done"}, "Try again.")
	defer fixture.close()
	tampered := append([]byte(nil), fixture.supervisorSource...)
	tampered[len(tampered)/2] ^= 1
	if _, err := New(Config{
		Pack: fixture.pack, Executor: fixture.harness,
		SupervisorPlanSource:       tampered,
		SupervisorPlanSourceSHA256: fixture.supervisorSourceSHA256,
	}); !errors.Is(err, ErrInvalidClosedLoop) || !strings.Contains(err.Error(), "do not match") {
		t.Fatalf("tampered source error = %v", err)
	}
}

func TestRunnerRejectsExecutorFromDifferentCasepackDigest(t *testing.T) {
	fixture := newLoopFixture(t, []string{"done"}, "Try again.")
	defer fixture.close()
	otherPack, _, _, supervisorSHA, userSHA := writeLoopCase(t, "A different signed follow-up.")
	supervisorSource, err := otherPack.ReadFile("sealed/supervisor-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	userSource, err := otherPack.ReadFile("sealed/user-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{
		Pack: otherPack, Executor: fixture.harness,
		SupervisorPlanSource:          supervisorSource,
		SupervisorPlanSourceSHA256:    supervisorSHA,
		UserSimulatorPlanSource:       userSource,
		UserSimulatorPlanSourceSHA256: userSHA,
	}); !errors.Is(err, ErrInvalidClosedLoop) || !strings.Contains(err.Error(), "executor does not match") {
		t.Fatalf("cross-pack harness error = %v", err)
	}
}

func TestRunnerAndHarnessPreserveProvenanceDespiteLaterPackMutation(t *testing.T) {
	fixture := newLoopFixture(t, []string{"The approved budget is 120."}, "Try again.")
	defer fixture.close()
	before, err := fixture.harness.Binding()
	if err != nil {
		t.Fatal(err)
	}
	fixture.pack.Digest = strings.Repeat("f", 64)
	fixture.pack.Manifest.CaseID = "mutated-case"
	after, err := fixture.harness.Binding()
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("harness binding followed caller mutation: before=%+v after=%+v", before, after)
	}
	result, err := fixture.runner.Run(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.CaseID != before.CaseID || result.CasepackSHA256 != before.CasepackSHA256 {
		t.Fatalf("runner provenance followed caller mutation: %+v", result)
	}
}

func TestRunnerRejectsSupervisorFingerprintBeforeMismatchedArmRuns(t *testing.T) {
	fixture := newLoopFixture(t, []string{"done"}, "Try again.")
	defer fixture.close()
	root, err := runtimebriefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	harness, err := runtimebriefcase.NewChatHarness(runtimebriefcase.ChatHarnessConfig{
		Pack: fixture.pack, Root: root, Client: llm.NewClient(fixture.server.URL, "test-key"),
		Model: "test-model", Arm: runcontract.ArmMemoryAssisted,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close()
	if _, err := New(Config{
		Pack: fixture.pack, Executor: harness,
		SupervisorPlanSource:          fixture.supervisorSource,
		SupervisorPlanSourceSHA256:    fixture.supervisorSourceSHA256,
		UserSimulatorPlanSource:       fixture.userSource,
		UserSimulatorPlanSourceSHA256: fixture.userSourceSHA256,
	}); !errors.Is(err, ErrInvalidClosedLoop) || !strings.Contains(err.Error(), "arm does not match") {
		t.Fatalf("arm preflight error = %v", err)
	}
	if len(fixture.requests) != 0 {
		t.Fatalf("mismatched arm spent executor tokens: requests=%d", len(fixture.requests))
	}
}

func TestVisibleTrajectoryIncludesPublicDialogueWithoutSealedPlanTokens(t *testing.T) {
	pack, _, _, _, _ := writeLoopCase(t, "Please retry using only public evidence.")
	run := &runcontract.RunResult{Episodes: []runcontract.EpisodeResult{
		{EpisodeID: "initial", Phase: "timeline", Model: "test-model", Text: "I have a draft."},
		{EpisodeID: "simulator-followup-1", Phase: "follow-up", Cycle: 1, Model: "test-model", Text: "I checked again."},
	}}
	trajectory, err := visibleTrajectory(pack, run, []CycleResult{{
		Cycle: 1, Feedback: "Please retry using only public evidence.",
	}})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(trajectory, "\n")
	for _, visible := range []string{"Find the latest approved budget", "I have a draft", "Please retry", "I checked again"} {
		if !strings.Contains(joined, visible) {
			t.Fatalf("trajectory omitted public dialogue %q: %s", visible, joined)
		}
	}
	for _, sealed := range []string{"latest-budget", "deneb-briefcase-supervisor", "120"} {
		if strings.Contains(joined, sealed) {
			t.Fatalf("trajectory leaked sealed plan token %q: %s", sealed, joined)
		}
	}
}

func TestHiddenFeedbackInputsRejectsStateScalarLeaks(t *testing.T) {
	plan := evalbriefcase.SupervisorPlan{Checkpoints: []evalbriefcase.SupervisorCheckpoint{{
		Cycle: 1,
		Checks: []evalbriefcase.Check{
			{
				ID: "state", Type: evalbriefcase.CheckStateJSONEqual, Weight: 1,
				ExpectedState: json.RawMessage(`{"answer":"cobalt-17","budget":1e2,"approved":true,"optional":null,"nested":["final"]}`),
			},
			{ID: "token", Type: evalbriefcase.CheckContainsToken, Weight: 1, Needle: "987"},
		},
	}}}
	hidden, err := HiddenFeedbackInputs(nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	firewall, err := feedbackcontract.NewFeedbackFirewall(hidden, feedbackcontract.FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{
		"Use cobalt-17.", "The budget is １００.", "Mark it final.", "Use ９８７.",
		"Set approved.", "Set true.", "Set optional.", "Set null.",
	} {
		if _, err := firewall.SanitizeFollowUp(leak); !errors.Is(err, feedbackcontract.ErrFeedbackLeak) {
			t.Fatalf("state scalar leak passed: %q err=%v hidden=%+v", leak, err, hidden)
		}
	}
}

func TestExpectedStateScalarTokensNormalizeAllJSONScalars(t *testing.T) {
	tokens := expectedStateScalarTokens(json.RawMessage(`{"budget":1e2,"approved":true,"optional":null,"label":"final"}`))
	joined := "\x00" + strings.Join(tokens, "\x00") + "\x00"
	for _, want := range []string{"budget", "1e2", "100", "approved", "true", "optional", "null", "label", "final"} {
		if !strings.Contains(joined, "\x00"+want+"\x00") {
			t.Fatalf("state scalar token %q missing from %v", want, tokens)
		}
	}
}

func TestHiddenFeedbackInputsRejectsSupervisorMetadataLeaks(t *testing.T) {
	plan := evalbriefcase.SupervisorPlan{
		SchemaVersion: evalbriefcase.SupervisorPlanSchemaVersion,
		PlanDigest:    strings.Repeat("a", 64),
		Fingerprint: evalbriefcase.SupervisorFingerprint{
			CaseID: "private-case", Seed: 739, Model: "private-model", Arm: string(runcontract.ArmRawPrimary),
		},
		MaxCycles: 2, PassThreshold: 0.75,
		Checkpoints: []evalbriefcase.SupervisorCheckpoint{{Cycle: 1, Checks: []evalbriefcase.Check{{
			ID: "private-check", Type: evalbriefcase.CheckContainsToken, Critical: true, Weight: 2.5, Needle: "answer",
		}}}},
	}
	hidden, err := HiddenFeedbackInputs(nil, plan)
	if err != nil {
		t.Fatal(err)
	}
	firewall, err := feedbackcontract.NewFeedbackFirewall(hidden, feedbackcontract.FeedbackLimits{})
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{
		"The pass threshold is 0.75.", "The check weight is 2.5.", "This is a critical check.",
		"The fingerprint model is private-model.", "The plan digest starts with " + strings.Repeat("a", 64) + ".",
	} {
		if _, err := firewall.SanitizeFollowUp(leak); !errors.Is(err, feedbackcontract.ErrFeedbackLeak) {
			t.Fatalf("supervisor metadata leak passed: %q err=%v", leak, err)
		}
	}
}

type loopFixture struct {
	pack     *casepack.Pack
	harness  *runtimebriefcase.ChatHarness
	runner   *Runner
	plan     evalbriefcase.SupervisorPlan
	userPlan feedbackcontract.UserSimulatorPlan
	root     *runtimebriefcase.RunRoot
	server   *httptest.Server
	requests []string

	supervisorSource       []byte
	userSource             []byte
	supervisorSourceSHA256 string
	userSourceSHA256       string
}

type loopResponseHook func(fixture *loopFixture, requestIndex int) error

func (f *loopFixture) close() {
	if f.harness != nil {
		_ = f.harness.Close()
	}
	if f.root != nil {
		_ = f.root.Close()
	}
	if f.server != nil {
		f.server.Close()
	}
}

func newLoopFixture(t *testing.T, responses []string, feedback string, hooks ...loopResponseHook) *loopFixture {
	t.Helper()
	fixture, err := newLoopFixtureAllowRunnerError(t, responses, feedback, hooks...)
	if err != nil {
		fixture.close()
		t.Fatal(err)
	}
	return fixture
}

func newLoopFixtureAllowRunnerError(t *testing.T, responses []string, feedback string, hooks ...loopResponseHook) (*loopFixture, error) {
	t.Helper()
	if len(hooks) > 1 {
		t.Fatal("newLoopFixture accepts at most one response hook")
	}
	var responseHook loopResponseHook
	if len(hooks) == 1 {
		responseHook = hooks[0]
	}
	pack, plan, userPlan, supervisorSourceSHA256, userSourceSHA256 := writeLoopCase(t, feedback)
	fixture := &loopFixture{
		pack: pack, plan: plan, userPlan: userPlan,
		supervisorSourceSHA256: supervisorSourceSHA256,
		userSourceSHA256:       userSourceSHA256,
	}
	var err error
	fixture.supervisorSource, err = pack.ReadFile("sealed/supervisor-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture.userSource, err = pack.ReadFile("sealed/user-plan.json")
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			t.Errorf("read model request: %v", err)
		}
		fixture.requests = append(fixture.requests, string(body))
		index := len(fixture.requests) - 1
		if index >= len(responses) {
			index = len(responses) - 1
		}
		if responseHook != nil {
			if err := responseHook(fixture, index); err != nil {
				t.Errorf("response hook: %v", err)
				http.Error(w, "fixture response hook failed", http.StatusInternalServerError)
				return
			}
		}
		if responses[index] == "__HTTP_400__" {
			http.Error(w, "fixture rejected follow-up", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, loopTextSSE(responses[index]))
	}))
	root, err := runtimebriefcase.NewRunRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fixture.root = root
	harness, err := runtimebriefcase.NewChatHarness(runtimebriefcase.ChatHarnessConfig{
		Pack: fixture.pack, Root: root, Client: llm.NewClient(fixture.server.URL, "test-key"),
		Model: "test-model", Arm: runcontract.ArmRawPrimary,
	})
	if err != nil {
		fixture.close()
		t.Fatal(err)
	}
	fixture.harness = harness
	fixture.runner, err = New(Config{
		Pack: fixture.pack, Executor: harness,
		SupervisorPlanSource:          fixture.supervisorSource,
		SupervisorPlanSourceSHA256:    fixture.supervisorSourceSHA256,
		UserSimulatorPlanSource:       fixture.userSource,
		UserSimulatorPlanSourceSHA256: fixture.userSourceSHA256,
	})
	if err != nil {
		return fixture, err
	}
	return fixture, nil
}

func writeLoopCase(t *testing.T, feedback string) (*casepack.Pack, evalbriefcase.SupervisorPlan, feedbackcontract.UserSimulatorPlan, string, string) {
	t.Helper()
	dir := t.TempDir()
	task := []byte("Find the latest approved budget and answer with evidence.\n")
	for _, subdir := range []string{"timeline", "sealed"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "timeline", "task.md"), task, 0o600); err != nil {
		t.Fatal(err)
	}
	plan := evalbriefcase.SupervisorPlan{
		SchemaVersion: evalbriefcase.SupervisorPlanSchemaVersion,
		Fingerprint: evalbriefcase.SupervisorFingerprint{
			CaseID: "closed-loop-budget", Seed: 77, Model: "test-model",
			Arm: string(runcontract.ArmRawPrimary), APIMode: llm.APIModeOpenAI,
		},
		MaxCycles:          2,
		PassThreshold:      1,
		FeedbackDenyTokens: []string{"120"},
		Checkpoints: []evalbriefcase.SupervisorCheckpoint{
			{Cycle: 1, Checks: []evalbriefcase.Check{{ID: "latest-budget", Type: evalbriefcase.CheckContains, Weight: 1, Needle: "120"}}},
			{Cycle: 2, Checks: []evalbriefcase.Check{{ID: "latest-budget", Type: evalbriefcase.CheckContains, Critical: true, Weight: 1, Needle: "120"}}},
		},
	}
	if _, err := evalbriefcase.SetSupervisorPlanDigest(&plan); err != nil {
		t.Fatal(err)
	}
	userPlan := feedbackcontract.UserSimulatorPlan{
		SchemaVersion: feedbackcontract.UserSimulatorPlanSchemaVersion,
		CaseID:        "closed-loop-budget",
		FollowUps: []feedbackcontract.ScriptedFollowUp{{
			Cycle: 1, WhenVerdict: feedbackcontract.VerdictNeedsRevision, Message: feedback,
		}},
	}
	supervisorData := marshalTestJSON(t, plan)
	userData := marshalTestJSON(t, userPlan)
	if err := os.WriteFile(filepath.Join(dir, "sealed", "supervisor-plan.json"), supervisorData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sealed", "user-plan.json"), userData, 0o600); err != nil {
		t.Fatal(err)
	}
	supervisorSourceSHA256 := casepack.DigestBytes(supervisorData)
	userSourceSHA256 := casepack.DigestBytes(userData)
	now := time.Date(2029, time.July, 4, 10, 0, 0, 0, time.UTC)
	manifest := casepack.Manifest{
		SchemaVersion: casepack.SchemaVersionV1,
		CaseID:        "closed-loop-budget",
		FamilyID:      "closed-loop",
		Split:         casepack.SplitDev,
		PrivacyMode:   casepack.PrivacyPortable,
		Seed:          77,
		CutoffAt:      now,
		FrozenNow:     now,
		Timezone:      "Asia/Seoul",
		Locale:        "ko-KR",
		Sources: []casepack.Source{
			{
				ID: "sealed-supervisor", Kind: casepack.SourceFile, Origin: casepack.SourceOriginSynthetic,
				Access: casepack.SourceAccessSealed, Path: "sealed/supervisor-plan.json", SHA256: supervisorSourceSHA256,
				EventAt: now, AvailableAt: now, CapturedAt: now, SourceRef: supervisorSourceRef,
			},
			{
				ID: "sealed-user", Kind: casepack.SourceFile, Origin: casepack.SourceOriginSynthetic,
				Access: casepack.SourceAccessSealed, Path: "sealed/user-plan.json", SHA256: userSourceSHA256,
				EventAt: now, AvailableAt: now, CapturedAt: now, SourceRef: userSimulatorSourceRef,
			},
		},
		Episodes: []casepack.Episode{{
			ID: "initial", Kind: casepack.EpisodeUserTurn, At: now,
			Input: &casepack.FileRef{Path: "timeline/task.md", SHA256: casepack.DigestBytes(task)},
		}},
		Artifacts: []casepack.Artifact{{
			ID: "report", Path: "output/report.txt", MIME: "text/plain", MaxBytes: 1024,
		}},
		RunPolicy: casepack.RunPolicy{
			MaxTurns: 4, TimeoutSeconds: 20, MaxTokens: 1024,
			MaxFollowUps: 1, PerTurnTimeoutSeconds: 10,
		},
		ToolPolicy:    casepack.ToolPolicy{Default: casepack.ToolDeny, MaxCalls: 1},
		NetworkPolicy: casepack.NetworkPolicy{Mode: casepack.NetworkDeny},
	}
	digest, err := casepack.CanonicalDigest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifest.ManifestDigest = digest
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, casepack.ManifestFile), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	pack, err := casepack.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return pack, plan, userPlan, supervisorSourceSHA256, userSourceSHA256
}

func marshalTestJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(data, '\n')
}

func loopTextSSE(text string) string {
	encoded, _ := json.Marshal(text)
	return fmt.Sprintf("data: {\"id\":\"chatcmpl-loop\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":null}]}\n\n"+
		"data: {\"id\":\"chatcmpl-loop\",\"object\":\"chat.completion.chunk\",\"model\":\"test-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
		"data: [DONE]\n\n", encoded)
}
