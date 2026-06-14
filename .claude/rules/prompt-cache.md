---
description: "프롬프트 캐시 불가침 원칙, 3계층 구조, cache-aware 슬래시"
globs: ["gateway-go/internal/pipeline/chat/prompt/**", "gateway-go/internal/pipeline/chat/slash_commands.go"]
---

# Prompt Cache Doctrine

> **프롬프트 캐시는 불가침 영역이다.** Anthropic/OpenRouter의 `cache_control` 기반 prefix 캐시가 깨지면 Claude 요청당 입력 토큰 비용이 정가로 복귀한다 (캐시 히트 시 10% 수준). 코드베이스 전반에 걸쳐 이 원칙을 **강제**한다.

---

## 1. 캐시 마커 배치 ("system_and_3" 변형)

Anthropic 은 **요청당 cache_control breakpoint 최대 4개** 한도. 우리는 시스템 블록 2개 + 메시지 트레일링 2개 = 정확히 4개로 채운다. Hermes Agent 의 system_and_3 패턴 (system 1 + msg 3) 을 우리 환경 (Semi-static 의 ~10-15K 스킬 prompt 캐시 가치 보존) 에 맞춰 축소한 변형이다.

| 슬롯 | 부착 시점 | 포함 내용 | 캐시 여부 | 비고 |
|---|---|---|---|---|
| **Static** (system) | `BuildSystemPromptBlocks` 조립 시 | identity, communication, attitude, tooling | ✅ ephemeral | 가장 크고 오래 캐시. 툴셋 바뀔 때만 무효화 |
| **Semi-static** (system) | 같음 | skills compact-index (name + description + location) | ✅ ephemeral | P5 적용 후 ~5-7K 토큰 (full 의 절반). tags/category/related_skills 제외. 본문은 `skills(action=read)` 또는 `read` 도구로 on-demand 로드. 스킬 추가/제거 시 invalidate 동일 |
| **Dynamic** (system) | — | memory (P1 frozen), messaging, context, workspace, runtime | ❌ 마커 없음 | 자체 변동 요인은 거의 timestamp 뿐. P1.5 에서 day-only precision 으로 정화 → 자정 넘김 외에는 byte-stable. 정확한 시각은 P6 이 user message 에 ISO 8601 prefix 로 baking (transcript 에 그대로 저장 → 매 turn 일관 prefix). 이 stability 가 trailing message marker 의 prefix 매칭 전제 |
| **User message timestamp** (P6) | `executeAgentRun` 의 transcript persist 시 prepend | `[2026-05-10T15:30:00+09:00] {raw}` | — (transcript 자체) | system 의 day-only 보완. 모델이 fresh wall-clock 인식 + 매 turn 의 timestamp 가 transcript 에 baked → 다음 turn history load 시 같은 prefix → trailing marker prefix matching 보존 |
| **Trailing msg N-1** | `BeforeAPICall` hook | 끝에서 2번째 non-system 메시지의 마지막 ContentBlock | ✅ ephemeral | 멀티턴 prefix 재사용 |
| **Trailing msg N**   | `BeforeAPICall` hook | 마지막 non-system 메시지의 마지막 ContentBlock | ✅ ephemeral | 멀티턴 prefix 재사용 |

구현 진입점:
- 시스템 블록 마커: `gateway-go/internal/pipeline/chat/prompt/system_prompt.go:BuildSystemPromptBlocks`
- 트레일링 마커: `gateway-go/internal/pipeline/chat/cache_breakpoints.go:buildTrailingCacheHook` → `run_exec.go` 의 `cfg.BeforeAPICall = ComposeBeforeAPICall(steer, trailingCache)`
- Anthropic 모드 결정: `gateway-go/internal/pipeline/chat/run_provider.go:resolveAPIMode` (non-Anthropic 에서는 hook 이 nil 반환 → ComposeBeforeAPICall 가 필터)

`gateway-go/internal/pipeline/chat/prompt/prompt_cache.go:PromptCache` 가 static 블록을 **툴 이름 리스트 해시 키**로 캐싱해 재조립 비용도 제거한다.

### 4-breakpoint 한도 (절대 어기지 말 것)

