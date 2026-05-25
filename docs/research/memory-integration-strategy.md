# 메모리 통합 전략 (2.5-Layer)

**Status:** approved
**Scope:** wiki · polaris · graphify · hindsight · MemorySubsystem 의 역할 분담.

> **한 줄.** Wiki = 영구 truth. Polaris = 현 세션 staging. Hindsight = cue-gated semantic 검색.

---

## 모델

```
Wiki (영구 fact, dreamer 가 정제)
  ▲
  │ dreamer 가 승급
  │
Polaris (현 세션 압축 staging)

Hindsight (cue-gated 장기 semantic 검색)
  ↑ 사용자가 회상 의도 보일 때만 호출
```

- **Wiki**: 단일 사실 source. Dreamer 가 일지/polaris 요약에서 추출해 페이지로 정제.
- **Polaris**: 현재 대화의 LLM 압축 요약. 세션 종료 시 dreamer 가 fact 추출.
- **Hindsight**: hybrid semantic + BM25 + 서버 reranking. **Wiki BM25 가 못 잡는 의미 회상** (한국어 어휘 변형, 다른 표현으로 같은 개념) 을 cue 있을 때만 호출.
- **Graphify**: wiki 의 read-only graph view (현재 그대로).
- **MemorySubsystem**: 컨테이너 (현재 그대로).

---

## 통합 작업 (3개)

### 1. Hindsight 를 auto-recall → cue-gated 로 전환

- 매 턴 호출 → cue (회상 의도 단어) 가 있을 때만 호출
- 평균 latency 1.5s 절감, semantic 검색 능력 보존
- `recall_preflight.go` 에서 cue-gated 분기 안으로 이동

### 2. Wiki Tier1 페이지 → Polaris anchor

- importance ≥ 0.95 페이지 제목 (최대 5개) 을 polaris LLM summarizer 의 system prompt 에 anchor 키워드로 주입
- "이 키워드와 관련된 사실은 누락하지 말고 보존하라" soft hint
- Anchor 가 매칭된 메시지의 핵심 fact 가 압축 요약에서 사라지는 위험 감소

### 3. Dreamer 입력에 polaris 요약 추가

- Dreamer 가 raw 일지 외에 polaris 압축 요약 (사전 추출된 fact) 도 본다
- 일지 → polaris 압축 → dreamer 의 2-stage 정제로 fact 누락 감소
- `polaris.Store.RecentSummariesAcrossSessions(limit)` cross-session 조회 신규

---

## 비-목표 (이번 PR 범위 외)

- ❌ `memory.query` federated RPC
- ❌ 출처 태그 / 등급 / MMR
- ❌ 새 슬래시 명령
- ❌ Graphify temporal node / 1-hop 확장
- ❌ Entity-based compaction
- ❌ 세션 종료 시 hindsight retain (지금도 retain 발화)

---

## 참고

- `.claude/rules/prompt-cache.md` — anchor 는 transcript 영역, system prompt 와 무관
- `.claude/rules/concurrency.md` — wiki 동시 mutate (dreamer + 자율 task) 락 hierarchy
