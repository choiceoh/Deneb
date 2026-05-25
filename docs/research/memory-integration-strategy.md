# 메모리 서브시스템 유기적 통합 전략

**Status:** ideation / proposal backlog
**Audience:** Deneb 운영자 + 차기 AI 세션
**Scope:** wiki · polaris · graphify · hindsight · memory 다섯 서브시스템의 결합 강화 방향.
**Methodology:** `gateway-go/internal/{domain,pipeline}/` 코드 인벤토리 + `recall_*.go` 호출 그래프 + `.claude/rules/prompt-cache.md` 캐시 제약 교차 검토.

> **읽는 법.** 각 아이디어는 **무엇을 → 왜 → 어디서 → 캐시 영향** 순서. 우선순위 (P0~P3) 와 추정 작업량 (S/M/L). 채택 여부는 운영자 판단. 합의된 아이디어만 별도 PR 로 진행한다.

---

## 0. 한 줄 요약 (TL;DR)

| # | 아이디어 | P | 작업량 |
|---|---|---|---|
| 1 | `MemorySubsystem` 컨테이너 확장 (wiki + polaris + hindsight + graph) | P1 | S |
| 2 | 통합 recall preflight: 출처 태그 + cross-source MMR dedup | P1 | M |
| 3 | Polaris LLM 요약 → wiki 일기 자동 승급 (high-quality only) | P2 | M |
| 4 | Hindsight retain 소스를 turn → wiki diary capsule 로 격상 | P2 | S |
| 5 | Wiki Tier1 페이지를 Polaris anchor (inevictable) 로 통합 | P1 | M |
| 6 | Graphify 1-hop 확장 recall (페이지 hit → 인접 노드 자동 후보화) | P2 | M |
| 7 | Polaris 요약 DAG 노드를 graphify 그래프에 시간축으로 편입 | P3 | L |
| 8 | Hindsight 회상 결과 → wiki dreamer 검증 큐 (양방향 동기화) | P3 | M |
| 9 | 세션 만료 시 Polaris DAG 루트 → Hindsight 장기 보관 | P3 | M |
| 10 | `memory.query` RPC: 단일 엔드포인트로 4-소스 federated 조회 | P2 | M |
| 11 | 충돌 해결 정책: 최신성 + 출처 등급 (wiki > polaris > hindsight) | P2 | S |
| 12 | Graphify 기반 compaction: 메시지를 wiki 엔티티별로 클러스터 요약 | P3 | L |

---

## 1. 현재 상태 지도

### 1.1 5개 서브시스템 한눈에

| 서브시스템 | 위치 | 핵심 역할 | 저장소 | 호출 시점 |
|---|---|---|---|---|
| **wiki** | `internal/domain/wiki/` | 카테고리별 마크다운 KB + 일기 + FTS | 파일 (`~/.deneb/wiki/`) | Tier1 inject (매 턴), 큐 기반 search, 일기 append |
| **polaris** | `internal/pipeline/polaris/` + `compaction/` | 대화 이력 압축 DAG | JSONL + summary JSON | 컨텍스트 budget 초과 시 |
| **graphify** | `internal/pipeline/chat/tools/graphify.go` + `wiki/graph_snapshot.go` | wiki 그래프 view (NetworkX) | `~/.deneb/wiki-graph/graph.json` | agent tool 호출 시 + dream cycle |
| **hindsight** | `internal/domain/hindsight/` | 외부 메모리 뱅크 (HTTP 클라이언트) | 외부 서버 | 매 턴 auto-recall + 턴 종료 async retain |
| **MemorySubsystem** | `internal/runtime/server/memory_subsystem.go` | wiki 컨테이너 (현재) | — | Server 임베드 |

### 1.2 기존 결합점

```
                  ┌──── Tier1 inject (매턴) ────┐
                  │                              ▼
wiki ──────────► graphify (snapshot)      [system prompt]
  ▲ (큐 기반)         ▲                          ▲
  │                   │ tool 호출                │
  │                   │                          │
recall_preflight ─────┴─── hindsight (auto-recall)
  │                                              ▲
  │                                              │
polaris (transcript bridge) ──── 압축 ───────────┘
```

