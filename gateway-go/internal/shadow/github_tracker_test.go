package shadow

import (
	"log/slog"
	"testing"
)

// newTestService creates a minimal shadow Service for testing.
func newTestService() *Service {
	return NewService(Config{
		MainSessionKey: "telegram:12345",
		Logger:         slog.Default(),
	})
}

func TestOnEvent_Push(t *testing.T) {
	svc := newTestService()
	svc.started = true

	payload := map[string]any{
		"ref": "refs/heads/main",
		"commits": []any{
			map[string]any{"id": "abc1234", "message": "feat: add X"},
			map[string]any{"id": "def5678", "message": "fix: Y"},
		},
		"repository": map[string]any{"full_name": "choiceoh/deneb"},
		"sender":     map[string]any{"login": "peter"},
	}

	svc.OnGitHubEvent("push", payload)

	gt := svc.GitHubTracker()

	// Check event was recorded.
	events := gt.GetRecentEvents()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].EventType != "push" {
		t.Errorf("expected push event, got %s", events[0].EventType)
	}
	if events[0].Repo != "choiceoh/deneb" {
		t.Errorf("expected repo choiceoh/deneb, got %s", events[0].Repo)
	}
	if events[0].Actor != "peter" {
		t.Errorf("expected actor peter, got %s", events[0].Actor)
	}

	// Check push count incremented.
	summary := gt.GetActivitySummary()
	if summary.TotalPushes != 1 {
		t.Errorf("expected 1 push, got %d", summary.TotalPushes)
	}

	// Check BuildWatcher got the push.
	svc.mu.Lock()
	branch := svc.buildWatcher.lastBranch
	source := svc.buildWatcher.pushSource
	svc.mu.Unlock()
	if branch != "main" {
		t.Errorf("expected branch main, got %s", branch)
	}
	if source != "webhook" {
		t.Errorf("expected pushSource webhook, got %s", source)
	}
}

func TestOnEvent_WorkflowRun_Failure(t *testing.T) {
	svc := newTestService()
	svc.started = true

	payload := map[string]any{
		"action": "completed",
		"workflow_run": map[string]any{
			"name":        "CI",
			"conclusion":  "failure",
			"head_branch": "feature/test",
			"html_url":    "https://github.com/choiceoh/deneb/actions/runs/123",
		},
		"repository": map[string]any{"full_name": "choiceoh/deneb"},
		"sender":     map[string]any{"login": "peter"},
	}

	svc.OnGitHubEvent("workflow_run", payload)

	// Check CI run was recorded.
	summary := svc.GitHubTracker().GetActivitySummary()
	if len(summary.CIRuns) != 1 {
		t.Fatalf("expected 1 CI run, got %d", len(summary.CIRuns))
	}
	if summary.CIRuns[0].Conclusion != "failure" {
		t.Errorf("expected failure conclusion, got %s", summary.CIRuns[0].Conclusion)
	}

	// Check ErrorLearner got the CI failure.
	errors := svc.ErrorLearner().GetErrorHistory()
	found := false
	for _, e := range errors {
		if e.SessionKey == "github:ci" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected ErrorLearner to record a CI failure")
	}
}

func TestOnEvent_WorkflowRun_Success(t *testing.T) {
	svc := newTestService()
	svc.started = true

	payload := map[string]any{
		"action": "completed",
		"workflow_run": map[string]any{
			"name":        "CI",
			"conclusion":  "success",
			"head_branch": "main",
			"html_url":    "https://github.com/choiceoh/deneb/actions/runs/456",
		},
		"repository": map[string]any{"full_name": "choiceoh/deneb"},
		"sender":     map[string]any{"login": "peter"},
	}

	svc.OnGitHubEvent("workflow_run", payload)

	summary := svc.GitHubTracker().GetActivitySummary()
	if len(summary.CIRuns) != 1 {
		t.Fatalf("expected 1 CI run, got %d", len(summary.CIRuns))
	}
	if summary.CIRuns[0].Conclusion != "success" {
		t.Errorf("expected success, got %s", summary.CIRuns[0].Conclusion)
	}

	// ErrorLearner should NOT have a CI failure recorded.
	errors := svc.ErrorLearner().GetErrorHistory()
	for _, e := range errors {
		if e.SessionKey == "github:ci" {
			t.Error("ErrorLearner should not record CI success as error")
		}
	}
}

