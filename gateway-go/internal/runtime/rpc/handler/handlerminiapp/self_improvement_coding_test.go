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
			}}
			if status != "" && status != genesis.SelfCorrectionStatusProposed {
				return nil, nil
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
	candidate := payload.Candidates[0]
	if candidate.ID != "sc-1" ||
		candidate.Status != genesis.SelfCorrectionStatusProposed ||
		candidate.Scope != "code" ||
		candidate.Title != "코딩 모델 후보 표시" ||
		candidate.ProposedChange != "자가개선 코딩 화면에서 후보 큐를 렌더링" ||
		len(candidate.TargetFiles) != 1 {
		t.Fatalf("unexpected self-improvement coding candidate: %+v", candidate)
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
	if payload.Count != 0 || len(payload.Candidates) != 0 {
		t.Fatalf("expected empty applied candidate view, got %+v", payload)
	}
}

func TestSelfImprovementCodingMethods_NilProvider(t *testing.T) {
	if got := SelfImprovementCodingMethods(SelfImprovementCodingDeps{}); got != nil {
		t.Fatalf("SelfImprovementCodingMethods(nil) = %#v, want nil", got)
	}
}