> **breakpoint 5개 이상을 보내면 Anthropic 이 요청을 거부한다 (400).** 새 마커를 추가할 때는 위 표에서 어느 슬롯을 차지하는지 명시하라. 5번째가 필요하면 기존 슬롯 하나를 비워야 한다 (예: trailing 을 2 → 1 로 줄이거나, Semi-static 을 Static 에 합쳐 system 마커를 1개로 축소).

### 자동 검증 (regression 가드)

`gateway-go/internal/pipeline/chat/cache_breakpoint_budget_test.go` 가 system blocks + messages + tools 의 합산 cache_control 마커를 실제 카운트해 4 이하 invariant 를 보장한다. system 단독 카운트가 아니라 trailing hook 적용 후 통합 합산이므로, 새 marker 가 어디에 추가되든 이 테스트가 가장 먼저 fail. 테스트가 빨갛게 되면 즉시 위 표를 업데이트하고 슬롯 재조정.

### 캐시 histogram 확인

```bash
# 라이브 테스트 중 캐시 히트/미스 카운트 (Anthropic 응답 헤더)
scripts/dev/live-test.sh logs-grep "cache_read_input_tokens\|cache_creation_input_tokens"
```

멀티턴 (3+ 메시지) 후 `cache_read_input_tokens` 가 명확히 증가해야 정상. 첫 턴은 `cache_creation_input_tokens` 만 보이고, 다음 턴부터 `cache_read_input_tokens` 가 누적되는 것이 시그널.

---

## 1.5. vLLM APC (메인 모델 경로) — 꼬리 주입 원칙

> 메인 챗 모델이 로컬 vLLM(현재 DSV4-Flash)로 옮겨가면서 **마커 기반 Anthropic 캐시와 전혀 다른 제약**이 1순위가 됐다. vLLM 의 Automatic Prefix Caching 은 렌더된 프롬프트 전체에 대한 **엄격한 byte-prefix 매칭**이고, DSV4 인코더의 렌더 순서는 `[system 내용][tools 스키마][대화 히스토리]` 다. 즉 **system 끝의 per-turn 바이트 1줄이 tools + 전체 히스토리(수만 토큰)의 KV 를 통째로 무효화**한다.

2026-06-13 측정: recall(`<recall-context>`, hindsight auto-recall 이 매 턴 변동) + tier-1 위키가 system 꼬리에 append 되던 시절 적중률 80.7%, 인터랙티브 턴 프리필 꼬리 20–40초 (프리필 스파이크는 dsv4 메모리 워치독 트립의 문서화된 원인이기도 하다).

### 원칙 (Anthropic 4-마커 룰과 별개로 동시 적용)

1. **per-turn 가변 바이트는 system 프롬프트에 절대 넣지 마라.** dynamic 블록이 "마커가 없어서 공짜"인 것은 Anthropic 이야기다 — vLLM 에선 system 의 모든 바이트가 히스토리보다 앞 prefix 다.
2. **per-turn 주입은 마지막 user 메시지의 wire-only suffix 로.** `chat/run_tail_inject.go` (`buildTailAdditions` + `injectTailAdditions`) 가 단일 진입점이다: recall 증거, auto-delivery 지시문이 여기로 간다. transcript 에는 깨끗한 원문이 저장돼 다음 턴 히스토리는 이번 턴이 캐시한 prefix 와 byte-동일하다. 비용 = 추가분 자신의 토큰뿐.
3. **세션 내 안정, 세션 간 공유.** system 에 남는 준가변 콘텐츠는 세션 동결로: tier-1 위키(`chat/tier1_cache.go`), 컨텍스트 파일(`prompt.WithSessionSnapshot`), 토픽 지식, calendar glance(일 단위). `/reset` 이 일괄 clear.
4. **run 패밀리를 가르지 마라.** 같은 세션의 heartbeat/인터랙티브 턴이 다른 system 바이트를 받으면 prefix 패밀리가 2개로 갈라져 KV 풀을 이중 점유한다. AutoDelivered 지시문이 tail 로 간 이유이자, nil-Delivery 런이 세션 키에서 채널을 폴백(`sessionFallbackChannel`)하는 이유.

### 측정

- 엔진 전역: `curl -s http://<engine>/metrics | grep prefix_cache` (`vllm:prefix_cache_{hits,queries}_total`, 토큰 단위 누적).
- per-run: agentlog `run.cache` 이벤트 (`chat/engine_cache_sample.go` 가 턴 종료 후 /metrics 델타를 비동기 기록; vLLM usage 에 cached_tokens 가 없는 빌드에서 유일한 per-turn 신호). 단일 사용자 직렬 트래픽 기준 근사치.

