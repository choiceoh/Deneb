# Deneb 코드베이스 수준 평가

> 평가일: 2026-03-25 | 평가 범위: 아키텍처, 코드 품질, 테스트, 보안, CI/CD, 유지보수성

Deneb는 단일 사용자(DGX Spark)를 위한 다채널 AI 게이트웨이로, TypeScript + Rust + Go 폴리글랏 아키텍처를 채택한 성숙한 프로덕션 코드베이스다.

---

## 종합 점수

| 영역 | 점수 | 비고 |
|------|------|------|
| 아키텍처 | 9/10 | 계층 분리, 플러그인 경계, 다국어 IPC 우수 |
| 코드 품질 | 9/10 | strict TS, 린트 하드게이트, 기술부채 최소 |
| 테스트 | 8.5/10 | 1,629개 테스트, 다계층, 성능 프로파일링 |
| 보안 | 9.5/10 | Rust FFI 보안, 입력 검증, 경로 샌드박싱 |
| CI/CD | 9/10 | 스코프 감지, 6-shard, 경계 가드 |
| 유지보수성 | 8.5/10 | AI 바이브 코딩 최적화, 자동화 스크립트 |
| **종합** | **9/10** | **프로덕션 등급, 높은 완성도의 폴리글랏 코드베이스** |

---

## 1. 아키텍처 (9/10)

### 강점
- **명확한 계층 분리**: CLI → Gateway → Channels → Plugins 4계층 구조가 일관되게 유지됨
- **플러그인 SDK 경계 강제**: 160+ subpath export, 린트 규칙으로 extension이 내부 코드에 접근 불가
- **다국어 IPC 설계**: Protobuf를 cross-language source of truth로 사용, Go↔Rust CGo FFI, Go↔Node.js Unix socket
- **의존성 주입**: `createDefaultDeps()` 팩토리 패턴이 전체 코드에 일관 적용, 테스트 용이
- **상태 머신**: 세션 생명주기(IDLE→RUNNING→DONE/FAILED/KILLED/TIMEOUT) 명시적 관리

### 규모

| 영역 | LoC | 파일 수 |
|------|-----|---------|
| TypeScript (`src/`) | ~305,000 | ~2,477 |
| Go (`gateway-go/`) | ~47,800 | 31개 내부 모듈 |
| Rust (`core-rs/`) | ~20,400 | workspace 3개 |
| Extensions | ~23,300 | 111개 소스 |
| 테스트 | - | 1,629개 |

### 개선 여지
- `src/gateway/server.impl.ts` (976 LoC), `src/plugins/discovery.ts` (846 LoC) 등 일부 파일이 단일 책임 원칙 경계에 있음
- `src/infra/state-migrations.ts` (967 LoC)는 버전별 플러그인 패턴으로 분리하면 유지보수 향상 가능

---

## 2. 코드 품질 (9/10)

### 강점
- **TypeScript strict mode 전면 적용**: `strict: true`, `any` 금지, ESM 전용
- **Oxlint + Oxfmt**: `correctness`, `perf`, `suspicious` 카테고리 모두 `error` 수준
- **Pre-commit 하드 게이트**: `pnpm check` 통과 필수, main push 전 `pnpm test` + `pnpm build` 필수
- **TODO/FIXME 최소**: 전체 `src/`에서 3개만 발견 -- 기술 부채 극소
- **파일 크기 규율**: 대부분 200~700 LoC, 가이드라인 ~500 LoC

### Rust 코드 품질
- FFI 패닉 복구 래퍼 (`ffi_catch`) -- Rust 패닉이 Go 프로세스를 abort하지 않음
- 모든 unsafe 블록에 safety contract 문서화
- 입력 크기 제한 (FFI_MAX_INPUT_LEN = 16MB) -- DoS 방지
- `thiserror` 크레이트 활용, 제네릭 패닉 최소

### Go 코드 품질
- `safeGo` 래퍼로 goroutine 패닉 복구
- 함수형 Options 패턴 (WithLogger, WithVersion, WithConfig)
- `sync.RWMutex`, context 기반 취소, 구조화된 에러 반환

---

## 3. 테스트 (8.5/10)

