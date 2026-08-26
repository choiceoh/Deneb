package skilllifecycle

import (
	"errors"
	"log/slog"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/llm"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chatport"
	"github.com/choiceoh/deneb/gateway-go/pkg/safego"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

type chatUsageRecorderAdapter struct {
	inner         *genesis.Tracker
	transcripts   toolport.TranscriptStore
	logger        *slog.Logger
	replayEnabled bool
	// relevanceClient/relevanceModel drive a fresh-context relevance classifier
	// (the lightweight role) that filters mis-attributed validation cases before
	// they enter the corpus — a session that merely CONSULTED a skill while the
	// real work was unrelated must not become that skill's held-out case. nil
	// disables the gate (records everything, the prior behavior).
	relevanceClient *llm.Client
	relevanceModel  string
	// spawn runs validation-case capture off the caller's goroutine. Production
	// detaches it (safego.GoWithSlog) so a chat turn never waits on corpus
	// bookkeeping. Tests substitute a synchronous runner: a detached goroutine
	// writing under $HOME outlives the test that started it, and racing
	// t.TempDir()'s cleanup made this package fail roughly one run in four
	// ("TempDir RemoveAll cleanup: .../.deneb: directory not empty"). Sleeping
	// long enough to "usually" win that race is the same bug with a timer.
	spawn func(logger *slog.Logger, name string, fn func())
}

// NewChatUsageRecorder adapts a genesis tracker to chatport.SkillUsageRecorder.
// relevanceClient/relevanceModel are optional: when wired (the lightweight text
// role), successful-use validation-case capture is gated by a relevance check so
// off-topic sessions do not contaminate a skill's held-out corpus. Pass nil to
// keep the record-everything behavior.
func NewChatUsageRecorder(t *genesis.Tracker, transcripts toolport.TranscriptStore, logger *slog.Logger, replayEnabled bool, relevanceClient *llm.Client, relevanceModel string) chatport.SkillUsageRecorder {
	return &chatUsageRecorderAdapter{
		inner: t, transcripts: transcripts, logger: logger, replayEnabled: replayEnabled,
		relevanceClient: relevanceClient, relevanceModel: strings.TrimSpace(relevanceModel),
		spawn: safego.GoWithSlog,
	}
}

// RecordSkillUse records the outcome of one skill invocation.
func (a *chatUsageRecorderAdapter) RecordSkillUse(sessionKey, skillName string, success bool, errMsg, model string) {
	a.recordSkillUse(sessionKey, skillName, success, errMsg, model, chatport.SkillUseAttribution{})
}

// RecordSkillUseAttributed additionally carries WHERE the outcome belongs:
// how the skill reached the turn and whether its declared tools actually ran.
func (a *chatUsageRecorderAdapter) RecordSkillUseAttributed(sessionKey, skillName string, success bool, errMsg, model string, attr chatport.SkillUseAttribution) {
	a.recordSkillUse(sessionKey, skillName, success, errMsg, model, attr)
}

func (a *chatUsageRecorderAdapter) recordSkillUse(sessionKey, skillName string, success bool, errMsg, model string, attr chatport.SkillUseAttribution) {
	if a == nil || a.inner == nil {
		return
	}
	failureTrace := a.failureTraceForSkillUse(sessionKey, success, errMsg)
	if err := a.inner.RecordUsage(genesis.UsageRecord{
		SkillName:    skillName,
		SessionKey:   sessionKey,
		Model:        model,
		Success:      success,
		ErrorMsg:     errMsg,
		FailureTrace: failureTrace,
		Source:       genesis.UsageSourceReal,
		Delivery:     attr.Delivery,
		Exercised:    attr.Exercised,
	}); err != nil && a.logger != nil {
		// Usage telemetry is best-effort — a write failure must never affect the
		// chat turn, but log it so a persistently failing tracker is visible.
		a.logger.Warn("genesis: skill usage record failed", "skill", skillName, "error", err)
	}
	// A skill whose procedure was DELIVERED and then not followed is a failure
	// of that skill, even when the turn itself errored on nothing. Without this
	// the only failures the corpus ever saw were turn-level errors, and skills
	// error at the turn level almost never: 30 days of the live ledger produced
	// 10 real-failure validation cases against 1,695 total, so 98% of what the
	// held-out gate measures is synthetic — and a synthetic corpus is exactly
	// the one where every candidate ties the original (2026-08-26 diagnosis:
	// 118 proposals, 28 rejections, 0 accepted evolves in 30 days).
	unexercised := success && attr.Exercised == chatport.SkillExercisedNo
	switch {
	case !success:
		a.spawnCapture("skill-failed-use-validation-case", func() {
			a.recordValidationCaseFromFailedUse(sessionKey, skillName, errMsg)
		})
	case unexercised:
		a.spawnCapture("skill-unexercised-validation-case", func() {
			a.recordValidationCaseFromFailedUse(sessionKey, skillName,
				"절차가 주입됐지만 선언된 도구가 한 번도 실행되지 않았다 (exercised=no)")
		})
	case a.replayEnabled:
		// Success mirror of the failed-use capture above: record the proven-good
		// tool-call behavior of a successful run as a held-out replay case so the
		// behavioral evolve gate (SkillValidationEngine.EvaluateBehavior) has
		// something to protect against regression — without this corpus the gate
		// is inert. Gated by the same flag as the gate (DENEB_SKILL_EVOLVE_REPLAY)
		// so capture and consume turn on together, and the far-more-frequent
		// successful turns don't write cases nothing reads.
		a.spawnCapture("skill-success-use-validation-case", func() {
			a.recordValidationCaseFromSuccessfulUse(sessionKey, skillName)
		})
	}
}

func (a *chatUsageRecorderAdapter) failureTraceForSkillUse(sessionKey string, success bool, errMsg string) *genesis.UsageFailureTrace {
	if success {
		return nil
	}
	trace := &genesis.UsageFailureTrace{ErrorMsg: strings.TrimSpace(errMsg)}
	if a == nil || a.transcripts == nil || strings.TrimSpace(sessionKey) == "" {
		if trace.ErrorMsg == "" {
			return nil
		}
		return trace
	}
	sctx, err := BuildSessionContext(a.transcripts, sessionKey)
	if err != nil {
		if a.logger != nil {
			a.logger.Debug("genesis: skill failure trace transcript load failed",
				"session", sessionKey, "error", err)
		}
		if trace.ErrorMsg == "" {
			return nil
		}
		return trace
	}
	for i := len(sctx.ToolActivities) - 1; i >= 0; i-- {
		activity := sctx.ToolActivities[i]
		if !activity.IsError {
			continue
		}
		trace.ToolName = strings.TrimSpace(activity.Name)
		trace.ToolInput = textutil.TruncateRunes(strings.TrimSpace(activity.Input), 1000, "\n...(truncated)")
		trace.ToolOutput = textutil.TruncateRunes(strings.TrimSpace(activity.Output), 1000, "\n...(truncated)")
		trace.ToolError = true
		break
	}
	if trace.ErrorMsg == "" && trace.ToolName == "" && trace.ToolInput == "" && trace.ToolOutput == "" {
		return nil
	}
	return trace
}

func (a *chatUsageRecorderAdapter) recordValidationCaseFromFailedUse(sessionKey, skillName, errMsg string) {
	sessionKey = strings.TrimSpace(sessionKey)
	skillName = strings.TrimSpace(skillName)
	if sessionKey == "" || skillName == "" || a.transcripts == nil {
		return
	}
	sctx, err := BuildSessionContext(a.transcripts, sessionKey)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case transcript load failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
		return
	}
	description := "Failed skill use in session " + sessionKey
	if msg := strings.TrimSpace(errMsg); msg != "" {
		description += ": " + textutil.TruncateRunes(msg, 180, "\n...(truncated)")
	}
	record := BuildValidationCaseFromSession(chattools.SkillValidationCaseFromSessionRequest{
		SkillName:   skillName,
		SessionKey:  sessionKey,
		Description: description,
		Source:      "auto-failed-skill-use",
	}, sctx)
	record.Replay = failedUseValidationReplay(record.Replay)
	if a.validationCaseAlreadyRecorded(skillName, record) {
		return
	}
	if err := a.inner.RecordSkillValidationCase(record); err != nil {
		if errors.Is(err, genesis.ErrWeakAutomaticValidationCase) {
			if a.logger != nil {
				a.logger.Debug("genesis: auto validation case skipped weak failed-use trace",
					"skill", skillName, "session", sessionKey)
			}
			return
		}
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case record failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
	}
}

