# Deneb 개선 아이디어

**Status:** ideation / proposal backlog
**Audience:** Deneb 운영자 + 차기 AI 세션
**Scope:** 코드 위생, 성능, 신뢰성, 제품/UX, AI 능력 — 다섯 갈래.
**Methodology:** `gateway-go/` 전체 inventory + 최근 4.22.x CHANGELOG + `docs/agent-rules/` 도메인 규칙 + 기존 research 노트 (hermes-agent-analysis, tool-interception-gap) 교차 검토.

> **읽는 법.** 각 아이디어는 **무엇을 → 왜 → 어디서** 순으로 정리했다. 우선순위 (P0~P3) 와 추정 작업량 (S/M/L) 을 라벨로 붙였다. 채택 여부는 운영자 판단. 합의된 아이디어만 별도 PR 로 진행한다.

**상태·RSI 태그 범례 (2026-08 추가).**

- **상태 어휘(정규화):** `ideation` → `adopted`(합의, PR 진행) → `implemented`(완료). 본문의 기존 라벨은 이 어휘로 읽는다 — `P0~P3`/`S·M·L`은 우선순위·작업량, `DONE`/`✅ 구현됨` = `implemented`, `~~취소선~~`·`보류` = `blocked`/`superseded`, §8 `Out of Scope` = `rejected`.
- **RSI 태깅:** 각 아이디어는 북극성 `docs/research/recursive-self-improvement-roadmap.md`의 P1–P5 중 기여 단계로 태깅한다 (아래 영역 단위 매핑). 채택 시 상태를 `adopted`로 바꾸고, 결정의 시점별 근거는 `docs/adr/`에 기록한다.

| 영역 | RSI 단계 | 근거 |
|---|---|---|
| §1 코드 위생 | P1 (절차 외부화) | 책임 경계·진입점 정리가 다음 세션의 개선 절차를 외부화 |
| §2 성능 | P2 (slow loop) · P5 (효용 접지) | 캐시 대시보드·계측이 측정 기반 제공 |
| §3 신뢰성 | P1/P2 (검증) | 회귀 테스트 = slow loop의 결정적 bench |
| §4 제품/UX | P5 (수요 생성·재귀 표면) | cadence·trace·투명성이 수요·케이던스 생성 |
| §5 AI 능력 | P4 (스킬+도구 번들) | 신규 도구·스킬 번들 |
| §6 인프라/운영 | P2 · P5 | live-test·bench 케이던스 |

---

## 0. 한 줄 요약 (TL;DR)

| # | 아이디어 | P | 예상 작업량 |
|---|---|---|---|
| 1 | `run_exec.go` 책임 분리 (가이드라인 ~700 LOC; 분할 진행 중) | P1 | M |
| 2 | ~~`notify_relay.go` 3-way 분리~~ **DONE** (`notify_heartbeat.go` / `notify_status.go` 등) | P2 | ✅ |
| 3 | Polaris bootstrap & session-reopen 라운드트립 테스트 | P1 | S |
| 4 | CJK rune boundary 테스트 (`compaction/restore.go`) | P1 | S |
| 5 | Dreamer 단위 테스트 (`domain/wiki/dreamer.go`) | P2 | M |
| 6 | 슬래시 명령 디스커버리 (`/?`, 네이티브 클라 autocomplete 힌트) | P2 | S |
| 7 | 모닝 레터/이브닝 레터 통일된 cadence editor | P2 | M |
| 8 | Gmail 라벨/스레드 기반 우선순위 큐 | P3 | L |
| 9 | Tool histogram → 세션 종료 요약 카드 | P3 | S |
| 10 | Embedding-aware tool routing (LLM 호출 전 후보 도구 pre-filter) | P3 | L |
| 11 | Polaris semantic-anchor 압축 (Tier 3a MMR 개선) | P3 | M |
| 12 | DGX Spark GPU pool 헬스 페이지 (/health/gpu) | P2 | S |
| 13 | "왜 그 도구를 골랐는지" 1-line trace 로그 | P2 | S |
| 14 | 한국어 응답 품질 자동 회귀 (quality baseline → CI gate) | P1 | M |
| 15 | 네이티브 클라 "더보기/요약/원문" 액션셋 | P3 | M |
| 16 | 캐시 히트율 ops 대시보드 (`cache_read_input_tokens` 누적) | P2 | S |
| 17 | 멀티턴 컨텍스트 휘발 방지: per-session "pinned facts" 슬롯 | P3 | M |
| 18 | `executeAgentRun` 12개 파라미터 → struct 압축 | P2 | S |
| 19 | Skill SKILL.md schema lint + CI | P3 | S |
| 20 | 대용량 파일 전송 처리 (다운로드 링크 폴백) | P3 | M |
| 21 | Trust Inbox: 자율 변경 통합 승인 대기열 (안드로이드 워크피드) — ✅ 구현됨 | P1 | M |
| 22 | 개인 MCP 브로커 — tailnet 경유 읽기 전용 노출 — ✅ 구현됨 | P2 | M |
| 23 | 인물·거래 도시에 (Business Dossier) — ✅ 구현됨 | P2 | M |
| 24 | 주간 기억 다이제스트 + dreamer 피드백 루프 — ✅ 구현됨 | P2 | S |
| 25 | 드리머 팩트 원장 — 사실 단위 감사 로그 + 카운터 실측 — ✅ 구현됨 | P1 | M |
| 26 | 드리머 반증 증거 큐 — 사용자 정정의 비평 주입 — ✅ 구현됨 | P1 | M |
| 27 | 드리머 변경 기록 + 선택적 되돌리기 — ✅ 구현됨 | P2 | M |

