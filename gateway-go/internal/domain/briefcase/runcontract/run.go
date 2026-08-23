// Package runcontract owns the stable execution evidence exchanged by the
// Briefcase runtime, evaluator, and command composition root.
package runcontract

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	casepack "github.com/choiceoh/deneb/gateway-go/internal/domain/briefcase"
)

type Arm string

const (
	RunSchemaVersion      = "deneb-briefcase-run/v1"
	ArmRawPrimary     Arm = "raw-primary"
	ArmMemoryAssisted Arm = "memory-assisted"
)

var ErrTurnTimeout = errors.New("briefcase executor turn timed out")

type DeviceStatus string

const (
	DeviceConfirmed   DeviceStatus = "confirmed"
	DeviceFailed      DeviceStatus = "failed"
	DeviceUnconfirmed DeviceStatus = "unconfirmed"
	DeviceDelayed     DeviceStatus = "delayed"
)

type EpisodeResult struct {
	EpisodeID          string   `json:"episodeId"`
	At                 string   `json:"at"`
	Phase              string   `json:"phase,omitempty"`
	Cycle              int      `json:"cycle,omitempty"`
	InputSHA256        string   `json:"inputSha256,omitempty"`
	SystemPromptSHA256 string   `json:"systemPromptSha256,omitempty"`
	Text               string   `json:"text,omitempty"`
	AllText            string   `json:"allText,omitempty"`
	Model              string   `json:"model,omitempty"`
	ProviderModel      string   `json:"providerModel,omitempty"`
	StopReason         string   `json:"stopReason,omitempty"`
	InputTokens        int      `json:"inputTokens,omitempty"`
	OutputTokens       int      `json:"outputTokens,omitempty"`
	Turns              int      `json:"turns,omitempty"`
	ReleasedSource     []string `json:"releasedSourceIds,omitempty"`
	WithheldSource     []string `json:"withheldSourceIds,omitempty"`
}

type SamplingProfile struct {
	Temperature      float64 `json:"temperature"`
	TopP             float64 `json:"topP"`
	FrequencyPenalty float64 `json:"frequencyPenalty"`
	PresencePenalty  float64 `json:"presencePenalty"`
}

type ToolCallRecord struct {
	Name        string `json:"name"`
	ToolCallID  string `json:"toolCallId"`
	InputSHA256 string `json:"inputSha256"`
	Decision    string `json:"decision"`
}

type ToolResultRecord struct {
	Name         string `json:"name"`
	ToolUseID    string `json:"toolUseId"`
	ResultSHA256 string `json:"resultSha256"`
	Error        bool   `json:"error,omitempty"`
}

type DeviceActionRecord struct {
	ActionID          string       `json:"actionId"`
	Kind              string       `json:"kind"`
	Payload           rawJSON      `json:"payload"`
	Fingerprint       string       `json:"fingerprint"`
	Status            DeviceStatus `json:"status"`
	RequestedAt       time.Time    `json:"requestedAt"`
	ReadyAt           time.Time    `json:"readyAt,omitempty"`
	ResolvedAt        time.Time    `json:"resolvedAt,omitempty"`
	Result            rawJSON      `json:"result,omitempty"`
	Failure           string       `json:"failure,omitempty"`
	DuplicateAttempts int          `json:"duplicateAttempts,omitempty"`
}

type RunResult struct {
	SchemaVersion              string               `json:"schemaVersion"`
	RunID                      string               `json:"runId"`
	CaseID                     string               `json:"caseId"`
	CasepackSHA256             string               `json:"casepackSha256"`
	Seed                       int64                `json:"seed"`
	Model                      string               `json:"model"`
	ProviderModel              string               `json:"providerModel"`
	APIMode                    string               `json:"apiMode"`
	SeedForwarded              bool                 `json:"seedForwarded"`
	RecallMode                 string               `json:"recallMode"`
	Arm                        Arm                  `json:"arm"`
	Episodes                   []EpisodeResult      `json:"episodes"`
	ToolCalls                  []ToolCallRecord     `json:"toolCalls"`
	ToolResults                []ToolResultRecord   `json:"toolResults"`
	VisibleSourceIDs           []string             `json:"visibleSourceIds"`
	DeviceLedger               []DeviceActionRecord `json:"deviceLedger,omitempty"`
	DevicePlanSHA256           string               `json:"devicePlanSha256,omitempty"`
	DevicePlanSourceSHA256     string               `json:"devicePlanSourceSha256,omitempty"`
	ToolSchemaSHA256           string               `json:"toolSchemaSha256"`
	EndpointSHA256             string               `json:"endpointSha256"`
	BuildSHA256                string               `json:"buildSha256"`
	ExecutionProfileSHA256     string               `json:"executionProfileSha256"`
	SystemPromptSequenceSHA256 string               `json:"systemPromptSequenceSha256"`
	Sampling                   SamplingProfile      `json:"sampling"`
	ArtifactRoot               string               `json:"artifactRoot"`
	State                      rawJSON              `json:"state"`
}

