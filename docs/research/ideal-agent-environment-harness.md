# 에이전트에게 이상적인 환경과 하네스 — 고찰

**Status:** research / reflection note
**Audience:** Deneb 운영자 + 차기 AI 세션
**Scope:** "에이전트가 가장 잘 작동하는 *환경(environment)* 과 *하네스(harness)* 는 무엇인가" 에 대한 원리 고찰.
**Methodology:** 네 좌표 대조 —
  (1) Deneb 자신의 런타임 (`pilot` 경량 워커, 세션 런타임 = 이 레포의 "Pi" 격, chat 실행 루프, autonomous/dreaming, Polaris 압축, 프롬프트 캐시 교리),
  (2) Hermes Agent (`docs/research/hermes-agent-analysis.md`, `docs/research/tool-interception-gap.md`),
  (3) 이 문서를 쓰고 있는 하네스 그 자체 — Claude Code on web (ephemeral container, MCP, subagent, deferred tools, plan mode, PR-as-output),
  (4) **Terminal-Bench 2.0 상위권 하네스** — ForgeCode, LangChain DeepAgents, Warp, Terminus/KIRA, 그리고 arxiv 의 terminal-native 에이전트 설계 논문 (§0.1, §10, §11, §12).

> **용어 정리 (이 고찰의 축).**
> - **환경(environment)** = 에이전트가 *사는 세계*: 컨테이너·파일시스템·도구라는 행위 가능성(affordance)·데이터(메일/일정/위키)·I/O 표면·네트워크 정책·영속성/휘발성.
> - **하네스(harness)** = 에이전트를 *시간에 걸쳐 돌리는 골조*: 프롬프트 조립·컨텍스트/캐시 관리·턴 루프·도구 디스패치·압축·라이프사이클 상태기계·재시도·루프 감지·데드라인·관찰성.
> - Terminal-Bench 문헌은 하네스를 다시 둘로 쪼갠다: **scaffolding**(첫 프롬프트 *전* 조립 — 시스템 프롬프트·툴 스키마·서브에이전트 레지스트리) + **harness**(런타임 오케스트레이션 — 디스패치·컨텍스트·안전·세션 영속). 즉 *조립 시점 결정* 과 *실행 시점 관심사* 의 분리.
>
> 환경은 "어디 사는가", 하네스는 "어떻게 생각하고 행동하는가". 둘은 별개지만 서로를 제약한다. 좋은 환경이 나쁜 하네스를 구하지 못하고, 그 역도 같다.

> **"Pi" 에 대한 주석.** 이 레포 어휘에서 "Pi" 는 디스크에 사는 세션/에이전트 런타임(`~/.deneb/sessions/`, `.claude/rules/collaboration.md`)을 가리키고, `gateway-go/internal/pipeline/pilot/` 은 그 위에서 도는 경량 서브에이전트(워커)다. 외부의 다른 "Pi" 제품을 의도했다면 이 고찰은 Deneb 의 pilot/세션 런타임을 그 자리에 대입해 읽으면 된다 — 원리는 동일하다.

---

## 0. 한 줄 결론 (TL;DR)

> **이상적 *환경* 은 에이전트의 세계를 *읽을 수 있게(legible)* 만들고 그 행동을 *되돌릴 수 있거나 명시적이게(reversible-or-explicit)* 만든다.
> 이상적 *하네스* 는 컨텍스트와 추론을 *배급(ration)* 하고, 자유 루프 대신 *구조화된 워크플로우(Plan→Build→Verify→Fix)* 로 돌며, 모든 루프를 *유계(bounded)* 로 만들고, 자신의 무게를 *작업에 맞춘다(right-size)* — 그리고 이 모든 것을 규율하는 단 하나의 법은 *prefix 캐시 불변(immutability of the past)* 이다.**

| # | 원리 | 수렴 증거 (둘 이상에서 독립 도달) |
|---|---|---|
| 1 | 하네스의 본질은 컨텍스트 경제의 관리다 | Deneb Polaris + Hermes 압축 + arxiv staged compaction |
| 2 | prefix 캐시 불변은 협상 불가능한 법이다 | Deneb prompt-cache 교리 + Hermes "캐시 신성화" + arxiv "reminder as user-role" |
| 3 | 하네스 무게는 작업 무게에 비례한다 | Deneb pilot/chat/dreaming + TB multi-model routing(5 roles) |
| 4 | 환경은 영속성/휘발성·legibility·되돌리기를 명시한다 | Deneb(영속) vs Claude Code(휘발) + TB LocalContext·shadow-git |
| 5 | 도구 표면은 정적이되 노출은 지연된다 | Deneb fetch_tools + 임베딩 라우팅 + ToolSearch + TB lazy MCP |
| 6 | 자율성 = 트리거 + 멱등성 + 알림 | Deneb autonomous.Service / dreaming / genesis |
| 7 | 관찰성이 곧 디버깅성·최적화성이다 | Deneb logging+optimization + Hermes trace + TB Trace Analyzer |
| 8 | 루프는 종료조건과 안전판으로 정의된다 | Deneb concurrency + tool-loop detector + TB doom-loop middleware |
| 9 | 출력 표면은 환경의 일부다 | Deneb 네이티브 렌더 vs Claude Code PR/diff |
| 10 | **자유 루프보다 구조화된 워크플로우 (종료 전 검증 강제)** | **TB: LangChain Plan→Build→Verify→Fix, Forge/arxiv 6-phase** |
| 11 | **추론도 배급한다 (reasoning sandwich, max ≠ best)** | **TB: LangChain 53.9%(max) < 66.5%(sandwich)** |

