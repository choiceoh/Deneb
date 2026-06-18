package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolctx"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools"
)

func newSkillLifecycleTestTracker(t *testing.T) *genesis.Tracker {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	return tracker
}

func requireStringSliceContains(t *testing.T, values []string, want string) {
	t.Helper()
	for _, value := range values {
		if value == want {
			return
		}
	}
	t.Fatalf("expected %q in %+v", want, values)
}

type skillLifecycleTranscriptStore struct {
	msgs  []toolctx.ChatMessage
	byKey map[string][]toolctx.ChatMessage
}

func (s skillLifecycleTranscriptStore) Append(string, toolctx.ChatMessage) error { return nil }
func (s skillLifecycleTranscriptStore) Load(sessionKey string, _ int) ([]toolctx.ChatMessage, int, error) {
	if s.byKey != nil {
		msgs := append([]toolctx.ChatMessage(nil), s.byKey[sessionKey]...)
		return msgs, len(msgs), nil
	}
	return append([]toolctx.ChatMessage(nil), s.msgs...), len(s.msgs), nil
}
func (s skillLifecycleTranscriptStore) Delete(string) error { return nil }
func (s skillLifecycleTranscriptStore) ListKeys() ([]string, error) {
	keys := make([]string, 0, len(s.byKey))
	for key := range s.byKey {
		keys = append(keys, key)
	}
	return keys, nil
}
func (s skillLifecycleTranscriptStore) Search(string, int) ([]toolctx.SearchResult, error) {
	return nil, nil
}
func (s skillLifecycleTranscriptStore) CloneRecent(string, string, int) error { return nil }

func TestSkillLifecycleStatusFiltersBySkillAndStats(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.RecordUsage(genesis.UsageRecord{
		SkillName:  "deploy-helper",
		SessionKey: "telegram:1",
		Success:    false,
		ErrorMsg:   "missing token",
	}); err != nil {
		t.Fatalf("RecordUsage: %v", err)
	}
	if err := tracker.RecordUsage(genesis.UsageRecord{
		SkillName: "deploy-helper",
		Success:   false,
	}); err != nil {
		t.Fatalf("RecordUsage empty legacy failure: %v", err)
	}
	if err := tracker.LogGenesis("other-skill", "session", "", "coding", "Other workflow"); err != nil {
		t.Fatalf("LogGenesis(other): %v", err)
	}
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis(deploy): %v", err)
	}
	if err := tracker.LogEvolutionProposal(genesis.EvolutionProposalRecord{
		Candidate: "repeatable deploy fix",
		Route:     "evolve",
		SkillName: "deploy-helper",
		Executed:  true,
	}); err != nil {
		t.Fatalf("LogEvolutionProposal: %v", err)
	}
	if err := tracker.RecordRejectedSkillEdit(genesis.RejectedSkillEditRecord{
		SkillName:     "deploy-helper",
		Reason:        "invented command",
		CandidateBody: "bad candidate",
		Source:        "self-test",
	}); err != nil {
		t.Fatalf("RecordRejectedSkillEdit: %v", err)
	}
	if err := tracker.RecordSkillOpportunity(genesis.SkillOpportunityRecord{
		Candidate: "repeatable deploy fix",
		Route:     "evolve",
		SkillName: "deploy-helper",
		Evidence:  "same deploy gap repeated",
	}); err != nil {
		t.Fatalf("RecordSkillOpportunity: %v", err)
	}
	if _, err := tracker.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		Scope:          "skill",
		SkillName:      "deploy-helper",
		Title:          "Tighten deploy verification",
		Candidate:      "add origin/main proof reminder",
		ProposedChange: "record deferred patch for batch review",
	}); err != nil {
		t.Fatalf("RecordSelfCorrectionCandidate: %v", err)
	}

	backend := &skillLifecycleBackend{tracker: tracker}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "deploy-helper",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	system := got["system"].(map[string]any)
	if system["name"] != "Propus" || system["tool"] != "skill_lifecycle" || system["scope"] != "skill" {
		t.Fatalf("unexpected Propus system status: %+v", system)
	}
	if system["version"] != genesis.PropusDoctrine().Version {
		t.Fatalf("unexpected Propus doctrine version: %+v", system)
	}
	sourcePapers := system["sourcePapers"].([]string)
	if len(sourcePapers) != len(genesis.PropusDoctrine().SourceIDs()) ||
		sourcePapers[0] != "arxiv:2602.20867" ||
		sourcePapers[len(sourcePapers)-1] != "arxiv:2605.21240" {
		t.Fatalf("unexpected source papers: %+v", sourcePapers)
	}
	filteredSources := system["filteredSources"].([]string)
	if len(filteredSources) != 1 || filteredSources[0] != "arxiv:2606.15363" {
		t.Fatalf("unexpected filtered sources: %+v", filteredSources)
	}
	overview := got["overview"].(map[string]any)
	if overview["state"] != "needs_review" ||
		overview["pendingSelfCorrections"] != 1 ||
		overview["openOpportunities"] != 1 ||
		overview["validationCases"] != 0 {
		t.Fatalf("unexpected Propus overview: %+v", overview)
	}
	nextActions := overview["nextActions"].([]string)
	if len(nextActions) < 3 ||
		nextActions[0] != "review_pending_self_corrections" {
		t.Fatalf("unexpected Propus next actions: %+v", nextActions)
	}
	requireStringSliceContains(t, nextActions, "record_validation_case_from_session")
	requireStringSliceContains(t, nextActions, "triage_opportunity_backlog")
	coverage := overview["doctrineCoverage"].(map[string]any)
	if coverage["state"] != "partial" || coverage["sourcePolicy"] != "core_sources_only_filtered_sources_not_gates" {
		t.Fatalf("unexpected doctrine coverage: %+v", coverage)
	}
	requireStringSliceContains(t, coverage["covered"].([]string), "exploration_backlog_available")
	requireStringSliceContains(t, coverage["gaps"].([]string), "missing_held_out_validation_corpus")
	requireStringSliceContains(t, coverage["gaps"].([]string), "apex_mixed_frontier_unmeasured")
	coverageFiltered := coverage["filteredSources"].([]string)
	if len(coverageFiltered) != 1 || coverageFiltered[0] != "arxiv:2606.15363" {
		t.Fatalf("unexpected coverage filtered sources: %+v", coverageFiltered)
	}
	recent := got["recent"].([]genesis.LifecycleLogEntry)
	if len(recent) != 2 {
		t.Fatalf("expected 2 deploy-helper lifecycle entries, got %+v", recent)
	}
	stats := got["stats"].(*genesis.UsageStats)
	if stats.SkillName != "deploy-helper" || stats.TotalUses != 1 || stats.SuccessRate != 0 {
		t.Fatalf("unexpected stats: %+v", stats)
	}
	quality := got["usageQuality"].(genesis.UsageQualitySummary)
	if quality.SkillName != "deploy-helper" || quality.TotalRecords != 2 || quality.CountedRecords != 1 || quality.IgnoredUnactionableLegacyFailures != 1 {
		t.Fatalf("unexpected usage quality: %+v", quality)
	}
	curator := got["curator"].([]genesis.SkillCuratorRecord)
	if len(curator) != 1 || curator[0].SkillName != "deploy-helper" {
		t.Fatalf("unexpected curator report: %+v", curator)
	}
	rejected := got["rejectedEdits"].([]genesis.RejectedSkillEditRecord)
	if len(rejected) != 1 || rejected[0].Reason != "invented command" {
		t.Fatalf("unexpected rejected edits: %+v", rejected)
	}
	opportunities := got["opportunities"].([]genesis.SkillOpportunityRecord)
	if len(opportunities) != 1 || opportunities[0].Candidate != "repeatable deploy fix" {
		t.Fatalf("unexpected opportunities: %+v", opportunities)
	}
	selfCorrections := got["selfCorrectionCandidates"].([]genesis.SelfCorrectionCandidateRecord)
	if len(selfCorrections) != 1 || selfCorrections[0].SkillName != "deploy-helper" {
		t.Fatalf("unexpected self-correction candidates: %+v", selfCorrections)
	}
}

