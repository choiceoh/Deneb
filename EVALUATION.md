# Deneb 코드베이스 품질 평가 보고서

> **평가 기준 빌드**: PR#122 (`c3e23b0` — config: split schema.help.ts into domain-specific modules)
> **평가 일자**: 2026-03-23
> **이전 평가**: PR#106 기준 4.1/5.0 → 현재 **4.3/5.0** (+0.2)

---

## 종합 점수: 4.3 / 5.0

| 평가 항목                 |  점수   | 이전 | 변화 | 비고                                                            |
| ------------------------- | :-----: | :--: | :--: | --------------------------------------------------------------- |
| 1. 코드 품질 & 유지보수성 | **4.5** | 4.0  | +0.5 | `as any` 97→3건, 대형 파일 분할 진행, 에러 로깅 정비            |
| 2. 아키텍처 & 설계        | **4.5** | 4.5  |  =   | 플러그인 시스템, DAG-LCM, 레이어 분리 우수. agents/ 세분화 완료 |
| 3. 테스팅 & 신뢰성        | **4.0** | 3.5  | +0.5 | 핵심 모듈 테스트 추가, 경계 검증 테스트 강화                    |
| 4. 문서화 & 개발자 경험   | **4.5** | 4.5  |  =   | CLAUDE.md 포괄적 가이드, Mintlify docs                          |
| 5. 보안                   | **4.5** | 4.0  | +0.5 | `as any` 3건으로 격감, CodeQL, 다층 감사 시스템                 |
| 6. 성능 & 확장성          | **4.0** | 4.0  |  =   | DAG 컴팩션, OpenTelemetry, 이중 전송 계층                       |
| 7. DevOps & 도구          | **4.5** | 4.5  |  =   | 11 CI 워크플로우, Oxlint+Oxfmt, 22 경계 lint                    |
| 8. 기술 부채              | **4.5** | 4.0  | +0.5 | TODO 6건, 레거시 코드 삭제, 모듈 분할 진행                      |

**가중치**: 아키텍처(20%), 테스팅(15%), 보안(15%), 코드품질(15%), 성능(10%), 문서화(10%), DevOps(10%), 기술부채(5%)

---

## 기본 지표

| 지표                         | 현재 값                | 이전 값   | 변화      |
| ---------------------------- | ---------------------- | --------- | --------- |
| 총 코드량                    | ~846K LOC (TypeScript) | ~570K LOC | +48%      |
| 소스 파일 (비테스트)         | 2,756개                | 2,675개   | +81       |
| 테스트 파일                  | 1,872개                | 1,801개   | +71       |
| 테스트-소스 비율             | 0.68                   | 0.67      | +0.01     |
| 500줄 초과 파일              | 203개 (~7.4%)          | 209개     | -6        |
| 1,500줄 초과 파일            | 4개                    | 6개       | -2        |
| 타입 안전 위반 (`as any`)    | **3건** (0.0004%)      | 97건      | **-97%**  |
| `@ts-ignore` / `@ts-nocheck` | **0건**                | —         | 완전 제거 |
| TODO/FIXME/HACK              | **6건**                | 22건      | -73%      |
| `console.log` (비테스트 src) | 49건                   | —         | 신규 측정 |
| Runtime 의존성               | 125개                  | 47개      | +78       |
| Dev 의존성                   | 77개                   | 34개      | +43       |
| CI 워크플로우                | 11개                   | 11개      | =         |
| 경계 lint 스크립트           | 22개                   | 15+       | +7        |
| Plugin-SDK subpath exports   | 70개                   | 160+      | 정밀 측정 |

---

## PR#111 이후 개선 사항 요약 (11 commits)

### 코드 품질 개선

| PR   | 내용                                           | 영향                      |
| ---- | ---------------------------------------------- | ------------------------- |
| #112 | 코드베이스 전체 빈 catch 블록에 에러 로깅 추가 | 디버깅 가시성 향상        |
| #120 | 공통 패턴을 재사용 가능한 헬퍼 함수로 추출     | 코드 중복 감소            |
| #119 | 불필요한 프로바이더별 테스트 파일 제거         | 테스트 유지보수 부담 경감 |

### 아키텍처 리팩토링

