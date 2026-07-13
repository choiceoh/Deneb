package handlerminiapp

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func testSelfImprovementCodingDeps() SelfImprovementCodingDeps {
	return SelfImprovementCodingDeps{
		RecentCandidates: func(status string, limit int) ([]genesis.SelfCorrectionCandidateRecord, error) {
			recs := []genesis.SelfCorrectionCandidateRecord{{
				ID:             "sc-1",
				Status:         genesis.SelfCorrectionStatusProposed,
				Scope:          "code",
				Title:          "코딩 모델 후보 표시",
				Candidate:      "native 설정에 pending 후보를 노출",
				Evidence:       "PR #2624에서 self_correction queue를 추가",
				TargetFiles:    []string{"client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/ConfigSelfImprovementCodingTab.kt"},
				ProposedChange: "자가개선 코딩 화면에서 후보 큐를 렌더링",
				Risk:           "후보와 적용 완료 이벤트가 섞이면 혼란",
				Source:         "self-correction",
				DispatchPhase:  genesis.SelfCorrectionDispatchMerged,
				AttemptID:      "attempt-1",
				PRNumber:       42,
				CommitSHA:      "merge-sha",
				CreatedAt:      444,
				UpdatedAt:      444,
			}, {
				ID:             "sc-2",
				Status:         genesis.SelfCorrectionStatusApplied,
				Scope:          "code",
				Title:          "적용된 코드 후보",
				ProposedChange: "검증된 후보를 적용 완료로 표시",
				CreatedAt:      333,
				UpdatedAt:      333,
			}, {
				ID:             "sc-3",
				Status:         genesis.SelfCorrectionStatusRejected,
				Scope:          "code",
				Title:          "기각된 코드 후보",
				ProposedChange: "근거가 약한 후보를 숨기지 않고 기각으로 보존",
				CreatedAt:      222,
				UpdatedAt:      222,
			}}
			if status != "" {
				filtered := make([]genesis.SelfCorrectionCandidateRecord, 0, len(recs))
				for _, rec := range recs {
					if rec.Status == status {
						filtered = append(filtered, rec)
					}
				}
				recs = filtered
			}
			if limit > 0 && limit < len(recs) {
				return recs[:limit], nil
			}
			return recs, nil
		},
		Funnel: func() genesis.SelfCorrectionFunnelSummary {
			return genesis.SelfCorrectionFunnelSummary{
				LastCaptureAt:          444,
				LastReviewAt:           555,
				Rejections7d:           2,
				PromotableRejections7d: 1,
				LastRejectionAt:        666,
				PendingCount:           2,
				OldestPendingAgeMs:     888,
				Dispatched7d:           3,
				WatchPassed7d:          1,
				RolledBack7d:           1,
			}
		},
		LastNudgeAtMs: func() int64 { return 777 },
	}
}

func TestSelfImprovementCodingList_PendingCandidates(t *testing.T) {
	h := selfImprovementCodingList(testSelfImprovementCodingDeps())
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.self_improvement_coding.list"})
	payload := decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)

	if payload.Count != 1 || len(payload.Candidates) != 1 {
		t.Fatalf("expected one candidate, got %+v", payload)
	}
	if countSelfImprovementCodingStatus(payload.StatusCounts, "all") != 3 ||
		countSelfImprovementCodingStatus(payload.StatusCounts, genesis.SelfCorrectionStatusProposed) != 1 ||
		countSelfImprovementCodingStatus(payload.StatusCounts, genesis.SelfCorrectionStatusApplied) != 1 ||
		countSelfImprovementCodingStatus(payload.StatusCounts, genesis.SelfCorrectionStatusRejected) != 1 {
		t.Fatalf("unexpected status counts: %+v", payload.StatusCounts)
	}
	candidate := payload.Candidates[0]
	if candidate.ID != "sc-1" ||
		candidate.Status != genesis.SelfCorrectionStatusProposed ||
		candidate.Scope != "code" ||
		candidate.Title != "코딩 모델 후보 표시" ||
		candidate.ProposedChange != "자가개선 코딩 화면에서 후보 큐를 렌더링" ||
		len(candidate.TargetFiles) != 1 {
		t.Fatalf("unexpected self-improvement coding candidate: %+v", candidate)
	}
	if candidate.DispatchPhase != genesis.SelfCorrectionDispatchMerged || candidate.AttemptID != "attempt-1" ||
		candidate.PRNumber != 42 || candidate.CommitSHA != "merge-sha" {
		t.Fatalf("dispatch provenance missing from candidate: %+v", candidate)
	}
	if len(candidate.EvidenceKinds) != 3 ||
		candidate.EvidenceKinds[0] != "evidence" ||
		candidate.EvidenceKinds[1] != "target_files" ||
		candidate.EvidenceKinds[2] != "risk" {
		t.Fatalf("unexpected evidence kinds: %+v", candidate.EvidenceKinds)
	}
	if len(candidate.ReviewActions) != 3 ||
		candidate.ReviewActions[0] != "inspect_target_files" ||
		candidate.ReviewActions[1] != "run_focused_validation" ||
		candidate.ReviewActions[2] != "mark_review_status" {
		t.Fatalf("unexpected review actions: %+v", candidate.ReviewActions)
	}
}