---

## 1. 코드 위생 (Code Hygiene)

### 1.1 `run_exec.go` 책임 분리 — **P1 / M**

**무엇.** `gateway-go/internal/pipeline/chat/run_exec.go` 는 chat 실행 오케스트레이션의 중심. 한 파일이 (a) 컨텍스트 어셈블리, (b) provider 결정, (c) LLM invocation, (d) fallback retry, (e) tool histogram 포맷팅을 모두 들고 있다 (LOC는 분할에 따라 변동 — 고정 수치 대신 책임 경계를 본다).

**왜.** `executeAgentRun` 한 함수의 인지 비용이 너무 큼. 신규 AI 세션이 진입할 때 캐시 정책 (cache_breakpoints.go) ↔ retry (run_exec_retry_test.go) ↔ steer (steer.go) 사이 호출 그래프를 따라가기 어렵다.

**어디서.**

- 타임스탬프 포맷팅 → `prompt/timestamp.go` (또는 기존 prompt/ 로 흡수)
- tool histogram → `run_helpers.go` 흡수
- 컨텍스트 pre-warming → `run_context_prewarm.go`
- 핵심 agent loop 만 `run_exec.go` 에 남기고 ~600 LOC 목표.

**주의.** `docs/agent-rules/prompt-cache.md` 의 `BeforeAPICall` hook 부착 위치를 깨지 말 것. `cache_breakpoint_budget_test.go` 가 회귀를 잡아주지만, 분리 PR 에서 hook 등록 순서 (`ComposeBeforeAPICall(steer, trailingCache)`) 보존이 필수.

---

### 1.2 `notify_relay.go` 3-way 분리 — **DONE**

**상태.** `notify_heartbeat.go` / `notify_status.go` 등으로 분리 완료. `notify_relay.go` 는 공통 helpers·릴레이 잔존 (~200 LOC대).

---

### 1.3 `openai.go` 분리 — **P2 / M**

**어디서.** `gateway-go/internal/ai/llm/openai.go` → `openai_request.go` (req 빌드) + `openai_stream.go` (SSE parsing). 현재 한 파일에 wire translation + streaming + provider fallback 이 섞여 있어 vLLM 호환성 디버깅 시 진입점을 찾기 어렵다 (LOC는 분할에 따라 변동).

**참고.** 같은 패키지의 `types.go` 가 wire types 의 single source 인지 확인. 분리 PR 에서 import 사이클 위험 점검.

---

### 1.4 `executeAgentRun` 파라미터 12개 → struct — **P2 / S**

**무엇.** `(ctx, params, deps, broadcaster, typingSignaler, statusCtrl, logger, runLog, ...)` 12개. 신규 호출 사이트 추가 시 boilerplate 증가.

**어디서.** `pipeline/chat/run_exec.go` 시그니처. `AgentRunInputs` struct 도입 (단 `deps` 와 분리 — deps 는 stable, inputs 는 per-call).

**주의.** 기존 호출자가 한 곳뿐이면 struct 화 이득 작음. 호출 사이트 grep 후 결정.

---

### 1.5 Tool 디렉터리 sub-grouping 보류 — **P3 / 결정 필요**

**현황.** `pipeline/chat/tools/` 는 `runtimeops/`·`mailarchive/`·`codeaction/` 등으로 이미 서브그룹화. flat+subpackage 혼재 — 추가 도구 시 기존 군집을 따른다.

---

## 2. 성능 (Performance)

### 2.1 캐시 히트율 ops 대시보드 — **P2 / S**

**무엇.** `docs/agent-rules/prompt-cache.md` 에 정의된 4-breakpoint 캐시가 실제 프로덕션에서 얼마나 hit 하는지 누적 지표가 없다.

**어디서.**

- `gateway-go/internal/ai/llm/openai.go` 응답 처리 부분에서 `cache_read_input_tokens`, `cache_creation_input_tokens` 헤더/필드를 metric counter 로 누적
- `/health` 응답에 24h rolling cache hit ratio 노출 (또는 `gateway.cache_stats` RPC)
- `/status` 슬래시에 한 줄 표시: `📦 캐시: 78% hit (24h)`

**왜.** 캐시 doctrine 의 5가지 금지 (시스템 프롬프트 재구성, 매 턴 툴셋 rebuild 등) 위반이 슬그머니 일어나도 현재는 알 수 없다. 히트율 대시보드가 곧 regression alarm.

---

### 2.2 Embedding-aware tool routing — **P3 / L**

**무엇.** 현재 LLM 은 매 턴 ~50개 tool schema 카탈로그를 본다(프리셋·deferred에 따라 노출 상이). 사용자 메시지 embedding 으로 후보 도구 K개 (예: top-8) 만 prompt 에 노출.

**왜.** 토큰 절감 + LLM tool-selection 정확도 향상. 단 cache 위협 — tool list 가 동적이면 static 캐시가 매 턴 깨진다.

**해결.** 두 trick:

1. **Stable bucket.** 8개 도구 풀이 (의미적으로) 같으면 동일 hash → 같은 cached static block. Embedding 라우터의 결정성을 buckle 단위로 강제.
2. **Always-on baseline.** `fs/read`, `wiki/search`, `polaris/search` 같은 hot 도구는 항상 포함. routing 은 long-tail tool 만.

**선결조건.** 2.1 의 캐시 히트율 대시보드. 라우팅 도입 후 회귀 측정 가능해야 함.

---

### 2.3 Polaris semantic-anchor 압축 (Tier 3a 개선) — **P3 / M**