**관찰**:
- **wiki ↔ graphify**: 단방향 read-only (snapshot → tool query)
- **polaris ↔ chat**: dual-write transcript, 압축 시 자율 발화
- **hindsight ↔ chat**: 매 턴 recall (cue 무관) + async retain
- **wiki ↔ polaris**: 직접 결합 0
- **polaris ↔ hindsight**: 직접 결합 0
- **graphify ↔ polaris**: 직접 결합 0
- **MemorySubsystem**: 현재 wiki 만 보유. 다른 셋은 deps 로 따로 흐름.

### 1.3 핵심 갭

1. **추상화 누수.** 4개 메모리 소스가 chat 파이프라인 곳곳에 흩어져 있다 (recall_preflight 에서 wiki/diary/hindsight 호출, run_exec 에서 polaris 호출, run_lifecycle 에서 hindsight retain). `MemorySubsystem` 이 이름만 컨테이너이고 실질적 통합 표면이 없다.
2. **출처 미식별.** recall_preflight 가 evidence string 을 만들 때 "wiki diary 에서 왔는지, hindsight 에서 왔는지, polaris 요약에서 왔는지" 가 시스템 프롬프트에 명시되지 않음 → 모델이 출처별 신뢰도를 구분 못함.
3. **중복 회상.** 같은 사실이 wiki diary 와 hindsight 양쪽에 저장되면 매 턴 recall 시 두 번 노출 → 토큰 낭비 + 모델 confused.
4. **압축 inevictability 부재.** Polaris 가 압축할 때 wiki Tier1 사실은 "절대 안 잊어야 함" 표시가 없음. 사용자 핵심 사실이 LLM 요약 단계에서 흐려질 수 있음.
5. **세션 경계 누수.** Polaris 는 세션-스코프. 세션 만료 후 그 세션의 핵심 요약은 사라짐 (transcript JSONL 은 남지만 DAG 요약 가치는 휘발). hindsight 가 turn-level retain 만 해서 "고차원 요약" 은 cross-session 으로 안 넘어감.

---

## 2. 통합 아이디어

### 2.1 `MemorySubsystem` 컨테이너 확장 — **P1 / S**

**무엇.** `internal/runtime/server/memory_subsystem.go` 가 wiki 만 보유. 이를 canonical container 로 격상:

```go
type MemorySubsystem struct {
    wikiStore       *wiki.Store
    polarisStore    *polaris.Store        // 추가
    polarisEngine   *polaris.Engine        // 추가
    hindsightClient *hindsight.Client      // 추가
    graphPath       string                 // 추가 (graphify snapshot path)
}
```

**왜.** Hub 규칙 (`.claude/rules/hub-wiring.md`) 과 정합. 현재는 4개 의존성이 `Deps` struct 곳곳에 흩어져 있음. 통합 컨테이너 하나로 묶으면 (a) chat 파이프라인 어셈블리 단순화, (b) 새 메모리 소스 추가 시 진입점 명확, (c) ops 헬스 체크 한 곳에서 가능.

**어디서.**
- `memory_subsystem.go` 확장 + `buildHub` 에서 단일 객체 주입
- `recall_preflight.go` 시그니처 단순화 (`deps.memory.Federated(query)` 같은 facade)
- `MemorySubsystem.Health()` → `/health/memory` 응답에 4 소스 상태 한 줄씩

**캐시 영향.** 0. 순수 wiring 리팩토.

---

### 2.2 통합 recall preflight: 출처 태그 + cross-source MMR dedup — **P1 / M**

**무엇.** 현재 `recall_preflight.go` 는 wiki / diary / hindsight 각각 별도 search 후 단순 concat. 다음을 추가:

1. **소스 태깅.** 각 evidence 에 `[wiki:기술/dgx-spark]`, `[diary:2026-05-23]`, `[hindsight:m_abc]`, `[polaris:summary L2]` 라벨.
2. **Cross-source dedup.** 같은 fact 가 두 출처에 있으면 신뢰도 높은 쪽만 (wiki > polaris L2 > diary > hindsight 의 등급, § 2.11 참조).
3. **MMR 재정렬.** Embedding 으로 의미 유사도 측정 후 다양성+관련성 trade-off. 현재는 score 순 단순 concat.

**왜.** 모델이 "wiki 에 정의된 fact 와 hindsight 에 어렴풋이 회상된 fact 의 차이" 를 인지하면 응답 품질↑. 중복 제거로 토큰 절감. MMR 로 한 토픽에 5개 evidence 가 몰리는 현상 방지.

**어디서.**
- `recall_preflight.go` 의 evidence aggregator 단계
- `compaction/embedding.go` 의 MMR 구현 재사용 가능 (BGE-M3 임베더 이미 있음)
- 출처 태그 포맷은 `prompt/recall_format.go` 신규

