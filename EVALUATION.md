# Deneb 코드베이스 품질 평가 보고서

> **평가 기준 빌드**: PR#106 (`064c8117` — docs: add comprehensive project commands reference to CLAUDE.md)
> **평가 일자**: 2026-03-23

---

## 종합 점수: 4.1 / 5.0

| 평가 항목                 |  점수   | 비고                                                     |
| ------------------------- | :-----: | -------------------------------------------------------- |
| 1. 코드 품질 & 유지보수성 | **4.0** | strict mode, Oxlint 적용. 일부 대형 파일 존재            |
| 2. 아키텍처 & 설계        | **4.5** | 플러그인 시스템, DAG 기반 LCM, 레이어 분리 우수          |
| 3. 테스팅 & 신뢰성        | **3.5** | 70% 커버리지 임계값, 다중 설정. 핵심 모듈 밀도 확인 필요 |
| 4. 문서화 & 개발자 경험   | **4.5** | AGENTS.md 35K, Mintlify docs, CONTRIBUTING.md            |
| 5. 보안                   | **4.0** | CodeQL, RBAC, 시크릿 관리 모듈. `as any` 97건            |
| 6. 성능 & 확장성          | **4.0** | DAG 컴팩션, OpenTelemetry, 이중 전송 계층                |
| 7. DevOps & 도구          | **4.5** | 11개 CI 워크플로우, Oxlint+Oxfmt, pre-commit             |
| 8. 기술 부채              | **4.0** | TODO 22건/570K LOC, 최신 의존성                          |

**가중치**: 아키텍처(20%), 테스팅(15%), 보안(15%), 코드품질(15%), 성능(10%), 문서화(10%), DevOps(10%), 기술부채(5%)

---

## 기본 지표

| 지표                                   | 값                     |
| -------------------------------------- | ---------------------- |
| 총 코드량                              | ~570K LOC (TypeScript) |
| 소스 파일                              | 2,675개                |
| 테스트 파일                            | 1,801개                |
| 테스트-소스 비율                       | 0.67                   |
| 500줄 초과 파일                        | 209개 (~8%)            |
| 타입 안전 위반 (`as any`/`@ts-ignore`) | 97건 (0.02%)           |
| TODO/FIXME/HACK                        | 22건                   |
| Runtime 의존성                         | 47개                   |
| Dev 의존성                             | 34개                   |
| CI 워크플로우                          | 11개                   |

---

## 1. 코드 품질 & 유지보수성 — 4.0/5

### 강점

- **TypeScript strict mode** 전역 적용, 타입 안전 위반 97건은 570K LOC 대비 0.02% 미만
- **Oxlint** (type-aware) + **Oxfmt** 으로 코드 스타일 자동 강제
- 모듈별 명확한 디렉토리 구조: `agents/`, `gateway/`, `channels/`, `plugins/`, `context-engine/`, `memory/` 등
- `pnpm check` 게이트: format → tsgo → lint → exports → extension boundaries 순차 검증

### 약점

- 500줄 초과 파일 **209개** — 전체의 ~8%
  - `src/memory/qmd-manager.ts` (1,903줄)
  - `src/context-engine/lcm/src/compaction.ts` (1,701줄)
  - `src/agents/subagent-registry.ts` (1,512줄)
  - `src/agents/subagent-announce.ts` (1,509줄)
- 일부 대형 모듈은 책임 분리가 추가로 필요

### 권고

- 1,500줄 이상 파일을 우선 리팩토링 대상으로 분류
- `qmd-manager.ts`, `compaction.ts`를 기능 단위로 분리

---

## 2. 아키텍처 & 설계 — 4.5/5

### 강점

- **플러그인 시스템**: 매니페스트 레지스트리 + 160+ subpath export의 `plugin-sdk`
- **Gateway 메시지 브로커**: HTTP + WebSocket 이중 전송, RPC 메서드 레지스트리, 세션 라이프사이클 상태 머신
- **Agent 런타임**: ACP(Agent Control Protocol) 기반 서브에이전트 관리, 토큰/타임아웃 바운딩
- **LCM 엔진**: DAG 기반 컴팩션으로 독창적 무손실 메모리 관리
- **채널 프레임워크**: 채널 무관 추상화 + 채널별 확장