**현황.** `compaction/embedding.go` 는 임베딩 사이드카(현행 Nemotron, 구 BGE-M3) + MMR 로 dedup. 다만 **앵커** (사용자가 명시한 핵심 사실: "내 이름은 X", "프로젝트 Y 마감 6/15") 가 일반 메시지와 똑같이 MMR 점수 경쟁.

**제안.** Anchor extraction 패스 1회 추가:

- LLM 한 번 호출 → "이 대화에서 잊으면 안 되는 사실 5개" 추출
- Anchor 는 압축에서 **inevictable** (Hermes 의 frozen MEMORY snapshot 과 같은 발상)

**어디서.** `compaction/polaris.go` 에 anchor stage 추가 (Tier 1 LLM 전, 또는 함께). `polaris/store.go` 에 anchor field 추가.

**위험.** anchor extraction 자체가 LLM 호출 → latency. **frozen 패턴** 으로 세션 첫 evidence-bearing turn 1회만 (`docs/agent-rules/prompt-cache.md` § 3.5 lazy session-frozen snapshots 패턴 그대로 적용).

---

### 2.4 한국어 응답 품질 회귀 → CI gate — ✅ **구현됨 (opt-in)**

**현황.** `make check` 가 이제 `quality-gate` 를 마지막 단계로 포함한다(`scripts/dev/quality-gate.sh`). 기본은 skip(exit 0) — off-DGX 에서 `make check` 를 깨지 않는다. 모델 사용 가능한 DGX 호스트에서 `DENEB_QUALITY_GATE=1` 로 무장하면 `scripts/dev/iterate.sh --metric quality` → `scripts/dev/baseline.sh compare` 를 돌려 브랜치 baseline 대비 회귀 시 빌드를 fail 한다.

**구현.**

- `make check` 의존성에 `quality-gate` 추가 (opt-in env 로 DGX 에서만 무장)
- 회귀 판정은 기존 `baseline.sh compare`(metric 하락 / quality component -5 / latency +20%)를 재사용 — exit 1 전파
- 브랜치별 baseline 은 기존 `scripts/dev/baseline.sh save` 로 저장(첫 실행 시 NO_BASELINE → exit 0)

**왜.** "테스트는 통과하는데 한국어 응답이 망가졌다" 는 가장 흔한 회귀. 단위 테스트는 catch 못함. live-test 가 catch 하지만 사람이 실행해야 했음 — 이제 무장 시 자동.

---

## 3. 신뢰성 (Reliability)

### 3.1 Polaris session-reopen 라운드트립 테스트 — **P1 / S**

**현황.** `polaris/engine.go:9941 LOC` — `bootstrap: raw < 50K` 로직 존재하나 round-trip 테스트 없음. 압축된 세션이 다시 열렸을 때 DAG 에서 옛 메시지가 정확히 복원되는지 검증 안 됨.

**테스트 시나리오.**

1. 30 turn 대화 → Tier 1 LLM 압축 발화
2. 세션 종료 → process restart
3. 같은 세션 키로 재오픈 → 첫 turn 의 user 메시지 본문이 (요약이 아닌 원본 형태로) DAG 에서 복원 가능?
4. 새 turn 에 컨텍스트 일관성?

**왜.** 단일 사용자 환경에서 process restart 는 잦음 (deploy, OOM, manual). 압축 + reopen path 가 사일런트하게 망가지면 사용자는 "왜 갑자기 내 이름을 까먹지" 로 인지.

---

### 3.2 CJK rune boundary 테스트 — **P1 / S**

**현황.** `compaction/restore.go` 의 `TruncateOldToolResults` 가 rune count (CJK-safe) 로 동작하지만, 256-rune 임계값에서 **한글 정확 경계** 테스트가 없음. 기존 테스트는 ASCII 만.

**테스트.**

- 255 / 256 / 257 rune Hangul 입력 (자모 결합 케이스 포함)
- 한글+영문 mixed at exact boundary
- 조합형 (NFD) vs 완성형 (NFC) Hangul 모두

**왜.** Korean-first 프로젝트. 한글 boundary off-by-one 이 user-visible 손상 (잘림 위치) 으로 새어나갈 수 있다.

---

### 3.3 Dreamer 단위 테스트 — **P2 / M**

**현황.** `gateway-go/internal/domain/wiki/dreamer.go` (965 LOC) — 자율 메모리 합성. `service_async_test.go` 가 dispatch 만 검증. dreamer 자체 단위 테스트 없음.

**테스트.**

- Diary capsule dedup (같은 fact 두 번 기록 → 1개 유지)
- Recent-limit 12 강제 (13번째 호출 시 oldest 제거)
- 상태 파일 corruption recovery (`.diary-process-state.json` 손상 → 정상 fallback)
- Verification phase 의 false-positive 처리

**왜.** Dreamer 는 백그라운드에서 위키를 mutate 하는 가장 영향력 큰 자율 컴포넌트. 회귀 발생 시 user-visible (잘못된 기억).

---

### 3.4 Embedding/handler/agent/handler/provider/media/zip 단위 테스트 — **P2 / S each**

**현황.** Explore 분석:

- `ai/embedding/` — MMR ranking 로컬 테스트 없음
- `runtime/rpc/handler/agent/` — RPC 디스패치 단위 테스트 없음 (integration 만)
- `runtime/rpc/handler/provider/` — provider auth 단위 테스트 없음
- `platform/media/zip.go` — empty/oversized/symlink 경계 케이스 없음

**왜.** Integration test 만 있으면 회귀 분류 비용 큼 ("어디서 깨졌나" 가 unit test 로는 즉답, integration 으로는 추적 필요).

---

### 3.5 DGX Spark GPU pool 헬스 — **P2 / S**

