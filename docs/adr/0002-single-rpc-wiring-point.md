# 0002. RPC 배선 단일 지점 (method_registry)

**Status:** accepted (retrospective)

## Context

게이트웨이는 200+ `miniapp.*` RPC 메서드를 registry 기반 디스패처로 노출한다.
핸들러를 여기저기서 배선하면 동적 디스패치(문자열 메서드명 → 핸들러)를 추적하기
어렵고, 새 메서드 추가 시 누락이 생긴다.

## Decision

RPC 핸들러의 `Deps` 배선은 `gateway-go/internal/runtime/server/`의
`method_registry.go`와 `method_registry_late.go` **단 두 파일이 유일한 배선 지점**이다.
`requiredMethods` 스냅샷 목록(`method_registry_test.go`)이 메서드 등록 집합을 고정해
누락/중복을 컴파일·테스트 시점에 잡는다.

## Alternatives rejected

- **도메인 패키지별 분산 배선** — 배선 위치가 흩어져 등록 누락·중복 감지가 안 됨.
- **런타임 문자열 매핑의 자유로운 확장 지점** — 스냅샷 테스트가 없으면 조용한 drift 발생.

## Consequences

- 새 RPC 추가는 핸들러 `Deps` 정의 → (새 도메인만) `GatewayHub` 필드 + `Validate()` →
  `method_registry.go`(또는 `_late.go`) 배선 → `requiredMethods` 스냅샷 갱신의
  4단계 절차를 따른다 (gateway-go/CLAUDE.md의 hub-wiring 규약).
- 배선 지점이 2파일이라 grep/CodeGraph로 전체 등록 표면을 한 번에 볼 수 있다.