func TestSkillLifecycleStatusIdentifiesPropusWhenTrackerMissing(t *testing.T) {
	backend := &skillLifecycleBackend{}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["ok"] != false {
		t.Fatalf("expected unavailable status, got %+v", got)
	}
	system := got["system"].(map[string]any)
	if system["name"] != "Propus" || system["scope"] != "global" {
		t.Fatalf("unexpected Propus system status: %+v", system)
	}
	overview := got["overview"].(map[string]any)
	if overview["state"] != "unavailable" {
		t.Fatalf("unexpected unavailable overview: %+v", overview)
	}
}

func TestSkillLifecycleStatusKeepsPartialStateWhenRejectedEditsUnreadable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tracker, err := genesis.NewTracker(slog.Default())
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}

	rejectedPath := filepath.Join(home, ".deneb", "data", "skill_rejected_edits.jsonl")
	if err := os.WriteFile(rejectedPath, []byte(strings.Repeat("x", 1<<20+1)+"\n"), 0o644); err != nil {
		t.Fatalf("write oversized rejected edits sidecar: %v", err)
	}

	backend := &skillLifecycleBackend{tracker: tracker}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "deploy-helper",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus should return partial state: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["ok"] != true {
		t.Fatalf("expected ok partial status, got %+v", got)
	}
	recent := got["recent"].([]genesis.LifecycleLogEntry)
	if len(recent) != 1 || recent[0].SkillName != "deploy-helper" {
		t.Fatalf("expected lifecycle log to remain available, got %+v", recent)
	}
	if rejected, ok := got["rejectedEdits"].([]genesis.RejectedSkillEditRecord); !ok || len(rejected) != 0 {
		t.Fatalf("expected empty rejected edits on sidecar error, got %#v", got["rejectedEdits"])
	}
	if errText, ok := got["rejectedEditsError"].(string); !ok || !strings.Contains(errText, "load rejected edits") {
		t.Fatalf("expected rejectedEditsError, got %+v", got)
	}
}

