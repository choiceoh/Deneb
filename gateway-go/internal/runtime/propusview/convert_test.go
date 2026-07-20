package propusview

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
)

func TestLifecycleEntriesProjectionPreservesFieldsAndCopiesAudit(t *testing.T) {
	t.Parallel()
	input := []genesis.LifecycleLogEntry{
		{
			Type:       "",
			SkillName:  "skill-0",
			Source:     "source-0",
			SessionKey: "session-0",
			CreatedAt:  -50,
			Executed:   true,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-0",
				EditedSurface:          "surface-0",
				ExpectedBehaviorChange: "change-0",
				RegressionRisk:         "risk-0",
				PrimaryDimension:       genesis.HarnessDimensionToolInteraction,
				SecondaryDimensions:    []string{genesis.HarnessDimensionOutput},
			},
		},
		{
			Type:       "genesis",
			SkillName:  "skill-1",
			Source:     "source-1",
			SessionKey: "session-1",
			CreatedAt:  51,
			Executed:   false,
		},
		{
			Type:       "evolved",
			SkillName:  "skill-2",
			Source:     "source-2",
			SessionKey: "session-2",
			CreatedAt:  152,
			Executed:   true,
		},
		{
			Type:       "evolve_rejected",
			SkillName:  "skill-3",
			Source:     "source-3",
			SessionKey: "session-3",
			CreatedAt:  253,
			Executed:   false,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-3",
				EditedSurface:          "surface-3",
				ExpectedBehaviorChange: "change-3",
				RegressionRisk:         "risk-3",
			},
		},
		{
			Type:       "evolve_rolled_back",
			SkillName:  "skill-4",
			Source:     "source-4",
			SessionKey: "session-4",
			CreatedAt:  354,
			Executed:   true,
		},
		{
			Type:       "evolution_proposal",
			SkillName:  "skill-5",
			Source:     "source-5",
			SessionKey: "session-5",
			CreatedAt:  455,
			Executed:   false,
		},
		{
			Type:       "review",
			SkillName:  "skill-6",
			Source:     "source-6",
			SessionKey: "session-6",
			CreatedAt:  556,
			Executed:   true,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-6",
				EditedSurface:          "surface-6",
				ExpectedBehaviorChange: "change-6",
				RegressionRisk:         "risk-6",
			},
		},
		{
			Type:       "custom",
			SkillName:  "skill-7",
			Source:     "source-7",
			SessionKey: "session-7",
			CreatedAt:  657,
			Executed:   false,
		},
		{
			Type:       "",
			SkillName:  "skill-8",
			Source:     "source-8",
			SessionKey: "session-8",
			CreatedAt:  758,
			Executed:   true,
		},
		{
			Type:       "genesis",
			SkillName:  "skill-9",
			Source:     "source-9",
			SessionKey: "session-9",
			CreatedAt:  859,
			Executed:   false,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-9",
				EditedSurface:          "surface-9",
				ExpectedBehaviorChange: "change-9",
				RegressionRisk:         "risk-9",
			},
		},
		{
			Type:       "evolved",
			SkillName:  "skill-10",
			Source:     "source-10",
			SessionKey: "session-10",
			CreatedAt:  960,
			Executed:   true,
		},
		{
			Type:       "evolve_rejected",
			SkillName:  "skill-11",
			Source:     "source-11",
			SessionKey: "session-11",
			CreatedAt:  1061,
			Executed:   false,
		},
		{
			Type:       "evolve_rolled_back",
			SkillName:  "skill-12",
			Source:     "source-12",
			SessionKey: "session-12",
			CreatedAt:  1162,
			Executed:   true,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-12",
				EditedSurface:          "surface-12",
				ExpectedBehaviorChange: "change-12",
				RegressionRisk:         "risk-12",
			},
		},
		{
			Type:       "evolution_proposal",
			SkillName:  "skill-13",
			Source:     "source-13",
			SessionKey: "session-13",
			CreatedAt:  1263,
			Executed:   false,
		},
		{
			Type:       "review",
			SkillName:  "skill-14",
			Source:     "source-14",
			SessionKey: "session-14",
			CreatedAt:  1364,
			Executed:   true,
		},
		{
			Type:       "custom",
			SkillName:  "skill-15",
			Source:     "source-15",
			SessionKey: "session-15",
			CreatedAt:  1465,
			Executed:   false,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-15",
				EditedSurface:          "surface-15",
				ExpectedBehaviorChange: "change-15",
				RegressionRisk:         "risk-15",
			},
		},
		{
			Type:       "",
			SkillName:  "skill-16",
			Source:     "source-16",
			SessionKey: "session-16",
			CreatedAt:  1566,
			Executed:   true,
		},
		{
			Type:       "genesis",
			SkillName:  "skill-17",
			Source:     "source-17",
			SessionKey: "session-17",
			CreatedAt:  1667,
			Executed:   false,
		},
		{
			Type:       "evolved",
			SkillName:  "skill-18",
			Source:     "source-18",
			SessionKey: "session-18",
			CreatedAt:  1768,
			Executed:   true,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-18",
				EditedSurface:          "surface-18",
				ExpectedBehaviorChange: "change-18",
				RegressionRisk:         "risk-18",
			},
		},
		{
			Type:       "evolve_rejected",
			SkillName:  "skill-19",
			Source:     "source-19",
			SessionKey: "session-19",
			CreatedAt:  1869,
			Executed:   false,
		},
		{
			Type:       "evolve_rolled_back",
			SkillName:  "skill-20",
			Source:     "source-20",
			SessionKey: "session-20",
			CreatedAt:  1970,
			Executed:   true,
		},
		{
			Type:       "evolution_proposal",
			SkillName:  "skill-21",
			Source:     "source-21",
			SessionKey: "session-21",
			CreatedAt:  2071,
			Executed:   false,
			SelfHarnessAudit: &genesis.HarnessEditAudit{
				TargetSignature:        "signature-21",
				EditedSurface:          "surface-21",
				ExpectedBehaviorChange: "change-21",
				RegressionRisk:         "risk-21",
			},
		},
		{
			Type:       "review",
			SkillName:  "skill-22",
			Source:     "source-22",
			SessionKey: "session-22",
			CreatedAt:  2172,
			Executed:   true,
		},
		{
			Type:       "custom",
			SkillName:  "skill-23",
			Source:     "source-23",
			SessionKey: "session-23",
			CreatedAt:  2273,
			Executed:   false,
		},
	}
	got := LifecycleEntries(input)
	if len(got) != len(input) {
		t.Fatalf("len = %d, want %d", len(got), len(input))
	}
	for i := range input {
		if got[i].Type != input[i].Type || got[i].SkillName != input[i].SkillName || got[i].CreatedAt != input[i].CreatedAt || got[i].Executed != input[i].Executed {
			t.Fatalf("entry %d = %#v, input %#v", i, got[i], input[i])
		}
		if (got[i].SelfHarnessAudit == nil) != (input[i].SelfHarnessAudit == nil) {
			t.Fatalf("entry %d audit nil mismatch", i)
		}
		if input[i].SelfHarnessAudit != nil {
			want := input[i].SelfHarnessAudit
			audit := got[i].SelfHarnessAudit
			if audit.TargetSignature != want.TargetSignature || audit.EditedSurface != want.EditedSurface || audit.ExpectedBehaviorChange != want.ExpectedBehaviorChange || audit.RegressionRisk != want.RegressionRisk || audit.PrimaryDimension != want.PrimaryDimension || !reflect.DeepEqual(audit.SecondaryDimensions, want.SecondaryDimensions) {
				t.Fatalf("entry %d audit = %#v, want %#v", i, audit, want)
			}
			if any(audit) == any(want) {
				t.Fatalf("entry %d audit pointer aliases input", i)
			}
			if len(audit.SecondaryDimensions) > 0 && &audit.SecondaryDimensions[0] == &want.SecondaryDimensions[0] {
				t.Fatalf("entry %d audit dimension slice aliases input", i)
			}
		}
	}
	if got == nil {
		t.Fatal("non-nil input projected to nil")
	}
}