아래 본문은 위를 풀어쓴다. 각 원리는 *독립적으로 같은 답에 수렴* 한 것만 골랐다 — 한 시스템의 취향이 아니라 구조적 압력의 증거이기 때문이다.

---

## 0.1 경험적 닻 — "하네스는 모델만큼 중요하다"

고찰 전체의 정당화는 이 한 가지 사실에 매달려 있다: **같은 모델로도 하네스만 바꾸면 성능이 모델 세대 차이만큼 움직인다.**

- **LangChain DeepAgents (Terminal-Bench 2.0):** 모델(`gpt-5.2-codex`)을 *고정* 한 채 하네스만 개선해 **52.8% → 66.5% (+13.7pt)**. 모델은 한 글자도 안 바꿨다.
- **리더보드 일반 패턴:** 같은 모델이 감싸는 에이전트에 따라 점수가 갈리며, scaffolding 품질이 raw 모델 능력 위에 **+2~6pt** 를 얹는다 (Claude Opus 4.6: Terminus-KIRA 74.7% vs TongAgents 71.9%). 업계 표현으로 *"성능의 70% 가 모델 바깥에 산다."*
- **반례적 정렬:** 추론을 *최대* 로 켜는 것은 오히려 **악화** (max 53.9% < sandwich 66.5%) — 더 똑똑한 설정이 아니라 *더 잘 배급된* 설정이 이긴다 (§11).

함의: **에이전트 품질 투자에서 한계 수익이 가장 높은 곳은 종종 모델 교체가 아니라 하네스다.** 이 고찰의 9+2 원리는 그 하네스 투자를 어디에 쓸지에 대한 지도다. Deneb 의 단일 사용자·로컬 추론 환경에서는 모델 선택지가 제한적이므로 이 함의가 더 강하다 — *우리가 통제할 수 있는 변수는 대부분 하네스다.*

---

## 1. 하네스의 본질은 컨텍스트 윈도우의 *경제* 를 관리하는 것이다

에이전트 하네스에서 가장 희소한 단일 자원은 **컨텍스트 윈도우** 다. 캐시 교리, 압축 tier, frozen snapshot, lazy tool loading, recall preflight — 표면상 무관해 보이는 이 모든 장치가 사실은 *하나의 예산을 배급하는* 같은 일을 한다.

이상적 하네스는 컨텍스트를 "프롬프트 문자열" 이 아니라 **관리되는 경제** 로 다룬다:

- **안정 prefix** — 캐시 히트를 위한 byte-stable 머리 (§2).
- **계단식 축출(tiered eviction)** — 싼 가지치기 먼저, 비싼 LLM 요약은 나중에. Deneb Polaris: Tier 2 `MicroCompact`(코드블록만 제거) → Tier 2b `TruncateOldToolResults` → *그제야* Tier 1 LLM 요약 → Tier 3a 임베딩+MMR → Tier 3b recency. Hermes "Phase 1 cheap pruning", arxiv 의 "staged progressive compaction(5-tier, 자동 트리거)" 과 동형 — **세 시스템이 같은 계단 구조에 도달.**
- **이중 메모리(arxiv):** episodic(전체 history) vs working(최근). 압축은 episodic 에만. Deneb 의 transcript(영속) vs 컨텍스트 윈도우 분리와 같은 발상.
- **불가축출 앵커(inevictable anchors)** — "내 이름은 X", "마감 6/15" 같은 핵심 사실은 압축 경쟁에서 빼낸다 (Hermes frozen MEMORY, Deneb 의 미구현 anchor/pinned-facts).
- **도구 결과 최적화(arxiv):** 타입별 요약(파일 내용 vs 셸 출력), 대용량은 외부 저장 + 참조, 잘림 힌트 제공. Deneb 의 `read_spillover`·`TruncateOldToolResults` 와 같은 방향.
- **지연 확장(lazy expansion)** — 처음부터 다 넣지 말고 필요할 때 끌어온다 (도구는 `fetch_tools`/ToolSearch, 스킬 본문은 on-demand `read`, 기억은 recall preflight).

