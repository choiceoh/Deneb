# APEX Prompt-Optimization 도입 분석과 Deneb 적용 범위

Source: https://arxiv.org/html/2606.11459v1, arXiv v1, 2026-06-09.

This note covers **APEX: Automated Prompt Engineering eXpert with Dynamic Data
Selection**. Propus treats this as the core APEX source for frontier data
selection. It separately keeps Autonomous Policy Exploration (`arxiv:2605.21240`)
as supporting transfer evidence for strategy-map exploration against collapse.
Adaptive Principle EXtraction (`arxiv:2606.15363`) is intentionally filtered from
canonical Propus sources: it is useful background vocabulary, but its single
production-agent case study and incomplete L2 integration are not strong enough
to create a product quality gate.

## 논문 핵심

APEX는 프롬프트 최적화에서 병목이 mutation 알고리즘 자체보다 평가 데이터 사용법에 있다고 본다. 기존 GEPA/APO류 루프는 실패 케이스를 무작위로 뽑거나 전체 개발셋을 매번 평가한다. 이 방식은 두 가지 비용을 낳는다.

- mutation 단계: 현재 프롬프트로는 아직 풀 수 없는 Hard 실패를 계속 보여줘서 과한 수정과 망각을 유도한다.
- selection 단계: 모든 후보가 똑같이 맞히거나 틀리는 데이터에 예산을 써서 후보 간 rank를 구분하지 못한다.

APEX의 대안은 평가 이력을 prompt lineage로 보고 각 datapoint를 최근 `k`개 결과 기준으로 계층화하는 것이다.

- `Easy`: 최근 후보들이 계속 통과한 케이스. 회귀 anchor로는 유용하지만 개선 신호는 낮다.
- `Hard`: 최근 후보들이 계속 실패한 케이스. 당장 mutation 신호로 쓰면 과적합 위험이 크다.
- `Mixed`: 최근 후보들 사이에서 pass/fail이 갈린 케이스. rank-sensitive frontier이며 가장 좋은 개선 신호다.

논문 기본값은 lookback `k=5`, mutation batch `m=5`, per-iteration eval budget `100`, anchor ratio `alpha=0.2`, 성공 시 `beta=0.03` 증가다. selection은 `BM,empty`를 먼저 평가하고, 남은 예산을 positive anchors(`BM,1`, `BE,empty`)와 negative checks(`BM,0`, `BH,empty`)로 나눈다. 이미 평가한 결과는 history에서 재사용한다.

## Deneb 매핑

Deneb에서 바로 안전하게 매핑되는 단위는 `scripts/dev/quality-test.py`의 test case다.

- datapoint: `scripts/quality-tests.yaml`의 test definition
- lineage: `~/.deneb/quality-results.db`에 기록된 최근 run 결과
- score: `test_results.passed`와 `score`
- Easy/Hard/Mixed: 최근 `--apex-lookback`개 pass/fail 결과
- current state: 최신 recorded run에 해당 test가 있으면 pass/fail, 없으면 skipped

이 구현은 논문의 자동 prompt mutation 전체가 아니라 rank-sensitive sampling을 먼저 도입한다. 이유는 Deneb의 일반 비서/메일 분석 응답은 대표 데이터셋과 programmatic evaluator가 아직 충분히 안정적이지 않기 때문이다.

## 구현된 사용법

현재 frontier를 확인한다. 게이트웨이 연결이 필요 없다.

```bash
python3 scripts/dev/quality-test.py --apex-plan --scenario all
```

APEX sampler로 예산 100개만 실행하고 결과를 기록한다.

```bash
python3 scripts/dev/quality-test.py --scenario all --sample apex --apex-budget 100 --record
```

카테고리별 파일럿은 더 싸다.

```bash
python3 scripts/dev/quality-test.py --scenario compact --sample apex --apex-budget 30 --record
python3 scripts/dev/quality-test.py --scenario bench --sample apex --apex-budget 40 --record
```

전체 suite는 주기적인 anchor로 남긴다.

```bash
python3 scripts/dev/quality-test.py --scenario all --record
```

## 적용 우선순위

1. 컴팩션 프롬프트 평가
   - 이미 deterministic gold fact recall, section completeness, LLM judge가 있다.
   - APEX sampling으로 반복 eval 비용을 줄이고 Mixed fixture를 우선 확인한다.

2. 스킬 evolution 검증
   - 기존 rejected edit buffer와 held-out validation case가 있다.
   - `skill_lifecycle` validation case의 `frontierTier`가 `mixed`와 `easy`를 모두 포함할 때만 Propus status가 APEX frontier coverage를 인정한다.
   - Mixed validation case를 mutation/evaluation 우선순위로 올리면 실패 재현과 회귀 방지가 좋아진다.

3. 일반 품질 suite
   - daily/system/code/edge 전체를 매번 돌리는 대신 frontier를 먼저 돌리고, full run은 anchor로 남긴다.

4. 메일 분석 프롬프트
   - 자동 반영 금지. shadow 후보 비교와 수동 검토가 먼저다.
   - 중요도/일정/업무 맥락 판단은 오판 비용이 커서 programmatic evaluator만으로 닫으면 안 된다.

## 운영 가드레일

- `--sample apex`는 full-suite 대체재가 아니라 빠른 frontier gate다.
- Easy 케이스도 일정 비율 anchor로 유지해야 한다. 논문도 성공할수록 anchor ratio를 늘려 mastered logic을 잠근다.
- Hard 실패만 보고 프롬프트를 고치지 않는다. Hard-only sampling은 논문 ablation에서 성능을 크게 해친다.
- prompt 길이를 늘리는 식의 개선은 기본적으로 의심한다. 논문은 APEX가 더 긴 프롬프트 때문이 아니라 핵심 지시를 더 정확히 남긴 덕분이라고 분석한다.
- 대표 데이터셋과 신뢰 가능한 평가 함수가 없는 업무/메일/일정 자동화는 shadow mode를 거친다.

## 다음 확장

- `quality-results.db`에 prompt/candidate id를 별도로 기록하면 진짜 prompt lineage를 복원할 수 있다.
- mutation usage history를 저장하면 같은 실패 케이스 반복 과적합을 더 강하게 막을 수 있다.
- compaction live eval의 fixture별 결과를 test_results와 같은 shape로 기록하면 APEX sampler를 Go eval에도 재사용할 수 있다.