func TestUsageStatsProjectionPreservesAllFields(t *testing.T) {
	t.Parallel()
	input := []genesis.UsageStats{
		{
			SkillName:    "skill-0",
			TotalUses:    0,
			SuccessCount: 0,
			FailureCount: 0,
			SuccessRate:  0,
			RecentErrors: []string{"ignored-0"},
		},
		{
			SkillName:    "skill-1",
			TotalUses:    1,
			SuccessCount: 0,
			FailureCount: 1,
			SuccessRate:  0,
			RecentErrors: []string{"ignored-1"},
		},
		{
			SkillName:    "skill-2",
			TotalUses:    2,
			SuccessCount: 1,
			FailureCount: 1,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-2"},
		},
		{
			SkillName:    "skill-3",
			TotalUses:    3,
			SuccessCount: 1,
			FailureCount: 2,
			SuccessRate:  0.3333333333333333,
			RecentErrors: []string{"ignored-3"},
		},
		{
			SkillName:    "skill-4",
			TotalUses:    4,
			SuccessCount: 2,
			FailureCount: 2,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-4"},
		},
		{
			SkillName:    "skill-5",
			TotalUses:    5,
			SuccessCount: 2,
			FailureCount: 3,
			SuccessRate:  0.4,
			RecentErrors: []string{"ignored-5"},
		},
		{
			SkillName:    "skill-6",
			TotalUses:    6,
			SuccessCount: 3,
			FailureCount: 3,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-6"},
		},
		{
			SkillName:    "skill-7",
			TotalUses:    7,
			SuccessCount: 3,
			FailureCount: 4,
			SuccessRate:  0.42857142857142855,
			RecentErrors: []string{"ignored-7"},
		},
		{
			SkillName:    "skill-8",
			TotalUses:    8,
			SuccessCount: 4,
			FailureCount: 4,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-8"},
		},
		{
			SkillName:    "skill-9",
			TotalUses:    9,
			SuccessCount: 4,
			FailureCount: 5,
			SuccessRate:  0.4444444444444444,
			RecentErrors: []string{"ignored-9"},
		},
		{
			SkillName:    "skill-10",
			TotalUses:    10,
			SuccessCount: 5,
			FailureCount: 5,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-10"},
		},
		{
			SkillName:    "skill-11",
			TotalUses:    11,
			SuccessCount: 5,
			FailureCount: 6,
			SuccessRate:  0.45454545454545453,
			RecentErrors: []string{"ignored-11"},
		},
		{
			SkillName:    "skill-12",
			TotalUses:    12,
			SuccessCount: 6,
			FailureCount: 6,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-12"},
		},
		{
			SkillName:    "skill-13",
			TotalUses:    13,
			SuccessCount: 6,
			FailureCount: 7,
			SuccessRate:  0.46153846153846156,
			RecentErrors: []string{"ignored-13"},
		},
		{
			SkillName:    "skill-14",
			TotalUses:    14,
			SuccessCount: 7,
			FailureCount: 7,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-14"},
		},
		{
			SkillName:    "skill-15",
			TotalUses:    15,
			SuccessCount: 7,
			FailureCount: 8,
			SuccessRate:  0.4666666666666667,
			RecentErrors: []string{"ignored-15"},
		},
		{
			SkillName:    "skill-16",
			TotalUses:    16,
			SuccessCount: 8,
			FailureCount: 8,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-16"},
		},
		{
			SkillName:    "skill-17",
			TotalUses:    17,
			SuccessCount: 8,
			FailureCount: 9,
			SuccessRate:  0.47058823529411764,
			RecentErrors: []string{"ignored-17"},
		},
		{
			SkillName:    "skill-18",
			TotalUses:    18,
			SuccessCount: 9,
			FailureCount: 9,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-18"},
		},
		{
			SkillName:    "skill-19",
			TotalUses:    19,
			SuccessCount: 9,
			FailureCount: 10,
			SuccessRate:  0.47368421052631576,
			RecentErrors: []string{"ignored-19"},
		},
		{
			SkillName:    "skill-20",
			TotalUses:    20,
			SuccessCount: 10,
			FailureCount: 10,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-20"},
		},
		{
			SkillName:    "skill-21",
			TotalUses:    21,
			SuccessCount: 10,
			FailureCount: 11,
			SuccessRate:  0.47619047619047616,
			RecentErrors: []string{"ignored-21"},
		},
		{
			SkillName:    "skill-22",
			TotalUses:    22,
			SuccessCount: 11,
			FailureCount: 11,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-22"},
		},
		{
			SkillName:    "skill-23",
			TotalUses:    23,
			SuccessCount: 11,
			FailureCount: 12,
			SuccessRate:  0.4782608695652174,
			RecentErrors: []string{"ignored-23"},
		},
		{
			SkillName:    "skill-24",
			TotalUses:    24,
			SuccessCount: 12,
			FailureCount: 12,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-24"},
		},
		{
			SkillName:    "skill-25",
			TotalUses:    25,
			SuccessCount: 12,
			FailureCount: 13,
			SuccessRate:  0.48,
			RecentErrors: []string{"ignored-25"},
		},
		{
			SkillName:    "skill-26",
			TotalUses:    26,
			SuccessCount: 13,
			FailureCount: 13,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-26"},
		},
		{
			SkillName:    "skill-27",
			TotalUses:    27,
			SuccessCount: 13,
			FailureCount: 14,
			SuccessRate:  0.48148148148148145,
			RecentErrors: []string{"ignored-27"},
		},
		{
			SkillName:    "skill-28",
			TotalUses:    28,
			SuccessCount: 14,
			FailureCount: 14,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-28"},
		},
		{
			SkillName:    "skill-29",
			TotalUses:    29,
			SuccessCount: 14,
			FailureCount: 15,
			SuccessRate:  0.4827586206896552,
			RecentErrors: []string{"ignored-29"},
		},
		{
			SkillName:    "skill-30",
			TotalUses:    30,
			SuccessCount: 15,
			FailureCount: 15,
			SuccessRate:  0.5,
			RecentErrors: []string{"ignored-30"},
		},
		{
			SkillName:    "skill-31",
			TotalUses:    31,
			SuccessCount: 15,
			FailureCount: 16,
			SuccessRate:  0.4838709677419355,
			RecentErrors: []string{"ignored-31"},
		},
	}
	got := UsageStats(input)
	if len(got) != len(input) {
		t.Fatalf("len = %d, want %d", len(got), len(input))
	}
	for i, want := range input {
		if got[i].SkillName != want.SkillName || got[i].TotalUses != want.TotalUses || got[i].SuccessCount != want.SuccessCount || got[i].FailureCount != want.FailureCount || got[i].SuccessRate != want.SuccessRate {
			t.Fatalf("stat %d = %#v, want projection of %#v", i, got[i], want)
		}
	}
}