**캐시 영향.** Recall 결과는 system prompt 의 **Dynamic** 블록 (캐시 마커 없음). 영향 없음. 단 trailing message marker 의 prefix 안정성 검증 필요.

---

### 2.3 Polaris LLM 요약 → wiki 일기 자동 승급 — **P2 / M**

**무엇.** Polaris 가 Tier 1 LLM 요약 발화 시 결과물은 "이 세션의 핵심 사실 N개" 텍스트. 현재는 polaris DAG 에만 남고 사라짐. **이 중 confidence 가 높은 fact 를 wiki 일기 (`diary/YYYY-MM-DD.md`) 에 자동 append.**

**선별 기준.**
- LLM 요약에 "사용자가 명시한 사실" 패턴 (이름, 날짜, 결정, 약속)
- 한 줄 단위로 추출 (LLM 후처리 1회) — confidence score 부착
- threshold (예: 0.7) 이상만 diary append

**왜.** 현재 dreamer 가 이미 비슷한 일을 하지만 source 가 transcript JSONL. Polaris 요약은 이미 압축된 고밀도 사실이라 dreamer 의 입력으로 훨씬 효율적. 중복 작업 통합.

**어디서.**
- `polaris/engine.go` 의 `CompactAndPersist` 완료 hook
- `wiki/dreamer.go` 의 capsule 입력에 polaris source 추가
- 충돌: 같은 fact 가 이미 wiki 에 있으면 skip (`wiki.SearchDiary` 로 사전 체크)

**캐시 영향.** wiki diary 변경은 다음 세션의 Tier1 inject 에 반영. **현재 세션의 static cache 는 무관** (dreamer 가 다음 turn 즉시 system prompt 에 추가하지 않으면).

---

### 2.4 Hindsight retain: turn → wiki diary capsule — **P2 / S**

**무엇.** 현재 `hindsight_recorder.go` 가 매 턴 user+assistant 메시지를 그대로 retain. 노이즈 비율 높음 ("안녕", "그렇네" 같은 짧은 turn 도 저장). **대신 dreamer 가 생성한 diary capsule 만 retain.**

**왜.** Hindsight 의 가치는 cross-session episodic memory. 일상적 small-talk turn 은 noise. Dreamer 가 이미 "기억할 가치 있는 사실" 만 capsule 화 → 그 결과만 외부 뱅크로 승급하면 신호 대 잡음비↑.

**어디서.**
- `recall_hindsight.go` 의 retain 호출을 turn-level → dreamer event subscribe 로 전환
- `domain/wiki/dreamer.go` 의 capsule 생성 이벤트에 hindsight retain handler 부착
- 기존 turn-level retain 은 fallback 으로 1주일간 dual-write (회귀 확인 후 제거)

**캐시 영향.** 없음. retain 은 비동기 백그라운드.

---

### 2.5 Wiki Tier1 페이지를 Polaris anchor 로 통합 — **P1 / M**

**무엇.** `.claude/rules/improvement-ideas.md` § 2.3 "Polaris semantic-anchor 압축" 의 자연스러운 확장. **별도 anchor extraction LLM 호출 대신 wiki Tier1 (importance≥9) 페이지를 곧 anchor 로 사용.**

```
Polaris compact() 시:
  1. 메시지 중 wiki Tier1 페이지의 ID/제목 언급 검색
  2. 해당 메시지 → anchor 표시 → 압축에서 inevictable
  3. anchor 가 아닌 메시지만 LLM 요약 후보
```

**왜.** Anchor 의 진짜 의미는 "사용자가 까먹지 말라고 한 것". wiki Tier1 이 정확히 그 신호 (사용자 또는 dreamer 가 importance 9-10 부여한 페이지). 별도 LLM 호출 없이 무료 anchor 획득.

**어디서.**
- `compaction/polaris.go` 의 Tier 1 LLM 단계 전에 anchor 표시 단계
- `wiki/store.go` 의 `Tier1Pages()` 결과로부터 anchor 키워드 추출 (제목 + ID + 주요 tag)
- `polaris/types.go` 에 `Message.Anchored bool` 필드

**캐시 영향.** Anchor 표시 자체는 압축 결과물에 반영. 시스템 프롬프트와 무관.

---

### 2.6 Graphify 1-hop 확장 recall — **P2 / M**

