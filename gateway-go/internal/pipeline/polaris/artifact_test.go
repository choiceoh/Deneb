package polaris

import (
	"context"
	"strings"
	"testing"
)

func TestInsertSummaryDerivesStructuredArtifactAndHighSignalBursts(t *testing.T) {
	store := testStore(t)
	session := "artifact-session"
	longDecision := "배포 방식은 canary로 확정하고 2026-07-20에 검증한다. " +
		strings.Repeat("요구사항 성능 지표 장애 복구 롤백 관측 로그 승인 결과 ", 8) +
		"gateway-go/internal/domain/knowledge/router.go"
	if err := store.AppendMessage(session, textMsg("user", "검색 파이프라인을 어떻게 개선할까?", 1000)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(session, textMsg("user", longDecision, 1100)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(session, textMsg("assistant", "[도구 exec] make check 완료", 1200)); err != nil {
		t.Fatal(err)
	}

	if _, err := store.InsertSummary(SummaryNode{
		SessionKey: session, Level: 1, MsgStart: 0, MsgEnd: 2, CreatedAt: 1300,
		Content: "### 핵심 사실 (Facts)\n- canary 배포를 확정함\n\n### 도구 결과 (Tool Outcomes)\n- [exec] make check 통과",
	}); err != nil {
		t.Fatalf("InsertSummary: %v", err)
	}
	nodes, err := store.LoadSummaries(session, 1)
	if err != nil || len(nodes) != 1 {
		t.Fatalf("LoadSummaries = %d, %v", len(nodes), err)
	}
	artifact := nodes[0].Artifact
	if artifact == nil {
		t.Fatal("summary artifact was not derived")
	}
	if !strings.Contains(artifact.Question, "검색 파이프라인") || !strings.Contains(artifact.Summary, "canary") || !strings.Contains(artifact.Resolution, "make check") {
		t.Fatalf("artifact projection = %+v", artifact)
	}
	if len(artifact.Systems) != 1 || artifact.Systems[0] != "exec" {
		t.Fatalf("systems = %v, want exec", artifact.Systems)
	}
	if len(artifact.CodeRefs) != 1 || artifact.CodeRefs[0] != "gateway-go/internal/domain/knowledge/router.go" {
		t.Fatalf("code refs = %v", artifact.CodeRefs)
	}
	if len(artifact.Bursts) < 1 || artifact.Bursts[0].Role != "user" || artifact.Bursts[0].MsgStart != 0 || artifact.Bursts[0].MsgEnd != 1 {
		t.Fatalf("bursts = %+v, want the consecutive user burst", artifact.Bursts)
	}
}

type burstSemanticEmbedder struct{}

func (burstSemanticEmbedder) IsHealthy() bool { return true }
func (burstSemanticEmbedder) Embed(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if strings.Contains(strings.ToLower(text), "quasar") {
			out[i] = []float32{1, 0}
		} else {
			out[i] = []float32{0, 1}
		}
	}
	return out, nil
}

func TestSemanticRecallIndexesAdmittedBurstSeparatelyFromArtifact(t *testing.T) {
	store := testStore(t)
	session := "past"
	if err := store.AppendMessage(session, textMsg("user", "작업 진행 상황을 정리해줘", 1000)); err != nil {
		t.Fatal(err)
	}
	burst := "QUASAR 전환은 2026-07-20 완료 및 검증됨. " + strings.Repeat("성능 지연 처리량 오류 복구 배포 단계 로그 측정 결과 승인 ", 10)
	if err := store.AppendMessage(session, textMsg("assistant", burst, 1100)); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendMessage(session, textMsg("assistant", "후속 스모크 검증도 완료했다", 1200)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertSummary(SummaryNode{
		SessionKey: session, Level: 1, MsgStart: 0, MsgEnd: 2, CreatedAt: 1300,
		Content: "### 핵심 사실 (Facts)\n- 작업 결과를 정리함",
	}); err != nil {
		t.Fatal(err)
	}
	store.SetSummaryEmbedder(burstSemanticEmbedder{})
	if err := store.warmSummarySem(context.Background()); err != nil {
		t.Fatalf("warm: %v", err)
	}
	hits := store.SearchSummariesSemantic(context.Background(), "current", []string{"quasar 전환 결과"}, 5)
	if len(hits) != 1 || len(hits[0]) == 0 {
		t.Fatalf("semantic hits = %+v", hits)
	}
	if hits[0][0].Representation != "burst" || !strings.Contains(hits[0][0].Content, "QUASAR") {
		t.Fatalf("top representation = %+v, want high-signal burst", hits[0][0])
	}
}