| PR   | 내용                                                     | 영향                                |
| ---- | -------------------------------------------------------- | ----------------------------------- |
| #115 | Gateway 최상위 파일을 주제별 하위 디렉토리로 정리        | Gateway 모듈 탐색성 향상            |
| #116 | Agents 모듈을 하위 디렉토리 + barrel export로 재구성     | agents/ 세분화 (이전 권고사항 해결) |
| #117 | Gateway server-methods를 도메인별 하위 디렉토리로 구조화 | RPC 메서드 그룹 분리                |
| #118 | CLI plugins-cli.ts를 서브커맨드 모듈로 분할              | CLI 모듈성 향상                     |
| #121 | 대형 모듈을 집중된 하위 모듈로 분할                      | 파일 크기 감소                      |
| #122 | schema.help.ts (1,443줄)를 도메인별 모듈로 분할          | 1,500줄+ 파일 수 감소               |

### 테스팅 강화

| PR   | 내용                                                                    | 영향                         |
| ---- | ----------------------------------------------------------------------- | ---------------------------- |
| #113 | context-engine, security, sessions, link-understanding 단위 테스트 추가 | 핵심 모듈 테스트 밀도 향상   |
| #114 | lint 경계 검증 함수 단위 테스트 추가                                    | 경계 규칙 자체의 신뢰성 확보 |

---

## 1. 코드 품질 & 유지보수성 — 4.5/5 (↑0.5)

### 강점

- **TypeScript strict mode** 전역 적용
- **`as any` 3건** — 이전 97건에서 97% 감소, 846K LOC 대비 0.0004%
- **`@ts-ignore` / `@ts-nocheck` 0건** — 완전 제거
- **Oxlint** (type-aware, `no-explicit-any` 강제) + **Oxfmt** 자동 포매팅
- `pnpm check` 게이트: format → tsgo → lint → exports → 22개 경계 lint 순차 검증
- PR#112에서 빈 catch 블록 일괄 개선 — 에러 무시 방지
- PR#120에서 공통 패턴 헬퍼 추출 — 코드 중복 감소

### 약점

- 500줄 초과 파일 **203개** (~7.4%) — 이전 209개에서 감소했으나 여전히 존재
  - `src/memory/qmd-manager.ts` (1,903줄)
  - `src/context-engine/lcm/src/compaction.ts` (1,701줄)
  - `src/agents/subagent/subagent-registry.ts` (1,512줄)
  - `src/agents/subagent/subagent-announce.ts` (1,509줄)
- `console.log` 49건 — 디버그 잔여물 가능성

### 이전 권고 대비 진행 상황

| 권고사항               | 상태       | 비고                                |
| ---------------------- | ---------- | ----------------------------------- |
| 1,500줄+ 파일 리팩토링 | **진행중** | 6→4개 감소 (schema.help.ts 분할 등) |
| `as any` 감사 및 제거  | **완료**   | 97→3건 (97% 감소)                   |

---

## 2. 아키텍처 & 설계 — 4.5/5 (=)

### 강점

- **플러그인 시스템**: 매니페스트 레지스트리 + 70개 plugin-sdk subpath export
- **Gateway 메시지 브로커**: HTTP + WebSocket 이중 전송, RPC 레지스트리, 세션 상태 머신
- **Agent 런타임**: ACP 기반 서브에이전트, 토큰/타임아웃 바운딩
- **LCM 엔진**: DAG 기반 컴팩션으로 무손실 메모리 관리
- **채널 프레임워크**: 채널 무관 추상화 + 확장 플러그인
- **DI 패턴**: `createDefaultDeps()` + 채널별 lazy loading — 경량, 테스트 가능
- **경계 lint 22개**: 플러그인/채널/확장/보안/인증 영역별 자동 위반 탐지

### 이전 권고 대비 진행 상황

| 권고사항                     | 상태     | 비고                                                  |
| ---------------------------- | -------- | ----------------------------------------------------- |
| agents/ 하위 패키지 세분화   | **완료** | PR#116: `loop/`, `session/`, `tools/`, `subagent/` 등 |
| Gateway RPC 메서드 그룹 분리 | **완료** | PR#117: domain subdirectories                         |
| Gateway 최상위 파일 정리     | **완료** | PR#115: themed subdirectories                         |

### 남은 과제

- `src/agents/` 디렉토리가 여전히 가장 큰 모듈 (~200 파일)
- 확장(extensions)이 워크스페이스가 아닌 별도 설치 — 개발 시 동기화 마찰

