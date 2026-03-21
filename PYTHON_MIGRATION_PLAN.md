# Deneb TypeScript → Python 완전 마이그레이션 계획

## Context

Deneb는 621,137줄의 TypeScript로 작성된 멀티채널 AI 게이트웨이로, 7개 이상의 메시징 플랫폼(Telegram, Discord, Slack, LINE, Matrix, MS Teams, WhatsApp)을 지원합니다. 이를 Python으로 완전히 대체하여 Vega(기존 Python 프로젝트)와 통합된 단일 Python 생태계를 구축하는 것이 목표입니다.

**규모**: 4,705개 .ts 파일 / 50+ 모듈 / 6개 extension / 16개 skill

---

## 기술 스택 결정

| 영역 | 선택 | 이유 |
|------|------|------|
| Async | `asyncio` | 표준 라이브러리, FastAPI/aiogram 등 호환 |
| Web Framework | `FastAPI` + `Uvicorn` | 게이트웨이의 HTTP+WS 서버 대체, OpenAPI 자동 생성 |
| Type/Validation | `Pydantic v2` | Zod 30+ 스키마 파일 대체, JSON Schema 생성 |
| CLI | `Typer` | Commander.js 40+ 서브커맨드 대체 |
| Plugin System | `ABC` + `Protocol` + `entry_points` | 플러그인 계약 정의 + 동적 발견 |
| Build/Packaging | `uv` workspace | pnpm workspace 대체, 모노레포 지원 |
| Testing | `pytest` + `pytest-asyncio` | Vitest 대체, 70% 커버리지 유지 |
| Linting | `ruff` + `mypy` | oxlint + tsc 대체 |
| Logging | `structlog` | 구조화된 로깅 |
| Terminal UI | `rich` | chalk + ink 대체 |

---

## 핵심 의존성 매핑

| npm 패키지 | Python 대체 |
|------------|-------------|
| `grammy` (Telegram) | `aiogram` 3.x |
| `discord.js` | `discord.py` |
| `@slack/bolt` | `slack-bolt` |
| `@line/bot-sdk` | `line-bot-sdk` |
| `matrix-js-sdk` | `matrix-nio` |
| `@microsoft/agents-hosting` | `botbuilder-python` |
| `@modelcontextprotocol/sdk` | `mcp` (공식 Python SDK) |
| `@lancedb/lancedb` | `lancedb` (Python SDK가 더 성숙) |
| `openai` | `openai` (공식) |
| `@aws-sdk/client-bedrock` | `boto3` |
| `@opentelemetry/*` | `opentelemetry-*` |
| `playwright-core` | `playwright` |
| `sharp` | `Pillow` |
| `express` / `hono` | FastAPI |
| `zod` | Pydantic |
| `commander` | Typer |
| `chalk` | `rich` |
| `ws` | `websockets` |
| `chokidar` | `watchdog` |
| `croner` | `APScheduler` |
| `node-edge-tts` | `edge-tts` |
| `@lydell/node-pty` | `pexpect` / `ptyprocess` |
| `@whiskeysockets/baileys` (WhatsApp) | **위험** - Python 대체 없음. WhatsApp Cloud API 전환 또는 Node.js 사이드카 유지 |

---

## 프로젝트 구조

```
deneb-py/
├── pyproject.toml              # uv workspace root
├── src/
│   └── deneb/
│       ├── __init__.py
│       ├── cli/                # Typer 기반 CLI
│       │   ├── app.py          # 메인 Typer app
│       │   ├── commands/       # 서브커맨드 모듈들
│       │   └── deps.py         # DI 컨테이너
│       ├── config/             # Pydantic 설정 시스템
│       │   ├── schema.py       # DenebConfig 모델
│       │   ├── io.py           # JSON5/YAML 로딩
│       │   └── env.py          # 환경변수 처리
│       ├── infra/              # 공통 유틸리티
│       │   ├── errors.py       # DenebError 계층
│       │   ├── env.py          # RuntimeEnv
│       │   └── ...
│       ├── gateway/            # FastAPI 게이트웨이 서버
│       │   ├── server.py       # FastAPI app factory
│       │   ├── ws.py           # WebSocket 핸들링
│       │   ├── auth.py         # 인증 미들웨어
│       │   ├── methods/        # RPC 메서드들
│       │   └── openai_compat.py
│       ├── agents/             # 에이전트 런타임
│       │   ├── runtime.py      # LLM 상호작용
│       │   ├── tools/          # 에이전트 도구
│       │   ├── skills/         # 스킬 로딩
│       │   └── sandbox/        # 실행 샌드박스
│       ├── channels/           # 채널 어댑터 인터페이스
│       │   ├── base.py         # ChannelPlugin ABC
│       │   ├── adapters.py     # 어댑터 Protocol 정의
│       │   └── web/            # 웹 채널
│       ├── plugins/            # 플러그인 시스템
│       │   ├── types.py        # DenebPluginDefinition ABC
│       │   ├── loader.py       # 플러그인 로딩/발견
│       │   └── registry.py     # 레지스트리
│       ├── auto_reply/         # 자동 응답 파이프라인
│       ├── memory/             # 임베딩 + LanceDB
│       ├── cron/               # APScheduler 기반 스케줄러
│       ├── hooks/              # 이벤트 훅 시스템
│       ├── media/              # 미디어 처리
│       ├── browser/            # Playwright 브라우저
│       ├── sessions/           # 세션 관리
│       ├── tts/                # TTS
│       └── shared/             # 공유 타입/유틸
├── extensions/
│   ├── telegram/               # aiogram 기반
│   ├── discord/                # discord.py 기반
│   ├── slack/                  # slack-bolt 기반
│   ├── diagnostics_otel/       # OpenTelemetry
│   └── ...
├── skills/                     # 16개 스킬 포팅
└── tests/                      # pytest 미러 구조
```