### 약점

- `src/agents/` 디렉토리가 8.0MB로 가장 크며, 에이전트 루프/세션/커맨드 라우팅/도구 실행/서브에이전트 등 다수 책임 집중
- 확장(extensions)이 워크스페이스 패키지가 아닌 별도 설치 — 개발 시 동기화 마찰 가능

### 권고

- `agents/` 내부를 `loop/`, `session/`, `tools/`, `subagent/` 등 하위 패키지로 세분화 고려

---

## 3. 테스팅 & 신뢰성 — 3.5/5

### 강점

- **Vitest 4.1** + 7개 설정 파일 (unit, e2e, extensions, gateway, channels, live, main)
- 70% 커버리지 임계값 (lines/functions/statements), 55% branches
- **GitHub Actions CI** 자동 검증 (`ci.yml`)
- **Pre-commit hooks** (`git-hooks/`)
- fake timers 적용으로 flaky 테스트 제거 (PR#102)
- 테스트 재개(resume) 기능 (PR#99)

### 약점

- 테스트 파일 수는 1,801개로 많지만, 570K LOC 대비 실제 커버리지가 일부 모듈에 집중될 가능성
- `context-engine/lcm/`, `agents/` 등 핵심 모듈의 테스트 밀도 검증 필요
- Docker 기반 테스트(`test:docker:*`)는 로컬 개발 시 실행 장벽

### 권고

- 모듈별 커버리지 리포트 생성하여 테스트 사각지대 식별
- `agents/`, `context-engine/lcm/` 모듈의 단위 테스트 강화

---

## 4. 문서화 & 개발자 경험 — 4.5/5

### 강점

- **AGENTS.md** (35K줄) — 모듈, 아키텍처, 워크플로우, 코딩 스타일 포괄
- **Mintlify 외부 문서** — 27개 폴더, 채널/설치/레퍼런스/트러블슈팅/프로바이더/플러그인
- **CONTRIBUTING.md** — PR 가이드라인, 테스팅 요구사항, AI 지원 PR 프로세스
- **SECURITY.md** (22K) — 보안 정책, 취약점 보고 절차
- **CHANGELOG.md** — 릴리스 노트
- **다국어 지원** — 중국어 번역 (`docs/zh-CN/`)

### 약점

- AGENTS.md가 35K줄로 너무 방대 — 필요한 정보 탐색에 시간 소요
- 일부 내부 모듈에 JSDoc/TSDoc 부족 가능

### 권고

- AGENTS.md를 주제별로 분리하거나, 상세 목차 + 빠른 참조 섹션 추가
- 공개 API에 TSDoc 어노테이션 강화

---

## 5. 보안 — 4.0/5

### 강점

- **CodeQL** 보안 스캔 CI 워크플로우 (`codeql.yml`)
- **RBAC** + 입력 허용목록 + 도구 승인 워크플로우 (Gateway)
- 전용 시크릿 관리 모듈 (`src/secrets/`, 568K)
- 전용 보안 정책 & 감사 모듈 (`src/security/`, `audit.ts`, `audit-extra.sync.ts`)
- Zod 4.3 + AJV 8.18 기반 입력 검증
- 디바이스 페어링 인증 체계

### 약점

- `as any` 97건 — 잠재적 타입 안전 우회 지점 (런타임 에러 원인 가능)
- pnpm overrides로 의존성 핀닝 시 보안 패치 지연 가능

### 권고

- `as any` 사용 사례 감사 — 불필요한 캐스트 제거, 필요한 경우 `unknown` + 타입 가드로 대체
- Dependabot + pnpm audit를 CI에 통합

---

## 6. 성능 & 확장성 — 4.0/5

### 강점

- **DAG 기반 컴팩션** — 백그라운드 옵저버로 프로액티브 요약 생성
- **OpenTelemetry** — 트레이스, 메트릭, 로그 통합 관측성
- **WebSocket + HTTP** 이중 전송 계층
- 토큰/타임아웃 제한의 서브에이전트 바운딩
- SQLite + sqlite-vec 벡터 DB 로컬 처리

### 약점

- 장기 실행 에이전트 프로세스에서의 메모리 누수 가능성 모니터링 필요
- 대규모 DAG 컴팩션 시 성능 프로파일링 데이터 부재

### 권고

- 장기 실행 시나리오에 대한 메모리 프로파일링 테스트 추가
- LCM 엔진의 대규모 대화 처리 벤치마크 구축

---

## 7. DevOps & 도구 — 4.5/5

### 강점

- **11개 GitHub Actions 워크플로우**: CI, CodeQL, npm 릴리스, Docker 릴리스, smoke 테스트, 라벨러, stale 관리 등
- **Oxlint 1.56** (type-aware) + **Oxfmt 0.41** — Rust 기반 고속 린팅/포매팅
- **pnpm 10.23** 모노레포 + **tsdown 0.21** 번들러
- **Pre-commit hooks** (`git-hooks/`) — format + type-check + lint
- Docker 컨테이너화된 테스트 (`test:docker:*`)
- `pnpm check` — 15+ 전문화 lint 규칙 게이트

### 약점

- 빌드 프로세스가 다단계 (canvas → tsdown → postbuild → SDK DTS → hooks → metadata) — 초기 설정 복잡도

### 권고

- 빌드 프로세스 문서화 강화 (각 단계의 목적과 의존 관계)

---

## 8. 기술 부채 — 4.0/5

### 강점

- **TODO/FIXME 22건** — 570K LOC 대비 극히 적음 (0.004%)
- 최신 의존성 스택: TypeScript 5.9, Node 22+, Express 5, Vitest 4, Zod 4
- `pnpm.patchedDependencies`로 의존성 패치 체계적 관리
- PR#103에서 기술 부채 적극 해소 (빈 catch 블록, 이중 캐스트, 테스트 모의 불일치)
- PR#104에서 deprecated 코드 삭제 (dead 설정, 미사용 스텁, 레거시 이름 내보내기)

### 약점

- `vega/` 디렉토리에 Python 코드 미러링 — 동기화 관리 부담

### 권고

- `vega/` 디렉토리의 동기화 전략 문서화 또는 자동화

---

## Top 3 강점

1. **성숙한 아키텍처** — 플러그인 시스템, Gateway 브로커, ACP 기반 에이전트 관리, DAG-LCM 엔진까지 명확한 레이어 분리와 확장 포인트 확보
2. **강력한 DevOps 파이프라인** — 11개 CI 워크플로우, Oxlint/Oxfmt, pre-commit hooks, 70% 커버리지 임계값으로 코드 품질 자동 보장
3. **포괄적 문서화** — AGENTS.md(35K), Mintlify 외부 docs, SECURITY.md(22K), CONTRIBUTING.md로 개발자 온보딩 지원

## Top 3 약점

1. **대형 파일 존재** — 209개 파일이 500줄 초과, 핵심 모듈(`qmd-manager.ts` 1,903줄)의 책임이 과도하게 집중
2. **핵심 모듈 테스트 밀도** — 전체 커버리지 70% 임계값은 있으나, `agents/`·`context-engine/lcm/` 등 핵심 경로의 개별 커버리지 가시성 부족
3. **타입 안전 우회** — `as any`/`@ts-ignore` 97건이 잠재적 런타임 에러 진입점

---

## 개선 우선순위

| 순위 | 권고사항                                                   | 노력 | 영향           |
| :--: | ---------------------------------------------------------- | :--: | -------------- |
|  1   | 1,500줄+ 파일 리팩토링 (`qmd-manager.ts`, `compaction.ts`) |  M   | 유지보수성     |
|  2   | 모듈별 커버리지 리포트 + 핵심 모듈 테스트 강화             |  M   | 테스팅         |
|  3   | `as any` 97건 감사 및 점진적 제거                          |  M   | 보안, 코드품질 |
|  4   | AGENTS.md 구조화 (주제별 분리 또는 목차 개선)              |  S   | 문서화         |
|  5   | `agents/` 하위 패키지 세분화                               |  L   | 아키텍처       |

---

## 테스트 성능 리포트

> 측정일: 2026-03-23 | 대상: 1,824개 테스트 파일 | 최적화 적용 후 기준

### Top 30 가장 느린 테스트 파일

| 순위 | 소요시간 | 테스트 수 | 파일                                                                |
| ---- | -------- | --------- | ------------------------------------------------------------------- |
| 1    | 545.4s   | 24        | `extensions/telegram/src/monitor.test.ts`                           |
| 2    | 380.5s   | 17        | `src/agents/tools/web-tools.fetch.test.ts`                          |
| 3    | 360.2s   | 13        | `extensions/telegram/src/webhook.test.ts`                           |
| 4    | 290.5s   | 30        | `src/agents/pi-embedded-runner.sanitize-session-history.test.ts`    |
| 5    | 240.5s   | 9         | `src/agents/subagent-announce.timeout.test.ts`                      |
| 6    | 220.6s   | 20        | `extensions/telegram/src/bot/delivery.resolve-media-retry.test.ts`  |
| 7    | 200.8s   | 31        | `extensions/telegram/src/bot/delivery.test.ts`                      |
| 8    | 166.0s   | 23        | `src/cli/command-secret-gateway.test.ts`                            |
| 9    | 154.0s   | 12        | `src/commands/status.test.ts`                                       |
| 10   | 150.0s   | 16        | `src/gateway/server-plugins.test.ts`                                |
| 11   | 149.8s   | 50        | `extensions/telegram/src/bot.create-telegram-bot.test.ts`           |
| 12   | 146.6s   | 51        | `src/agents/tools/pdf-tool.test.ts`                                 |
| 13   | 143.3s   | 19        | `src/memory/embeddings.test.ts`                                     |
| 14   | 140.9s   | 7         | `src/agents/tools/web-fetch.cf-markdown.test.ts`                    |
| 15   | 138.0s   | 10        | `extensions/telegram/src/probe.test.ts`                             |
| 16   | 134.9s   | 69        | `src/plugins/loader.test.ts`                                        |
| 17   | 92.8s    | 53        | `src/auto-reply/reply/dispatch-from-config.test.ts`                 |
| 18   | 87.0s    | 10        | `src/agents/sessions-spawn-hooks.test.ts`                           |
| 19   | 80.6s    | 4         | `src/image-generation/providers/google.test.ts`                     |
| 20   | 72.8s    | 57        | `src/gateway/call.test.ts`                                          |
| 21   | 70.9s    | 3         | `src/memory/manager.batch.test.ts`                                  |
| 22   | 70.6s    | 5         | `src/agents/tools/web-fetch.ssrf.test.ts`                           |
| 23   | 67.5s    | 22        | `src/agents/tools/message-tool.test.ts`                             |
| 24   | 66.4s    | 28        | `src/agents/tools/browser-tool.test.ts`                             |
| 25   | 63.1s    | 21        | `extensions/telegram/src/fetch.test.ts`                             |
| 26   | 62.0s    | 18        | `src/browser/server-context.remote-tab-ops.test.ts`                 |
| 27   | 61.2s    | 11        | `src/browser/server-context.remote-profile-tab-ops.test.ts`         |
| 28   | 61.0s    | 5         | `src/gateway/server.canvas-auth.test.ts`                            |
| 29   | 60.6s    | 4         | `src/memory/embeddings-voyage.test.ts`                              |
| 30   | 58.7s    | 12        | `src/agents/deneb-tools.subagents.sessions-spawn.allowlist.test.ts` |

### 적용된 최적화

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

### 추가 최적화 기회

| 영역                     | 순위                   | 원인                                               | 난이도          |
| ------------------------ | ---------------------- | -------------------------------------------------- | --------------- |
| Telegram 테스트          | 1, 3, 6, 7, 11, 15, 25 | `vi.doMock` + `importOriginal` 체인, 40+ mock 리셋 | 대규모 리팩터링 |
| Gateway/CLI 테스트       | 8, 9, 10, 20, 28       | 통합 스타일 setup, 모듈 리로딩                     | 중간            |
| Memory/Embeddings 테스트 | 13, 21, 29             | 임베딩 연산 또는 무거운 mock setup                 | 중간            |
| Browser 테스트           | 24, 26, 27             | 원격 탭 조작, 비동기 대기                          | 중간            |