핵심 통찰: **압축은 손실이 아니라 환율 정책이다.** "정보를 얼마나, 어떤 형태로, 언제 토큰으로 환전할 것인가" 를 정하는 것이 하네스의 지능이다.

---

## 2. prefix 캐시 불변은 *편의* 가 아니라 *법* 이다

prefix 캐싱을 채택하는 순간 **"과거는 변경 불가" 가 구조적 법칙이 된다.** Deneb `.claude/rules/prompt-cache.md` 교리와 Hermes "프롬프트 캐시 신성화" 가 *독립적으로* 도달한 같은 결론이다.

법칙이 강제하는 비직관적 귀결들:

- **타임스탬프는 day-only 로 정화** 하고, 정확한 시각은 *user 메시지 본문에 baking* 한다. 매 턴 바뀌는 값을 시스템 프롬프트에 두면 캐시가 매번 깨지므로.
- **`/steer`·도구 인터셉트는 transcript 를 mutate 하지 않고 per-request 사본에만** 가한다 (`BeforeAPICall` hook).
- **대화 중 툴셋 rebuild 금지** — 정적 블록 캐시 키가 정렬된 툴 이름 리스트이므로.
- **breakpoint 예산은 하드 한도(Anthropic 4개)** 이고, 이를 *테스트로 강제* 한다 (`gateway-go/internal/pipeline/chat/cache_breakpoint_budget_test.go`).

Terminal-Bench 수렴: arxiv 의 **"context-aware system reminders 를 user-role 메시지로 주입"** 이 정확히 같은 압력의 산물이다 — 시스템 프롬프트를 매 턴 다시 쓰면 캐시·길이 페널티를 먹으므로, 가변 가이드를 *프롬프트 머리가 아니라 메시지 꼬리* 에 넣는다. Deneb 의 user-message baking 과 판박이.

이상적 하네스는 이 불변을 **관례가 아니라 enforced invariant** 로 만든다 — drift 테스트, 예산 테스트, "슬래시 커맨드는 `--now` 없이는 캐시를 깨지 않는다" 같은 기본값. 즉 *에이전트의 역사는 append-only 이며, 유일하게 허가된 변형은 압축뿐* 이다 (§1). 이 단 하나의 제약이 나머지 원리에 규율을 부여한다.

---

## 3. 하네스의 무게는 작업의 무게에 *비례* 해야 한다

흔한 실수는 *하나의 무거운 에이전트 루프를 모든 일에 강제* 하는 것이다. Deneb 은 **하네스의 스펙트럼** 을 둔다:

| 결 | 무게 | 형태 | 예 |
|---|---|---|---|
| **Fire-and-forget 워커** (`pilot`) | 가벼움 | 스트리밍 루프 없음, `CollectStream` 1회 호출, 예산-aware 큐, fallback 체인, 좀비 취소 | 메일 분류, 결정트리, genesis 잡일 |
| **대화형 풀 에이전트** (chat loop) | 무거움 | 스트리밍, 풀 도구, 압축, recall, 루프 감지, 라이프사이클, 5분 데드라인 | 사용자 대면 턴 |
| **자율 백그라운드 루프** (dreaming/genesis) | 중간 | 트리거 기반, 멱등, notifier 백킹, 자체 cancelable goroutine | 메모리 합성, 스킬 진화 |

Terminal-Bench 수렴: 상위권 하네스는 **multi-model routing — 워크플로우별 5개 역할(execution/thinking/critique/vision/planning)** 에 각기 다른 모델을 배정한다. Warp 는 *실행은 Sonnet, 계획은 Opus* 로 갈랐다. Deneb 의 `modelrole`(main/lightweight/fallback) 과 같은 축 — **"작업의 결마다 다른 두뇌"** 다. 그리고 arxiv 의 핵심: *plan mode 를 상태 전이가 아니라 read-only 서브에이전트* 로 둔다(write 도구가 스키마에 아예 없음) → mode-lock 버그 제거. 이 역시 "무거운 단일 루프" 대신 "결에 맞는 가벼운 전용 골조" 라는 같은 원리.

**판별 기준:** 사용자가 *기다리는가*(→ 스트리밍·데드라인), 결과가 *영속 상태를 mutate 하는가*(→ 멱등성·알림), 컨텍스트가 *길게 누적되는가*(→ 압축). 세 답이 모두 "아니오" 면 pilot 급으로 충분하다.

---

