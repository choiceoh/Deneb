---
title: "프로덕션 서빙 모델 전환"
summary: "엔진 화면에서 프로덕션 엔진이 서빙할 모델을 고르고, 로컬 역할이 그 모델을 따라가는 경로"
read_when:
  - 엔진 화면에서 서빙 모델을 바꾸거나 전환이 멈춘 이유를 찾을 때
  - 플릿에 새 모델 프로필을 들이거나 라우터 엔트리를 추가할 때
---

# 프로덕션 서빙 모델 전환

엔진 화면 상단의 모델 이름(`glm-5.3-flash ⌄`)이 전환 버튼이다. 누르면 플릿이 서빙할 수
있는 모델 목록이 뜨고, 고른 모델로 프로덕션 엔진이 다시 뜬다. 전환에는 2~5분이 걸리고,
그동안 로컬 모델로 가는 요청은 게이트웨이가 폴백 체인으로 넘긴다. 전환이 끝나면 엔진을
쓰던 역할이 새 모델로 옮겨진다.

## 누가 무엇을 결정하나

- **선택**은 플릿이 가진다. 플릿 헤드의 `~/st-engine/launchers/st_production.py`(stkernel)가
  선택 파일(`~/glm53-logs/st-production.json`)을 쓰고 읽는다. 선택이 없거나 모르는 이름이면
  기본값 `glm53`이다.
- **전환**은 프로덕션 슈퍼바이저(`st-glm53-supervisor.sh`, systemd `st-glm53.service`)가 <!-- docref:ignore -->
  한다. 30초마다 선택을 읽고, 달라졌으면 서빙 중인 요청이 끝나길 최대 120초 기다린 뒤 이전
  모델을 내리고 새 모델을 프로덕션 리스로 띄운다. 진행 상황은
  `~/glm53-logs/st-production-state.json`에 남긴다.
- **게이트웨이**는 ssh로 헤드에 묻고(`st_production.py show`) 선택을 쓴다
  (`st_production.py select <profile> --by deneb`). 판단을 복제하지 않는다.
- **라우팅 추종**은 게이트웨이가 한다. 엔진의 `GET /`이 말하는 모델이 프로덕션 리스
  소유(`production/…`)일 때만 따라간다. 캠페인 창이 한 시간 Qwen3.8을 띄워도 역할은
  움직이지 않는다.

## 화면이 말하는 것

| 상태 줄 둘째 줄 | 뜻 |
|---|---|
| `glm-5.3-flash ⌄` | 그 모델을 서빙 중. 누르면 전환 |
| `a → b · 곧 시작` | 선택은 기록됐고 슈퍼바이저가 아직 읽지 않음 (30초 이내) |
| `a → b · 이전 모델을 내리는 중` | 요청이 끝나길 기다렸다가 이전 모델을 내리는 중 |
| `b · 띄우는 중` | 새 모델 부팅 중 |
| `b · 플릿이 비길 기다리는 중` | 다른 작업(캠페인 창)이 플릿을 쥐고 있음. 끝나면 띄움 |

그 아래 줄은 예외가 있을 때만 나온다.

- `고른 모델이 부팅에 실패해 기본 모델로 돌아왔습니다`: 고른 모델이 연속 5회 부팅에
  실패해 슈퍼바이저가 `glm53`을 다시 골랐다. 다음 선택 때까지 표시된다.
- `부팅 재시도를 멈췄습니다`: 기본 모델마저 계속 실패해 사람이 봐야 한다.
- `비전은 그대로 — …`: 새 모델의 라우터 엔트리가 이미지를 받지 않아 비전 역할은 옮기지
  않았다. ST의 Qwen3.8 경로는 비전 타워를 싣지 않는다.
- `모델 전환 불가 — …`: 헤드에 닿지 않거나, 플릿 트리에 `st_production.py`가 없거나(배포 <!-- docref:ignore -->
  전), 선택을 읽는 슈퍼바이저가 아직 상태를 알리지 않았다. 배포(`st-deploy-watch.py`)는 <!-- docref:ignore -->
  슈퍼바이저 서비스를 멈췄다가 새 트리로 다시 켜므로, 새 슈퍼바이저가 첫 점검(30초 이내)을
  마치면 사라진다.

## 라우팅 추종 규칙

`enginecontrol.PlanFollow`가 30초마다 계획하고 모델 피커(`setMiniappModel`)가 적용한다.

- 엔진 엔트리(라우터 설정에서 URL의 호스트:포트가 엔진과 같은 `wormhole` 엔트리)에 있는 역할 중
  업스트림 모델이 서빙 모델과 다른 것만 옮긴다. 대상은 서빙 모델의 엔트리 가운데
  `thinkingMode`가 같은 것이다(`glm-5.3-flash-low` → `qwen3.8-flash-next-low`).
- 비전 역할은 대상 엔트리가 `vision: true`일 때만 옮긴다.
- 대상 엔트리가 없으면 옮기지 않고 이유를 남긴다. 새 모델을 들일 때는 라우터에 생각 켠/끈
  엔트리를 둘 다 만들고, `deneb.json`의 `models.providers.wormhole.models`에도 같은 id를
  넣는다. 피커는 목록에 없는 id를 거절한다.

## 설정

| 환경변수 | 기본값 | 뜻 |
|---|---|---|
| `DENEB_ENGINE_CONTROL_SSH` | 엔진 메트릭 URL의 호스트(`<user>@<host>`) | ssh 대상. `off`면 전환 기능을 끈다 |
| `DENEB_ENGINE_CONTROL_SCRIPT` | `/home/choiceoh/st-engine/launchers/st_production.py` | 헤드의 선택 스크립트 |
| `DENEB_ENGINE_ROUTING_FOLLOW` | 켜짐 | `off`면 라우팅 추종을 끈다 |

## 구현 진입점

- 제어: `gateway-go/internal/ai/enginecontrol/control.go` (ssh, 10초 캐시)
- 추종: `gateway-go/internal/ai/enginecontrol/follow.go`, `gateway-go/internal/ai/enginecontrol/follow_task.go`,
  `gateway-go/internal/runtime/modelpicker/engine_follow.go`
- RPC: `miniapp.engine.serving`, `miniapp.engine.select`
  (`gateway-go/internal/runtime/rpc/handler/handlerminiapp/engine_serving.go`)
- 화면: `client-android/app/composeApp/src/commonMain/kotlin/ai/deneb/deneb/DenebEngineServing.kt`
- 플릿: stkernel `launchers/st_production.py`, `launchers/st-glm53-supervisor.sh`, <!-- docref:ignore -->
  `launchers/start-st-qwen38.sh` (`ST_LEASE_KIND=production`) <!-- docref:ignore -->

## 검증

```sh
ssh choiceoh@10.10.10.2 python3 ~/st-engine/launchers/st_production.py show
cd gateway-go && go test ./internal/ai/enginecontrol ./internal/runtime/modelpicker ./internal/runtime/rpc/handler/handlerminiapp
```

`show`는 읽기만 한다. 시험 삼아 `select`를 부르면 실제 프로덕션이 전환된다.
