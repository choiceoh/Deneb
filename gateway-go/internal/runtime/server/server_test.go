package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/memory"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	propussystem "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

func TestNewReturnsDirectGrammarCatalogError(t *testing.T) {
	catalogErr := &memory.DirectGrammarCatalogError{Cause: errors.New("broken fixture")}
	_, err := newWithDirectGrammarCatalogValidator(":0", func() error {
		return catalogErr
	})
	if err == nil {
		t.Fatal("New unexpectedly succeeded")
	}
	var got *memory.DirectGrammarCatalogError
	if !errors.As(err, &got) || got != catalogErr {
		t.Fatalf("error = %T %[1]v, want wrapped DirectGrammarCatalogError", err)
	}
	if !strings.Contains(err.Error(), "validate memory direct grammar") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestHealthEndpointReturnsOKWithWorkerPoolStats(t *testing.T) {
	t.Parallel()
	srv := sharedReadOnlyServer(t)
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("got %v, want status ok", resp["status"])
	}
	if _, ok := resp["subsystems"]; !ok {
		t.Errorf("expected subsystems field in health response")
	}
	manifest, ok := resp["runtime_manifest"].(map[string]any)
	if !ok {
		t.Fatalf("expected runtime_manifest health section, got %T", resp["runtime_manifest"])
	}
	if len(manifest["sha256"].(string)) != 64 {
		t.Fatalf("invalid runtime manifest digest: %+v", manifest)
	}
	if tools := manifest["tools"].(map[string]any); tools["state"] != "loaded" || tools["count"].(float64) < 1 {
		t.Fatalf("runtime manifest did not capture registered tools: %+v", tools)
	}
	if models := manifest["models"].(map[string]any); models["state"] != "loaded" || models["count"].(float64) < 1 {
		t.Fatalf("runtime manifest did not capture configured models: %+v", models)
	}
	rpcHealth, ok := resp["rpc"].(map[string]any)
	if !ok {
		t.Fatalf("expected rpc health section, got %T", resp["rpc"])
	}
	pool, ok := rpcHealth["worker_pool"].(map[string]any)
	if !ok {
		t.Fatalf("expected rpc worker_pool health, got %T", rpcHealth["worker_pool"])
	}
	if pool["maxSize"].(float64) < 1 ||
		pool["canceledBeforeStart"].(float64) != 0 ||
		pool["saturationEvents"].(float64) != 0 ||
		pool["peakQueued"].(float64) != 0 ||
		pool["saturated"].(bool) {
		t.Fatalf("unexpected rpc worker-pool health: %+v", pool)
	}
}