**무엇.** Recall 이 wiki 페이지를 hit 했을 때, 그 페이지의 1-hop 이웃 (graphify edges 기준) 도 후보 evidence 로 추가. **단 최종 evidence 에는 MMR dedup 후 일부만 포함.**

**왜.** 사용자가 "Y 프로젝트 마감일" 을 물으면 wiki 의 `프로젝트/Y` 페이지가 hit. 하지만 인접한 `사람/김부장` (그 프로젝트 담당자) 은 키워드 매치 안 됨. Graphify 가 이미 edge 정보 보유 → 거의 무료로 recall 확장 가능.

**어디서.**
- `recall_preflight.go` 에서 wiki hit 마다 graphify snapshot 조회
- `wiki/graph_snapshot.go` 에 in-memory adjacency map 추가 (현재 snapshot 은 JSON 파일)
- 확장 깊이는 1-hop 고정 (2-hop 은 노이즈 폭증)

**캐시 영향.** Dynamic 블록만 변동 → static cache 무관.

---

### 2.7 Polaris 요약 DAG → graphify 그래프 시간축 편입 — **P3 / L**

**무엇.** Graphify 가 현재 wiki 페이지만 노드로 가짐. **Polaris summary node 를 "temporal node" 로 추가.** Edge 종류 확장:
- `(polaris_summary) -- mentions --> (wiki_page)`
- `(polaris_summary L2) -- derived_from --> (polaris_summary L1)`
- `(polaris_summary) -- session --> (session_id)`

**왜.** 그래프 query 가 "지난 한 달간 X 페이지를 언급한 대화 요약 보여줘" 같은 시간축 query 가능. 현재 graphify 는 정적 (wiki 만). Temporal 차원 추가 시 retrospective query 의 새 surface.

**어디서.**
- `wiki/graph_snapshot.go` 의 graph builder 확장
- `polaris/engine.go` 의 summary persist 후 snapshot 재생성 트리거 (or incremental)
- `graphify` 외부 CLI 가 새 node type 처리 가능한지 검증 필요

**캐시 영향.** 없음 (graphify 는 agent tool, system prompt 외).

**위험.** 그래프 노드 폭증. 시간 기반 TTL (90일 이전 polaris summary node 제거) 같은 정책 필요.

---

### 2.8 Hindsight → wiki dreamer 검증 큐 — **P3 / M**

**무엇.** Hindsight 회상 결과 중 wiki 에 없는 새 fact 가 자주 나오면, dreamer 의 검증 큐에 후보로 enqueue. 검증 통과 시 wiki page 생성.

**왜.** 양방향 동기화. 현재는 wiki → hindsight 방향만 (§ 2.4). 역방향이 빠지면 hindsight 가 외부 source 로부터 받은 사실 (예: 다른 채널) 이 wiki 에 영원히 안 들어옴.

**어디서.**
- `recall_hindsight.go` 에서 회상된 memory 중 `wiki.SearchDiary` 무관계 → 큐 push
- `wiki/dreamer.go` 의 verification phase 에 hindsight-sourced 후보 처리
- 큐 길이 제한 (오버플로우 시 가장 오래된 drop)

**캐시 영향.** 없음.

**위험.** Hindsight 노이즈가 wiki 오염. Confidence threshold + 수동 승급 옵션 필요.

---

### 2.9 세션 만료 시 Polaris DAG 루트 → Hindsight 장기 보관 — **P3 / M**

**무엇.** 세션이 timeout/done 으로 종료될 때 polaris 의 최상위 DAG 노드 (가장 압축된 요약) 를 hindsight 에 retain.

**왜.** Polaris 의 가치는 세션 내. 세션 만료 후 raw transcript JSONL 은 남지만 DAG 요약은 묻힘. 다음 세션에서 "지난 주 대화 요약" 회상이 안 됨. Hindsight 가 그 역할의 backplane.

**어디서.**
- `session/lifecycle.go` 의 종료 transition hook
- `polaris/store.go` 에서 DAG root node 추출
- `hindsight/client.go` retain 호출 (이미 존재)

**캐시 영향.** 없음 (세션 종료 후).

---

### 2.10 `memory.query` RPC — **P2 / M**

**무엇.** 단일 RPC 가 4-소스 federated query 수행. 응답은 출처 태그 + score + content. Agent tool 도 이 RPC 만 호출.