**무엇.** `/health` 가 gateway 자체 상태만. GPU 가용성/큐 깊이/local LLM 응답 latency 가 없음.

**어디서.**

- `gateway-go/internal/runtime/health/ (GPU collectors) + runtime/server/health_*.go` 에 GPU 섹션
- `nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total --format=csv` 1초 간격 캐시
- localai client latency p50/p95

**왜.** "왜 답이 느리지" 의 1차 진단. 현재는 로그 grep 필요. `/health/gpu` 또는 `/status` 슬래시에 한 줄.

---

### 3.6 대용량 파일 전송 처리 — **P3 / M**

**현황.** 대용량 파일 send 시 현재 동작 불명확.

**제안.**

- `pipeline/chat/tools/send_file.go` 에서 size check → 임계값 초과 시 (a) auto-split (b) 다운로드 링크 + 1회용 토큰 전송
- 옵션 (b) 는 gateway 가 임시 HTTP endpoint expose (DGX Spark 의 NAT 통과 가능한 경우만)

---

## 4. 제품/UX (Product)

### 4.1 슬래시 명령 디스커버리 — **P2 / S**

**현황.** `/reset`, `/status`, `/kill`, `/model`, `/think`, `/steer` 등 존재. 사용자가 외워야 함.

**제안.**

- `/?` 또는 `/help` → 모든 슬래시 + 1줄 설명 (한국어)
- 네이티브 클라 입력창에서 `/` 치면 autocomplete 힌트 노출
- 새 슬래시 추가 시 `slash_commands.go` 의 metadata 가 single source

**어디서.** `gateway-go/internal/pipeline/chat/slash_commands.go` 의 metadata 를 single source 로 두고, 네이티브 클라가 이를 읽어 힌트로 노출.

---

### 4.2 Tool histogram → 세션 종료 카드 — **P3 / S**

**무엇.** 한 턴 (또는 한 세션) 종료 시 "이번 답변에 쓴 도구" 요약 카드를 옵션으로 제공.

**예시.**

```
✓ 답변 완료
🔧 사용한 도구: wiki/search ×2 · gmail/list ×1 · fs/read ×3
⏱ 3.2s · 캐시 hit 91%
```

**왜.** "왜 그 답이 나왔지" 의 투명성. 단일 사용자라 noise 부담 작음. 토글 슬래시 (`/trace on|off`) 로 opt-in.

**어디서.** `pipeline/chat/run_lifecycle.go` 의 final delivery 직전, `pipeline/chat/run_exec.go:1409-1431` 의 histogram 재사용.

---

### 4.3 "왜 그 도구를 골랐는지" 1-line trace — **P2 / S**

**무엇.** Tool 호출 직전 LLM 의 reasoning 한 줄을 로그 + 옵션으로 사용자에게.

**현황.** `<thinking>` 블록은 시스템 프롬프트에서 silent. 사용자는 도구가 왜 불렸는지 모름. 디버깅 시 로그 grep.

**제안.** Anthropic extended thinking 의 첫 sentence 만 추출 → `slog.Debug` + (opt-in) 네이티브 클라 inline. 캐시에 영향 없음 (thinking 은 메시지 본문 외부).

---

### 4.4 모닝 레터 / 이브닝 레터 cadence editor — **P2 / M**

**현황.** `pipeline/chat/tools/routine/morning_letter.go` + `skills/productivity/morning-letter/` 존재. cadence (몇 시, 어떤 요일) 가 hardcode 또는 config 파일.

**제안.**

- `/cadence` 슬래시 → 인라인 키보드로 시간/요일 편집
- Evening letter (오늘 한 일 + 내일 일정) 추가
- Cadence persistence: `~/.deneb/cadence.json` 단일 파일

**왜.** 자율 cadence 가 personal AI 의 차별점. 사용자가 GUI 없이 chat 만으로 편집할 수 있어야 함.

---

### 4.5 네이티브 클라 "더보기/요약/원문" 액션셋 — **P3 / M**

**무엇.** 아주 긴 응답일 때 자동으로 요약 + "원문 보기" 액션을 제공.

**현황.** 단순 출력. 사용자 액션 없음.

**제안.** 게이트웨이가 긴 응답을 감지 → 요약 (2-3 문장) + 원문을 함께 내려보내고, 네이티브 클라가 "원문 전체 보기" 토글로 펼친다.

**왜.** 모바일 화면에서 아주 긴 메시지는 스크롤 지옥. 요약 → 필요시 펼침이 모바일 UX 정답.

---

### 4.6 멀티턴 컨텍스트 — per-session pinned facts — **P3 / M**

**무엇.** 사용자가 `/pin <fact>` 로 세션 내내 회상 보장되는 fact 슬롯. 예: `/pin 클라이언트는 X사 임원, 호칭은 부장님`.

**현황.** Polaris 압축 + wiki recall 이 있으나 어떤 fact 가 살아남을지 사용자가 제어 못함.

**제안.** 세션 metadata 에 `pinnedFacts []string` 슬롯. system prompt 의 Dynamic 블록 끝에 항상 prepend. 5개 제한. `/unpin` 으로 제거.

**캐시 영향.** Dynamic 블록 (캐시 마커 없음) 이라 영향 미미. 단 trailing message marker 와 안 충돌하는지 검증 필요.

---

### 4.7 Trust Inbox: 자율 변경 통합 승인 대기열 (안드로이드) — **P1 / M · implemented 2026-08-23**

**무엇.** 시스템이 자율적으로 적용·제안하는 변경 — genesis 스킬 진화·auto-apply, `skill_lifecycle` 자기교정 후보, graduation 단계 상승, dreamer 위키 합성 — 을 안드로이드 워크피드의 승인 카드로 모으고, 카드에서 승인/거절/상세(diff)를 처리.