func TestUsageStatNilAndCopySemantics(t *testing.T) {
	t.Parallel()
	if got := UsageStat(nil); got != nil {
		t.Fatalf("UsageStat(nil) = %#v", got)
	}
	input := &genesis.UsageStats{SkillName: "alpha", TotalUses: 9, SuccessCount: 7, FailureCount: 2, SuccessRate: 7.0 / 9.0, RecentErrors: []string{"ignored"}}
	got := UsageStat(input)
	if got == nil {
		t.Fatal("UsageStat(non-nil) = nil")
	}
	if got.SkillName != input.SkillName || got.TotalUses != input.TotalUses || got.SuccessCount != input.SuccessCount || got.FailureCount != input.FailureCount || got.SuccessRate != input.SuccessRate {
		t.Fatalf("got %#v, input %#v", got, input)
	}
	got.SkillName = "mutated"
	if input.SkillName != "alpha" {
		t.Fatal("projection aliases input")
	}
}

func TestCuratorProjectionFieldContractKeepsNameCreatedByState(t *testing.T) {
	t.Parallel()
	input := []genesis.SkillCuratorRecord{
		{
			SkillName:  "curated-0",
			CreatedBy:  "",
			State:      "",
			Pinned:     true,
			UseCount:   0,
			PatchCount: 0,
		},
		{
			SkillName:  "curated-1",
			CreatedBy:  "agent",
			State:      "active",
			Pinned:     false,
			UseCount:   3,
			PatchCount: 2,
		},
		{
			SkillName:  "curated-2",
			CreatedBy:  "user",
			State:      "stale",
			Pinned:     true,
			UseCount:   6,
			PatchCount: 4,
		},
		{
			SkillName:  "curated-3",
			CreatedBy:  "system",
			State:      "archived",
			Pinned:     false,
			UseCount:   9,
			PatchCount: 6,
		},
		{
			SkillName:  "curated-4",
			CreatedBy:  "",
			State:      "unknown",
			Pinned:     true,
			UseCount:   12,
			PatchCount: 8,
		},
		{
			SkillName:  "curated-5",
			CreatedBy:  "agent",
			State:      "",
			Pinned:     false,
			UseCount:   15,
			PatchCount: 10,
		},
		{
			SkillName:  "curated-6",
			CreatedBy:  "user",
			State:      "active",
			Pinned:     true,
			UseCount:   18,
			PatchCount: 12,
		},
		{
			SkillName:  "curated-7",
			CreatedBy:  "system",
			State:      "stale",
			Pinned:     false,
			UseCount:   21,
			PatchCount: 14,
		},
		{
			SkillName:  "curated-8",
			CreatedBy:  "",
			State:      "archived",
			Pinned:     true,
			UseCount:   24,
			PatchCount: 16,
		},
		{
			SkillName:  "curated-9",
			CreatedBy:  "agent",
			State:      "unknown",
			Pinned:     false,
			UseCount:   27,
			PatchCount: 18,
		},
		{
			SkillName:  "curated-10",
			CreatedBy:  "user",
			State:      "",
			Pinned:     true,
			UseCount:   30,
			PatchCount: 20,
		},
		{
			SkillName:  "curated-11",
			CreatedBy:  "system",
			State:      "active",
			Pinned:     false,
			UseCount:   33,
			PatchCount: 22,
		},
		{
			SkillName:  "curated-12",
			CreatedBy:  "",
			State:      "stale",
			Pinned:     true,
			UseCount:   36,
			PatchCount: 24,
		},
		{
			SkillName:  "curated-13",
			CreatedBy:  "agent",
			State:      "archived",
			Pinned:     false,
			UseCount:   39,
			PatchCount: 26,
		},
		{
			SkillName:  "curated-14",
			CreatedBy:  "user",
			State:      "unknown",
			Pinned:     true,
			UseCount:   42,
			PatchCount: 28,
		},
		{
			SkillName:  "curated-15",
			CreatedBy:  "system",
			State:      "",
			Pinned:     false,
			UseCount:   45,
			PatchCount: 30,
		},
		{
			SkillName:  "curated-16",
			CreatedBy:  "",
			State:      "active",
			Pinned:     true,
			UseCount:   48,
			PatchCount: 32,
		},
		{
			SkillName:  "curated-17",
			CreatedBy:  "agent",
			State:      "stale",
			Pinned:     false,
			UseCount:   51,
			PatchCount: 34,
		},
		{
			SkillName:  "curated-18",
			CreatedBy:  "user",
			State:      "archived",
			Pinned:     true,
			UseCount:   54,
			PatchCount: 36,
		},
		{
			SkillName:  "curated-19",
			CreatedBy:  "system",
			State:      "unknown",
			Pinned:     false,
			UseCount:   57,
			PatchCount: 38,
		},
		{
			SkillName:  "curated-20",
			CreatedBy:  "",
			State:      "",
			Pinned:     true,
			UseCount:   60,
			PatchCount: 40,
		},
		{
			SkillName:  "curated-21",
			CreatedBy:  "agent",
			State:      "active",
			Pinned:     false,
			UseCount:   63,
			PatchCount: 42,
		},
		{
			SkillName:  "curated-22",
			CreatedBy:  "user",
			State:      "stale",
			Pinned:     true,
			UseCount:   66,
			PatchCount: 44,
		},
		{
			SkillName:  "curated-23",
			CreatedBy:  "system",
			State:      "archived",
			Pinned:     false,
			UseCount:   69,
			PatchCount: 46,
		},
		{
			SkillName:  "curated-24",
			CreatedBy:  "",
			State:      "unknown",
			Pinned:     true,
			UseCount:   72,
			PatchCount: 48,
		},
		{
			SkillName:  "curated-25",
			CreatedBy:  "agent",
			State:      "",
			Pinned:     false,
			UseCount:   75,
			PatchCount: 50,
		},
		{
			SkillName:  "curated-26",
			CreatedBy:  "user",
			State:      "active",
			Pinned:     true,
			UseCount:   78,
			PatchCount: 52,
		},
		{
			SkillName:  "curated-27",
			CreatedBy:  "system",
			State:      "stale",
			Pinned:     false,
			UseCount:   81,
			PatchCount: 54,
		},
	}
	got := Curator(input)
	if len(got) != len(input) {
		t.Fatalf("len = %d, want %d", len(got), len(input))
	}
	for i, want := range input {
		if got[i] != (propus.SkillCuratorRecord{SkillName: want.SkillName, CreatedBy: want.CreatedBy, State: want.State}) {
			t.Fatalf("record %d = %#v, want projection of %#v", i, got[i], want)
		}
	}
}

