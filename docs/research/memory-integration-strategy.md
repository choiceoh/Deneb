# 메모리 통합 전략 (2.5-Layer)

**Status:** historical / partially landed (Hindsight retired 2026-06-15)
**Scope:** wiki · polaris · graphify · knowledge/recall · MemorySubsystem 의 역할 분담.

> **한 줄 (현재).** Wiki = 영구 truth. Polaris = 현 세션 staging. 장기 회상 = wiki/diary/polaris + cue-gated `recall/` preflight. Hindsight 서비스는 은퇴.

---

## 모델 (현행)

```
Wiki (영구 fact, dreamer 가 정제)
  ▲
  │ dreamer 가 승급
  │
Polaris (현 세션 압축 staging)

recall/ (cue-gated preflight — wiki/diary/polaris|transcript/file/org 병렬)
  ↑ 사용자가 회상 의도 보일 때만 호출
```

- **Wiki**: 단일 사실 source. Dreamer 가 일지/polaris 요약에서 추출해 페이지로 정제.
- **Polaris**: 현재 대화의 LLM 압축 요약. 세션 종료 시 dreamer 가 fact 추출.
- **Recall preflight**: cue-gated 검색 (`gateway-go/internal/pipeline/chat/recall/recall_preflight.go`). Hindsight FastAPI 레이어는 제거됨 — 의미 회상은 wiki BM25 + diary/polaris 원문 검색이 담당.
- **Graphify**: wiki 의 read-only graph view (현재 그대로).
- **MemorySubsystem**: 컨테이너 (현재 그대로).

---

## 통합 작업 (3개) — 진행 상태

### 1. ~~Hindsight 를 auto-recall → cue-gated 로 전환~~ → **Hindsight 은퇴 + cue-gated recall**

- 원안: 매 턴 Hindsight → cue 있을 때만
- **랜딩:** Hindsight 클라이언트·retain·프롬프트 블록 전부 제거(2026-06-15). cue-gated 분기는 `recall_preflight.go` 에 남음.
- 장기기억은 **wiki(큐레이션)+diary(원문)+polaris(세션)** — `docs/agent-rules/sidecar-models.md` Hindsight 은퇴 절 참조.

### 2. Wiki Tier1 페이지 → Polaris anchor

- importance ≥ 0.95 페이지 제목 (최대 5개) 을 polaris LLM summarizer 의 system prompt 에 anchor 키워드로 주입
- "이 키워드와 관련된 사실은 누락하지 말고 보존하라" soft hint
- Anchor 가 매칭된 메시지의 핵심 fact 가 압축 요약에서 사라지는 위험 감소

### 3. Dreamer 입력에 polaris 요약 추가

- Dreamer 가 raw 일지 외에 polaris 압축 요약 (사전 추출된 fact) 도 본다
- 일지 → polaris 압축 → dreamer 의 2-stage 정제로 fact 누락 감소
- `polaris.Store.RecentSummariesAcrossSessions(limit)` cross-session 조회 신규
