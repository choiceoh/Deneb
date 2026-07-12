# autonomous (자율 주기작업 스케줄러) 지도

백그라운드 주기작업의 스케줄러 도메인이다. 태스크 등록/실행/이벤트 방출과
능동 신호 감지 프리미티브를 소유한다. 개별 작업의 비즈니스 로직은 각 소유
패키지(heartbeat·genesis·backup 등)에 있고, 여기는 수명주기만 담당한다.

## 진입점과 책임

- `task.go` — `PeriodicTask` 인터페이스(Name/Interval/Run)와 `TaskStatus`.
  서버의 모든 배경 주기작업이 이 계약으로 등록된다.
- `service.go` — `Service`, `NewService`: 태스크 등록·틱 루프·`CycleEvent`
  방출(`EventListener`), `Notifier` 연동.
- `signal.go` — 능동 신호 감지: `SignalInputs`(`MailSignalInput`,
  `EventSignalInput`, `DeadlineSignalInput`), `SignalConfig`,
  `DefaultSignalConfig`. heartbeat가 틱마다 호출한다.
- `dreamer.go` — `Dreamer` 인터페이스(AuroraDream 메모리 통합의 스케줄 접점;
  구현은 memory 패키지).

## 의존 방향과 불변조건

- 의존 방향: 소유 패키지들이 autonomous를 임포트해 `PeriodicTask`를 구현한다.
  autonomous는 구현 패키지를 절대 임포트하지 않는다(순환 금지).
- `Run(ctx)`은 전달된 ctx 취소에 반드시 응답해 종료해야 한다 — 서버 셧다운이
  이 ctx로 내려온다. 취소를 무시하는 태스크는 핫스왑을 지연시킨다.
- 태스크 `Name()`은 저널 마커·상태 표면의 안정 키다 — 개명은 대시보드/로그
  grep 계약을 깬다.

## 변경과 검증

`cd gateway-go && go test ./internal/domain/autonomous`

스케줄링 의미(인터벌·재시작 동작) 변경은 `service_restart_test.go`·
`service_async_test.go`로 고정하고, 신호 임계 변경은
`signal_threshold_test.go`와 함께 움직인다.