## 4. 이상적 *환경* 은 영속성·legibility·되돌리기를 *명시* 한다

네 좌표가 양극단을 보여준다:

- **Deneb = 영속 환경.** 단일 머신 상주, transcript 디스크 영속, 위키 메모리, dreaming 이 세션을 위키로 합성. → 환경이 **무한 성장** 하므로 하네스는 GC(터미널 세션 1h evict), 라이프사이클 상태기계, *consolidation 경로(dreaming)* 가 필수.
- **Claude Code on web / Terminal-Bench = 휘발 환경.** ephemeral container, 매번 fresh clone, 비활동 시 회수. → **commit-or-lose.** 영속 출력은 PR/diff 뿐.

여기서 세 가지 환경 원리가 나온다:

1. **영속성 계약(명시적).** 에이전트는 자신이 어느 세계에 있는지 알아야 하고 "무엇이 살아남는가" 가 자명해야 한다. 휘발 환경 최악은 *모르고* 작업물을 컨테이너와 함께 잃는 것(→ 하네스가 push 강제로 드러냄). 영속 환경 최악은 *무엇이든 영원히 남아* 컨텍스트가 오염되는 것(→ GC·압축·consolidation 으로 *잊기* 제공).
2. **Legibility(읽을 수 있음).** Terminal-Bench 상위권은 시작 시 `LocalContextMiddleware` 로 **cwd·부모/자식 디렉터리·가용 도구(파이썬 설치 등)를 매핑** 한다 → 온보딩 탐색 오류 급감. 즉 *환경을 에이전트에게 먼저 그려준다.* Deneb 의 context-files 로더(AGENTS.md/CLAUDE.md/USER.md…)와 워크스페이스 컨텍스트가 같은 일 — "세계의 지도를 첫 턴에 손에 쥐여준다."
3. **되돌리기(reversibility).** arxiv 하네스는 **per-step shadow git snapshot** 으로 임의 시점 롤백을 제공한다 — 실패 복구가 프롬프트가 아니라 *결정적 연산* 이 된다. 이것이 TL;DR 의 "reversible-or-explicit" 의 구체형: 위험 행동은 *되돌릴 수 있게* 만들거나(스냅샷), 그게 안 되면 *명시적 승인* 을 받는다(§5 의 approval 층). Deneb 은 단일 사용자·git 레포 환경이라 "커밋 단위 되돌리기" 가 자연 제공되고, *세션 내 중간 롤백* 도 이제 구현돼 있다 — `pkg/checkpoint/` 가 매 `write`/`edit` 전 shadow 스냅샷을 떠 `/rollback [목록|비교|복원]` 으로 되돌린다. 단 **파일 내용 한정**(transcript·전송 메시지 등 side-effect 비포함)이고 `exec` 발 파일 변경은 아직 스냅샷되지 않는다 (2026-06-26 정정).

단일 사용자·단일 머신이라는 Deneb 의 철학적 단순화가 이 계약을 *깨끗하게* 만든다 — multi-tenant 였다면 격리·권한이 경계를 흐렸을 것이다. **제약이 곧 명료함이다.**

---

## 5. 도구 표면은 *정적* 이되 노출은 *지연* 된다

네 시스템이 독립적으로 같은 답에 도달했다: Deneb `fetch_tools`, improvement-ideas 의 embedding-aware routing, 이 하네스의 **deferred tools + ToolSearch**, 그리고 Terminal-Bench 상위권의 **lazy MCP discovery**("token-efficient extensibility — 필요할 때만 스키마에 나타남"). 결론은 동일하다 —

> **레지스트리(무엇이 존재하는가)는 고정이고, 프롬프트 노출(지금 모델이 무엇을 보는가)은 지연된다.**

이유는 두 압력의 교집합이다: (a) §2 캐시 법 — 툴셋이 런타임에 바뀌면 정적 캐시가 깨진다 → 레지스트리는 정적, 단 eager 하게 *시작 전* 조립(arxiv: first-call latency·race 제거). (b) §1 컨텍스트 경제 — 42개 스키마를 매 턴 다 보여줄 순 없다 → 노출은 지연. 해법: **작은 always-on hot set**(`fs/read`, `wiki/search`, `polaris/search`) + **lazy long tail**. 임베딩 라우팅을 한다면 *결정적 bucket* 으로 캐시 안정성을 보존.

추가로 arxiv 가 강조하는 **스키마 필터링**: 서브에이전트는 *자기가 못 쓰는 도구 정의를 아예 안 본다* ("the LLM never sees tool definitions it cannot use"). Deneb 의 preset 필터링과 같다 — capability creep 차단.