---

## 마이그레이션 페이즈

### Phase 0: 프로젝트 셋업 (1-4주)

**목표**: Python 프로젝트 기반 구축

- [ ] `deneb-py/` uv workspace 생성 + `pyproject.toml` 설정
- [ ] 패키지 구조 생성 (`src/deneb/`)
- [ ] pytest + ruff + mypy CI 파이프라인 설정
- [ ] Dockerfile.python 작성
- [ ] 기존 `deneb.json5` 설정 파일 파싱 테스트 픽스처 준비

**핵심 파일**: `pyproject.toml`, CI 설정

---

### Phase 1: 코어 추상화 (5-12주)

**목표**: 모든 모듈이 의존하는 기반 레이어 포팅

**우선순위 순서:**

1. **`infra/`** → `deneb.infra`
   - 에러 계층 (`DenebError`, `ConfigError`, `ChannelError` 등)
   - 환경 유틸리티, 경로 처리, backoff, dedupe
   - 원본: `src/infra/` (3.8MB, 498 파일)

2. **`config/`** → `deneb.config`
   - Zod 스키마 30+개 → Pydantic BaseModel로 변환
   - JSON5/YAML 설정 로더
   - 환경변수 치환 (`env-substitution.ts`)
   - 원본: `src/config/` (2.1MB, 240 파일)

3. **`logging/`** → `deneb.logging`
   - structlog 기반 구조화 로깅

4. **`plugins/types.ts`** → `deneb.plugins.types`
   - `DenebPluginDefinition` ABC
   - `PluginRuntime` Protocol
   - 원본: `src/plugins/types.ts`

**검증**: 동일한 `deneb.json5`를 TS와 Python이 파싱하여 동일 결과 확인

---

### Phase 2: CLI + 게이트웨이 스켈레톤 (13-20주)

**목표**: 앱이 시작되고 HTTP 요청을 받을 수 있는 상태

1. **CLI** → Typer 앱
   - `deneb run` (게이트웨이 시작)
   - `deneb config` (설정 조회/수정)
   - `deneb status` (상태 확인)
   - `deneb models` (모델 목록)
   - 원본: `src/cli/program.ts` (2.3MB, 303 파일)

2. **게이트웨이 서버** → FastAPI
   - HTTP 엔드포인트 (health, probe, OpenAI 호환)
   - WebSocket 연결
   - 인증 미들웨어
   - 원본: `src/gateway/server.impl.ts` (3.8MB, 384 파일)

3. **세션 관리** → `deneb.sessions`
   - 세션 키 해석, 저장소

**검증**: `deneb run`으로 서버 기동, health endpoint 응답 확인

---

### Phase 3: 채널 어댑터 (21-32주)

**목표**: 메시징 플랫폼 연동 포팅 (Python 라이브러리 성숙도 순)

| 순서 | 채널 | Python 라이브러리 | 난이도 |
|------|------|-------------------|--------|
| 1 | Telegram | `aiogram` 3.x | 중 |
| 2 | Discord | `discord.py` | 중 |
| 3 | Slack | `slack-bolt` | 중 |
| 4 | Web/HTTP | 자체 WebSocket | 낮음 |
| 5 | Matrix | `matrix-nio` | 중 |
| 6 | LINE | `line-bot-sdk` | 낮음 |
| 7 | MS Teams | `botbuilder-python` | 중 |
| 8 | WhatsApp | Cloud API 전환 or Node 사이드카 | **높음** |
| 9 | 기타 (Signal, iMessage, IRC 등) | 개별 평가 | 가변 |

**각 채널 포팅 단위:**
- 채널 플러그인 (`ChannelPlugin` 구현)
- 어댑터 타입 (메시징, 인증, 설정, 페어링)
- 채널별 설정 스키마
- 채널별 CLI 커맨드
- 통합 테스트

---

### Phase 4: 에이전트 런타임 (33-44주) — 최대 난이도

**목표**: LLM 상호작용 + 도구 실행 + 스킬 시스템 포팅

**레이어별 포팅:**