---

## 3. 테스팅 & 신뢰성 — 4.0/5 (↑0.5)

### 강점

- **Vitest** + 7개 설정 파일 (unit, e2e, extensions, gateway, channels, live, main)
- **70% 커버리지 임계값** (lines/functions/statements), 55% branches
- **1,872개 테스트 파일** — 이전 1,801개에서 +71개 증가
- **테스트-소스 비율 0.68** — 건전한 테스트 밀도
- PR#113: context-engine, security, sessions, link-understanding 테스트 추가
- PR#114: 경계 lint 검증 함수 자체의 단위 테스트 추가
- 테스트 성능 최적화 (vi.resetModules 캐시, 불필요 테스트 삭제)

### 이전 권고 대비 진행 상황

| 권고사항               | 상태       | 비고                                           |
| ---------------------- | ---------- | ---------------------------------------------- |
| 핵심 모듈 테스트 강화  | **진행중** | context-engine, security, sessions 테스트 추가 |
| 모듈별 커버리지 리포트 | 미착수     |                                                |

### 남은 과제

- 모듈별 커버리지 가시성 부족 — 전체 70% 임계값만으로는 사각지대 식별 어려움
- Docker 기반 테스트(`test:docker:*`)는 로컬 실행 장벽
- Telegram 확장 테스트 모음이 특히 느림 (Top 30 중 6개 차지)

---

## 4. 문서화 & 개발자 경험 — 4.5/5 (=)

### 강점

- **CLAUDE.md** — 포괄적 모듈 맵, 아키텍처 플로우, 코딩 스타일, 전체 커맨드 레퍼런스
- **Mintlify 외부 문서** — 채널/설치/프로바이더/보안/트러블슈팅
- **다국어 지원** — 중국어 번역 (`docs/zh-CN/`)
- **CONTRIBUTING.md**, **SECURITY.md** — PR/보안 가이드라인
- **AI 에이전트 워크플로우 스크립트** — `dev-patch-impact.ts`, `dev-affected.ts`, `dev-commit-gate.ts`, `dev-create-pr.ts`

### 약점

- CLAUDE.md가 방대 — 필요 정보 탐색에 시간 소요
- 일부 내부 모듈에 JSDoc/TSDoc 부족

---

## 5. 보안 — 4.5/5 (↑0.5)

### 강점

- **CodeQL** 보안 스캔 CI
- **RBAC** + 입력 허용목록 + 도구 승인 워크플로우
- **`as any` 3건** — 타입 안전 우회 거의 제거 (이전 97건)
- 전용 보안 모듈: `src/security/` (30+ 파일) — 다층 감사 시스템
- 시크릿 관리: `src/secrets/` — 런타임 스냅샷 기반 격리
- Timing-safe 비교, regex DoS 방어, 경로 순회 탐지
- SSRF 정책, 외부 콘텐츠 검증

### 남은 과제

- `console.log` 49건 — 민감 정보 노출 가능성 검토 필요
- pnpm overrides 의존성 핀닝 시 보안 패치 지연 가능

---

## 6. 성능 & 확장성 — 4.0/5 (=)

### 강점

- **DAG 기반 컴팩션** — 백그라운드 프로액티브 요약
- **OpenTelemetry** — 트레이스, 메트릭, 로그 통합 관측성
- **WebSocket + HTTP** 이중 전송 계층
- **SQLite + sqlite-vec** 벡터 DB 로컬 처리
- **성능 예산 테스트** — `test:perf:budget`, `test:startup:memory`

### 남은 과제

- 장기 실행 에이전트 메모리 프로파일링 데이터 부재
- 대규모 DAG 컴팩션 벤치마크 미구축

---

## 7. DevOps & 도구 — 4.5/5 (=)

### 강점

- **11개 GitHub Actions 워크플로우**
- **Oxlint** (type-aware) + **Oxfmt** — Rust 기반 고속 도구
- **22개 경계 lint 스크립트** — 아키텍처 침식 자동 방지
- **pnpm 모노레포** + **tsdown 번들러**
- **Pre-commit hooks** + `pnpm check` 게이트
- **AI 에이전트 DevOps** — 스마트 게이트, 영향 분석, 원커맨드 PR 생성

---

## 8. 기술 부채 — 4.5/5 (↑0.5)

### 강점

