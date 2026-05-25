# Deneb 개선 아이디어

**Status:** ideation / proposal backlog
**Audience:** Deneb 운영자 + 차기 AI 세션
**Scope:** 코드 위생, 성능, 신뢰성, 제품/UX, AI 능력 — 다섯 갈래.
**Methodology:** `gateway-go/` 전체 inventory + 최근 4.22.x CHANGELOG + `.claude/rules/` 도메인 규칙 + 기존 research 노트 (hermes-agent-analysis, tool-interception-gap) 교차 검토.

> **읽는 법.** 각 아이디어는 **무엇을 → 왜 → 어디서** 순으로 정리했다. 우선순위 (P0~P3) 와 추정 작업량 (S/M/L) 을 라벨로 붙였다. 채택 여부는 운영자 판단. 합의된 아이디어만 별도 PR 로 진행한다.

---

## 0. 한 줄 요약 (TL;DR)

| # | 아이디어 | P | 예상 작업량 |
|---|---|---|---|
| 1 | `run_exec.go` (1432 LOC) 책임 분리 | P1 | M |
| 2 | `notify_relay.go` (877 LOC) 3-way 분리 (snapshots/errors/heartbeat) | P2 | S |
| 3 | Polaris bootstrap & session-reopen 라운드트립 테스트 | P1 | S |
| 4 | CJK rune boundary 테스트 (`compaction/restore.go`) | P1 | S |
| 5 | Dreamer 단위 테스트 (`domain/wiki/dreamer.go`) | P2 | M |
| 6 | Telegram 슬래시 명령 디스커버리 (`/?`, autocomplete 힌트) | P2 | S |
| 7 | 모닝 레터/이브닝 레터 통일된 cadence editor | P2 | M |
| 8 | Gmail 라벨/스레드 기반 우선순위 큐 | P3 | L |
| 9 | Tool histogram → 세션 종료 요약 카드 | P3 | S |
| 10 | Embedding-aware tool routing (LLM 호출 전 후보 도구 pre-filter) | P3 | L |
| 11 | Polaris semantic-anchor 압축 (Tier 3a MMR 개선) | P3 | M |
| 12 | DGX Spark GPU pool 헬스 페이지 (/health/gpu) | P2 | S |
| 13 | "왜 그 도구를 골랐는지" 1-line trace 로그 | P2 | S |
| 14 | 한국어 응답 품질 자동 회귀 (quality baseline → CI gate) | P1 | M |
| 15 | Telegram inline 키보드 "더보기/요약/원문" 액션셋 | P3 | M |
| 16 | 캐시 히트율 ops 대시보드 (`cache_read_input_tokens` 누적) | P2 | S |
| 17 | 멀티턴 컨텍스트 휘발 방지: per-session "pinned facts" 슬롯 | P3 | M |
| 18 | `executeAgentRun` 12개 파라미터 → struct 압축 | P2 | S |
| 19 | Skill SKILL.md schema lint + CI | P3 | S |
| 20 | Telegram 대용량 파일(>50MB) 분할 전송 폴백 | P3 | M |

---

## 1. 코드 위생 (Code Hygiene)

### 1.1 `run_exec.go` 1432 LOC 분리 — **P1 / M**

**무엇.** `gateway-go/internal/pipeline/chat/run_exec.go` 는 700 LOC 가이드라인의 2배. 한 파일이 (a) 컨텍스트 어셈블리, (b) provider 결정, (c) LLM invocation, (d) fallback retry, (e) tool histogram 포맷팅을 모두 들고 있다.

**왜.** `executeAgentRun` 한 함수의 인지 비용이 너무 큼. 신규 AI 세션이 진입할 때 캐시 정책 (cache_breakpoints.go) ↔ retry (run_exec_retry_test.go) ↔ steer (steer.go) 사이 호출 그래프를 따라가기 어렵다.

**어디서.**
- `run_exec.go:85-91` — 타임스탬프 포맷팅 → `prompt/timestamp.go` (또는 기존 prompt/ 로 흡수)
- `run_exec.go:1409-1431` — tool histogram → `run_helpers.go` 흡수
- `run_exec.go:100~` — 컨텍스트 pre-warming → `run_context_prewarm.go`
- 핵심 agent loop 만 `run_exec.go` 에 남기고 ~600 LOC 목표.

**주의.** `.claude/rules/prompt-cache.md` 의 `BeforeAPICall` hook 부착 위치를 깨지 말 것. `cache_breakpoint_budget_test.go` 가 회귀를 잡아주지만, 분리 PR 에서 hook 등록 순서 (`ComposeBeforeAPICall(steer, trailingCache)`) 보존이 필수.

---

### 1.2 `notify_relay.go` 877 LOC 3-way 분리 — **P2 / S**

