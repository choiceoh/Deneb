# enginecontrol 변경 지도

로컬 엔진의 **프로덕션이 서빙할 모델**을 플릿에 묻고 고르며(`Controller`), 프로덕션이
실제로 다른 모델을 서빙하면 그 엔진을 쓰던 역할을 옮긴다(`PlanFollow` + `FollowTask`).
결정은 플릿이 한다: stkernel `st_production.py`(헤드의 선택 파일)와 프로덕션 슈퍼바이저. <!-- docref:ignore -->
이 패키지는 그 판단을 복제하지 않는다. 운영 문서: `docs/operations/engine-serving-model.md`.

## 계약

- `Controller`(`control.go`)는 헤드에서 `python3 <script> show|select`를 ssh로 실행한다.
  `show`는 10초 캐시, `Select`는 모르는 프로필이면 `ErrUnknownProfile`, 이미 고른
  프로필이면 쓰지 않는다. `FromEnv`는 `DENEB_ENGINE_CONTROL_SSH=off`이거나 엔진이 없으면
  nil(기능 없음). ★`select`는 **실제 프로덕션을 전환한다** — 테스트·라이브 검증은
  `DENEB_ENGINE_CONTROL_SCRIPT`로 선택·상태 파일을 /tmp로 돌리는 래퍼를 가리킬 것.
- `Supervised`는 슈퍼바이저가 상태 파일에 phase를 쓴 적이 있을 때만 참이다. 선택을 읽지
  않는 슈퍼바이저 앞에서 고르면 아무도 가져가지 않는다.
- `PlanFollow`(`follow.go`)는 순수 함수다: 엔진 엔트리(라우터 설정에서 host:port가 같은
  엔트리) 위의 역할 중 업스트림이 서빙 모델과 다른 것을, 서빙 모델의 **같은 thinkingMode**
  엔트리로 옮긴다. 비전은 대상 엔트리가 `vision: true`일 때만. 대상이 없으면 `Skip`(이유는
  사용자에게 그대로 보인다 — 한국어, 조사는 한국어 단어에 붙일 것).
- `FollowTask`(`follow_task.go`, 30초)는 엔진 `GET /`의 fleet owner가 `production/`일
  때만 따른다. 캠페인 창(`session/`·`queue/`)이 다른 모델을 띄워도 역할은 움직이지 않는다.
  다운인 엔진에는 묻지 않는다. 같은 `Skip`의 경고는 사라졌다 다시 생길 때만 한 번 더.
- `FollowLog`는 마지막 **이동**을 남긴다: 이동 없는 패스는 이전 이동을 덮지 않고,
  보류 목록만 매 패스 갱신한다. 게이트웨이 재시작 시 비어 있다(인메모리).

## 배선

- RPC: `handlerminiapp/engine_serving.go`(`miniapp.engine.serving`·`select`),
  배선은 `server/method_registry.go`의 `EngineDeps.Control`/`Follow`.
- 태스크: `server/engine_follow_task.go`(`DENEB_ENGINE_ROUTING_FOLLOW=off`로 끔),
  적용은 `modelpicker/engine_follow.go`(`setMiniappModel` — 피커 허용 목록에 없는 id는
  거절되어 `Skip`이 된다).

## 집중 검증

`cd gateway-go && go test -count=1 ./internal/ai/enginecontrol ./internal/runtime/modelpicker ./internal/runtime/rpc/handler/handlerminiapp ./internal/runtime/server`