- **TODO/FIXME 6건** — 이전 22건에서 73% 감소
- **`as any` 3건** — 이전 97건에서 97% 감소
- 최신 의존성: TypeScript 5.9, Node 22+, Vitest 4, Zod 4
- PR#104: deprecated 코드 삭제 (dead 설정, 미사용 스텁, 레거시 이름)
- PR#112: 빈 catch 블록 에러 로깅 일괄 추가
- 활발한 모듈 분할 진행: agents, gateway, config, CLI

### 남은 과제

- 1,500줄+ 파일 4개 잔여
- `console.log` 49건 정리 필요
- `vega/` Python 코드 동기화 전략 미문서화

---

## 현재 1,500줄+ 대형 파일 (리팩토링 대상)

| 파일                                       | LOC   | 권고                      |
| ------------------------------------------ | ----- | ------------------------- |
| `src/memory/qmd-manager.ts`                | 1,903 | 쿼리/인덱스/캐시 분리     |
| `src/context-engine/lcm/src/compaction.ts` | 1,701 | 전략/실행/결과 분리       |
| `src/agents/subagent/subagent-registry.ts` | 1,512 | 등록/조회/생명주기 분리   |
| `src/agents/subagent/subagent-announce.ts` | 1,509 | 알림/프로토콜/직렬화 분리 |

---

## Top 3 강점

1. **성숙한 플러그인 아키텍처** — 70개 plugin-sdk subpath export, 22개 경계 lint, 매니페스트 레지스트리로 확장 포인트 철저히 관리
2. **극적인 타입 안전 개선** — `as any` 97→3건 (97% 감소), `@ts-ignore` 완전 제거, strict mode + Oxlint `no-explicit-any` 강제
3. **체계적 리팩토링 진행** — agents/gateway/config/CLI 모듈 분할, 에러 로깅 정비, 테스트 강화가 지속적으로 이루어짐

## Top 3 약점

1. **1,500줄+ 대형 파일 4개 잔여** — `qmd-manager.ts` (1,903줄) 등 핵심 모듈의 책임 과도 집중
2. **모듈별 커버리지 가시성** — 전체 70% 임계값 외에 모듈 단위 커버리지 리포트 미생성
3. **`console.log` 49건** — 프로덕션 코드 내 디버그 잔여물 또는 비구조화 로깅

---

## 개선 우선순위 (업데이트)

| 순위 | 권고사항                                                       | 노력 | 영향           | 상태   |
| :--: | -------------------------------------------------------------- | :--: | -------------- | ------ |
|  1   | 1,500줄+ 파일 4개 리팩토링 (`qmd-manager.ts`, `compaction.ts`) |  M   | 유지보수성     | 진행중 |
|  2   | 모듈별 커버리지 리포트 + 사각지대 식별                         |  M   | 테스팅         | 미착수 |
|  3   | `console.log` 49건 정리 (구조화 로거 전환 또는 제거)           |  S   | 보안, 코드품질 | 신규   |
|  4   | Telegram 확장 테스트 성능 개선                                 |  L   | 테스팅         | 미착수 |
|  5   | `vega/` 동기화 전략 문서화 또는 자동화                         |  S   | 기술부채       | 미착수 |

---

## 이전 권고 이행 현황

| 이전 권고                  | 현재 상태  | 비고                                   |
| -------------------------- | ---------- | -------------------------------------- |
| 1,500줄+ 파일 리팩토링     | **진행중** | 6→4개 (schema.help.ts 등 분할)         |
| 모듈별 커버리지 리포트     | 미착수     |                                        |
| `as any` 97건 감사 및 제거 | **완료**   | 97→3건 (97% 감소)                      |
| AGENTS.md 구조화           | **완료**   | CLAUDE.md로 통합 + 섹션별 구조화       |
| agents/ 하위 패키지 세분화 | **완료**   | PR#116: 하위 디렉토리 + barrel exports |

---

## 느린 세부 테스트 Top 30 (개별 테스트 단위)

> **측정 일자**: 2026-03-23 | **측정 방법**: `npx vitest run <file> --reporter=json` 배치 실행
> **총 수집 테스트**: 654개 (통과 393, 실패 261) | **대상 파일**: 느린 파일 Top 30 + 분할된 후속 파일

### A. 가장 느린 개별 테스트 Top 30