**무엇.** `gateway-go/internal/runtime/server/notify_relay.go` 는 세 가지 무관한 책임을 묶고 있다:
1. **Status snapshot** — on-demand 세션 상태 조회
2. **Error mirror** — broadcast tap → telegram error notify
3. **Health heartbeat** — periodic self-poll

**어디서.** `notify_snapshots.go` / `notify_errors.go` / `notify_heartbeat.go` 로 분리. 공통 helpers 만 `notify_relay.go` 잔존. Test 도 같은 분할.

**왜.** 단일 책임 + grep 가능성 향상. heartbeat 가 가장 자주 수정되는 영역인데, 매번 877 LOC 파일을 열어야 한다.

---

### 1.3 `openai.go` 790 LOC 분리 — **P2 / M**

**어디서.** `gateway-go/internal/ai/llm/openai.go` → `openai_request.go` (req 빌드) + `openai_stream.go` (SSE parsing). 현재 한 파일에 wire translation + streaming + provider fallback 이 섞여 있어 vLLM 호환성 디버깅 시 진입점을 찾기 어렵다.

**참고.** 같은 패키지의 `types.go` 가 wire types 의 single source 인지 확인. 분리 PR 에서 import 사이클 위험 점검.

---

### 1.4 `executeAgentRun` 파라미터 12개 → struct — **P2 / S**

**무엇.** `(ctx, params, deps, broadcaster, typingSignaler, statusCtrl, logger, runLog, ...)` 12개. 신규 호출 사이트 추가 시 boilerplate 증가.

**어디서.** `pipeline/chat/run_exec.go` 시그니처. `AgentRunInputs` struct 도입 (단 `deps` 와 분리 — deps 는 stable, inputs 는 per-call).

**주의.** 기존 호출자가 한 곳뿐이면 struct 화 이득 작음. 호출 사이트 grep 후 결정.

---

### 1.5 Tool 디렉터리 sub-grouping 보류 — **P3 / 결정 필요**

**현황.** `pipeline/chat/tools/` 42 파일. Explore 분석은 "flat 유지 OK" 였음. 다만 `gmail*`, `skill*`, `exec*`, `fs*` 가 prefix 군집을 형성 — 50+ 가 되면 `tools/{fs,gmail,exec,skill}/` 로 자연스럽게 분리될 가능성. **지금은 보류, 다음 5개 추가 시 재검토.**

---

## 2. 성능 (Performance)

### 2.1 캐시 히트율 ops 대시보드 — **P2 / S**

**무엇.** `.claude/rules/prompt-cache.md` 에 정의된 4-breakpoint 캐시가 실제 프로덕션에서 얼마나 hit 하는지 누적 지표가 없다.

**어디서.**
- `gateway-go/internal/ai/llm/openai.go` 응답 처리 부분에서 `cache_read_input_tokens`, `cache_creation_input_tokens` 헤더/필드를 metric counter 로 누적
- `/health` 응답에 24h rolling cache hit ratio 노출 (또는 `gateway.cache_stats` RPC)
- Telegram `/status` 슬래시에 한 줄 표시: `📦 캐시: 78% hit (24h)`

**왜.** 캐시 doctrine 의 5가지 금지 (시스템 프롬프트 재구성, 매 턴 툴셋 rebuild 등) 위반이 슬그머니 일어나도 현재는 알 수 없다. 히트율 대시보드가 곧 regression alarm.

---

### 2.2 Embedding-aware tool routing — **P3 / L**

**무엇.** 현재 LLM 은 매 턴 42개 tool schema 를 모두 본다. 사용자 메시지 embedding 으로 후보 도구 K개 (예: top-8) 만 prompt 에 노출.

**왜.** 토큰 절감 + LLM tool-selection 정확도 향상. 단 cache 위협 — tool list 가 동적이면 static 캐시가 매 턴 깨진다.

**해결.** 두 trick:
1. **Stable bucket.** 8개 도구 풀이 (의미적으로) 같으면 동일 hash → 같은 cached static block. Embedding 라우터의 결정성을 buckle 단위로 강제.
2. **Always-on baseline.** `fs/read`, `wiki/search`, `polaris/search` 같은 hot 도구는 항상 포함. routing 은 long-tail tool 만.

**선결조건.** 2.1 의 캐시 히트율 대시보드. 라우팅 도입 후 회귀 측정 가능해야 함.

---

### 2.3 Polaris semantic-anchor 압축 (Tier 3a 개선) — **P3 / M**

**현황.** `compaction/embedding.go` 는 BGE-M3 + MMR 로 dedup. 다만 **앵커** (사용자가 명시한 핵심 사실: "내 이름은 X", "프로젝트 Y 마감 6/15") 가 일반 메시지와 똑같이 MMR 점수 경쟁.

