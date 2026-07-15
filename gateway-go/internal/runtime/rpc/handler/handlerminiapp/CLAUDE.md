# Miniapp RPC Handler 변경 지도

이 패키지는 native/desktop client의 `miniapp.*` RPC 요청을 typed dependency와
wire 응답으로 번역한다. business state와 서비스 수명주기는 domain/platform이,
handler 배선은 runtime/server method registry가 소유한다.

## 진입점과 책임

- `miniapp.go`의 `Deps`와 `Methods`가 ping, whoami,
  client.hello의 최소 공통 표면이다.
- 각 기능은 파일별 `*Deps` + `*Methods` 쌍으로 노출된다.
  `dashboard/dashboard.go`의 `DashboardMethods`, `project.go`의
  `ProjectMethods`, `skillsrpc/skills.go`의 `SkillsMethods`,
  `sessions/sessions.go`의 `SessionsMethods`가 대표 진입점이다.
- `files/`의 `FilesBrowseMethods`, `knowledge/`의
  `SearchMethods`·`NotebookMethods`·`PeopleMethods`, `schedule/`의
  `CalendarMethods`·`TodoMethods`가 큰 client 영역을 분리한다.
- `mail_wire.go`, `models.go`, `project.go`, `dashboard/dashboard.go`의
  `//deneb:wire` struct가 Kotlin과 TypeScript client DTO의 Go source of
  truth다.
- 실제 등록과 dependency literal은
  `runtime/server/method_registry.go`의 early/late 단계가 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 `server method registry → handlerminiapp → domain/platform`다.
  handler는 `server`나 `rpcutil.GatewayHub`를 import하지 않고 좁은
  `Deps`/interface만 받는다.
- 모든 `miniapp.*` 호출은 HTTP bridge의 client-token 검증 뒤에 도달한다.
  identity가 필요한 handler는 `minibind.Identity` 부재를 unauthorized로
  처리하며 인증을 payload 값으로 대체하지 않는다.
- RPC method 이름은 `*Methods` map과 method registry에서만 연결한다.
  init 함수나 별도 global registry로 중복 등록하지 않는다.
- `//deneb:wire` Go struct를 바꾸면 generated Kotlin과 TypeScript를 모두
  재생성·검증한다. 생성 파일을 직접 수정하거나 한 client만 갱신하지 않는다.
- handler는 parse, 권한 확인, DTO 변환까지만 소유한다. 검색·상태 전이·저장
  규칙을 handler에 복제하지 않고 주입된 서비스 계약을 호출한다.
- 새 method는 성공만이 아니라 malformed request, dependency 부재, 권한
  실패를 같은 package 테스트에서 고정한다.

## Local change scope

handler는 번역 계층이다. 상태·검색·저장 규칙을 여기로 끌어오지 않는다.

- 함께 바꿔도 되는 이웃: `runtime/server/method_registry.go`(등록),
  `files/`·`knowledge/`·`schedule/` 하위 패키지, 주입되는 domain 서비스
  (`domain/wiki`, `domain/skills`). `Deps`/`Methods`/`ProjectMethods`/
  `SkillsMethods` 계약이 바뀌면 `miniapp_test.go`와 `models_test.go`를 먼저 본다.
- 건드리지 말 것: `rpcutil.GatewayHub` import, domain 내부 게이트 로직 복제,
  `//deneb:wire` 생성 Kotlin/TS 직접 수정, client-token 검증을 payload로 우회.
- 집중 검증:
  `cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/handlerminiapp`

## 집중 검증

root handler와 files/knowledge/schedule 하위 패키지를 모두 검증한다.

`cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/handlerminiapp`

`cd gateway-go && go test -count=1 ./internal/runtime/rpc/handler/handlerminiapp/...`

wire struct를 바꿨다면 repository root에서 `make kotlin-models-check`와
`cd andromeda && node scripts/gen-wire.mjs --check`도 실행한다.