디스패치 자체(`docs/research/tool-interception-gap.md`): **단일 평면 lookup 이 정렬된 인터셉트 체인보다 낫다.** Hermes 는 todo→memory→clarify→… 순서 체인을 두지만, Deneb 은 모든 도구를 같은 map 에 두고 상태는 등록 시 closure 로 주입. 인터셉션은 (a) 선택적 pre-call hook(block 전용) + (b) post-processor 면 충분. 단 **안전은 다층(defense-in-depth)** 이어야 한다 — arxiv 의 5층(프롬프트 가드 → 스키마 제한 → 런타임 승인 → 툴 검증(DANGEROUS_PATTERNS·stale-read·timeout) → 사용자 훅). 그리고 **승인 피로 방지: 권한을 턴 간 영속 캐시** (Manual/Semi-Auto/Auto). Deneb 은 단일 사용자라 승인 층이 가볍지만, exec/web/send_file 의 위험 분류는 같은 원리로 둘 수 있다.

---

## 6. 자율성 = 트리거 + 멱등성 + 알림 (request/response 와 다른 도형)

능동형(비서) 측면 — dreaming, genesis, gmail poll, morning letter — 은 대화와 *근본적으로 다른 모양* 이다. 이상적 자율 루프의 4요소(Deneb `autonomous.Service` 가 정확히 이 형태):

1. **명시적 트리거** — 타이머(30분), 턴 카운터(`IncrementDreamTurn`), 임계값.
2. **멱등 효과** — 재실행이 상태를 오염시키지 않는다 (diary capsule dedup, recent-limit). 백그라운드는 중복 실행이 잦으므로 *멱등성이 정확성의 전제*.
3. **알림 경로(notifier)** — 결과가 사용자에게 돌아가는 길(proactive relay → 네이티브 세션 + push). 알림 없는 자율 = 사일런트 mutation = 디버깅 지옥.
4. **수명 관리** — 자체 cancelable goroutine + `defer recover()` + shutdown-linked ctx. 단 하나의 panic 이 프로세스를 죽이지 않게.

그리고 *제품* 제약 하나가 하네스에 인코딩되어야 한다: **"능동적이되 침해적이지 않게"** (over-notification 가드). 자율성의 난이도는 "무엇을 할 수 있는가" 가 아니라 **"언제 *안* 끼어드는가"** 다. 이상적 하네스는 개입 임계값을 *조정 가능한 1급 파라미터* 로 둔다. Deneb는 이 다이얼을 `agents.proactiveEscalateThreshold`(deneb.json)로 노출한다 — 낮추면 적극, 높이면 조용, 미설정 시 보정된 기본값(40). 전체 cadence editor UI는 아직 갭.

---

## 7. 관찰성이 곧 디버깅성이고, 디버깅성이 곧 최적화성이다

에이전트의 실패는 **기본적으로 조용하다** — 틀린 도구 선택, 누락된 컨텍스트, 캐시 미스, 삼켜진 `NO_REPLY`. 이상적 하네스는 자신의 실행을 *읽을 수 있게* 만든다:

- **사용자 무응답 사건은 무조건 `Error`** (Deneb `.claude/rules/logging.md`): replyFunc/media/push 영구 실패. 이 레포는 이런 사건이 `Warn` 에 파묻혔던 이력으로 이 교리를 세웠다.
- **재시도는 2단계 로깅** (transient `Warn` → 영구 `Error` + broadcast).
- **자기 성찰 도구** — `/status`, 캐시 히트율 대시보드, tool histogram, "왜 그 도구를 골랐나" 1줄 trace, `/explain`(improvement-ideas 미구현). 모두 "이 답이 어떻게 나왔나" 를 추궁 가능하게 한다.

Terminal-Bench 수렴 — *가장 인상적인 부분*: LangChain 은 **Trace Analyzer Skill** 로 *디버깅 루프 자체를 자동화* 했다 — LangSmith 트레이스를 가져와 **병렬 오류분석 에이전트** 를 띄우고 결과를 종합해 하네스 변경 지점을 콕 집는다. 이것이 Deneb `.claude/rules/optimization.md` 의 반복 루프(가설→1 atomic change→측정→keep/revert)를 *에이전트가 직접 도는* 형태다. 결정적 연결: **최적화는 metric 없이는 불가능하고, metric 은 관찰성 없이는 없다.** `iterate.sh` 의 `ITERATE_RESULT`/`DENEB_TEST_JSON` 가 그 관찰 신호다. **관찰성은 운영 편의가 아니라 자기개선의 전제조건.**

---

## 8. 루프는 종료 조건과 안전판으로 *정의* 된다

