# Claw-Anything ↔ Deneb: Always-On 비서 벤치마크 매핑 & 개선 탐구

**Status:** research / proposal backlog
**Audience:** Deneb 운영자 + 차기 AI 세션
**분석 기준:** *Claw-Anything: Benchmarking Always-On Personal Assistants with Broader Access to User's Digital World* (arXiv [2605.26086](https://arxiv.org/abs/2605.26086), Huawei · 베이징이공대 · 베이징대 · CAS-IA, 2026-05)
**일시:** 2026-06-06
**방법:** arXiv 본문 정독 + `gateway-go/` 능력별 전수 매핑 (Explore 에이전트) + 기존 research 노트 (improvement-ideas, hermes-deneb-mapping) 교차 검토

> **왜 이 논문인가.** Claw-Anything 은 Deneb 가 **정확히 그 범주에 속하는** 시스템 — "항상 켜져 있고(always-on), 사용자의 디지털 세계 전반에 폭넓게 접근하는 단일 개인 비서" — 를 평가하는 벤치마크다. 즉 이 논문의 실패 모드 분석은 곧 **Deneb 의 잠재 약점 진단표**다. SOTA(GPT-5.5)조차 34.5% pass@1, Pass^3(3회 연속) 20% 에 그쳤다는 결론은 "이 문제 도메인 자체가 미해결"임을 뜻하고, 우리가 어디에 투자해야 차별화되는지를 알려준다.

> **읽는 법.** 논문의 5대 발견을 각각 **논문 사실 → Deneb 현황(코드 근거) → 갭 → 제안(P/작업량)** 으로 정리했다. 채택 여부는 운영자 판단. 합의된 항목만 별도 PR.

---

## 0. TL;DR

| # | 논문 발견 | Deneb 현황 | 핵심 제안 | P |
|---|---|---|---|---|
| A | **Investigation–Execution Gap** — 맥락은 정확히 식별, 실행에서 실패 (지배적 실패 모드) | recall(분석)은 강함, 실행 후 검증 없음 | 실행-후 검증(post-action verify) 루프 + 실패 surface | P1 |
| B | **Proactive 4배 난이도** — 능동 6.7% vs 반응 25.9% pass@1 | autonomous(boot/heartbeat/dreaming/gmailpoll) 있음, but 트리거가 시간 기반 | 이벤트-스트림 이상탐지 기반 proactive 트리거 | P1 |
| C | **Long-horizon 열화** — 이력 길수록 성능↓ (있어도 못 씀) | recall = retrieval(BM25 top-3, cap 8), full-dump 아님 → **논문 비판을 이미 회피** | recall 랭킹 강화 + pinned/anchor facts | P2 |
| D | **Multi-service 충돌 → 행동 정지** — 불일치 정보 조화 실패 | tool dispatch 에 cross-service 일관성 검사 **없음** | 충돌 감지 → 사용자 surface (정지 대신) | P2 |
| E | **합성 데이터 파인튜닝 +23.7%p** — 구조 혁신 > 스케일링 | DGX Spark 로컬 추론 + 끊긴 chat live-test | Deneb 자체 eval 하네스(proactive/reactive 분리 측정) | P1 |