60s 타임아웃(hookTimeout) 1건과 30s 타임아웃(testTimeout) 29건으로, 전부 실패한 테스트입니다.

| 순위 | 소요시간 | 파일                                                           | 테스트명                                                                       |
| :--: | :------: | -------------------------------------------------------------- | ------------------------------------------------------------------------------ |
|  1   |  60.0s   | `src/gateway/server.canvas-auth.test.ts`                       | accepts capability-scoped paths over IPv6 loopback                             |
|  2   |  30.0s   | `extensions/telegram/src/probe.test.ts`                        | retry logic succeeds after retry pattern 0                                     |
|  3   |  30.0s   | `extensions/telegram/src/probe.test.ts`                        | retry logic succeeds after retry pattern 1                                     |
|  4   |  30.0s   | `extensions/telegram/src/probe.test.ts`                        | should fail after 3 unsuccessful attempts                                      |
|  5   |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | falls back to grandparent only when parent subagent session is missing         |
|  6   |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | keeps child announce internal when requester is a cron run session             |
|  7   |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | honors configured announce timeout for direct announce agent call              |
|  8   |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | does not retry gateway timeout for externally delivered completion announces   |
|  9   |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | routes child announce to parent session instead of grandparent                 |
|  10  |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | honors configured announce timeout for completion direct agent call            |
|  11  |  30.0s   | `extensions/telegram/src/webhook.test.ts`                      | starts server, registers webhook, and serves health                            |
|  12  |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | uses 90s timeout by default for direct announce agent call                     |
|  13  |  30.0s   | `src/agents/subagent/subagent-announce.timeout.test.ts`        | supports cron announceType without declaration order errors                    |
|  14  |  30.0s   | `extensions/telegram/src/probe.test.ts`                        | retry logic succeeds after retry pattern 2                                     |
|  15  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | uses guarded endpoint fetch for firecrawl requests                             |
|  16  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | wraps firecrawl error details                                                  |
|  17  |  30.0s   | `src/memory/manager.batch.test.ts`                             | tracks batch failures, resets on success, and disables after repeated failures |
|  18  |  30.0s   | `src/agents/tools/web-fetch.ssrf.test.ts`                      | blocks redirects to private hosts                                              |
|  19  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | falls back to firecrawl when readability returns no content                    |
|  20  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | uses firecrawl when direct fetch fails                                         |
|  21  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | normalizes firecrawl Authorization header values                               |
|  22  |  30.0s   | `src/agents/tools/web-tools.fetch.test.ts`                     | throws when readability is empty and firecrawl fails                           |
|  23  |  30.0s   | `extensions/telegram/src/monitor.test.ts`                      | processes a DM and sends reply                                                 |
|  24  |  30.0s   | `extensions/telegram/src/monitor.test.ts`                      | stops bot instance when polling cycle exits                                    |
|  25  |  30.0s   | `extensions/telegram/src/bot.create-telegram-bot.topics-media` | processes remaining media group photos when one photo download fails           |
|  26  |  30.0s   | `extensions/telegram/src/monitor.test.ts`                      | skips offset confirmation when persisted offset is invalid                     |
|  27  |  30.0s   | `extensions/telegram/src/fetch.test.ts`                        | uses no_proxy over NO_PROXY when deciding env-proxy bypass                     |
|  28  |  30.0s   | `extensions/telegram/src/monitor.test.ts`                      | uses agent maxConcurrent for runner concurrency                                |
|  29  |  30.0s   | `extensions/telegram/src/monitor.test.ts`                      | retries on recoverable undici fetch errors                                     |
|  30  |  30.0s   | `extensions/telegram/src/webhook.test.ts`                      | registers webhook with certificate when webhookCertPath is provided            |

#### 타임아웃 실패 파일별 분포 (전체 55건)

| 파일                                                                   | 30s 타임아웃 | 합계 |
| ---------------------------------------------------------------------- | :----------: | :--: |
| `extensions/telegram/src/monitor.test.ts`                              |      18      |  18  |
| `extensions/telegram/src/webhook.test.ts`                              |      12      |  12  |
| `src/agents/subagent/subagent-announce.timeout.test.ts`                |      8       |  8   |
| `src/agents/tools/web-tools.fetch.test.ts`                             |      6       |  6   |
| `extensions/telegram/src/probe.test.ts`                                |      4       |  4   |
| `extensions/telegram/src/bot.create-telegram-bot.topics-media.test.ts` |      2       |  2   |
| `extensions/telegram/src/fetch.test.ts`                                |      2       |  2   |
| `src/gateway/server.canvas-auth.test.ts`                               |      1       |  1   |
| `src/memory/manager.batch.test.ts`                                     |      1       |  1   |
| `src/agents/tools/web-fetch.ssrf.test.ts`                              |      1       |  1   |