func TestSkillLifecycleStatusIncludesOptimizerMemory(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.LogEvolve("deploy-helper", "1.0.1", "tighten verification steps"); err != nil {
		t.Fatalf("LogEvolve: %v", err)
	}
	if err := tracker.LogEvolveRejected("deploy-helper", "invented command"); err != nil {
		t.Fatalf("LogEvolveRejected: %v", err)
	}

	backend := &skillLifecycleBackend{tracker: tracker}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "deploy-helper",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	memory := got["optimizerMemory"].(genesis.SkillOptimizerMemoryEntry)
	if memory.AcceptedCount != 1 || memory.RejectedCount != 1 {
		t.Fatalf("unexpected optimizer memory counts: %+v", memory)
	}
	if len(memory.StableDirections) != 1 || memory.StableDirections[0] != "tighten verification steps" {
		t.Fatalf("unexpected stable directions: %+v", memory.StableDirections)
	}
	if len(memory.AvoidDirections) != 1 || memory.AvoidDirections[0] != "invented command" {
		t.Fatalf("unexpected avoid directions: %+v", memory.AvoidDirections)
	}
}

func TestSkillLifecycleValidationCaseRecordsAndStatusSurfacesIt(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	backend := &skillLifecycleBackend{tracker: tracker}

	if _, err := backend.RecordSkillValidationCase(context.Background(), chattools.SkillValidationCaseRequest{
		SkillName:           "topsolar-db",
		ID:                  "safe-wrapper",
		Description:         "preserve safe execution wrapper",
		FrontierTier:        "mixed",
		RequiredSubstrings:  []string{"단일 bash block"},
		ForbiddenSubstrings: []string{"eval"},
		RequiredHeadings:    []string{"통합 실행 흐름"},
		Replay: chattools.SkillReplayCaseRequest{
			Input:                 "srv1에서 실제 deneb-gateway 상태를 확인하고 개선",
			RequiredActions:       []string{"ssh srv1", "systemctl --user status deneb-gateway.service"},
			ForbiddenActions:      []string{"로컬 상태만 보고 판단"},
			RequiredObservations:  []string{"active (running)"},
			ForbiddenObservations: []string{"stopped"},
			RequiredTools:         []string{"ssh"},
			ExpectedToolCalls: []chattools.SkillReplayToolCallRequest{
				{Name: "exec", InputIncludes: []string{"ssh srv1"}},
				{Name: "exec", InputIncludes: []string{"systemctl --user status deneb-gateway.service"}, FixtureOutput: "Active: active (running)"},
			},
			ForbiddenToolCalls: []chattools.SkillReplayToolCallRequest{
				{Name: "exec", InputIncludes: []string{"rm -rf"}},
			},
			RequireOrder: true,
		},
		Source: "operator",
	}); err != nil {
		t.Fatalf("RecordSkillValidationCase: %v", err)
	}

	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "topsolar-db",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	cases := got["validationCases"].([]genesis.SkillValidationCaseRecord)
	if len(cases) != 1 || cases[0].ID != "safe-wrapper" || cases[0].Source != "operator" {
		t.Fatalf("unexpected validation cases: %+v", cases)
	}
	if cases[0].FrontierTier != "mixed" {
		t.Fatalf("expected normalized frontier tier, got %+v", cases[0])
	}
	if cases[0].Replay.Input == "" ||
		len(cases[0].Replay.RequiredActions) != 2 ||
		len(cases[0].Replay.ForbiddenActions) != 1 ||
		len(cases[0].Replay.RequiredObservations) != 1 ||
		len(cases[0].Replay.ForbiddenObservations) != 1 ||
		len(cases[0].Replay.RequiredTools) != 1 ||
		len(cases[0].Replay.ExpectedToolCalls) != 2 ||
		cases[0].Replay.ExpectedToolCalls[1].FixtureOutput == "" ||
		len(cases[0].Replay.ForbiddenToolCalls) != 1 ||
		!cases[0].Replay.RequireOrder {
		t.Fatalf("unexpected replay case: %+v", cases[0].Replay)
	}
	summary := got["validationCaseSummary"].(genesis.SkillValidationCaseSummary)
	if summary.SkillName != "topsolar-db" ||
		summary.RawRecords != 1 ||
		summary.UniqueRecords != 1 ||
		summary.DuplicateRecords != 0 {
		t.Fatalf("unexpected validation case summary: %+v", summary)
	}
	if summary.UniqueMixedFrontierCases != 1 {
		t.Fatalf("unexpected frontier tier summary: %+v", summary)
	}
}

