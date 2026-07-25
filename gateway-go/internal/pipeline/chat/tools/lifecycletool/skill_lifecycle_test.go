package lifecycletool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
)

type fakeSkillLifecycleBackend struct {
	proposal       SkillEvolutionProposalRequest
	genesis        SkillGenesisRequest
	evolve         SkillEvolutionRequest
	status         SkillLifecycleStatusRequest
	curator        SkillCuratorActionRequest
	validationCase SkillValidationCaseRequest
	fromSession    SkillValidationCaseFromSessionRequest
	backfill       SkillValidationBackfillRequest
	selfCorrection SkillSelfCorrectionCandidateRequest
	selfReview     SkillSelfCorrectionReviewRequest
	shadowReplay   HeartbeatShadowReplayRequest
}

func (f *fakeSkillLifecycleBackend) ProposeSkillEvolution(_ context.Context, req SkillEvolutionProposalRequest) (SkillEvolutionProposalResult, error) {
	f.proposal = req
	return SkillEvolutionProposalResult{OK: true, Candidate: req.Candidate, Route: req.Route, Executed: req.Execute}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillGenesis(_ context.Context, req SkillGenesisRequest) (SkillGenesisResult, error) {
	f.genesis = req
	return SkillGenesisResult{OK: true, Source: req.SessionKey}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillEvolution(_ context.Context, req SkillEvolutionRequest) (SkillEvolutionResult, error) {
	f.evolve = req
	return SkillEvolutionResult{OK: true, Result: &genesis.EvolveResult{SkillName: req.SkillName}}, nil
}

func (f *fakeSkillLifecycleBackend) SkillLifecycleStatus(_ context.Context, req SkillLifecycleStatusRequest) (SkillLifecycleStatusResult, error) {
	f.status = req
	return SkillLifecycleStatusResult{
		Overview: SkillLifecycleOverview{Unavailable: &SkillLifecycleUnavailableOverview{
			State: "unavailable", Scope: "skill", SkillName: req.SkillName, NextActions: []string{},
		}},
		OK:        true,
		SkillName: req.SkillName,
		Limit:     fakeLifecycleValue(req.Limit),
	}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillCuratorAction(_ context.Context, req SkillCuratorActionRequest) (SkillCuratorActionResult, error) {
	f.curator = req
	return SkillCuratorActionResult{OK: true, Action: fakeLifecycleValue(req.Action), SkillName: fakeLifecycleValue(req.SkillName)}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSkillValidationCase(_ context.Context, req SkillValidationCaseRequest) (SkillValidationCaseResult, error) {
	f.validationCase = req
	return SkillValidationCaseResult{OK: true, SkillName: fakeLifecycleValue(req.SkillName), ID: fakeLifecycleValue(req.ID)}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSkillValidationCaseFromSession(_ context.Context, req SkillValidationCaseFromSessionRequest) (SkillValidationCaseFromSessionResult, error) {
	f.fromSession = req
	return SkillValidationCaseFromSessionResult{
		OK: true, SkillName: fakeLifecycleValue(req.SkillName), ID: fakeLifecycleValue(req.ID), SessionKey: fakeLifecycleValue(req.SessionKey),
	}, nil
}

func (f *fakeSkillLifecycleBackend) BackfillSkillValidationCases(_ context.Context, req SkillValidationBackfillRequest) (SkillValidationBackfillResult, error) {
	f.backfill = req
	return SkillValidationBackfillResult{OK: true, SkillName: fakeLifecycleValue(req.SkillName), Limit: fakeLifecycleValue(req.Limit)}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSelfCorrectionCandidate(_ context.Context, req SkillSelfCorrectionCandidateRequest) (SkillSelfCorrectionCandidateResult, error) {
	f.selfCorrection = req
	return SkillSelfCorrectionCandidateResult{OK: true, Candidate: &genesis.SelfCorrectionCandidateRecord{Title: req.Title, Scope: req.Scope}}, nil
}

func (f *fakeSkillLifecycleBackend) ReviewSelfCorrectionCandidate(_ context.Context, req SkillSelfCorrectionReviewRequest) (SkillSelfCorrectionReviewResult, error) {
	f.selfReview = req
	return SkillSelfCorrectionReviewResult{OK: true, Review: &genesis.SelfCorrectionCandidateRecord{ID: req.ID, Status: req.Status}}, nil
}

func TestToolSkillLifecycleCreatesProposal(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":     "propose",
		"candidate":  "repeatable deploy fix",
		"route":      "genesis",
		"sessionKey": "telegram:1",
		"execute":    true,
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"executed":true`) {
		t.Fatalf("expected executed result, got %s", out)
	}
	if backend.proposal.Candidate != "repeatable deploy fix" || backend.proposal.Route != "genesis" || !backend.proposal.Execute {
		t.Fatalf("unexpected proposal request: %+v", backend.proposal)
	}
}

func TestToolSkillLifecycleStartsGenesis(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	if _, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":     "genesis",
		"sessionKey": "telegram:1",
	})); err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if backend.genesis.SessionKey != "telegram:1" {
		t.Fatalf("unexpected genesis request: %+v", backend.genesis)
	}
}

func TestToolSkillLifecycleEvolveUpdatesSkill(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	if _, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":    "evolve",
		"skillName": "skill-factory",
		"finding":   "add validation gate",
	})); err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if backend.evolve.SkillName != "skill-factory" || backend.evolve.Finding != "add validation gate" {
		t.Fatalf("unexpected evolve request: %+v", backend.evolve)
	}
}

func TestToolSkillLifecycleReadsSkillStatus(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":    "status",
		"skillName": "skill-factory",
		"limit":     3,
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"limit":3`) {
		t.Fatalf("expected status result, got %s", out)
	}
	if backend.status.SkillName != "skill-factory" || backend.status.Limit != 3 {
		t.Fatalf("unexpected status request: %+v", backend.status)
	}
}

func TestSkillLifecycleToolDescriptionDocumentsActionRequirements(t *testing.T) {
	desc := SkillLifecycleToolDescription()
	for _, want := range []string{
		"propose requires route",
		"candidate unless route=no-op",
		"self_correction_review requires id",
		"validation_case requires skillName",
		"heartbeat_shadow_replay requires candidate",
	} {
		if !strings.Contains(desc, want) {
			t.Fatalf("SkillLifecycleToolDescription missing %q:\n%s", want, desc)
		}
	}
}

func TestSkillLifecycleToolSchemaDocumentsActionRequirements(t *testing.T) {
	schema := SkillLifecycleToolSchema()
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties missing: %#v", schema["properties"])
	}

	for field, want := range map[string]string{
		"action":    "Action requirements:",
		"route":     "Required for action=propose",
		"skillName": "Required for action=evolve",
		"id":        "self_correction_review: required candidate id",
		"title":     "At least one of title, candidate, or proposedChange is required",
		"replay":    "at least one replay assertion",
	} {
		prop, ok := properties[field].(map[string]any)
		if !ok {
			t.Fatalf("schema property %q missing: %#v", field, properties[field])
		}
		desc, ok := prop["description"].(string)
		if !ok || !strings.Contains(desc, want) {
			t.Fatalf("schema property %q description missing %q:\n%v", field, want, prop["description"])
		}
	}
}

func TestToolSkillLifecycleCreatesSelfCorrectionCandidate(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":         "self_correction",
		"scope":          "skill",
		"skillName":      "email-analysis",
		"title":          "Defer noisy mail rewrite",
		"candidate":      "tighten calendar extraction",
		"targetFiles":    []string{"skills/productivity/email-analysis/SKILL.md"},
		"proposedChange": "add proposal-only guard",
		"risk":           "may suppress a valid event",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"scope":"skill"`) {
		t.Fatalf("expected self-correction result, got %s", out)
	}
	if backend.selfCorrection.SkillName != "email-analysis" || backend.selfCorrection.Scope != "skill" {
		t.Fatalf("unexpected self-correction request: %+v", backend.selfCorrection)
	}
	if len(backend.selfCorrection.TargetFiles) != 1 {
		t.Fatalf("expected target file, got %+v", backend.selfCorrection.TargetFiles)
	}
}

func TestToolSkillLifecycleUpdatesSelfCorrectionReview(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	if _, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":     "self_correction_review",
		"id":         "sc-1",
		"status":     "accepted",
		"reviewer":   "codex",
		"reviewNote": "batch accepted",
	})); err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if backend.selfReview.ID != "sc-1" || backend.selfReview.Status != "accepted" || backend.selfReview.Reviewer != "codex" {
		t.Fatalf("unexpected self-correction review request: %+v", backend.selfReview)
	}
}

func TestToolSkillLifecycleUpdatesSkillViaCuratorAction(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":    "archive",
		"skillName": "generated-helper",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"action":"archive"`) {
		t.Fatalf("expected archive result, got %s", out)
	}
	if backend.curator.Action != "archive" || backend.curator.SkillName != "generated-helper" {
		t.Fatalf("unexpected curator request: %+v", backend.curator)
	}
}

func TestToolSkillLifecycleCreatesValidationCase(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":              "validation_case",
		"skillName":           "topsolar-db",
		"id":                  "preserve-single-bash-block",
		"description":         "candidate must preserve safe execution wrapper",
		"requiredSubstrings":  []string{"단일 bash block"},
		"forbiddenSubstrings": []string{"eval"},
		"requiredHeadings":    []string{"통합 실행 흐름"},
		"replay": map[string]any{
			"input":                 "srv1에서 실제 deneb-gateway 상태를 확인하고 개선",
			"requiredActions":       []string{"ssh srv1", "systemctl --user status deneb-gateway.service"},
			"forbiddenActions":      []string{"로컬 상태만 보고 판단"},
			"requiredObservations":  []string{"active (running)"},
			"forbiddenObservations": []string{"stopped"},
			"requiredTools":         []string{"ssh"},
			"expectedToolCalls": []map[string]any{
				{"name": "exec", "inputIncludes": []string{"ssh srv1"}},
				{"name": "exec", "inputIncludes": []string{"systemctl --user status deneb-gateway.service"}, "fixtureOutput": "Active: active (running)"},
			},
			"forbiddenToolCalls": []map[string]any{
				{"name": "exec", "inputIncludes": []string{"rm -rf"}},
			},
			"requireOrder": true,
		},
		"source": "operator",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"id":"preserve-single-bash-block"`) {
		t.Fatalf("expected validation case result, got %s", out)
	}
	if backend.validationCase.SkillName != "topsolar-db" ||
		backend.validationCase.ID != "preserve-single-bash-block" ||
		len(backend.validationCase.RequiredSubstrings) != 1 ||
		len(backend.validationCase.ForbiddenSubstrings) != 1 ||
		len(backend.validationCase.RequiredHeadings) != 1 ||
		backend.validationCase.Replay.Input == "" ||
		len(backend.validationCase.Replay.RequiredActions) != 2 ||
		len(backend.validationCase.Replay.ForbiddenActions) != 1 ||
		len(backend.validationCase.Replay.RequiredObservations) != 1 ||
		len(backend.validationCase.Replay.ForbiddenObservations) != 1 ||
		len(backend.validationCase.Replay.RequiredTools) != 1 ||
		len(backend.validationCase.Replay.ExpectedToolCalls) != 2 ||
		backend.validationCase.Replay.ExpectedToolCalls[1].FixtureOutput == "" ||
		len(backend.validationCase.Replay.ForbiddenToolCalls) != 1 ||
		!backend.validationCase.Replay.RequireOrder {
		t.Fatalf("unexpected validation case request: %+v", backend.validationCase)
	}
}

func TestToolSkillLifecycleCreatesValidationCaseFromSession(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":      "validation_case_from_session",
		"skillName":   "srv1-ops",
		"sessionKey":  "client:main:srv1",
		"id":          "real-server-state",
		"description": "preserve real server inspection before edits",
		"replay": map[string]any{
			"requiredActions":      []string{"ssh srv1"},
			"requiredObservations": []string{"active (running)"},
		},
		"source": "review-finding",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"sessionKey":"client:main:srv1"`) {
		t.Fatalf("expected validation case result, got %s", out)
	}
	if backend.fromSession.SkillName != "srv1-ops" ||
		backend.fromSession.SessionKey != "client:main:srv1" ||
		backend.fromSession.ID != "real-server-state" ||
		len(backend.fromSession.Replay.RequiredActions) != 1 ||
		len(backend.fromSession.Replay.RequiredObservations) != 1 ||
		backend.fromSession.Source != "review-finding" {
		t.Fatalf("unexpected from-session request: %+v", backend.fromSession)
	}
}

func TestToolSkillLifecycleCreatesBackfilledValidationCases(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":      "validation_backfill",
		"skillName":   "srv1-ops",
		"sessionKey":  "client:main:srv1",
		"limit":       7,
		"description": "backfill real server checks",
		"replay": map[string]any{
			"requiredActions": []string{"ssh srv1"},
		},
		"source": "operator-backfill",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"limit":7`) {
		t.Fatalf("expected backfill result, got %s", out)
	}
	if backend.backfill.SkillName != "srv1-ops" ||
		backend.backfill.SessionKey != "client:main:srv1" ||
		backend.backfill.Limit != 7 ||
		backend.backfill.Description != "backfill real server checks" ||
		len(backend.backfill.Replay.RequiredActions) != 1 ||
		backend.backfill.Source != "operator-backfill" {
		t.Fatalf("unexpected backfill request: %+v", backend.backfill)
	}
}

func mustJSONSkillLifecycle(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func (f *fakeSkillLifecycleBackend) HeartbeatShadowReplay(_ context.Context, req HeartbeatShadowReplayRequest) (HeartbeatShadowReplayResult, error) {
	f.shadowReplay = req
	return HeartbeatShadowReplayResult{OK: true, DryRun: true}, nil
}

func fakeLifecycleValue[T any](value T) *T {
	return &value
}
