# chat 서브트리 지도 (구조)

> 챗 파이프라인의 **구조적 지도** — 무엇이 어디에 있고 한 턴이 어떻게 흐르는지. 정책(캐시·동시성·로깅·모델역할)은 `docs/agent-rules/*.md`가 소관이고 여기 복붙하지 않는다. 모듈 전체 맵·"도구/RPC 추가법"은 상위 `gateway-go/CLAUDE.md`.

## 디렉토리 맵 (서브패키지)

| 경로 | 역할 | 의존 규칙 |
|---|---|---|
| `toolport/` | 리프 패키지. 공유 타입(`ToolFunc`/`ToolDef`/`ToolRegistrar`/`ToolExecutor`), `TurnContext`/`RunCache`, context helper, display 새니타이저(`display.go`) | chat 내부 import 0, domain/platform import 0 |
| `tooldeps/` | 도구 wiring 의존 bag(`CoreToolDeps`/`ProcessDeps`/`SessionDeps`/`ChronoDeps`/`CalendarDeps` 등)과 capability port | tool 실행 타입·context helper 소유 금지 |
| `tools/` | 순수 도구 구현(fs/exec/git/mail_archive/calendar/wiki/web/asr/paddleocr/sessions/…). `toolport/` 실행 계약과 `tooldeps/` 의존 bag을 소비 | chat/ import 금지 |
| `toolwire/` | 도구 등록 허브. `core/register.go`의 `Register*Tools(...)`가 구현(tools/)+스키마(`schema/tool_schemas_gen.go`)를 `ToolRegistrar`에 배선. `schema/tool_schemas_gen.go`는 **생성물** | chat/ import 금지 |
| `toolreg/` | 얇은 leaf — session tool-preset 상수/헬퍼만. 등록 배선은 `toolwire/` | chat/ import 금지 |
| `prompt/` | 시스템 프롬프트 조립(`system_prompt.go`), 컨텍스트 파일 로더(`context_files.go`), `prompt_cache.go`, 토픽지식(`topic_knowledge.go`), 예산(`budget.go`) | — |
| `web/` | `web` 도구 백엔드: fetch/HTML 전처리/youtube/검색 escalate/stealth, singleflight+캐시 | — |
| `linkenrichment/` | 입력 사용자 메시지 링크의 URL 추출, bounded 병렬 fetch, HTML/YouTube 변환, panic 격리와 start/join/cancel 수명주기 | chat root 타입을 import하지 않고 좁은 `Config`/`Sanitizer` 계약만 소비 |
| `streaming/` | `Broadcaster` — 턴 이벤트 SSE 방출 | — |
| `knowledge/` | `knowledge` 도구 → `domain/knowledge`'s `Router` 얇은 어댑터 | — |
| `denebui/` | deneb-ui 블록 검증·저작. wire 포맷 = **라벨 HTML v2**(`html.go`, 그래머: `docs/research/deneb-ui-html.md`; legacy JSON은 구 트랜스크립트 표시용 strict 경로). 서버 조립 collapsed 카드(메일 등) + 시스템 프롬프트 소통 섹션이 일반 응답 카드 사용을 허용([project_kaiui_server_assembly]) | — |
| `toolpreset/` (형제: `pipeline/toolpreset/`) | 서브에이전트 도구 프리셋(implementer 등) | — |

루스 파일(top-level)은 기능별 클러스터로 읽는다:
- **`run_*`** — 한 에이전트 턴의 실행 파이프라인(↓ 흐름).
- **`tool_*`** — 도구 실행 주변(분류/압축/캐시안정/사후처리/변이검증/skill consult).
- **`recall_*` + `run_tail_inject.go`** — 회상 프리플라이트 → 마지막 user 메시지 꼬리 주입.
- **`slash_*` + `*_dispatch.go`** — 슬래시 커맨드(`/help`·`/reset`·`/status`·`/kill`·`/goal`·`/rollback`·`/update`·`/restart`·`/weekly`).
- 캐시 마커: `cache_breakpoints.go`·`tier1_cache.go`·`prompt_snapshot_persist.go`·`calendar_glance.go`.

## 핵심 흐름: 한 턴의 실행 순서

```
startAsyncRun (run_start.go)        # 세션 확보, abort ctx, buildRunDeps, goroutine 스폰
  └ runAgentAsync (run.go)
      └ runAgentWithFallback (run_fallback.go)   # 역할→폴백 체인, isStalledResult 판정
          └ executeAgentRun (run_exec.go)        # ★코어: user msg persist → 컨텍스트 조립 → LLM 루프 → 결과 persist
              ├ prepareContextAndPrompt (run_prepare.go)  # assembleMessages(압축 포함) + finalizePrompt
              ├ resolveModel / resolveClient (run_model.go, run_provider.go)  # 역할→모델, API 모드, 캐시 호환
              ├ wireStreamHooks (run_hooks.go)            # before/after tool, steer, trailing 캐시 훅
              └ (도구 루프는 agentsys/agent 가 구동, 도구 실행은 ToolRegistry.Execute)
      └ handleRunSuccess / handleRunError (run_lifecycle.go)  # 라이프사이클 이벤트, 실패 분류, finishRun
```

- 도구 디스패치는 **단일 평면 레지스트리 lookup** = `tools.go`의 `ToolRegistry.Execute`. 순서 있는 인터셉션 체인 없음(상위 CLAUDE.md "Tool Interception & Safety" 참조).
- 압축은 `run_prepare.go`의 `assembleMessages`가 `pipeline/compaction`(티어 알고리즘)을 호출하고, 장기세션 상태/DAG는 `pipeline/polaris`(스토어 엔진)가 보유. **compaction=전략, polaris=스토어**로 분리돼 있다.