// recordValidationCaseFromSuccessfulUse is the success mirror of
// recordValidationCaseFromFailedUse: it captures a successful run's tool-call
// behavior as a held-out replay case whose ExpectedToolCalls are the proven-good
// calls the agent actually made. This is the corpus the behavioral evolve gate
// (SkillValidationEngine.EvaluateBehavior) consumes — without it the gate has no
// cases and stays inert. The caller gates this on DENEB_SKILL_EVOLVE_REPLAY.
func (a *chatUsageRecorderAdapter) recordValidationCaseFromSuccessfulUse(sessionKey, skillName string) {
	sessionKey = strings.TrimSpace(sessionKey)
	skillName = strings.TrimSpace(skillName)
	if sessionKey == "" || skillName == "" || a.transcripts == nil {
		return
	}
	sctx, err := BuildSessionContext(a.transcripts, sessionKey)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("genesis: auto success validation case transcript load failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
		return
	}
	// Relevance gate: a skill is recorded as "used" when it was merely CONSULTED
	// during the turn (run_agent_config.recordRunSkillUsage), so a session about
	// something unrelated (e.g. a mail/project query that happened to load
	// system-health-check) would otherwise capture that session's tool calls as
	// the skill's held-out case. Those mis-attributed cases demand irrelevant
	// tools the skill's body can never satisfy, pinning it at an unimprovable
	// score so the evolve gate rejects good on-topic rewrites. Skip when the
	// classifier says the session did not exercise this skill.
	if !sessionExercisesSkill(a.logger, a.relevanceClient, a.relevanceModel, skillName, sctx) {
		return
	}
	record := BuildValidationCaseFromSession(chattools.SkillValidationCaseFromSessionRequest{
		SkillName:   skillName,
		SessionKey:  sessionKey,
		Description: "Successful skill use in session " + sessionKey,
		Source:      "auto-successful-skill-use",
	}, sctx)
	// Unlike the failed-use path, keep the auto-extracted ExpectedToolCalls as
	// EXPECTED behavior — the case asserts "a correct run makes these tool
	// calls", exactly what a regressing rewrite must not drop. With no extracted
	// tool calls there is nothing for the behavioral gate to protect (and the
	// weak-automatic guard would reject it), so skip early.
	if len(record.Replay.ExpectedToolCalls) == 0 {
		return
	}
	if a.validationCaseAlreadyRecorded(skillName, record) {
		return
	}
	if err := a.inner.RecordSkillValidationCase(record); err != nil {
		if errors.Is(err, genesis.ErrWeakAutomaticValidationCase) {
			if a.logger != nil {
				a.logger.Debug("genesis: auto success validation case skipped weak trace",
					"skill", skillName, "session", sessionKey)
			}
			return
		}
		if a.logger != nil {
			a.logger.Warn("genesis: auto success validation case record failed",
				"skill", skillName, "session", sessionKey, "error", err)
		}
	}
}