1. **에이전트 스코프/경로** - 에이전트 식별 및 파일 경로
2. **인증 프로필** - 자격증명 관리
3. **스키마 시스템** - TypeBox → Pydantic `model_json_schema()`
4. **도구 시스템** - 에이전트 도구 정의 및 실행
5. **스킬 로더** - 스킬 발견 및 관리
6. **샌드박스** - 실행 격리
7. **LLM 런타임** - `@mariozechner/pi-*` 대체 → `litellm` 또는 직접 SDK 사용

**핵심 결정**: `@mariozechner/pi-*` TS 전용 패키지는 Python LLM 라이브러리(`openai`, `anthropic`, `litellm`)로 재작성. 이는 추상화 단순화의 기회.

원본: `src/agents/` (585 파일, 8.1MB — 전체의 최대 모듈)

---

### Phase 5: 지원 시스템 (45-52주)

| 모듈 | 난이도 | Python 대체/활용 |
|------|--------|-----------------|
| `auto-reply/` | 높음 | 메시지 처리 파이프라인 재구현 |
| `memory/` | 중 | LanceDB Python SDK (JS보다 성숙) |
| `cron/` | 중 | APScheduler |
| `hooks/` | 낮음 | 커스텀 이벤트 시스템 |
| `browser/` | 낮음 | Playwright Python (동일 API) |
| `media/` | 중 | Pillow + mutagen |
| `media-understanding/` | 중 | Pillow + PyMuPDF |
| `tts/` | 낮음 | edge-tts |
| `acp/` (MCP) | 중 | mcp Python SDK |

---

### Phase 6: Extension + 마무리 (53-60주)

- [ ] 나머지 extension 포팅 (diagnostics-otel, device-pair 등)
- [ ] 미포팅 CLI 커맨드 완료
- [ ] 16개 skills/ 포팅
- [ ] Vega 통합 (공유 인프라 모듈 정리)
- [ ] 성능 벤치마크 (TS 대비)
- [ ] Docker 이미지 업데이트
- [ ] 문서 업데이트
- [ ] TypeScript 코드 retire

---

## TypeScript → Python 패턴 매핑

| TypeScript | Python |
|------------|--------|
| `interface` / `type` | `Protocol` / Pydantic `BaseModel` |
| `enum` | `enum.StrEnum` |
| `zod` schema | Pydantic `BaseModel` + validator |
| `TypeBox` | `model_json_schema()` |
| `class implements Interface` | `class(ABC)` / `class(Protocol)` |
| `async/await` + `Promise.all` | `async/await` + `asyncio.gather` |
| `EventEmitter` | `blinker` signals / 커스텀 pub/sub |
| `Map<K, V>` / `Record<string, T>` | `dict[K, V]` |
| Discriminated unions | `Annotated[Union[...], Field(discriminator=...)]` |
| Generic `<T>` | `Generic[T]` + `TypeVar` |
| `vi.fn()` / `vi.mock()` | `pytest-mock` `mocker.patch()` |
| `child_process` | `asyncio.create_subprocess_exec` |
| Dynamic `import()` | `importlib.import_module()` |

---

## 공존 전략 (마이그레이션 기간)

1. **공유 설정**: 동일한 `deneb.json5`를 양쪽이 읽음
2. **HTTP 프록시**: TS 게이트웨이가 포팅 완료된 채널을 Python으로 프록시
3. **채널별 전환**: 설정으로 채널별 런타임 선택 (`runtime: "python" | "typescript"`)
4. **CLI 폴백**: 미포팅 커맨드는 TS CLI로 위임
5. **파일 기반 공유 상태**: SQLite, JSONL로 세션/메모리 공유 (프로세스 내 상태 공유 금지)

---

## 리스크 및 완화

| 리스크 | 영향 | 완화 |
|--------|------|------|
| WhatsApp (`baileys`) Python 대체 없음 | 높음 | WhatsApp Cloud API 전환 또는 Node.js 사이드카 유지 |
| `@mariozechner/pi-*` TS 전용 | 높음 | litellm/직접 SDK로 재작성 |
| 621K줄 규모의 마이그레이션 | 매우 높음 | 페이즈별 점진적 포팅 + 공존 기간 |
| Vega와의 패턴 불일치 | 낮음 | Pydantic은 상위 호환, Vega 점진적 업그레이드 가능 |
| 성능 차이 (Node.js vs Python) | 중 | uvloop + uvicorn으로 완화, I/O bound 특성상 차이 제한적 |

---

## 검증 계획

1. **단위 테스트**: pytest로 각 모듈별 테스트 (70% 커버리지 목표)
2. **계약 테스트**: 동일 설정 파일 → TS/Python 파싱 결과 비교
3. **통합 테스트**: `httpx.AsyncClient` + FastAPI TestClient
4. **채널 어댑터 테스트**: `respx` / `aioresponses`로 외부 API 모킹
5. **E2E 테스트**: 게이트웨이 시작 → 메시지 라우팅 → 채널 전달 전체 흐름
6. **호환성 회귀 테스트**: TS/Python 게이트웨이 동일 시나리오 실행 후 동작 비교
7. **성능 벤치마크**: 응답 지연, 처리량 TS 대비 측정
