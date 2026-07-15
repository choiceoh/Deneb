# Skill Lifecycle 런타임 접착 지도

genesis 도메인(스킬 생성·진화·추적)을 채팅 런타임에 접착하는 패키지다.
`skill_lifecycle` 에이전트 도구의 백엔드, Propus 리뷰 포크, chat↔genesis
어댑터, 그리고 조성 루트용 **genesis core registrar**(`BuildCore` /
`GenesisBundle`)를 소유한다. 판단 로직은 `domain/skills/genesis`에 있고
여기는 배선과 턴 실행 형태만 남긴다.

## 진입점과 책임

- `core/genesis_bundle.go`의 `BuildCore`, `GenesisBundle`, type aliases —
  `runtime/server`가 `generation`/`review` leaf를 직접 임포트하지 않도록
  하는 owning-module port. 코어 서비스(catalog/service/tracker/evolver/meta)
  조립은 여기; nudger/chat 접착은 composition root에 남긴다.
- `skill_lifecycle_tool.go`의 `Backend`, `BackendConfig` — `skill_lifecycle`
  도구(액션: status·propose·self_correction·self_correction_review·replay 등)의
  단일 백엔드. 상태 조회는 `skill_lifecycle_tool_status.go`, 검증 케이스 흐름은
  `skill_lifecycle_tool_validation.go`(`BuildValidationCaseFromSession`), 리플레이는
  `skill_lifecycle_tool_replay.go`(`ReplayToolNames`)로 분할돼 있다.
- `skill_review_fork.go`의 `ReviewFork`, `NewReviewFork` — 넛저/아이들 백스톱이
  발화하는 **펜스드 Propus 리뷰 턴**. `BuildSessionContext`가 transcript에서
  리뷰 입력을 조립한다.
- `chat_adapters.go`의 `NewChatNudgerAdapter`, `NewChatUsageRecorder` — chat
  핸들러가 genesis 패키지를 임포트하지 않고 넛저·사용기록을 부르게 하는 어댑터
  (의존 역전).
- `NewValidationBackfillTask` — 실사용 transcript에서 held-out 검증 케이스를
  소급 추출하는 자율 태스크.

## 의존 방향과 불변조건

- 의존 방향: 이 패키지 → `domain/skills/genesis`(+`generation`/`review` 서브패키지),
  chat 포트. `runtime/server`는 여기의 생성자를 호출해 조립할 뿐, 역방향 임포트는
  금지다. server는 genesis leaf(`generation`/`review`)를 직접 임포트하지 않는다.
- 리뷰 포크와 트래커는 **프로덕션 공유 genesis sidecar**(homeDir/.deneb)에 리뷰
  liveness·proposal을 기록한다. 비프로덕션(state dir) 인스턴스에서 리뷰를 발화하는
  배선은 반드시 프로덕션 게이트 뒤에 둬야 한다 — dev 발 리뷰 완주가 프로덕션
  review_age를 리셋해 아이들 백스톱을 침묵시킨 실측 사고(2026-07-11)가 근거다.
- 리뷰 포크의 모델 id는 반드시 "provider/model" 전체 id여야 한다 — bare 이름은
  SendSync 재해석에서 클라이언트 결핍으로 조용히 죽는다(`init_genesis.go` 주석 참조).
- 리뷰 턴은 도구 호출로만 제안을 남긴다. 제안 기록은 실행 전에 durable해야
  한다는 genesis 계약(#3344)을 이 레이어가 깨면 안 된다.

## 변경과 검증

`cd gateway-go && go test ./internal/runtime/skilllifecycle`
`cd gateway-go && go test ./internal/runtime/server/ -count=1`

도구 액션 표면을 바꾸면 `skill_lifecycle_tool_test.go`의 계약 테스트를 갱신하고,
리뷰 포크 프롬프트 변경은 `skill_review_fork_test.go`의 자기완결성 단언과 함께
움직인다. 라이브 확인은 dev 게이트웨이 저널의 `skill nudger`/`idle skill review`
마커를 본다.
