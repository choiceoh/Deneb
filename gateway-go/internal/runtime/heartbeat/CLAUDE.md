# Heartbeat 자율 점검 태스크 지도

30분 주기 자율 점검(`heartbeat`)과 그 레인들을 소유하는 패키지다. HEARTBEAT.md
사용자 체크, 능동 신호 감지, 자가개선 큐 소비/생성 넛지를 한 번의 에이전트
턴으로 합성해 사용자 네이티브 세션에 얹는다.

## 진입점과 책임

- `heartbeat_task.go`의 `Task`, `NewTask`, `TaskConfig`가 태스크 수명주기와 틱
  합성을 소유한다. `Run`이 활성시간·유저활동 게이트 → 결정적 레인(승격·아이들
  리뷰 클로저) → 넛지 감지 → 턴 발사 → fixture 기록 순서로 흐른다.
- `heartbeat_selfcoding.go` — 자가코딩 제안 **소비(드레인) 레인**. proposed
  후보가 있으면 필수-판정 계약 넛지를 발화하고, 핑거프린트가 움직이지 않는
  연속 무소비는 `selfCodingEscalateAfterIgnored`에서 운영자 보고로 에스컬레이션.
- `heartbeat_selfimprove_sweep.go` — 큐가 빌 때 신호(거절·재발·실패 클러스터)를
  채굴해 후보를 **생성**하는 레인. 12h 인터벌, 동일하게 연속 무시 에스컬레이션.
- `heartbeat_research.go` — 신규 데이터 축적 시 "[연구]" 아이템 자기 큐잉.
- `signals/heartbeat_signals.go` + `signals/heartbeat_deal_signals.go` — 능동 신호 수집기
  (`CalendarSignalCollector`, `TodoDeadlineCollector`, `DealDeadlineSignalCollector`,
  `CombineCollectors`).
- `heartbeat_fixtures.go` — 발화 입력/결과 fixture 수확(`FixturePath`),
  `heartbeat_shadow_replay.go`의 `RunShadowReplay`가 그 코퍼스로 지시문 후보를
  섀도 재생한다.
- `boot_task.go`의 `NewBootTask` — 기동 1회 부트 체크(별개 태스크).
- `signals/open_loop_sink.go` — 열린 루프(미완 약속) 싱크.

## 의존 방향과 불변조건

- 이 패키지는 `domain/autonomous`·`domain/monitoring`·`domain/skills/genesis`와
  chat 포트에만 의존한다. `runtime/server`를 임포트하지 않는다 — 서버는
  `TaskConfig` 클로저(예: 아이들 스킬 리뷰 레인, `server/heartbeat_idle_review.go`)로
  능력을 주입하는 방향이 유일하다.
- 레인 마커 파일(`~/.deneb/heartbeat-*.json`)은 **fail-closed가 반드시 유지**돼야
  한다: 마커 저장 실패 시 넛지를 건너뛴다 — 깨진 state dir가 30분마다 클라우드
  턴을 재발화시키면 안 된다.
- 마커·fixture는 homeDir 기준이라 dev 인스턴스와 프로덕션이 **공유**된다.
  프로덕션 상태를 쓰는 레인을 새로 추가할 때는 서버 쪽 프로덕션 state-dir
  게이트와 같은 불변조건을 검토하라.
- sweep 레인은 proposed 큐가 빌 때만 발화한다(드레인 레인과 상호배타 — 순서는
  `Run`의 감지 순서가 단일 소스). 넛지 본문 계약은 필수-행동이다: NO_REPLY는
  사용자 메시지 억제에만 쓰고, 판정/확인 도구 호출 자체를 건너뛰는 지름길로
  쓰면 안 된다.
- 하트비트 턴은 `EphemeralUser`/`EphemeralAssistant`로 사용자 대화 transcript를
  오염시키지 않는다 — 진행 상태는 HEARTBEAT.md와 마커 파일에만 남긴다.

## 변경과 검증

레인 감지 로직은 마커 상태·인터벌·에스컬레이션 스트릭 전이를 테스트로 고정한다.
이 패키지의 집중 검증 명령:

`cd gateway-go && go test ./internal/runtime/heartbeat`

넛지 문구를 바꾸면 계약 문자열 단언(`heartbeat_selfcoding_test.go`,
`heartbeat_selfimprove_sweep_test.go`)을 함께 갱신하고, 발화 조건 변경은 라이브
게이트웨이에서 저널 마커(`heartbeat: ... nudge fired`)로 확인한다.
