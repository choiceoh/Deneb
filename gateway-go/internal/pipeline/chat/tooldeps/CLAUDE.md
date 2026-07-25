# Tool Dependencies 계약 변경 지도

이 패키지는 chat tool registration과 tool implementation이 공유하는
dependency bag을 소유한다. `toolport/`의 안정 실행 계약과 분리해, 새
도메인·platform wiring이 tool context 포트를 계속 흔들지 않게 한다.

## 진입점과 책임

- `deps.go` — `CoreToolDeps`, `ProcessDeps`, `SessionDeps`, `ChronoDeps`,
  `WikiDeps`, `NotebookDeps`, `ContactsDeps`, `CalendarDeps`, `FleetDeps`
- Ports in `deps.go`: `PhoneActionFunc`/`ErrPhoneActionUnconfirmed`,
  `SpilloverStore`, `AgentLogStats`, `ContactsBook`
- `CoreToolDeps`는 composition root가 `toolreg.RegisterCoreTools`에 넘기는
  최상위 bag이다.
- `ProcessDeps`, `SessionDeps`, `ChronoDeps`, `WikiDeps`, `NotebookDeps`,
  `ContactsDeps`, `CalendarDeps`, `FleetDeps`는 각 tool family constructor가
  소비하는 좁은 bag이다.
- `PanelAnswer`, `WorkFeedRW`, `SourceReader`, `CalendarReader`,
  `LocalCalendar`는 해당 bag이 필요로 하는 capability port다.

## 의존 방향과 불변조건

- `tooldeps`는 tool 실행 타입이나 context helper를 소유하지 않는다. 그 책임은
  `toolport/`에 있다.
- 새 tool wiring 의존성은 가능한 한 해당 tool family의 작은 dep struct에
  추가한다. `CoreToolDeps`에는 composition root에서 fan-out해야 하는 필드만 둔다.
- server 객체나 registration side effect를 bag에 넣지 않는다. bag은 데이터와
  capability port만 담는다.
- nil 의존성은 기존처럼 feature-off/degrade 경계로 처리한다. constructor가 nil
  bag 또는 nil capability를 받았을 때 panic하지 않게 유지한다.

## 집중 검증

도구 wiring 계약을 바꿨다면 다음을 우선 실행한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tooldeps`
