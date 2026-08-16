---
description: "Health Bench 2.0 점수·finding·baseline 변경 규약"
globs: ["scripts/audit/codebase-health-v2.py", "scripts/audit/health_v2/**", "scripts/audit/health-v2-*.json"]
---

> **⚠️ 마이그레이션 안내 (2026-08).** 이 벤치는 **Health Bench 3.0**(`codebase-health-v3.md`)에 승계되었다. 3.0이 구조·런타임·RSI 피트니스를 단일 벤치로 통합하며(설계: `docs/research/health-bench-3.0.md`), v1/v2/v3 점수는 서로 비교하지 않는다. v2의 **scorer 단위 테스트(`make health-v2-test`)는 운영자 결정(2026-07-24)으로 로컬 `make check`에 남아** 있어 scorer 자체가 썩지 않게 한다 — 이는 퇴역이 아니라 의도된 병존이다. 신규 평가·래칫은 v3(`make health-v3`, `make bench-check`)를 쓴다.

# Codebase Health Bench 2.0

Health Bench 2.0은 줄 수를 줄이는 게임이 아니라, 변경이 국소적이고 계약이
명시적이며 실패 원인을 빠르게 찾을 수 있는지를 추적한다. 기존
`scripts/audit/codebase-health.py`와 `scripts/audit/health-baseline.json`은 v1
호환성을 위해 그대로 둔다. v1과 v2 점수는 서로 비교하지 않는다.

## 평가 원칙

- 기본 `fast` 프로필은 네트워크와 실행 중인 Deneb에 의존하지 않는다. 같은
  revision과 입력은 같은 JSON과 finding ID를 만들어야 한다.
- `deep` 프로필은 pinned formatting, vet, lint, fresh shuffled coverage, race처럼
  비용이 큰 실행 증거를 보강한다. Mutation과 dead-code는 향후 deep 확장 후보이며
  현재 점수에 포함하지 않는다. 도구 누락이나 timeout을 100점으로 처리하지 말고
  `unavailable`로 보고한다. Formatting과 raw statement coverage는 deep에서도
  readiness/diagnostic evidence이며 delivery 점수는 바꾸지 않는다.
- LOC, 테스트 LOC, 파일 개수는 원인 조사용 증거일 뿐 독립적인 성공 기준이
  아니다. 파일만 쪼개거나 테스트 본문을 복제해 점수가 올라가면 rubric 버그다.
- 포맷터 실행, 결과 artifact 업로드, 모든 언어에 같은 coverage·race·순서 무작위화
  요구를 적용하는 것도 독립적인 점수 항목이 아니다. Delivery는 각 언어의 실제
  역할과 제품 영향도에 맞는 실패 예방 gate만 평가한다.
- AI 관련 문서는 존재하거나 길다는 이유만으로 점수를 얻지 않는다. 문서가 가리킨
  경로·심볼·테스트·검증 명령이 실제 tree에 있고, 변경 범위와 source of truth까지
  추적할 수 있어야 한다.
- 점수는 change locality, boundary integrity, contract integrity, test confidence,
  diagnosability, AI maintainability를 함께 반영한다. 한 축의 개선으로 다른 축의
  의미 있는 퇴행을 상쇄하지 않는다.
- 필수 증거가 없으면 check를 tooling error로 끝낸다. 모르는 상태를 건강한
  상태로 간주하지 않는다.

## Rubric 2.2.0

2.2.0은 2.1.2의 typed-contract와 테스트 provenance 규칙을 유지하면서,
단일 패키지 안에서 반복된 변경 빈도를 integration bottleneck으로 오인하지
않는다. Locality hotspot은 여러 컴포넌트를 가로지른 변경에 반복 참여한 현재
패키지만 점수화하며, 원시 빈도와 산개 변경·co-change·volatile hub 증거는 계속
보존한다.

| Pillar | Weight | 실제로 묻는 질문 | 주요 증거 |
|---|---:|---|---|
| Boundary integrity | 12 | 의존 방향과 컴포넌트 소유권이 한 방향인가 | 금지 import, direct/two-hop fan-out, 컴포넌트 SCC |
| Change locality | 10 | 한 기능 변경이 몇 책임·컴포넌트로 퍼지는가 | 최근 400개 production commit, co-change, churn × reverse dependency |
| Responsibility cohesion | 6 | 패키지가 한 이유로 바뀌는가 | cross-package co-change, export volatility × external consumers; LOC·파일 수는 진단 전용 |
| Contract explicitness | 8 | 경계가 compile time에 검증되는 typed contract인가 | exported dynamic type ratio와 worst tail; dependency bag·raw API blast는 진단 전용 |
| AI navigability | 12 | AI가 실제 진입점·의존 방향·불변식·검증법을 찾을 수 있는가 | 실재 symbol/path/test/command 링크, 위험 가중 package 안내와 가까운 guide |
| AI change readiness | 8 | AI가 한 변경의 범위·원본 계약·집중 검증을 바로 구성할 수 있는가 | source↔test 탐색, scope guide, registry/entrypoint, generated source linkage, 실행 가능한 verify command |
| Complexity hotspots | 7 | 가장 어려운 실행 경로도 안전하게 이해 가능한가 | 고정 rank cyclomatic/cognitive worst tail; p95·maximum·raw count는 진단 전용 |
| Runtime safety | 7 | 실패 원인이 보존되고 library가 복구 경계를 우회하지 않는가 | lost error chain과 library fatal; ignored result·context·goroutine·inline timeout은 진단 전용 |
| Test effectiveness | 14 | 테스트가 독립적인 위험 행동을 실제 oracle로 보호하는가 | risk obligation, oracle-bearing unique shape, production subject locality |
| Test maintainability | 8 | 테스트가 복제 없이 의도적이고 격리되어 탐색 가능한가 | file-local shape uniqueness, behavior naming, clock/global/network hazard; 파일 크기는 진단 전용 |
| Delivery confidence | 8 | 제품 영향이 큰 변경을 역할에 맞는 gate가 막는가 | lane별 static/build/test/integration/lock 증거와 Go/Kotlin/TS/Python/Shell/Rust 제품 영향도 |

