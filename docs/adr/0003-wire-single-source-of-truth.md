# 0003. wire 타입 단일 소스 (Go → Kotlin + TypeScript)

**Status:** accepted (retrospective)

## Context

모바일(Kotlin Multiplatform)과 데스크톱(TypeScript) 클라이언트가 게이트웨이와
같은 `miniapp.*` RPC wire 타입을 주고받는다. 언어별로 wire 타입을 손으로 병행
유지하면 계약 drift가 조용히 누적된다.

## Decision

wire 타입의 단일 소스는 Go의 `//deneb:wire` 구조체다. 여기서 Kotlin 모델은
`make kotlin-models`(`gateway-go/cmd/kotlin-models-gen`), TypeScript 모델은
`pnpm gen:wire`(`gateway-go/cmd/ts-models-gen`)로 **양쪽 생성**한다. 생성 파일은
`DO NOT EDIT` 헤더를 달고 손수정을 금지하며, CI의 wire-drift 잡이 커밋된 생성물이
소스보다 뒤처지면 실패시킨다.

## Alternatives rejected

- **언어별 수기 wire 타입 병행 유지** — drift로 인한 런타임 불일치 위험.
- **별도 IDL(예: Protobuf) 도입** — 기존 Go 구조체가 이미 소스 역할을 하므로
  이중 소스가 됨.

## Consequences

- `//deneb:wire` 변경은 `make kotlin-models` **와** `pnpm gen:wire` 둘 다 실행해야
  한다 (generated-code 규약).
- 생성 파일을 직접 고치면 안 되고, 소스/생성기를 수정 후 make 타깃으로 재생성한다.