**제안.** Anchor extraction 패스 1회 추가:
- LLM 한 번 호출 → "이 대화에서 잊으면 안 되는 사실 5개" 추출
- Anchor 는 압축에서 **inevictable** (Hermes 의 frozen MEMORY snapshot 과 같은 발상)

**어디서.** `compaction/polaris.go` 에 anchor stage 추가 (Tier 1 LLM 전, 또는 함께). `polaris/store.go` 에 anchor field 추가.

**위험.** anchor extraction 자체가 LLM 호출 → latency. **frozen 패턴** 으로 세션 첫 evidence-bearing turn 1회만 (`.claude/rules/prompt-cache.md` § 3.5 lazy session-frozen snapshots 패턴 그대로 적용).

---

### 2.4 한국어 응답 품질 회귀 → CI gate — **P1 / M**

**현황.** `scripts/dev/live-test.sh quality` 가 100점 만점 metric. 베이스라인 저장은 있으나 CI 자동 gate 가 없음.

**제안.**
- `make check` 의존성에 quality test 추가 (DGX Spark 환경에서만)
- baseline 대비 -10pt 회귀 시 빌드 fail
- 브랜치별 baseline 자동 저장 (이미 `baseline save` 명령 존재)

**왜.** "테스트는 통과하는데 한국어 응답이 망가졌다" 는 가장 흔한 회귀. 단위 테스트는 catch 못함. live-test 가 catch 하지만 사람이 실행해야 함.

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
- `gateway-go/internal/runtime/server/health.go` 에 GPU 섹션
- `nvidia-smi --query-gpu=utilization.gpu,memory.used,memory.total --format=csv` 1초 간격 캐시
- localai client latency p50/p95

**왜.** "왜 답이 느리지" 의 1차 진단. 현재는 로그 grep 필요. `/health/gpu` 또는 Telegram `/status` 에 한 줄.

---

### 3.6 Telegram 대용량 파일 분할 전송 폴백 — **P3 / M**

**현황.** Bot API 50MB 제한 (CLAUDE.md 명시). 50MB 초과 파일 send 시 현재 동작 불명확.

**제안.**
- `pipeline/chat/tools/send_file.go` 에서 size check → 50MB 초과 시 (a) auto-split (b) 다운로드 링크 + 1회용 토큰 전송
- 옵션 (b) 는 gateway 가 임시 HTTP endpoint expose (DGX Spark 의 NAT 통과 가능한 경우만)

---

## 4. 제품/UX (Product)

### 4.1 Telegram 슬래시 명령 디스커버리 — **P2 / S**

**현황.** `/reset`, `/status`, `/kill`, `/model`, `/think`, `/steer` 등 존재. 사용자가 외워야 함.

**제안.**
- `/?` 또는 `/help` → 모든 슬래시 + 1줄 설명 (한국어)
- Telegram BotFather `setMyCommands` 자동 동기화 (입력창에서 `/` 치면 autocomplete)
- 새 슬래시 추가 시 `slash_commands.go` 의 metadata 가 single source

**어디서.** `gateway-go/internal/pipeline/chat/slash_commands.go` + `internal/platform/telegram/slash_commands.go`. 두 파일이 분리돼 있는데 metadata 통일.

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

**제안.** Anthropic extended thinking 의 첫 sentence 만 추출 → `slog.Debug` + (opt-in) Telegram inline. 캐시에 영향 없음 (thinking 은 메시지 본문 외부).

---

### 4.4 모닝 레터 / 이브닝 레터 cadence editor — **P2 / M**

**현황.** `pipeline/chat/tools/morning_letter.go` + `skills/productivity/morning-letter/` 존재. cadence (몇 시, 어떤 요일) 가 hardcode 또는 config 파일.

**제안.**
- `/cadence` 슬래시 → 인라인 키보드로 시간/요일 편집
- Evening letter (오늘 한 일 + 내일 일정) 추가
- Cadence persistence: `~/.deneb/cadence.json` 단일 파일

**왜.** 자율 cadence 가 personal AI 의 차별점. 사용자가 GUI 없이 chat 만으로 편집할 수 있어야 함.

---

### 4.5 Telegram inline 키보드 "더보기/요약/원문" — **P3 / M**

**무엇.** 긴 응답 (4096자 근접) 시 자동으로 요약 + "원문 보기" 인라인 버튼.

**현황.** 단순 truncate or split. 사용자 액션 없음.

**제안.** `internal/platform/telegram/send.go` 에서 length > 3500 chars 감지 → 요약 (2-3 문장) + `[원문 전체 보기]` 콜백. 콜백 시 원문 send.

