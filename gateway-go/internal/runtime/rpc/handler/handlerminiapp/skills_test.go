package handlerminiapp

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
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
				},
					Frontmatter: skills.ParsedFrontmatter{"homepage": "https://deneb.local/skills/email-analysis"},
					Metadata: &skills.DenebSkillMetadata{
						Tags:          []string{"mail", "analysis"},
						RelatedSkills: []string{"meeting-minutes"},
						Requires:      &skills.SkillRequires{Bins: []string{"gh"}, Env: []string{"GMAIL_TOKEN"}},
						Install:       []skills.SkillInstallSpec{{Kind: "brew", Formula: "gh", Label: "Install GitHub CLI"}},
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
			audit := &genesis.HarnessEditAudit{
				TargetSignature:        "terminal=timeout|mechanism=bounded-execution",
				EditedSurface:          "Procedure",
				ExpectedBehaviorChange: "bounded recovery",
				RegressionRisk:         "preserve verification",
			}
			return []genesis.LifecycleLogEntry{
				{Type: "evolved", SkillName: "email-analysis", NewVersion: "1.1.1", Description: "개선", CreatedAt: 333, SelfHarnessAudit: audit},
				{Type: "evolution_proposal", SkillName: "email-analysis", Route: "no-op", Reason: "기존 커버", Evidence: "세션 관찰 기록", CreatedAt: 300},
				{Type: "evolve_rejected", SkillName: "email-analysis", Reason: "judge 기각", CreatedAt: 250},
				{Type: "evolve_rolled_back", SkillName: "email-analysis", Reason: "post-evolve rollback fired", CreatedAt: 225},
				{Type: "evolved", SkillName: "email-analysis", NewVersion: "1.1.0", CreatedAt: 200},
				{Type: "genesis", SkillName: "morning-letter", Description: "생성", CreatedAt: 111},
			}, nil
		},
		ValidationSummary: func(skillName string) (genesis.SkillValidationCaseSummary, error) {
			return genesis.SkillValidationCaseSummary{
				SkillName:                skillName,
				RawRecords:               2,
				UniqueRecords:            2,
				UniqueEasyAnchorCases:    1,
				UniqueMixedFrontierCases: 1,
				SkillsWithCases:          1,
			}, nil
		},
		RecentOpportunities: func(skillName string, limit int) ([]genesis.SkillOpportunityRecord, error) {
			if skillName == "morning-letter" {
				return nil, nil
			}
			return []genesis.SkillOpportunityRecord{{
				SkillName: skillName,
				Candidate: "record frontier tiers",
				Route:     "evolve",
				Evidence:  "Propus coverage check",
			}}, nil
		},
		RecentSelfCorrections: func(skillName string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
			return nil, nil
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
	if ea.Homepage != "https://deneb.local/skills/email-analysis" ||
		len(ea.Tags) != 2 ||
		len(ea.RelatedSkills) != 1 ||
		len(ea.DependencySummary) != 2 ||
		len(ea.InstallSummary) != 1 {
		t.Errorf("email-analysis metadata not exposed: %+v", ea)
	}
	if ea.CuratorState != "" {
		t.Errorf("initial skill must not carry curator state, got %q", ea.CuratorState)
	}
	if !ea.Editable || !ea.Deletable {
		t.Errorf("managed skill mutability = (editable=%v, deletable=%v), want true/true", ea.Editable, ea.Deletable)
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

// Detail returns the same enriched row as the list plus the SKILL.md body
// read from the entry's FilePath.
func TestSkillsDetail_RowAndBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	body := "---\nname: email-analysis\n---\n\n# 메일 분석\n\n절차…"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := testSkillsDeps()
	base := deps.List
	deps.List = func() []skills.SkillEntry {
		entries := base()
		entries[0].Skill.FilePath = path
		return entries
	}

	h := skillsDetail(deps)
	params, _ := json.Marshal(map[string]any{"name": "email-analysis"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.detail", Params: params})
	payload := decodeSkillsPayload[SkillDetailResponse](t, resp)

	if payload.Skill.Name != "email-analysis" || payload.Skill.EvolveCount != 2 || payload.Skill.TotalUses != 7 {
		t.Errorf("detail row not enriched like the list: %+v", payload.Skill)
	}
	if payload.Body != body || payload.BodyTruncated {
		t.Errorf("body = %q (truncated=%v), want the file content", payload.Body, payload.BodyTruncated)
	}
	if payload.Path != path {
		t.Errorf("path = %q, want %q", payload.Path, path)
	}
}

func TestSkillsUpdate_WritesBodyAndInvalidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: email-analysis\n---\n\n# Old\n"
	updated := "---\nname: email-analysis\nversion: 1.2.0\n---\n\n# New\n\n절차를 갱신합니다.\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	invalidated := 0
	deps := testSkillsDeps()
	base := deps.List
	deps.List = func() []skills.SkillEntry {
		entries := base()
		entries[0].Skill.FilePath = path
		return entries[:1]
	}
	deps.InvalidateSkills = func() { invalidated++ }

	h := skillsUpdate(deps)
	params, _ := json.Marshal(map[string]any{"name": "email-analysis", "body": updated})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.update", Params: params})
	payload := decodeSkillsPayload[SkillDetailResponse](t, resp)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != updated {
		t.Errorf("file body = %q, want updated body", string(data))
	}
	if payload.Body != updated || payload.Skill.Name != "email-analysis" || !payload.Skill.Editable {
		t.Errorf("unexpected update response: %+v", payload)
	}
	if invalidated != 1 {
		t.Errorf("InvalidateSkills calls = %d, want 1", invalidated)
	}
}

func TestSkillsUpdate_RejectsRenameAndImmutableSources(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "SKILL.md")
	original := "---\nname: email-analysis\n---\n\n# Old\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := testSkillsDeps()
	base := deps.List
	deps.List = func() []skills.SkillEntry {
		entries := base()
		entries[0].Skill.FilePath = path
		return entries[:1]
	}
	params, _ := json.Marshal(map[string]any{"name": "email-analysis", "body": "---\nname: other\n---\n\n# Rename\n"})
	if resp := skillsUpdate(deps)(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.update", Params: params}); resp.OK {
		t.Fatal("expected rename attempt to fail")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Errorf("rename failure changed file to %q", string(data))
	}

	deps.List = func() []skills.SkillEntry {
		entries := base()
		entries[0].Skill.FilePath = path
		entries[0].Skill.Source = skills.SourceBundled
		return entries[:1]
	}
	params, _ = json.Marshal(map[string]any{"name": "email-analysis", "body": original})
	if resp := skillsUpdate(deps)(authedSkillsCtx(), &protocol.RequestFrame{ID: "2", Method: "miniapp.skills.update", Params: params}); resp.OK {
		t.Fatal("expected bundled skill update to fail")
	}
}

func TestSkillsDelete_RemovesSkillDirectoryAndInvalidates(t *testing.T) {
	root := t.TempDir()
	skillDir := filepath.Join(root, "email-analysis")
	path := filepath.Join(skillDir, "SKILL.md")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: email-analysis\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "notes.md"), []byte("sidecar"), 0o644); err != nil {
		t.Fatal(err)
	}

	invalidated := 0
	deps := testSkillsDeps()
	base := deps.List
	deps.List = func() []skills.SkillEntry {
		entries := base()
		entries[0].Skill.FilePath = path
		return entries[:1]
	}
	deps.InvalidateSkills = func() { invalidated++ }

	params, _ := json.Marshal(map[string]any{"name": "email-analysis"})
	resp := skillsDelete(deps)(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.delete", Params: params})
	if resp == nil || !resp.OK {
		t.Fatalf("expected OK delete response, got %+v", resp)
	}
	if _, err := os.Stat(skillDir); !os.IsNotExist(err) {
		t.Fatalf("skill dir still exists or stat failed unexpectedly: %v", err)
	}
	if invalidated != 1 {
		t.Errorf("InvalidateSkills calls = %d, want 1", invalidated)
	}
}