**현황.** 부품은 존재, 연결이 없음:

- 게이트웨이: `domain/workfeed/store.go`의 콜백 훅(`OnAnswer`·`OnMetaProposal`·`OnEvolveVerdict`·`OnLadder`)이 사실상 승인 큐 골격. `miniapp.workfeed.{list,ack,action.run,answer}` RPC도 이미 있음.
- 자기교정: `pipeline/chat/tools/lifecycletool/skill_lifecycle.go` ↔ `domain/skills/genesis/tracker_self_correction.go` → `~/.deneb/data/self_correction_candidates.jsonl` (`accepted|rejected|superseded|applied` 상태 이미 정의).
- 안드로이드: `WorkFeedPanel.kt`가 `approval:approve`/`approval:reject` 액션을 인식하는 승인 다이얼로그를 이미 보유. `DenebApprovalsScreen`(groupware 결재)이 인박스 UI 선례.

**제안.**

- genesis auto-apply(`runtime/heartbeat/heartbeat_auto_apply.go`), graduation(`genesis/graduation_state.go`), 자기교정 dispatch, dreamer `DreamReport`를 workfeed 카드로 발행 — 기존 `runtime/server/workfeed_dream.go` 패턴 확장.
- 거절 액션을 각 도메인의 되돌리기와 연결: graduation 재잠금(`RelockGraduation`), 자기교정 `rejected` 처리, dreamer git 스냅숏 롤백 힌트.
- 안드로이드: 알림 tray에 승인/거절 직접 액션 추가(기존 `DenebReplyReceiver` 인터랙티브 알림 패턴 재사용). 파괴적 액션(롤백)은 앱 내 상세 화면으로 한정.

