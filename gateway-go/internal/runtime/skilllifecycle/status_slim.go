package skilllifecycle

import (
	"fmt"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	genesiscommon "github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/common"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
)

// Agent-facing status budgets. Full fixture bodies / candidate SKILL.md dumps
// blow past the tool MaxOutput and force head/tail truncation that hides the
// useful middle (overview.nextActions, open self-corrections). Status keeps
// identifiers + short summaries; large bodies are replaced with an omit note.
const (
	statusSlimResultRunes    = 400
	statusSlimEvidenceRunes  = 600
	statusSlimCandidateRunes = 400
	// Bodies longer than this are omitted entirely (not head-truncated) so a
	// 2KB SKILL.md dump cannot crowd out nextActions / open corrections.
	statusSlimBodyOmitAbove = 800
	statusSlimBodyOmitNote  = "[omitted from status — see rejected-edit store / validation corpus]"
)

// slimSkillLifecycleStatusForAgent mutates status in place to drop bulky
// payloads that are rarely needed for routing decisions. Nested slices are
// deep-copied so tracker/jsonl-backed data is not mutated.
func slimSkillLifecycleStatusForAgent(status *chattools.SkillLifecycleStatusResult) {
	if status == nil {
		return
	}
	status.System.SourcePrinciples = nil
	status.System.FilteredPrinciples = nil
	slimStatusUsageStats(status)
	if status.Recent != nil {
		slim := make([]genesis.LifecycleLogEntry, len(*status.Recent))
		copy(slim, *status.Recent)
		for i := range slim {
			slim[i].Candidate = genesiscommon.TruncateRunes(slim[i].Candidate, statusSlimCandidateRunes)
			slim[i].Evidence = genesiscommon.TruncateRunes(slim[i].Evidence, statusSlimEvidenceRunes)
			slim[i].Reason = genesiscommon.TruncateRunes(slim[i].Reason, statusSlimResultRunes)
			slim[i].Result = genesiscommon.TruncateRunes(slim[i].Result, statusSlimResultRunes)
			slim[i].Description = genesiscommon.TruncateRunes(slim[i].Description, statusSlimResultRunes)
		}
		status.Recent = &slim
	}
	if status.RejectedEdits != nil {
		slim := make([]genesis.RejectedSkillEditRecord, len(*status.RejectedEdits))
		copy(slim, *status.RejectedEdits)
		for i := range slim {
			slim[i].Reason = genesiscommon.TruncateRunes(slim[i].Reason, statusSlimResultRunes)
			slim[i].CandidateBody = omitOrTruncateBody(slim[i].CandidateBody)
		}
		status.RejectedEdits = &slim
	}
	if status.ValidationCases != nil {
		slim := make([]genesis.SkillValidationCaseRecord, len(*status.ValidationCases))
		copy(slim, *status.ValidationCases)
		for i := range slim {
			slim[i].Description = genesiscommon.TruncateRunes(slim[i].Description, statusSlimResultRunes)
			slim[i].Replay.Input = genesiscommon.TruncateRunes(slim[i].Replay.Input, statusSlimCandidateRunes)
			slim[i].Replay.Context = append([]string(nil), slim[i].Replay.Context...)
			slim[i].Replay.ExpectedToolCalls = slimReplayToolCalls(slim[i].Replay.ExpectedToolCalls)
			slim[i].Replay.ForbiddenToolCalls = slimReplayToolCalls(slim[i].Replay.ForbiddenToolCalls)
		}
		status.ValidationCases = &slim
	}
	if status.SelfCorrectionCandidates != nil {
		slim := make([]genesis.SelfCorrectionCandidateRecord, len(*status.SelfCorrectionCandidates))
		copy(slim, *status.SelfCorrectionCandidates)
		for i := range slim {
			slim[i].Candidate = genesiscommon.TruncateRunes(slim[i].Candidate, statusSlimCandidateRunes)
			slim[i].Evidence = genesiscommon.TruncateRunes(slim[i].Evidence, statusSlimEvidenceRunes)
			slim[i].ProposedChange = genesiscommon.TruncateRunes(slim[i].ProposedChange, statusSlimEvidenceRunes)
			slim[i].Reason = genesiscommon.TruncateRunes(slim[i].Reason, statusSlimResultRunes)
			slim[i].ReviewNote = genesiscommon.TruncateRunes(slim[i].ReviewNote, statusSlimResultRunes)
			slim[i].OutcomeNote = genesiscommon.TruncateRunes(slim[i].OutcomeNote, statusSlimResultRunes)
			slim[i].TargetFiles = append([]string(nil), slim[i].TargetFiles...)
		}
		status.SelfCorrectionCandidates = &slim
	}
	if status.Opportunities != nil {
		slim := make([]genesis.SkillOpportunityRecord, len(*status.Opportunities))
		copy(slim, *status.Opportunities)
		for i := range slim {
			slim[i].Candidate = genesiscommon.TruncateRunes(slim[i].Candidate, statusSlimCandidateRunes)
			slim[i].Evidence = genesiscommon.TruncateRunes(slim[i].Evidence, statusSlimEvidenceRunes)
			slim[i].Reason = genesiscommon.TruncateRunes(slim[i].Reason, statusSlimResultRunes)
		}
		status.Opportunities = &slim
	}
}

func slimStatusUsageStats(status *chattools.SkillLifecycleStatusResult) {
	if status.Stats == nil {
		return
	}
	if status.Stats.Skill != nil {
		skill := *status.Stats.Skill
		skill.RecentErrors = nil
		skill.RecentFailureTraces = nil
		status.Stats.Skill = &skill
	}
	if len(status.Stats.Fleet) > 0 {
		fleet := make([]genesis.UsageStats, len(status.Stats.Fleet))
		copy(fleet, status.Stats.Fleet)
		for i := range fleet {
			fleet[i].RecentErrors = nil
			fleet[i].RecentFailureTraces = nil
		}
		status.Stats.Fleet = fleet
	}
}

func slimReplayToolCalls(calls []genesis.SkillReplayToolCallRecord) []genesis.SkillReplayToolCallRecord {
	if len(calls) == 0 {
		return calls
	}
	out := make([]genesis.SkillReplayToolCallRecord, len(calls))
	copy(out, calls)
	for i := range out {
		out[i].InputIncludes = append([]string(nil), out[i].InputIncludes...)
		out[i].InputExcludes = append([]string(nil), out[i].InputExcludes...)
		out[i].FixtureOutput = omitOrTruncateBody(out[i].FixtureOutput)
	}
	return out
}

func omitOrTruncateBody(body string) string {
	runes := []rune(body)
	if len(runes) <= statusSlimBodyOmitAbove {
		return genesiscommon.TruncateRunes(body, statusSlimEvidenceRunes)
	}
	return fmt.Sprintf("%s (%d runes)", statusSlimBodyOmitNote, len(runes))
}