func TestOpportunityAndSelfCorrectionProjectionMatrices(t *testing.T) {
	t.Parallel()
	opportunities := []genesis.SkillOpportunityRecord{
		{
			SkillName: "opportunity-0",
			Route:     "",
			Candidate: "ignored candidate 0",
			Evidence:  "ignored evidence 0",
		},
		{
			SkillName: "opportunity-1",
			Route:     "no-op",
			Candidate: "ignored candidate 1",
			Evidence:  "ignored evidence 1",
		},
		{
			SkillName: "opportunity-2",
			Route:     "genesis",
			Candidate: "ignored candidate 2",
			Evidence:  "ignored evidence 2",
		},
		{
			SkillName: "opportunity-3",
			Route:     "evolve",
			Candidate: "ignored candidate 3",
			Evidence:  "ignored evidence 3",
		},
		{
			SkillName: "opportunity-4",
			Route:     "create",
			Candidate: "ignored candidate 4",
			Evidence:  "ignored evidence 4",
		},
		{
			SkillName: "opportunity-5",
			Route:     "",
			Candidate: "ignored candidate 5",
			Evidence:  "ignored evidence 5",
		},
		{
			SkillName: "opportunity-6",
			Route:     "no-op",
			Candidate: "ignored candidate 6",
			Evidence:  "ignored evidence 6",
		},
		{
			SkillName: "opportunity-7",
			Route:     "genesis",
			Candidate: "ignored candidate 7",
			Evidence:  "ignored evidence 7",
		},
		{
			SkillName: "opportunity-8",
			Route:     "evolve",
			Candidate: "ignored candidate 8",
			Evidence:  "ignored evidence 8",
		},
		{
			SkillName: "opportunity-9",
			Route:     "create",
			Candidate: "ignored candidate 9",
			Evidence:  "ignored evidence 9",
		},
		{
			SkillName: "opportunity-10",
			Route:     "",
			Candidate: "ignored candidate 10",
			Evidence:  "ignored evidence 10",
		},
		{
			SkillName: "opportunity-11",
			Route:     "no-op",
			Candidate: "ignored candidate 11",
			Evidence:  "ignored evidence 11",
		},
		{
			SkillName: "opportunity-12",
			Route:     "genesis",
			Candidate: "ignored candidate 12",
			Evidence:  "ignored evidence 12",
		},
		{
			SkillName: "opportunity-13",
			Route:     "evolve",
			Candidate: "ignored candidate 13",
			Evidence:  "ignored evidence 13",
		},
		{
			SkillName: "opportunity-14",
			Route:     "create",
			Candidate: "ignored candidate 14",
			Evidence:  "ignored evidence 14",
		},
		{
			SkillName: "opportunity-15",
			Route:     "",
			Candidate: "ignored candidate 15",
			Evidence:  "ignored evidence 15",
		},
		{
			SkillName: "opportunity-16",
			Route:     "no-op",
			Candidate: "ignored candidate 16",
			Evidence:  "ignored evidence 16",
		},
		{
			SkillName: "opportunity-17",
			Route:     "genesis",
			Candidate: "ignored candidate 17",
			Evidence:  "ignored evidence 17",
		},
		{
			SkillName: "opportunity-18",
			Route:     "evolve",
			Candidate: "ignored candidate 18",
			Evidence:  "ignored evidence 18",
		},
		{
			SkillName: "opportunity-19",
			Route:     "create",
			Candidate: "ignored candidate 19",
			Evidence:  "ignored evidence 19",
		},
		{
			SkillName: "opportunity-20",
			Route:     "",
			Candidate: "ignored candidate 20",
			Evidence:  "ignored evidence 20",
		},
		{
			SkillName: "opportunity-21",
			Route:     "no-op",
			Candidate: "ignored candidate 21",
			Evidence:  "ignored evidence 21",
		},
		{
			SkillName: "opportunity-22",
			Route:     "genesis",
			Candidate: "ignored candidate 22",
			Evidence:  "ignored evidence 22",
		},
		{
			SkillName: "opportunity-23",
			Route:     "evolve",
			Candidate: "ignored candidate 23",
			Evidence:  "ignored evidence 23",
		},
	}
	gotOpportunities := Opportunities(opportunities)
	for i, want := range opportunities {
		if gotOpportunities[i] != (propus.SkillOpportunityRecord{SkillName: want.SkillName, Route: want.Route}) {
			t.Fatalf("opportunity %d = %#v", i, gotOpportunities[i])
		}
	}
	corrections := []genesis.SelfCorrectionCandidateRecord{
		{
			SkillName: "correction-0",
			Status:    "",
			Title:     "ignored title 0",
			Risk:      "ignored risk 0",
		},
		{
			SkillName: "correction-1",
			Status:    "proposed",
			Title:     "ignored title 1",
			Risk:      "ignored risk 1",
		},
		{
			SkillName: "correction-2",
			Status:    "accepted",
			Title:     "ignored title 2",
			Risk:      "ignored risk 2",
		},
		{
			SkillName: "correction-3",
			Status:    "rejected",
			Title:     "ignored title 3",
			Risk:      "ignored risk 3",
		},
		{
			SkillName: "correction-4",
			Status:    "applied",
			Title:     "ignored title 4",
			Risk:      "ignored risk 4",
		},
		{
			SkillName: "correction-5",
			Status:    "superseded",
			Title:     "ignored title 5",
			Risk:      "ignored risk 5",
		},
		{
			SkillName: "correction-6",
			Status:    "",
			Title:     "ignored title 6",
			Risk:      "ignored risk 6",
		},
		{
			SkillName: "correction-7",
			Status:    "proposed",
			Title:     "ignored title 7",
			Risk:      "ignored risk 7",
		},
		{
			SkillName: "correction-8",
			Status:    "accepted",
			Title:     "ignored title 8",
			Risk:      "ignored risk 8",
		},
		{
			SkillName: "correction-9",
			Status:    "rejected",
			Title:     "ignored title 9",
			Risk:      "ignored risk 9",
		},
		{
			SkillName: "correction-10",
			Status:    "applied",
			Title:     "ignored title 10",
			Risk:      "ignored risk 10",
		},
		{
			SkillName: "correction-11",
			Status:    "superseded",
			Title:     "ignored title 11",
			Risk:      "ignored risk 11",
		},
		{
			SkillName: "correction-12",
			Status:    "",
			Title:     "ignored title 12",
			Risk:      "ignored risk 12",
		},
		{
			SkillName: "correction-13",
			Status:    "proposed",
			Title:     "ignored title 13",
			Risk:      "ignored risk 13",
		},
		{
			SkillName: "correction-14",
			Status:    "accepted",
			Title:     "ignored title 14",
			Risk:      "ignored risk 14",
		},
		{
			SkillName: "correction-15",
			Status:    "rejected",
			Title:     "ignored title 15",
			Risk:      "ignored risk 15",
		},
		{
			SkillName: "correction-16",
			Status:    "applied",
			Title:     "ignored title 16",
			Risk:      "ignored risk 16",
		},
		{
			SkillName: "correction-17",
			Status:    "superseded",
			Title:     "ignored title 17",
			Risk:      "ignored risk 17",
		},
		{
			SkillName: "correction-18",
			Status:    "",
			Title:     "ignored title 18",
			Risk:      "ignored risk 18",
		},
		{
			SkillName: "correction-19",
			Status:    "proposed",
			Title:     "ignored title 19",
			Risk:      "ignored risk 19",
		},
		{
			SkillName: "correction-20",
			Status:    "accepted",
			Title:     "ignored title 20",
			Risk:      "ignored risk 20",
		},
		{
			SkillName: "correction-21",
			Status:    "rejected",
			Title:     "ignored title 21",
			Risk:      "ignored risk 21",
		},
		{
			SkillName: "correction-22",
			Status:    "applied",
			Title:     "ignored title 22",
			Risk:      "ignored risk 22",
		},
		{
			SkillName: "correction-23",
			Status:    "superseded",
			Title:     "ignored title 23",
			Risk:      "ignored risk 23",
		},
	}
	gotCorrections := SelfCorrections(corrections)
	for i, want := range corrections {
		if gotCorrections[i] != (propus.SelfCorrectionCandidateRecord{SkillName: want.SkillName, Status: want.Status}) {
			t.Fatalf("correction %d = %#v", i, gotCorrections[i])
		}
	}
}