func TestSkillLifecycleStatusRequiresTieredApexFrontierEvidence(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.LogGenesis("deploy-helper", "session", "telegram:1", "coding", "Deploy workflow"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	audit := genesis.HarnessEditAudit{
		TargetSignature:        "terminal=timeout|mechanism=bounded-execution",
		EditedSurface:          "Procedure",
		ExpectedBehaviorChange: "bounded recovery",
		RegressionRisk:         "preserve verification",
	}
	if err := tracker.LogEvolveWithAudit("deploy-helper", "1.0.1", "tighten timeout recovery", audit); err != nil {
		t.Fatalf("LogEvolveWithAudit: %v", err)
	}
	for _, rec := range []genesis.SkillValidationCaseRecord{
		{
			SkillName:          "deploy-helper",
			ID:                 "easy-anchor",
			FrontierTier:       "easy",
			RequiredSubstrings: []string{"origin/main"},
			Source:             "operator",
		},
		{
			SkillName:          "deploy-helper",
			ID:                 "mixed-frontier",
			FrontierTier:       "mixed",
			RequiredSubstrings: []string{"real listener"},
			Source:             "operator",
		},
	} {
		if err := tracker.RecordSkillValidationCase(rec); err != nil {
			t.Fatalf("RecordSkillValidationCase(%s): %v", rec.ID, err)
		}
	}
	if err := tracker.RecordSkillOpportunity(genesis.SkillOpportunityRecord{
		Candidate: "record production deploy proof variants",
		Route:     "evolve",
		SkillName: "deploy-helper",
		Evidence:  "operator repeatedly asks for real-state verification",
	}); err != nil {
		t.Fatalf("RecordSkillOpportunity: %v", err)
	}

	backend := &skillLifecycleBackend{tracker: tracker}
	gotAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "deploy-helper",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	got := gotAny.(map[string]any)
	overview := got["overview"].(map[string]any)
	coverage := overview["doctrineCoverage"].(map[string]any)
	if coverage["state"] != "covered" {
		t.Fatalf("expected covered doctrine coverage, got %+v", coverage)
	}
	for _, want := range []string{
		"held_out_validation_corpus",
		"self_harness_failure_signature_audit",
		"apex_mixed_frontier_with_easy_anchor",
		"exploration_backlog_available",
	} {
		requireStringSliceContains(t, coverage["covered"].([]string), want)
	}
	if gaps := coverage["gaps"].([]string); len(gaps) != 0 {
		t.Fatalf("expected no doctrine coverage gaps, got %+v", gaps)
	}
	if coverage["easyAnchorCases"] != 1 || coverage["mixedFrontierCases"] != 1 {
		t.Fatalf("unexpected frontier coverage counts: %+v", coverage)
	}
}

func TestSkillLifecycleValidationCaseFromSessionExtractsToolTrace(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	store := skillLifecycleTranscriptStore{msgs: []toolctx.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1에서 실제 deneb-gateway 상태를 확인하고 개선"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service","workdir":"/srv/deneb"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Active: active (running)","is_error":false}
		]`)},
		{Role: "assistant", Content: json.RawMessage(`"실제 서버 상태를 확인했습니다."`)},
	}}
	backend := &skillLifecycleBackend{tracker: tracker, transcripts: store}

	gotAny, err := backend.RecordSkillValidationCaseFromSession(context.Background(), chattools.SkillValidationCaseFromSessionRequest{
		SkillName:   "srv1-ops",
		SessionKey:  "client:main:srv1",
		Description: "preserve real server inspection before edits",
		Replay: chattools.SkillReplayCaseRequest{
			RequiredActions:      []string{"ssh srv1"},
			RequiredObservations: []string{"active (running)"},
		},
	})
	if err != nil {
		t.Fatalf("RecordSkillValidationCaseFromSession: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["expectedToolCalls"] != 1 || got["forbiddenToolCalls"] != 0 || got["requiredTools"] != 1 {
		t.Fatalf("unexpected extraction result: %+v", got)
	}

	statusAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{
		SkillName: "srv1-ops",
		Limit:     5,
	})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	status := statusAny.(map[string]any)
	cases := status["validationCases"].([]genesis.SkillValidationCaseRecord)
	if len(cases) != 1 {
		t.Fatalf("expected 1 validation case, got %+v", cases)
	}
	replay := cases[0].Replay
	if cases[0].Source != "review-session" ||
		replay.Input != "srv1에서 실제 deneb-gateway 상태를 확인하고 개선" ||
		len(replay.ExpectedToolCalls) != 1 ||
		replay.ExpectedToolCalls[0].Name != "exec" ||
		len(replay.ExpectedToolCalls[0].InputIncludes) != 2 ||
		replay.ExpectedToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		replay.ExpectedToolCalls[0].InputIncludes[1] != "systemctl --user status" ||
		replay.ExpectedToolCalls[0].FixtureOutput != "Active: active (running)" ||
		len(replay.RequiredTools) != 1 ||
		replay.RequiredTools[0] != "exec" {
		t.Fatalf("unexpected replay extraction: %+v", replay)
	}
}

func TestSkillLifecycleValidationCaseFromSessionSeparatesErroredToolTrace(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	store := skillLifecycleTranscriptStore{msgs: []toolctx.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1 배포 상태 확인 후 필요하면 복구"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"systemctl --user restart deneb-gateway.service"}},
			{"type":"tool_use","id":"tu_2","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Failed to connect to bus","is_error":true},
			{"type":"tool_result","tool_use_id":"tu_2","content":"Active: active (running)","is_error":false}
		]`)},
		{Role: "assistant", Content: json.RawMessage(`"srv1에서 실제 상태를 확인했습니다."`)},
	}}
	backend := &skillLifecycleBackend{tracker: tracker, transcripts: store}

	gotAny, err := backend.RecordSkillValidationCaseFromSession(context.Background(), chattools.SkillValidationCaseFromSessionRequest{
		SkillName:  "srv1-ops",
		SessionKey: "client:main:mixed-trace",
	})
	if err != nil {
		t.Fatalf("RecordSkillValidationCaseFromSession: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["expectedToolCalls"] != 1 || got["forbiddenToolCalls"] != 1 || got["requiredTools"] != 1 {
		t.Fatalf("unexpected extraction result: %+v", got)
	}

	cases, err := tracker.RecentSkillValidationCases("srv1-ops", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 validation case, got %+v", cases)
	}
	replay := cases[0].Replay
	if len(replay.ExpectedToolCalls) != 1 ||
		replay.ExpectedToolCalls[0].Name != "exec" ||
		len(replay.ExpectedToolCalls[0].InputIncludes) != 2 ||
		replay.ExpectedToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		replay.ExpectedToolCalls[0].InputIncludes[1] != "systemctl --user status" ||
		replay.ExpectedToolCalls[0].FixtureError ||
		len(replay.ForbiddenToolCalls) != 1 ||
		replay.ForbiddenToolCalls[0].Name != "exec" ||
		len(replay.ForbiddenToolCalls[0].InputIncludes) != 1 ||
		replay.ForbiddenToolCalls[0].InputIncludes[0] != "systemctl --user restart" ||
		replay.ForbiddenToolCalls[0].FixtureError {
		t.Fatalf("expected errored trace to become forbidden, got %+v", replay)
	}
}