func (a *chatUsageRecorderAdapter) validationCaseAlreadyRecorded(skillName string, record genesis.SkillValidationCaseRecord) bool {
	id := strings.TrimSpace(record.ID)
	if id == "" {
		return false
	}
	cases, err := a.inner.RecentSkillValidationCases(skillName, 50)
	if err != nil {
		if a.logger != nil {
			a.logger.Warn("genesis: auto validation case duplicate check failed",
				"skill", skillName, "id", id, "error", err)
		}
		return false
	}
	nextWeight := validationCaseAssertionWeight(record)
	for _, tc := range cases {
		if strings.TrimSpace(tc.ID) == id && validationCaseAssertionWeight(tc) >= nextWeight {
			return true
		}
	}
	return false
}

func failedUseValidationReplay(replay genesis.SkillReplayCaseRecord) genesis.SkillReplayCaseRecord {
	expected := make([]genesis.SkillReplayToolCallRecord, 0, len(replay.ExpectedToolCalls))
	forbidden := make([]genesis.SkillReplayToolCallRecord, 0, len(replay.ForbiddenToolCalls)+len(replay.ExpectedToolCalls))
	forbidden = append(forbidden, replay.ForbiddenToolCalls...)
	for _, call := range replay.ExpectedToolCalls {
		if !call.FixtureError {
			expected = append(expected, call)
			continue
		}
		if len(call.InputIncludes)+len(call.InputExcludes) == 0 {
			continue
		}
		forbidden = append(forbidden, genesis.SkillReplayToolCallRecord{
			Name:          call.Name,
			InputIncludes: append([]string(nil), call.InputIncludes...),
			InputExcludes: append([]string(nil), call.InputExcludes...),
		})
	}
	replay.ExpectedToolCalls = expected
	replay.ForbiddenToolCalls = forbidden
	replay.RequiredTools = ReplayToolNames(expected)
	if len(expected) < 2 {
		replay.RequireOrder = false
	}
	return replay
}

func validationCaseAssertionWeight(record genesis.SkillValidationCaseRecord) int {
	weight := len(record.RequiredSubstrings) + len(record.ForbiddenSubstrings) + len(record.RequiredHeadings)
	replay := record.Replay
	weight += len(replay.RequiredActions) + len(replay.ForbiddenActions)
	weight += len(replay.RequiredObservations) + len(replay.ForbiddenObservations)
	weight += len(replay.RequiredTools) + len(replay.ForbiddenTools)
	weight += replayToolCallAssertionWeight(replay.ExpectedToolCalls)
	weight += replayToolCallAssertionWeight(replay.ForbiddenToolCalls)
	if replay.RequireOrder && len(replay.ExpectedToolCalls) > 1 {
		weight++
	}
	return weight
}

func replayToolCallAssertionWeight(calls []genesis.SkillReplayToolCallRecord) int {
	weight := 0
	for _, call := range calls {
		if strings.TrimSpace(call.Name) != "" {
			weight++
		}
		weight += len(call.InputIncludes) + len(call.InputExcludes)
		if strings.TrimSpace(call.FixtureOutput) != "" {
			weight++
		}
		if call.FixtureError {
			weight++
		}
	}
	return weight
}

// spawnCapture runs deferred validation-case capture, defaulting to a detached
// goroutine so a zero-value adapter (constructed outside NewChatUsageRecorder)
// still behaves like production rather than panicking on a nil hook.
func (a *chatUsageRecorderAdapter) spawnCapture(name string, fn func()) {
	spawn := a.spawn
	if spawn == nil {
		spawn = safego.GoWithSlog
	}
	spawn(a.logger, name, fn)
}