### B. 통과한 느린 테스트 Top 30

실제 실행 후 통과한 테스트 중 가장 오래 걸린 30개입니다.

| 순위 | 소요시간 | 파일                                                                | 테스트명                                                                          |
| :--: | :------: | ------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
|  1   |  20.0s   | `extensions/telegram/src/bot.create-telegram-bot.topics-media`      | notifies users when media download fails for direct messages                      |
|  2   |  20.0s   | `src/agents/tools/web-fetch.ssrf.test.ts`                           | blocks when DNS resolves to private addresses                                     |
|  3   |   4.8s   | `src/plugins/loader.test.ts`                                        | supports legacy plugins importing monolithic plugin-sdk root                      |
|  4   |   0.7s   | `src/agents/pi-embedded-runner.sanitize-session-history.test.ts`    | passes simple user-only history through for Google model APIs                     |
|  5   |   0.7s   | `src/agents/tools/pdf-tool.test.ts`                                 | resolvePdfModelConfigForTool returns null without any auth                        |
|  6   |   0.7s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool uses native PDF path without eager extraction                       |
|  7   |   0.7s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool throws when agentDir missing but explicit config present            |
|  8   |   0.6s   | `src/cli/command-secret-gateway.test.ts`                            | returns config unchanged when no target SecretRefs are configured                 |
|  9   |   0.6s   | `src/auto-reply/reply/dispatch-from-config.test.ts`                 | does not route when Provider matches OriginatingChannel (even if Surface missing) |
|  10  |   0.6s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool returns null without any auth configured                            |
|  11  |   0.6s   | `src/gateway/server-plugins.test.ts`                                | shares fallback context across module reloads for existing runtimes               |
|  12  |   0.6s   | `src/agents/tools/pdf-tool.test.ts`                                 | resolvePdfModelConfigForTool prefers explicit pdfModel config                     |
|  13  |   0.5s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool rejects pages parameter for native PDF providers                    |
|  14  |   0.5s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool deduplicates pdf inputs before loading                              |
|  15  |   0.5s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool uses extraction fallback for non-native models                      |
|  16  |   0.5s   | `src/agents/tools/pdf-tool.test.ts`                                 | resolvePdfModelConfigForTool falls back to imageModel config when no pdfModel set |
|  17  |   0.5s   | `src/agents/sessions-spawn-hooks.test.ts`                           | runs subagent_spawning and emits subagent_spawned with requester metadata         |
|  18  |   0.5s   | `src/agents/tools/pdf-tool.test.ts`                                 | createPdfTool returns null without agentDir and no explicit config                |
|  19  |   0.4s   | `src/agents/deneb-tools.subagents.sessions-spawn.allowlist.test.ts` | sessions_spawn only allows same-agent by default                                  |
|  20  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | agent routing derives agentId from the session key                                |
|  21  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | path passthrough does not convert 'path' to media for send                        |
|  22  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | schema scoping scopes schema fields for 'telegram'                                |
|  23  |   0.1s   | `src/memory/embeddings.test.ts`                                     | loads the model only once when embedBatch is called concurrently                  |
|  24  |   0.1s   | `src/memory/embeddings.test.ts`                                     | shares initialization when embedQuery and embedBatch start concurrently           |
|  25  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | does not include 'Other configured channels' when only one configured             |
|  26  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | sanitizes reasoning tags in 'text' before sending                                 |
|  27  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | normalizes channel aliases before building current channel desc                   |
|  28  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | path passthrough does not convert 'filePath' to media for send                    |
|  29  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | schema scoping hides telegram poll extras when polls disabled in scoped mode      |
|  30  |   0.1s   | `src/agents/tools/message-tool.test.ts`                             | schema scoping scopes schema fields for 'slack'                                   |

### 파일 단위 소요시간 (이전 대비)

