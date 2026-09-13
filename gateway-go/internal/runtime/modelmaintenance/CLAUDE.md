# Model maintenance 변경 지도

이 패키지는 모델 품질을 유지하는 백그라운드 작업의 생성과 활성화 조건을
소유한다. `runtime/server`는 결과 task를 스케줄러에 등록할 뿐 구체 구현과
telemetry adapter를 직접 조립하지 않는다.

## 계약

- `suite.go`의 `New(Deps)`가 model tuner → regression watch → 선택적
  compaction tuner 순서를 고정한다.
- 핵심 log/registry 입력이 없으면 suite 전체를 비활성화한다.
- compaction tuner는 환경 opt-in, summary source, lightweight client가 모두
  있을 때만 task와 `PromptTuner` 표면에 같은 인스턴스로 노출된다.
- 관측 ring adapter는 최근 error 수만 regression signal로 변환한다.
- engine speed task는 `DENEB_ENGINE_METRICS_URL`이 있을 때만 붙는다. 로컬 서빙
  엔진의 `/metrics`를 읽어 일자별 디코드·프리필 속도와 동시성을 낸다 — 게이트웨이가
  자기 호출을 재는 방식이 아니다(프로바이더가 첫 토큰까지 응답 헤더를 붙잡아
  게이트웨이 시계가 프리필 0ms를 보고한 전례). 클라우드 모델은 구조상 미포함.
  `Suite.EngineSpeed()`가 observe 표면에 그 이력을 넘긴다.
- 이 패키지에서 server, RPC handler, scheduler 구현을 import하지 않는다.

## 집중 검증

`cd gateway-go && go test -race -count=1 ./internal/runtime/modelmaintenance ./internal/runtime/server`