---

## 2. 불가침 3원칙

### Rule A — **과거 메시지를 변경하지 마라**
- 이미 LLM에 전송된 메시지 content를 사후에 mutate 금지
- 예외 1: **컨텍스트 압축(compaction)**. 압축은 의도적으로 캐시 breaking point를 만든다
- 예외 2: **`BeforeAPICall` hook 의 per-request copy**. 트레일링 cache_control 마커 부착(`chat/cache_breakpoints.go`)이나 `/steer` (`chat/steer.go`) 처럼 transcript 자체는 건드리지 않고 LLM 호출용 사본에만 변경을 가하는 패턴은 허용
- 위반 예시 (금지):
  ```go
  // BAD — 과거 assistant 메시지에 추가 정보 주입 (transcript mutate)
  messages[len(messages)-3].Content += "\n\nUpdate: ..."
  ```

### Rule B — **대화 중 툴셋을 바꾸지 마라**
- `BuildSystemPromptBlocks`는 static 블록 키를 **정렬된 툴 이름 리스트**로 생성. 툴 추가/제거는 static 캐시 무효화
- 대화 시작 후 `/tools` 조작이나 `toolreg` 재등록 금지 — 다음 세션부터 반영
- 위반 예시 (금지):
  ```go
  // BAD — 대화 중간에 툴셋 rebuild
  pipeline.Reconfigure(newToolset)  // 매 턴 static prompt 재생성됨
  ```

### Rule C — **시스템 프롬프트를 매 턴 재구성하지 마라**
- Memory reload, 컨텍스트 파일 refresh, timezone recheck 등이 매 요청마다 발화하면 system 블록의 `cache_control`도 무력화
- `PromptCache.ContextFiles`는 mtime 기반 TTL로 이미 이 문제를 해결 — **이 캐시를 우회하거나 비활성화하지 말 것**
- 위반 예시 (금지):
  ```go
  // BAD — 매 요청 파일 재로드
  files := loadContextFilesDirectly(workspace)  // Cache 우회
  ```

---

## 3. Cache-aware 슬래시 커맨드

슬래시 커맨드가 시스템 프롬프트 state를 바꿔야 할 때는 **기본 deferred**, 명시적 `--now` 플래그로 즉시 invalidation opt-in.

### 패턴

```go
// 슬래시 "/<cmd>" 핸들러
func handleCmd(args []string) error {
    immediate := hasFlag(args, "--now")

    persistChange(args)  // 디스크/DB 쓰기

    if immediate {
        pipeline.InvalidateStaticCache("cmd-applied")
        return replyToUser("적용했습니다 (이번 세션 즉시 반영).")
    }
    return replyToUser("저장했습니다. 다음 세션부터 반영됩니다. 지금 바로 적용하려면 `/cmd --now`.")
}
```

### 대상 슬래시 예

- `/skills install <name>` — skill 추가는 semi-static 캐시 깸
- `/model <new>` — 모델 변경이 capability 힌트 바꾸면 static 캐시 영향
- `/personality <set>` — 페르소나는 dynamic 블록이면 캐시 영향 없음, 그러나 static 블록에 페르소나가 섞이면 영향

**판단 기준**: 슬래시 커맨드가 `system_prompt.go`의 Static/Semi-static 블록 생성 입력에 영향을 주면 cache-aware 처리 필수.

---

## 3.5. Lazy session-frozen snapshots (P1)

매 요청마다 system prompt 의 dynamic 영역에 들어가는 동적 콘텐츠 (recall preflight, 향후 메모리 회상 등) 가 있다면 **세션 첫 evidence-bearing 발화 시 1회 build → 그 세션 내내 frozen** 패턴을 사용하라. Hermes Agent 의 frozen MEMORY snapshot 과 같은 발상.

### 현재 적용

- **RecallMemory** — `gateway-go/internal/pipeline/chat/recall_cache.go`
  - (session, cue-fingerprint) 별 cache; hindsight auto-recall 은 매 턴 fresh (no-cue 비캐시)
  - `/reset` 핸들러 (`slash_dispatch.go`) 에서 clear
  - 가치: cache 가 아니라 **latency 절감** — wiki/diary/transcript/polaris 검색 (각 1.5s timeout) 을 같은 cue 반복 시 재사용
  - ★ **주입 위치는 system 이 아니라 마지막 user 메시지 wire-only suffix** (§1.5; `run_tail_inject.go`). per-turn 변동 콘텐츠를 system 에 두면 vLLM APC 에서 히스토리 전체가 죽는다.