## 흔한 작업 진입점

| 하려는 것 | 시작 파일 |
|---|---|
| 새 도구 추가 | `tools/<name>.go` 구현 → `toolwire/core/register.go` `Register*Tools`에 배선 → 스키마는 `toolwire/schema/tool_schemas.json` + `make tool-schemas` (상위 CLAUDE.md 절차) |
| 턴 실행 단계 수정 | `run_exec.go`(코어) 부터. 준비단계는 `run_prepare.go`, 폴백은 `run_fallback.go` |
| 모델/프로바이더 해석 변경 | `run_model.go`(역할→모델), `run_provider.go`(클라이언트·API모드·캐시호환) |
| 회상(recall) 경로 | `recall/recall_preflight.go`(오케스트레이션) → `recall/recall_evidence.go`(소스별 수집) → `run_tail_inject.go`(주입) |
| 시스템 프롬프트 | `prompt/system_prompt.go`(조립), `prompt/context_files.go`(컨텍스트 파일) |
| 사용자 입력 링크 보강 | `link_enrichment.go`(briefcase/caller-history gate·adapter) → `linkenrichment/engine.go`(fetch·변환·수명주기) |
| 슬래시 커맨드 | `slash_commands.go`(전처리) → `slash_dispatch.go`(디스패치) |

## 함정 (이 서브트리 특유)

- **레이어 의존 방향 엄수**: `toolport/`(안정 포트)와 `tooldeps/`(도구 의존 bag)를 `tools/`와 `toolwire/`가 소비한다. `tools/`·`toolwire/`는 **chat/를 import하지 않는다**. chat-내부 상태(post-processor·skills/wiki/contacts/calendar 배선)에 의존하는 등록만 `toolreg_core.go`(얇은 래퍼)에서 별도 수행.
- **회상·자동배달 지시문은 system이 아니라 마지막 user 메시지 꼬리로 주입**(`run_tail_inject.go`). per-turn 가변 바이트를 system에 넣으면 vLLM APC가 히스토리 전체를 무효화한다 — 근거/규칙: `docs/agent-rules/prompt-cache.md` §1.5.
- **캐시 마커는 정확히 4개**(system 2 + trailing 2). 새 `cache_control` 추가 시 `cache_breakpoint_budget_test.go`가 먼저 빨개진다 — `docs/agent-rules/prompt-cache.md` §1.
- **세션-동결 스냅샷**(tier1/context/topic)은 재시작 시 증발해 APC 재-prefill을 유발 → `prompt_snapshot_persist.go`가 persist/복원. `/reset`이 일괄 clear.
- **생성 파일 직접 수정 금지**: `toolwire/schema/tool_schemas_gen.go`, `tool_classification_gen.go` (`docs/agent-rules/generated-code.md`).
- **모델 역할 하드코딩 금지** — 코드는 역할만 고른다(`docs/agent-rules/model-roles.md`). 요약/추출 헬퍼를 analysis(클라우드)로 두면 비용·레이턴시가 샌다.
- **새 goroutine/뮤텍스**는 `docs/agent-rules/concurrency.md`(턴 데드라인=`server.DefaultTurnDeadline`, recover 필수). 사용자 무응답 실패는 `Error`+broadcast(`docs/agent-rules/logging.md`).
- 링크 보강 goroutine과 budget을 top-level chat으로 되돌리지 않는다.
  `linkenrichment.Engine`이 start/join/cancel과 fallback을 함께 소유하고,
  chat root는 요청 gate와 사용자 표시 sanitize adapter만 소유한다.

## Local change scope

한 턴 실행·도구·프롬프트 변경은 chat 서브트리 안에 가둔다.

- 함께 바꿔도 되는 이웃: `toolport/`·`tooldeps/`·`tools/`·`toolwire/`(도구 계약),
  `prompt/`(시스템 프롬프트), `pipeline/compaction`·`pipeline/polaris`(압축/스토어),
  `linkenrichment/`(링크 보강). `Execute`/`ToolRegistry`/`TurnContext`/
  `RunCache` 계약이 바뀌면 `agent_test.go`와 해당 `run_*_test.go`를 먼저 본다.
- 건드리지 말 것: `runtime/server` method registry, genesis acceptance machinery,
  prompt-cache 불가침 규칙(`docs/agent-rules/prompt-cache.md`)을 깨는 system
  프롬프트 per-turn 가변 바이트, 생성 스키마 파일 직접 수정.
- 집중 검증: `cd gateway-go && go test ./internal/pipeline/chat`

## 집중 검증

top-level chat 변경은 `run_exec.go`의 `executeAgentRun`, `handler.go`의
`ChatHandler`, `tools.go`의 `ToolRegistry` 중 실제 변경 진입 심볼을 테스트에서
직접 호출해 검증한다. 패키지 전체의 결정적 검증 명령은 다음과 같다.

`cd gateway-go && go test ./internal/pipeline/chat`

링크 fetch·변환·취소 의미를 바꿨다면
`go test -race ./internal/pipeline/chat/linkenrichment ./internal/pipeline/chat`를
함께 실행한다.

도구·프롬프트·회상처럼 하위 패키지 계약도 바뀌면 해당 하위 패키지를 별도로
검증한다. 생성 스키마를 건드린 변경은 `make tool-schemas` 후 diff가 비어 있어야
하며, 런타임 배선 확인을 위해 server 테스트를 대신 사용하지 않는다.
