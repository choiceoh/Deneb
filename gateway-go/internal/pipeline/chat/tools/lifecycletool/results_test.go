package lifecycletool

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/propus"
)

func TestSkillLifecycleResultPreservesJSONShape(t *testing.T) {
	emptyID := ""
	skillName := "deploy-helper"
	action := "archive"
	curatorRecord := genesis.SkillCuratorRecord{SkillName: skillName, State: genesis.SkillCuratorStateArchived}
	curatorRecords := []genesis.SkillCuratorRecord{{SkillName: skillName, State: genesis.SkillCuratorStateActive}}

	tests := []struct {
		name string
		got  any
		want string
	}{
		{
			name: "proposal embeds selected execution without union wrapper",
			got: SkillEvolutionProposalResult{
				OK:        true,
				Candidate: "repeatable deploy fix",
				Route:     "genesis",
				Executed:  true,
				Result: &SkillEvolutionProposalExecutionResult{Genesis: &SkillGenesisResult{
					OK: true, Skip: true, Reason: "no skill-worthy pattern detected", Source: "session",
				}},
			},
			want: `{
				"ok": true,
				"candidate": "repeatable deploy fix",
				"route": "genesis",
				"executed": true,
				"result": {"ok": true, "skip": true, "reason": "no skill-worthy pattern detected", "source": "session"}
			}`,
		},
		{
			name: "validation success keeps an explicitly empty id",
			got:  SkillValidationCaseResult{OK: true, SkillName: &skillName, ID: &emptyID},
			want: `{"ok":true,"skillName":"deploy-helper","id":""}`,
		},
		{
			name: "unconfigured operation stays minimal",
			got:  SkillValidationCaseResult{Reason: "skill tracker is not configured"},
			want: `{"ok":false,"reason":"skill tracker is not configured"}`,
		},
		{
			name: "curator transition keeps object shape",
			got: SkillCuratorActionResult{
				OK: true, Action: &action, SkillName: &skillName,
				Curator: &SkillCuratorResult{Record: &curatorRecord},
			},
			want: `{
				"ok":true,"action":"archive","skillName":"deploy-helper",
				"curator":{"skillName":"deploy-helper","state":"archived"}
			}`,
		},
		{
			name: "curator pin report keeps array shape",
			got: SkillCuratorActionResult{
				OK: true, Action: lifecycleTestValue("pin"), SkillName: &skillName,
				Curator: &SkillCuratorResult{Records: &curatorRecords},
			},
			want: `{
				"ok":true,"action":"pin","skillName":"deploy-helper",
				"curator":[{"skillName":"deploy-helper","state":"active"}]
			}`,
		},
		{
			name: "unavailable overview keeps legacy four-field shape",
			got: SkillLifecycleOverview{Unavailable: &SkillLifecycleUnavailableOverview{
				State: "unavailable", Scope: "global", SkillName: "", NextActions: []string{"configure_skill_tracker"},
			}},
			want: `{
				"state":"unavailable","scope":"global","skillName":"",
				"nextActions":["configure_skill_tracker"]
			}`,
		},
		{
			name: "skill overview keeps meaningful zero fields",
			got: SkillLifecycleOverview{Operational: &propus.PropusOverview{
				State:       "steady",
				Scope:       propus.PropusScopeSkill,
				EventCounts: map[string]int{},
				DoctrineCoverage: propus.PropusDoctrineCoverage{
					State: "unproven", Covered: []string{}, Gaps: []string{}, AxisCoverage: map[string]int{},
					SourcePolicy: "core_sources_only_filtered_sources_not_gates", FilteredSources: []string{},
				},
				NextActions: []string{},
			}},
			want: `{
				"state":"steady","scope":"skill","eventCounts":{},
				"countedUsageRecords":0,"ignoredUsageRecords":0,"validationCases":0,
				"pendingSelfCorrections":0,"openOpportunities":0,
				"selfHarnessRejections":0,"selfHarnessDrafts":0,"selfHarnessRecurrences":0,
				"doctrineCoverage":{
					"state":"unproven","covered":[],"gaps":[],"axisCoverage":{},
					"sourcePolicy":"core_sources_only_filtered_sources_not_gates","filteredSources":[],
					"selfHarnessAudits":0,"validationCases":0,"easyAnchorCases":0,
					"mixedFrontierCases":0,"hardFrontierCases":0,"opportunities":0
				},
				"nextActions":[],"totalUses":0,"successRate":0,"curatorState":"","createdBy":""
			}`,
		},
		{
			name: "heartbeat empty rows remain omitted",
			got:  HeartbeatShadowReplayResult{Verdict: "insufficient-corpus", Reason: "need fixtures", DryRun: true},
			want: `{
				"ok":false,"verdict":"insufficient-corpus","reason":"need fixtures","fixtures":0,
				"heldInOriginal":0,"heldInCandidate":0,"heldInTotal":0,
				"heldOutOriginal":0,"heldOutCandidate":0,"heldOutTotal":0,"dryRun":true
			}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertLifecycleJSONEqual(t, tt.got, tt.want)
		})
	}
}

func assertLifecycleJSONEqual(t *testing.T, got any, want string) {
	t.Helper()
	gotJSON, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("decode expected JSON: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("JSON shape changed\n got: %s\nwant: %s", gotJSON, want)
	}
}

func lifecycleTestValue[T any](value T) *T {
	return &value
}
