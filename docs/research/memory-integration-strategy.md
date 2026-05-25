# 메모리 통합 전략 (2-Layer)

**Status:** approved
**Scope:** wiki · polaris · graphify · hindsight · MemorySubsystem.

> **한 줄 요약.** Wiki 가 메모리다. Polaris 는 미정착 fact 의 임시 거처. Dreamer 가 다리.

---

## 모델

```
Wiki = 영구 메모리 (사실)
  ↑
  │ dreamer 가 승급
  │
Polaris = 현 세션 staging (아직 wiki 로 안 들어간 fact)
```

- **Hindsight** = wiki 의 외부 백업 (cross-session 동기화). 회상 경로에서 빠짐.
- **Graphify** = wiki 의 read-only view (현재 그대로).
- **MemorySubsystem** = wiki + polaris 보유 컨테이너 (그게 다).

---

## 통합 작업 (3개)

### 1. Dreamer 입력에 polaris 요약 추가

현재 dreamer 는 transcript JSONL 만 본다. Polaris 압축 요약 (Tier 1 LLM 결과) 이 이미 고밀도 fact 라 dreamer 의 더 좋은 입력.

- `wiki/dreamer.go` 입력에 polaris store 추가
- 같은 fact 가 wiki 에 이미 있으면 skip

### 2. Recall = wiki + 현 세션 polaris 만

Hindsight 직접 회상 제거. **Retain 경로는 유지** (백업 목적).

- `recall_preflight.go` 에서 hindsight Recall 호출 제거
- `recall_hindsight.go` + 테스트 파일 삭제
- `hindsight_recorder.go` (retain) 는 그대로 — wiki 의 외부 백업 역할

세션 시작 시 hindsight → wiki prime 은 단일 사용자 환경에서 불필요 (wiki 가
이미 영구 truth). 향후 cross-machine 동기화 또는 wiki 손실 복구 시점에 별도
검토.

### 3. Wiki Tier1 = Polaris anchor

- `compaction/polaris.go` 의 Tier 1 LLM 압축 단계 전에 anchor 표시 단계 추가
- Anchor 키워드 = wiki importance ≥ 10 페이지의 제목 + ID (~5개 이내)
- Anchor 가 매칭된 메시지는 inevictable

---

## 버리는 것들

- ❌ Federated query / memory.query RPC
- ❌ 출처 태그 / 등급 / MMR
- ❌ 충돌 해결 정책 (wiki 가 truth, 충돌 없음)
- ❌ `/recall_mode` `/unlearn` `/pin` `/recall_verbose` `/recall_trace` `/health/memory`
- ❌ Graphify 1-hop expansion / temporal node
- ❌ Idle-trigger hindsight retain
- ❌ Hindsight 2-tier retain
- ❌ Entity-based compaction

---

## 비-목표

- ❌ Hindsight 회상 경로 유지 (백업 전용으로 축소)
- ❌ 새 슬래시 / RPC / 도구

---

## 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-05-25 | Claude (claude-opus-4-7) | 12-아이디어 초안 → 2-layer 단순화 |

---

## 참고

- `.claude/rules/prompt-cache.md` — anchor 표시는 transcript 영역, system prompt 와 무관
- `.claude/rules/concurrency.md` — wiki 동시 mutate (dreamer + 자율 task) 락 hierarchy