func TestChatUsageRecorderAutoValidationCaseFromFailedUse(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	store := skillLifecycleTranscriptStore{msgs: []toolctx.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1에서 실제 deneb-gateway 상태를 확인하고 개선"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service","workdir":"/srv/deneb"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Active: failed","is_error":true}
		]`)},
	}}
	rec := newChatUsageRecorderAdapter(tracker, store, slog.Default())

	rec.RecordSkillUse("client:main:srv1", "srv1-ops", false, "turn failed: tool exec errored")

	var cases []genesis.SkillValidationCaseRecord
	var err error
	for i := 0; i < 20; i++ {
		cases, err = tracker.RecentSkillValidationCases("srv1-ops", 5)
		if err != nil {
			t.Fatalf("RecentSkillValidationCases: %v", err)
		}
		if len(cases) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one auto validation case, got %+v", cases)
	}
	rec.RecordSkillUse("client:main:srv1", "srv1-ops", false, "turn failed: tool exec errored")
	time.Sleep(50 * time.Millisecond)
	tc := cases[0]
	if tc.Source != "auto-failed-skill-use" ||
		tc.ID != "session-client:main:srv1" ||
		!strings.Contains(tc.Description, "turn failed: tool exec errored") ||
		tc.Replay.Input != "srv1에서 실제 deneb-gateway 상태를 확인하고 개선" ||
		len(tc.Replay.ExpectedToolCalls) != 0 ||
		len(tc.Replay.RequiredTools) != 0 ||
		len(tc.Replay.ForbiddenToolCalls) != 1 ||
		tc.Replay.ForbiddenToolCalls[0].Name != "exec" ||
		len(tc.Replay.ForbiddenToolCalls[0].InputIncludes) != 2 ||
		tc.Replay.ForbiddenToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		tc.Replay.ForbiddenToolCalls[0].InputIncludes[1] != "systemctl --user status" {
		t.Fatalf("unexpected auto validation case: %+v", tc)
	}
	summary, err := tracker.ValidationCaseSummary("srv1-ops")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 1 || summary.AutomaticRecords != 1 || summary.UniqueAutomaticRecords != 1 {
		t.Fatalf("expected auto failed-use case in automatic summary, got %+v", summary)
	}
}

func TestChatUsageRecorderAutoValidationCaseRefreshesRicherFailedUseTrace(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	sessionKey := "client:main:srv1"
	store := skillLifecycleTranscriptStore{byKey: map[string][]toolctx.ChatMessage{
		sessionKey: {
			{Role: "user", Content: json.RawMessage(`"srv1에서 deneb-gateway 복구"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"systemctl --user restart deneb-gateway.service"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"tu_1","content":"Failed to connect to bus","is_error":true}
			]`)},
		},
	}}
	rec := &chatUsageRecorderAdapter{inner: tracker, transcripts: store, logger: slog.Default()}

	rec.recordValidationCaseFromFailedUse(sessionKey, "srv1-ops", "turn failed: local restart errored")
	store.byKey[sessionKey] = []toolctx.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"srv1에서 deneb-gateway 복구"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"systemctl --user restart deneb-gateway.service"}},
			{"type":"tool_use","id":"tu_2","name":"exec","input":{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service"}}
		]`)},
		{Role: "user", Content: json.RawMessage(`[
			{"type":"tool_result","tool_use_id":"tu_1","content":"Failed to connect to bus","is_error":true},
			{"type":"tool_result","tool_use_id":"tu_2","content":"Active: active (running)","is_error":false}
		]`)},
	}
	rec.recordValidationCaseFromFailedUse(sessionKey, "srv1-ops", "turn failed: local restart errored")

	summary, err := tracker.ValidationCaseSummary("srv1-ops")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 2 || summary.UniqueRecords != 1 || summary.DuplicateRecords != 1 {
		t.Fatalf("expected richer same-session case to append and dedupe, got %+v", summary)
	}
	cases, err := tracker.RecentSkillValidationCases("srv1-ops", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected one effective case, got %+v", cases)
	}
	replay := cases[0].Replay
	if len(replay.ExpectedToolCalls) != 1 ||
		replay.ExpectedToolCalls[0].InputIncludes[0] != "ssh srv1" ||
		replay.ExpectedToolCalls[0].InputIncludes[1] != "systemctl --user status" ||
		len(replay.ForbiddenToolCalls) != 1 ||
		replay.ForbiddenToolCalls[0].InputIncludes[0] != "systemctl --user restart" ||
		len(replay.RequiredTools) != 1 ||
		replay.RequiredTools[0] != "exec" {
		t.Fatalf("expected richer replay to preserve recovery and forbidden calls, got %+v", replay)
	}
}