> **한 줄 결론.** Deneb 의 아키텍처는 논문이 지적한 두 약점(full-context dump, retrieval 부재)을 **이미 우회**하고 있다(C). 반면 논문이 "가장 어렵다"고 못박은 영역 — **proactive(B)** 와 **investigation-execution gap(A)** — 은 Deneb 의 핵심 차별점(비서실장 페르소나)과 정확히 겹치므로, 여기에 투자하면 SOTA 모델도 못 푸는 문제에서 우위를 만든다. 그러나 그걸 **측정할 수단(E)** 이 지금 없다(chat live-test 가 PR #1922 로 끊김).

---

## 1. 논문 요약 (Deneb 관점)

### 1.1 무엇을 평가하나

3개 차원에서 200개 태스크 (150 CLI-only, 50 CLI+GUI):

1. **장기 이벤트 스트림** — 3개월+ 세밀 활동 기록. 과제당 평균 **19.17만 단어** (기존 벤치마크 2K~12K 의 수십 배).
2. **상호의존 서비스** — 평균 10.1개 (최대 18개) 서비스 크로스 조정.
3. **다중 디바이스** — CLI + GUI 이종 인터페이스.

태스크는 **reactive(명시 요청)** vs **proactive(요청 없이 선제 발견·행동)** 으로 양분.

### 1.2 핵심 수치

| 항목 | 값 | 함의 |
|---|---|---|
| GPT-5.5 pass@1 | 34.5% | SOTA 도 신뢰 불가 |
| Claude Opus 4.7 / Sonnet 4.5 | 31.8% / 28.0% | 우리가 쓰는 라인도 동일 한계 |
| GLM-5.1 (오픈소스) | 31.7% | 클로즈드 갭 작음 |
| Pass^3 (3연속) | ~20% | **일관성** 이 진짜 벽 |
| reactive vs proactive | 25.9% vs **6.7%** | proactive 가 ~4배 어려움 |
| 이벤트 스트림 제거 시 | 21.0% → ~0% | 맥락 필수, but… |
| 이벤트 스트림 ↑ | 성능 지속 ↓ (Fig 5a) | …길면 오히려 못 씀 |
| GUI 제거 시 CLI+GUI 태스크 | ~2.0% | 이종 인터페이스 합성 난이도 |
| 합성 데이터 파인튜닝 | base +23.7%p | 구조적 개선 경로 |

### 1.3 저자들의 진단 (암시적 권고)

- 전체 맥락 입력은 비효율 → **동적 retrieval / 계층 요약** 필요.
- 서비스 간 **참조 일관성 검증 메타-도구** 필요.
- proactive 는 **주기 모니터링 + 이상탐지 + 선제 제안** 별도 능력으로 봐야 함.
- 단순 스케일링보다 **검색·계획·다중서비스 추상화** 의 구조 혁신.

---

## 2. 발견 A — Investigation–Execution Gap

> **논문.** 지배적 실패 모드는 "관련 맥락을 정확히 식별하면서도 그 이해를 성공적 행동으로 번역하는 데 실패". Claude Opus 는 "과도한 명확화 또는 루프 갇힘", Qwen 계열은 "실행 부정확성·소스 생략".

### Deneb 현황

이 갭은 Deneb 의 **비서실장 페르소나(업무분석 + 업무비서)** 구조와 정확히 대응한다 — "왜 지금 중요한가(investigation)" 와 "언제까지 처리(execution)" 가 한 응답에 나와야 한다는 CLAUDE.md 원칙이 바로 이 갭을 메우려는 설계다.

- **Investigation(강함):** recall preflight (`gateway-go/internal/pipeline/chat/recall_preflight.go:buildRecallPreflight`) 가 wiki/diary/polaris/transcript/hindsight 5소스를 합성. 맥락 식별은 잘 한다.
- **Execution(검증 부재):** tool dispatch (`gateway-go/internal/pipeline/chat/tools.go:ToolRegistry.Execute`) 는 도구를 실행하고 결과를 truncate/cache 할 뿐, **"의도한 효과가 실제로 났는지" 검증 단계가 없다.** 메일 보냄 → 정말 전송됐나? 일정 충돌 해결 → 정말 반영됐나? 를 turn 내부에서 다시 확인하지 않는다.
- **루프 갇힘 위험:** 논문이 Opus 의 약점으로 지목한 "과도한 명확화/루프"는 Deneb 에도 `compact_guard.go` 의 anti-thrashing 외엔 turn-level 루프 가드가 약하다.

### 갭

mutation 도구(메일/일정/wiki write)가 **fire-and-forget**. 실행 실패가 로그에만 남고 사용자 무응답으로 묻힐 수 있다 (`.claude/rules/logging.md` 의 "유저 무응답 = Error" 원칙이 도구 실패에는 일관 적용 안 됨).

### 제안

**A1. Post-action verification 루프 — P1 / M.**
mutation 도구에 선택적 `verify` 후처리를 `PostProcessRegistry` (`tools.go:192`) 로 추가. 예: `gmail.send` 후 sent 폴더 확인, `cron.create` 후 등록 재조회. 실패 시 같은 turn 에서 LLM 에 재시도 신호 + 사용자에게 명확한 실패 surface.
- 캐시 영향 없음 (도구 후처리는 메시지 본문 외부).
- `.claude/rules/logging.md` 규칙 2(재시도 2단계 로깅) + 규칙 3(broadcast) 적용.

**A2. Turn-level "execution budget" 가드 — P2 / S.**
한 turn 내 동일 도구 N회 반복 또는 명확화 질문 M회 초과 시 강제 수렴 (사용자에게 중간 상태 보고 후 진행). Opus 의 "루프 갇힘"을 구조적으로 차단.

**A3. Investigation→Execution 전이 trace — P2 / S.**
improvement-ideas 4.3("왜 그 도구를 골랐는지")과 통합. recall 로 식별한 맥락이 어떤 실행으로 이어졌는지 1줄 trace → 갭 발생 지점 가시화.

---

## 3. 발견 B — Proactive 4배 난이도 (Deneb 의 핵심 전장)

> **논문.** proactive 6.7% vs reactive 25.9%. proactive 는 "노이즈 스트림에서 문제 **자체를 발견**"해야 하고, 주기 모니터링 + 비정상 패턴 인식 + 의도 추론 + 선제 행동을 요구. → 가장 미해결 영역, "미래 모델 개선의 핵심 목표".

### Deneb 현황 (이미 상당히 앞서 있음)

Deneb 의 **업무비서 모드 = 정확히 proactive**. 그리고 SOTA 모델이 LLM 단일 호출로 6.7% 밖에 못 푸는 이 영역을, Deneb 는 **시스템 레벨 스캐폴딩**으로 우회한다:

- `gateway-go/internal/agentsys/autonomous/service.go:Service` — dreaming + periodic task 오케스트레이터. task 상태를 `~/.deneb/autonomous_state.json` 에 영속 → 24h/weekly 간격이 재시작 생존.
- `boot_task.go` (24h, 기동 시) — `~/.deneb/BOOT.md` 기반 선제 turn.
- `heartbeat_task.go` (30min, 08:00–23:00 KST) — `~/.deneb/HEARTBEAT.md` 기반 주기 체크.
- `gmailpoll` — 신규 메일 주기 분석.
- proactive relay (`server_rpc_session.go:proactiveRelayDeps`) — LLM 재합성 없이 `client:main` 세션에 보고 전달.

즉 **"주기 모니터링"(논문 요구 1) 은 이미 있다.** Deneb 가 SOTA 보다 유리한 지점.

### 갭 (논문이 정확히 찌르는 부분)

Deneb 의 proactive 트리거는 **시간 기반(매 30분, 매 24h)** 이지 **이벤트-신호 기반이 아니다.** 논문이 강조한 "노이즈 스트림에서 **비정상 패턴 발견**"(요구 2)·"의도 추론 후 선제 제안"(요구 3) 은 약하다. heartbeat 가 돌긴 도는데, *무엇이 비정상인지 판단하는 신호 레이어*가 HEARTBEAT.md 의 자유서술에 위임돼 있다.

### 제안

**B1. 이벤트-스트림 이상탐지 신호 레이어 — P1 / M. 🚧 1차 착수됨 (이 PR).**
> **구현 현황:** 순수 신호 엔진 `gateway-go/internal/agentsys/autonomous/signal.go`
> (`DetectSignals` — VIP 미응답 메일·일정 충돌·임박 일정·마감 임박을 가중 점수화,
> exhaustive 단위 테스트) + heartbeat 가산 훅(`heartbeat_task.go`, nil-safe·기존
> HEARTBEAT.md 체크를 **억제하지 않고** 신호 요약을 앞에 덧붙임) + 캘린더 수집기
> (`heartbeat_signals.go`, OAuth 없으면 무신호로 graceful). **signals-only 트리거 추가됨:**
> 빈 HEARTBEAT.md 에서도 escalation-worthy 신호가 있으면 proactive turn 을 발화한다
> (`heartbeatShouldRun`/`composeHeartbeatBody`, 순수 함수·단위 테스트). **남은 것:** 메일 VIP
> 수집기(위키-VIP 룩업)·deadline 수집기, DGX 호스트 실데이터 임계 튜닝.

heartbeat turn 이 매번 "전체를 다시 읽는" 대신, 경량 신호 추출 패스를 먼저:
- 마지막 체크 이후 **delta** (새 메일/일정 변경/마감 임박/응답 지연 스레드) 만 후보로 모음.
- "비정상" 룰: VIP 발신자 + 미응답 N시간 / 일정 충돌 / 마감 D-1 / 평소 패턴 이탈.
- 신호가 있을 때만 full agent turn 발화 → over-notification 금지 (CLAUDE.md "능동적이되 침해적이지 않게") 와 정합.
- **어디서:** `autonomous/` 에 `signal.go` 추가, gmailpoll 의 priority 점수(improvement-ideas 5.2)와 통합.

**B2. Proactive 성공률 자체 측정 — P1 / S (E와 묶음).**
논문의 reactive/proactive 분리 측정을 Deneb eval 하네스에 이식 (§6). proactive 가 Deneb 의 차별점인데 **회귀를 측정할 수단이 현재 0.** "선제 알림이 유용했나" 를 추적 못 하면 개선도 못 한다.

**B3. 선제 제안의 "실행 가능한 형태" 강제 — P2 / S.**
proactive 보고는 분석으로 끝나지 말고 **즉시 실행 액션**을 동반해야 한다 (논문의 investigation-execution gap 이 proactive 에서 더 심함). 네이티브 클라 액션 버튼(예: "충돌 해결" / "초안 보기")으로 한 탭 실행. 발견 A1 의 verify 루프와 결합.

---

## 4. 발견 C — Long-Horizon 열화 (Deneb 가 이미 우회 중)

> **논문.** 이벤트 스트림 제거하면 21%→0% (맥락 필수). 그러나 **있어도 길수록 성능 하락**(Fig 5a). 전체 19만 단어를 통째 입력하는 방식이 비효율. → 동적 retrieval / 계층 요약 권고.

### Deneb 현황 (논문 권고를 선반영)

이건 Deneb 가 **논문보다 앞선** 영역이다. Deneb 는 처음부터 full-dump 를 안 한다:

- **Retrieval-over-dump:** recall preflight 가 BM25 로 wiki top-3 + diary top-3 + polaris + transcript 를 검색해 **8행으로 cap** (`recall_preflight.go`, cue fingerprint 로 cross-topic 오염 차단). 논문이 "권고"한 동적 retrieval 을 이미 구현.
- **계층 요약:** compaction 3-tier (`compaction/polaris.go:Compact`) — Emergency(30K) → Micro(코드펜스 제거) → Stub(256룬 초과 tool_result) → LLM 요약(90% threshold, 20% target) → Embedding+MMR → Recency. 논문이 권고한 "계층 요약"의 정교한 구현체.
- **frozen snapshot:** recall 을 세션 첫 evidence turn 1회만 build → latency 절감 (`.claude/rules/prompt-cache.md` §3.5).

### 갭

논문이 동시에 지적한 "**중요도 가중치를 학습하지 못한다**"는 Deneb recall 에도 남아 있다:

- recall 랭킹이 BM25 score + recency 뿐 (`recall_preflight.go:120-125`). 사용자가 명시한 **앵커 사실**("내 호칭은 부장님", "마감 6/15")이 일반 메시지와 동일하게 경쟁 → 긴 이력에서 밀려날 수 있다.
- top-3/cap-8 이 **고정** → 19만 단어급 컨텍스트에서 recall 이 너무 인색할 수 있음(논문 규모 대비).

### 제안

**C1. Pinned / anchor facts — P2 / M.**
improvement-ideas 4.6(pinned facts) + 2.3(semantic-anchor) 을 이 발견이 **실증적으로 정당화**. 사용자가 `/pin` 한 사실 또는 dreamer 가 추출한 앵커를 recall 에서 **inevictable** 처리, Dynamic 블록 끝에 항상 prepend. 논문 Fig 5a 의 "길수록 하락"을 앵커 보존으로 완화.
- 캐시 영향: Dynamic 블록(마커 없음)이라 미미, trailing marker 충돌만 검증.

**C2. Adaptive recall depth — P3 / M.**
recall cap 을 컨텍스트 길이/cue 강도에 비례해 동적 조정 (짧은 세션 8, 장기 이력 cue 강함 → 더 깊게). 단 토큰 예산·latency 와 trade-off, 측정(E) 선행 필수.

**C3. 논문 검증 재확인 — P2 / S.**
improvement-ideas 3.1(Polaris reopen 라운드트립 테스트)이 이 발견의 직접 리스크. 압축+재오픈 시 앵커 사실 손실 = "왜 갑자기 내 이름 까먹지" → C1 과 함께 테스트.

---

## 5. 발견 D — Multi-Service 충돌 시 행동 정지

> **논문.** 필요 도구 마스킹하면 성공률 ~0 붕괴 (Table 3). fixture 충돌↑ → 성공률 단조 감소 (Fig 7c). 모델이 **불일치 정보를 "조화"시키지 못하고 행동을 정지.** → 서비스 간 참조 일관성 검증 메타-도구 권고.

### Deneb 현황

- tool dispatch (`tools.go:ToolRegistry.Execute`) 는 flat registry, 단일 디스패치. **cross-service 일관성 검사 없음.** 각 도구 독립.
- `PostProcessRegistry` 로 name-matched 후처리는 가능하나 **글로벌 일관성 체커는 없다.**
- 충돌 해소는 전적으로 agent turn 로직(도구 순서, steering)에 위임 — 즉 논문이 "모델이 못 한다"고 한 바로 그 LLM 능력에 의존.

### 갭

예: wiki 의 "거래처 X 담당자 = 김부장" vs gmail 최근 스레드의 "담당자 = 이과장" 이 충돌할 때, Deneb 는 이를 감지·surface 하는 레이어가 없다. LLM 이 알아서 조화시키길 기대 → 논문에 따르면 여기서 정지하거나 틀린 쪽을 택한다. 단일 사용자 KB(wiki) + 외부 서비스(gmail/calendar) 가 점점 늘면 충돌 빈도↑.

### 제안

**D1. 충돌 surface (정지 대신 보고) — P2 / M.**
recall preflight 가 여러 소스에서 **같은 엔티티에 상충하는 사실**을 모았을 때, 이를 합치지 말고 `<recall-context>` 에 명시 태깅 ("⚠️ 출처 불일치: wiki=김부장 / 최근메일=이과장"). LLM 이 조화 실패로 정지하는 대신, 사용자에게 명확히 묻거나 최신 출처 우선 룰 적용.
- **어디서:** `recall_preflight.go` 의 evidence 병합 단계에 엔티티-레벨 충돌 감지.

**D2. 가벼운 참조 일관성 체크(메타-도구) — P3 / L.**
논문 권고의 직접 구현. wiki write 시 기존 사실과 모순되면 dreamer/verification 단계에서 플래그. 단 복잡도 높음(L), D1 의 read-side 충돌 surface 가 먼저.

**D3. GUI/CLI 합성은 비-제안 — N/A.**
논문 발견 중 "CLI+GUI 협력"(GUI 제거 시 ~2%) 은 Deneb 에 **해당 없음.** Deneb 는 네이티브 클라가 구조화 capture(image/audio/contacts)를 RPC 로 보낼 뿐, **원격 GUI 자동화 레이어가 없다**(`miniapp_bridge.go`, 화면 제어 불가). 논문의 GUI 차원은 Deneb 의 단일-표면 철학(PR #1922)과 어긋나므로 도입하지 않는다. 이건 갭이 아니라 의도적 범위 축소.

---

## 6. 발견 E — 측정 수단 부재 (가장 시급한 메타-문제)

> **논문.** 합성 데이터 2,000 환경 → base 모델 +23.7%p. 자동 데이터 생성 파이프라인 + reactive/proactive 분리 평가가 개선의 엔진.

### Deneb 현황 (치명적 공백)

**Deneb 의 채팅 기반 라이브 테스트가 PR #1922 로 끊겨 있다.** (`.claude/rules/live-testing.md`, CLAUDE.md 명시): 목 텔레그램 주입 경로가 죽어서 `chat`/`quality`/`chat-check` 가 동작 안 함. 현재 enforced 게이트는 `make check`(빌드+단위) + `smoke`(HTTP /health) + `logs-errors` 뿐.

즉 **A~D 의 어떤 개선도 "실제로 좋아졌나"를 측정할 수단이 없다.** 논문이 증명한 "측정 → 합성데이터 → 파인튜닝" 루프의 1단계조차 막혀 있다. DGX Spark 로컬 추론 환경은 논문의 파인튜닝 경로(+23.7%p)에 이상적인데, 측정이 없어 활용 못 한다.

### 제안

**E1. 네이티브 주입 경로로 chat live-test 재작성 — P1 / M.** ★최우선★
`.claude/rules/live-testing.md` 가 "후속 과제"로 남긴 것. `miniapp.chat.send` (SendSync, `miniapp_bridge.go`) 가 동기 응답을 반환하므로, 목 텔레그램 대신 **이 RPC 에 직접 주입**하면 chat/quality 테스트가 부활한다. 모든 후속 개선(A~D)의 선결조건.

**E2. Claw-Anything 식 Deneb mini-benchmark — P1 / L.**
논문 방법론을 Deneb 표면에 이식한 소규모 평가셋:
- **reactive vs proactive 분리 측정** (논문 핵심 축). proactive 점수가 Deneb 차별점인데 지금 0 측정.
- **investigation-execution 분해** — recall hit 했는데 실행 실패한 케이스 비율.
- **Pass^k 일관성** — 논문이 진짜 벽이라 한 일관성. 같은 시나리오 3회 → 안정성.
- 실데이터 대신 **고정 fixture 세션**(가짜 메일/일정/wiki)으로 재현 가능하게.
- **어디서:** `docs/research/ideal-agent-environment-harness.md` 의 하네스 구상과 통합, `scripts/dev/` 에 평가 러너.

**E3. 합성 trajectory 수집 → 로컬 파인튜닝 (장기) — P3 / L.**
논문의 +23.7%p 경로. E2 의 fixture 환경에서 성공 궤적을 수집 → DGX Spark 의 lightweight 모델(`modelrole`) 파인튜닝. 단 메인 챗 LLM(Claude/외부)은 파인튜닝 불가 → **gmailpoll/genesis/pilot 등 로컬 잡일꾼 모델**의 proactive 신호 판단(B1) 정확도 개선에 적용. 현실적 타깃.

---

## 7. 우선순위 종합

### Now (P1) — 측정 복구 + 핵심 전장
- **E1** chat live-test 네이티브 재작성 ← *모든 것의 선결*
- **E2** reactive/proactive 분리 mini-benchmark
- **A1** post-action verification 루프
- **B1** 이벤트-스트림 이상탐지 신호 레이어

### Next (P2)
- **A2** turn-level execution budget 가드
- **A3** investigation→execution trace (improvement-ideas 4.3 통합)
- **B3** proactive 제안의 실행가능 형태 강제
- **C1** pinned/anchor facts (improvement-ideas 4.6/2.3 통합)
- **C3** Polaris reopen 라운드트립 테스트 (improvement-ideas 3.1)
- **D1** 충돌 surface

### Later (P3)
- **C2** adaptive recall depth
- **D2** 참조 일관성 메타-도구
- **E3** 합성 trajectory → 로컬 모델 파인튜닝

---

## 8. 명시적 비-제안 (Out of Scope)

- 🚫 **GUI 원격 자동화 (논문 D 차원).** Deneb 는 구조화 capture RPC 만 (`miniapp_bridge.go`), 화면 제어 없음. 단일-표면 철학(PR #1922) 위반 + attack surface. 논문의 CLI+GUI 협력 점수(~2%)는 Deneb 에 무관.
- 🚫 **메인 챗 LLM 파인튜닝.** Claude/외부 provider 는 파인튜닝 불가·비현실. E3 는 로컬 잡일꾼 모델 한정.
- 🚫 **18개 서비스로 확장.** 논문은 광범위 접근의 *어려움*을 보였을 뿐, 더 붙이라는 게 아니다. Deneb 는 narrow scope deep quality (CLAUDE.md).
- 🚫 **over-notification.** B1 의 신호 레이어는 "비정상일 때만" 발화. 침해적 알림은 페르소나 위반.

---

## 9. 핵심 통찰 (운영자용 한 단락)

논문은 "always-on 비서는 SOTA 도 34.5%, 3연속 20% 인 미해결 문제"라고 못박았다. 그런데 Deneb 가 **이미 잘하는 것**(retrieval-over-dump, 계층 압축, 주기 모니터링 스캐폴딩)은 논문이 "권고"한 방향이고, Deneb 가 **약한 것**(실행 검증, 이벤트-신호 기반 proactive, 충돌 surface)은 논문이 "제일 어렵다"고 한 것과 정확히 겹친다. 둘 다 Deneb 의 비서실장 페르소나의 본질이다. **문제는 이 모든 걸 측정할 수단(chat live-test)이 PR #1922 이후 끊겨 있다는 것** — 그래서 E1(측정 복구)이 단일 최우선이고, 그 위에서 B(proactive)·A(execution)에 투자하면 SOTA 모델도 못 푸는 영역에서 시스템-레벨 우위를 만든다.

---

## 10. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-06-06 | Claude | 초안 — Claw-Anything (arXiv 2605.26086) 5대 발견 → Deneb 매핑 + 개선 제안 |
| 2026-06-07 | Claude | E1(네이티브 chat live-test 복구) + B1 1차(proactive 신호 엔진 + heartbeat 가산 훅 + 캘린더 수집기) 착수 |

---

## 11. 참고

- 논문: [arXiv 2605.26086](https://arxiv.org/abs/2605.26086) · [HTML](https://arxiv.org/html/2605.26086) · 국내 보도 [AI Matters](https://aimatters.co.kr/news-report/43380/)
- 관련 벤치마크: [π-Bench (2605.14678)](https://arxiv.org/html/2605.14678v3) — proactive long-horizon 평가 (B 발견 보강 참고)
- Deneb research: `docs/research/{improvement-ideas, ideal-agent-environment-harness, hermes-deneb-mapping, memory-integration-strategy}.md`
- 코드 근거: `gateway-go/internal/agentsys/autonomous/`, `internal/pipeline/chat/recall_preflight.go`, `internal/pipeline/compaction/polaris.go`, `internal/pipeline/chat/tools.go`, `internal/runtime/rpc/handler/chat/miniapp_bridge.go`
- 도메인 규칙: `.claude/rules/{live-testing, prompt-cache, logging, optimization}.md`