가중치 합은 100이며 AI coding 용이성 두 축이 합계 20점이다. 평균적인 작은
함수나 패키지를 추가해 hotspot을 희석하지 못하도록 tail과 위험 가중치를
사용한다. 테스트 case 기반 비율은 raw case 수가 아니라 같은 파일 안의 semantic
shape를 하나로 접어 계산한다. Git 이력이 없거나 필수 source inventory가 비면
해당 축을 100점으로 건너뛰지 않고 측정 실패로 처리한다.

Change locality는 raw 변경 빈도를 관측 증거로 계속 보고하되, 그 빈도만으로
feature ownership hotspot을 감점하지 않는다. Hotspot은 같은 패키지가 반복적인
cross-component 변경에 참여할 때만 점수화한다. 조립 루트를 포함한 산개 변경,
cross-package co-change, 현재 reverse dependency와 결합된 volatile-hub 위험은
그대로 점수화한다. 따라서 조립 루트 경로로 코드를 옮기는 것만으로는 경계 문제를
숨길 수 없고, 과거 wiring 빈도도 보고서에서 사라지지 않는다.

언어 lane의 제품 영향도는 Go gateway 45%, Kotlin native 25%, TypeScript desktop
15%, Python operations 10%, Shell operations 3%, Rust/Tauri 2%로 고정한다. 이는 LOC
비율이 아니라 런타임 소유권과 사용자 노출도를 나타낸다. 새 핵심 제품 surface가
생기면 rubric version과 함께 명시적으로 재검토한다.

`score`와 `readiness`는 분리한다. Score는 장기 개선 추세이고 readiness는 현재
revision의 format, vet, lint, test, race 실행 결과다. Fast 프로필에서 실행 증거를
전달하지 않으면 readiness는 `unknown`, `healthy=false`로 명시한다. CI의 기존
언어별 gate 실패는 점수와 관계없이 merge를 막는다.

**Ratchet은 CI 밖이다 (운영자 결정 2026-07-18).** git-window 기반 pillar 점수는
무관한 PR을 허위 레드로 물들인다(systemd 유닛 1파일 diff에 5-pillar 동시 하락
관측) — 우는 게이트는 진짜 레드를 무시하게 학습시킨다. PR CI와 로컬
`make ci`/`ci/fast`에서 `health-v2-check`를 제거했고, scorer 단위 테스트 <!-- docref:ignore -->
(`health-v2-test`)만 게이트에 남는다. Fail-closed 래칫 스윕은 Nightly Drift
Watch 소관이며, 수동 실행은 `make health-v2-check`.

## 측정 신뢰도와 범위

- Metric에 `scored: false`가 있으면 문제 판정이 아니라 조사 위치를 제공하는
  진단값이다. LOC·파일 수·raw goroutine·root context·blank assignment·inline
  HTTP timeout·test file size는 이 방식으로만 보고한다.
- Rubric 2.2의 정밀 AI navigation은 우선 Go gateway package를 측정한다. Kotlin,
  TypeScript, Python의 AI 전용 증거는 아직 측정하지 않는다. 미측정 영역을 0점이나
  100점으로 만들지 않고 report confidence를 낮춘다. 이 두 pillar는 provisional Go
  scope로 해석하고 `measured_product_lanes`와
  `active_lane_product_impact_coverage`를 함께 본다.
- Fast delivery 검사는 주석·표시 이름, literal `if: false`, non-blocking command,
  main과 무관한 branch, 명백한 docs-only trigger를 제외한 실제 command/target 존재를
  확인한다. Lane별 path trigger 정합성, required-check 설정, 최근 실행 성공은
  증명하지 않으므로 deep/readiness와 CI 결과가 최종 실행 증거다.
- Complexity와 runtime fast 검사는 compiler AST를 대신하는 보수적 proxy다. 낮은
  신뢰의 정규식 hit는 감점하지 않고, 실제 error chain 손실·library fatal처럼
  원인이 좁혀지는 신호만 점수화한다.