### 강점
- **테스트-소스 비율**: core 63% (1,571/2,477), 별도 e2e/live/Docker 계층
- **커버리지 임계값**: Lines 70%, Functions 70%, Branches 55%, Statements 70% (V8)
- **스마트 시퀀싱**: `DurationSequencer` -- 가장 느린 테스트 먼저 실행
- **환경 격리**: `pool=forks`, `unstubEnvs: true`, `unstubGlobals: true`
- **다국어 테스트**: Rust (`cargo test`), Go (`go test ./...` + race detector + fuzz), TypeScript (Vitest)
- **성능 프로파일링**: `test:perf:budget`, `test:startup:memory`, `test:extensions:memory`

### 테스트 계층

| 계층 | 설명 |
|------|------|
| Unit | 1,571개 `.test.ts` + Rust inline + Go `_test.go` |
| E2E | 41개 전용 파일 |
| Live | 실 API 키 필요 (`CLAWDBOT_LIVE_TEST=1`) |
| Docker | 온보딩, 게이트웨이, 플러그인, 모델 |
| Contract | 채널 + 플러그인 계약 검증 |
| Fuzz | Go 프레임 파싱 퍼지 테스트 |

### 개선 여지
- Extension 테스트 비율 (~25%)이 core 대비 낮음 (e2e로 보완되나 단위 테스트 보강 가능)
- Gateway 통합 표면은 커버리지에서 제외 -- 의도적이나 리스크 존재

---

## 4. 보안 (9.5/10)

### 강점
- **SSRF 보호**: Rust FFI `is_safe_url` 함수
- **상수 시간 비교**: `constant_time_eq` (비밀 비교)
- **HTML 새니타이저**: `sanitize_html` (XSS 방지)
- **세션 키 검증**: ASCII fast-path + grapheme-aware Unicode path
- **입력 크기 제한**: FFI 16MB, napi-rs JSON 16MiB / String 64MiB / Compare 256MiB
- **경로 경계 검증**: `boundary-path.ts` (861 LoC) -- 샌드박싱
- **제어 문자 새니타이징**: SIMD `memchr` 활용 패턴 매칭
- **WebSocket 제한**: 클라이언트 256개, RPC body 1MB
- **보안 감사**: `src/security/audit-channel.ts` (825 LoC)

---

## 5. CI/CD (9/10)

### 강점
- **스코프 감지**: docs-only 변경 시 무거운 작업 스킵
- **6-shard 병렬 실행**: Node + Bun 호환성 레인
- **경계 가드**: 13개 린트 규칙이 별도 CI 작업으로 실행
- **다국어 체크**: Rust test + Go build/test + TS check + proto check
- **Strict smoke build**: `pnpm build:strict-smoke`
- **데드코드 분석**: knip + ts-prune + ts-unused (CI 아티팩트)

---

## 6. 유지보수성 (8.5/10)

### 강점
- **AI 바이브 코딩 최적화**: 충분한 컨텍스트/주석, 작은 함수 단위 분리
- **CLAUDE.md**: 370줄+ 상세 개발 가이드 (모든 AI 에이전트가 참고)
- **스크립트 자동화**: `dev-patch-impact.ts`, `dev-affected.ts`, `dev-commit-gate.ts`, `dev-create-pr.ts`
- **Compat 레이어 최소**: 단일 `plugin-sdk/compat.ts`만 유지
- **버전 관리**: 5곳 동기화 (package.json, Android, iOS, macOS, docs)

### 개선 여지
- `src/infra/` 하위 일부 모듈 (archive 18,915 LoC, bonjour 11,970 LoC)이 대형 -- 분리 검토 가능
- 단일 사용자 최적화로 인해 다중 사용자 확장 시 대규모 리팩토링 필요 (의도적 설계 결정)

---

## 핵심 강점

1. **Rust FFI 안전성 설계가 업계 최고 수준** -- 패닉 복구, 크기 제한, safety contract 문서화
2. **플러그인 SDK 경계 강제가 13개 린트 규칙 + CI로 완벽 보호**
3. **기술 부채가 극소** (TODO 3개) -- 지속적 정리가 이루어지고 있음
4. **AI 에이전트 개발에 최적화된 문서와 자동화**

## 개선 권장사항

1. 대형 인프라 모듈 (`archive`, `bonjour`) 분리 검토
2. Extension 단위 테스트 비율 25% -> 50% 이상으로 강화
3. `server.impl.ts`, `discovery.ts` 등 경계 파일 리팩토링
