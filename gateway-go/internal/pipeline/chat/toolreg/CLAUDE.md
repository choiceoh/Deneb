# Tool Registry 변경 지도

이 패키지는 agent tool의 이름, 설명, JSON schema와 실행 constructor를 한
`toolport.ToolRegistrar`에 연결하는 배선 계층이다. 실행 동작은 `tools` 하위
패키지가, 공용 계약은 `toolport`가 소유하며 여기서는 등록 정책만 결정한다.

## 진입점과 소유권

- `core.go`의 `RegisterCoreTools`가 기본 tool 집합을 조립한다.
- `file_tools.go`의 `RegisterFileTools`는 workspace 경로와 추가 읽기 root만
  받아 `read/write/edit/grep`를 등록한다.
- `runtime_ops.go`의 `RegisterRuntimeOpsTools`는 이미 배선된 runtime 실행기로
  `gateway/observe/fleet/read_spillover`를, `graph_tool.go`의
  `RegisterGraphTool`은 workspace 경로로 `graphify`를 등록한다.
- `RegisterProcessTools`, `RegisterWebTools`, `RegisterSessionTools` 등 나머지
  책임별 함수가 정확한 이름과 deferred 정책을 등록한다.
- `core.go`의 `FetchToolsSchema`와 생성된 `tool_schemas_gen.go`의
  `ToolMaxOutputs`가 deferred-tool schema와 출력 한도를 노출한다.
- schema의 단일 원본은 `tool_schemas.json`이다. 생성 파일
  `tool_schemas_gen.go`를 직접 수정하지 않고 `make tool-schemas`로 갱신한다.

## 의존 방향과 불변조건

- 의존 방향은 `chat → toolreg → tools/toolport`다. toolreg는 상위
  `pipeline/chat` 패키지를 import하지 않으며, tool 구현이나 dependency bag
  정의를 이 패키지로 옮기지 않는다. 도구 의존 bag은 `tooldeps`가 소유한다.
- 등록되는 모든 `toolport.ToolDef`는 유일하고 비어 있지 않은 이름·설명,
  `type=object` schema, non-nil 함수가 반드시 함께 있어야 한다.
- optional dependency가 없으면 해당 tool만 등록하지 않는다. 비어 있는
  구현을 노출하거나 eager/deferred 정책을 우회하지 않는다.
- schema builder가 반환한 map은 호출 간 공유하면 안 된다. 한 등록의 수정이
  다음 등록이나 동시 호출에 누출되지 않아야 한다.

## 테스트와 집중 검증

- `toolreg_boundary_test.go`의
  `TestRegisterCoreToolsWithMinimalDependenciesHasValidUniqueContracts`,
  `TestRegisterCoreToolsDeferredPolicyMatchesOperationalIntent`,
  `TestWorkspaceRegistrationGroupsPreserveOrder`,
  `TestConcurrentSchemaConstructionIsIsolated`가 핵심 계약을 고정한다.
- `core_test.go`의 `TestRegisterFileToolsRegistersOnlyFileTools`와
  `TestRegisterSessionToolsContracts`가 책임별 등록 표면을 확인한다.

`cd gateway-go && go test -count=1 ./internal/pipeline/chat/toolreg`