func TestSummaryProjectionFieldContracts(t *testing.T) {
	t.Parallel()
	quality := UsageQuality(genesis.UsageQualitySummary{TotalRecords: 11, CountedRecords: 7, IgnoredRecords: 4})
	if quality != (propus.UsageQualitySummary{TotalRecords: 11, CountedRecords: 7, IgnoredRecords: 4}) {
		t.Fatalf("quality = %#v", quality)
	}
	validation := Validation(genesis.SkillValidationCaseSummary{SkillName: "alpha", RawRecords: 99, UniqueRecords: 8, DuplicateRecords: 91, SkillsWithCases: 3, UniqueEasyAnchorCases: 2, UniqueMixedFrontierCases: 4, UniqueHardFrontierCases: 2})
	wantValidation := propus.SkillValidationCaseSummary{SkillName: "alpha", UniqueRecords: 8, SkillsWithCases: 3, UniqueEasyAnchorCases: 2, UniqueMixedFrontierCases: 4, UniqueHardFrontierCases: 2}
	if validation != wantValidation {
		t.Fatalf("validation = %#v, want %#v", validation, wantValidation)
	}
	harness := SelfHarness(genesis.SelfHarnessSignalSummary{Rejections7d: 9, ValidationDrafts7d: 8, TargetRecurrences7d: 7, SignatureMismatchRejections7d: 6, SurfaceMismatchRejections7d: 5, MissingAuditRejections7d: 4, HeldOutReplayRejections7d: 3})
	wantHarness := propus.SelfHarnessSignalSummary{Rejections7d: 9, ValidationDrafts7d: 8, TargetRecurrences7d: 7, SignatureMismatchRejections7d: 6, SurfaceMismatchRejections7d: 5}
	if harness != wantHarness {
		t.Fatalf("harness = %#v, want %#v", harness, wantHarness)
	}
	live := Liveness(genesis.SkillLivenessState{LastReviewAt: 1, LastEvolveAt: 2, LastGenesisAt: 3, LastErrorAt: 4, LastError: "boom", ReviewAttempts: 5, ReviewSkips: 6, ValidationRejections: 7, UpdatedAt: 99})
	wantLive := propus.SkillLivenessState{LastReviewAt: 1, LastEvolveAt: 2, LastGenesisAt: 3, LastErrorAt: 4, LastError: "boom", ReviewAttempts: 5, ReviewSkips: 6, ValidationRejections: 7}
	if live != wantLive {
		t.Fatalf("liveness = %#v, want %#v", live, wantLive)
	}
	evolution := Evolution(genesis.EvolutionHealthSummary{Thrash: true, EvolveRolledBack7d: 3, EvolveRejected7d: 4, Evolves7d: 99, ConfirmRate: .75})
	wantEvolution := propus.EvolutionHealthSummary{Thrash: true, EvolveRolledBack7d: 3, EvolveRejected7d: 4}
	if evolution != wantEvolution {
		t.Fatalf("evolution = %#v, want %#v", evolution, wantEvolution)
	}
}