// A missing SKILL.md is non-fatal: the row meta still renders.
func TestSkillsDetail_MissingBodyDegrades(t *testing.T) {
	h := skillsDetail(testSkillsDeps())
	params, _ := json.Marshal(map[string]any{"name": "morning-letter"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.detail", Params: params})
	payload := decodeSkillsPayload[SkillDetailResponse](t, resp)
	if payload.Skill.Origin != skillOriginGenesis {
		t.Errorf("origin = %q, want genesis", payload.Skill.Origin)
	}
	if payload.Body != "" {
		t.Errorf("expected empty body for unreadable file, got %q", payload.Body)
	}
}

func TestSkillsDetail_UnknownAndMissingName(t *testing.T) {
	h := skillsDetail(testSkillsDeps())

	params, _ := json.Marshal(map[string]any{"name": "no-such-skill"})
	if resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.detail", Params: params}); resp.OK {
		t.Error("expected not-found error for unknown skill")
	}
	if resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "2", Method: "miniapp.skills.detail"}); resp.OK {
		t.Error("expected missing-param error without a name")
	}
}

func TestSkillsLifecycle_MappingAndLimit(t *testing.T) {
	h := skillsLifecycle(testSkillsDeps())
	params, _ := json.Marshal(map[string]any{"limit": 4})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.lifecycle", Params: params})
	payload := decodeSkillsPayload[SkillsLifecycleResponse](t, resp)

	if payload.Count != 4 {
		t.Fatalf("expected 4 events (limit), got %d", payload.Count)
	}
	if payload.Summary.System != "Propus" ||
		payload.Summary.State != "needs_attention" ||
		payload.Summary.Total != 4 ||
		payload.Summary.Evolved != 1 ||
		payload.Summary.Review != 1 ||
		payload.Summary.Rejected != 1 ||
		payload.Summary.RolledBack != 1 ||
		payload.Summary.Attention != 2 ||
		payload.Summary.LatestAt != 333 ||
		payload.Summary.LatestSkill != "email-analysis" ||
		payload.Summary.DoctrineVersion != genesis.PropusDoctrine().Version ||
		len(payload.Summary.SourcePapers) != len(genesis.PropusDoctrine().SourceIDs()) ||
		len(payload.Summary.FilteredSources) != len(genesis.PropusDoctrine().FilteredSourceIDs()) ||
		len(payload.Summary.Principles) != len(genesis.PropusDoctrine().ProductRules()) ||
		len(payload.Summary.QualityGates) != len(genesis.PropusDoctrine().QualityGates) ||
		payload.Summary.CoverageState != "covered" ||
		len(payload.Summary.CoverageGaps) != 0 ||
		len(payload.Summary.NextActions) == 0 ||
		payload.Summary.NextCue == "" ||
		payload.Summary.QualityGate == "" {
		t.Fatalf("unexpected Propus summary: %+v", payload.Summary)
	}
	first := payload.Events[0]
	if first.Type != "evolved" || first.Version != "1.1.1" || first.Detail != "개선" {
		t.Errorf("first event = %+v, want evolved/1.1.1/개선", first)
	}
	if first.TargetSignature != "terminal=timeout|mechanism=bounded-execution" ||
		first.EditedSurface != "Procedure" ||
		first.ExpectedBehaviorChange != "bounded recovery" ||
		first.RegressionRisk != "preserve verification" {
		t.Errorf("first event audit = %+v", first)
	}
	second := payload.Events[1]
	if second.Type != "review" || second.Route != "no-op" || second.Detail != "기존 커버" {
		t.Errorf("proposal must map to review verdict, got %+v", second)
	}
	if second.Evidence != "세션 관찰 기록" {
		t.Errorf("review evidence = %q, want the proposal evidence", second.Evidence)
	}
	third := payload.Events[2]
	if third.Type != "evolve_rejected" || third.Detail != "judge 기각" {
		t.Errorf("third event = %+v, want evolve_rejected", third)
	}
	fourth := payload.Events[3]
	if fourth.Type != "evolve_rolled_back" || fourth.Detail != "post-evolve rollback fired" {
		t.Errorf("fourth event = %+v, want evolve_rolled_back", fourth)
	}
}