| 순위 |  현재  |  이전  |   변화    | 파일                                                               |
| :--: | :----: | :----: | :-------: | ------------------------------------------------------------------ |
|  1   | 545.1s | 545.4s |     =     | `extensions/telegram/src/monitor.test.ts`                          |
|  2   | 380.3s | 380.5s |     =     | `src/agents/tools/web-tools.fetch.test.ts`                         |
|  3   | 360.0s | 360.2s |     =     | `extensions/telegram/src/webhook.test.ts`                          |
|  4   | 240.0s | 240.5s |     =     | `src/agents/subagent/subagent-announce.timeout.test.ts`            |
|  5   | 220.3s | 220.6s |     =     | `extensions/telegram/src/bot/delivery.resolve-media-retry.test.ts` |
|  6   | 200.4s | 200.8s |     =     | `extensions/telegram/src/bot/delivery.test.ts`                     |
|  7   | 140.2s | 140.9s |     =     | `src/agents/tools/web-fetch.cf-markdown.test.ts`                   |
|  8   | 136.0s | 138.0s |     =     | `extensions/telegram/src/probe.test.ts`                            |
|  9   | 120.0s |   —    |   신규    | `extensions/telegram/src/bot.create-telegram-bot.topics-media`     |
|  10  | 80.6s  | 66.4s  |   +21%    | `src/agents/tools/browser-tool.test.ts`                            |
|  11  | 80.0s  | 80.6s  |     =     | `src/image-generation/providers/google.test.ts`                    |
|  12  | 70.1s  | 70.6s  |     =     | `src/agents/tools/web-fetch.ssrf.test.ts`                          |
|  13  | 70.0s  | 72.8s  |     =     | `src/gateway/call.test.ts`                                         |
|  14  | 70.0s  | 70.9s  |     =     | `src/memory/manager.batch.test.ts`                                 |
|  15  | 60.0s  | 61.0s  |     =     | `src/gateway/server.canvas-auth.test.ts`                           |
|  16  | 60.0s  | 62.0s  |     =     | `src/browser/server-context.remote-tab-ops.test.ts`                |
|  17  | 60.0s  | 61.2s  |     =     | `src/browser/server-context.remote-profile-tab-ops.test.ts`        |
|  18  | 60.0s  | 63.1s  |     =     | `extensions/telegram/src/fetch.test.ts`                            |
|  19  | 21.5s  | 154.0s | **-86%**  | `src/commands/status.test.ts`                                      |
|  20  | 20.0s  |   —    |   신규    | `extensions/telegram/src/bot.create-telegram-bot.routing.test.ts`  |
|  21  | 10.0s  | 146.6s | **-93%**  | `src/agents/tools/pdf-tool.test.ts`                                |
|  22  |  5.6s  | 134.9s | **-96%**  | `src/plugins/loader.test.ts`                                       |
|  23  |  2.5s  | 67.5s  | **-96%**  | `src/agents/tools/message-tool.test.ts`                            |
|  24  |  1.0s  | 150.0s | **-99%**  | `src/gateway/server-plugins.test.ts`                               |
|  25  |  1.0s  | 166.0s | **-99%**  | `src/cli/command-secret-gateway.test.ts`                           |
|  26  |  1.0s  | 92.8s  | **-99%**  | `src/auto-reply/reply/dispatch-from-config.test.ts`                |
|  27  |  1.0s  | 87.0s  | **-99%**  | `src/agents/sessions-spawn-hooks.test.ts`                          |
|  28  |  0.8s  | 290.5s | **-100%** | `src/agents/pi-embedded-runner.sanitize-session-history.test.ts`   |
|  29  |  0.5s  | 58.7s  | **-99%**  | `src/agents/deneb-tools.subagents.sessions-spawn.allowlist`        |
|  30  |  0.3s  | 143.3s | **-100%** | `src/memory/embeddings.test.ts`                                    |
|  —   |  0.0s  | 60.6s  | **-100%** | `src/memory/embeddings-voyage.test.ts`                             |
|  —   |  0.0s  | 149.8s |   분할    | `extensions/telegram/src/bot.create-telegram-bot.test.ts`          |

### 분석 요약