func TestProjectionEmptySlicesAreStableNonNil(t *testing.T) {
	t.Parallel()
	checks := map[string]any{
		"lifecycle":     LifecycleEntries([]genesis.LifecycleLogEntry{}),
		"usage":         UsageStats([]genesis.UsageStats{}),
		"curator":       Curator([]genesis.SkillCuratorRecord{}),
		"opportunities": Opportunities([]genesis.SkillOpportunityRecord{}),
		"corrections":   SelfCorrections([]genesis.SelfCorrectionCandidateRecord{}),
	}
	for name, value := range checks {
		rv := reflect.ValueOf(value)
		if rv.IsNil() {
			t.Errorf("%s projection returned nil empty slice", name)
		}
		if rv.Len() != 0 {
			t.Errorf("%s projection len = %d", name, rv.Len())
		}
	}
}

func TestProjectionJSONEncodeIsDeterministic(t *testing.T) {
	t.Parallel()
	value := struct {
		Lifecycle []propus.LifecycleLogEntry
		Usage     []propus.UsageStats
	}{
		Lifecycle: LifecycleEntries([]genesis.LifecycleLogEntry{{Type: "evolved", SkillName: "alpha", CreatedAt: 42, Executed: true, SelfHarnessAudit: &genesis.HarnessEditAudit{TargetSignature: "terminal=timeout"}}}),
		Usage:     UsageStats([]genesis.UsageStats{{SkillName: "alpha", TotalUses: 4, SuccessCount: 3, FailureCount: 1, SuccessRate: .75}}),
	}
	first, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(first): %v", err)
	}
	second, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("Marshal(second): %v", err)
	}
	if string(first) != string(second) {
		t.Fatalf("serialization changed: %s != %s", first, second)
	}
	var decoded map[string]any
	if err := json.Unmarshal(first, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded) != 2 {
		t.Fatalf("decoded = %#v", decoded)
	}
}

func TestProjectionsAreSafeForConcurrentReaders(t *testing.T) {
	t.Parallel()
	input := make([]genesis.UsageStats, 128)
	for i := range input {
		input[i] = genesis.UsageStats{SkillName: fmt.Sprintf("skill-%03d", i), TotalUses: i, SuccessCount: i / 2, FailureCount: i - i/2, SuccessRate: .5}
	}
	const workers = 32
	const iterations = 50
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for worker := 0; worker < workers; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for iteration := 0; iteration < iterations; iteration++ {
				got := UsageStats(input)
				if len(got) != len(input) || got[127].SkillName != "skill-127" {
					errs <- fmt.Errorf("invalid projection at iteration %d", iteration)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
