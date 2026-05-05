package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type fakeSkillLifecycleBackend struct {
	proposal SkillEvolutionProposalRequest
	genesis  SkillGenesisRequest
	evolve   SkillEvolutionRequest
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
	})); err != nil {
		t.Fatalf("ToolSkillLifecycle: %v", err)
	}
	if backend.evolve.SkillName != "skill-factory" {
		t.Fatalf("unexpected evolve request: %+v", backend.evolve)
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
