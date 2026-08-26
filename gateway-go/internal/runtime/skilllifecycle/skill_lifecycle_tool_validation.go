package skilllifecycle

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/toolport"
	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"
	"github.com/choiceoh/deneb/gateway-go/pkg/textutil"
)

// Self-correction and validation-case recording split out of
// skill_lifecycle_tool.go (pure move, no behavior change): candidate
// record/review, validation-case capture, and session backfill.

const (
	// maxDistinctReplayShapes caps how many DISTINCT tool-call shapes one case
	// asserts. A session is a multi-turn trajectory while the behavioral gate
	// replays a single plan, so encoding the whole trace made the fixture
	// unsatisfiable by construction: the backfill lane averaged 11 distinct
	// shapes per case, and the UNCHANGED incumbent skill scored 15-63% against
	// its own corpus. A gate the incumbent cannot pass cannot rank a rewrite —
	// 15 straight rejections and zero landed evolutions (2026-07-25 → 08-08).
	maxDistinctReplayShapes = 4
	// maxOrderedReplayCalls bounds when sequence itself is asserted.
	maxOrderedReplayCalls = 3
)

// RecordSelfCorrectionCandidate queues an evidence-backed correction for later review.
func (b *skillLifecycleBackend) RecordSelfCorrectionCandidate(ctx context.Context, req chattools.SkillSelfCorrectionCandidateRequest) (chattools.SkillSelfCorrectionCandidateResult, error) {
	if b.tracker == nil {
		return chattools.SkillSelfCorrectionCandidateResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	if req.SessionKey == "" {
		req.SessionKey = toolport.SessionKeyFromContext(ctx)
	}
	// Default provenance: agent-proposed candidates were landing with an empty
	// source (observed 2026-07-04), indistinguishable from legacy rows.
	if strings.TrimSpace(req.Source) == "" {
		req.Source = "skill-lifecycle-tool"
	}
	rec, err := b.tracker.RecordSelfCorrectionCandidate(genesis.SelfCorrectionCandidateRecord{
		ID:             req.ID,
		Scope:          req.Scope,
		SkillName:      req.SkillName,
		SessionKey:     req.SessionKey,
		Title:          req.Title,
		Candidate:      req.Candidate,
		Evidence:       req.Evidence,
		Reason:         req.Reason,
		TargetFiles:    req.TargetFiles,
		ProposedChange: req.ProposedChange,
		Risk:           req.Risk,
		Source:         req.Source,
	})
	if err != nil {
		return chattools.SkillSelfCorrectionCandidateResult{}, err
	}
	b.clearSkillLifecycleStatusCache()
	return chattools.SkillSelfCorrectionCandidateResult{
		OK:        true,
		Candidate: &rec,
	}, nil
}

// ReviewSelfCorrectionCandidate records the disposition of a queued correction.
func (b *skillLifecycleBackend) ReviewSelfCorrectionCandidate(_ context.Context, req chattools.SkillSelfCorrectionReviewRequest) (chattools.SkillSelfCorrectionReviewResult, error) {
	if b.tracker == nil {
		return chattools.SkillSelfCorrectionReviewResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	rec, err := b.tracker.RecordSelfCorrectionReview(genesis.SelfCorrectionCandidateRecord{
		ID:         req.ID,
		Status:     req.Status,
		Reviewer:   req.Reviewer,
		ReviewNote: req.ReviewNote,
	})
	if errors.Is(err, genesis.ErrSelfCorrectionTransition) {
		// Already settled by another path. This is an ANSWER, not a tool
		// failure: the review contract tells the model to stop the turn on an
		// error, so returning one here let a single stale id — carried in from
		// memory or a spillover rather than the current status output — cost
		// every other candidate in that turn its review. Say what happened and
		// let the turn continue.
		return chattools.SkillSelfCorrectionReviewResult{
			Reason: "이 후보는 이미 종결됐습니다 (" + err.Error() + "). 다른 경로에서 처리된 건이니 건너뛰고 나머지를 계속 검토하세요 — status 출력에 실제로 들어 있는 후보만 대상입니다.",
		}, nil
	}
	if err != nil {
		return chattools.SkillSelfCorrectionReviewResult{}, err
	}
	b.clearSkillLifecycleStatusCache()
	return chattools.SkillSelfCorrectionReviewResult{
		OK:     true,
		Review: &rec,
	}, nil
}

// RecordSkillValidationCase persists a held-out assertion for one named skill.
func (b *skillLifecycleBackend) RecordSkillValidationCase(_ context.Context, req chattools.SkillValidationCaseRequest) (chattools.SkillValidationCaseResult, error) {
	if b.tracker == nil {
		return chattools.SkillValidationCaseResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	// A validation_case is a held-out assertion bound to a specific skill. A
	// session-level correction with no skill to attach to belongs in the
	// self-correction queue instead — steer the caller there so the capture is
	// not lost when it reached for the wrong action (observed: the review lane
	// hitting a bare "skillName is required" and dropping the correction).
	if strings.TrimSpace(req.SkillName) == "" {
		return chattools.SkillValidationCaseResult{}, fmt.Errorf("validation_case requires a skill binding (skillName). For a session-level correction with no owning skill, use action=self_correction instead (title/evidence/proposedChange/risk)")
	}
	record := genesis.SkillValidationCaseRecord{
		SkillName:           req.SkillName,
		ID:                  req.ID,
		Description:         req.Description,
		FrontierTier:        req.FrontierTier,
		RequiredSubstrings:  req.RequiredSubstrings,
		ForbiddenSubstrings: req.ForbiddenSubstrings,
		RequiredHeadings:    req.RequiredHeadings,
		Replay: genesis.SkillReplayCaseRecord{
			Input:                 req.Replay.Input,
			Context:               req.Replay.Context,
			RequiredActions:       req.Replay.RequiredActions,
			ForbiddenActions:      req.Replay.ForbiddenActions,
			RequiredObservations:  req.Replay.RequiredObservations,
			ForbiddenObservations: req.Replay.ForbiddenObservations,
			RequiredTools:         req.Replay.RequiredTools,
			ForbiddenTools:        req.Replay.ForbiddenTools,
			ExpectedToolCalls:     skillReplayToolCallsFromRequest(req.Replay.ExpectedToolCalls),
			ForbiddenToolCalls:    skillReplayToolCallsFromRequest(req.Replay.ForbiddenToolCalls),
			RequireOrder:          req.Replay.RequireOrder,
		},
		Source: req.Source,
	}
	if err := b.tracker.RecordSkillValidationCase(record); err != nil {
		return chattools.SkillValidationCaseResult{}, err
	}
	b.clearSkillLifecycleStatusCache()
	return chattools.SkillValidationCaseResult{
		OK:        true,
		SkillName: lifecycleValue(strings.TrimSpace(req.SkillName)),
		ID:        lifecycleValue(strings.TrimSpace(req.ID)),
	}, nil
}

// RecordSkillValidationCaseFromSession derives and persists a replay case from a transcript.
func (b *skillLifecycleBackend) RecordSkillValidationCaseFromSession(ctx context.Context, req chattools.SkillValidationCaseFromSessionRequest) (chattools.SkillValidationCaseFromSessionResult, error) {
	if b.tracker == nil {
		return chattools.SkillValidationCaseFromSessionResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	if req.SessionKey == "" {
		req.SessionKey = toolport.SessionKeyFromContext(ctx)
	}
	if strings.TrimSpace(req.SessionKey) == "" {
		return chattools.SkillValidationCaseFromSessionResult{}, fmt.Errorf("sessionKey is required")
	}
	sctx, err := buildSkillLifecycleSessionContext(b.transcripts, req.SessionKey)
	if err != nil {
		return chattools.SkillValidationCaseFromSessionResult{}, fmt.Errorf("load session: %w", err)
	}
	record := buildSkillValidationCaseFromSession(req, sctx)
	if err := b.tracker.RecordSkillValidationCase(record); err != nil {
		if errors.Is(err, genesis.ErrWeakAutomaticValidationCase) {
			return chattools.SkillValidationCaseFromSessionResult{
				OK:         true,
				Skip:       true,
				Reason:     err.Error(),
				SkillName:  lifecycleValue(strings.TrimSpace(req.SkillName)),
				SessionKey: lifecycleValue(strings.TrimSpace(req.SessionKey)),
			}, nil
		}
		return chattools.SkillValidationCaseFromSessionResult{}, err
	}
	b.clearSkillLifecycleStatusCache()
	return chattools.SkillValidationCaseFromSessionResult{
		OK:                 true,
		SkillName:          lifecycleValue(strings.TrimSpace(req.SkillName)),
		ID:                 lifecycleValue(record.ID),
		SessionKey:         lifecycleValue(strings.TrimSpace(req.SessionKey)),
		ExpectedToolCalls:  lifecycleValue(len(record.Replay.ExpectedToolCalls)),
		ForbiddenToolCalls: lifecycleValue(len(record.Replay.ForbiddenToolCalls)),
		RequiredTools:      lifecycleValue(len(record.Replay.RequiredTools)),
	}, nil
}

// BackfillSkillValidationCases mines recent transcripts for additional held-out cases.
func (b *skillLifecycleBackend) BackfillSkillValidationCases(ctx context.Context, req chattools.SkillValidationBackfillRequest) (chattools.SkillValidationBackfillResult, error) {
	if b.tracker == nil {
		return chattools.SkillValidationBackfillResult{
			Reason: "skill tracker is not configured",
		}, nil
	}
	skillName := strings.TrimSpace(req.SkillName)
	if skillName == "" {
		return chattools.SkillValidationBackfillResult{}, fmt.Errorf("skillName is required")
	}

	limit := normalizeSkillValidationBackfillLimit(req.Limit)
	sessionKey := strings.TrimSpace(req.SessionKey)
	if sessionKey != "" {
		return b.backfillSkillValidationCasesFromKeys(ctx, req, []string{sessionKey}, 1)
	}
	if b.transcripts == nil {
		return chattools.SkillValidationBackfillResult{}, fmt.Errorf("transcript store is not configured")
	}
	keys, err := b.transcripts.ListKeys()
	if err != nil {
		return chattools.SkillValidationBackfillResult{}, fmt.Errorf("list transcripts: %w", err)
	}
	sort.Strings(keys)
	for i, j := 0, len(keys)-1; i < j; i, j = i+1, j-1 {
		keys[i], keys[j] = keys[j], keys[i]
	}
	return b.backfillSkillValidationCasesFromKeys(ctx, req, keys, limit)
}

func (b *skillLifecycleBackend) backfillSkillValidationCasesFromKeys(ctx context.Context, req chattools.SkillValidationBackfillRequest, keys []string, limit int) (chattools.SkillValidationBackfillResult, error) {
	var (
		scanned  int
		recorded int
		skipped  int
		errs     []string
		details  []chattools.SkillValidationBackfillDetail
	)
	for _, key := range keys {
		if scanned >= limit {
			break
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		scanned++
		// Relevance gate: RecentRealUseSessionsBySkill attributes a session to a
		// skill on the "consulted" signal, so an off-topic session that merely
		// loaded the skill would otherwise be backfilled as its held-out case.
		// Skip when the classifier says the session did not exercise the skill.
		if b.relevanceClient != nil && b.transcripts != nil {
			if sctx, serr := BuildSessionContext(b.transcripts, key); serr == nil &&
				!sessionExercisesSkill(b.logger, b.relevanceClient, b.relevanceModel, req.SkillName, sctx) {
				skipped++
				if len(details) < 20 {
					details = append(details, chattools.SkillValidationBackfillDetail{
						SessionKey: key, OK: true, Skip: true, Reason: "off-topic: session did not exercise the skill",
					})
				}
				continue
			}
		}
		got, err := b.RecordSkillValidationCaseFromSession(ctx, skillValidationBackfillCaseRequest(req, key))
		if err != nil {
			errText := key + ": " + err.Error()
			errs = append(errs, errText)
			if len(details) < 20 {
				details = append(details, chattools.SkillValidationBackfillDetail{
					SessionKey: key,
					Error:      err.Error(),
				})
			}
			continue
		}
		if got.Skip {
			skipped++
			if len(details) < 20 {
				details = append(details, chattools.SkillValidationBackfillDetail{
					SessionKey: key,
					OK:         true,
					Skip:       true,
					Reason:     got.Reason,
				})
			}
			continue
		}
		recorded++
		if len(details) < 20 {
			details = append(details, chattools.SkillValidationBackfillDetail{
				SessionKey:         key,
				OK:                 true,
				ID:                 got.ID,
				ExpectedToolCalls:  got.ExpectedToolCalls,
				ForbiddenToolCalls: got.ForbiddenToolCalls,
				RequiredTools:      got.RequiredTools,
			})
		}
	}
	result := chattools.SkillValidationBackfillResult{
		OK:        true,
		SkillName: lifecycleValue(strings.TrimSpace(req.SkillName)),
		Limit:     lifecycleValue(limit),
		Scanned:   lifecycleValue(scanned),
		Recorded:  lifecycleValue(recorded),
		Skipped:   lifecycleValue(skipped),
		Errors:    lifecycleValue(errs),
		Details:   lifecycleValue(details),
	}
	if summary, summaryErr := b.validationCaseSummary(strings.TrimSpace(req.SkillName)); summaryErr == "" {
		result.ValidationCaseSummary = &summary
	} else {
		result.ValidationCaseSummaryError = summaryErr
	}
	return result, nil
}

func skillValidationBackfillCaseRequest(req chattools.SkillValidationBackfillRequest, sessionKey string) chattools.SkillValidationCaseFromSessionRequest {
	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "session-backfill"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Backfilled replay trace from session " + strings.TrimSpace(sessionKey)
	}
	return chattools.SkillValidationCaseFromSessionRequest{
		SkillName:    req.SkillName,
		SessionKey:   sessionKey,
		Description:  description,
		FrontierTier: req.FrontierTier,
		Replay:       req.Replay,
		Source:       source,
	}
}

func skillReplayToolCallsFromRequest(calls []chattools.SkillReplayToolCallRequest) []genesis.SkillReplayToolCallRecord {
	out := make([]genesis.SkillReplayToolCallRecord, 0, len(calls))
	for _, call := range calls {
		out = append(out, genesis.SkillReplayToolCallRecord{
			Name:          call.Name,
			InputIncludes: call.InputIncludes,
			InputExcludes: call.InputExcludes,
			FixtureOutput: call.FixtureOutput,
			FixtureError:  call.FixtureError,
		})
	}
	return out
}

func buildSkillValidationCaseFromSession(req chattools.SkillValidationCaseFromSessionRequest, sctx generation.SessionContext) genesis.SkillValidationCaseRecord {
	replay := genesis.SkillReplayCaseRecord{
		Input:                 textutil.FirstNonBlank(req.Replay.Input, skillReplayInputFromTranscript(sctx.AllText)),
		Context:               append([]string(nil), req.Replay.Context...),
		RequiredActions:       append([]string(nil), req.Replay.RequiredActions...),
		ForbiddenActions:      append([]string(nil), req.Replay.ForbiddenActions...),
		RequiredObservations:  append([]string(nil), req.Replay.RequiredObservations...),
		ForbiddenObservations: append([]string(nil), req.Replay.ForbiddenObservations...),
		RequiredTools:         append([]string(nil), req.Replay.RequiredTools...),
		ForbiddenTools:        append([]string(nil), req.Replay.ForbiddenTools...),
		ExpectedToolCalls:     skillReplayToolCallsFromRequest(req.Replay.ExpectedToolCalls),
		ForbiddenToolCalls:    skillReplayToolCallsFromRequest(req.Replay.ForbiddenToolCalls),
		RequireOrder:          req.Replay.RequireOrder,
	}
	replay.Context = append(replay.Context, skillReplayContextFromTranscript(sctx.AllText)...)

	autoExpectedCalls, autoForbiddenCalls := skillReplayToolCallsFromActivities(sctx.ToolActivities)
	if len(autoExpectedCalls) > 0 {
		replay.ExpectedToolCalls = append(autoExpectedCalls, replay.ExpectedToolCalls...)
		replay.RequiredTools = appendUniqueStrings(replay.RequiredTools, skillReplayToolNames(autoExpectedCalls)...)
		// Order is only a fair assertion while the sequence is short enough for
		// one plan to lay out. Demanding the full ordering of a long trajectory
		// was a guaranteed miss — "expected tool calls are out of order" is 7%
		// of every replay failure on record.
		if len(autoExpectedCalls) > 1 && len(autoExpectedCalls) <= maxOrderedReplayCalls {
			replay.RequireOrder = true
		}
	}
	replay.ForbiddenToolCalls = append(autoForbiddenCalls, replay.ForbiddenToolCalls...)

	source := strings.TrimSpace(req.Source)
	if source == "" {
		source = "review-session"
	}
	description := strings.TrimSpace(req.Description)
	if description == "" {
		description = "Replay trace extracted from session " + strings.TrimSpace(req.SessionKey)
	}
	id := strings.TrimSpace(req.ID)
	if id == "" {
		id = skillValidationCaseIDFromSession(req.SessionKey)
	}
	return genesis.SkillValidationCaseRecord{
		SkillName:           req.SkillName,
		ID:                  id,
		Description:         description,
		FrontierTier:        req.FrontierTier,
		RequiredSubstrings:  req.RequiredSubstrings,
		ForbiddenSubstrings: req.ForbiddenSubstrings,
		RequiredHeadings:    req.RequiredHeadings,
		Replay:              replay,
		Source:              source,
	}
}

// BuildValidationCaseFromSession derives a held-out replay record from a real
// session trace.
func BuildValidationCaseFromSession(req chattools.SkillValidationCaseFromSessionRequest, sctx generation.SessionContext) genesis.SkillValidationCaseRecord {
	return buildSkillValidationCaseFromSession(req, sctx)
}

// replayVolatileFragment matches argument substrings welded to the session that
// produced them — timestamps, absolute paths, ids, long digit runs. Asserting on
// those turns "did the rewrite break what worked" into "reproduce that July
// session verbatim": a replay can pick the right tool for the right reason and
// still never emit the same date or path.
var replayVolatileFragment = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|/home/|~/|[0-9a-f]{8,}|\d{6,}`)

// stableReplayIncludes drops session-bound fragments. Returning nil is the point
// when everything was volatile: the call keeps asserting the TOOL, which is the
// durable half of the observation, and stops asserting an unrepeatable argument.
func stableReplayIncludes(includes []string) []string {
	out := make([]string, 0, len(includes))
	for _, include := range includes {
		if replayVolatileFragment.MatchString(include) {
			continue
		}
		out = append(out, include)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func replayCallShapeKey(call genesis.SkillReplayToolCallRecord) string {
	return call.Name + "\x00" + strings.Join(call.InputIncludes, "\x01")
}

func skillReplayToolCallsFromActivities(activities []generation.ToolActivity) ([]genesis.SkillReplayToolCallRecord, []genesis.SkillReplayToolCallRecord) {
	const maxExtractedReplayToolCalls = 12
	expected := make([]genesis.SkillReplayToolCallRecord, 0, min(len(activities), maxDistinctReplayShapes))
	forbidden := make([]genesis.SkillReplayToolCallRecord, 0, min(len(activities), maxExtractedReplayToolCalls))
	seen := make(map[string]struct{}, maxExtractedReplayToolCalls)
	for _, activity := range activities {
		name := strings.TrimSpace(activity.Name)
		if name == "" {
			continue
		}
		call := genesis.SkillReplayToolCallRecord{
			Name:          name,
			InputIncludes: stableReplayIncludes(skillReplayInputIncludes(activity.Input)),
			FixtureOutput: truncateRunes(strings.TrimSpace(activity.Output), 1000),
			FixtureError:  activity.IsError,
		}
		// A trajectory repeats the same shape (exec seven times in a row is the
		// median here); the gate matches on existence, so the repeats add no
		// signal and only crowd out distinct behavior.
		key := replayCallShapeKey(call)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		if activity.IsError {
			if len(call.InputIncludes)+len(call.InputExcludes) > 0 {
				forbidden = append(forbidden, genesis.SkillReplayToolCallRecord{
					Name:          call.Name,
					InputIncludes: append([]string(nil), call.InputIncludes...),
					InputExcludes: append([]string(nil), call.InputExcludes...),
				})
			}
		} else if len(expected) < maxDistinctReplayShapes {
			expected = append(expected, call)
		}
		if len(expected)+len(forbidden) >= maxExtractedReplayToolCalls {
			break
		}
	}
	return expected, forbidden
}

func skillReplayToolNames(calls []genesis.SkillReplayToolCallRecord) []string {
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if name := strings.TrimSpace(call.Name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// ReplayToolNames returns the stable unique tool-name list for replay calls.
func ReplayToolNames(calls []genesis.SkillReplayToolCallRecord) []string {
	return skillReplayToolNames(calls)
}