**왜.** auto-apply 표면이 skill-body → gateway-source로 졸업(#4536)하며 자율 범위가 넓어지는데, 검토·번복 표면은 jsonl grep뿐. 승인/거절 이력이 쌓이면 신뢰 경계(#4565) 운용의 실험 근거가 됨 — RSI P5(재귀 표면)의 사용자 측 완결.

---

### 4.8 인물·거래 도시에 (Business Dossier) — **P2 / M · implemented 2026-08-23**

**무엇.** 사람·프로젝트·거래별 종합 카드 — 최근 메일 요약, 통화·알림 이력, 관련 위키 페이지(인물/거래/프로젝트 로그), 답장 대기 약속 — 를 한 화면 타임라인으로.

**현황.** 조각은 있고 조이너가 없음:

- `miniapp.people.list`(`runtime/rpc/handler/handlerminiapp/knowledge/people.go`) — Gmail 발신인 × `인물` 위키 페이지 조인 (mail+wiki 2개 소스만).
- 위키 빌딩 블록: `domain/wiki`의 `person_emails.go`·`person_resolve.go`·`counterparties.go`·`deal_records.go`·`project_log.go`.
- `domain/phoneledger` — 통화·알림 일별 JSONL (30일 보존).
- 클라 선례: 안드로이드 `DenebPersonScreen`(`miniapp.mail.sender_context` 기반), 안드로메다 `PeoplePane` 상세 카드.

**제안.**

- 게이트웨이: `people.list`를 확장하거나 `miniapp.person.dossier` 신설 — sender_context(메일) + phoneledger(통화) + deal_records/project_log(위키) 집계.
- 클라: 레인 분리해 각자 선례 확장 — 안드로이드 `DenebPersonScreen`, 안드로메다 `PeoplePane` 상세 → 독립 pane (`panes/index.ts` PANES 등록).

**왜.** README의 제품 정체성 "deep business analysis (mail, projects, people, deals)"의 열람 표면 완결. 데이터는 이미 흐르는데 검색 나열로만 접근됨.

**주의.** 통화 이력 30일 초과분은 phoneledger 보존 한계 — noti-digest 위키 다이제스트와 조인해 장기 타임라인 구성.

---

### 4.9 주간 기억 다이제스트 + dreamer 피드백 — **P2 / S · implemented 2026-08-23**

**무엇.** dreamer가 주간에 검증·병합·만료한 기억 요약 카드(새 사실 N개 / 병합 M개 / 만료 K개 + 항목 펼침 보기)를 발행하고, 사실별 맞아요/틀려요 피드백을 dreamer 검증 루프로 회귀.

**현황.**

- dreamer는 `DreamReport`(`factsVerified/Merged/Expired/Pruned`, `domain/autonomous/dreamer.go`)와 위키 git 스냅숏 롤백 힌트를 이미 생성하나 **별도 감사 로그가 없고**, 소비처는 `workfeed_dream.go` 카드 1장뿐.
- 케이던스 표면: 4.4 cadence editor(모닝/이브닝)에 주간 다이제스트가 세 번째 항목으로 자연 편입.

**제안.**

- `DreamReport`를 주간 롤업으로 누적(예: `~/.deneb/data/dream-reports.jsonl`) — 다이제스트 원천 + 3.3 dreamer 단위 테스트의 실데이터.
- 발행: weekly-report 케이던스로 안드로이드 푸시. 피드백 액션은 4.7 Trust Inbox의 approval:* 메커니즘 공유.
- "틀려요" → `dreamer_critique` 검증에 반증 증거로 주입, 만료 사실 재검토.

**왜.** dreamer는 백그라운드에서 장기기억(위키)을 mutate하는 가장 영향력 큰 자율 컴포넌트인데 사용자에게 불투명. 잘못 학습된 기억의 유일한 조기 검출 경로이자, slow loop(P2)에 사용자 신호를 넣는 첫 회로.

---

## 5. AI 능력 (Capabilities)

### 5.1 Anchor extraction (3.3 과 연관) — **P3 / M**

**무엇.** 위키 fact 자동 추출 + Polaris anchor 의 통합. Dreamer 가 이미 비슷한 일을 함 — 통합으로 dual-source 제거.

**어디서.** `domain/wiki/dreamer.go` ↔ `compaction/embedding.go` 의 anchor 후보 stream 화.

---

### 5.2 Email priority queue (Gmail 라벨 + 사람) — **P3 / L**

**현황.** `platform/mailanalysis` 파이프라인 — 메일 분석은 있으나 우선순위가 평면적 (시간순).

**제안.** 발신자/라벨/스레드 활동에 따른 priority 점수:

- 위키에 "VIP" 표시된 사람 → +50
- "결제/마감" 키워드 → +30
- 같은 스레드에 사용자 회신 있음 → +20
- 신규 발신자 (위키에 없음) → 0
- 점수순으로 morning letter 정렬

**왜.** 현재는 사용자가 모든 메일 분석을 읽고 우선순위를 머리에서 매김. AI 의 가장 큰 가치는 그 sort 를 미리 해주는 것.

**위험.** False negative (놓침). priority 0 도 항상 노출, 단 접힘 처리.

---

### 5.3 Skill SKILL.md schema lint + CI — **P3 / S**

**현황.** `skills/` 의 각 SKILL.md 형식이 손으로 관리됨. schema drift 위험.

**제안.** `scripts/lint-skills.sh` — frontmatter 필수 field (name, description, triggers) 검증. `make check` 에 추가.

---

### 5.4 Slash `/explain` — 직전 응답 어떻게 만들었나 — **P3 / S**

**무엇.** `/explain` → 직전 turn 의 tool 호출, recall hit, 압축 발화 여부를 텍스트로.

**왜.** 사용자가 "왜 이렇게 답했지" 를 추궁할 수 있어야 함. 4.2 의 보조.

---

### 5.5 개인 MCP 브로커 (외부 에이전트에 내 도구 제공) — **P2 / M · implemented 2026-08-23**

**무엇.** 게이트웨이 MCP 엔드포인트(`POST /mcp`, 2026-07-28 전용 — #4562)를 tailnet(Tailscale) 등 사설망 경유로 외부 MCP 클라이언트에 개방. 어디서든 내 위키·일정·일기 검색 도구 호출.

**현황.**

- `runtime/mcpapi/handler.go`는 읽기 전용 allowlist(`wiki_search`·`wiki_read`·`wiki_list`·`project_digests`·`diary_recent`·`calendar_upcoming`·`search_all` — 각각 `miniapp.*` 선언적 매핑)로 이미 좁게 서빙. 쓰기 도구 추가는 명시적으로 "보안 결정"(파일 헤더 주석).
- 인증: 미니앱과 동일한 단일 클라이언트 토큰(`X-Deneb-Client-Token`). 바인딩: loopback 기본.

**제안.**

- 1단계 (노출 변화 없음): MCP 전용 별도 토큰 분리 + allowlist 유지. tailnet 내 디바이스(Cursor 등)에서 접속.
- 2단계: `tailscale serve`가 loopback `/mcp`를 tailnet 한정 프록시 — 게이트웨이 바인드는 loopback 유지.
- `integration` 빈 스킬 카테고리의 첫 주민(외부 MCP 연동 스킬) 후보.

**⚠️ §8과의 긴장 (명시적 요건).** §8 "External-facing API 금지 — loopback이 정답"과 충돌한다. 본 제안은 (a) 게이트웨이 바인드 변경 없음 (b) tailnet 단말 한정 (c) 읽기 전용 — §8의 취지(attack surface 최소)를 준수하는 좁은 예외다. 채택 시 §8 해당 항목에 스코프를 명시해 개정하는 것을 전제로 하며, 운영자 판단 필요.

**구현 (2026-08-23).** 1단계 완료 — `DENEB_MCP_TOKEN` 전용 토큰(`mcpapi.WithDedicatedToken`, 상수시간 비교, 클라이언트 토큰과 공존·독립 폐기), §8 스코프 명시 개정, 운영자 가이드 `docs/tools/mcp-broker.md`(토큰 발급·tailscale serve 노출·클라 설정·가드레일). 2단계(tailnet serve 활성화)는 운영자 절차.

**왜.** MCP 2.0 서버·양클라 채택(#4561) 직후라 한계 비용이 가장 낮은 시점. 개인 지식 자산을 도구 번들로 제공하는 RSI P4 표면.

---

### 5.6 드리머 팩트 원장 + 카운터 실측 — **P1 / M · implemented 2026-08-23**

**무엇.** 드리머의 모든 뮤테이션(합성 적용·중복 병합·아카이브·재분류 이동)을 사실 단위로 `.dream-fact-ledger.jsonl`에 기록하고, `DreamReport`의 팩트 카운터를 원장에서 실측해 채운다.

**배경.** `DreamReport.FactsVerified/FactsPruned`는 레거시 SQL-드리머의 이름을 물려받아 WikiDreamer가 한 번도 설정한 적 없는 필드였다(항상 0). 4.9 다이제스트가 이 카운터를 집계해 "검증 0 · 병합 0 · 만료 0"이 찍히는 결함이 있었다(2026-08-23 발견). 병합·아카이브는 구조화 기록 자체가 없어 밤사이 무엇이 정리됐는지는 git 역사를 뒤져야만 알 수 있었다.

**구현.** `wiki/dream_fact_ledger.go` — `{ts, phase, op, page, refPage, detail}` append 원장(베스트에포트). 적용 지점: `dreamer_apply.go`(learned), `verify_apply.go`의 레코더 콜백(merged/expired/moved) → `rebuildAndVerifyDreamWiki`가 카운터(`FactsMerged/FactsExpired/FactsMoved`)와 함께 기록. `FactsLearned = len(appliedPaths)`(가드로 탈락한 제안 미집계). 레거시 `FactsVerified/FactsPruned`는 wire 호환으로 남기며 주석으로 폐기 명시. 4.9 다이제스트 제목·본문을 실측값(학습/병합/만료/재분류)으로 교체.

**왜.** 잘못된 기억의 추적·디버깅 경로가 생기고, 다이제스트가 정직해지며, 5.7(팩트 단위 피드백)이 짚을 키가 마련된다.

---

### 5.7 드리머 반증 증거 큐 — **P1 / M · implemented 2026-08-23**

**무엇.** 사용자 정정이 드리머에 직접 도달하는 큐. 드림/다이제스트 카드의 정정(workfeed `Correct`)이 `.dream-corrections.jsonl`에 기록되고, 다음 비평 자격 사이클(제안 ≥3 · LLM 배선)의 비평 프롬프트에 "사용자 반증" 블록으로 주입된다 — 정정된 사실을 재진술·위반하는 제안은 drop 사유가 된다. 반증은 실제로 검토한 사이클만 소비하고, `DreamReport.CorrectionsConsidered`로 보고된다.

**배경.** 비평·검증은 제안+인덱스만 봤다(`dreamer_critique.go`). 모순 채널은 LLM 스스로 제안하는 `supersedes`뿐 — 사용자의 정정은 채팅 턴과 수동 위키 수정으로 죽었다. 4.9 다이제스트가 약속한 "정정은 다음 검증 주기에 반영"의 실제 회로.

---

### 5.8 드리머 변경 기록 + 선택적 되돌리기 — **P2 / M · implemented 2026-08-23**

**무엇.** 사이클별 구조화 변경 기록(`.dream-cycle-changes.jsonl` — 커밋 해시 → 생성/갱신/병합/만료/이동 페이지 맵)과 되돌리기 API 2종: `RevertDreamCycle`(사이클 전체 git revert)·`RevertDreamPages`(페이지별 선택 복원 — 갱신 페이지는 이전 내용, 사이클 생성 페이지는 삭제). `DreamReport.GitCommit`가 카드로 흘러 들르미 카드에 "되돌리기" 액션이 달린다(실패 시 카드 미정착·재시도). 감사 파일 두 종(팩트 원장·변경 기록)은 의도적으로 git 버전 관리 대상 — 커서 상태와 달리 드리머의 책무 흔적이라 스스로 못 고치게.

**배경.** `WikiChangeSummary`는 산문이었고 되돌리기 최소 단위가 커밋 전체라 좋은 변경과 나쁜 변경이 함께 죽거나 함께 살아야 했다. 4.7에서 의도적으로 뺐던 dream 카드 되돌리기 액션의 안전한 기반.

---

## 6. 인프라/운영 (Ops)

### 6.1 Live-test 시간 단축 — **P2 / M**

**현황.** `live-test.sh quality` 전체 실행 시간 길음 (수 분). 자주 못 돌림.

**제안.**

- Quality sub-test 병렬화 (현재 sequential)
- Mock LLM 옵션 — 외부 호출 0회로 cache/format/edge 만 빠르게
- `quality --fast` 모드 (~30s 목표)

---

### 6.2 Pre-commit hook — `make check` short-circuit — **P3 / S**

**현황.** `.pre-commit-config.yaml` 존재. 단 변경 파일 기반 partial check 없음.

**제안.** Go file 변경 시 영향받는 package 만 `go test`. Markdown 만 변경시 spellcheck 만.

---

### 6.3 Release-please autobump 검증 — **P3 / S**

**현황.** Conventional commit 강제 (`docs/agent-rules` 의 git-pr.md). 단 release-please 가 실제로 올바르게 bump 하는지 dry-run CI 없음.

**제안.** PR open 시 `release-please --dry-run` → 다음 버전 예측 표시. 사용자가 의도와 맞는지 review.

---

## 7. 단기 (Now) vs 중기 (Next) vs 장기 (Later)

### Now — 다음 1주 (P1)

- 1.1 `run_exec.go` 분리
- 3.1 Polaris reopen 라운드트립 테스트
- 3.2 CJK rune boundary 테스트
- 2.4 한국어 quality CI gate
- 4.7 Trust Inbox 통합 승인 대기열 (implemented 2026-08-23)

### Next — 다음 1개월 (P2)

- 1.2 `notify_relay.go` 분리
- 2.1 캐시 히트율 대시보드
- 3.3 Dreamer 단위 테스트
- 3.5 GPU 헬스
- 4.1 슬래시 디스커버리
- 4.3 Tool reasoning 1-line trace
- 4.4 Cadence editor
- 6.1 Live-test 시간 단축
- 4.8 인물·거래 도시에 Business Dossier (implemented 2026-08-23)
- 4.9 주간 기억 다이제스트 + dreamer 피드백 (implemented 2026-08-23)
- 5.5 개인 MCP 브로커 (implemented 2026-08-23 — §8 개정 완료)
- 5.6 드리머 팩트 원장 + 카운터 실측 (implemented 2026-08-23)
- 5.7 드리머 반증 증거 큐 (implemented 2026-08-23)
- 5.8 드리머 변경 기록 + 선택적 되돌리기 (implemented 2026-08-23)

### Later — 분기 단위 (P3)

- 2.2 Embedding-aware tool routing
- 2.3 Polaris semantic-anchor
- 4.2 Tool histogram 카드
- 4.5 네이티브 클라 "더보기" 액션셋
- 4.6 Pinned facts
- 5.2 Email priority queue
- 5.3 Skill schema lint
- 5.4 `/explain`
- 3.6 50MB 초과 파일 폴백

---

## 8. 명시적 비-제안 (Out of Scope)

다음은 **하지 말자** 로 명시:

- ❌ **Multi-user / multi-tenant.** CLAUDE.md philosophy 위반. 단일 사용자 가정이 코드 단순성의 핵심.
- ❌ **추가 메시징 surface (Telegram/Slack/Discord 등).** 네이티브 클라이언트 단일 표면 원칙 (PR 1922). Surface 추가 = 광범위 회귀 위험.
- ❌ **External-facing API — 단, 5.5의 좁은 예외 제외 (2026-08-23 개정).** Gateway 는 loopback bind 유지가 원칙. 예외: `/mcp` 표면을 tailnet 경유로 열 때 — (a) 게이트웨이 바인드 변경 없음 (`tailscale serve` 가 loopback 을 프록시) (b) tailnet 단말 한정 (funnel 금지) (c) 읽기 전용 allowlist (d) 전용 토큰 `DENEB_MCP_TOKEN` 분리. 이 스코프 밖 노출은 여전히 금지.
- ❌ **새 LLM provider 추가 (Claude/OpenAI/local 외).** 현재 3개 라인 유지보수도 충분. Provider 다양성보다 deep quality.
- ❌ **i18n.** Korean-first 원칙. 영어/타국어 추가 = string 관리 비용.
- ❌ **Plugin marketplace.** Skills 는 in-repo 로 충분. 외부 plugin 은 security review 비용 폭증.

---

## 9. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-05-25 | Claude (claude-opus-4-7) | 초안 작성 |
| 2026-06-02 | Claude | PR 1922 (Telegram 봇 제거) 반영 — 표면 참조를 네이티브 클라로 정정 |
| 2026-08-23 | ZCode (GLM-5.3) | 운영자 요청 4개 제안 추가 (모두 ideation) — 4.7 Trust Inbox(안드로이드), 4.8 Business Dossier, 4.9 주간 기억 다이제스트, 5.5 개인 MCP 브로커(§8 긴장 명시) |
| 2026-08-23 | ZCode (GLM-5.3) | 4.7 Trust Inbox 구현 — 자기교정 감시 태스크(신규 후보 승인/거절 카드) + dream 카드 확인 액션 + 안드로이드 알림 tray 승인/거절 버튼. 기존 auto-apply 표면(meta·graduation·evolve verdict) 카드는 이미 존재해 재활용 |
| 2026-08-23 | ZCode (GLM-5.3) | 4.9 주간 기억 다이제스트 구현 — DreamReport 롤업(`dream-reports.jsonl`) + 주간 다이제스트 카드(확인/틀린 기억 알리기 → 정정 턴) |
| 2026-08-23 | ZCode (GLM-5.3) | 4.8 Business Dossier 구현 — `miniapp.person.dossier` RPC(메일 롤업 + phoneledger 통화·알림 + 위키 전문검색 조인) + 안드로이드 사람 화면·안드로메다 PersonCard 도시에 섹션 |
| 2026-08-23 | ZCode (GLM-5.3) | 5.8 변경 기록+선택적 되돌리기 구현 — 사이클 변경 맵 + RevertDreamCycle/RevertDreamPages + dream 카드 되돌리기 액션 |
| 2026-08-23 | ZCode (GLM-5.3) | 5.7 반증 증거 큐 구현 — 드림/다이제스트 카드 정정 → 비평 프롬프트 반증 블록 주입·소비 |
| 2026-08-23 | ZCode (GLM-5.3) | 드리머 개선 3건(5.6–5.8) 착수 — 5.6 팩트 원장 구현 완료. 사실: 4.9 다이제스트가 항상 0이던 레거시 카운터를 집계하고 있었음을 발견·수정 |
| 2026-08-23 | ZCode (GLM-5.3) | 5.5 개인 MCP 브로커 구현 — `DENEB_MCP_TOKEN` 전용 토큰 분리(상수시간 비교, 클라이언트 토큰 공존) + §8 스코프 명시 개정 + `docs/tools/mcp-broker.md` 가이드. tailnet serve 노출은 운영자 절차로 가이드 |
| 2026-08-23 | Claude (Fable 5) | 위키 전용 개선안을 별도 문서로 분리 — [wiki-improvement-plan-2026-08](wiki-improvement-plan-2026-08.md) (W1~W17: verify 자동이동·드리머 정체성·동일ID 병합·supersede 가드 P0, 메일 재분류·원장 위치·인물·계측·링크 P1, 저장/성능·라우팅·표면·린트 P2) |

---

## 10. 참고

- 코드 인벤토리: Explore 에이전트 (2026-05-25) — `gateway-go/` 핵심 파일 LOC, 테스트 커버리지 갭, 컴팩션 tier 점검
- 도메인 규칙: `docs/agent-rules/{go-gateway,prompt-cache,concurrency,logging,live-testing,optimization}.md`
- 최근 4.22.x CHANGELOG: Polaris/Wiki/단일사용자 simplification 흐름
- 위키 전용 개선안: `docs/research/wiki-improvement-plan-2026-08.md` (2026-08-23, 10영역 조사 + 3렌즈 검증; 본 문서 §4.7~4.9·5.6과 교차)
- 관련 research: `docs/research/{hermes-agent-analysis,hermes-deneb-mapping,tool-interception-gap}.md`