func TestOnEvent_PullRequest(t *testing.T) {
	svc := newTestService()
	svc.started = true

	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number":   float64(42),
			"title":    "feat(chat): add new tool",
			"html_url": "https://github.com/choiceoh/deneb/pull/42",
		},
		"repository": map[string]any{"full_name": "choiceoh/deneb"},
		"sender":     map[string]any{"login": "peter"},
	}

	svc.OnGitHubEvent("pull_request", payload)

	// Check PR activity was recorded.
	summary := svc.GitHubTracker().GetActivitySummary()
	if len(summary.PRActivity) != 1 {
		t.Fatalf("expected 1 PR activity, got %d", len(summary.PRActivity))
	}
	if summary.PRActivity[0].Number != 42 {
		t.Errorf("expected PR #42, got #%d", summary.PRActivity[0].Number)
	}

	// Check ContextPrefetcher was triggered (should have a prefetched context).
	prefetched := svc.ContextPrefetcher().GetPrefetchedContexts()
	if len(prefetched) != 1 {
		t.Fatalf("expected 1 prefetched context, got %d", len(prefetched))
	}
}

func TestFormatDigestSection(t *testing.T) {
	svc := newTestService()
	gt := svc.GitHubTracker()

	// Empty state — should return "".
	if got := gt.FormatDigestSection(); got != "" {
		t.Errorf("expected empty digest, got %q", got)
	}

	// Add some activity.
	svc.mu.Lock()
	gt.pushCount = 3
	gt.prActivity = []PRActivityRecord{{Number: 1}}
	gt.ciRuns = []CIRunRecord{
		{Conclusion: "success"},
		{Conclusion: "failure"},
	}
	svc.mu.Unlock()

	digest := gt.FormatDigestSection()
	if digest == "" {
		t.Fatal("expected non-empty digest")
	}
	// Check Korean content.
	for _, want := range []string{"GitHub 활동", "푸시: 3건", "PR 활동: 1건", "1건 성공", "1건 실패"} {
		if !contains(digest, want) {
			t.Errorf("digest missing %q: %s", want, digest)
		}
	}
}

func TestRecentEventsRingBuffer(t *testing.T) {
	svc := newTestService()
	svc.started = true

	// Insert more than maxGitHubEvents events.
	for i := 0; i < maxGitHubEvents+20; i++ {
		payload := map[string]any{
			"ref":        "refs/heads/main",
			"commits":    []any{},
			"repository": map[string]any{"full_name": "choiceoh/deneb"},
			"sender":     map[string]any{"login": "peter"},
		}
		svc.GitHubTracker().OnEvent("push", payload)
	}

	events := svc.GitHubTracker().GetRecentEvents()
	if len(events) != maxGitHubEvents {
		t.Errorf("expected ring buffer cap %d, got %d", maxGitHubEvents, len(events))
	}
}

func TestResetDigestCounters(t *testing.T) {
	svc := newTestService()
	gt := svc.GitHubTracker()

	svc.mu.Lock()
	gt.pushCount = 5
	gt.prActivity = []PRActivityRecord{{Number: 1}, {Number: 2}}
	gt.ciRuns = []CIRunRecord{{Conclusion: "success"}}
	svc.mu.Unlock()

	gt.ResetDigestCounters()

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if gt.pushCount != 0 {
		t.Errorf("expected pushCount 0, got %d", gt.pushCount)
	}
	if len(gt.prActivity) != 0 {
		t.Errorf("expected prActivity empty, got %d", len(gt.prActivity))
	}
	if len(gt.ciRuns) != 0 {
		t.Errorf("expected ciRuns empty, got %d", len(gt.ciRuns))
	}
}