파국적 실패 모드 — 무한 도구 루프, KV 캐시를 쥔 좀비 goroutine, 뮤텍스 재진입 데드락 — 는 *모두 비종료* 이고 *모두 조용하다*. 이상적 하네스는 비종료에 대해 **편집증적** 이다. Deneb 이 실제 프로덕션 행(cron emit 재진입 데드락, tracks-process drain panic)을 겪고 세운 방어선:

- **모든 루프에 터미널 상태** — 턴 캡(기본 25, grace +1), 턴 데드라인(5분), `ctx.Done()`, 루프 감지(generic_repeat 30회 / poll_no_progress / ping_pong / circuit breaker), 압축 anti-thrashing 가드, abort grace(4h).
- **동시성 교리**(`.claude/rules/concurrency.md`): 뮤텍스 재진입 금지(`xxxLocked`), 2개↑면 lock hierarchy docstring, 콜백은 *snapshot-후-락해제*, 모든 장기 goroutine 에 `defer recover()` + `pkg/safego`, 채널 close 는 owner 만(`sync.Once`).
- **context 규율** — 사용자 응답 경로는 절대 `context.Background()` 금지, 항상 request ctx / `ShutdownCtx()` 파생.

Terminal-Bench 수렴: 상위권 전부가 **doom-loop detection** 을 둔다. LangChain 의 `LoopDetectionMiddleware` 는 *파일별 편집 횟수* 를 세다가 N회 초과 시 *"접근을 재고하라"* 를 주입한다(10+회 같은 실패 변주 반복 탈출). arxiv 도 "같은 행동 연속 반복 감지 → 자동 에스컬레이션(사용자 질문/플래너 스폰/중단) + iteration cap" — Deneb tool-loop detector 와 *완전 동형*. **세 시스템이 같은 함정을 같은 장치로 막았다.**

원리: **루프는 "무엇을 하는가" 가 아니라 "어떻게 *멈추는가*" 로 설계한다.** 시작 조건은 쉽다; 이상적 하네스는 종료 조건을 *먼저* 쓴다.

---

## 9. 출력 표면은 *환경의 일부* 다

에이전트의 "목소리" 는 말하는 장소에 의해 제약된다:

- **Deneb 네이티브 클라** — 풍부한 렌더(마크다운·카드·네이티브 리스트), 4096자 캡 없음. → 하네스는 streaming 을 *내부 UI 에만*, *최종 답만* 외부 전달, `NO_REPLY` 억제, 긴 응답 요약+펼침.
- **Claude Code web / Terminal-Bench** — PR/diff·터미널 상태가 출력. → "작업물 = 커밋된 변경/통과한 테스트" 이고 산문은 보조.

§4(영속성)와 §9(출력)는 한 쌍이다: *무엇이 살아남는가* 와 *어디로 나가는가* 가 환경을 규정한다. 이상적 하네스는 출력을 표면에 맞춰 *형상화(shape)* 한다 — streaming/partial 을 외부 메시징에 절대 안 보내고, silent-reply 억제, 표면 렌더 능력에 맞춘 포맷. 페르소나는 표면에 따라 *나뉘지 않되*(UI 분리 금지), 레이아웃·렌더는 표면에 *적응*.

---

## 10. (신규) 자유 루프보다 *구조화된 워크플로우* — 종료 전 검증을 강제하라

Terminal-Bench 상위권의 가장 강한 *공통* 발견: **비구조 ReAct 루프는 진다.** 명시적 단계 구조가 이긴다.

- **LangChain: Planning&Discovery → Build → Verify → Fix** 4단계. 핵심 통찰: *"에이전트는 기본적으로 코드를 쓰고, 다시 읽고, 멈춘다. 테스트는 자동으로 안 한다."* 그래서 **`PreCompletionChecklistMiddleware` 로 종료 전 검증을 강제** 했다 — 이것이 +13.7pt 의 큰 몫.
- **ForgeCode/arxiv: 6단계 per-iteration** — pre-check/compaction → thinking → self-critique → action → tool execution → post-processing. *thinking* 과 *self-critique* 를 행동에서 분리해 추론을 관찰·감사 가능하게.
- **시간 예산 인식:** 에이전트는 *시간을 과소평가* 한다 → 시간 경고를 주입해 무한 반복 대신 검증으로 유도.

이상적 하네스 함의: **루프에 골격을 박는다.** 특히 *"끝내기 전에 스스로 검증했는가"* 를 종료 게이트로 둔다 — LLM 의 기본 성향(쓰고 멈춤)을 구조로 교정. Deneb 매핑: chat 루프는 자유 ReAct 에 가깝다. Verify 게이트(예: 코드 변경 턴은 `make check`/테스트 통과를 *종료 조건* 으로)는 `.claude/rules/live-testing.md` 의 "라이브 검증 필수" 교리를 *하네스 레벨로 내재화* 하는 자연스러운 다음 수다. self-critique 단계는 Anthropic extended thinking 을 종료 직전 1회 "스펙 대조" 로 쓰면 캐시 영향 없이 얹을 수 있다.