func TestSkillLifecycleValidationCaseFromSessionSkipsWeakAutomaticTrace(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	store := skillLifecycleTranscriptStore{msgs: []toolctx.ChatMessage{
		{Role: "user", Content: json.RawMessage(`"상태 확인"`)},
		{Role: "assistant", Content: json.RawMessage(`[
			{"type":"tool_use","id":"tu_1","name":"exec","input":{}}
		]`)},
	}}
	backend := &skillLifecycleBackend{tracker: tracker, transcripts: store}

	gotAny, err := backend.RecordSkillValidationCaseFromSession(context.Background(), chattools.SkillValidationCaseFromSessionRequest{
		SkillName:  "srv1-ops",
		SessionKey: "client:main:weak",
	})
	if err != nil {
		t.Fatalf("RecordSkillValidationCaseFromSession should skip weak automatic cases: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["skip"] != true {
		t.Fatalf("expected weak automatic trace to be skipped, got %+v", got)
	}
	summary, err := tracker.ValidationCaseSummary("srv1-ops")
	if err != nil {
		t.Fatalf("ValidationCaseSummary: %v", err)
	}
	if summary.RawRecords != 0 {
		t.Fatalf("expected weak automatic trace not to be stored, got %+v", summary)
	}
}

func TestSkillLifecycleValidationBackfillScansSessionsAndSkipsWeak(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	store := skillLifecycleTranscriptStore{byKey: map[string][]toolctx.ChatMessage{
		"client:main:z-valid": {
			{Role: "user", Content: json.RawMessage(`"srv1 상태를 확인"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu_1","name":"exec","input":{"cmd":"ssh srv1 uptime"}}
			]`)},
			{Role: "user", Content: json.RawMessage(`[
				{"type":"tool_result","tool_use_id":"tu_1","content":"up 12 days","is_error":false}
			]`)},
		},
		"client:main:a-weak": {
			{Role: "user", Content: json.RawMessage(`"확인"`)},
			{Role: "assistant", Content: json.RawMessage(`[
				{"type":"tool_use","id":"tu_2","name":"exec","input":{}}
			]`)},
		},
	}}
	backend := &skillLifecycleBackend{tracker: tracker, transcripts: store}

	gotAny, err := backend.BackfillSkillValidationCases(context.Background(), chattools.SkillValidationBackfillRequest{
		SkillName: "srv1-ops",
		Limit:     2,
	})
	if err != nil {
		t.Fatalf("BackfillSkillValidationCases: %v", err)
	}
	got := gotAny.(map[string]any)
	if got["scanned"] != 2 || got["recorded"] != 1 || got["skipped"] != 1 {
		t.Fatalf("unexpected backfill result: %+v", got)
	}
	summary := got["validationCaseSummary"].(genesis.SkillValidationCaseSummary)
	if summary.SkillName != "srv1-ops" || summary.RawRecords != 1 || summary.UniqueRecords != 1 || summary.AutomaticRecords != 1 {
		t.Fatalf("unexpected backfill validation summary: %+v", summary)
	}
	cases, err := tracker.RecentSkillValidationCases("srv1-ops", 5)
	if err != nil {
		t.Fatalf("RecentSkillValidationCases: %v", err)
	}
	if len(cases) != 1 ||
		cases[0].Source != "session-backfill" ||
		cases[0].ID != "session-client:main:z-valid" ||
		len(cases[0].Replay.ExpectedToolCalls) != 1 ||
		cases[0].Replay.ExpectedToolCalls[0].InputIncludes[0] != "ssh srv1" {
		t.Fatalf("unexpected backfilled case: %+v", cases)
	}

	engine := genesis.NewSkillValidationEngine(tracker, slog.Default())
	result, err := engine.ValidateCandidate(
		"srv1-ops",
		"---\nname: srv1-ops\n---\n\n# Ops\n\n## Procedure\n- Check status.\n",
		"# Ops\n\n## Procedure\n- Use exec to run ssh srv1 before changing the service.\n",
	)
	if err != nil {
		t.Fatalf("ValidateCandidate: %v", err)
	}
	if !result.Evaluated || !result.Pass {
		t.Fatalf("automatic replay should not require the one-off uptime command, got %+v", result)
	}
}

func TestSkillReplayInputIncludesGeneralizesCommandFragments(t *testing.T) {
	got := skillReplayInputIncludes(`{"cmd":"ssh srv1 systemctl --user status deneb-gateway.service","workdir":"/srv/deneb"}`)
	if len(got) != 2 || got[0] != "ssh srv1" || got[1] != "systemctl --user status" {
		t.Fatalf("expected generalized ssh/systemctl fragments, got %+v", got)
	}

	got = skillReplayInputIncludes(`{"cmd":"go test ./internal/runtime/server -run TestHandleCronRun_Success"}`)
	if len(got) != 1 || got[0] != "go test" {
		t.Fatalf("expected generic go test intent, got %+v", got)
	}

	got = skillReplayInputIncludes(`{"cmd":"ssh -o BatchMode=yes srv1 uptime"}`)
	if len(got) != 1 || got[0] != "ssh srv1" {
		t.Fatalf("expected ssh options to be skipped, got %+v", got)
	}
}

func TestSkillLifecycleLogProposalStoresActualExecutionAndTruncates(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	backend := &skillLifecycleBackend{tracker: tracker}

	backend.logProposal(chattools.SkillEvolutionProposalRequest{
		Candidate: "manual create route",
		Route:     "create",
		Execute:   true,
	}, "create", map[string]any{
		"ok":        true,
		"executed":  false,
		"largeText": strings.Repeat("x", skillLifecycleMaxProposalResultBytes+100),
	})

	entries, err := tracker.RecentLifecycleLog(1)
	if err != nil {
		t.Fatalf("RecentLifecycleLog: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one proposal entry, got %d", len(entries))
	}
	if entries[0].Executed {
		t.Fatalf("expected actual execution=false even when execute was requested: %+v", entries[0])
	}
	if !strings.HasSuffix(entries[0].Result, "...[truncated]") {
		t.Fatalf("expected truncated result, got length %d", len(entries[0].Result))
	}
}

func TestSkillLifecycleSelfCorrectionRecordReviewAndStatus(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	backend := &skillLifecycleBackend{tracker: tracker}

	gotAny, err := backend.RecordSelfCorrectionCandidate(context.Background(), chattools.SkillSelfCorrectionCandidateRequest{
		Scope:          "code",
		Title:          "Defer route threshold tweak",
		Candidate:      "adjust effort router threshold after batch review",
		Evidence:       "operator correction",
		TargetFiles:    []string{"gateway-go/internal/pipeline/chat/effort_router.go"},
		ProposedChange: "review threshold constants against agentlog",
		Risk:           "could route hard turns too cheaply",
		Source:         "operator",
	})
	if err != nil {
		t.Fatalf("RecordSelfCorrectionCandidate: %v", err)
	}
	got := gotAny.(map[string]any)
	rec := got["candidate"].(genesis.SelfCorrectionCandidateRecord)
	if rec.ID == "" || rec.Status != genesis.SelfCorrectionStatusProposed {
		t.Fatalf("unexpected candidate: %+v", rec)
	}

	statusAny, err := backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{Limit: 5})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus: %v", err)
	}
	status := statusAny.(map[string]any)
	pending := status["selfCorrectionCandidates"].([]genesis.SelfCorrectionCandidateRecord)
	if len(pending) != 1 || pending[0].ID != rec.ID {
		t.Fatalf("expected pending candidate in status, got %+v", pending)
	}

	if _, err := backend.ReviewSelfCorrectionCandidate(context.Background(), chattools.SkillSelfCorrectionReviewRequest{
		ID:         rec.ID,
		Status:     genesis.SelfCorrectionStatusRejected,
		Reviewer:   "codex",
		ReviewNote: "superseded by existing scorecard",
	}); err != nil {
		t.Fatalf("ReviewSelfCorrectionCandidate: %v", err)
	}
	statusAny, err = backend.SkillLifecycleStatus(context.Background(), chattools.SkillLifecycleStatusRequest{Limit: 5})
	if err != nil {
		t.Fatalf("SkillLifecycleStatus after review: %v", err)
	}
	status = statusAny.(map[string]any)
	pending = status["selfCorrectionCandidates"].([]genesis.SelfCorrectionCandidateRecord)
	if len(pending) != 0 {
		t.Fatalf("reviewed candidate should leave pending status view, got %+v", pending)
	}
}