func TestFormatEventSummary(t *testing.T) {
	tests := []struct {
		eventType string
		payload   map[string]any
		wantEmpty bool
	}{
		{
			"push",
			map[string]any{
				"ref":        "refs/heads/main",
				"commits":    []any{map[string]any{}},
				"repository": map[string]any{"full_name": "a/b"},
				"sender":     map[string]any{"login": "u"},
			},
			false,
		},
		{"ping", map[string]any{}, true},
		{
			"workflow_run",
			map[string]any{
				"action":       "completed",
				"workflow_run": map[string]any{"conclusion": "success", "name": "CI", "head_branch": "main", "html_url": ""},
				"repository":   map[string]any{"full_name": "a/b"},
				"sender":       map[string]any{"login": "u"},
			},
			false,
		},
		{
			"workflow_run",
			map[string]any{"action": "in_progress"},
			true, // non-completed workflow_run should be skipped
		},
	}

	for _, tt := range tests {
		repo := ghPayloadRepo(tt.payload)
		actor := ghPayloadActor(tt.payload)
		summary := formatEventSummary(tt.eventType, tt.payload, repo, actor)
		if tt.wantEmpty && summary != "" {
			t.Errorf("formatEventSummary(%s) expected empty, got %q", tt.eventType, summary)
		}
		if !tt.wantEmpty && summary == "" {
			t.Errorf("formatEventSummary(%s) expected non-empty", tt.eventType)
		}
	}
}

func TestOnCIFailure_RecurringEscalation(t *testing.T) {
	svc := newTestService()

	// Record the same CI failure 3 times to trigger recurring error escalation.
	for i := 0; i < 3; i++ {
		svc.ErrorLearner().OnCIFailure("lint", "main", "https://example.com")
	}

	recurring := svc.ErrorLearner().GetRecurringErrors()
	found := false
	for _, r := range recurring {
		if r.SessionKey == "github:ci" && r.Occurrences >= 3 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected recurring error after 3 CI failures of the same workflow")
	}
}

func TestOnGitHubPush_SetsFields(t *testing.T) {
	svc := newTestService()
	bw := svc.BuildWatcher()

	bw.OnGitHubPush("feature/x", 3, "choiceoh/deneb")

	svc.mu.Lock()
	defer svc.mu.Unlock()

	if bw.lastBranch != "feature/x" {
		t.Errorf("expected branch feature/x, got %s", bw.lastBranch)
	}
	if bw.lastRepo != "choiceoh/deneb" {
		t.Errorf("expected repo choiceoh/deneb, got %s", bw.lastRepo)
	}
	if bw.pushSource != "webhook" {
		t.Errorf("expected pushSource webhook, got %s", bw.pushSource)
	}
	if !bw.watchingBuild {
		t.Error("expected watchingBuild to be true")
	}
	if bw.ciReceived == nil {
		t.Error("expected ciReceived channel to be created")
	}
}

func TestOnCIResult_ClosesCIChannel(t *testing.T) {
	svc := newTestService()
	bw := svc.BuildWatcher()

	// Simulate a push first to create the ciReceived channel.
	bw.OnGitHubPush("main", 1, "choiceoh/deneb")

	svc.mu.Lock()
	ch := bw.ciReceived
	svc.mu.Unlock()

	// Now deliver a CI result.
	bw.OnCIResult("main", true, "CI")

	// Channel should be closed.
	select {
	case <-ch:
		// ok
	default:
		t.Error("expected ciReceived channel to be closed after OnCIResult")
	}

	svc.mu.Lock()
	watching := bw.watchingBuild
	svc.mu.Unlock()
	if watching {
		t.Error("expected watchingBuild to be false after OnCIResult")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && len(substr) > 0 && searchSubstring(s, substr)
}

func searchSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
