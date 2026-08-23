# 위키 회상 베이스라인 재측정 — 2026-08-23

> 2026-07 회상 개선 사이클
> ([wiki-retrieval-improvement-cycle-2026-07.md](wiki-retrieval-improvement-cycle-2026-07.md))이
> 남긴 미실행 권고 3항(골드 확대, 쿼리 확장 라이브 A/B, downstream 측정) 중 측정
> 두 축을 실행한 스냅샷. 결론부터: **"hard 셋 회상 병목"의 대부분은 죽은 골드였고,
> 배포된 backfill 쿼리 확장은 구조적으로 발동하지 않으며, multiturn 실패는 검색이
> 아니라 지칭 해결(세션 컨텍스트)의 문제다.** single-turn 검색 스택 자체는
> 건강하다.

## 방법 (프로덕션 패리티, 로컬 실행)

srv4에서 읽기 전용 복사 후 로컬 Mac에서 벤치 — 원격 측정 재현 레시피:

```bash
# 1) 데이터: 골드 11종 + 위키 사본 (232MB, 1169 md)
rsync -az srv4:'.deneb/wiki-qa-gold*.jsonl' gold/
rsync -az srv4:.deneb/wiki/ wiki/
# 2) 사이드카 터널 (임베더 8002 / 리랭커 8004 / 웜홀 LLM 18800)
ssh -N -T -L 8002:127.0.0.1:8002 -L 8004:127.0.0.1:8004 -L 18800:127.0.0.1:18800 srv4
# 3) 벤치
DENEB_EMBEDDING_URL=http://127.0.0.1:8002 DENEB_RERANK_URL=http://127.0.0.1:8004 \
  go run ./cmd/recall-bench --wiki wiki --gold gold/wiki-qa-gold.jsonl \
  --k 8 --content --matrix --pool-depth 100
```

K=8, `--content`(토큰 히트 — 7월의 교훈: 경로 기반 골드는 개명에 취약),
라이브 프로덕션 골드(2026-08-23 시점), 라이브 `.recall-hits.jsonl`.

## 베이스라인

| 골드셋 | 케이스 | P@1 | r@8 | MRR | 비고 |
|---|---|---|---|---|---|
| main | 161 | 91.9% | 97.5% | 0.942 | matrix full 암 |
| hard | 27 | 51.9% | 55.6% | 0.537 | **천장 63% (10건 죽은 골드)** |
| meeting-xl | 46 | 82.6% | 97.8% | 0.892 | 하이브리드가 P@1 84.8%로 최고 |
| multiturn | 16 | 12.5% | 18.8% | 0.135 | **천장 50% (8건 죽은 골드)** |
| multiturn-a1sim | 16 | 25.0% | 50.0% | 0.359 | 천장 50% 동일 |
| analysis-xl | 450 | 68.4% | 89.3% | 0.758 | 메일/결재 주체 경로 |
| facet | 91 | 90.1% | 96.7% | 0.931 | |
| typed | 78 | 92.3% | 97.4% | 0.943 | |
| bm25 | 24 | 79.2% | 91.7% | 0.854 | |
| traffic | 7 | 85.7% | 85.7% | 0.857 | 실트래픽 유래 |
| analysis | 10 | 90.0% | 100% | 0.933 | |

matrix(main): bm25 90.7/95.0 · semantic 87.0/88.8 · hybrid 91.3/97.5 · full 91.9/97.5.
hard의 bm25 단독 37.0% → full 55.6%: 융합이 절반을 회복시키지만 천장에 막힘.

recall-health: `RECALL_UTIL distinct=298 total=919 repeat=155 used=184` ·
`RECALL_COVERAGE 72개 프로젝트 중 56 커버 (77.8%)` · `RECALL_HEALTH 87.4`
(retrieval 93.9 / coverage 77.8). `--emit-gold`가 미커버 16개 프로젝트에서
후보 37건 생성 — 사람 검토 후 골드에 추가할 것.

## 발견 1 — 죽은 골드가 "hard 병목"의 주범 (재매핑 적용 완료)

전수 집계: **9개 셋에서 39건**의 gold_paths가 현존 페이지와 불일치. hard(10/27),
multiturn(8/16), multiturn-a1sim(8/16), typed(5), main(3), bm25(2),
analysis-xl·facet·traffic(각 1). 2026-07-19 코드 폴더 개명의 잔해 — main 셋만
수리됐고 파생 셋들은 그대로였다.

검증된 매핑으로 26건 재매핑(기아-광주는 폴더 경로로)한 뒤 재측정:

| 셋 | r@8 (전) | r@8 (후) | P@1 (전→후) | pool-depth 100 |
|---|---|---|---|---|
| hard | 55.6% | **92.6%** | 51.9→74.1% | ranking_miss=0, generation_miss=2 |
| multiturn | 18.8% | 18.8% | 12.5→12.5% | ranking_miss=4, generation_miss=9 |
| multiturn-a1sim | 50.0% | **100%** | 25.0→56.2% | ranking_miss=0, generation_miss=0 |

- **srv4 골드 3종(hard/multiturn/multiturn-a1sim)에 적용 완료** — 원본은
  `~/.deneb/wiki-qa-gold-<셋>.jsonl.bak-20260823` 백업.
- 부수 발견: `프로젝트/pl2-kia-epc-002`(기아 AL광주 2공장)에 **대표.md가 없다**
  (로그·메일분석만 존재) — 프로젝트 레이아웃 불변식 위반. 골드는 폴더 경로로
  임시 매핑. 대표 페이지 생성이 위생 작업 1순위.

## 발견 2 — backfill 쿼리 확장은 구조적으로 발동하지 않는다

`query_expansion.go`의 backfill 설계는 "원 질의가 limit을 못 채울 때만" 확장
히트로 빈 칸을 메운다(`len(results) >= limit` 조기 복귀). 그런데 1169페이지
코퍼스에서 BM25는 거의 항상 8칸을 채운다 — **확장 LLM은 결과가 거의 비는
질의에서만 호출되고, hard/multiturn 같은 "8개를 채우지만 전부 틀린" 실패
모드에는 영원히 닿지 않는다.**

실측: hard·multiturn·meeting-xl에서 control 대비 glm-5.2 / qwen3.6-35b-a3b
처리군이 **모든 지표에서 자릿수까지 동일** (발동 0회와 일관). 7월의
"+3–5pp 레버"는 RRF 병합 형태에서 나온 것이고, 그 형태는 hit@1 −18건으로
기각돼 backfill로 대체됐다 — 즉 배포 형태는 원래 레버의 효과를 담지 못한다.

유효한 재설계 방향(다음 사이클 후보): **weak-primary 트리거** — 상위 히트
스코어가 임계값 미만이거나 exact-match가 없을 때만 확장 질의를 RRF 병합하되
원 exact-match 핀은 보존. hit@1 회귀 없이 generation 미스만 노리는 형태.

## 발견 3 — multiturn 실패는 지칭 해결의 문제 (재작성 시 100% 도달)

multiturn 원본("그 현장 계약 조건은 어떻게 됐지?")은 재매핑 후에도 18.8%.
같은 질문을 독립형으로 재작성한 a1sim 변형은 **r@8 100%, P@1 56.2%** —
현재 검색 스택이 세션 컨텍스트만 보태면 멀티턴 골드를 전부 회수한다는
뜻. pool 탐침도 일관(원본 pool_recall 43.8% vs a1sim 100%).

권고: recall preflight의 `searchQueries` 조립 단계에서 **직전 사용자/어시스턴트
턴을 결합한 독립형 재작성** 1개를 추가(멀티턴 감지: 지시사("그", "거기",
"아까") + 이전 턴 존재). P1의 남은 미실행 항목(downstream 측정)도 이
리라이팅과 함께 측정하면 답변 품질 게인이 잡힐 것.

## 권고 요약

1. **골드 위생 주기화** — 죽은 골드는 천장을 내려 모든 개선을 가린다(7월
   main 셋 사태의 재연). recall-health에 dead-gold 감지(경고 코드는 이미
   있음)를 정기 점검 항목으로 편성하고, 파생 셋 생성 시 경로는 코드
   폴더 프리픽스로만 쓴다.
2. **emit-gold 후보 37건 검토·승인** (커버리지 77.8% → 90%+ 도달 가능).
3. **weak-primary RRF 확장 재설계** (발견 2) — 벤치 코드는 이미
   `DENEB_EXPANSION_MODEL`로 A/B 가능.
4. **멀티턴 컨텍스트 재작성** (발견 3) — 회상 파이프라인 변경 + a1sim
   골드로 회귀 측정.
5. **pl2-kia-epc-002 대표 페이지 생성** (레이아웃 불변식 복구).

주의(재현 시): 웜홀 확장 A/B는 **클라우드 모델만** — deepseek-v4-flash-api는
reasoning이 MaxTokens를 소진해 빈 응답(벤치가 잡아줌), 로컬 dsv4 계열은
라이브 채팅을 멈춘 전례(main.go 주석). 이번에 안 것: glm-5.2·qwen3.6-35b-a3b
출력은 깨끗.