func TestHealthEndpointReturnsUsageQualityAndPropusSignals(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	if err := tracker.RecordUsage(genesis.UsageRecord{SkillName: "topsolar-db", Success: true}); err != nil {
		t.Fatalf("RecordUsage success: %v", err)
	}
	if err := tracker.RecordUsage(genesis.UsageRecord{SkillName: "topsolar-db", Success: false}); err != nil {
		t.Fatalf("RecordUsage empty legacy failure: %v", err)
	}
	tracker.RecordEvolutionActivity(genesis.SkillActivityReviewAttempt, true, "")
	tracker.RecordEvolutionActivity(genesis.SkillActivityReviewSkipped, true, "")
	tracker.RecordEvolutionActivity(genesis.SkillActivityValidationRejected, true, "")
	for _, desc := range []string{"older trace", "newer trace"} {
		if err := tracker.RecordSkillValidationCase(genesis.SkillValidationCaseRecord{
			SkillName:   "topsolar-db",
			ID:          "real-server-health",
			Description: desc,
			Replay: genesis.SkillReplayCaseRecord{
				ExpectedToolCalls: []genesis.SkillReplayToolCallRecord{
					{Name: "exec", InputIncludes: []string{"systemctl --user status deneb-gateway.service"}},
				},
			},
			Source: "session-backfill",
		}); err != nil {
			t.Fatalf("RecordSkillValidationCase(%s): %v", desc, err)
		}
	}
	if err := tracker.LogEvolveRejectedWithAudit("topsolar-db", "self-harness audit rejected: missing target_signature", genesis.HarnessEditAudit{}); err != nil {
		t.Fatalf("LogEvolveRejectedWithAudit: %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		Scope:     "test",
		SkillName: "topsolar-db",
		Title:     "Promote rejected evolve into held-out validation",
		Source:    "self-harness-rejected-evolve:preflight",
	}); err != nil {
		t.Fatalf("RecordSelfCorrectionCandidate: %v", err)
	}

	srv := testutil.Must(New(":0"))
	srv.GenesisSubsystem = &GenesisSubsystem{genesisTracker: tracker}
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	srv.handleHealth(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	selfEvolution := resp["self_evolution"].(map[string]any)
	propus := resp["propus"].(map[string]any)
	if !reflect.DeepEqual(propus, selfEvolution) {
		t.Fatalf("propus and self_evolution health payloads diverged: propus=%+v self=%+v", propus, selfEvolution)
	}
	if propus["system"] != "Propus" || propus["tool"] != "skill_lifecycle" {
		t.Fatalf("unexpected Propus health identity: %+v", propus)
	}
	if propus["doctrine_version"] != propussystem.PropusDoctrine().Version {
		t.Fatalf("unexpected Propus doctrine version: %+v", propus)
	}
	if gates, ok := propus["quality_gates"].([]any); !ok || len(gates) != len(propussystem.PropusDoctrine().QualityGates) {
		t.Fatalf("expected Propus quality gates, got %+v", propus)
	}
	if propus["state"] != "attention" {
		t.Fatalf("expected Propus attention state, got %+v", propus)
	}
	if propus["overview_state"] == "" || propus["coverage_state"] == "" {
		t.Fatalf("expected unified Propus overview/coverage state, got %+v", propus)
	}
	if _, ok := propus["next_actions"].([]any); !ok {
		t.Fatalf("expected Propus next actions from unified overview, got %+v", propus)
	}
	if attention, ok := propus["attention"].([]any); !ok || len(attention) == 0 {
		t.Fatalf("expected Propus attention signals, got %+v", propus)
	}
	if propus["validation_case_records"] != selfEvolution["validation_case_records"] {
		t.Fatalf("propus and self_evolution health payloads diverged: propus=%+v self=%+v", propus, selfEvolution)
	}
	if selfEvolution["usage_records"] != float64(2) ||
		selfEvolution["usage_counted_records"] != float64(1) ||
		selfEvolution["ignored_unactionable_legacy_failures"] != float64(1) {
		t.Fatalf("unexpected usage-quality health payload: %+v", selfEvolution)
	}
	if selfEvolution["top_ignored_unactionable_legacy_failure_skill"] != "topsolar-db" {
		t.Fatalf("unexpected top ignored skill: %+v", selfEvolution)
	}
	if selfEvolution["review_attempts"] != float64(1) ||
		selfEvolution["review_skips"] != float64(1) ||
		selfEvolution["validation_rejections"] != float64(1) {
		t.Fatalf("unexpected self-evolution attempt counters: %+v", selfEvolution)
	}
	if selfEvolution["validation_case_records"] != float64(2) ||
		selfEvolution["validation_cases_unique"] != float64(1) ||
		selfEvolution["validation_case_duplicates"] != float64(1) ||
		selfEvolution["validation_cases_automatic"] != float64(2) ||
		selfEvolution["validation_cases_unique_automatic"] != float64(1) ||
		selfEvolution["validation_case_skills"] != float64(1) {
		t.Fatalf("unexpected validation-case health payload: %+v", selfEvolution)
	}
	if selfEvolution["validation_case_top_skill"] != "topsolar-db" ||
		selfEvolution["validation_case_top_skill_cases"] != float64(1) {
		t.Fatalf("unexpected top validation-case skill: %+v", selfEvolution)
	}
	if selfEvolution["self_harness_rejections_7d"] != float64(1) ||
		selfEvolution["self_harness_missing_audit_rejections_7d"] != float64(1) ||
		selfEvolution["self_harness_validation_drafts_7d"] != float64(1) {
		t.Fatalf("unexpected self-harness health payload: %+v", selfEvolution)
	}
	if propus["self_harness_rejections_7d"] != selfEvolution["self_harness_rejections_7d"] {
		t.Fatalf("propus and self_evolution self-harness payloads diverged: propus=%+v self=%+v", propus, selfEvolution)
	}
}

func TestReadyEndpoint(t *testing.T) {
	t.Parallel()
	// Mutates ready — cannot share the read-only fixture.
	srv := testutil.Must(New(":0"))

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/ready", nil)
	w := httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", w.Code)
	}

	srv.ready.Store(true)
	w = httptest.NewRecorder()
	srv.handleReady(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("got %d, want 200", w.Code)
	}
}

func TestServerStartStop(t *testing.T) {
	t.Parallel()
	srv := testutil.Must(New("127.0.0.1:0"))
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	err := srv.Run(ctx)
	testutil.NoError(t, err)
}