// HarnessBinding is immutable provenance exposed before execution.
type HarnessBinding struct {
	CaseID                 string `json:"caseId"`
	CasepackSHA256         string `json:"casepackSha256"`
	Seed                   int64  `json:"seed"`
	Model                  string `json:"model"`
	APIMode                string `json:"apiMode"`
	Arm                    Arm    `json:"arm"`
	RecallMode             string `json:"recallMode"`
	DevicePlanSHA256       string `json:"devicePlanSha256,omitempty"`
	DevicePlanSourceSHA256 string `json:"devicePlanSourceSha256,omitempty"`
	ToolSchemaSHA256       string `json:"toolSchemaSha256"`
	EndpointSHA256         string `json:"endpointSha256"`
	BuildSHA256            string `json:"buildSha256"`
	ExecutionProfileSHA256 string `json:"executionProfileSha256"`
}

// LatestExecutorText returns the most recent completed model answer.
func LatestExecutorText(run *RunResult) string {
	if run == nil {
		return ""
	}
	for i := len(run.Episodes) - 1; i >= 0; i-- {
		episode := run.Episodes[i]
		if episode.Model != "" || episode.Text != "" || episode.AllText != "" || episode.Phase == "follow-up" {
			return episode.Text
		}
	}
	return ""
}

// ExecutionProfile is the fixed deterministic execution contract whose digest
// is recorded in every run.
type ExecutionProfile struct {
	Version                 string          `json:"version"`
	Model                   string          `json:"model"`
	APIMode                 string          `json:"apiMode"`
	ToolSchema              string          `json:"toolSchemaSha256"`
	Endpoint                string          `json:"endpointSha256"`
	Build                   string          `json:"buildSha256"`
	Sampling                SamplingProfile `json:"sampling"`
	MemoryTokens            uint64          `json:"memoryTokenBudget"`
	SystemTokens            uint64          `json:"systemPromptBudget"`
	FreshTail               uint32          `json:"freshTailCount"`
	SingleAttempt           bool            `json:"singleAttempt"`
	BudgetGrace             bool            `json:"budgetGrace"`
	OutputRecovery          int             `json:"outputRecovery"`
	CumulativeTurns         bool            `json:"cumulativeTurns"`
	CumulativeOutputTokens  bool            `json:"cumulativeOutputTokens"`
	ExplicitStopReason      bool            `json:"explicitStopReason"`
	StreamByteFactor        int             `json:"streamByteFactor"`
	StreamByteCeiling       int             `json:"streamByteCeiling"`
	StreamIdleTimeoutMillis int64           `json:"streamIdleTimeoutMs"`
	ParallelToolsEnabled    bool            `json:"parallelToolsEnabled"`
	PromptCacheEnabled      bool            `json:"promptCacheEnabled"`
}

func FixedExecutionProfile(model, apiMode, toolSchema, endpoint, build string, sampling SamplingProfile) ExecutionProfile {
	return ExecutionProfile{
		Version: "deneb-briefcase-execution-profile/v1", Model: model, APIMode: apiMode,
		ToolSchema: toolSchema, Endpoint: endpoint, Build: build, Sampling: sampling,
		MemoryTokens: 170_000, SystemTokens: 30_000, FreshTail: 24,
		SingleAttempt: true, BudgetGrace: false, OutputRecovery: 0,
		CumulativeTurns: true, CumulativeOutputTokens: true, ExplicitStopReason: true,
		StreamByteFactor: 16, StreamByteCeiling: 8 << 20,
		StreamIdleTimeoutMillis: 180_000,
		ParallelToolsEnabled:    false,
		PromptCacheEnabled:      false,
	}
}