```
memory.query({
  query: "X 프로젝트 마감일",
  sources: ["wiki", "polaris", "hindsight", "diary"],  // optional, 기본 all
  limit: 8,
  dedup: true
}) → {
  results: [
    {source: "wiki", path: "프로젝트/X.md", score: 0.92, snippet: "..."},
    {source: "diary", date: "2026-05-20", score: 0.81, snippet: "..."},
    ...
  ]
}
```

**왜.** Agent 가 현재 `wiki_search`, `polaris_search`, `hindsight_recall` 을 따로 호출 (각각 tool schema 따로). 단일 진입점 → tool registry 단순화 + 모델의 도구 선택 부담 경감.

**어디서.**
- `rpc/handler/memory/` 신규 핸들러
- `MemorySubsystem.Federated()` 메서드가 backbone
- 기존 별도 tool 들은 deprecated 표시 후 1주일 dual-route

**캐시 영향.** Tool schema 1개 추가, N개 제거 → static cache 1회 invalidate. 이후 안정.

---

### 2.11 충돌 해결 정책 — **P2 / S**

**무엇.** 같은 사실이 출처마다 다를 때 (wiki 는 "마감 6/15", polaris 요약은 "마감 6/20" 같이) 명시적 정책.

**제안 등급.**
```
wiki/page (수동 또는 dreamer 검증) > polaris L2+ 요약 > diary > polaris L1 요약 > hindsight > transcript raw
```

**최신성 가중치.** 같은 등급 내에서는 `updated_at` 최신.

**왜.** 현재는 단순 score 순. 사용자가 wiki 에 수동으로 적은 fact 를 hindsight 의 흐릿한 회상이 덮어쓰는 경우 방지.

**어디서.**
- `recall_preflight.go` 의 merge 단계 정책 함수 도입
- `prompt/recall_format.go` 에서 출처 등급도 evidence 옆에 명시 ("출처: wiki, 신뢰도: 높음")

**캐시 영향.** 없음 (Dynamic).

---

### 2.12 Graphify-aware compaction — **P3 / L**

**무엇.** Polaris 가 시간 윈도우 (chunk 단위) 로 요약. **대신 wiki 엔티티 (페이지) 별로 클러스터링 후 요약.** Graphify 가 메시지의 엔티티 멘션을 매핑.

**예시.** 30 turn 대화에서:
- 메시지 1-5, 12, 18: `프로젝트/X` 관련 → "X 프로젝트 요약" 1 노드
- 메시지 6-10, 15: `사람/김부장` 관련 → "김부장 관련 결정" 1 노드
- 나머지: 기존 시간순 요약

**왜.** 회상 시 "X 프로젝트 관련해서 지난번에 뭐라고 했지" 가 정확히 hit. 시간순 요약은 entity-based query 가 약함.

**어디서.**
- `compaction/polaris.go` 의 chunking 단계 교체 (또는 dual strategy)
- `wiki/graph_snapshot.go` 의 mention detector 재사용
- Tier 3a embedding fallback 은 유지 (엔티티 매핑 실패 시)

**캐시 영향.** 없음.

**위험.** 엔티티 누락 시 일부 메시지가 클러스터에 안 들어감. Fallback 으로 "기타" 클러스터 보장.

---

## 3. 아키텍처 패턴 정리

### 3.1 통합 후 호출 그래프

```
                      ┌─────────────────────────────────┐
                      │      MemorySubsystem            │
                      │  ┌────┐ ┌─────┐ ┌────┐ ┌──────┐ │
                      │  │wiki│ │polar│ │hind│ │graph │ │
                      │  └────┘ └─────┘ └────┘ └──────┘ │
                      └─────────────────────────────────┘
                              ▲      ▲       ▲
                ┌─────────────┘      │       └────────────┐
                │                    │                    │
        recall_preflight       compact (anchor)      session_end
        (federated MMR)        (Tier1 inevict)        (DAG → hind)
                │                    │
                ▼                    ▼
          [system prompt]      [transcript]
          (Dynamic 블록)        (compacted)
```

### 3.2 출처 등급 표 (§ 2.11 요약)

| Rank | 출처 | 신뢰도 신호 |
|---|---|---|
| 1 | wiki/page (manual or dreamer verified) | 사용자 또는 dreamer 가 확정 |
| 2 | polaris L2+ 요약 | 다중 메시지에서 합의된 사실 |
| 3 | wiki/diary | dreamer capsule (자동 추출) |
| 4 | polaris L1 요약 | 단일 chunk 요약 |
| 5 | hindsight memory | 외부 뱅크 (cross-session) |
| 6 | transcript raw | 가공 안 된 발화 |

