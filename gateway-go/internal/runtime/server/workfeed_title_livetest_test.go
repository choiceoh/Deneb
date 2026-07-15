package server

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/pilot"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/proactive/cardtitle"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/aibind"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/server/svcbind"
)

// TestCardTitle_RoleCompare_Live exercises the REAL card-title prompt against the
// tiny and lightweight roles side by side, using the production config + wormhole
// creds (read by the config loader — keys never touch stdout, only the model's
// title/summary output is logged). It answers "does tiny actually do this job
// well?" before we switch cardTitleSummary from lightweight → tiny.
//
//	DENEB_TITLE_LIVETEST=1 go test -run TestCardTitle_RoleCompare_Live -v ./internal/runtime/server/
func TestCardTitleRoleCompareLiveSkipsWithoutEnvFlag(t *testing.T) {
	if os.Getenv("DENEB_TITLE_LIVETEST") == "" {
		t.Skip("set DENEB_TITLE_LIVETEST=1 to run (needs real config + network to wormhole)")
	}
	logger := slog.Default()
	reg := aibind.NewRegistryWithOptions(logger, aibind.RegistryOptions{
		MainModel:        svcbind.DefaultModel(logger),
		LocalVllmModel:   svcbind.LocalVLLMModel(logger),
		LightweightModel: svcbind.LightweightModel(logger),
		TinyModel:        svcbind.TinyModel(logger),
		CodingModel:      svcbind.CodingModel(logger),
		FallbackModel:    svcbind.FallbackModel(logger),
		VisionModel:      svcbind.VisionModel(logger),
		Providers:        svcbind.ProviderCatalog(logger),
	})
	pilot.SetModelRoleRegistry(reg)

	// Realistic proactive/mail report bodies (the head is what the titler sees).
	samples := []string{
		"📬 메일 분석 리포트\n\n한국화웨이가 자사 ESS(에너지저장장치) 신제품 라인업 협력을 제안했습니다. 발신자는 한국화웨이 김철수 부장. 우리 측 김세미 과장이 사양·가격 검토 후 회신이 필요하며, 납기 일정은 아직 미정입니다. 경쟁사 대비 가격 경쟁력이 핵심 검토 포인트입니다.",
		"📬 메일 분석 리포트\n\n현대차·기아향 Trina Solar 모듈 입찰 준비 건입니다. 김성은 이사가 공급자 확인 및 A/S 프로세스 자료를 요청했습니다. 다음 주 화요일까지 견적서와 A/S 정책서를 제출해야 하며, 전주공장 사양서 회신도 함께 진행됩니다.",
		"운영 텔레메트리 회귀가 감지되었습니다. glm-5.2 모델의 도구 호출 에러율이 지난 24시간 평균 대비 3배로 상승했고, p95 레이턴시도 172초로 악화됐습니다. thinking 런 비중이 100/127로 높아 경량 역할 재배치 검토가 필요합니다.",
	}

	for _, role := range []aibind.Role{aibind.RoleTiny, aibind.RoleLightweight} {
		t.Logf("========== role=%s  model=%s ==========", role, reg.FullModelID(role))
		for i, body := range samples {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			start := time.Now()
			title, summary, out, err := cardtitle.EvaluateCardTitleRole(ctx, role, body)
			el := time.Since(start).Round(time.Millisecond)
			cancel()
			verdict := "OK"
			if err != nil {
				verdict = "ERR"
			} else if title == "" {
				verdict = "EMPTY(→heuristic fallback)"
			}
			raw := out
			if r := []rune(raw); len(r) > 140 {
				raw = string(r[:140]) + "…"
			}
			t.Logf("[#%d %s] %s  err=%v\n    title=%q\n    summary=%q\n    raw=%q", i, el, verdict, err, title, summary, raw)
		}
	}
}
