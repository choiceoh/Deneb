package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
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
}

func (f *fakeSkillLifecycleBackend) ProposeSkillEvolution(_ context.Context, req SkillEvolutionProposalRequest) (any, error) {
	f.proposal = req
	return map[string]any{"ok": true, "route": req.Route, "executed": req.Execute}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillGenesis(_ context.Context, req SkillGenesisRequest) (any, error) {
	f.genesis = req
	return map[string]any{"ok": true, "source": req.SessionKey}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillEvolution(_ context.Context, req SkillEvolutionRequest) (any, error) {
	f.evolve = req
	return map[string]any{"ok": true, "skillName": req.SkillName}, nil
}

func (f *fakeSkillLifecycleBackend) SkillLifecycleStatus(_ context.Context, req SkillLifecycleStatusRequest) (any, error) {
	f.status = req
	return map[string]any{"ok": true, "limit": req.Limit, "skillName": req.SkillName}, nil
}

func (f *fakeSkillLifecycleBackend) RunSkillCuratorAction(_ context.Context, req SkillCuratorActionRequest) (any, error) {
	f.curator = req
	return map[string]any{"ok": true, "action": req.Action, "skillName": req.SkillName}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSkillValidationCase(_ context.Context, req SkillValidationCaseRequest) (any, error) {
	f.validationCase = req
	return map[string]any{"ok": true, "skillName": req.SkillName, "id": req.ID}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSkillValidationCaseFromSession(_ context.Context, req SkillValidationCaseFromSessionRequest) (any, error) {
	f.fromSession = req
	return map[string]any{"ok": true, "skillName": req.SkillName, "id": req.ID, "sessionKey": req.SessionKey}, nil
}

func (f *fakeSkillLifecycleBackend) BackfillSkillValidationCases(_ context.Context, req SkillValidationBackfillRequest) (any, error) {
	f.backfill = req
	return map[string]any{"ok": true, "skillName": req.SkillName, "limit": req.Limit, "sessionKey": req.SessionKey}, nil
}

func (f *fakeSkillLifecycleBackend) RecordSelfCorrectionCandidate(_ context.Context, req SkillSelfCorrectionCandidateRequest) (any, error) {
	f.selfCorrection = req
	return map[string]any{"ok": true, "title": req.Title, "scope": req.Scope}, nil
}

func (f *fakeSkillLifecycleBackend) ReviewSelfCorrectionCandidate(_ context.Context, req SkillSelfCorrectionReviewRequest) (any, error) {
	f.selfReview = req
	return map[string]any{"ok": true, "id": req.ID, "status": req.Status}, nil
}

func TestToolSkillLifecyclePropose(t *testing.T) {
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
	if !strings.Contains(out, `"executed": true`) {
		t.Fatalf("expected executed result, got %s", out)
	}
	if backend.proposal.Candidate != "repeatable deploy fix" || backend.proposal.Route != "genesis" || !backend.proposal.Execute {
		t.Fatalf("unexpected proposal request: %+v", backend.proposal)
	}
}

func TestToolSkillLifecycleGenesis(t *testing.T) {
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

func TestToolSkillLifecycleEvolve(t *testing.T) {
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

func TestToolSkillLifecycleStatus(t *testing.T) {
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
	if !strings.Contains(out, `"limit": 3`) {
		t.Fatalf("expected status result, got %s", out)
	}
	if backend.status.SkillName != "skill-factory" || backend.status.Limit != 3 {
		t.Fatalf("unexpected status request: %+v", backend.status)
	}
}

func TestToolSkillLifecycleSelfCorrection(t *testing.T) {
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
	if !strings.Contains(out, `"scope": "skill"`) {
		t.Fatalf("expected self-correction result, got %s", out)
	}
	if backend.selfCorrection.SkillName != "email-analysis" || backend.selfCorrection.Scope != "skill" {
		t.Fatalf("unexpected self-correction request: %+v", backend.selfCorrection)
	}
	if len(backend.selfCorrection.TargetFiles) != 1 {
		t.Fatalf("expected target file, got %+v", backend.selfCorrection.TargetFiles)
	}
}

func TestToolSkillLifecycleSelfCorrectionReview(t *testing.T) {
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

func TestToolSkillLifecycleCuratorAction(t *testing.T) {
	backend := &fakeSkillLifecycleBackend{}
	fn := ToolSkillLifecycle(backend)

	out, err := fn(context.Background(), mustJSONSkillLifecycle(t, map[string]any{
		"action":    "archive",
		"skillName": "generated-helper",
	}))
	if err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if !strings.Contains(out, `"action": "archive"`) {
		t.Fatalf("expected archive result, got %s", out)
	}
	if backend.curator.Action != "archive" || backend.curator.SkillName != "generated-helper" {
		t.Fatalf("unexpected curator request: %+v", backend.curator)
	}
}

func TestToolSkillLifecycleValidationCase(t *testing.T) {
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
	if !strings.Contains(out, `"id": "preserve-single-bash-block"`) {
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

func TestToolSkillLifecycleValidationCaseFromSession(t *testing.T) {
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
	if !strings.Contains(out, `"sessionKey": "client:main:srv1"`) {
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

func TestToolSkillLifecycleValidationBackfill(t *testing.T) {
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
	if !strings.Contains(out, `"limit": 7`) {
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
