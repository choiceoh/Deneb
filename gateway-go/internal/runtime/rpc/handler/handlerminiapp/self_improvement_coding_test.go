package handlerminiapp

import (
	"encoding/json"
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

func TestSelfImprovementCodingMethods_NilProvider(t *testing.T) {
	if got := SelfImprovementCodingMethods(SelfImprovementCodingDeps{}); got != nil {
		t.Fatalf("SelfImprovementCodingMethods(nil) = %#v, want nil", got)
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