func ExecutionProfileDigest(model, apiMode, toolSchema, endpoint, build string, sampling SamplingProfile) (string, error) {
	encoded, err := json.Marshal(FixedExecutionProfile(model, apiMode, toolSchema, endpoint, build, sampling))
	if err != nil {
		return "", fmt.Errorf("briefcase: encode execution profile: %w", err)
	}
	return casepack.DigestBytes(encoded), nil
}

func SystemPromptSequenceDigest(digests []string) (string, error) {
	encoded, err := json.Marshal(digests)
	if err != nil {
		return "", err
	}
	return casepack.DigestBytes(encoded), nil
}

func ValidateRunProvenance(result *RunResult) error {
	if result == nil {
		return errors.New("briefcase: run result is nil")
	}
	for name, digest := range map[string]string{
		"casepackSha256": result.CasepackSHA256, "toolSchemaSha256": result.ToolSchemaSHA256,
		"endpointSha256": result.EndpointSHA256, "buildSha256": result.BuildSHA256,
		"executionProfileSha256":     result.ExecutionProfileSHA256,
		"systemPromptSequenceSha256": result.SystemPromptSequenceSHA256,
	} {
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size || digest != strings.ToLower(digest) {
			return fmt.Errorf("briefcase: %s must be lowercase SHA-256", name)
		}
	}
	wantSampling := SamplingProfile{Temperature: 0, TopP: 1}
	if result.Sampling != wantSampling {
		return errors.New("briefcase: run sampling profile is not the fixed deterministic profile")
	}
	digests := make([]string, 0, len(result.Episodes))
	if strings.TrimSpace(result.ProviderModel) == "" {
		return errors.New("briefcase: run providerModel is required")
	}
	for _, episode := range result.Episodes {
		if episode.Model == "" && episode.Text == "" && episode.AllText == "" {
			continue
		}
		decoded, err := hex.DecodeString(episode.SystemPromptSHA256)
		if err != nil || len(decoded) != sha256.Size || episode.SystemPromptSHA256 != strings.ToLower(episode.SystemPromptSHA256) {
			return fmt.Errorf("briefcase: executor episode %q has invalid systemPromptSha256", episode.EpisodeID)
		}
		if episode.ProviderModel != result.ProviderModel {
			return fmt.Errorf("briefcase: executor episode %q provider model does not match the run", episode.EpisodeID)
		}
		digests = append(digests, episode.SystemPromptSHA256)
	}
	wantSequence, err := SystemPromptSequenceDigest(digests)
	if err != nil {
		return err
	}
	if wantSequence != result.SystemPromptSequenceSHA256 {
		return errors.New("briefcase: system prompt sequence digest mismatch")
	}
	wantProfile, err := ExecutionProfileDigest(result.Model, result.APIMode, result.ToolSchemaSHA256, result.EndpointSHA256, result.BuildSHA256, result.Sampling)
	if err != nil {
		return err
	}
	if wantProfile != result.ExecutionProfileSHA256 {
		return errors.New("briefcase: execution profile digest mismatch")
	}
	return nil
}

func CloneRunResult(result *RunResult) *RunResult {
	if result == nil {
		return nil
	}
	clone := *result
	clone.Episodes = append([]EpisodeResult(nil), result.Episodes...)
	for i := range clone.Episodes {
		clone.Episodes[i].ReleasedSource = append([]string(nil), result.Episodes[i].ReleasedSource...)
		clone.Episodes[i].WithheldSource = append([]string(nil), result.Episodes[i].WithheldSource...)
	}
	clone.ToolCalls = append([]ToolCallRecord(nil), result.ToolCalls...)
	clone.ToolResults = append([]ToolResultRecord(nil), result.ToolResults...)
	clone.VisibleSourceIDs = append([]string(nil), result.VisibleSourceIDs...)
	clone.DeviceLedger = append([]DeviceActionRecord(nil), result.DeviceLedger...)
	for i := range clone.DeviceLedger {
		clone.DeviceLedger[i].Payload = append(json.RawMessage(nil), result.DeviceLedger[i].Payload...)
		clone.DeviceLedger[i].Result = append(json.RawMessage(nil), result.DeviceLedger[i].Result...)
	}
	clone.State = append(json.RawMessage(nil), result.State...)
	return &clone
}
