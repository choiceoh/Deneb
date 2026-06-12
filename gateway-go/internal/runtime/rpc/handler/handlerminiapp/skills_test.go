package handlerminiapp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/infra/clientauth"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func authedSkillsCtx() context.Context {
	return clientauth.WithContext(context.Background(), &clientauth.Identity{})
}

func testSkillsDeps() SkillsDeps {
	return SkillsDeps{
		List: func() []skills.SkillEntry {
			return []skills.SkillEntry{
				{Skill: skills.Skill{
					Name: "email-analysis", Version: "1.1.0", Source: skills.SourceManaged,
					FilePath: "/home/u/.deneb/skills/email-analysis/SKILL.md",
				}},
				{Skill: skills.Skill{
					Name: "morning-letter", Version: "0.1.0", Source: skills.SourceManaged,
					FilePath: "/home/u/.deneb/skills/genesis/productivity/morning-letter/SKILL.md",
				}},
			}
		},
		CuratorRecords: func() ([]genesis.SkillCuratorRecord, error) {
			return []genesis.SkillCuratorRecord{{
				SkillName: "morning-letter",
				CreatedBy: genesis.SkillCuratorCreatedByAgent,
				State:     genesis.SkillCuratorStateActive,
				CreatedAt: 111,
			}}, nil
		},
		UsageStats: func() ([]genesis.UsageStats, error) {
			return []genesis.UsageStats{{SkillName: "email-analysis", TotalUses: 7, LastUsed: 222}}, nil
		},
		RecentLifecycle: func(limit int) ([]genesis.LifecycleLogEntry, error) {
			return []genesis.LifecycleLogEntry{
				{Type: "evolved", SkillName: "email-analysis", NewVersion: "1.1.1", Description: "개선", CreatedAt: 333},
				{Type: "evolution_proposal", SkillName: "email-analysis", Route: "no-op", Reason: "기존 커버", CreatedAt: 300},
				{Type: "evolve_rejected", SkillName: "email-analysis", Reason: "judge 기각", CreatedAt: 250},
				{Type: "evolved", SkillName: "email-analysis", NewVersion: "1.1.0", CreatedAt: 200},
				{Type: "genesis", SkillName: "morning-letter", Description: "생성", CreatedAt: 111},
			}, nil
		},
	}
}

func decodeSkillsPayload[T any](t *testing.T, resp *protocol.ResponseFrame) T {
	t.Helper()
	if resp == nil || !resp.OK {
		t.Fatalf("expected OK response, got %+v", resp)
	}
	var out T
	if err := json.Unmarshal(resp.Payload, &out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	return out
}

func TestSkillsList_OriginAndEvolveEnrichment(t *testing.T) {
	h := skillsList(testSkillsDeps())
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.list"})
	payload := decodeSkillsPayload[SkillsListResponse](t, resp)

	if payload.Count != 2 {
		t.Fatalf("expected 2 skills, got %d", payload.Count)
	}
	byName := map[string]SkillRow{}
	for _, r := range payload.Skills {
		byName[r.Name] = r
	}

	ea := byName["email-analysis"]
	if ea.Origin != skillOriginInitial {
		t.Errorf("email-analysis origin = %q, want initial", ea.Origin)
	}
	if ea.EvolveCount != 2 || ea.LastEvolvedAt != 333 {
		t.Errorf("email-analysis evolve agg = (%d, %d), want (2, 333)", ea.EvolveCount, ea.LastEvolvedAt)
	}
	if ea.TotalUses != 7 || ea.LastUsedAt != 222 {
		t.Errorf("email-analysis usage = (%d, %d), want (7, 222)", ea.TotalUses, ea.LastUsedAt)
	}
	if ea.CuratorState != "" {
		t.Errorf("initial skill must not carry curator state, got %q", ea.CuratorState)
	}

	ml := byName["morning-letter"]
	if ml.Origin != skillOriginGenesis {
		t.Errorf("morning-letter origin = %q, want genesis", ml.Origin)
	}
	if ml.CreatedAt != 111 || ml.CuratorState != genesis.SkillCuratorStateActive {
		t.Errorf("morning-letter curator fields = (%d, %q), want (111, active)", ml.CreatedAt, ml.CuratorState)
	}
}

// A generated skill that predates the curator marker is still classified by
// its on-disk location under the genesis output dir.
func TestSkillsList_GenesisDirFallbackOrigin(t *testing.T) {
	deps := testSkillsDeps()
	deps.CuratorRecords = nil
	h := skillsList(deps)
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.list"})
	payload := decodeSkillsPayload[SkillsListResponse](t, resp)
	for _, r := range payload.Skills {
		if r.Name == "morning-letter" && r.Origin != skillOriginGenesis {
			t.Errorf("genesis-dir skill origin = %q, want genesis", r.Origin)
		}
	}
}

func TestSkillsLifecycle_MappingAndLimit(t *testing.T) {
	h := skillsLifecycle(testSkillsDeps())
	params, _ := json.Marshal(map[string]any{"limit": 3})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.lifecycle", Params: params})
	payload := decodeSkillsPayload[SkillsLifecycleResponse](t, resp)

	if payload.Count != 3 {
		t.Fatalf("expected 3 events (limit), got %d", payload.Count)
	}
	first := payload.Events[0]
	if first.Type != "evolved" || first.Version != "1.1.1" || first.Detail != "개선" {
		t.Errorf("first event = %+v, want evolved/1.1.1/개선", first)
	}
	second := payload.Events[1]
	if second.Type != "review" || second.Route != "no-op" || second.Detail != "기존 커버" {
		t.Errorf("proposal must map to review verdict, got %+v", second)
	}
	third := payload.Events[2]
	if third.Type != "evolve_rejected" || third.Detail != "judge 기각" {
		t.Errorf("third event = %+v, want evolve_rejected", third)
	}
}

func TestSkillsLifecycle_SkillFilter(t *testing.T) {
	h := skillsLifecycle(testSkillsDeps())
	params, _ := json.Marshal(map[string]any{"skillName": "morning-letter"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.lifecycle", Params: params})
	payload := decodeSkillsPayload[SkillsLifecycleResponse](t, resp)

	if payload.Count != 1 || payload.Events[0].Type != "genesis" {
		t.Fatalf("expected single genesis event for morning-letter, got %+v", payload.Events)
	}
}

// Without a tracker the feed degrades to empty instead of erroring.
func TestSkillsLifecycle_NilProvider(t *testing.T) {
	deps := testSkillsDeps()
	deps.RecentLifecycle = nil
	h := skillsLifecycle(deps)
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.lifecycle"})
	payload := decodeSkillsPayload[SkillsLifecycleResponse](t, resp)
	if payload.Count != 0 {
		t.Fatalf("expected empty feed, got %d events", payload.Count)
	}
}