- **타임아웃 실패 55건** (30s testTimeout 도달) — Telegram 확장 (38건), subagent-announce (8건), web-fetch (7건), 기타 (2건)
- **통과 테스트 중 진짜 느린 것은 3건**: Telegram topics-media DM (20s), SSRF DNS 해석 (20s), plugin-sdk 레거시 로드 (4.8s)
- **나머지 통과 테스트는 1초 미만** — `vi.resetModules()` 캐시 최적화 효과 유지 확인
- **이전 대비 12개 파일이 86~100% 단축** — 캐시 최적화가 안정적으로 유지됨

### 핵심 최적화 대상

| 우선순위 | 영역                        | 대상 테스트 수 | 원인                                                  | 난이도 |
| :------: | --------------------------- | :------------: | ----------------------------------------------------- | :----: |
|    1     | Telegram monitor/webhook    |     30건+      | `vi.doMock` + `importOriginal` 체인, 비동기 mock 누락 |   L    |
|    2     | subagent-announce timeout   |      8건       | mock setup 결함으로 전체 타임아웃                     |   M    |
|    3     | web-tools.fetch firecrawl   |      6건+      | firecrawl/readability mock 미해결 Promise 대기        |   M    |
|    4     | web-fetch SSRF DNS (20s)    |      1건       | 실제 DNS 해석 대기 — mock DNS resolver 적용 필요      |   S    |
|    5     | plugin loader legacy (4.8s) |      1건       | 모놀리식 plugin-sdk 전체 import — lazy load 분할 가능 |   M    |
|    6     | Telegram bot delivery       |     21건+      | `vi.doMock` 체인 + 40+ mock 리셋 비용                 |   L    |
|    7     | Browser remote tab ops      |      10건      | 원격 탭 조작 비동기 대기 타임아웃                     |   M    |
|    8     | web-fetch cf-markdown       |      7건       | Cloudflare markdown mock 설정 결함                    |   S    |

### 이전 적용된 최적화 (유지 중)

#### 1. 불필요한 느린 테스트 11개 삭제 (~160초 절감)

- `models-config.providers.{minimax,kilocode,matrix,volcengine-byteplus,vercel-ai-gateway,cloudflare-ai-gateway}.test.ts`
- `auth-profiles/oauth.openai-codex-refresh-fallback.test.ts`
- `deneb-tools.web-runtime.test.ts`, `sessions-spawn.cron-note.test.ts`
- `models-config.providers.plugin-allowlist-compat.test.ts`
- `index.test.ts`

#### 2. sessions-spawn harness `vi.resetModules()` 캐시 (~95초 절감)

파일당 1회로 변경. 기존 `beforeEach()` 상태 리셋이 충분한 테스트 격리를 제공.

| 테스트 파일                              | 이전  | 이후 | 배속    |
| ---------------------------------------- | ----- | ---- | ------- |
| `sessions-spawn.allowlist.test.ts`       | 58.7s | 1.5s | **39x** |
| `sessions-spawn.model.test.ts`           | 41.5s | 1.5s | **28x** |
| `sessions-spawn.lifecycle.test.ts`       | 13.3s | 1.6s | **8x**  |
| `sessions-spawn-default-timeout.test.ts` | 12.4s | 1.4s | **9x**  |

#### 3. plugin loader 캐시 eviction 테스트 반복 축소 (~5초 절감)

`__testing.maxPluginRegistryCacheEntries`를 테스트 중 4로 낮춰 129회 → 5회 로드로 동일 LRU 검증.

| 이전              | 이후            |
| ----------------- | --------------- |
| 5.3s (129회 로드) | 0.1s (5회 로드) |

#### 4. `vi.resetModules()` 캐시 확대 적용 (~700초 절감)

sessions-spawn과 동일한 패턴을 추가 4개 파일에 적용. 첫 번째 테스트에서만 `vi.resetModules()` + `import()`를 실행하고 나머지 테스트는 캐시된 모듈을 재사용.

| 테스트 파일                                        | 이전   | 이후  | 배속      |
| -------------------------------------------------- | ------ | ----- | --------- |
| `dispatch-from-config.test.ts` (53 tests)          | 92.8s  | ~0.7s | **~130x** |
| `pi-embedded-runner.sanitize-session-history` (30) | 290.5s | ~0.7s | **~415x** |
| `command-secret-gateway.test.ts` (23 tests)        | 166.0s | ~0.7s | **~237x** |
| `server-plugins.test.ts` (16 tests)                | 150.0s | ~0.6s | **~250x** |

---

_이 보고서는 자동 생성되었으며, 정기적으로 업데이트됩니다._
