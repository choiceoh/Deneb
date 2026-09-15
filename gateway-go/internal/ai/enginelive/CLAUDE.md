# enginelive 변경 지도

로컬 서빙 엔진의 readiness 를 5초마다 `/health` 로 읽어, 엔진이 요청을
거부하는 동안 그 엔진이 서빙하는 모델 집합을 `Sink`(= `modelrole.Registry`)에
알린다. 목적은 하나: **죽은 엔진에 재시도를 쓰지 않는다.** 2026-09-14 엔진
사흘 다운 때 한 턴이 클라이언트 재시도(≈70초)와 런 재생을 거쳐 2분 넘게
기다린 뒤에야 폴백했다.

## 계약

- `internal/ai/enginelive/watcher.go`의 `New`가 사설 호스트(`httputil.IsPrivateHost`)가 아닌
  엔드포인트를 버리고, 남는 게 없으면 nil 을 돌려준다. `Start` 는 nil 에 안전.
- 판정 3종 (`probe`): 500 미만 응답 = 준비(엔드포인트가 없어도 답했으면 살아
  있음) · 5xx 또는 연결 거부 = 거부(**즉시** 다운 — 엔진 자신의 대답) · 무응답
  = 침묵(**2회 연속**이어야 다운 — 플릿이 Wi-Fi 라 한 번 유실은 장애가 아님).
- 복구는 준비 **2회 연속**. 부팅 중 깜빡이는 엔진이 두 거부 사이에 턴을 끌어가지
  않게.
- 다운 동안은 매 프로브마다 모델 집합을 다시 해석해 넘긴다(장애 중 라우터 설정
  이름 변경 반영). 로그는 전이 시에만: 다운 WARN 1회, 복구 INFO 1회(`downFor`).
- 모델 해석은 서버 배선(`internal/runtime/server/server_workflow_capabilities.go`)이 주입한다:
  라우터 설정에서 같은 host:port 를 가리키는 엔트리(`configresolve.EngineModels`)
  + 엔진을 직접 가리키는 역할(`Registry.ModelsAt`).
- ST 엔진의 `/health` 는 draining(핸드오버 중 새 요청 전부 503)도 503 으로
  말한다 — 웜홀의 `/v1/models` 프로브는 그 상태를 못 본다.

## 집중 검증

`cd gateway-go && go test -race -count=1 ./internal/ai/enginelive ./internal/ai/modelrole ./internal/ai/llm ./internal/pipeline/chat`