**왜.** Telegram on Android 의 좁은 화면에서 4096자 메시지는 스크롤 지옥. 요약 → 필요시 펼침이 모바일 UX 정답.

---

### 4.6 멀티턴 컨텍스트 — per-session pinned facts — **P3 / M**

**무엇.** 사용자가 `/pin <fact>` 로 세션 내내 회상 보장되는 fact 슬롯. 예: `/pin 클라이언트는 X사 임원, 호칭은 부장님`.

**현황.** Polaris 압축 + wiki recall 이 있으나 어떤 fact 가 살아남을지 사용자가 제어 못함.

**제안.** 세션 metadata 에 `pinnedFacts []string` 슬롯. system prompt 의 Dynamic 블록 끝에 항상 prepend. 5개 제한. `/unpin` 으로 제거.

**캐시 영향.** Dynamic 블록 (캐시 마커 없음) 이라 영향 미미. 단 trailing message marker 와 안 충돌하는지 검증 필요.

---

## 5. AI 능력 (Capabilities)

### 5.1 Anchor extraction (3.3 과 연관) — **P3 / M**

**무엇.** 위키 fact 자동 추출 + Polaris anchor 의 통합. Dreamer 가 이미 비슷한 일을 함 — 통합으로 dual-source 제거.

**어디서.** `domain/wiki/dreamer.go` ↔ `compaction/embedding.go` 의 anchor 후보 stream 화.

---

### 5.2 Email priority queue (Gmail 라벨 + 사람) — **P3 / L**

**현황.** `gmailpoll/pipeline.go` (757 LOC) — 메일 분석은 있으나 우선순위가 평면적 (시간순).

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

**현황.** Conventional commit 강제 (`.claude/rules` 의 git-pr.md). 단 release-please 가 실제로 올바르게 bump 하는지 dry-run CI 없음.

**제안.** PR open 시 `release-please --dry-run` → 다음 버전 예측 표시. 사용자가 의도와 맞는지 review.

---

## 7. 단기 (Now) vs 중기 (Next) vs 장기 (Later)

### Now — 다음 1주 (P1)
- 1.1 `run_exec.go` 분리
- 3.1 Polaris reopen 라운드트립 테스트
- 3.2 CJK rune boundary 테스트
- 2.4 한국어 quality CI gate

### Next — 다음 1개월 (P2)
- 1.2 `notify_relay.go` 분리
- 2.1 캐시 히트율 대시보드
- 3.3 Dreamer 단위 테스트
- 3.5 GPU 헬스
- 4.1 슬래시 디스커버리
- 4.3 Tool reasoning 1-line trace
- 4.4 Cadence editor
- 6.1 Live-test 시간 단축

### Later — 분기 단위 (P3)
- 2.2 Embedding-aware tool routing
- 2.3 Polaris semantic-anchor
- 4.2 Tool histogram 카드
- 4.5 Telegram inline "더보기" 키보드
- 4.6 Pinned facts
- 5.2 Email priority queue
- 5.3 Skill schema lint
- 5.4 `/explain`
- 3.6 50MB 초과 파일 폴백

---

## 8. 명시적 비-제안 (Out of Scope)

다음은 **하지 말자** 로 명시:

- ❌ **Multi-user / multi-tenant.** CLAUDE.md philosophy 위반. 단일 사용자 가정이 코드 단순성의 핵심.
- ❌ **Web UI / desktop client.** Telegram-only optimization 원칙. Surface 추가 = 광범위 회귀 위험.
- ❌ **External-facing API.** Gateway 는 loopback bind 가 정답. 외부 노출은 attack surface 만 늘림.
- ❌ **새 LLM provider 추가 (Claude/OpenAI/local 외).** 현재 3개 라인 유지보수도 충분. Provider 다양성보다 deep quality.
- ❌ **i18n.** Korean-first 원칙. 영어/타국어 추가 = string 관리 비용.
- ❌ **Plugin marketplace.** Skills 는 in-repo 로 충분. 외부 plugin 은 security review 비용 폭증.

---

## 9. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-05-25 | Claude (claude-opus-4-7) | 초안 작성 |

---

## 10. 참고

- 코드 인벤토리: Explore 에이전트 (2026-05-25) — `gateway-go/` 핵심 파일 LOC, 테스트 커버리지 갭, 컴팩션 tier 점검
- 도메인 규칙: `.claude/rules/{go-gateway,prompt-cache,concurrency,logging,live-testing,optimization}.md`
- 최근 4.22.x CHANGELOG: Polaris/Wiki/단일사용자 simplification 흐름
- 관련 research: `docs/research/{hermes-agent-analysis,hermes-deneb-mapping,tool-interception-gap}.md`
