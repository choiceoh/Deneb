package runcontract

import (
	"encoding/json"
	"strings"
	"testing"
)

func setRunProvenanceForTest(t *testing.T, run *RunResult) {
	t.Helper()
	run.Sampling = SamplingProfile{Temperature: 0, TopP: 1}
	digests := make([]string, 0, len(run.Episodes))
	for _, episode := range run.Episodes {
		if episode.SystemPromptSHA256 != "" {
			digests = append(digests, episode.SystemPromptSHA256)
		}
	}
	var err error
	run.SystemPromptSequenceSHA256, err = SystemPromptSequenceDigest(digests)
	if err != nil {
		t.Fatal(err)
	}
	run.ExecutionProfileSHA256, err = ExecutionProfileDigest(run.Model, run.APIMode, run.ToolSchemaSHA256, run.EndpointSHA256, run.BuildSHA256, run.Sampling)
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunContractProvenanceAndWireRoundTrip(t *testing.T) {
	digest := strings.Repeat("a", 64)
	run := &RunResult{
		SchemaVersion:    RunSchemaVersion,
		RunID:            "run-1",
		CaseID:           "case-1",
		CasepackSHA256:   digest,
		Model:            "executor-model",
		ProviderModel:    "served-model",
		APIMode:          "openai",
		Arm:              ArmMemoryAssisted,
		RecallMode:       "enabled",
		ToolSchemaSHA256: digest,
		EndpointSHA256:   digest,
		BuildSHA256:      digest,
		Episodes: []EpisodeResult{{
			EpisodeID: "turn-1", Text: "answer", Model: "executor-model",
			ProviderModel: "served-model", StopReason: "end_turn", SystemPromptSHA256: digest,
		}},
		State: json.RawMessage(`{"ok":true}`),
	}
	setRunProvenanceForTest(t, run)
	if err := ValidateRunProvenance(run); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var decoded RunResult
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RunID != "run-1" || decoded.Arm != ArmMemoryAssisted || LatestExecutorText(&decoded) != "answer" {
		t.Fatalf("wire round trip changed run: %#v", decoded)
	}
}

func TestCloneRunResultPreservesOriginalEvidence(t *testing.T) {
	run := &RunResult{
		Episodes:     []EpisodeResult{{ReleasedSource: []string{"source-1"}}},
		DeviceLedger: []DeviceActionRecord{{Payload: json.RawMessage(`{"a":1}`)}},
		State:        json.RawMessage(`{"state":1}`),
	}
	clone := CloneRunResult(run)
	clone.Episodes[0].ReleasedSource[0] = "changed"
	clone.DeviceLedger[0].Payload[2] = 'b'
	clone.State[2] = 'x'
	if run.Episodes[0].ReleasedSource[0] != "source-1" || string(run.DeviceLedger[0].Payload) != `{"a":1}` || string(run.State) != `{"state":1}` {
		t.Fatalf("clone mutated source: %#v", run)
	}
}
