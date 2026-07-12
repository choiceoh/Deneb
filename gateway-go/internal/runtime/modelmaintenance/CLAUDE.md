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
- 이 패키지에서 server, RPC handler, scheduler 구현을 import하지 않는다.

## 집중 검증

`cd gateway-go && go test -race -count=1 ./internal/runtime/modelmaintenance ./internal/runtime/server`
