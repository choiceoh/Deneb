# Chat Tool 구현 변경 지도

이 디렉터리는 agent가 호출하는 tool의 실행 구현을 소유한다. 도구의 공유
함수형 계약은 `../toolport`, 등록과 schema 연결은 `../toolwire`가 소유한다.
구현 파일이 자신을 등록하거나 chat turn 오케스트레이션을 import하지 않는다.

## 진입점과 책임

- `wikitool/wiki.go`의 `ToolWiki`, `wikitool/contacts.go`의 `ToolContacts`,
  `schedule/cron_tool.go`의 `ToolCron`, top-level `message.go`의 `ToolMessage`처럼
  `Tool*` constructor가 `toolport.ToolFunc`를 반환하는 것이 기본 계약이다.
- `filesystem/fs.go`의 `ToolWrite`, `ToolEdit`와
  `filesystem/read.go`의 `ToolRead`, `filesystem/fs_search.go`의
  `ToolGrep`가 workspace 파일 경계를 소유한다.
- `runtimeops/exec.go`의 `ToolExec`, `ToolProcess`가 실행 표면을 소유한다.
  `sessionops/`는 `sessions`/`sessions_spawn`/`subagents`, `fetchops/`는
  `fetch_tools`, `phoneops/`는 `phone_read`/`phone_write`, `gatewayops/`는
  `gateway`/`heartbeat_update`를 소유한다.
- `artifact/`는 chart/diagram/file/media 결과물, `document/`는 문서 추출,
  `schedule/`은 calendar/todo, `mailarchive/`는 archive 조회 구현이다.
- `groupwareops/`는 Amaranth10 전자결재·게시판·ERP 원장 조회와
  people wiki/org enrichment를 소유한다.
- `skilltool/`과 `lifecycletool/`이 skill 조회·수명주기 구현을 소유하며
  top-level alias 파일은 기존 등록 계약을 유지하는 얇은 호환 표면이다.
- `routine/`의 `ToolMorningLetter`, `ToolEveningLetter`는 정기 산출물
  조립을 소유한다.

## 의존 방향과 불변조건

- 의존 방향은 `toolwire → tools → toolport/domain/platform`이다. tools에서
  `pipeline/chat` root, prompt, tool registry를 import하지 않는다.
- 새 도구는 구현만으로 끝나지 않는다. `../toolwire/core/register.go`에 constructor를
  배선하고 `../toolwire/schema/tool_schemas.json`을 수정한 뒤 generator를 사용한다.
  `tool_schemas_gen.go`는 직접 편집하지 않는다.
- 모든 구현은 context 취소와 전달받은 dependency를 존중한다. package
  singleton이나 server 전역을 조회해 `Deps` 경계를 우회하지 않는다.
- filesystem/exec 계열의 path guard, destructive-command 검사와 write 전
  checkpoint를 생략하지 않는다. 편의를 위해 별도 우회 constructor를 만들지
  않는다.
- mutation 도구를 추가하면 `toolport.IsMutationTool`과 run-cache 무효화
  의미를 함께 검토한다. 변경 후 stale cached read가 남으면 안 된다.
- tool 결과의 사용자 표시용 정제와 원본 실행 결과를 혼합하지 않는다.
  display 정제는 `toolport`, turn 후처리는 chat pipeline이 소유한다.

## 집중 검증

top-level 구현의 빠른 검증과 모든 하위 tool package의 전체 검증을 모두
제공한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools`

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/tools/...`