- Git 이력 측정은 체크아웃 깊이에 중립이어야 한다. Shallow clone은 경계 커밋을
  parent 없는 커밋으로 보여 400-커밋 윈도를 자르고 전체 트리를 가짜 bulk
  커밋으로 노출하므로, 윈도가 shallow 경계에 닿으면 history를 `unavailable`
  측정 실패로 처리하고 `git fetch --unshallow`를 안내한다. 같은 revision이
  체크아웃 깊이에 따라 다른 점수를 조용히 만들지 않기 위한 것이다. CI는
  `fetch-depth: 0` 풀 히스토리로 측정한다.

## Finding 계약

낮은 metric은 반드시 실제 행동으로 이어지는 finding을 만든다. 각 finding에는
다음 정보가 있어야 한다.

- **What**: 관측된 문제와 정량적 증거
- **Why**: 변경 위험, 장애 진단, 계약 안정성에 미치는 영향
- **Where**: 주 경로와 함께 바뀌는 관련 경로
- **How**: 책임 이동, 경계 축소, 계약 또는 테스트 추가 방향
- **Verify**: 개선을 확인하는 구체적인 명령이나 검사

Finding ID는 rule, path, symbol 또는 dependency edge에서 안정적으로 생성한다.
표시 순서는 score gain이 아니라 severity와
`change frequency × blast radius × test gap` 위험을 따른다. 같은 remediation과
verification을 공유하는 증상은 하나의 intervention으로 묶고, 중복 finding 수를
합산해 우선순위를 부풀리지 않는다.

## Baseline과 ratchet

기본 허용치는 overall 0.3점, pillar 1.0점이다. `python3 … --check` / nightly는
다음 중 하나라도 발생하면 실패한다.

- overall이 허용치보다 더 하락
- 어느 pillar든 허용치보다 더 하락
- 새로운 `critical` finding 발생 (또는 기존 finding이 `critical`로 악화)
- schema, rubric, profile 또는 pillar 집합 불일치

새 `high` finding은 보고서에 남지만 `--check`를 막지 않는다 — rolling git
윈도 tip churn이 무관한 PR을 빨간불로 만들지 않기 위함이다. Baseline *update*는
여전히 새 high/critical을 거부한다. PR CI의 Health Bench ratchet 스텝은
advisory(`continue-on-error`); fail-closed 스윕은 Nightly Drift Watch다.

Baseline update는 허용치를 적용하지 않는 엄격한 단방향 갱신이다. Overall이나
어떤 pillar라도 낮추거나 새 고위험 finding을 받아들이는 갱신은 거부한다.
Baseline 파일을 직접 편집해 check를 통과시키지 않는다.

최초 v2 전환은 v1 점수를 산술 변환하지 않고 현재 tree를 v2 rubric으로 다시
측정한다. 현재 main이 의도한 초기 구간인 45–55점을 벗어나면 기준선을 쓰지
말고 rubric과 증거 수집을 검토한다.

```bash
python3 scripts/audit/codebase-health-v2.py \
  --migrate-v1 scripts/audit/health-baseline.json \
  --expect-band 45:55 \
  --write-baseline scripts/audit/health-v2-baseline.json
```

기존 v2 baseline과 점수 의미가 달라지는 rubric 변경도 일반 update로 우회하지
않는다. 이전 파일을 migration source로 읽고 현재 tree를 새 rubric으로 재측정한다.
예상 구간은 목표 점수가 아니라 리뷰 중 합의한 sanity check이며, 이전 rubric·점수와
provenance는 새 baseline에 보존된다.

```bash
python3 scripts/audit/codebase-health-v2.py \
  --migrate-rubric scripts/audit/health-v2-baseline.json \
  --migration-reason "Describe the reviewed scoring-policy change" \
  --expect-band 45:55 \
  --write-baseline scripts/audit/health-v2-baseline.json
```

일상 측정과 ratchet 확인은 다음 명령을 사용한다.

```bash
make health-v2
make health-v2-check
make health-v2-deep
make health-v2-test
```

Baseline을 올릴 때는 실제 개선 diff와 검증 결과를 함께 리뷰한다.

```bash
python3 scripts/audit/codebase-health-v2.py --update-baseline
```

## 변경 시 검증

Scorer나 baseline 정책을 바꾸면 작은 synthetic repository fixture로 다음을
검증한다.

- 같은 입력의 JSON과 finding 순서가 byte-for-byte 동일
- threshold와 score 함수가 단조적이며 pillar weight 합이 100
- 새 high/critical finding과 pillar 퇴행이 composite 개선으로 가려지지 않음
- baseline 하향 갱신과 schema/rubric 불일치를 거부
- 필수 증거 누락이 100점으로 바뀌지 않음
- 테스트 복제와 파일 분할만으로 관련 점수가 오르지 않음
- human/Markdown 출력이 점수표보다 상위 intervention의 What, Why, Where, How,
  Verify를 먼저 표시

`fast` 검사는 모든 PR에서 실행한다. `deep`은 정해진 도구 버전과 입력으로
nightly 또는 수동 실행하고, 안정화 전에는 PR 차단 기준으로 사용하지 않는다.