- **Tier-1 위키** — `gateway-go/internal/pipeline/chat/tier1_cache.go`
  - 세션 첫 비어있지 않은 결과를 동결 (first-write-wins), `/reset` 에서 clear
  - FormatTier1 이 라이브 위키를 읽으므로 동결 없이는 mid-session 위키 쓰기마다 system 꼬리가 흔들린다

### Lazy semantics 가 핵심

- "첫 turn" 이 아닌 **"첫 evidence"** 기준. cue 가 없거나 evidence 가 없는 결과는 cache 안 함 → 다음 turn 의 더 나은 cue 기회 보존.
- Snapshot 이 한 번 형성되면 그 세션 끝까지 frozen. 새 cue 가 와도 무시. 사용자가 새 회상 필요하면 wiki tool 직접 호출.

### Reset semantics

- `/reset` 핸들러가 frozen snapshot 을 일괄 clear (transcript, session snapshot 과 같이).
- Session 종료 (timeout, abort) 시점은 현재 cache 유지 — 단일 사용자 환경에서 누수 미미. 향후 PhaseEnd lifecycle hook 추가 가능 (P1.1).

### 왜 cache hit 효과가 아니라 latency 효과인가

P0 가 Dynamic 블록의 cache_control 마커를 제거했기 때문에 Dynamic 자체는 어차피 매 turn 정가다. RecallMemory 가 byte-stable 해져도 그것만으로는 cache hit 가 안 생긴다. P1 의 진짜 이득은 wiki/diary/transcript/polaris search 의 **wall-clock latency 제거**. 향후 Dynamic 을 sub-block 으로 쪼개서 frozen part 에 marker 를 다시 부착하면 그때 비로소 cache 효과가 추가로 따라온다.

---

## 4. `/steer` — 캐시-안전 중간 개입

`/steer <note>` 는 실행 중인 에이전트 턴에 note를 주입하되 **기존 tool-role 메시지의 content에 append**하여 캐시를 깨지 않는다:

```
기존 메시지: [system, user, assistant(tool_call), tool(result)]
/steer "참고로 X는 무시해" → [system, user, assistant(tool_call), tool(result + "\n\n[사용자 조정: 참고로 X는 무시해]")]
```

Role alternation 유지 + content prefix 보존 → cache breakpoint까지의 prefix 동일.

구현 위치: `gateway-go/internal/pipeline/chat/steer.go` (또는 관련 파일). 마지막 tool-role 메시지가 없으면 pending 유지.

---

## 5. 컨텍스트 압축 — 유일한 예외

`internal/pipeline/chat/` 의 compaction은 의도적으로 과거 메시지를 요약/교체한다. **이것만이 Rule A의 transcript-mutating 공식 예외**.

### 압축 규약

- 요약된 영역에는 **SUMMARY_PREFIX**를 부착해 모델이 "요약에 답하지 않도록" 강제
- 권장 한국어 prefix: `"[컨텍스트 요약 — 참고 전용] 이 요약에 직접 답하지 마세요. 요약 뒤의 최신 사용자 메시지에만 응답하세요."`
- Head protect (최소 3 메시지: system, 첫 user, 첫 assistant) + Tail protect (최근 N 메시지) + Middle summarize
- **재압축 시 요약을 업데이트**(replace)하지 말고 이전 요약에 추가하거나 갱신
- Hermes 권장: 첫 압축 때만 system 끝에 `"[Note: Some earlier conversation turns have been compacted...]"` 한 줄 append, 이후 압축은 system 미터치 → static cache 영구 생존
- **우리 구현 (P4)**: `Session.CompactionFired` flag 가 sticky. summary-producing tier (LLM/Embedding/Recency/Emergency) 가 발화하면 set, 다음 turn 부터 system prompt 의 dynamic block 끝에 한국어 reminder 추가 (`[알림: 이 세션의 일부 이전 메시지는 자동 요약으로 압축되었습니다... [컨텍스트 요약 — 참고 전용] 표식이 붙은 메시지...]`). 첫 set 시 trailing message marker 의 prefix 1회 깨짐 (한 cache miss), 그 후 dynamic block byte-stable. cheap pruning (Micro/Tier 2b) 는 set X — summary 없으니 모델 알림 불필요. `chat/compaction_marker.go:markCompactionFired` 가 진입점, `assembleMessages` 의 polaris result 검사 후 호출. mid-loop retry 의 압축은 mark X (rare path; 다음 turn 의 assembleMessages 압축이 또 fire 하면 그때 mark).