// A proposal whose reason is empty falls back to evidence as the detail line
// and must not duplicate it into the Evidence field.
func TestLifecycleEvent_EvidenceFallback(t *testing.T) {
	ev := lifecycleEvent(genesis.LifecycleLogEntry{Type: "evolution_proposal", Route: "no-op", Evidence: "관찰만 존재"})
	if ev.Detail != "관찰만 존재" || ev.Evidence != "" {
		t.Errorf("evidence-only proposal = %+v, want Detail=관찰만 존재 Evidence=empty", ev)
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
	if payload.Summary.State != "steady" || payload.Summary.Genesis != 1 || payload.Summary.Attention != 0 {
		t.Fatalf("unexpected filtered Propus summary: %+v", payload.Summary)
	}
}

// Without a tracker the feed degrades to empty instead of erroring.
func TestSkillsLifecycle_NilProvider(t *testing.T) {
	deps := testSkillsDeps()
	deps.RecentLifecycle = nil
	deps.CuratorRecords = nil
	deps.UsageStats = nil
	deps.ValidationSummary = nil
	deps.RecentOpportunities = nil
	deps.RecentSelfCorrections = nil
	h := skillsLifecycle(deps)
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.skills.lifecycle"})
	payload := decodeSkillsPayload[SkillsLifecycleResponse](t, resp)
	if payload.Count != 0 {
		t.Fatalf("expected empty feed, got %d events", payload.Count)
	}
	if payload.Summary.State != "idle" || payload.Summary.System != "Propus" {
		t.Fatalf("unexpected nil-provider Propus summary: %+v", payload.Summary)
	}
}