func TestSelfImprovementCodingList_StatusFilter(t *testing.T) {
	h := selfImprovementCodingList(testSelfImprovementCodingDeps())
	params, _ := json.Marshal(map[string]any{"status": genesis.SelfCorrectionStatusApplied})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.list",
		Params: params,
	})
	payload := decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)
	if payload.Count != 1 || len(payload.Candidates) != 1 || payload.Candidates[0].ID != "sc-2" {
		t.Fatalf("expected applied candidate view, got %+v", payload)
	}
}

func TestSelfImprovementCodingList_AllStatus(t *testing.T) {
	h := selfImprovementCodingList(testSelfImprovementCodingDeps())
	params, _ := json.Marshal(map[string]any{"status": "all"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.list",
		Params: params,
	})
	payload := decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)
	if payload.Count != 3 || len(payload.Candidates) != 3 {
		t.Fatalf("expected all candidates, got %+v", payload)
	}
}

func TestSelfImprovementCodingList_RejectsUnknownStatus(t *testing.T) {
	h := selfImprovementCodingList(testSelfImprovementCodingDeps())
	params, _ := json.Marshal(map[string]any{"status": "mystery"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.list",
		Params: params,
	})
	if resp.Error == nil {
		t.Fatalf("expected invalid params for unknown status, got %+v", resp)
	}
}

func TestSelfImprovementCodingList_FunnelSummary(t *testing.T) {
	h := selfImprovementCodingList(testSelfImprovementCodingDeps())
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.self_improvement_coding.list"})
	payload := decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)

	want := SelfImprovementCodingFunnel{
		LastCaptureAt:          444,
		LastReviewAt:           555,
		Rejections7d:           2,
		PromotableRejections7d: 1,
		LastRejectionAt:        666,
		LastNudgeAt:            777,
		PendingCount:           2,
		OldestPendingAgeMs:     888,
		Dispatched7d:           3,
		WatchPassed7d:          1,
		RolledBack7d:           1,
	}
	if payload.Funnel != want {
		t.Fatalf("funnel = %+v, want %+v", payload.Funnel, want)
	}
}

func TestSelfImprovementCodingList_FunnelOptional(t *testing.T) {
	deps := testSelfImprovementCodingDeps()
	deps.Funnel = nil
	deps.LastNudgeAtMs = nil
	h := selfImprovementCodingList(deps)
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Method: "miniapp.self_improvement_coding.list"})
	payload := decodeSkillsPayload[SelfImprovementCodingListResponse](t, resp)
	if payload.Funnel != (SelfImprovementCodingFunnel{}) {
		t.Fatalf("funnel without deps = %+v, want zero", payload.Funnel)
	}
}

func TestSelfImprovementCodingMethods_NilProvider(t *testing.T) {
	if got := SelfImprovementCodingMethods(SelfImprovementCodingDeps{}); got != nil {
		t.Fatalf("SelfImprovementCodingMethods(nil) = %#v, want nil", got)
	}
}

func TestSelfImprovementCodingMethods_RecordOptional(t *testing.T) {
	deps := testSelfImprovementCodingDeps()
	deps.RecordCandidate = nil
	methods := SelfImprovementCodingMethods(deps)
	if _, ok := methods["miniapp.self_improvement_coding.record"]; ok {
		t.Fatalf("record method must be absent without a RecordCandidate dep")
	}
	deps.RecordCandidate = func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
		return rec, nil
	}
	methods = SelfImprovementCodingMethods(deps)
	if _, ok := methods["miniapp.self_improvement_coding.record"]; !ok {
		t.Fatalf("record method missing despite RecordCandidate dep")
	}
}

func TestSelfImprovementCodingMethods_DispatchOptional(t *testing.T) {
	deps := testSelfImprovementCodingDeps()
	methods := SelfImprovementCodingMethods(deps)
	if _, ok := methods["miniapp.self_improvement_coding.dispatch"]; ok {
		t.Fatal("dispatch method must be absent without a RecordDispatch dep")
	}
	deps.RecordDispatch = func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
		return rec, nil
	}
	methods = SelfImprovementCodingMethods(deps)
	if _, ok := methods["miniapp.self_improvement_coding.dispatch"]; !ok {
		t.Fatal("dispatch method missing despite RecordDispatch dep")
	}
}

func TestSelfImprovementCodingDispatch_RecordsProvenance(t *testing.T) {
	var got genesis.SelfCorrectionCandidateRecord
	deps := testSelfImprovementCodingDeps()
	deps.RecordDispatch = func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
		got = rec
		return rec, nil
	}
	h := selfImprovementCodingDispatch(deps)
	params, _ := json.Marshal(map[string]any{
		"id": "sc-1", "dispatchPhase": "merged", "attemptId": "attempt-1",
		"branch": "dispatch/sc-1", "prNumber": 42, "prUrl": "https://example.test/pr/42",
		"commitSha": "merge-sha", "outcomeNote": "checks green",
	})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID: "1", Method: "miniapp.self_improvement_coding.dispatch", Params: params,
	})
	if resp.Error != nil {
		t.Fatalf("dispatch response error: %+v", resp.Error)
	}
	if got.ID != "sc-1" || got.DispatchPhase != "merged" || got.AttemptID != "attempt-1" ||
		got.PRNumber != 42 || got.CommitSHA != "merge-sha" {
		t.Fatalf("dispatch provenance not passed through: %+v", got)
	}
}