### 압축 trigger thresholds — 우리 환경의 정당화 (P3)

`gateway-go/internal/pipeline/compaction/polaris.go` 의 상수:

| 상수 | 값 | 역할 |
|---|---|---|
| `DefaultEmergencyInputThreshold` | 30,000 tokens | 단일 user input 이 이 값을 넘으면 Emergency tier 발화 (오래된 message evict + summary) |
| `DefaultLLMThresholdPct` | 0.90 | 전체 messages 토큰이 컨텍스트 budget 의 90% 초과 시 LLM 요약 발화 |
| `DefaultLLMTargetPct` | 0.20 | 압축 후 목표 토큰 비율 (budget 의 20%) |
| `DefaultMicroTurnThreshold` | 4 turns | Tier 2 / Tier 2b cheap pruning 이 적용되는 cutoff (마지막 4 assistant turn 보호) |
| `DefaultStubMinChars` | 256 runes | Tier 2b 가 stub 으로 교체할 tool_result content 임계값 |

**Hermes Agent 권장 (50% primary + 85% safety net) 보다 보수적인 90% 단일 threshold 채택.** 이유:

- **단일 사용자 + DGX Spark 환경**. LLM 요약은 비용 이슈가 아니라 latency 자원. 너무 자주 압축하면 매 turn 에 추가 STW (Stop-The-World) latency.
- **Cheap pruning 이 매 turn 발화** (Tier 2 + Tier 2b). 토큰 누적이 천천히 — 90% 까지는 cheap pass 가 흡수. LLM 요약은 정말 큰 누적에서만 발화.
- **Safety net 역할**: `gateway-go/internal/pipeline/chat/compact_guard.go` 의 anti-thrashing guard 가 budget 초과 + 압축 불가능 상황에서 `compression_stuck` 으로 fallback. Hermes 의 85% safety net 과 같은 의도 (지속 LLM 호출 방지) 를 다른 위치에서 실현.
- **Tier 1 LLM → Tier 3a Embedding+MMR → Tier 3b Recency** 의 fallback 체인이 LLM 호출 실패 시 더 cheap 한 방식으로 graceful degradation. 즉 90% 도달 후에도 비싼 호출 강제 X.

### Cheap pruning (LLM 호출 전 단계, P2)

LLM summarizer 를 부르기 전에 두 단계 cheap pruning 이 항상 발화한다 (`compaction/polaris.go:Compact`):

1. **Tier 2 — `MicroCompact`** (`compaction/micro.go`): 4 turn 이전의 tool_result 의 fenced code block 만 `[code omitted]` 로 교체. 30-60% 토큰 절감 가능, 정보 손실 작음.
2. **Tier 2b — `TruncateOldToolResults`** (`compaction/restore.go`): 같은 cutoff 의 tool_result 중 content 가 256 runes 초과인 블록 통째 `[older tool output cleared to save context]` placeholder 로 교체. MicroCompact 가 이미 줄여놓은 짧은 결과는 자동 패스. CJK rune-count 기반 (byte-count 아님).

두 단계 모두 LLM 호출 X. Hermes Agent 의 Phase 1 cheap pruning 패턴. Tier 2b 가 발화하면 `polaris: stubbed old tool results count=N` 로 logger 에 기록.

### Bounded digestion — LLM tier 의 실행 형태 (P7, 2026-06-11)

`compaction/llm.go` 의 청크 요약은 **턴당 유한한 양만 소화**한다. 2026-06-05~10 의 client:main 고착(요약 노드 1개 생기는 순간 `SkipLLMCompaction` 이 LLM tier 를 영구 차단 + 30청크 무제한 fan-out 이 공유 2분 deadline 에 전멸)을 푼 구조:

| 상수/동작 | 값 | 역할 |
|---|---|---|
| `maxChunksPerPass` | 4 chunks (≈80K tokens) | 한 압축 pass 가 요약하는 최대 청크 수. 나머지 backlog 은 raw 로 보존 → 다음 pass 가 이어서 소화 (DAG 에 leaf 노드 누적, Condense 가 병합) |
| per-chunk output cap | 1024–2048 tokens | 청크당 생성 시간(≈30–45s)이 deadline 의 지배 요인이라 출력 상한으로 묶음 |
| prefix tolerance | — | 청크 일부 실패 시 **연속 성공 prefix 만 채택** (커버리지는 gapless 必). 전체 폐기 대신 부분 진전 |
| ctx-expiry guard | — | deadline 에 잘린 partial text (`CollectStream` 이 nil error 로 반환) 를 실패로 취급 — 절단된 요약이 커버리지로 persist 되는 것을 방지 |
| fence 분리 | — | `polaris/engine.go` 가 주입된 요약 fence 를 분리해 보호하고 **나머지 raw 에는 LLM tier 를 계속 적용** (요약 존재 ≠ tier 차단). safety trim 도 fence 우선 보존 + post-trim 토큰을 `Result.TokensAfter` 에 반영 |

---

## 6. PR 체크리스트 (시스템 프롬프트/컨텍스트/캐시 관련)

새 코드가 `gateway-go/internal/pipeline/chat/prompt/` 나 context 생성 경로 또는 `BeforeAPICall` hook 을 건드리면:

- [ ] § 1 의 어느 슬롯에 영향? 새 슬롯이라면 표 업데이트
- [ ] **4-breakpoint 한도 안에서 동작하는가?** 새 cache_control 마커 추가 시 기존 슬롯 합산 검증
- [ ] 새 입력이 static 블록에 들어가면 캐시 키에 반영됐는가?
- [ ] `PromptCache` 우회 경로 없는가?
- [ ] 대화 중간에 발화하는 코드인가? 그렇다면 transcript 자체를 mutate 하지 않는가? (per-request copy 사용)
- [ ] `BeforeAPICall` hook 추가 시 nil-safe (non-Anthropic 에서 nil 반환) + per-request copy + 입력 mutate 금지인가?
- [ ] 슬래시 커맨드라면 `--now` 플래그 없이 cache 깨지 않는가?
- [ ] 라이브 테스트로 멀티턴(3+ 턴) 후 `cache_read_input_tokens` 가 예상대로 올라가는지 확인
- [ ] `system_prompt_drift_test.go` 또는 `cache_breakpoints_test.go` 에 새 입력의 invariant 추가

---

## 7. 추가 레퍼런스

- 구현 (시스템 블록 마커): `gateway-go/internal/pipeline/chat/prompt/system_prompt.go`, `prompt_cache.go`
- 구현 (트레일링 메시지 마커): `gateway-go/internal/pipeline/chat/cache_breakpoints.go`, `run_exec.go`, `run_provider.go:resolveAPIMode`
- 압축 정책: `gateway-go/internal/pipeline/chat/` (compaction 관련 파일 — `merge_window.go`, `compact_guard.go` 참조)
- Hermes 설계 소스: [Hermes Agent 심층 분석 보고서](../docs/research/hermes-agent-analysis.md) § "프롬프트 캐시 신성화"
- Hermes 공식 문서 (작업 시 자주 참조):
  - [Prompt assembly](https://hermes-agent.nousresearch.com/docs/developer-guide/prompt-assembly)
  - [Context compression and caching](https://hermes-agent.nousresearch.com/docs/developer-guide/context-compression-and-caching)
- Anthropic 공식 문서: [Prompt caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching) — ephemeral TTL 5분, **요청당 cache_control 최대 4개**

---

## 금지 (한 줄 요약)

- ❌ 과거 메시지 content 변경 (compaction + `BeforeAPICall` per-request copy 제외)
- ❌ 대화 중 툴셋 rebuild
- ❌ 매 요청 시스템 프롬프트 재구성
- ❌ `PromptCache` 우회
- ❌ static 블록에 per-request 변수 끼워넣기
- ❌ 슬래시 커맨드에서 `--now` 없이 캐시 무효화
- ❌ 한 요청에 cache_control 마커 5개 이상 (Anthropic 4-breakpoint 한도 초과 → 400)
- ❌ Dynamic 블록에 cache_control 부착 (매 턴 무용지물 + breakpoint 1개 낭비)