> 주의: 단계 구조가 §3(right-size)와 충돌하지 않게. pilot 급 단발 작업엔 4단계가 과하다. *기다리는 사용자 대면 작업 + 검증 가능한 산출물* 일 때만 풀 구조를 입힌다.

---

## 11. (신규) 추론도 *배급* 한다 — "reasoning sandwich" (max ≠ best)

직관에 반하는 Terminal-Bench 데이터: **추론을 최대로 켜면 더 나빠진다.**

- LangChain: max 추론 **53.9%** < high 추론 **63.6%** < **reasoning sandwich 66.5%**.
- 패턴: **계획에 extra-high, 구현에 standard, 최종 검증에 다시 extra-high.** 양 끝(이해/검증)에 추론을 몰고 가운데(실행)는 가볍게.
- 왜 max 가 지는가: **타임아웃**. 모든 턴에 최대 추론 = 시간/토큰 예산 소진 = 작업 미완. 즉 추론 토큰도 §1 컨텍스트 경제와 같은 *유한 예산* 이다.

이상적 하네스 함의: **추론 깊이를 단계별로 변조** 한다. "항상 깊게" 가 아니라 "필요한 곳에만 깊게." 이것은 §3(작업별 모델)와 §10(단계 구조)의 교차점 — *어느 단계에서 얼마나 생각할지* 가 1급 튜닝 노브다. Deneb 매핑: `/think` 슬래시가 이미 추론 토글을 노출하지만 *수동* 이다. 이상은 단계 인식 자동 변조 — 계획/검증 턴은 자동 high, 단순 도구 실행 턴은 자동 low. modelrole + thinking 예산을 *워크플로우 위치의 함수* 로 두는 것.

---

## 12. 종합 — "이상" 의 형상

네 좌표를 겹쳐 보면 윤곽이 드러난다. 그것은 *더 많은 기능* 이 아니라 **더 적은, 그러나 규율된 제약** 의 시스템이다:

1. **컨텍스트를 경제로 다루는 하네스** — 안정 prefix + 계단식 축출 + 불가축출 앵커 + 지연 확장.
2. 그 경제를 규율하는 **단 하나의 법: 과거 불변(prefix 캐시).**
3. **작업에 맞춰 입는 하네스 스펙트럼** — pilot / chat-loop / 자율 루프 + 역할별 모델.
4. **영속성 계약 + legibility(세계 지도) + 되돌리기(스냅샷/명시 승인)** 가 명시된 환경.
5. **정적 레지스트리 + 지연 노출 + 평면 디스패치 + 다층 안전** 의 도구 표면.
6. **트리거·멱등·알림·수명** 으로 구성되고 *비침해성* 이 인코딩된 자율성.
7. **1급 관찰성** — 자기개선의 전제 (트레이스 자동 분석까지).
8. **종료 조건부터 쓰는 유계 루프** + 편집증적 동시성 + doom-loop 감지.
9. **표면에 형상화되는 출력.**
10. **자유 루프 대신 Plan→Build→Verify→Fix, 종료 전 검증 강제.**
11. **단계별로 변조되는 추론 예산(reasoning sandwich).**

그리고 경험적 닻(§0.1): **이 모두는 모델이 아니라 하네스에 사는 레버다** — 같은 모델로 +13.7pt. 메타 원리: **Deneb 의 단일 사용자·단일 머신·Korean-first·네이티브 단일표면 철학** 이 위를 *가능* 하게 한다. multi-tenant·다표면·i18n 을 *거부* 했기에 깨끗이 성립한다. 이상의 첫걸음은 새 기능이 아니라 **무엇을 안 할지 정하는 것**(`docs/research/improvement-ideas.md` §8) — *narrow scope, deep quality.*

---

## 13. Deneb 의 현재 좌표 — 이미 갖춘 것 / 다음 간극