func TestSkillLifecycleCuratorActionsPinArchiveRestore(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	if err := tracker.LogGenesis("generated-helper", "session", "telegram:1", "coding", "Generated helper"); err != nil {
		t.Fatalf("LogGenesis: %v", err)
	}
	backend := &skillLifecycleBackend{tracker: tracker}

	if _, err := backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "pin",
		SkillName: "generated-helper",
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	report, err := tracker.SkillCuratorReport("generated-helper")
	if err != nil {
		t.Fatalf("SkillCuratorReport: %v", err)
	}
	if len(report) != 1 || !report[0].Pinned {
		t.Fatalf("expected pinned curator record, got %+v", report)
	}

	gotAny, err := backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "archive",
		SkillName: "generated-helper",
	})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	got := gotAny.(map[string]any)
	rec := got["curator"].(genesis.SkillCuratorRecord)
	if rec.State != genesis.SkillCuratorStateArchived || rec.ArchivedAt == 0 {
		t.Fatalf("expected archived record, got %+v", rec)
	}

	gotAny, err = backend.RunSkillCuratorAction(context.Background(), chattools.SkillCuratorActionRequest{
		Action:    "restore",
		SkillName: "generated-helper",
	})
	if err != nil {
		t.Fatalf("restore: %v", err)
	}
	got = gotAny.(map[string]any)
	rec = got["curator"].(genesis.SkillCuratorRecord)
	if rec.State != genesis.SkillCuratorStateActive || rec.ArchivedAt != 0 {
		t.Fatalf("expected restored active record, got %+v", rec)
	}
}