### 3.3 캐시 invariant 체크리스트

`.claude/rules/prompt-cache.md` § 6 PR 체크리스트에 다음 추가:

- [ ] `memory.query` 같은 신규 tool 도입 시 tool list hash 변경 → static cache 1회 invalidate 후 안정?
- [ ] Anchor 표시는 polaris 결과물 (transcript) 에만 영향, system prompt 와 무관?
- [ ] Federated recall 결과의 출처 태그 포맷이 Dynamic 블록 내에 있고 day-only timestamp 와 호환?
- [ ] graphify 1-hop 확장이 매 턴 발화해도 system prompt 의 Tier1 inject 와 충돌 안 함?

---

## 4. 시퀀싱 (Now / Next / Later)

### Now — 다음 1주 (P1)
- 2.1 `MemorySubsystem` 컨테이너 확장 (S)
- 2.2 통합 recall preflight + 출처 태그 + MMR (M)
- 2.5 Wiki Tier1 → Polaris anchor (M)

### Next — 다음 1개월 (P2)
- 2.3 Polaris 요약 → wiki 일기 승급 (M)
- 2.4 Hindsight retain 소스 격상 (S)
- 2.6 Graphify 1-hop 확장 recall (M)
- 2.10 `memory.query` RPC (M)
- 2.11 충돌 해결 정책 (S)

### Later — 분기 단위 (P3)
- 2.7 Polaris DAG → graphify temporal node (L)
- 2.8 Hindsight → wiki dreamer 검증 큐 (M)
- 2.9 세션 만료 시 DAG 루트 → hindsight (M)
- 2.12 Graphify-aware compaction (L)

---

## 5. 명시적 비-제안 (Out of Scope)

- ❌ **메모리 소스 추가 (Notion, Obsidian 등).** 4개 소스 통합 자체가 미완. 신규 소스는 통합 패턴 안정 후.
- ❌ **메모리 sharding / multi-bank.** 단일 사용자 환경. Hindsight 도 단일 bank 가정.
- ❌ **Wiki 페이지 자동 삭제 / TTL.** 사용자 사실 보존이 최우선. Polaris/graphify temporal node 만 TTL 대상.
- ❌ **Federated query 의 외부 API 노출.** Loopback only. agent tool 만 호출.
- ❌ **Embedding 모델 교체 (BGE-M3 → 타).** Polaris/recall 양쪽이 같은 임베더 가정. 교체는 별도 ADR.

---

## 6. 위험 / 검증 필요

| # | 위험 | 검증 방법 |
|---|---|---|
| R1 | 통합 recall 이 정보 손실 (MMR dedup 너무 적극적) | `quality chat` baseline + 한국어 회상 케이스 추가 |
| R2 | Wiki Tier1 anchor 가 너무 많아 polaris 압축 효과 무력화 | importance 9-10 만 anchor, 메시지 매치 maxN 제한 |
| R3 | Polaris → wiki 자동 승급이 wiki 오염 | confidence threshold + dreamer verification phase 통과 강제 |
| R4 | Graphify 1-hop 확장이 토큰 폭증 | 확장 후보 max 3 + MMR 로 절단 |
| R5 | `memory.query` RPC 가 latency 추가 (4 소스 sequential) | 내부 parallel (errgroup), 1.5s timeout per source |
| R6 | 출처 등급이 사용자 의도와 충돌 (예: 사용자가 "wiki 무시하고 최근 대화 기준" 요청) | `/recall_mode <strict|loose>` 슬래시 검토 (P3) |

---

## 7. 변경 로그

| 날짜 | 작성자 | 내용 |
|---|---|---|
| 2026-05-25 | Claude (claude-opus-4-7) | 초안 작성 |

---

## 8. 참고

- 코드 인벤토리: Explore 에이전트 (2026-05-25) — `internal/{domain,pipeline}/` 전수 + recall 호출 그래프
- 캐시 doctrine: `.claude/rules/prompt-cache.md`
- Hub wiring: `.claude/rules/hub-wiring.md`
- 기존 개선 백로그: `docs/research/improvement-ideas.md` (§ 2.3, § 4.6 와 연관)
- 관련 research: `docs/research/{hermes-agent-analysis,hermes-deneb-mapping}.md`