| 원리 | Deneb 현황 | 다음 간극 |
|---|---|---|
| 1 컨텍스트 경제 | Polaris 5-tier ✅ | semantic-anchor 압축, anchor extraction |
| 2 캐시 불변 | 4-breakpoint 교리 + 예산 테스트 ✅ | 히트율 ops 대시보드 (위반 감지) |
| 3 하네스 스펙트럼 | pilot/chat/dreaming + modelrole ✅ | 단계별 모델 자동 배정 |
| 4 환경 계약 | transcript + GC + dreaming + context-files + **세션 내 중간 롤백**(`pkg/checkpoint`, `/rollback`) ✅ | reopen 라운드트립 테스트, 스냅샷 커버리지(`exec` 변경·side-effect 포함) |
| 5 도구 표면 | fetch_tools + 평면 레지스트리 + preset ✅ | embedding-aware routing, exec/web 위험 분류 |
| 6 자율성 | autonomous.Service 4요소 ✅ + 개입 임계값 config 노출 ✅ | full cadence editor UI |
| 7 관찰성 | logging 교리 + iterate metric ✅ | tool trace, `/explain`, **트레이스 자동분석 스킬** |
| 8 유계 루프 | concurrency 교리 + loop detector ✅ | — (실전 단련됨) |
| 9 출력 표면 | 네이티브 렌더 + silent-reply ✅ | 긴응답 요약/펼침 |
| 10 **구조화 워크플로우** | verify-gate 구현 ✅ (`verify_gate.go`: 변경 턴이 검증 없이 끝나면 종료 전 1회 넛지) | **이빨 부족** — 1회만 넛지·종료를 *차단* 안 함; 캡 상향 + finished-while-armed 카운터 |
| 11 **추론 배급** | sandwich *앞면* 구현 ✅ (`planningSandwichThinking`, opt-in) + effort-router | **반쪽** — turn0 만 부스트, 검증 턴 *뒷면* 재부스트 없음; sandwich↔router 가 modulator 를 상호 clobber → 통합 필요 |

**읽는 법 (2026-06-26 정정):** §10·§11·§4 는 이 문서(2026-06-03) 작성 *이후 구현됐다* — 더는 "빈 자리"가 아니라 **구현됐지만 얕다**. §10 verify-gate 는 종료를 차단하지 않고 1회만 넛지하고, §11 sandwich 는 앞면(계획 부스트)만 있고 검증 턴 뒷면이 없으며 effort-router 와 modulator 를 서로 덮어쓴다. 따라서 다음 무게중심은 "0 에서 새로 짓기"가 아니라 **반쪽 완성**(verify-gate 이빨·sandwich 뒷면·modulator 통합)과 **per-file edit-count loop breaker**(§8 의 마지막 미구현 doom-loop 변종)다.

---

## 14. 참고

- Deneb 런타임: `gateway-go/internal/pipeline/pilot/localai.go`(경량 워커), `gateway-go/internal/agentsys/agent/executor.go`(턴 루프), `gateway-go/internal/runtime/session/`(라이프사이클), `gateway-go/internal/agentsys/autonomous/`(자율 서비스).
- 교리(`.claude/rules/`): `prompt-cache.md`, `concurrency.md`, `logging.md`, `optimization.md`, `live-testing.md`, `go-gateway.md`.
- 관련 research: `docs/research/{hermes-agent-analysis,hermes-deneb-mapping,tool-interception-gap,improvement-ideas}.md`.
- 외부 — 캐시/압축: Hermes Agent [Prompt assembly](https://hermes-agent.nousresearch.com/docs/developer-guide/prompt-assembly), [Context compression and caching](https://hermes-agent.nousresearch.com/docs/developer-guide/context-compression-and-caching); Anthropic [Prompt caching](https://docs.anthropic.com/en/docs/build-with-claude/prompt-caching).
- 외부 — Terminal-Bench 하네스: [Terminal-Bench 2.0 리더보드](https://www.tbench.ai/leaderboard/terminal-bench/2.0); LangChain [Improving Deep Agents with harness engineering](https://www.langchain.com/blog/improving-deep-agents-with-harness-engineering) (모델 고정, 하네스만 52.8→66.5); arxiv [Building AI Coding Agents for the Terminal: Scaffolding, Harness, Context Engineering](https://arxiv.org/html/2603.05344v1); arxiv [Terminal-Bench](https://arxiv.org/pdf/2601.11868); [ForgeCode harness deep-dive](https://medium.com/@richardhightower/forgecode-dominating-terminal-bench-2-0-harness-engineering-beat-claude-code-codex-gemini-etc-eb5df74a3fa4).

---

## 15. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-06-03 | Claude | 초안 — 세 좌표(pilot/세션 런타임·Hermes·Claude Code web) 대조 고찰 |
| 2026-06-03 | Claude | Terminal-Bench 2.0 상위권 하네스(ForgeCode·LangChain·Warp·arxiv) 좌표 추가 — 경험적 닻(§0.1), 9개 원리에 수렴 근거 직조, 신규 원리 2개(§10 구조화 워크플로우, §11 추론 배급) |
| 2026-06-26 | Claude | §13 스코어카드·§4 정정 — §10 verify-gate / §11 reasoning sandwich / §4 세션 내 롤백은 이미 구현됨(얕은 상태). "다음 간극"을 "0 에서 짓기"에서 "반쪽 완성 + per-file loop breaker"로 재조준 |