// TestProposeSkillEvolution_NoOpWithoutCandidate verifies that a no-op proposal
// succeeds without a candidate. A no-op records "no skill-worthy pattern,
// nothing to do", so a reusable candidate is optional by definition.
// Regression for the reviewer agent's repeated "candidate is required for
// propose" failures, which forced every no-op review to error out.
func TestProposeSkillEvolution_NoOpWithoutCandidate(t *testing.T) {
	// nil tracker/genesis: logProposal no-ops and no route executes.
	b := &skillLifecycleBackend{}
	res, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route:      "no-op",
		Reason:     "existing skill already covers this",
		SessionKey: "test:session",
	})
	if err != nil {
		t.Fatalf("no-op without candidate should succeed, got: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map result, got %T", res)
	}
	if m["route"] != "no-op" {
		t.Errorf("expected route=no-op, got %v", m["route"])
	}
}

func TestProposeSkillEvolution_RecordsOpportunityBacklog(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	b := &skillLifecycleBackend{tracker: tracker}

	_, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route:      "no-op",
		SkillName:  "deploy-helper",
		SessionKey: "client:main",
		Reason:     "candidate is weak once, but useful if repeated",
		Evidence:   "user corrected merge verification",
	})
	if err != nil {
		t.Fatalf("ProposeSkillEvolution: %v", err)
	}

	records, err := tracker.RecentSkillOpportunities("deploy-helper", 5)
	if err != nil {
		t.Fatalf("RecentSkillOpportunities: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one opportunity record, got %+v", records)
	}
	got := records[0]
	if got.Route != "no-op" || got.SkillName != "deploy-helper" || got.Source != "skill_lifecycle" || got.Reason == "" {
		t.Fatalf("unexpected opportunity record: %+v", got)
	}
}

// TestProposeSkillEvolution_ExecutableRouteRequiresCandidate ensures executable
// routes (genesis/create/evolve) still require a candidate — the no-op
// exemption must not weaken validation for routes that actually do work.
func TestProposeSkillEvolution_ExecutableRouteRequiresCandidate(t *testing.T) {
	b := &skillLifecycleBackend{}
	_, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route:      "evolve",
		SessionKey: "test:session",
	})
	if err == nil {
		t.Fatal("expected error: candidate required for executable route, got nil")
	}
}

// TestProposeSkillEvolution_VerdictExcludedFromSuccessRate verifies a review
// verdict is recorded (it still drives the curator's staleness/lastUsed signal)
// but is NOT counted toward the evolver's success-rate stats. A judgment is not
// a real execution; conflating them pinned email-analysis as a phantom
// underperformer that re-evolved six times in two days (PR #2328). The
// success-rate now reflects real use only, so a pair of verdicts leaves it empty.
func TestProposeSkillEvolution_VerdictExcludedFromSuccessRate(t *testing.T) {
	tracker := newSkillLifecycleTestTracker(t)
	b := &skillLifecycleBackend{tracker: tracker}

	if _, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route: "no-op", SkillName: "email-analysis", Reason: "skill already covers it", SessionKey: "s1",
	}); err != nil {
		t.Fatalf("no-op: %v", err)
	}
	if _, err := b.ProposeSkillEvolution(context.Background(), chattools.SkillEvolutionProposalRequest{
		Route: "evolve", SkillName: "email-analysis", Candidate: "add category param", Reason: "category missing", SessionKey: "s2",
	}); err != nil {
		t.Fatalf("evolve: %v", err)
	}

	stats, err := tracker.Stats("email-analysis")
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalUses != 0 {
		t.Fatalf("review verdicts must not feed the success rate, got total=%d", stats.TotalUses)
	}
}
