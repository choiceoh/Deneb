# Toolreg — preset 상수 leaf

이 패키지는 session tool-preset 상수와 헬퍼만 보관하는 얇은 leaf다.
**도구 등록 배선은 `toolwire/`로 이동했다.** 과거 이 패키지가 담당하던
등록 허브 역할(`core.go`, `file_tools.go`, `runtime_ops.go`, <!-- docref:ignore -->
`tool_schemas.json`, `tool_schemas_gen.go`)은 모두 `toolwire/` 아래에 있다:

- `toolwire/core/register.go` — 책임별 `Register*Tools` (File/RuntimeOps/Graph/
  Office/Process/Web/Session/Chrono/Media/Phone). `Register`가 기본 tool 집합을
  조립한다.
- `toolwire/schema/tool_schemas.json` — schema 단일 원본.
- `toolwire/schema/tool_schemas_gen.go` — 생성물(`ToolMaxOutputs`). 직접 수정
  금지, `make tool-schemas`로 갱신.
- `toolwire/toolreg_boundary_test.go` — 핵심 등록 계약 테스트.

새 도구 추가 절차·의존 방향·불변조건은 `../toolwire/`와 상위
`pipeline/chat/CLAUDE.md`를 따른다.