func TestSelfImprovementCodingDispatch_RequiresAttempt(t *testing.T) {
	h := selfImprovementCodingDispatch(SelfImprovementCodingDeps{
		RecordDispatch: func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
			return rec, nil
		},
	})
	params, _ := json.Marshal(map[string]any{"id": "sc-1", "dispatchPhase": "started"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{ID: "1", Params: params})
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected missing attemptId, got %+v", resp.Error)
	}
}

func TestSelfImprovementCodingRecord_FilesProposeOnly(t *testing.T) {
	var got genesis.SelfCorrectionCandidateRecord
	deps := testSelfImprovementCodingDeps()
	deps.RecordCandidate = func(rec genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
		got = rec
		rec.ID = "sc-new"
		rec.Status = genesis.SelfCorrectionStatusProposed
		return rec, nil
	}
	h := selfImprovementCodingRecord(deps)
	params, _ := json.Marshal(map[string]any{
		"scope":          "code",
		"skillName":      "codebase-health",
		"title":          "structural finding: volatile-hub @ domain/wiki",
		"candidate":      "Many dependents consume a contract that changes frequently.",
		"evidence":       "volatile-hub:46a381ef4981 [change-locality/high] index 5.13",
		"targetFiles":    []string{"gateway-go/internal/domain/wiki"},
		"proposedChange": "Stabilize and narrow the public contract.",
		"source":         "health-finding:volatile-hub:46a381ef4981",
	})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.record",
		Params: params,
	})
	payload := decodeSkillsPayload[SelfImprovementCodingRecordResponse](t, resp)
	if !payload.OK || payload.Candidate.ID != "sc-new" {
		t.Fatalf("record response = %+v, want ok with recorded id", payload)
	}
	if payload.Candidate.Status != genesis.SelfCorrectionStatusProposed {
		t.Fatalf("recorded status = %q, want proposed", payload.Candidate.Status)
	}
	// The handler must pass provenance through untouched and never smuggle a
	// status past the tracker's propose-only default.
	if got.Source != "health-finding:volatile-hub:46a381ef4981" || got.Status != "" {
		t.Fatalf("tracker record = %+v, want untouched source and empty status", got)
	}
	if len(got.TargetFiles) != 1 || got.TargetFiles[0] != "gateway-go/internal/domain/wiki" {
		t.Fatalf("target files = %+v", got.TargetFiles)
	}
}

func TestSelfImprovementCodingRecord_RequiresSource(t *testing.T) {
	h := selfImprovementCodingRecord(testSelfImprovementCodingDeps())
	params, _ := json.Marshal(map[string]any{"title": "no provenance"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.record",
		Params: params,
	})
	if resp.Error == nil || resp.Error.Code != protocol.ErrMissingParam {
		t.Fatalf("expected MISSING_PARAM for empty source, got %+v", resp.Error)
	}
}

func TestSelfImprovementCodingRecord_RequiresContent(t *testing.T) {
	h := selfImprovementCodingRecord(testSelfImprovementCodingDeps())
	params, _ := json.Marshal(map[string]any{"source": "health-finding:x"})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.record",
		Params: params,
	})
	if resp.Error == nil || resp.Error.Code != protocol.ErrInvalidRequest {
		t.Fatalf("expected INVALID_REQUEST for empty content, got %+v", resp.Error)
	}
}

func TestSelfImprovementCodingRecord_TrackerRejection(t *testing.T) {
	deps := testSelfImprovementCodingDeps()
	deps.RecordCandidate = func(genesis.SelfCorrectionCandidateRecord) (genesis.SelfCorrectionCandidateRecord, error) {
		return genesis.SelfCorrectionCandidateRecord{}, errors.New("self-correction targets a forbidden surface: surfaces.go")
	}
	h := selfImprovementCodingRecord(deps)
	params, _ := json.Marshal(map[string]any{
		"title":  "forbidden",
		"source": "health-finding:x",
	})
	resp := h(authedSkillsCtx(), &protocol.RequestFrame{
		ID:     "1",
		Method: "miniapp.self_improvement_coding.record",
		Params: params,
	})
	if resp.Error == nil || resp.Error.Code != protocol.ErrValidationFailed {
		t.Fatalf("expected VALIDATION_FAILED passthrough, got %+v", resp.Error)
	}
	if !strings.Contains(resp.Error.Message, "forbidden surface") {
		t.Fatalf("rejection cause must reach the caller, got %q", resp.Error.Message)
	}
}

func countSelfImprovementCodingStatus(counts []SelfImprovementCodingStatusCount, status string) int {
	for _, count := range counts {
		if count.Status == status {
			return count.Count
		}
	}
	return -1
}
