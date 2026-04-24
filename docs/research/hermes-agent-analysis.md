# Hermes Agent 심층 분석 보고서

> **분석 대상**: [NousResearch/hermes-agent](https://github.com/NousResearch/hermes-agent)
> **분석 버전**: v0.11.0 (2026-04-23, "The Interface release")
> **분석 커밋**: `c95c6bd` (main, 2026-04-23 Merge PR #14818 ink-perf)
> **분석 일시**: 2026-04-24
> **분석 방법**: 레포 전체 클론 + 7개 병렬 서브에이전트(Agent A–G) 심층 파헤치기 + 인프라/정책 파일 직접 읽기
> **레포 규모**: 43MB, 2,310 파일, 1,091 Python 파일, 32,506 LOC (top-level `.py`만), 테스트 ~15,000 / ~700 파일

---

## 목차

1. [프로젝트 개요 & 철학](#1-프로젝트-개요--철학)
2. [레포 최상위 구조](#2-레포-최상위-구조)
3. [v0.11.0 릴리스 주요 변경](#3-v0110-릴리스-주요-변경)
4. [에이전트 코어: `run_agent.py`](#4-에이전트-코어-run_agentpy)
5. [LLM 어댑터 레이어](#5-llm-어댑터-레이어)
6. [크리덴셜 풀 & 레이트 리밋](#6-크리덴셜-풀--레이트-리밋)
7. [에러 분류 & 리다액션](#7-에러-분류--리다액션)
8. [도구(Tools) 시스템](#8-도구tools-시스템)
9. [보안 계층 스택](#9-보안-계층-스택)
10. [터미널 백엔드 (6종)](#10-터미널-백엔드-6종)
11. [자기개선 루프 (Memory/Skills/Insights)](#11-자기개선-루프-memoryskillsinsights)
12. [메모리 프로바이더 (Honcho/Mem0/…)](#12-메모리-프로바이더)
13. [세션 DB & FTS5 검색](#13-세션-db--fts5-검색)
14. [CLI 아키텍처](#14-cli-아키텍처)
15. [TUI (Ink/React + Python JSON-RPC)](#15-tui-inkreact--python-json-rpc)
16. [스킨 엔진 (데이터 주도 테마)](#16-스킨-엔진)
17. [Gateway & 메시징 플랫폼 (20+)](#17-gateway--메시징-플랫폼)
18. [Cron 스케줄러](#18-cron-스케줄러)
19. [ACP (IDE 통합)](#19-acp-ide-통합)
20. [RL 연구 인프라 (Atropos/Tinker)](#20-rl-연구-인프라-atropostinker)
21. [배치 러너 & 트래젝토리 컴프레서](#21-배치-러너--트래젝토리-컴프레서)
22. [인프라 (Docker/Nix/설치)](#22-인프라-dockernix설치)
23. [보안 정책 (SECURITY.md)](#23-보안-정책-securitymd)
24. [핵심 설계 패턴 정리](#24-핵심-설계-패턴-정리)
25. [Deneb 프로젝트에의 시사점](#25-deneb-프로젝트에의-시사점)

---

## 1. 프로젝트 개요 & 철학

### 1.1 정체성

Hermes Agent는 **Nous Research**가 2026년 3월부터 공개 개발해 온 **MIT 라이선스 "자기개선(self-improving) AI 에이전트"**. 공식 슬로건:

> "The only agent with a built-in learning loop — it creates skills from experience, improves them during use, nudges itself to persist knowledge, searches its own past conversations, and builds a deepening model of who you are across sessions."

### 1.2 핵심 철학

- **어떤 모델이든 사용 가능** (provider lock-in 회피): Nous Portal, OpenRouter(200+ 모델), NVIDIA NIM(Nemotron), Xiaomi MiMo, z.ai/GLM, Kimi/Moonshot, MiniMax, HuggingFace, OpenAI, Anthropic, Bedrock, Gemini, 자체 엔드포인트. `hermes model` 한 줄로 전환.
- **어디서나 실행**: 노트북/VPS(5달러)/GPU 클러스터/Modal/Daytona 서버리스. 로컬 의존 없음.
- **메시징 중심**: Telegram, Discord, Slack, WhatsApp, Signal, Matrix, Mattermost, Email, SMS, Home Assistant, DingTalk, Feishu, WeChat/WeCom, QQBot, BlueBubbles, webhook, api_server — 17+ 플랫폼에서 동일 에이전트에게 말 걸 수 있음.
- **6개 터미널 백엔드**: local, Docker, SSH, Daytona, Singularity, Modal. Modal/Daytona는 서버리스로 유휴 시 비용 거의 0.
- **연구 기반**: Atropos RL 환경 + Tinker 트레이너 + 트래젝토리 컴프레션으로 "다음 세대 tool-calling 모델 학습"을 타겟.

### 1.3 수치로 본 성장

- v0.1.0 → v0.11.0 (6주): 1,556 커밋 / 761 PR 머지 / 1,314 파일 / 224,174 insertions / 29 커뮤니티 기여자.
- 단일 개발자 프로젝트가 아닌 **커뮤니티 확장형**. PR 번호가 v0.11.0 릴리즈 노트에 `#14818`까지 등장 → 한 달에 수천 개 PR 단위 처리.

### 1.4 라이선스 & 트러스트 모델

- **MIT 라이선스**.
- **싱글 테넌트, 싱글 오퍼레이터** 모델. 멀티 유저 격리는 OS/호스트 레벨에서 해결. 게이트웨이의 Telegram/Discord/Slack 승인 유저는 **모두 동등 신뢰**를 받음.
- **스킬은 호스트 코드와 동급의 신뢰** / MCP 서버는 낮은 신뢰 (ENV 필터링, OSV 멀웨어 스캔, pinned 커밋).

---

## 2. 레포 최상위 구조

### 2.1 Top-level 파일 (32K LOC, `.py`만)

| 파일 | LOC | 역할 |
|---|---:|---|
| `run_agent.py` | 12,174 | **AIAgent 클래스 — 핵심 대화 루프** (~625KB) |
| `cli.py` | 11,096 | **HermesCLI 클래스 — 인터랙티브 오케스트레이터** (~498KB) |
| `hermes_state.py` | 1,591 | SQLite SessionDB + FTS5 검색 |
| `trajectory_compressor.py` | 1,508 | **트레이닝 데이터용** 트래젝토리 압축 (다름! context_compressor와 구분) |
| `batch_runner.py` | 1,291 | 병렬 배치 실행 (dataset→trajectory) |
| `mcp_serve.py` | 867 | MCP 서버 기능 (Hermes가 MCP 서버로도 작동) |
| `mini_swe_runner.py` | 736 | SWE-bench 외부 벤치마크 러너 |
| `toolsets.py` | 731 | `_HERMES_CORE_TOOLS`, 툴셋 묶음 |
| `model_tools.py` | 642 | `discover_builtin_tools()`, `handle_function_call()` |
| `rl_cli.py` | 446 | RL 훈련 런처 |
| `hermes_logging.py` | 390 | slog-style 세션 컨텍스트 로깅 |
| `toolset_distributions.py` | 364 | 플랫폼별 툴셋 배포 |
| `hermes_constants.py` | 295 | `get_hermes_home()`, 프로파일 지원, Termux/WSL/Container 탐지 |
| `utils.py` | 271 | atomic_json/yaml_write, IPv4 preference, proxy normalize |
| `hermes_time.py` | 104 | IANA 타임존 해석 (`HERMES_TIMEZONE` → config.yaml → 서버 로컬) |

### 2.2 디렉터리 계층

```
hermes-agent/
├── agent/                # LLM 어댑터, 메모리, 캐싱, 컨텍스트, 크리덴셜
├── hermes_cli/           # CLI 서브커맨드, 설정, 스킨, 플러그인 로더 (50+ 파일)
├── tools/                # 툴 구현 (~60 파일) + environments/ (6 터미널 백엔드)
├── gateway/              # 메시징 게이트웨이 런타임 + platforms/ (17+ 플랫폼 어댑터)
├── plugins/              # 플러그인 2종: 일반 + memory provider (honcho/mem0/...)
├── skills/               # 빌트인 스킬 (25 카테고리)
├── optional-skills/      # 헤비/니치 스킬 (16 카테고리, on-demand)
├── ui-tui/               # Ink(React) 터미널 UI — 신규 `hermes --tui`
├── tui_gateway/          # TUI용 Python JSON-RPC 백엔드 (66 메서드)
├── acp_adapter/          # ACP 서버 (VS Code / Zed / JetBrains 통합)
├── acp_registry/         # ACP 등록
├── cron/                 # 스케줄러 (jobs.py, scheduler.py)
├── environments/         # Atropos RL 환경 (hermes_base_env, agentic_opd_env, swe, web_research, terminal_test)
├── tinker-atropos/       # Tinker 트레이너 서브모듈
├── datagen-config-examples/ # YAML datagen 컨피그 예제
├── web/                  # Vite+TS 웹 대시보드
├── website/              # Docusaurus 공식 문서
├── scripts/              # 설치/테스트/릴리스/감사 스크립트
├── docker/               # entrypoint.sh, SOUL.md
├── nix/                  # Nix 모듈 (packages, nixosModules, devShell)
├── packaging/homebrew/   # Homebrew 포뮬러
├── tests/                # pytest — ~15k 테스트, ~700 파일
└── .plans/               # 로드맵 문서 (openai-api-server, streaming-support)
```

### 2.3 의존성 트리 (pyproject.toml)

**베이스** (`dependencies`):
- LLM SDK: `openai>=2.21.0`, `anthropic>=0.39.0`
- HTTP/retry: `httpx[socks]`, `tenacity`, `requests`
- CLI: `prompt_toolkit`, `rich`, `fire`
- 스키마: `pydantic`, `pyyaml`, `jinja2`
- 툴: `exa-py`, `firecrawl-py`, `parallel-web`, `fal-client`
- TTS: `edge-tts` (무료 기본)
- GitHub App: `PyJWT[crypto]`

**옵셔널 extras** (23종):
- `modal`, `daytona` — 서버리스 백엔드
- `messaging` — `python-telegram-bot`, `discord.py[voice]`, `slack-bolt`, `slack-sdk`, `aiohttp`, `qrcode`
- `slack`, `matrix`, `dingtalk`, `feishu` — 개별 플랫폼
- `cron` — `croniter`
- `mcp` — MCP SDK
- `acp` — Agent Client Protocol
- `honcho`, `mistral`, `bedrock` — 추가 프로바이더
- `web` — FastAPI + uvicorn (웹 대시보드)
- `voice` — `faster-whisper`, `sounddevice`, `numpy`
- `tts-premium` — `elevenlabs`
- `rl` — **`atroposlib` (Nous Research 레포 pinned 커밋), `tinker` (thinking-machines-lab pinned 커밋), `wandb`**
- `termux` — Android/Termux 전용 경량 조합

**엔트리포인트**:
- `hermes = "hermes_cli.main:main"`
- `hermes-agent = "run_agent:main"`
- `hermes-acp = "acp_adapter.entry:main"`

**Python**: `>=3.11` 요구, ty(static type check) `3.13` 기반.

---

## 3. v0.11.0 릴리스 주요 변경

**"The Interface release"** (2026-04-23, v0.9.0→v0.11.0: 1,556 commits / 761 PRs / 29 contributors)

### 3.1 하이라이트

1. **Ink-based TUI**: `hermes --tui` — React/Ink 기반 완전 재작성 + Python JSON-RPC 백엔드(`tui_gateway/`). sticky composer, OSC-52 클립보드, per-turn 스톱워치, git branch 상태바, subagent spawn observability 오버레이. ~310 커밋.
2. **Transport ABC + Native AWS Bedrock**: 포맷 변환과 HTTP transport가 `run_agent.py`에서 `agent/transports/` 레이어로 추출. `AnthropicTransport`, `ChatCompletionsTransport`, `ResponsesApiTransport`, `BedrockTransport` 각각 자기 포맷 소유.
3. **5개 추론 경로 신설**: NVIDIA NIM, Arcee AI, Step Plan, Google Gemini CLI OAuth, Vercel ai-gateway (pricing + dynamic discovery).
4. **GPT-5.5 via Codex OAuth**: ChatGPT Codex OAuth로 GPT-5.5 호출. 모델 picker에 live model discovery 배선.
5. **QQBot 17번째 플랫폼**: QQ Official API v2 어댑터, QR 설정 마법사, 스트리밍 커서, 이모지 리액션.
6. **플러그인 확장**: `register_command`(슬래시 추가), `dispatch_tool`(직접 디스패치), `pre_tool_call` veto, `transform_tool_result` rewrite, `transform_terminal_output`, 이미지 생성 백엔드, 커스텀 대시보드 탭.
7. **`/steer <prompt>`**: 실행 중인 에이전트에게 **다음 툴콜 직후 노트 주입** — 턴 중단 없이, 프롬프트 캐시도 깨지 않음.
8. **Shell hooks**: 쉘 스크립트를 파이썬 플러그인 없이 Hermes 라이프사이클 훅(pre/post_tool_call, on_session_start)에 바인딩.
9. **Webhook direct-delivery**: LLM 없이 웹훅 페이로드를 플랫폼 채팅으로 바로 푸시 (알림/업타임 체크용).
10. **Smarter delegation**: `delegate_task`에 `orchestrator` 역할 + `max_spawn_depth` 설정. 형제 subagent들이 파일 상태 협력 레이어로 충돌 회피.
11. **Auxiliary models UI**: `hermes model`에 per-task(compression/vision/search/title) 보조 모델 전용 설정 화면. `auto`는 메인 모델로 fallback.
12. **Dashboard 플러그인 시스템 + 라이브 테마**: 대시보드 써드파티 플러그인 (탭/위젯/뷰 추가), 테마 hot-swap.

---

## 4. 에이전트 코어: `run_agent.py`

### 4.1 AIAgent 클래스 개요

`run_agent.py:698-1399` 의 `__init__`은 약 **60개 파라미터**를 받는다. 카테고리별로:

#### A. 크리덴셜 & 라우팅 (`base_url`, `api_key`, `provider`, `api_mode`, `acp_command`)
- **호스트명 기반 자동 프로바이더 감지** (lines 857–873):
  - `bedrock-runtime.*` → Bedrock
  - `/anthropic` 접미사 → Anthropic 프로토콜
  - `api.anthropic.com` → 네이티브 Anthropic
- 나머지는 `api_mode`("chat_completions" | "codex_responses" | ...)로 라우팅

#### B. 실행 예산 & 동시성
- `max_iterations=90` (기본)
- `iteration_budget` — **thread-safe `IterationBudget` 클래스** (lines 191–233): `_used`를 `_lock`으로 보호, **세션 전체 API 턴이 공유** (즉 subagent도 같은 풀에서 차감)
- `tool_delay`, `enabled_toolsets`/`disabled_toolsets`

#### C. 콜백 & 훅 (15+ callable)
- `tool_progress_callback`, `thinking_callback`, `reasoning_callback`, `clarify_callback`, `step_callback`, `stream_delta_callback`, `interim_assistant_callback` …
- 에이전트 라이프사이클 각 지점에서 발화

#### D. 컨텍스트 & 메모리
- `skip_context_files`, `skip_memory`, `session_db`, `parent_session_id`
- 프롬프트 캐싱 정책 (lines 1007–1009): **Claude on Anthropic/OpenRouter면 자동 활성화**

#### E. 상태 필드 (lines 949–979)
- `_interrupt_requested`, `_interrupt_message`, `_execution_thread_id` — 인터럽트 메커니즘
- `_pending_steer`, `_pending_steer_lock` — **`/steer` 주입을 role-alternation 깨지 않게 tool result에 append**
- `_delegate_depth`, `_active_children`, `_active_children_lock` — 서브에이전트 추적 (부모 인터럽트가 자식에게 전파)
- `_tool_worker_threads`, `_tool_worker_threads_lock` — 동시 툴 실행 워커 TID 추적
- `_cached_system_prompt` (line 1372) — **한 번 생성 후 재사용 → 프롬프트 캐시 보존**
- `_checkpoint_mgr` — 역행 가능한 파일 시스템 스냅샷

### 4.2 메인 루프 (`run_conversation`, lines 8630–9520+)

```python
while (api_call_count < self.max_iterations and self.iteration_budget.remaining > 0) \
        or self._budget_grace_call:
    if self._interrupt_requested: break
    # API 호출
    response = client.chat.completions.create(...)
    if response.tool_calls:
        for tool_call in response.tool_calls:
            result = handle_function_call(...)
        api_call_count += 1
    else:
        return response.content  # 정상 종료
```

**종료 조건 4가지** (line 8999 루프 조건):
1. **인터럽트** — `_interrupt_requested` 플래그
2. **예산 소진** — `iteration_budget.consume()` 실패
3. **Grace call 만료** — 예산 초과 시 "마지막 한 번" 기회. `_budget_exhausted_injected=True` 플래그로 모델에게 예산 공지 메시지 1회 주입 → 모델이 마무리 응답 생성 → 플래그 해제 → 종료
4. **툴 없는 응답** — 모델이 순수 텍스트만 반환 (정상)

**예산 감소 타이밍 (line 9020)**: API 호출 **전에** `consume()` 호출. 실패하면 루프 즉시 이탈 → 오버런 방지.

### 4.3 툴 디스패치 (`_invoke_tool`, lines 7675-7752)

**Agent-level tool 가로채기** — registry lookup 전에 먼저 처리:

```python
if function_name == "todo":
    return _todo_tool(todos=..., store=self._todo_store)
elif function_name == "memory":
    return _memory_tool(action=..., store=self._memory_store)
elif self._memory_manager and self._memory_manager.has_tool(function_name):
    return self._memory_manager.handle_tool_call(...)  # Honcho 등
elif function_name == "delegate_task":
    return self._dispatch_delegate_task(function_args)
else:
    return handle_function_call(function_name, function_args, ...)  # 일반 툴
```

**interception 순서**: `todo` → `memory` → 외부 메모리 프로바이더 → `delegate_task` → registry. 외부 프로바이더가 registry보다 우선순위가 높아 Honcho 등이 자기 툴을 가로챌 수 있음.

### 4.4 스트리밍 & 리즈닝 처리 (lines 9115–9193)

- API 메시지 `api_messages` 구성 (line 9115) → 시스템 프롬프트 + prefill → conversation history → ephemeral context 순서
- **프롬프트 캐시 처리** (line 9188): `apply_anthropic_cache_control()` 로 시스템 프롬프트 + 최근 3개 메시지에 `cache_control` breakpoint 부착
- **리즈닝 content 복사** (line 9139): `_copy_reasoning_content_for_api()` 로 `reasoning_content` 필드 다음 턴 보존
- **내부 필드 strip** (lines 9141-9149): API 호출 직전 `_thinking_prefill` 등 private 필드 제거 (strict API rejection 방지)

### 4.5 인터럽트 전파 (lines 9003-9009, 7788-7797)

1. **루프 iter 시작마다 체크** — `self._interrupt_requested`
2. **per-thread 동기화** (line 8968) — `_execution_thread_id`에 신호 전파
3. **동시 실행 워커 TID 등록** (lines 7883-7885) — 각 툴 워커가 `_tool_worker_threads`에 자기 TID 등록
4. **fanout** (lines 7961-7973) — 인터럽트 발생 시 모든 워커 TID에 신호 전파, pending futures 캔슬

### 4.6 프롬프트 어셈블리 단계 (lines 8793-9172)

```
[ 시스템 프롬프트 (캐시됨, 턴마다 재사용) ]
  └─ line 8804-8843: `_cached_system_prompt` 초기화 후 compress 전까지 불변
[ prefill messages (API-time only, 영구 저장 X) ]
  └─ line 9174-9179
[ 대화 이력 (원본 메시지 — 복사, 변경 X) ]
[ ephemeral context (user message 내부에 주입) ]
  └─ line 9119-9136: memory manager prefetch + 플러그인 pre_llm_call
[ ephemeral system prompt (base 시스템에 append, 저장 X) ]
  └─ line 9164-9166
```

**캐시 breakpoint 배치** (`agent/prompt_caching.py` lines 41-72):
- 시스템 프롬프트: 1개 breakpoint
- 비시스템 최근 3개 메시지: 각 1개 (최대 4개, Anthropic 제한)
- TTL 기본 `"5m"` (5분 ephemeral)

### 4.7 컨텍스트 압축 (`agent/context_compressor.py`)

**트리거**:
1. **Preflight** (lines 8845-8911) — 루프 시작 전 이미 threshold(기본 context_length의 50%) 초과 시
2. **API 에러** — 컨텍스트 리밋 4xx 오류 시 `should_compress()` 체크
3. **수동** — 유저가 `/compress`

**전략**:
1. **Old tool result prune**: 큰 tool 출력을 1줄 요약으로 치환 (예: `"[terminal] ran 'npm test' → exit 0, 47 lines output"`)
2. **head protect**: 시스템 + 첫 user + 첫 assistant (기본 3개)
3. **tail protect**: 최근 ~20K 토큰 (또는 protect_last_n=20)
4. **middle summarize**: LLM이 "Active Task" 섹션 포함 구조화 요약 생성
5. **iterative update**: 재압축 시 summary 를 **업데이트**(교체 X)

**출력**: `SUMMARY_PREFIX = "[CONTEXT COMPACTION — REFERENCE ONLY]…Do NOT answer questions…Respond ONLY to the latest user message that appears AFTER this summary."` — 모델이 요약 자체에 답변하지 않도록 강제.

### 4.8 서브에이전트 (`_dispatch_delegate_task`, lines 7656-7673)

```python
def _dispatch_delegate_task(self, function_args):
    from tools.delegate_tool import delegate_task as _delegate_task
    return _delegate_task(
        goal=function_args.get("goal"),
        context=function_args.get("context"),
        toolsets=function_args.get("toolsets"),
        max_iterations=function_args.get("max_iterations"),
        parent_agent=self,
    )
```

**상태 격리**:
- `_delegate_depth` (line 977) — 깊이 증가, 이터레이션 예산 스케일
- `_active_children` (line 978) — 러닝 AIAgent 리스트. 부모 인터럽트 재귀 전파
- **공유 이터레이션 예산** (line 810) — 자식이 부모 `IterationBudget` 상속 → 트리 전체 합산 캡
- `SECURITY.md` 제약: **MAX_DEPTH = 2** (grandchildren 금지), `skip_memory=True` (메모리 공유 금지), `DELEGATE_BLOCKED_TOOLS = {delegate_task, clarify, memory, send_message, execute_code}` (재귀/유저인터랙션/부작용 금지)

### 4.9 주목할 설계 트릭

1. **프롬프트 캐시 불변성** (lines 8814-8842): 시스템 프롬프트를 DB에 저장, 재시작/재접속 시 **정확히 그 프롬프트 재사용** → Anthropic prefix cache hit 유지. 압축만이 유일하게 무효화.
2. **ephemeral/prefill 절대 영속화 X** (lines 9164-9179): API 호출 시에만 붙고 DB/트래젝토리에 기록 X → 유저 커스텀 페르소나/few-shot이 training 데이터에 유출되지 않음.
3. **Steer drain** (lines 9060-9108): `/steer` 메시지를 **마지막 tool-role 메시지에 주입** → role alternation 보존. tool 메시지 없으면 다음 툴 배치까지 pending.
4. **Grace call 메커니즘** (lines 9015-9024): 모델이 예산 소진 사실을 알리는 시점이 `api_call_count >= max_iterations` 시점. **한 번만** 주입 (`_budget_exhausted_injected=True`), 모델 1턴 더 → 플래그 해제. 중간 경고 없음 → 모델이 조기 포기("give up") 유발 안 함.
5. **Surrogate char 살균** (lines 363-481): 바이트 레벨 reasoning 모델(Kimi, GLM, Xiaomi)이 내뱉는 lone surrogate를 U+FFFD로 치환. 모든 중첩 필드(`reasoning_content`, `reasoning_details`) 에 걸쳐 pre-pass walk.

---

## 5. LLM 어댑터 레이어

### 5.1 통합 설계 철학

모든 어댑터는 **"OpenAI chat_completions 형태를 중심 프로토콜로"** 삼아 provider별 변환을 캡슐화. 각 어댑터 모듈은 stateless 함수 4종을 exports:

- `convert_messages_to_<provider>()` — 메시지 번역
- `convert_tools_to_<provider>()` — 툴 정의 변환
- `build_<provider>_kwargs()` — 요청 파라미터 조립
- `parse_<provider>_response()` — 응답 정규화 (OpenAI 호환 shape)

상속 없고, 컴포지션만 사용. 메인 retry loop가 dispatch.

### 5.2 프로바이더별 특이 처리

#### Anthropic Adapter (68KB)

**Thinking block signature 관리** (lines 1299-1390):
Anthropic은 thinking block을 **서명**한다. 컨텍스트 압축/메시지 병합으로 mutate 하면 서명 무효. 처리:
```python
_THINKING_TYPES = frozenset(("thinking", "redacted_thinking"))
if _is_third_party or idx != last_assistant_idx:
    # 써드파티 엔드포인트(MiniMax, Azure 등) 혹은 마지막이 아닌 assistant 턴:
    # 모든 thinking block strip — 서명이 Anthropic 독점이므로
    stripped = [b for b in m["content"]
                if not (isinstance(b, dict) and b.get("type") in _THINKING_TYPES)]
```

**Opus 4.7 sampling param 거부** (lines 1575-1582):
```python
if _forbids_sampling_params(model):
    for _sampling_key in ("temperature", "top_p", "top_k"):
        kwargs.pop(_sampling_key, None)
```
→ 서버 기본값 사용.

**Opus 4.6 xhigh effort 불가** (lines 1554-1568):
```python
if adaptive_effort == "xhigh" and not _supports_xhigh_effort(model):
    adaptive_effort = "max"
```

**OAuth via Claude Code 아이덴티티** (lines 1465-1491):
```python
if is_oauth:
    cc_block = {"type": "text", "text": _CLAUDE_CODE_SYSTEM_PREFIX}
    system = [cc_block] + system
    for tool in anthropic_tools:
        tool["name"] = _MCP_TOOL_PREFIX + tool["name"]  # "mcp_" 접두사
```
→ Claude Code 정체성 + user-agent 없으면 Anthropic 인프라가 OAuth 트래픽 500 반환.

#### Bedrock Adapter (42KB)

**Boto3 클라이언트 region-aware 캐싱** (lines 61-82):
```python
def _get_bedrock_runtime_client(region):
    if region not in _bedrock_runtime_client_cache:
        boto3 = _require_boto3()
        _bedrock_runtime_client_cache[region] = boto3.client(
            "bedrock-runtime", region_name=region)
    return _bedrock_runtime_client_cache[region]
```

**툴 지원 안 하는 모델 denylist** (lines 198-200): `_BEDROCK_NO_TOOL_SUPPORT_MODELS` — `toolConfig` 보내면 ValidationException.

#### Gemini Native Adapter (30KB)

**functionCall + thoughtSignature** (lines 136-155):
```python
part = {"functionCall": {"name": fn.get("name"), "args": args}}
thought_signature = _tool_call_extra_signature(tool_call)
if thought_signature:
    part["thoughtSignature"] = thought_signature  # 메시지 히스토리 validation
```

#### Codex Responses API (35KB)

**`fc_` 프리픽스 강제 + dual-ID 인코딩** (lines 145-155):
```python
def _derive_responses_function_call_id(call_id, response_item_id=None):
    # `call_id|response_item_id` 를 하나의 string으로 인코딩
    # Responses API가 `fc_` prefix 요구하면 합성
```

### 5.3 Transport ABC (v0.11.0 신규)

**`agent/transports/`** 레이어로 포맷 변환과 HTTP transport를 `run_agent.py`에서 추출:
- `AnthropicTransport` — Anthropic Messages API
- `ChatCompletionsTransport` — OpenAI-호환 기본 경로
- `ResponsesApiTransport` — OpenAI Responses API + Codex
- `BedrockTransport` — AWS Bedrock Converse API

각 Transport가 자기 포맷과 HTTP shape을 소유하므로 새 프로바이더 추가가 훨씬 쉬워짐 (PR #13347, #13366, #13430, #13805, #13814).

### 5.4 Auxiliary Client (132KB 거대 파일)

**역할**: 곁가지 작업(컨텍스트 압축, 세션 검색, 비전 분석, 타이틀 생성)이 **동일한 백엔드 해석 체인**을 쓰도록 중앙화.

**5가지 책임 영역**:
1. **프로바이더 해석 체인**: OpenRouter → Nous Portal → Custom → Codex OAuth → Anthropic → API-key providers (Gemini, Kimi 등)
2. **크레딧 소진 시 fallback**: 402/credit 에러 시 다음 프로바이더로
3. **프로바이더별 모델 선택** (lines 132-156): `"gemini": "gemini-3-flash-preview"`, `"zai": "glm-4.5-flash"`, `"kimi-coding": "kimi-k2-turbo-preview"` 등 cheap/fast auxiliary 모델
4. **vision/text 분기**: vision 작업은 vision-capable 백엔드(Gemini/Anthropic/Codex) 우선
5. **credential pool 통합** (lines 840-865): pool → env-var → config 순서

데드 코드 없음. 모든 함수는 `call_llm()` 또는 vision/compression/search 루틴에서 호출.

---

## 6. 크리덴셜 풀 & 레이트 리밋

### 6.1 데이터 구조 (`agent/credential_pool.py` 55KB)

```python
@dataclass
class PooledCredential:
    provider: str
    id: str                            # 6-char hex uuid
    label: str
    auth_type: str                     # AUTH_TYPE_OAUTH | AUTH_TYPE_API_KEY
    priority: int                      # 낮을수록 높은 우선순위
    source: str                        # "manual", "device_code", "claude_code"
    access_token: str
    refresh_token: Optional[str]
    last_status: Optional[str]         # STATUS_OK | STATUS_EXHAUSTED
    last_status_at: Optional[float]
    last_error_code: Optional[int]
    last_error_reset_at: Optional[float]  # 프로바이더 제공 reset 타임스탬프
    request_count: int = 0
    extra: Dict[str, Any]              # opaque JSON, persistence 왕복
```

### 6.2 선택 전략 (lines 728-756)

| 전략 | 알고리즘 |
|---|---|
| `STRATEGY_RANDOM` | `random.choice(available)` |
| `STRATEGY_LEAST_USED` | `min(available, key=lambda e: e.request_count)` |
| `STRATEGY_ROUND_ROBIN` | 첫 번째 선택 → priority 꼬리로 rotate → persist |
| `STRATEGY_FILL_FIRST` | 우선순위 최상위 하나 (기본) |

### 6.3 실패 처리

- **429 (rate limit)**: STATUS_EXHAUSTED 마킹, 기본 1시간 cooldown (`EXHAUSTED_TTL_429_SECONDS`), 프로바이더가 `reset_at` 주면 override
- **402 (billing)**: 같은 1시간 cooldown
- **401 (auth)**: STATUS_EXHAUSTED, **자동 refresh 없음** (caller 책임)
- **OAuth refresh** (lines 519-656): `AUTH_TYPE_OAUTH`이면 재발급 시도. Anthropic은 refresh 성공 시 **`~/.claude/.credentials.json`에 sync** → 다른 profile/CLI도 새 토큰 공유

**동시성**: `threading.Lock`으로 선택 + 리스 acquisition 보호 (lines 369-827). 크레덴셜별 request count로 부하 분산.

### 6.4 레이트 리밋 트래킹 (`rate_limit_tracker.py`)

12개 `x-ratelimit-*` 헤더 파싱: requests/min, requests/hour, tokens/min, tokens/hour + 각각의 reset 카운트.

```python
@dataclass
class RateLimitBucket:
    limit: int
    remaining: int
    reset_seconds: float
    captured_at: float

    @property
    def remaining_seconds_now(self) -> float:
        elapsed = time.time() - self.captured_at
        return max(0.0, self.reset_seconds - elapsed)
```

**`nous_rate_guard.py`** (5.6KB): Nous Portal 429를 **세션 간 기억**. 기록된 429가 있으면 다음 작업의 auxiliary client resolution이 reset 만료 전까지 Nous 건너뜀.

---

## 7. 에러 분류 & 리다액션

### 7.1 FailoverReason 분류 (`error_classifier.py` 32KB)

```python
class FailoverReason(enum.Enum):
    auth              # 일시 401/403 — refresh/rotate
    auth_permanent    # refresh 후에도 실패 — abort
    billing           # 402, credit 소진 — rotate
    rate_limit        # 429, 스로틀 — backoff + rotate
    overloaded        # 503/529 — backoff
    server_error      # 500/502 — retry
    timeout           # 연결/read 타임아웃
    context_overflow  # 컨텍스트 초과 — compress
    payload_too_large # 413
    model_not_found   # 404 — fallback
    format_error      # 400 bad request
    thinking_signature  # Anthropic thinking 서명 invalid
    long_context_tier # Anthropic extra usage gate
    unknown           # 미분류 — retry with backoff
```

**분류 파이프라인** (lines 289-474):
1. 프로바이더-특이 패턴 (thinking + "signature" → thinking_signature)
2. HTTP status
3. 에러 코드 (바디 JSON)
4. 메시지 패턴 매칭
5. SSL/TLS 일시 (`bad_record_mac`, `ssl alert` → timeout, 압축 X)
6. 서버 disconnect + 큰 세션 (>60% 토큰 or >120K) → context_overflow
7. Transport 에러 타입 (ReadTimeout, ConnectError)
8. fallback → unknown (재시도 with backoff)

### 7.2 리다액션 (`agent/redact.py` 12.6KB)

**패턴 (lines 62-174)**:
- 프리픽스: `sk-`, `ghp_`, `AIza`, `pplx-`, `AKIA`… (~20 벤더)
- 쿼리파람: `access_token`, `api_key`, `client_secret`, `code`, `signature`
- 바디 키: sensitive field names (JSON & form-urlencoded)
- Auth 헤더: `Authorization: Bearer ***`
- DB 커넥션: `postgres://user:***@host`
- JWT: `eyJ[A-Za-z0-9_-]{10,}...`
- Discord 멘션: `<@snowflake_id>`
- 전화번호: `+[1-9]\d{6,14}`

**짧은 토큰 마스킹** (lines 183-187):
```python
def _mask_token(token: str) -> str:
    if len(token) < 18:
        return "***"
    return f"{token[:6]}...{token[-4:]}"
```

**적용 단계**:
- 로그 (logger formatter)
- 툴 출력 (저장/표시 전)
- Prompt echoing (verbose 모드)
- **import time snapshot** (line 60): LLM이 `export HERMES_REDACT_SECRETS=false` 삽입해도 세션 중간에 리다액션 못 끔

---

## 8. 도구(Tools) 시스템

### 8.1 레지스트리 패턴 (`tools/registry.py`)

**싱글톤 레지스트리 + `threading.RLock()`**. 각 툴 모듈이 모듈-레벨 `registry.register()` 호출:

```python
registry.register(
    name="terminal",
    toolset="terminal_tools",
    schema={"description": "...", "parameters": {...}},
    handler=terminal_handler,
    check_fn=lambda: TERMINAL_ENV_AVAILABLE,
    requires_env=["TERMINAL_ENV"],
    is_async=False,
)
```

**시그니처** (lines 176-188):
- `check_fn`: 필터링 (truthy → 포함, falsy → 제외, 예외 → 불가)
- `requires_env`: ENV var 리스트 (비필수, 표시용)
- `is_async`: 비동기 핸들러는 `_run_async()` (model_tools.py lines 81-131)로 브릿지

**충돌 방지** (lines 191-213):
- built-in → plugin/MCP 덮어쓰기: **거부** (에러 로깅)
- MCP → MCP 덮어쓰기: **허용** (toolset 둘 다 `"mcp-"` 접두사)

**import 실패 핸들링** (lines 56-73): AST 스캐닝(`_module_registers_tools`)으로 self-registering 모듈 식별. import 실패해도 경고 로깅 후 스킵.

### 8.2 툴 발견 & 디스패치 (`model_tools.py` 26KB)

**`discover_builtin_tools()` 라이프사이클** (lines 138-152):
1. `tools/*.py` 전체 import → 각 모듈의 `registry.register()` 발화
2. MCP 서버 동적 등록 (MCP SDK 있으면)
3. 유저/프로젝트 플러그인 마지막 디스커버리

**`handle_function_call()` 디스패치 경로** (lines 477-608):

```python
def handle_function_call(function_name, function_args, task_id, ...):
    # 1. Arg coercion: "42" → 42, "true" → True
    function_args = coerce_tool_args(function_name, function_args)
    
    # 2. Agent-loop tool 차단 (lines 507-508)
    if function_name in {"todo", "memory", "session_search", "delegate_task"}:
        return {"error": "must be handled by agent loop"}
    
    # 3. Plugin pre_tool_call hook (lines 513-528): block signal 검사
    # 4. Non-read tool 시 read-loop 트래커 리셋 (lines 547-552)
    # 5. Registry dispatch (lines 558-568)
    result = registry.dispatch(function_name, function_args, task_id=..., ...)
    
    # 6. Plugin post_tool_call hook (lines 571-582)
    # 7. Plugin transform_tool_result hook (lines 590-606)
    return result
```

### 8.3 동적 스키마 후처리

**`get_tool_definitions()` post-processing** (lines 276-334): 조건부 의존성 툴의 스키마를 **실제 가용성에 맞게 재작성** — LLM이 없는 툴을 환각하지 않도록.

```python
# execute_code 스키마 재조립 (lines 282-289)
if "execute_code" in available_tool_names:
    sandbox_enabled = SANDBOX_ALLOWED_TOOLS & available_tool_names
    dynamic_schema = build_execute_code_schema(sandbox_enabled, mode=_get_execution_mode())

# browser_navigate 교차참조 스트리핑 (lines 320-334)
# browser_navigate는 있지만 web_search/web_extract 없으면
# "prefer web_search" 문구를 schema description에서 제거

# discord_server (lines 296-314)
# 봇의 실제 privileged intents (MESSAGE_CONTENT 등) 반영
```

### 8.4 주요 툴 목록 (60+)

**에이전트 레벨** (run_agent가 intercept):
- `todo` — 세션-스코프 todo 리스트
- `memory` — MEMORY.md/USER.md 읽기/쓰기
- `session_search` — FTS5 cross-session 검색
- `delegate_task` — 서브에이전트 스폰

**파일/쉘**:
- `terminal` — 메인 쉘 실행 (6개 백엔드)
- `read_file`, `write_file`, `patch` — 파일 조작 (체크포인트 통합)
- `search_files` — grep/ripgrep
- `file_state.py` — 파일 상태 트래킹

**웹/브라우저**:
- `web_search` — Exa/Parallel-Web 기반
- `web_extract` — Firecrawl 기반
- `browser_navigate`, `browser_snapshot`, `browser_click`, `browser_type` — 브라우저 자동화
- `browser_cdp_tool.py` — Chrome DevTools Protocol (유저 실제 브라우저 연결)
- `browser_camofox.py` — **Camoufox 안티탐지 Firefox 포크** (REST API, 300MB Node.js 서버)
- `browser_providers/`: base, Browserbase, BrowserUse, Firecrawl
- **Auto-detection 순서**: Camofox (local) → BrowserUse → Browserbase → agent-browser CLI

**멀티모달**:
- `vision_analyze` — 이미지 분석
- `image_generate` — fal/OpenAI/Gemini 이미지 생성
- `tts_tool.py` + `neutts_synth.py` — 무료 edge-tts + ElevenLabs(프리미엄) + **NeuTTS** (500MB 서브프로세스, Neuphonic 모델 `neutts-air-q4-gguf`)
- `transcription_tools.py` — faster-whisper STT
- `voice_mode.py` — 음성 인터페이스

**MCP & 통합**:
- `mcp_tool.py` — 지속적 데몬 이벤트 루프, `_servers: Dict[str, _ServerTask]`, 서버 `list_tools`를 `mcp-{config_name}` toolset으로 자동 등록
- `mcp_oauth.py`, `mcp_oauth_manager.py` — MCP 서버 OAuth flow
- `send_message_tool.py` — 플랫폼 간 메시지 (Telegram, Discord, Slack…)
- `discord_tool.py`, `homeassistant_tool.py`, `feishu_doc_tool.py`, `feishu_drive_tool.py`

**에이전트 제어**:
- `clarify_tool.py` — 유저에게 명확화 요청
- `approval.py` — 위험 명령 승인 gate
- `checkpoint_manager.py` — 파일 시스템 스냅샷/롤백
- `interrupt.py`, `process_registry.py` — 프로세스 관리
- `budget_config.py` — per-tool 예산

**자기개선**:
- `skill_manager_tool.py`, `skills_hub.py`, `skills_sync.py`, `skills_tool.py`, `skills_guard.py` — 스킬 관리
- `memory_tool.py` — 메모리 툴
- `session_search_tool.py` — 세션 검색

**특수**:
- `mixture_of_agents_tool.py` — MoA 앙상블 (Claude Opus/Gemini/GPT-5/Deepseek 병렬 → Opus aggregator). 하이퍼파라미터: REFERENCE_TEMPERATURE=0.6, AGGREGATOR_TEMPERATURE=0.4
- `rl_training_tool.py` — **에이전트가 자기 런타임에서 RL 훈련 시작** (Tinker+Atropos 서브모듈)
- `code_execution_tool.py` — LLM 생성 Python 실행 (child process, API 키 strip)
- `cronjob_tools.py` — 에이전트 self-scheduling
- `managed_tool_gateway.py`, `tool_backend_helpers.py`, `tool_result_storage.py` — 툴 인프라

**보안/시스템**:
- `path_security.py` — `validate_within_dir()` (심볼릭 링크 해결, `relative_to()` 확인)
- `tirith_security.py` — **Tirith 바이너리 서브프로세스 스캔** (GitHub에서 자동 설치, cosign provenance + SHA-256 체크섬, exit 0=allow/1=block/2=warn)
- `osv_check.py` — OSV 멀웨어 DB 체크 (`MAL-*` 어드바이저리만, CVE 아님; npx/uvx/pipx 패키지만, ~300ms)
- `url_safety.py` — SSRF 차단 (169.254.0.0/16 AWS metadata, 169.254.169.254, `metadata.google.internal` 등 하드블록)
- `website_policy.py` — 도메인 allow/block
- `credential_files.py` — 크레덴셜 파일 접근

### 8.5 서브에이전트 delegate_task

```python
DELEGATE_BLOCKED_TOOLS = frozenset({
    "delegate_task",      # 재귀 금지
    "clarify",            # 유저 인터랙션 금지
    "memory",             # 공유 MEMORY.md 쓰기 금지
    "send_message",       # 크로스 플랫폼 side-effect 금지
    "execute_code",       # 자식은 단계별 추론
})
_EXCLUDED_TOOLSET_NAMES = {"debugging", "safe", "delegation", "moa", "rl"}
```

부모가 자식에게 focused system prompt 전달. 자식은 자기 task_id, 자기 터미널 세션, 제한 toolset. 부모는 자식의 **summary result만** 보고 중간 턴 못 봄.

---

## 9. 보안 계층 스택

개념 파이프라인 (전부 한 파일은 아님):

### Layer 1: Tirith 프리 실행 스캔 (`tirith_security.py` 615-691)
- 외부 `tirith` 바이너리 서브프로세스 호출
- GitHub에서 cosign 검증 + SHA-256 체크섬으로 자동 설치
- exit code: 0=허용, 1=차단, 2=경고
- JSON 출력이 findings를 enrich하지만 exit code override 안 함
- Fail-open 설정 가능 (기본: 네트워크 에러 시 허용)

### Layer 2: URL 안전 (`url_safety.py` 78-97)
- SSRF 차단: 169.254.0.0/16 (AWS/GCP metadata), 100.64.0.0/10 (CGNAT)
- 항상 차단 IP: 169.254.169.254 (AWS/ECS task IAM 크레덴셜), 169.254.170.2
- 메타데이터 호스트네임 하드블록: `metadata.google.internal`, `metadata.goog`
- `security.allow_private_urls: true`로 RFC1918 허용 가능하지만 **메타데이터는 항상 차단**

### Layer 3: 웹사이트 정책 (`website_policy.py`)
- 도메인별 allow/block 리스트
- 브라우저 툴 접근 gating

### Layer 4: 경로 traversal (`path_security.py` 15-44)
- `validate_within_dir(path, root)`: 심볼릭 링크 해결 후 `relative_to()` 검사
- skill_manager, cronjob, credential_files 등이 사용

### Layer 5: OSV 멀웨어 (`osv_check.py` 26-62)
- MCP 서버 런치 전 `MAL-*` 어드바이저리 체크 (CVE 아님)
- npx/uvx 명령 파싱해서 ecosystem + 패키지명 추출
- Google 공용 OSV API 쿼리 (~300ms)
- Fail-open: 네트워크 에러 시 허용

### Layer 6: 명령 승인 (`approval.py`)
~70개 정규식 패턴:
- 파괴적: `rm -rf`, `git reset --hard`, `git push --force`
- 권한 상승: `chmod 777`, `chown -R root`
- 쉘 인젝션: `curl | bash`, `python <<`, `bash -c`
- 시스템 개입: `systemctl stop/restart`, `kill -9 -1`
- 디스크: `mkfs`, `dd if=`, 포크봄
- 프로젝트 config 덮어쓰기: `.env`, `config.yaml` redirect/tee

```python
_approval_session_key: contextvars.ContextVar[str]
def get_current_session_key(default="default"):
    # 우선순위: approval context var → session_context → os.environ
```

승인 모드:
- `on` (기본) — 유저에게 프롬프트
- `auto` — 설정한 지연 후 자동 승인
- `off` — 게이트 완전 해제 (break-glass)

### Layer 7: execute_code 샌드박스 (`code_execution_tool.py`)
- LLM 생성 Python 스크립트를 **child 프로세스**에서 실행
- API 키/토큰 strip (credential exfiltration 방지)
- 스킬이 `env_passthrough`로 선언했거나 `config.yaml`의 `terminal.env_passthrough`에 있는 env만 통과
- child는 Hermes 툴을 **RPC로만** 접근 (직접 API 콜 X)

---

## 10. 터미널 백엔드 (6종)

### 10.1 Base 클래스 (`tools/environments/base.py` 267-763)

```python
class BaseEnvironment(ABC):
    _stdin_mode = "pipe"  # "pipe" or "heredoc" (SDK 백엔드가 override)
    
    def __init__(self, cwd, timeout, env=None): ...
    
    @abstractmethod
    def _run_bash(self, cmd_string, *, login=False, timeout, stdin_data=None) -> ProcessHandle: ...
    
    @abstractmethod
    def cleanup(self): ...
    
    def execute(self, command, cwd="", *, timeout=None, stdin_data=None) -> dict:
        # 세션 스냅샷 sourcing, CWD 트래킹, 인터럽트 폴링, timeout enforcement
```

### 10.2 세션 스냅샷 (lines 330-365)

**백엔드 인스턴스당 1회**: 로그인 쉘 스폰 → env vars + 함수 + 별칭 캡처 → 스냅샷 파일 (`/tmp/hermes-snap-abc123.sh`). 각 명령 실행 전에 re-source → 상태 영속. 스냅샷 실패 시 `bash -l` fallback.

### 10.3 CWD 영속 (lines 371-407)

명령 래퍼가: 스냅샷 source → cwd로 cd → 명령 실행 → CWD를 파일 + stdout marker에 기록. 원격 백엔드는 marker를 stdout에서 파싱, 로컬은 temp 파일 읽기.

### 10.4 인터럽트 + 활동 콜백 (lines 423-627)

- 논블로킹 `select()` stdout 폴링
- 0.2초마다 `is_interrupted()` 체크
- 10초마다 activity 콜백 (gateway liveness)
- 인터럽트/타임아웃 시 `_kill_process()` — 프로세스 그룹 종료
- UTF-8 incremental decoder로 멀티바이트 분할 grace하게 처리

### 10.5 백엔드 비교

| 백엔드 | 파일 | 핵심 | stdin |
|---|---|---|---|
| **Local** | `local.py` | native subprocess, `os.setsid`, preexec signal mask | pipe |
| **Docker** | `docker.py` | `docker run` per-call, 워킹 디렉터리 바인드 마운트 | pipe |
| **SSH** | `ssh.py` | 원격 exec, `FileSyncManager`로 파일 I/O, CWD는 stdout marker | pipe |
| **Singularity** | `singularity.py` | `.sif` 컨테이너, overlay FS 영속, `_get_scratch_dir()` | pipe |
| **Daytona** | `daytona.py` | Workspace SDK, 영속 파일시스템, `_ThreadedProcessHandle` adapter | heredoc |
| **Modal** | `modal.py` | 네이티브 Modal SDK (런타임 래퍼 X), 샌드박스/호출, `~/.hermes/modal_snapshots.json` 키드 by task_id | heredoc |

**Modal 서버리스 패턴** (lines 147-150):
- `_AsyncWorker`: 백그라운드 event loop + `asyncio.run_coroutine_threadsafe()` 로 sync 브릿지
- task_id 기반 스냅샷 영속. fresh 샌드박스 per-call, 스냅샷 있으면 재사용 (env vars 복원, CWD 따라감)

---

## 11. 자기개선 루프 (Memory/Skills/Insights)

Hermes의 핵심 marketing claim. 코드 검증 결과:

| 클레임 | 상태 | 근거 |
|---|---|---|
| "Built-in learning loop" | ✅ 진짜 | `run_agent.py` lines 2595-2610 nudge 트리거, `_spawn_background_review()` |
| "Creates skills from experience" | ✅ 진짜 | iteration count 임계 (기본 10), 리뷰 에이전트가 skill store에 바로 씀 |
| "Improves skills during use" | ❌ 코드에 없음 | 스킬은 런타임에 static, 라이브 적응/평점/edit-during-run 메커니즘 부재 |
| "Nudges itself to persist knowledge" | ✅ 진짜 | `_memory_nudge_interval` N턴마다 background memory 리뷰 |
| "Searches its own past" | ✅ 진짜 | FTS5 `messages_fts` + 트리거 sync |
| "Deepening user model" | ⚠️ 부분 | Honcho dialectic(multi-pass synthesis)은 있음, 빌트인 MEMORY.md는 정적 |
| "Skills via slash commands (cache-safe)" | ✅ 검증 완료 | user message로 주입 (line 6227), system prompt 아님 |

### 11.1 메모리 Nudge

```python
self._memory_nudge_interval = 10  # config: memory.nudge_interval
self._turns_since_memory = 0

# memory 툴 호출마다 리셋 (line 2750)
if function_name == "memory":
    self._turns_since_memory = 0

# 트리거 (line 2600)
if (self._memory_nudge_interval > 0
        and self._turns_since_memory >= self._memory_nudge_interval):
    _should_review_memory = True
```

**nudge는 `_spawn_background_review()`** (line 2330) 로 fork된 AIAgent가 **백그라운드 스레드에서** 다음 프롬프트로 실행 (lines 2245-2260):

```python
_MEMORY_REVIEW_PROMPT = (
    "Review the conversation above. Extract 2-3 most important facts about the user "
    "(goals, preferences, working style, constraints) that would be useful to remember. "
    "Existing memory: ...\n\n"
    "Update MEMORY.md and USER.md using the memory tool if you discover anything worth saving."
)
```

유저 응답 반환 **후에** 트리거. 유저 메시지 주입 아님.

### 11.2 스킬 자동 생성

```python
self._skill_nudge_interval = 10

# API call 이터레이션마다 증가 (line 2963)
if (self._skill_nudge_interval > 0
        and "skill_manage" in self.valid_tool_names):
    self._iters_since_skill += 1

# 트리거 (line 2595)
if (self._skill_nudge_interval > 0
        and self._iters_since_skill >= self._skill_nudge_interval
        and "skill_manage" in self.valid_tool_names):
    _should_review_skills = True
```

**"복잡한 태스크" 임계값 = iteration count** (기본 10). 각 tool call (search/read/bash…)이 카운트 증가. 10+ 이터가 skill_manage 호출 없이 일어나면 nudge.

**리뷰 프롬프트** (lines 2267-2280):
```python
_SKILL_REVIEW_PROMPT = (
    "Review the task just completed. Did you solve something reusable? "
    "Create a new skill if the solution is genuinely useful and complex enough. "
    "Only save if there's something worth reusing. If nothing stands out, say 'Nothing to save.'"
)
```

**유저 승인 없이** 리뷰 에이전트가 skill store에 직접 씀.

### 11.3 스킬 slash 커맨드의 cache-safe 주입

**검증** — cli.py line 6220:
```python
elif base_cmd in _skill_commands:
    user_instruction = cmd_original[len(base_cmd):].strip()
    msg = build_skill_invocation_message(
        base_cmd, user_instruction, task_id=self.session_id
    )
    if msg:
        self._pending_input.put(msg)  # ← 유저 메시지로 큐잉
```

**활성화 노트 포맷** (skill_commands.py line 454):
```python
activation_note = (
    f'[SYSTEM: The user has invoked the "{skill_name}" skill, indicating they want '
    "you to follow its instructions. The full skill content is loaded below.]"
)
```

**메시지 플로우**:
1. 유저가 `/writing-plans "build a feature"` 입력
2. `build_skill_invocation_message()` 가 `writing-plans/SKILL.md` 로드
3. `[SYSTEM: ...]` 헤더 + 유저 instruction 으로 wrap
4. `_pending_input` 큐에 전체 메시지 put
5. `process_loop()` 가 큐에서 consume
6. **일반 유저 메시지로 API에 전송**
7. 모델은 유저-제공 컨텍스트로 봄, system-injected 아님

**캐시 안전성**: 스킬 내용은 호출마다 달라지지만(유저가 다른 instruction), **activation note는 스킬당 정적**. 프롬프트 캐싱이 user/system 메시지 분리하므로 스킬 가변 내용이 시스템 캐시 오염 안 함.

### 11.4 Insights (`agent/insights.py` 39KB)

```python
class InsightsEngine:
    def generate(self, days=30, source=None):
        sessions = self._get_sessions(cutoff, source)
        tool_usage = self._get_tool_usage(cutoff, source)
        skill_usage = self._get_skill_usage(cutoff, source)
        return {
            "overview": {...},
            "models": [...],
            "platforms": [...],
            "tools": [...],
            "skills": {"summary": {...}, "top_skills": [...]},
            "activity": {...},
            "top_sessions": [...]
        }
```

`/insights [--days N] [--source PLATFORM]` 으로 surface. 터미널 포맷 테이블(토큰 수, 비용, 툴 분포, 스킬 로드/편집, 활동 트렌드).

---

## 12. 메모리 프로바이더

### 12.1 ABC (`agent/memory_provider.py`)

라이프사이클: `initialize()` → `prefetch(query)` (per-turn) → `sync_turn(turn_messages)` (per-turn) → `shutdown()`. 옵션으로 `post_setup(hermes_home, config)` (setup-wizard 통합).

### 12.2 프로바이더 비교표

| 프로바이더 | 저장 모델 | 검색 전략 | 지연 | 설정 |
|---|---|---|---|---|
| **honcho** | 그래프 + 벡터 (Honcho 클라우드) | semantic + dialectic multi-pass | 500-2000ms | `honcho.json` |
| **mem0** | 벡터 DB (Mem0 클라우드) | semantic + keyword 하이브리드 | 300-800ms | `.env` (API key) |
| **supermemory** | 벡터 (self-hosted or cloud) | semantic | 200-600ms | `~/.supermemory/config.json` |
| **byterover** | 텍스트 청크 (로컬 SQLite) | keyword + BM25 랭킹 | <100ms | `~/.byterover/config.yaml` |
| **hindsight** | 로컬 파일 기반 요약 | 시간적 윈도잉 + 요약 | <50ms | config 파일 |
| **holographic** | 확률적 (sketching) | approximate + exact 하이브리드 | <200ms | 코드 기본값 |
| **openviking** | 벡터 + 메타데이터 (로컬) | semantic + 메타데이터 필터링 | 50-300ms | config YAML |
| **retaindb** | 구조화 triples (로컬) | 쿼리 기반 retrieval | <100ms | config 파일 |
| **빌트인 (always)** | `MEMORY.md` + `USER.md` (로컬 파일) | 파일 읽기 | <10ms | 유저 편집 가능 |

### 12.3 Honcho의 "Dialectic" (Plastic Labs)

```
depth=1 (single) — 콜드/웜 프롬프트 쿼리만
depth=2 — Pass 0 audit → Pass 1 synthesis
depth=3 — Pass 0 audit → Pass 1 synthesis → Pass 2 reconciliation
```

**Cold vs Warm 프롬프트** (자동 선택):
- **Cold (첫 세션)**: "Who is this person? What are their preferences, goals, working style?"
- **Warm (후속)**: "Given what's been discussed, what context about this user is most relevant now?"

**통합**: 베이스 컨텍스트 + dialectic supplement → API 호출 시점에 user message 내부로 주입 (프롬프트 캐시 유지). `<memory-context>` fence + 시스템 노트.

**"제안/도전" 메커니즘 아님** — iterative synthesis: Pass 0 사실 수집, Pass 1 종합/감사, Pass 2 모순 화해.

### 12.4 Context Engine vs Memory Provider

- **Memory Provider**: **세션 간 지속 recall**. 플러그인화 필수.
- **Context Engine**: **단일 턴 내 동적 컨텍스트 조립** (세션 요약, 토큰 예산, 관련도 랭킹).

둘의 rationale: memory는 cross-session이므로 pluggable, context engine은 per-session이므로 더 긴밀히 결합.

---

## 13. 세션 DB & FTS5 검색

### 13.1 스키마 (`hermes_state.py` lines 100-119)

```sql
CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
    content,
    content=messages,        -- 외부 컨텐츠 테이블
    content_rowid=id         -- join key
);

CREATE TRIGGER messages_fts_insert AFTER INSERT ON messages BEGIN
    INSERT INTO messages_fts(rowid, content) VALUES (new.id, new.content);
END;
```

`messages` 테이블이 모든 턴 저장, `messages_fts`는 `content` 컬럼만 미러(full-text 인덱싱). 트리거가 동기화.

### 13.2 검색 API (lines 1164-1245)

```python
def search_messages(query, source_filter=None, exclude_sources=None, ...):
    query = self._sanitize_fts5_query(query)  # quotes, boolean ops, 프리픽스 핸들
    
    sql = """
        SELECT ... snippet(messages_fts, 0, '>>>', '<<<', '...', 40) AS snippet ...
        FROM messages_fts
        JOIN messages m ON m.id = messages_fts.rowid
        JOIN sessions s ON s.id = m.session_id
        WHERE messages_fts MATCH ? ...
        ORDER BY rank
    """
```

**LLM 요약 경로**: FTS5 경로에 **없음**. 검색은 raw 메시지 + snippet 반환. "LLM summarization for cross-session recall"이 일어나면 memory provider (Honcho/Hindsight 등)의 `prefetch()` 콜에서 발생, 빌트인 검색 경로에서는 아님.

### 13.3 빌트인 스킬 카테고리 (25)

`apple`, `autonomous-ai-agents`, `creative`, `data-science`, `devops`, `diagramming`, `dogfood`, `domain`, `email`, `feeds`, `gaming`, `gifs`, `github`, `index-cache`, `inference-sh`, `mcp`, `media`, `mlops`, `note-taking`, `productivity`, `red-teaming`, `research`, `smart-home`, `social-media`, `software-development`

**예시 1: `software-development/test-driven-development/SKILL.md`**:
```yaml
name: test-driven-development
description: >
  Use when implementing any feature or bugfix, before writing implementation code.
  Enforces RED-GREEN-REFACTOR cycle with test-first approach.
version: 1.1.0
metadata:
  hermes:
    tags: [testing, tdd, development, quality, red-green-refactor]
    related_skills: [systematic-debugging, writing-plans, subagent-driven-development]
```

**예시 2: `software-development/writing-plans/SKILL.md`**:
```yaml
name: writing-plans
description: >
  Use when you have a spec or requirements for a multi-step task.
version: 1.1.0
metadata:
  hermes:
    tags: [planning, design, implementation, workflow, documentation]
```

둘 다 **프롬프트 엔지니어링 스킬** — 모델 행동을 가이드하는 instructional markdown, 실행 코드 아님.

### 13.4 Optional 스킬 카테고리 (16)

`autonomous-ai-agents`, `blockchain`, `communication`, `creative`, `devops`, `dogfood`, `email`, `health`, `mcp`, `migration`, `mlops`, `productivity`, `research`, `security`, `web-development`

`hermes skills install official/<category>/<skill>` 로 설치. `tools/skills_hub.py`의 `OptionalSkillSource`.

---

## 14. CLI 아키텍처

### 14.1 HermesCLI 라이프사이클 (`cli.py` 11,096 LOC)

**초기화** (lines 1802-2108):
- `~/.hermes/cli-config.yaml` 로드 (lines 298-644)
- 모델/프로바이더 우선순위: CLI args > config file > env vars > defaults
- prompt_toolkit Application 초기화, 세션 DB (SQLite `state.db`), 대화 이력, 스피너 상태
- 리소스 클린업 훅 (`_run_cleanup()`, lines 719-759): 터미널, 브라우저, MCP 서버 정리
- git worktree 격리 (#652)

**REPL 루프** (`process_loop`):
- prompt_toolkit 입력 + custom `SlashCommandCompleter` + `SlashCommandAutoSuggest`
- `process_command()` (line 5873) 디스패치, `False` 반환 시 exit

**Teardown**:
- `atexit` 훅이 `_run_cleanup()` 호출 → 터미널/브라우저/MCP 서버/메모리 프로바이더 닫기

### 14.2 COMMAND_REGISTRY (75개 슬래시 명령)

**Session (19)**: `/new`(reset), `/clear`, `/history`, `/save`, `/retry`, `/undo`, `/title`, `/branch`(fork), `/compress`, `/rollback`, `/snapshot`(snap), `/stop`, `/approve`(gw), `/deny`(gw), `/background`(bg), `/btw`, `/agents`(tasks), `/queue`(q), `/steer`, `/status`, `/profile`, `/sethome`(gw), `/resume`

**Configuration (12)**: `/config`, `/model`, `/provider`, `/gquota`, `/personality`, `/statusbar`(sb), `/verbose`, `/yolo`, `/reasoning`, `/fast`, `/skin`, `/voice`

**Tools & Skills (8)**: `/tools`, `/toolsets`, `/skills`, `/cron`, `/reload`, `/reload-mcp`, `/browser`, `/plugins`

**Info (11)**: `/commands`(gw), `/help`, `/restart`(gw), `/usage`, `/insights`, `/platforms`(gateway), `/copy`, `/paste`, `/image`, `/update`(gw), `/debug`

**Exit (1)**: `/quit`(exit)

**Gateway Config Gates**: `/verbose`는 `gateway_config_gate="display.tool_progress_command"` — 해당 config key truthy일 때만 활성화.

**Fallback 메커니즘** (lines 6169-6271):
1. Quick Commands (config.yaml `quick_commands` section)
2. Plugin Commands (`get_plugin_command_handler()`)
3. Skill Commands (`_skill_commands` 레지스트리)
4. Prefix Matching (`/upd` → `/update` if unambiguous)

### 14.3 `_apply_profile_override()` (hermes_cli/main.py:99-158)

**실행 타이밍**: 모든 모듈 import 전. main.py 최상단 (line 160).

**알고리즘**:
1. `sys.argv` pre-parse: `-p PROFILE` or `--profile=PROFILE`
2. Fallback: `~/.hermes/active_profile` 파일 읽기
3. `profiles.resolve_profile_env(profile_name)` → HERMES_HOME 경로
4. `os.environ["HERMES_HOME"]` set
5. sys.argv에서 플래그 제거 (argparse가 못 보도록)

**Why pre-import?** Profile이 config 경로(`~/.hermes/` vs `~/.hermes/alt-profile/`)를 결정하므로 config.py / plugins.py / auth.py import 전에 set 필수.

### 14.4 Setup Wizard Flow (`hermes_cli/setup.py` lines 2869-3029)

1. **권한 체크** — managed mode (CI/env) 면 실패
2. **환경 감지** — TTY 없으면(SSH/Docker/CI) 가이드 출력 후 종료
3. **섹션 체크** — `hermes setup model` 처럼 특정 섹션 요청이면 바로 디스패치
4. **기존 설치 자동 감지**:
   - OPENROUTER_API_KEY / OPENAI_BASE_URL / active_provider 체크
   - 있으면 "Returning User Menu": Quick Setup / Full Setup / 개별 섹션 / Exit
5. **SETUP_SECTIONS** (Provider/Model, TTS, Terminal, Gateway, Tools, Agent): 각 섹션 핸들러 콜 → config 저장 → 성공 메시지

주요 핸들러:
- `setup_inference_provider()` — 프로바이더 선택, API 키 입력
- `setup_terminal_backend()` — local/ssh/docker/modal…
- `setup_gateway_platforms()` — Telegram/Discord/Slack 활성화
- `setup_tools()` — 터미널/브라우저/MCP

### 14.5 모델/프로바이더 카탈로그

**파일**: `providers.py`, `models.py`, `model_switch.py`, `codex_models.py`, `model_normalize.py`

**`_PROVIDER_MODELS` 생성**:
- OpenRouter: API fetch 또는 하드코딩 fallback
- Anthropic/OpenAI: 하드코딩 (claude-opus, gpt-4-turbo 등)
- Nous/Minimax/기타: 프로바이더 감지

**모델 별칭**:
- `DIRECT_ALIASES` — config-defined (config.yaml `model_aliases`)
- `MODEL_ALIASES` — 빌트인 카탈로그 (opus, sonnet, haiku, gpt4, gpt35…)
- `MODEL_FAMILY_DEFAULTS` — 패밀리별 기본 (anthropic → claude-sonnet, openai → gpt-4-turbo)

**마이그레이션** (model_normalize.py):
- Stale 모델명 감지 (claude-2.1 → claude-3.5-sonnet)
- config deprecated 모델 경고
- `/model` 명령에서 대체 제안

**OpenRouter provider_routing**: `sort`(cost/latency), `only`(whitelist), `ignore`(blacklist), `order`, `require_parameters`.

### 14.6 OpenClaw 마이그레이션 (`claw.py`)

`hermes claw migrate [--dry-run|--yes|--preset PRESET|--overwrite]`

**스크립트 위치**: `optional-skills/migration/openclaw-migration/scripts/openclaw_to_hermes.py`

**전송 항목**:
- API 키/토큰 (Telegram/Discord/Slack/OpenAI)
- 모델 preferences
- 툴 설정
- Custom 스킬 `~/.openclaw/skills/ → ~/.hermes/skills/`
- Todo 리스트, memory, 세션 이력 (가능한 경우)

**사전 체크**:
- OpenClaw 프로세스 실행 중 경고 (systemd, pgrep)
- Hermes gateway 활성 플랫폼 경고
- 이유: Telegram/Discord/Slack은 토큰당 하나의 활성 연결만 허용

**Cleanup** (`hermes claw cleanup`):
- 남은 OpenClaw 디렉터리(.openclaw, .clawdbot, .moltbot) 타임스탬프 아카이브

### 14.7 Display: KawaiiSpinner + Activity Feed (`agent/display.py` 39KB)

**KawaiiSpinner** (display.py:573-722):
```python
SPINNERS = {
    'dots': ['⠋', '⠙', ...],
    'bounce': [...],
    'grow': ['▁', '▂', ...],
    'brain': ['🧠', '💭', ...],
    'sparkle': ['⁺', ...],
}

KAWAII_WAITING = ["(｡◕‿◕｡)", "(◕‿◕✿)", ...]      # 11 faces
KAWAII_THINKING = ["(｡•́︿•̀｡)", "(◔_◔)", ...]    # 16 faces
THINKING_VERBS = ["pondering", "contemplating", ...]  # 15 verbs
```

**상태 머신**:
1. `__init__` — stdout 캡처 (redirect_stdout 전)
2. `start()` — 애니메이션 스레드 스폰
3. `_animate()` — `sleep(0.1)`, frame_idx 증가, `\r` + frame write
4. `stop()` — running=False, join, 완료 메시지

**Smart output**:
- **TTY 감지** — 파이프/Docker/systemd면 애니메이션 skip, 메시지 1회만
- **prompt_toolkit 감지** — `patch_stdout` 내부면 skip (CLI가 `_spinner_text` TUI 위젯으로 렌더링 중이므로 중복 방지)

**Tool Activity Feed** (display.py:170-276): `build_tool_preview(tool_name, args, max_len)`:

| 툴 | 프리뷰 예 |
|---|---|
| terminal | `ls -la /tmp` |
| web_search | `python best practices` |
| read_file | `/etc/config.yaml` |
| patch | `main.py` |
| browser_navigate | `https://github.com` |
| image_generate | `sunset over mountains` |
| vision_analyze | `what's in this image?` |
| memory | `+notes: "learned about..."` |
| session_search | `recall: "previous solution"` |
| delegate_task | `write unit tests` |

Max length 설정 가능: `display.tool_preview_length` (0=무제한).

### 14.8 숨겨진 주요 서브커맨드

| 명령 | 용도 | 파일 |
|---|---|---|
| `/dump` | 세션 상태 JSON export | — |
| `/backup` | `~/.hermes` 백업 | `backup.py` |
| **`/doctor`** | 진단 (키, 버전, 연결) | `doctor.py` **58KB** |
| `/uninstall` | Hermes 제거 | `uninstall.py` |
| `/nous_subscription` | Nous API 구독 한도 체크 | `nous_subscription.py` |
| `/logs` | `agent.log`/`errors.log` tail | `logs.py` |

**Doctor** (58KB): Python 버전/의존성 체크, config.yaml 문법 검증, API 크레덴셜 테스트(더미 콜), 터미널 백엔드 연결(SSH/Docker), MCP 서버 config, 메모리 프로바이더, 플러그인/스킬 리스트, 전체 진단 리포트.

---

## 15. TUI (Ink/React + Python JSON-RPC)

### 15.1 프로세스 모델

```
hermes --tui
  └─ Node (Ink)  ──stdio JSON-RPC──  Python (tui_gateway)
       │                                  └─ AIAgent + tools + sessions
       └─ 트랜스크립트/컴포저/프롬프트/activity 렌더링
```

**TypeScript**: 화면 소유. **Python**: 세션/툴/모델콜/슬래시 로직 소유. **Newline-delimited JSON-RPC over stdio**.

### 15.2 TUI JSON-RPC 메서드 (66개)

**세션 관리** (13): `session.create`, `session.list`, `session.resume`, `session.title`, `session.usage`, `session.history`, `session.undo`, `session.compress`, `session.save`, `session.close`, `session.branch`, `session.interrupt`, `session.steer`

**Input/Output** (5): `prompt.submit`, `prompt.background`, `prompt.btw`, `clipboard.paste`, `image.attach`, `input.detect_drop`

**Approvals & Modals** (4): `clarify.respond`, `sudo.respond`, `secret.respond`, `approval.respond`

**Configuration** (3): `config.set`, `config.get`, `config.show`

**Commands** (4): `cli.exec`, `command.resolve`, `command.dispatch`, `slash.exec`

**Tools & Models** (8): `tools.list`, `tools.show`, `tools.configure`, `toolsets.list`, `model.options`, `complete.path`, `complete.slash`

**Cron & Plugins** (3): `cron.manage`, `plugins.list`, `skills.manage`

**Voice & Browser** (4): `voice.toggle`, `voice.record`, `voice.tts`, `browser.manage`

**Misc** (10+): `process.stop`, `reload.mcp`, `insights.get`, `rollback.list/restore/diff`, `agents.list`, `shell.exec`, `spawn_tree.save/list/load`, `delegation.status/pause`, `subagent.interrupt`, `terminal.resize`, `setup.status`, `commands.catalog`, `paste.collapse`

**Long handlers async 스레드 풀**: `/cli.exec`, `/session.branch`, `/session.resume`, `/shell.exec`, `/skills.manage`, `/slash.exec` — 느린 명령 실행 시 RPC dispatcher 블로킹 방지.

### 15.3 TUI 이벤트 (emit)

`status.update`, `session.info`, `tool.start/complete/progress/generating`, `thinking.delta`, `reasoning.delta/available`, `message.delta/complete`, `error`, `approval.request`, `clarify.request`, `sudo.request`, `secret.request`

### 15.4 TUI 컴포넌트 (주요)

- `app.tsx` — 상태 머신, 이벤트 핸들러, 슬래시 핸들러 (`app/event-handler`, `app/slash-handler`, `app/stores`, `app/hooks`로 decompose, PR #14640)
- `messageLine.tsx`, `thinking.tsx`, `prompts.tsx`, `sessionPicker.tsx`, `maskedPrompt.tsx`
- `branding.tsx`, `markdown.tsx`
- Hooks: `useCompletion`, `useInputHistory`, `useQueue`, `useVirtualHistory`
- Sticky composer (스크롤 중 고정), OSC-52 클립보드, 가상화 이력 렌더링 (성능)

---

## 16. 스킨 엔진

`hermes_cli/skin_engine.py` (427 LOC).

### 16.1 SkinConfig 데이터클래스

```python
@dataclass
class SkinConfig:
    name: str
    description: str
    colors: Dict[str, str]       # 40+ 색 키 (hex)
    spinner: Dict[str, Any]      # waiting_faces, thinking_faces, thinking_verbs, wings
    branding: Dict[str, str]     # agent_name, welcome, goodbye, response_label, prompt_symbol
    tool_prefix: str             # "┊"
    tool_emojis: Dict[str, str]  # per-tool 이모지 오버라이드
    banner_logo: str             # Rich-markup ASCII
    banner_hero: str             # Rich-markup hero art
```

### 16.2 빌트인 스킨 (6종)

- `default` — 클래식 골드/카와이
- `ares` — Crimson/bronze + 커스텀 wings
- `mono` — 그레이스케일
- `slate` — 쿨 블루
- `daylight` — 라이트 테마
- `warm-lightmode` — 브라운/골드 (라이트 터미널용)

### 16.3 해석 순서

1. User `~/.hermes/skins/<name>.yaml`
2. 빌트인 `_BUILTIN_SKINS` dict
3. Default (`config.yaml` `display.skin`)

### 16.4 40+ 색 키

banner: `border`, `title`, `accent`, `dim`, `text`
UI: `ui_accent`, `ui_label`, `ui_ok`, `ui_error`, `ui_warn`
기타: `prompt`, `input_rule`, `response_border`, `status_bar_bg`, `status_bar_text`, `status_bar_strong`, `status_bar_dim`, `status_bar_good`, `status_bar_warn`, `status_bar_bad`, `status_bar_critical`, `session_label`, `session_border`, `voice_status_bg`, `completion_menu_bg`, …

### 16.5 Spinner 커스터마이징

- `waiting_faces` — API 응답 대기 중
- `thinking_faces` — 리즈닝 중 (다른 세트)
- `thinking_verbs` — 액션 동사 ("forging", "marching"…)
- `wings` — 좌/우 장식 `[["⟪⚔", "⚔⟫"], ...]`

### 16.6 왜 Curses UI로 전환?

AGENTS.md의 "Known Pitfalls": `simple_term_menu` 가 tmux/iTerm2에서 화살표 키에 유령 중복 렌더링 버그. 신규 인터랙티브 메뉴는 curses(`hermes_cli/curses_ui.py`) 사용. 참고 패턴: `hermes_cli/tools_config.py`.

---

## 17. Gateway & 메시징 플랫폼

### 17.1 Gateway Run 루프 (`run.py` 1927-2268)

**`start()` 오케스트레이션**:
- 플랫폼 발견 & 연결 (2072-2175): 설정된 플랫폼 iterate, `_create_adapter()`, `adapter.connect()` 순차
- 메시지 핸들러 배선 (2083-2086): 각 adapter 콜백 등록
  - `set_message_handler(self._handle_message)` — 수신 메시지 디스패치
  - `set_fatal_error_handler(self._handle_adapter_fatal_error)` — 플랫폼 크래시
  - `set_busy_session_handler(...)` — 인터럽트 처리

**메시지 디스패치 파이프라인** (`_handle_message` lines 3131-3400+):
1. **Authorization** (3149-3190): 유저 allowlist 검증 or DM pairing 코드 플로우 트리거
2. **Session lookup** (3196): `_session_key_for_source(source)` → unique session key
3. **Interrupt handling** (3229-3308): stale/hung 에이전트 TTL로 감지 후 축출. `/stop`, `/reset`, `/new` 우선 라우팅
4. **Agent cache lookup** (664-676): `OrderedDict[(AIAgent, config_signature)]`, 128 엔트리 캡, LRU + idle TTL

### 17.2 TWO GUARDS 문제

**Guard 1: Adapter Level** (`base.py` 909-912):
```python
self._active_sessions: Dict[str, asyncio.Event] = {}
self._pending_messages: Dict[str, MessageEvent] = {}
self._session_tasks: Dict[str, asyncio.Task] = {}
```
- per-session `asyncio.Event`로 동시 처리 블록
- 같은 채팅에서 두 메시지가 동시에 `_message_handler`로 들어오지 않음
- **Interception**: `_active_sessions[session_key]` set 되어 있으면 `_pending_messages` 에 큐잉

**Guard 2: Gateway Runner Level** (`run.py` 656-662):
```python
self._running_agents: Dict[str, Any] = {}
self._running_agents_ts: Dict[str, float] = {}
self._session_run_generation: Dict[str, int] = {}
self._pending_messages: Dict[str, str] = {}
```
- **Eager interrupt** (3281-3310): 유저 텍스트에 `/stop` 포함되면 내부 예외로 즉시 kill
- **Stale eviction** (3234-3279): `agent.get_activity_summary()` idle timeout 기본 1800s + wall-clock TTL 최대 2h

**플로우**: Adapter queue → Gateway `_handle_message`가 stale agent evict → 새 AIAgent 생성 → 응답 → Adapter가 `_active_sessions[key]` 해제.

### 17.3 Base Adapter ABC (`base.py` 882-1181)

**Abstract 계약**:
```python
@abstractmethod
async def connect(self) -> bool: ...
@abstractmethod
async def disconnect(self) -> None: ...
@abstractmethod
async def send(self, chat_id, content, reply_to=None, metadata=None) -> SendResult: ...
```

**큐 시맨틱**:
- `_pending_messages[session_key]` = 최신 `MessageEvent` (LIFO override, FIFO append 아님)
- `_active_sessions[session_key]` = `asyncio.Event()` (핸들러 완료까지 held)
- Gateway `_running_agents[key]` = async setup 동안 sentinel, 실제 AIAgent로 교체

**Media** (선택, 텍스트 fallback):
- `send_image_file(chat_id, image_path, caption)`
- `send_voice(chat_id, audio_path)`
- `send_document(chat_id, file_path, caption)`

### 17.4 Canonical Pattern: Telegram (`telegram.py`)

```python
class TelegramAdapter(BasePlatformAdapter):
    MAX_MESSAGE_LENGTH = 4096
    MEDIA_GROUP_WAIT_SECONDS = 0.8
    
    def __init__(self, config):
        super().__init__(config, Platform.TELEGRAM)
        self._app = None  # python-telegram-bot Application
        self._pending_photo_batches = {}  # photo burst coalesce
        self._pending_text_batches = {}   # client-side split coalesce
```

**Token Lock** (`status.py` 464-551 `acquire_scoped_lock`):
- scope = `"telegram_bot"`, identity = bot token
- Machine-local 락 파일: `~/.local/state/hermes/gateway-locks/{scope}-{hash(identity)}.lock`
- `(acquired: bool, existing_owner_record: dict)` 반환
- Stale PID check (`/proc/{pid}/stat`)
- 두 gateway 프로세스가 같은 봇 토큰 사용 방지 → "already in use" 에러

### 17.5 플랫폼 매트릭스

| 플랫폼 | 전송 | 인증 | 음성 | 비디오 | 스티커 | 리액션 | 스레드 |
|---|---|---|---|---|---|---|---|
| **Telegram** | Polling/Webhook | Bot token | Memo (OGG) | MP4 | Native | ✓ | Forum topics |
| **Discord** | WebSocket (d.py) | Bot token | Native (Opus) | MP4 | Emoji only | ✓ | Thread channels |
| **Slack** | RTM/Bolt | Token+secret | 파일 only | 파일 only | ✓ | ✓ | Thread_ts |
| **Matrix** | HTTP + **E2EE** | Homeserver URL + user/pass | OGG | MP4 | ✓ | ✓ | Room topics |
| **Signal** | libsignal | Phone number | Native | 파일 | ✗ | ✗ | Groups only |
| **WhatsApp** | Webhook | Meta token | OGG | MP4 | Media templates | ✓ | No |
| **WeChat** | Webhook | API token | AMR | MP4 | ✓ | ✓ | No |
| **QQBot** (v0.11) | QQ Official API v2 | Token | — | — | — | ✓ (emoji) | — |
| **Feishu** | Webhook | App credential | — | 파일 | — | — | Document comments |
| **DingTalk** | Stream | App credential | — | — | — | — | — |
| **Mattermost** | WebSocket | Personal token | — | — | — | ✓ | Threads |
| **Home Assistant** | WebSocket | Long-lived token | — | — | — | — | — |
| **Email** | SMTP/IMAP | Credentials | 첨부 | 첨부 | — | — | Thread-id |
| **SMS** | HTTPGateway | API key | — | — | — | — | — |
| **BlueBubbles** | Mac bridge | API key | — | — | — | — | — |
| **api_server** | HTTP + OpenAI-호환 | Token | — | — | — | — | — |
| **webhook** | HTTP POST | — | — | — | — | — | — |

**음성 파이프라인 차이**:
- **Discord**: 음성 채널 Native Opus 프레임 → `send_voice()` Opus bytes
- **Telegram**: OGG memo 다운로드 → 로컬 캐시 → STT 전사 → TTS 오디오 응답

### 17.6 Matrix E2E

- 암호화 키 `~/.hermes/matrix/` 계정별 SQLite Megolm 저장
- 수신 메시지: `message.decrypted_body` (libolm 복호화 후)
- 송신: Matrix client lib이 보내기 전 암호화
- **Critical**: cron delivery (scheduler.py 405-439)가 live adapter path 사용 — standalone HTTP client는 E2EE 방 복호화 불가

### 17.7 ADDING_A_PLATFORM.md 계약 (16 통합 지점)

1. Adapter 클래스 + `connect/disconnect/send/send_typing`
2. `Platform` enum + `config.py` env var loader
3. `gateway/run.py` adapter factory + allowlist 맵
4. 필요 시 `SessionSource` 확장
5. `agent/prompt_builder.py` `PLATFORM_HINTS` (에이전트 인식)
6. `toolsets.py` 이름있는 toolset + gateway composite
7. `scheduler.py` cron `platform_map`
8. `tools/send_message_tool.py` 메시지 전송 라우팅
9. Channel directory session-based discovery
10. `hermes_cli/status.py` status display
11. `hermes_cli/gateway.py` setup wizard
12. `agent/redact.py` 리다액션 패턴
13. 문서 (README, env vars, setup guide)
14. 유닛 테스트 `tests/gateway/test_<platform>.py`
15. (기타 2개)

**Key insight**: 하나라도 빠지면 gateway startup(factory), cron delivery(platform_map), agent awareness(PLATFORM_HINTS) 중 하나가 깨짐.

---

## 18. Cron 스케줄러

### 18.1 저장 & 실행

**Jobs 저장** (`cron/jobs.py`):
- Per-job JSON + SQLite 인덱스 `~/.hermes/cron/jobs.db`
- 메타데이터: schedule, deliver, origin, prompt, skills, model override
- Next run: `croniter` + `parse_schedule()`

**Delivery 경로** (`scheduler.py` 300-483 `_deliver_result`):
```python
targets = _resolve_delivery_targets(job)  # "telegram:123456" or "origin"
for target in targets:
    platform = platform_map[target["platform"]]
    adapter = (adapters or {}).get(platform)
    if adapter and loop.is_running():
        future = asyncio.run_coroutine_threadsafe(
            adapter.send(chat_id, content, metadata={"thread_id": thread_id}),
            loop
        )
        send_result = future.result(timeout=60)
    else:
        # Fallback: standalone HTTP + config
        result = asyncio.run(_send_to_platform(platform, pconfig, ...))
```

**미디어 추출** (365-367, 434-435): `BasePlatformAdapter.extract_media(content)` → `[MEDIA: /path/to/file]` 태그 찾기. 정리된 텍스트 + 미디어 파일을 별도 네이티브 첨부로 전송.

**스레드 보존** (383-397): origin이 `thread_id` 있으면 대상 플랫폼 같은 스레드로 시도. 로그 경고.

### 18.2 cronjob_tools.py 에이전트 인터페이스

```python
def cronjob(
    action: str,  # "create"|"list"|"pause"|"resume"|"trigger"|"delete"|"update"
    job_id: Optional[str] = None,
    prompt: Optional[str] = None,
    schedule: Optional[str] = None,  # "daily", "0 9 * * *", ...
    name: Optional[str] = None,
    repeat: Optional[int] = None,
    deliver: Optional[str] = None,  # "origin"|"local"|"telegram"|"telegram:123456"
    skill: Optional[str] = None,
    skills: Optional[List[str]] = None,
    model: Optional[str] = None,
    provider: Optional[str] = None,
    script: Optional[str] = None,  # pre-run script 경로
    enabled_toolsets: Optional[List[str]] = None,
) -> str:
```

**위협 스캐닝** (lines 60-68):
- 주입 패턴 차단: `ignore.*previous.*instructions`, `system.*prompt.*override`
- Exfiltration: `curl|wget` + `${KEY|TOKEN|SECRET}`
- Destructive: `rm -rf /`
- 보이지 않는 유니코드 (U+200B–U+202E)

**Origin 캡처** (lines 71-88): 에이전트가 환경에서 현재 세션 origin 자동 캡처 — `HERMES_SESSION_PLATFORM`, `HERMES_SESSION_CHAT_ID`, `HERMES_SESSION_THREAD_ID`. `deliver="origin"` 의 기본값.

### 18.3 Scheduler 동시성

- 게이트웨이가 백그라운드 스레드에서 60초마다 `tick()` 호출
- 파일 락 `~/.hermes/cron/.tick.lock` — 여러 프로세스 겹쳐도 한 번만 실행 (fcntl/msvcrt)
- `_jobs_file_lock` (threading.Lock) — 병렬 스레드로 tick 실행 시 `load_jobs→modify→save_jobs` 경쟁 방지

---

## 19. ACP (IDE 통합)

### 19.1 프로토콜

- `agent-client-protocol` 라이브러리 (import as `acp`)
- `acp.Agent` 서브클래스 (`HermesACPAgent`)
- 스트리밍: `AvailableCommandsUpdate`, 세션 상태, 모델 선택

### 19.2 지원 IDE

- VS Code (ACP extension)
- Zed (빌트인 ACP)
- JetBrains
- 모든 MCP/ACP 호환 IDE

### 19.3 메시지 포맷 (lines 81-99)

- 혼합 컨텐츠 블록 수용: `TextContentBlock`, `ImageContentBlock`, `AudioContentBlock`
- 평문 텍스트 추출, 비텍스트 블록은 로깅만 (처리 안 함)

### 19.4 세션 관리

- 세션당 AIAgent 인스턴스
- `model`, `provider`, `available_models` 트래킹 (UI 모델 선택기)
- fork/load/resume 시맨틱 지원

### 19.5 Slash Commands (lines 104-144)

`_ADVERTISED_COMMANDS`: `/help`, `/model`, `/tools`, `/context`, `/reset`, `/compact`, `/version`

---

## 20. RL 연구 인프라 (Atropos/Tinker)

### 20.1 목표

> "training the next generation of tool-calling models"

**흐름**: 데이터 수집 → 트래젝토리 압축 → Atropos/Tinker 훈련 루프

1. **데이터 수집**: `batch_runner.py` + `environments/*` 가 tool-calling 트래젝토리 생성 (reward signal 포함 agent loop)
2. **트래젝토리 압축**: `trajectory_compressor.py` 가 토큰 예산(예: 29K)에 맞춤 (middle-region 요약)
3. **훈련**: `tinker-atropos/` 서브모듈 (Tinker RL 트레이너)가 per-token advantage로 모델 훈련

### 20.2 Atropos 통합

**`atroposlib`** (`environments/hermes_base_env.py` 50-55):
- 서버 관리 (VLLM, SGLang, OpenAI-호환 API)
- 병렬 롤아웃 워커 스케줄링
- WandB 메트릭 로깅
- CLI: `serve`, `process`, `evaluate`
- `evaluate_log()` eval 결과 기록

**`HermesAgentBaseEnv`** (221-280) — BaseEnv 확장:
- `os.environ["TERMINAL_ENV"]` 설정으로 백엔드 라우팅 (258-265)
- 툴셋 해석 `_resolve_tools_for_group()` (289-299) — `tools/registry.py` 쿼리
- `collect_trajectory()` 전체 agent loop 실행 + reward 계산

**Two-phase operation**:
- **Phase 1 (OpenAI server)**: `server.chat_completion()` with `tools=` — 서버가 tool call 네이티브 파싱. SFT + evaluation 용도.
- **Phase 2 (VLLM server)**: ManagedServer `/generate` + 클라이언트측 tool call 파서 → **정확한 토큰 IDs + logprobs**. 풀 RL 훈련용.

Concrete 환경들:
```python
async def compute_reward(self, item, result, ctx: ToolContext):
    test = ctx.terminal("pytest -v")
    return 1.0 if test["exit_code"] == 0 else 0.0
```

### 20.3 Tinker & OPD

**Tinker**: thinking-machines-lab/tinker, 커밋 `30517b66...` pinned. Atropos-생성 트래젝토리 훈련용.

**Locked fields** (`rl_training_tool.py` 72-100):
```python
LOCKED_FIELDS = {
    "tinker": {
        "lora_rank": 32,
        "learning_rate": 0.00004,
        "max_token_trainer_length": 9000,
        ...
    }
}
```

**`agentic_opd_env.py`** — **OPD = On-Policy Distillation** (OpenClaw-RL, Princeton 2026):
- 각 tool response에 모델의 **이전 응답이 개선될 수 있는 방식**에 대한 hindsight 포함
- (assistant_turn, next_state) 쌍 walk
- LLM judge로 next-state 신호(테스트 verdict, 에러 trace, 툴 결과)에서 "힌트" 추출
- Per-token advantage 계산: `A_t = teacher_logprob(token_t) - student_logprob(token_t)`
- **Dense, token-level training signal** (end-of-trajectory scalar reward 아님)

Reference: Wang et al., "OpenClaw-RL: Train Any Agent Simply by Talking" (arXiv:2603.10165, March 2026).

### 20.4 Agent Loop Engine (`environments/agent_loop.py`)

**`HermesAgentLoop`** (재사용 가능 멀티턴 엔진):
1. 메시지 + 툴 → API via `server.chat_completion()`
2. `tool_calls` 있으면 각 `handle_function_call()` 을 스레드 풀에서 실행
3. 툴 결과 append 후 계속
4. `tool_calls` 없으면 종료

**스레드 풀 안전성** (27-46): Modal/Docker/Daytona 백엔드가 내부적으로 `asyncio.run()` 호출 → Atropos event loop 내에서 돌면 데드락. 해결: 전용 `ThreadPoolExecutor` (기본 128 워커, `resize_tool_pool()` 로 조정) — 백엔드에 clean event loop 제공.

**AgentResult** (64-78): `messages, managed_state, turns_used, finished_naturally, reasoning_per_turn, tool_errors`.

### 20.5 Tool Call Parsers

**목적** (`tool_call_parsers/__init__.py`): Phase 2 (VLLM ManagedServer)가 구조화된 tool_calls 대신 **raw 텍스트** 반환. 클라이언트 측 파서가 모델-특이 포맷에서 구조화된 콜 추출.

**레지스트리** (62-105): 10개 파서 — hermes, mistral, llama3_json, qwen, qwen3_coder, deepseek_v3, deepseek_v3_1, kimi_k2, longcat, glm45, glm47

**포맷 예**:
- **Hermes**: `<tool_call>{"name": "func", "arguments": {...}}</tool_call>`
- **Mistral**: `[TOOL_CALLS]...`
- **Qwen**: Qwen-특이 JSON
- **DeepSeek V3**: DeepSeek tool-calling 컨벤션

각 파서는 **standalone 재구현** (VLLM 의존 없음), stdlib(`re`, `json`, `uuid`) + OpenAI types.

**훈련 통합**: 트레이너가 모든 tool-call 포맷을 OpenAI `ChatCompletionMessageToolCall` 스키마로 정규화 → 통합 loss 계산.

### 20.6 Tool Context (`environments/tool_context.py`)

Reward function용 per-rollout 핸들:

**메서드**:
- Terminal: `ctx.terminal(command, timeout)`
- Files: `ctx.read_file()`, `ctx.write_file()`, `ctx.search()`
- Transfers: `ctx.upload_file()`, `ctx.download_file()`
- Web: `ctx.web_search()`, `ctx.web_extract()`
- Browser: `ctx.browser_navigate()`, `ctx.browser_snapshot()`
- Generic: `ctx.call_tool(name, args)`
- Cleanup: `ctx.cleanup()` — 자동 리소스 해제

**Task scoping**: 같은 `task_id` = 같은 터미널/브라우저 세션 → verifier가 모델이 남긴 정확한 state에서 검사 가능.

**스레드 풀 안전성** (40-63 `_run_tool_in_thread`): `asyncio` 컨텍스트 감지. 안에 있으면 ThreadPoolExecutor, 아니면 직접 콜.

---

## 21. 배치 러너 & 트래젝토리 컴프레서

### 21.1 Batch Runner (`batch_runner.py` 1,291 LOC)

**아키텍처**:
- **Multiprocessing Pool** (30-33): 병렬 배치 처리
- **Checkpointing** (`test_batch_runner_checkpoint.py`): 원자적 JSON 체크포인트 + `last_updated`, `--resume` 플래그
- **에러 recovery** (233-249 `_process_single_prompt`): try-except, per-prompt 에러 로깅
- **툴 stats 추출** (114-194 `_extract_tool_stats`): 툴별 success/failure 카운트, `ALL_POSSIBLE_TOOLS` 로 정규화 스키마
- **Reasoning stats** (197-230): `<REASONING_SCRATCHPAD>` 또는 native reasoning 필드 있는 assistant 턴 카운트
- **Global worker config** (48): 프로세스 간 `_WORKER_CONFIG` dict 공유

**출력**: JSONL, ShareGPT-유사 (from/value) 페어. trajectory_compressor와 호환.

### 21.2 Trajectory Compressor (`trajectory_compressor.py` 1,508 LOC)

**용도**: **훈련 데이터를 토큰 예산에 맞춤** (예: 40K 토큰 트래젝토리를 15K에).

**전략** (8-14):
1. First 턴 protect: system, human, first gpt, first tool
2. Last N 턴 protect (기본 4 — 최종 액션 보존)
3. Middle 턴만 압축 (두 번째 tool response부터)
4. 압축 영역을 단일 human summary 메시지로 교체
5. 남은 tool call 그대로 (모델이 summary 후 계속 작업)

**Config** (82-179, `trajectory_compression.yaml`):
- `target_max_tokens`: 15,250–29,000
- `summary_target_tokens`: 750
- `protected_turns`: first_system, first_human, first_gpt, first_tool, last_n_turns
- `summarization_model`: `"google/gemini-3-flash-preview"` (fast/cheap)
- `num_workers`: 4-8
- `max_concurrent_requests`: 50 (OpenRouter 병렬)

**Metrics** (182-224):
- `compression_ratio`, `tokens_saved`, `turns_removed`
- `turns_compressed_start_idx/end_idx`
- `summarization_api_calls`, `summarization_errors`

요약은 OpenRouter API + retry (`jittered_backoff` from `agent.retry_utils`).

### 21.3 context_compressor vs trajectory_compressor

| 축 | context_compressor | trajectory_compressor |
|---|---|---|
| **목적** | In-session runtime; 라이브 컨텍스트 축소해 대화 지속 | Post-session offline; 토큰 예산 내 훈련 데이터 준비 |
| **트리거** | Auto (preflight/API error) or manual (/compress) | 배치 처리 스크립트 (`--input=data/`) |
| **보존** | 메시지 role/content 멀티모달 블록; tool call intact | 보호된 first/last 턴만; middle은 단일 summary 턴 |
| **Summary** | 재실행 시 반복 업데이트 | 원패스 생성 |
| **출력** | 다음 API 호출에 재사용 (라이브) | JSONL 트래젝토리 (훈련/eval) |
| **토큰 예산** | 기본 context_length의 50% | 고정 타겟 (15,250 기본) |

**코드 차이**:
- context_compressor (line 282): "Summarize middle turns with structured LLM prompt" — 세션 계속되면 summarizer 반복 실행
- trajectory_compressor (lines 331-341): "Replace compressed region with a single human summary message" — 원패스, 정적 파일 출력

### 21.4 mini_swe_runner.py

**외부 SWE-bench 평가용 런타임** (내부 벤치 아님). `batch_runner` / `trajectory_compressor` 와 호환 트래젝토리 생성.

vs. `hermes_swe_env`:
- `hermes_swe_env`: Atropos 환경 (훈련 루프 통합)
- `mini_swe_runner`: Standalone (외부 벤치 제출 어댑터)

```bash
python mini_swe_runner.py --prompts_file prompts.jsonl --output_file trajectories.jsonl --env docker
```

Local/Docker/Modal 지원. Hermes 트래젝토리 포맷 (`<tool_call>`/`<tool_response>` XML).

### 21.5 RL Training Tool (`tools/rl_training_tool.py`)

**에이전트가 자기 런타임에서 RL 훈련 시작**. 노출 함수:
- `rl_list_environments()` — 훈련 환경 발견
- `rl_select_environment()` — 활성 환경 전환
- `rl_edit_config()` — 잠금 해제된 필드 수정
- `rl_test_inference()` — 훈련 전 설정 검증
- `rl_start_training()` — 훈련 서브프로세스 launch
- `rl_check_status()` — WandB 메트릭 폴링
- `rl_stop_training()` — 런 종료
- `rl_get_results()` — 최종 메트릭

**Locked fields** (72-100): 인프라 튜닝 (lora_rank=32, learning_rate=0.00004, max_token_trainer_length=9000) 모델이 수정 불가 → 안정 훈련.

환경 발견은 **AST-based scanning** — tinker-atropos 서브디렉터리에서 BaseEnv 서브클래스 찾기.

**함의**: **자기개선 훈련 루프** — 에이전트가 롤아웃 실패 관찰 → 훈련 config 자동 조정 → 재훈련 → 한 세션 내 갱신된 모델 테스트.

### 21.6 RL CLI (`rl_cli.py` 446 LOC)

```bash
python rl_cli.py "Train a model on GSM8k for math reasoning"
python rl_cli.py --interactive
python rl_cli.py --list-environments
```

**System prompt** (113-174) RL-focused:
1. 환경 DISCOVER: `rl_list_environments`
2. 환경 파일 INSPECT (verifier, dataset loading)
3. 새 환경 CREATE (템플릿 복사 + 수정)
4. CONFIGURE: `rl_select_environment`, `rl_edit_config`
5. 풀 훈련 전 TEST inference
6. `rl_start_training` + `rl_check_status`로 TRAIN 모니터
7. WandB 메트릭으로 EVALUATE

**확장 타임아웃**: `RL_MAX_ITERATIONS = 200` (일반 Hermes ~30 턴 대비) — 장기 RL 워크플로우.

**서브모듈 경로 설정** (41-52): `TERMINAL_CWD` 를 tinker-atropos 서브모듈로 → 에이전트 컨텍스트가 RL-focused.

### 21.7 Datagen Config Examples

1. **`trajectory_compression.yaml`** (102줄): post-processing config — tokenizer, protection rules, summarization API, metrics
2. **`web_research.yaml`** (47줄): WebResearchEnv config (web/file toolsets, compression targets)
3. **`example_browser_tasks.jsonl`**: 샘플 브라우저 자동화 태스크
4. **`run_browser_tasks.sh`**: 쉘 스크립트 러너

`web_research.yaml`:
- 환경: web-research (FRAMES 벤치 Google 2024)
- Toolsets: web, file
- Compression: enabled, target 16K
- Eval: every 100 trajectories, 25-question 샘플
- Output: `data/web_research_v1/`

---

## 22. 인프라 (Docker/Nix/설치)

### 22.1 Dockerfile

```dockerfile
FROM ghcr.io/astral-sh/uv:0.11.6-python3.13-trixie@sha256:... AS uv_source
FROM tianon/gosu:1.19-trixie@sha256:... AS gosu_source
FROM debian:13.4

ENV PYTHONUNBUFFERED=1
ENV PLAYWRIGHT_BROWSERS_PATH=/opt/hermes/.playwright

RUN apt-get install -y --no-install-recommends \
    build-essential nodejs npm python3 ripgrep ffmpeg gcc python3-dev \
    libffi-dev procps git openssh-client docker-cli

RUN useradd -u 10000 -m -d /opt/data hermes  # 비루트 UID 10000
COPY --chmod=0755 --from=gosu_source /gosu /usr/local/bin/
COPY --chmod=0755 --from=uv_source /usr/local/bin/uv /usr/local/bin/uvx /usr/local/bin/

WORKDIR /opt/hermes

# npm + Playwright 캐시 레이어
COPY package.json package-lock.json ./
COPY web/package.json web/package-lock.json web/
RUN npm install --prefer-offline --no-audit && \
    npx playwright install --with-deps chromium --only-shell && \
    (cd web && npm install --prefer-offline --no-audit) && \
    npm cache clean --force

COPY --chown=hermes:hermes . .
RUN cd web && npm run build  # Vite → hermes_cli/web_dist/

RUN chown hermes:hermes /opt/hermes
USER hermes
RUN uv venv && uv pip install --no-cache-dir -e ".[all]"

ENV HERMES_WEB_DIST=/opt/hermes/hermes_cli/web_dist
ENV HERMES_HOME=/opt/data
ENV PATH="/opt/data/.local/bin:${PATH}"
VOLUME [ "/opt/data" ]
ENTRYPOINT [ "/opt/hermes/docker/entrypoint.sh" ]
```

포인트:
- **Multi-stage build**: uv + gosu는 별도 스테이지에서 바이너리만 복사
- **Layer caching**: `package.json` 만 먼저 복사 → npm install 캐시. `.dockerignore` 가 `node_modules` 제외
- **Non-root UID 10000**: `hermes` 유저
- **HERMES_HOME=/opt/data** + VOLUME → 영구 데이터는 볼륨 마운트
- **Playwright**: `/opt/hermes/.playwright` (볼륨 밖, 재빌드 때 유지)
- **gosu**: UID를 런타임에 바꾸는 용도 (entrypoint.sh에서 `HERMES_UID` override)

### 22.2 Nix (flake.nix)

```nix
{
  description = "Hermes Agent";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = { ... };
    pyproject-nix = { ... };     # pyproject.toml 파싱
    uv2nix = { ... };             # uv.lock → Nix
    pyproject-build-systems = { ... };
    npm-lockfile-fix = { ... };
  };
  outputs = inputs: inputs.flake-parts.lib.mkFlake { ... } {
    systems = [ "x86_64-linux" "aarch64-linux" "aarch64-darwin" ];
    imports = [
      ./nix/packages.nix        # Derivations
      ./nix/nixosModules.nix    # NixOS 모듈 (systemd 서비스 등)
      ./nix/checks.nix           # `nix flake check`
      ./nix/devShell.nix         # `nix develop`
    ];
  };
}
```

nix/ 하위 파일:
- `packages.nix` — 바이너리 derivation
- `nixosModules.nix` — 시스템 서비스 모듈 (NixOS 서버 배포)
- `checks.nix` — CI 체크
- `devShell.nix` — 개발 환경
- `lib.nix`, `python.nix`, `tui.nix`, `web.nix`, `configMergeScript.nix` — 빌드 헬퍼

### 22.3 설치 스크립트 (`scripts/install.sh`)

- `curl -fsSL ... | bash` 원라이너 지원
- **non-interactive 모드 감지**: `if [ -t 0 ]` — `read -p` 가 EOF로 silent abort 방지 fix
- OS 감지: Linux / macOS / WSL2 / Termux (Android)
- uv 자동 설치 + Python 3.11 venv + `.[all]` 설치
- `~/.local/bin/hermes` 심볼릭 링크 (Termux는 `$PREFIX/bin`)
- Options: `--no-venv`, `--skip-setup`, `--branch`, `--dir`, `--hermes-home`

### 22.4 Homebrew 포뮬러

`packaging/homebrew/` — macOS 공식 패키징.

### 22.5 프로파일 지원 (`hermes_constants.py`)

```python
def get_hermes_home() -> Path:
    """HERMES_HOME env var 또는 ~/.hermes"""
    val = os.environ.get("HERMES_HOME", "").strip()
    return Path(val) if val else Path.home() / ".hermes"

def get_default_hermes_root() -> Path:
    """프로파일/도커/커스텀 배포 대응 루트"""
    # native_home (~/.hermes)이면 그대로
    # profile 모드 (<root>/profiles/<name>) 면 <root> 반환
    # Docker (예: /opt/data) 면 env_path 직접

def get_subprocess_home() -> str | None:
    """{HERMES_HOME}/home/ 이 존재하면 서브프로세스의 HOME으로 사용"""
    # git, ssh, gh, npm 등이 HERMES_HOME 안에 config 쓰도록
    # Docker 영속성 + Profile 격리
```

**Special helpers**:
- `is_termux()` — `TERMUX_VERSION` or Termux `PREFIX` 체크
- `is_wsl()` — `/proc/version` 에서 "microsoft" 검사 (캐시)
- `is_container()` — `/.dockerenv`, `/run/.containerenv`, `/proc/1/cgroup` 검사 (캐시)
- `apply_ipv4_preference(force=True)` — 브로큰 IPv6 환경 우회. `socket.getaddrinfo` 를 monkey-patch해서 `AF_UNSPEC` → `AF_INET`

### 22.6 로깅 (`hermes_logging.py`)

- `~/.hermes/logs/agent.log` (INFO+) / `errors.log` (WARNING+) / `gateway.log` (INFO+, gateway component only)
- `RotatingFileHandler` + `RedactingFormatter` (secrets 리다액션)
- **세션 컨텍스트** — `set_session_context(session_id)` → 스레드-로컬, 모든 로그 레코드에 `[session_id]` 태그
- **Managed mode** (NixOS setgid 2770): `_ManagedRotatingFileHandler` 가 `_open()` + `doRollover()` 후 `chmod 0o660` 강제
- 3rd-party 노이즈 suppressor: openai, httpx, httpcore, asyncio, hpack, grpc, modal, urllib3, websockets, charset_normalizer, markdown_it → WARNING

컴포넌트 필터 (gateway.log):
```python
COMPONENT_PREFIXES = {
    "gateway": ("gateway",),
    "agent": ("agent", "run_agent", "model_tools", "batch_runner"),
    "tools": ("tools",),
    "cli": ("hermes_cli", "cli"),
    "cron": ("cron",),
}
```

### 22.7 유틸 (`utils.py`)

- **Atomic JSON/YAML write** — temp file + fsync + `os.replace`, 권한 비트 보존 (`mkstemp` 0600 → 원래 mode 복원)
- `safe_json_loads()` — `JSONDecodeError/TypeError/ValueError` → default
- `env_int()`, `env_bool()`
- **Proxy 정규화** — `socks://` → `socks5://` (WSL/Clash 호환)
- **base_url_hostname**: `urlparse` 로 정확한 호스트명 추출 (substring 매칭 금지)
- **base_url_host_matches**: `hostname == domain or hostname.endswith("." + domain)` — 서브도메인만 매치, `evil.com/api.openai.com/v1` 같은 공격 경로 차단

---

## 23. 보안 정책 (SECURITY.md)

### 23.1 신뢰 모델

- **Single Tenant, Single Operator** — 멀티유저 격리는 OS/호스트
- **Gateway Security**: Telegram/Discord/Slack 등 인가된 호출자는 **모두 동등 신뢰**. 세션 키는 라우팅용, 인증 경계 아님.
- **Execution**: 기본 `terminal.backend: local` (직접 호스트 실행). 컨테이너 격리 opt-in.

### 23.2 위험 명령 승인

- `approvals.mode: "on" | "auto" | "off"`
- `"on"` (기본) — 유저에게 프롬프트
- `"auto"` — 설정 지연 후 자동 승인
- `"off"` — 완전 해제 (break-glass)

### 23.3 출력 리다액션

`agent/redact.py` 가 API 키/토큰/크레덴셜 패턴을 **디스플레이 레이어에서** strip — 채팅 로그/툴 프리뷰/응답 텍스트 누출 방지. 내부 에이전트 동작에는 원본 보존.

### 23.4 Skills vs MCP Servers 신뢰 등급

- **Installed Skills**: 높은 신뢰. 로컬 호스트 코드와 동급. env var 읽고 임의 명령 실행 가능.
- **MCP Servers**: 낮은 신뢰. `_build_safe_env()` (`tools/mcp_tool.py`)가 안전한 기본 변수(`PATH`, `HOME`, `XDG_*`) + 서버 `env` 블록에 명시된 것만 전달. 호스트 크레덴셜 기본 strip. **npx/uvx는 OSV 멀웨어 DB 체크**.

### 23.5 Code Execution 샌드박스

`execute_code` 툴 (`tools/code_execution_tool.py`): LLM 생성 Python을 **child 프로세스**에서 실행, API 키/토큰 strip. 로드된 스킬의 `env_passthrough` 선언 또는 `config.yaml` `terminal.env_passthrough` 만 통과. child는 Hermes 툴에 **RPC로만** 접근.

### 23.6 Subagents

- **재귀 위임 없음**: `delegate_task` 가 자식에서 비활성화
- **깊이 제한**: `MAX_DEPTH = 2`
- **Memory isolation**: 자식은 `skip_memory=True`, 부모의 persistent memory provider 접근 불가. 부모는 task prompt + 최종 응답만 observation

### 23.7 Out of Scope

- **프롬프트 주입**: 승인 시스템/toolset 제한/컨테이너 샌드박스의 구체적 우회가 아닌 한 vulnerability 아님
- **공개 노출**: gateway를 VPN/Tailscale/방화벽 없이 공개 인터넷에 배포
- **Trusted State 접근**: `~/.hermes/`, `.env`, `config.yaml` 에 이미 쓰기 권한 있는 경우
- **Default 동작**: `terminal.backend: local` 로 호스트 실행 (기본)
- **Configuration trade-off**: `approvals.mode: "off"` 같은 break-glass
- **툴 레벨 읽기 제한**: 에이전트는 `terminal` 로 무제한 쉘 접근. 특정 툴(예: `read_file`)만 deny는 의미 없음 (`terminal` 동일 접근 허용하므로)

### 23.8 Deployment 하드닝

- **Production 샌드박싱**: 컨테이너 백엔드 (`docker`, `modal`, `daytona`)
- **File perms**: `chmod 600 ~/.hermes/.env`
- **네트워크**: gateway / API server 를 VPN/Tailscale/방화벽 없이 공개 금지. **SSRF 보호는 기본 활성** (모든 gateway adapters), redirect 검증. Note: 로컬 터미널 백엔드는 SSRF 필터링 없음 (operator 신뢰 환경).
- **스킬 설치**: Skills Guard (`tools/skills_guard.py`) 보고서 리뷰. Audit log `~/.hermes/skills/.hub/audit.log`.
- **MCP 안전**: OSV 멀웨어 체크 자동.
- **CI/CD**: GitHub Actions 전체 커밋 SHA 핀. `supply-chain-audit.yml` 이 `.pth` 파일 / 의심스러운 `base64`+`exec` 패턴 블록.

### 23.9 Disclosure

- **Coordinated**: 90일 창 또는 fix 릴리스 중 먼저
- **Communication**: GHSA 스레드 또는 security@nousresearch.com
- **Credits**: 릴리스 노트에 보고자 크레딧 (익명 요청 시 제외)

---

## 24. 핵심 설계 패턴 정리

### 24.1 프롬프트 캐시 신성화

Hermes 전체 아키텍처에서 **프롬프트 캐시는 불가침 영역**:
- 과거 컨텍스트 절대 변경 X (압축만 예외)
- 대화 중 toolset 절대 변경 X
- 메모리 리로드/시스템 프롬프트 재구성 대화 중 절대 X
- 슬래시 명령 중 시스템-프롬프트 state mutate하는 것은 **cache-aware** 해야 함 (기본: deferred invalidation = 다음 세션부터, opt-in `--now` 플래그)

이유: 캐시 깨지면 비용 급증. Anthropic 5분 TTL.

### 24.2 조건부 규칙 로딩 (AGENTS.md)

Claude Code의 프로젝트 규칙 패턴을 차용:
```
hermes-agent/
├── AGENTS.md           # 항상 필요한 핵심 규칙
└── .claude/rules/*.md  # 모듈별 조건부 (frontmatter globs 매치 시 로딩)
```

### 24.3 Dual-layer 메시지 시퀀싱

Gateway의 가장 미묘한 디자인:
- **Adapter layer**: 같은 채팅에서 동시 메시지 처리 방지
- **Gateway runner**: stale 에이전트 축출 + `/stop` eager interrupt

두 layer 모두 우회해야 하는 명령(예: 승인 프롬프트)은 **inline 디스패치** 해야 함 (`_process_message_background()` 경유 시 세션 라이프사이클 경쟁 위험).

### 24.4 Agent-level tool 가로채기

```
user message
  ↓
model tool_call ("memory")
  ↓
AIAgent._invoke_tool()
  ├─ "todo" → _todo_tool()                    ← 가로챔
  ├─ "memory" → _memory_tool()                 ← 가로챔
  ├─ memory_manager.has_tool(fn) → provider    ← 가로챔
  ├─ "delegate_task" → _dispatch_delegate_task ← 가로챔
  └─ else → handle_function_call() [registry]
```

registry dispatch 전에 에이전트 본인이 책임 지는 툴을 먼저 처리. **외부 메모리 프로바이더가 registry보다 우선순위 높음** — Honcho가 `honcho_search` 같은 툴 이름을 자기가 가로챔.

### 24.5 태스크 격리 (`task_id`)

모든 stateful 툴이 `task_id` 키:
- 터미널 세션 (local: snapshot 파일, Modal: 스냅샷 JSON, SSH: 연결)
- 브라우저 세션
- 파일 캐시
- 체크포인트
- delegate_task 서브에이전트

이 ID 덕분에 subagent가 **같은 파일시스템 상태에서** 부모의 실수를 복구할 수 있음.

### 24.6 지연 invalidation (Cache-aware 슬래시)

```
/skills install my-skill           # 다음 세션부터 적용 (cache 유지)
/skills install my-skill --now     # 지금 적용 (cache 깸)
```

이 패턴이 프로젝트 전반에 일관되게 적용됨.

### 24.7 플러그인 훅 체인

```
pre_tool_call (block veto) 
  → tool execute 
    → post_tool_call 
      → transform_tool_result (rewrite)
pre_llm_call (context inject)
  → API call
    → post_llm_call
```

플러그인이 각 훅 지점에서 개입 가능. 메모리 프로바이더, context engine, 이미지 gen 백엔드, 대시보드 탭이 모두 이 훅으로 확장.

### 24.8 Profile-safe 코드

```python
# GOOD
from hermes_constants import get_hermes_home
config_path = get_hermes_home() / "config.yaml"

# BAD — 프로파일 깨짐
config_path = Path.home() / ".hermes" / "config.yaml"
```

PR #3575가 5개 버그 수정. `get_hermes_home()` / `display_hermes_home()` 두 쌍 사용. 토큰 락으로 두 프로파일이 같은 크레덴셜 동시 사용 방지.

### 24.9 테스트 엄격성

- **`scripts/run_tests.sh` 필수** — 직접 pytest 호출 금지
- CI 환경 parity: API 키 unset, TZ=UTC, LANG=C.UTF-8, `-n 4` xdist (CI ubuntu-latest와 동일)
- `tests/conftest.py` autouse fixture로 ~/.hermes/ 리다이렉트
- **Change-detector 테스트 금지**: `assert "gemini-2.5-pro" in _PROVIDER_MODELS["gemini"]` 같은 스냅샷은 모델 릴리스마다 CI 깸. **Invariant** 만: `assert len(_PROVIDER_MODELS["gemini"]) >= 1`

### 24.10 안전한 URL 파싱

```python
def base_url_host_matches(base_url: str, domain: str) -> bool:
    hostname = base_url_hostname(base_url)
    return hostname == domain or hostname.endswith("." + domain)

# base_url_host_matches("https://evil.com/moonshot.ai/v1", "moonshot.ai") == False
# base_url_host_matches("https://moonshot.ai.evil/v1", "moonshot.ai") == False
```

`"moonshot.ai" in base_url` 같은 substring 검사는 공격자가 proxy/경로로 속이면 native endpoint로 잘못 라우팅. hostname 정확 매칭 + 서브도메인 규칙만.

### 24.11 원자적 파일 쓰기

```python
def atomic_json_write(path, data, *, indent=2, **dump_kwargs):
    original_mode = _preserve_file_mode(path)
    fd, tmp_path = tempfile.mkstemp(dir=str(path.parent), prefix=f".{path.stem}_", suffix=".tmp")
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as f:
            json.dump(data, f, indent=indent, ensure_ascii=False, **dump_kwargs)
            f.flush()
            os.fsync(f.fileno())
        os.replace(tmp_path, path)
        _restore_file_mode(path, original_mode)  # mkstemp 0600 → 원래 perms
    except BaseException:  # KeyboardInterrupt, SystemExit 까지
        try: os.unlink(tmp_path)
        except OSError: pass
        raise
```

temp + fsync + os.replace + 권한 복원. 프로세스 죽어도 이전 파일 intact.

### 24.12 백엔드 어떤 환경이든 동일 경험

```
scripts/install.sh — Linux/macOS/WSL2/Termux 동일 원라이너
Dockerfile — non-root UID 10000, 볼륨 /opt/data
flake.nix — NixOS systemd 서비스 포함 배포
brew install hermes — macOS 네이티브 (packaging/homebrew/)
hermes doctor — 어느 환경에서도 동일 진단
```

---

## 25. Deneb 프로젝트에의 시사점

### 25.1 직접 비교

| 축 | Deneb | Hermes |
|---|---|---|
| 언어 | Go (gateway-go) | Python |
| 타겟 | DGX Spark 단일 기계 | 5$ VPS / GPU 클러스터 / 서버리스 |
| UI | Telegram 전용 (안드로이드 S25 최적화) | Telegram/Discord/Slack/… 17+ 플랫폼 |
| 툴 | 150+ (내장) | 40+ 빌트인 + 스킬 + MCP + 플러그인 |
| LLM 통합 | 자체 pipeline | OpenAI-style unified protocol |
| 메모리 | vega (자체) | 8개 플러그인 + 빌트인 MEMORY.md/USER.md |
| 스킬 | 자체 skills/ | agentskills.io 공개 표준 호환 |
| 터미널 백엔드 | — | 6종 (local/docker/ssh/daytona/singularity/modal) |
| 자기개선 | 제한적 | 뉴지-기반 skill/memory review |
| 연구 | — | Atropos + Tinker + OPD |
| 철학 | **Narrow scope, deep quality**, Korean-first, single operator | Multi-platform breadth, MIT 오픈, 커뮤니티 확장 |

### 25.2 참고할 만한 패턴

1. **프롬프트 캐시 불가침 원칙** — Deneb이 현재 어느 정도 준수하고 있지만 명시적 문서화 + 슬래시 명령 cache-aware 패턴 도입 고려 ("지연 invalidation / `--now` 플래그")
2. **Agent-level tool 가로채기** — memory/todo/session_search 같이 에이전트 본인이 책임지는 툴을 registry 전에 인터셉트 (Deneb의 memory/vega 통합에 적용 가능)
3. **Skill을 user message로 주입** — system prompt 깨지 않는 cache-safe 패턴
4. **Dual-layer 메시지 시퀀싱** — gateway-go의 세션 관리에 참고할 가치 (adapter queue + runner interrupt 분리)
5. **`task_id` 기반 stateful 툴 격리** — Deneb의 멀티 세션 처리에 힌트
6. **원자적 config 쓰기** — `~/.deneb/` 의 config 업데이트 시 temp+fsync+replace 패턴
7. **에러 분류 FailoverReason enum** — 프로바이더 에러를 분류해 rotate/retry/compress 자동 선택
8. **크리덴셜 풀 선택 전략** — Deneb이 다중 API 키 사용한다면 round-robin/least-used/random/fill-first 중 선택
9. **Checkpoint Manager** — 파일 편집 시 자동 snapshot → `/rollback` 명령으로 복구 (유저 안전성)
10. **/steer 패턴** — 실행 중 에이전트에게 턴 중단 없이 노트 주입 (현재 Deneb에 없는 UX)
11. **Background Nudge Review** — N 이터레이션마다 forked 에이전트가 메모리/스킬 리뷰 (Korean-first로 번역만 맞추면 적용 가능)
12. **MCP OSV 멀웨어 체크** — MCP 서버 spawn 전 OSV 쿼리
13. **프로파일 격리** — `HERMES_HOME` 같은 환경 변수로 멀티 인스턴스 지원

### 25.3 Deneb이 Hermes보다 우월한 지점

- **단일 플랫폼 최적화** — Telegram MarkdownV2, 4096자 제한, inline keyboard, 50MB 미디어 제약을 수용한 **completeness**. Hermes는 breadth 우선 / depth는 plugin/skill에 위임
- **Korean-first** — i18n 프레임워크 없이 자연스러운 한국어. Hermes는 영어 중심
- **DGX Spark 특화** — 로컬 GPU 추론 우선, 외부 API 호출 최소화. Hermes는 provider 선택이 목표
- **Go 런타임** — Python 부팅 오버헤드 없음, 동시성 직관적 (goroutine/channel)

### 25.4 Deneb이 흡수하면 좋을 방향

- **컨텍스트 압축 전략** — head/tail protect + middle summarize + SUMMARY_PREFIX ("참고용 요약, 이 요약에 답하지 마라")
- **프로파일 시스템** — 개인 vs 업무 프로파일 분리 (기본 1 유저 전제지만 컨텍스트 분리용)
- **스킬 시스템** — agentskills.io 표준 호환 옵션 (예: `/test-driven-development` 같은 범용 스킬 자동 설치)
- **셸 훅** — 파이썬 플러그인 없이 쉘 스크립트를 lifecycle 훅에 바인딩
- **Checkpoint/Rollback** — 에이전트 파일 수정 시 자동 스냅샷
- **문서화 디스플린** — `AGENTS.md` + `.claude/rules/` 조건부 로딩 패턴이 이미 유사. Hermes가 훨씬 많은 규칙 + skill 문서화를 체계적으로 운영하는 법 참고

---

## 부록 A: 파일 참조표 (핵심)

| 영역 | 핵심 파일 | LOC |
|---|---|---:|
| 에이전트 루프 | `run_agent.py:698-1399 (__init__)`, `run_agent.py:8630-9520 (run_conversation)` | 12,174 |
| CLI | `cli.py:1794-... (HermesCLI)`, `cli.py:5873 (process_command)` | 11,096 |
| 슬래시 명령 레지스트리 | `hermes_cli/commands.py:59-175` | — |
| 컨텍스트 압축 | `agent/context_compressor.py` | ~1400 |
| 트래젝토리 압축 | `trajectory_compressor.py` | 1,508 |
| 배치 러너 | `batch_runner.py` | 1,291 |
| 세션 DB | `hermes_state.py:100-119 (FTS5)`, `1164-1245 (search)` | 1,591 |
| Anthropic 어댑터 | `agent/anthropic_adapter.py` | ~2000 |
| Bedrock | `agent/bedrock_adapter.py:61-82 (boto client)` | ~1200 |
| Gemini Native | `agent/gemini_native_adapter.py:136-155 (functionCall)` | ~900 |
| Codex | `agent/codex_responses_adapter.py:145-155 (fc_id)` | ~1050 |
| Credential Pool | `agent/credential_pool.py:89-170 (schema)`, `:728-756 (selection)` | ~1500 |
| Rate Limit | `agent/rate_limit_tracker.py`, `agent/nous_rate_guard.py` | ~400 |
| Error Classifier | `agent/error_classifier.py:24-57 (enum)`, `:289-474 (pipeline)` | ~900 |
| Redact | `agent/redact.py:62-174 (patterns)` | ~400 |
| Pricing | `agent/usage_pricing.py:47-66 (entry)`, `agent/account_usage.py`, `agent/model_metadata.py` | ~2500 |
| Tools Registry | `tools/registry.py:176-227 (register)`, `:191-213 (shadowing)` | ~700 |
| Tool Dispatch | `model_tools.py:477-608 (handle_function_call)` | 642 |
| Approval | `tools/approval.py:80-146 (patterns)` | ~800 |
| Tirith | `tools/tirith_security.py:615-691` | ~1000 |
| URL Safety | `tools/url_safety.py:78-97 (SSRF)` | ~300 |
| OSV | `tools/osv_check.py:26-62` | ~200 |
| Terminal Base | `tools/environments/base.py:267-763` | ~800 |
| Modal | `tools/environments/modal.py:147-150 (async worker)` | ~600 |
| MCP | `tools/mcp_tool.py` | ~1100 |
| Browser | `tools/browser_tool.py:70-93 (providers)`, `tools/browser_camofox.py:1-45` | ~800 |
| MoA | `tools/mixture_of_agents_tool.py:63-82 (models)` | ~400 |
| RL Training | `tools/rl_training_tool.py:72-100 (locked)` | ~500 |
| Delegate | `tools/delegate_tool.py:39-76 (blocked tools)` | ~400 |
| Memory Manager | `agent/memory_manager.py` | 14KB |
| Memory Provider | `agent/memory_provider.py` | 10KB |
| Insights | `agent/insights.py:93-173 (generate)` | 39KB |
| Skill Commands | `agent/skill_commands.py:429 (build_skill_invocation_message)`, `:454 (activation_note)` | 19KB |
| Gateway Run | `gateway/run.py:1927-2268 (start)`, `:3131-3400 (_handle_message)`, `:664-676 (cache)` | ~4000 |
| Base Adapter | `gateway/platforms/base.py:882-1181`, `:909-912 (guards)` | ~1200 |
| Telegram | `gateway/platforms/telegram.py:221-253 (init)` | ~2000 |
| Status Locks | `gateway/status.py:464-551 (acquire_scoped_lock)` | ~600 |
| Cron Scheduler | `cron/scheduler.py:300-483 (_deliver_result)` | ~800 |
| Cron Jobs | `cron/jobs.py` | ~600 |
| Cronjob Tool | `tools/cronjob_tools.py:60-68 (threat scan)`, `:71-88 (origin)`, `:223-241 (schema)` | ~400 |
| ACP | `acp_adapter/server.py:1-75 (server)`, `:81-99 (extract_text)`, `:104-144 (commands)` | ~800 |
| Pairing | `gateway/pairing.py:150-191 (code gen)`, `:250-283 (rate limit)` | ~400 |
| RL Env Base | `environments/hermes_base_env.py:50-55 (atropos)`, `:221-280 (subclass)`, `:258-265 (TERMINAL_ENV)` | ~400 |
| OPD | `environments/agentic_opd_env.py:1-59` | ~400 |
| Agent Loop | `environments/agent_loop.py:1-79`, `:27-46 (thread pool)`, `:64-78 (AgentResult)` | ~250 |
| Tool Context | `environments/tool_context.py:40-63 (thread safety)` | ~200 |
| TUI Gateway | `tui_gateway/server.py` | ~3000 |
| Display | `agent/display.py:170-276 (tool preview)`, `:573-722 (KawaiiSpinner)` | 39KB |
| 인프라 상수 | `hermes_constants.py` (profile helpers) | 295 |
| 로깅 | `hermes_logging.py` (세션 컨텍스트) | 390 |
| 유틸 | `utils.py` (atomic write, URL parse) | 271 |

## 부록 B: 내재 상수 & 설정 (주요)

```python
# run_agent.py
max_iterations = 90                     # 기본 툴콜 이터레이션
_memory_nudge_interval = 10             # 메모리 리뷰 턴 간격
_skill_nudge_interval = 10              # 스킬 리뷰 이터 간격

# tools/delegate_tool.py
MAX_DEPTH = 2                           # 서브에이전트 최대 깊이

# credential_pool.py
EXHAUSTED_TTL_429_SECONDS = 3600        # 429 cooldown 1시간

# pairing.py
ALPHABET = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
CODE_LENGTH = 8                         # ~33^8 ≈ 1.1×10^12
CODE_TTL_SECONDS = 3600                 # 1시간
RATE_LIMIT_SECONDS = 600                # 10분
# 5회 실패 → 1시간 lockout

# gateway/run.py
_agent_cache 캡 = 128 엔트리            # LRU
idle TTL 기본 = 1800s (30분)
wall-clock TTL 최대 = 2h

# tools/environments/base.py
Activity callback = 10초
Interrupt poll = 0.2초

# mixture_of_agents_tool.py
REFERENCE_TEMPERATURE = 0.6
AGGREGATOR_TEMPERATURE = 0.4
MIN_SUCCESSFUL_REFERENCES = 1

# rl_cli.py
RL_MAX_ITERATIONS = 200

# rl_training_tool.py (locked)
lora_rank = 32
learning_rate = 0.00004
max_token_trainer_length = 9000

# trajectory_compressor.py
target_max_tokens = 15250-29000
summary_target_tokens = 750
max_concurrent_requests = 50

# cronjob_tools.py
ONESHOT_GRACE_SECONDS = 120
```

---

## 부록 C: 분석 방법론

### 분석 파이프라인

1. **레포 클론** — GitHub `NousResearch/hermes-agent` (commit `c95c6bd`)
2. **Top-level 문서 읽기** — README.md (180줄), AGENTS.md (752줄), pyproject.toml (162줄), SECURITY.md (85줄)
3. **구조 맵 생성** — `ls -la`, `wc -l`, `find | wc`, 주요 디렉터리 inventory
4. **7개 Explore 에이전트 병렬 파견** (`very thorough` 설정, 각각 800-1500 단어 코드 레벨 분석 보고):
   - Agent A: 에이전트 코어 & 컨텍스트 파이프라인
   - Agent B: LLM 어댑터 & 크리덴셜/레이트
   - Agent C: 툴 & 보안 & 터미널 백엔드
   - Agent D: 메모리/스킬/인사이트 (self-improvement)
   - Agent E: CLI & TUI
   - Agent F: Gateway & 메시징 & Cron & ACP
   - Agent G: RL 환경 & 배치 & 트래젝토리
5. **직접 읽기** (에이전트 중복 피하면서 공백 메우기):
   - `hermes_constants.py`, `hermes_logging.py`, `utils.py`, `hermes_time.py` — 공통 인프라
   - `Dockerfile`, `flake.nix`, `scripts/install.sh`, `SECURITY.md` — 배포/보안
   - `RELEASE_v0.11.0.md` 헤드 — 최신 변경사항
   - `cron/scheduler.py`, `cron/jobs.py` 헤드 — cron 스토리지
   - `setup-hermes.sh` 헤드 — 개발자 셋업
6. **종합** — 이 보고서

### 에이전트 출력 요약

총 ~6,000 단어의 코드 레벨 분석 (7 에이전트 합산):
- 라인 번호 인용 500+개
- 3-5줄 코드 스니펫 100+개
- 데이터 구조 다이어그램 20+개
- 비교 테이블 15개
- 설계 결정 justification 50+개

### 한계

- **직접 읽은 Python 라인**: 약 1,200 라인 (인프라/공통)
- **에이전트 경유 파악한 Python 라인**: 약 30,000 라인 (코어 + 어댑터 + 툴 + gateway + RL)
- **읽지 않은 것**: 스킬 파일 본문, 웹 대시보드 TS/React 전체, 모든 테스트 본문, Docusaurus 문서, 모든 optional-skills, 모든 플러그인 세부 구현
- 이 보고서는 **구조와 설계 패턴** 위주. 특정 기능의 라인별 정확도는 추가 확인 필요한 경우 있음

---

*보고서 종료. 43MB / 2,310 파일 / 32,506 LOC 의 Hermes Agent v0.11.0 심층 분석 완료.*

**Generated**: 2026-04-24
**Analyst**: Claude Opus 4.7 (1M context) + 7 parallel Explore sub-agents
**Total analysis time**: ~10분 (병렬 실행)
