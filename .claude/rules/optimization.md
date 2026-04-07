---
description: "상수/파라미터 반복 최적화 전략 — 오토리서치 프롬프트 기반, 라이브 테스트 연동"
globs: ["gateway-go/**/*.go"]
---

# Iterative Optimization (Autoresearch Methodology)

> 원본: `gateway-go/internal/autoresearch/runner.go` buildPrompt(), buildConstantsPrompt()
> 클로드 코드가 직접 반복 최적화를 수행할 때 이 전략을 따른다.

## 도구

| Command | What |
|---|---|
| `scripts/dev-iterate.sh` | 빌드→서버→smoke 3체크→결과 (~2초) |
| `scripts/dev-iterate.sh --metric CMD` | 커스텀 metric |
| `scripts/dev-quality-metric.sh [PORT] [MSG]` | 채팅 품질 점수 0-100 (15~60초) |

### 오토리서치 (자율 최적화 루프)

| Command | What |
|---|---|
| `scripts/dev-metric-gen.sh list` | metric 프리셋 목록 |
| `scripts/dev-metric-gen.sh PRESET` | metric 스크립트 생성 (smoke\|quality\|combined) |
| `scripts/dev-autoresearch.sh start --target FILE --metric PRESET` | 오토리서치 시작 |
| `scripts/dev-autoresearch.sh status` | 상태 확인 |
| `scripts/dev-autoresearch.sh results --json` | 결과 JSON |
| `scripts/dev-autoresearch.sh stop` | 정지 |
| `scripts/dev-ar-results.sh --json` | 구조화된 결과 (게이트웨이 불필요) |
| `scripts/dev-ar-results.sh --suggest` | 다음 행동 제안 |

### metric 프리셋 선택 가이드

| 수정 대상 | 추천 metric | 이유 |
|---|---|---|
| 시스템 프롬프트/채팅 파이프라인 | `quality` (0~100) | 한국어 응답 품질 직접 측정 |
| 전반적 품질 | `combined` | smoke(20%) + quality(80%) |
| 인프라/시작 성능 | `smoke` (0~3) | 빠른 빌드+시작 확인 |
| 특정 메시지 응답 | `custom "메시지"` | 해당 메시지 품질 직접 측정 |

### 오토리서치 사용 절차

1. **metric 선택**: 수정 대상에 맞는 preset 선택
2. **시작**: `scripts/dev-autoresearch.sh start --target FILE --metric PRESET`
3. **모니터링**: `scripts/dev-autoresearch.sh status` (진행 확인)
4. **결과 해석**: `scripts/dev-ar-results.sh --json` → `DENEB_AR_RESULTS` 파싱
5. **다음 행동**: `scripts/dev-ar-results.sh --suggest` → 계속/전환/멈춤 판단
6. **적용**: 오토리서치가 찾은 최적값을 소스에 반영, `make check` 확인

### 결과 해석 (DENEB_AR_RESULTS JSON)

| 필드 | 의미 |
|---|---|
| `suggestion` | `continue_exploration` / `continue_exploitation` / `try_different_approach` / `change_strategy` / `stop_and_review` / `completed` |
| `improvement_pct` | 베이스라인 대비 개선율 (%) |
| `success_rate` | kept / total 비율 |
| `consecutive_failures` | 연속 실패 횟수 (3+ = 전략 전환 필요) |
| `top_changes` | 효과적이었던 변경 목록 + delta |
| `recent_failures` | 최근 실패한 가설 (반복 방지) |

---

## Autoresearch System Prompt (원문 요약)

아래는 오토리서치 러너가 LLM에게 보내는 시스템 프롬프트의 핵심 전략이다.
클로드 코드도 **동일한 원칙**으로 반복 최적화를 수행해야 한다.

### Identity & Mission

- 목표: 지정된 metric을 maximize 또는 minimize
- 루프: 매 반복마다 ONE change → 테스트 → keep(개선) or revert(퇴보)

### Hard Constraints

1. **target files만 수정.** 다른 파일 수정 금지.
2. **새 의존성/import 추가 금지** (이미 있는 것만 사용).
3. **metric 평가 로직을 제거하거나 비활성화하지 마라.**
4. **metric 값을 하드코딩하거나 조작하지 마라.**
5. **sleep/delay로 시간 예산을 소비하지 마라.**
6. **각 실험은 고정된 시간 예산** 내에서 완료되어야 한다.

### Strategy: Exploration vs Exploitation

오토리서치는 iteration 번호에 따라 전략을 전환한다:

| Phase | Iterations | 전략 |
|---|---|---|
| **Early** | 1~3 | 넓게 탐색. 다양한 접근법, 아키텍처, 하이퍼파라미터 시도 |
| **Exploration** | 4~15 | 효과 있는 것 활용. 최적 접근법 정제 |
| **Exploitation** | 16~30 | 미세 조정. 작고 정밀한 조정으로 이득 짜내기 |
| **Fine-tune** | 30+ | 쉬운 이득은 끝남. 극도로 작은 변화만 |

### Change Granularity

- **한 번에 하나의 집중된 atomic change.** 관련 없는 변경을 절대 합치지 마라.
- 개선 안 되면 **변경 전부 revert.** 부분 유지 없음.
- **작은 변화가 큰 리팩터보다 낫다.** 2줄 변경으로 metric 개선 > 50줄 재작성으로 무변화.

### Learning from History

- 히스토리를 꼼꼼히 분석. **어떤 TYPE의 변경**이 효과적인지 패턴 파악.
- X 증가가 효과 → X를 더 증가 시도 (수확 체감 주의).
- 특정 방향이 **계속 실패하면 그 방향을 멈춰라.**
- keep된 변경이 **왜** 효과 있었는지 이해한 후에 다음을 결정.
- 상호작용 주의: 변경 A가 효과 + 변경 B가 효과 → 합치면 충돌하거나 상승할 수 있음.

### Stuck Recovery (연속 실패)

| 연속 실패 | 대응 (오토리서치 원문) |
|---|---|
| **3회 (Mild)** | 전략 전환. 하이퍼파라미터 대신 구조 변경, 또는 더 보수적인 변경. 히스토리에서 미탐색 방향 찾기. |
| **5회 (Moderate)** | 현재 접근을 **완전히 포기.** keep된 실험으로 돌아가서 그 원리부터 다시 시작. "같은 것을 반복하면서 다른 결과를 기대하는 것은 미친 짓." |
| **8회+ (Critical)** | 극단적 조치. **가장 단순한 작동 구성으로 복원.** 모든 복잡한 가설 폐기. 최근 실패의 **반대** 방향 시도. |

### Constants Override Mode (상수 최적화)

오토리서치의 constants 모드 전략:

- 상수만 변경 가능, 소스 코드 수정 불가
- 각 상수에 type(float/int/string)과 min/max 범위가 있음
- **한 번에 1~2개 상수만 변경.** 격리된 변경이 평가하기 쉬움.
- 증가가 효과 → 더 증가 (수확 체감 주의)
- 일관되게 실패하는 방향 → 멈춤
- **상수 간 비선형 상호작용**을 고려

### Trend Analysis

오토리서치는 히스토리에서 자동으로 추세를 분석한다:

- **Best metric trajectory**: baseline → 현재 best (개선율 %)
- **Keep rate**: keep된 반복 / 전체 반복
- **Recent trend**: 최근 N회의 방향
- **Plateau detection**: 연속 N회 revert → 정체기 경고
- **Successful changes**: keep된 것만 모아서 패턴 파악
- **Recent failures**: 최근 실패한 가설 목록 (반복 방지)

---

## 실행 절차

### 1. Baseline

```bash
scripts/dev-iterate.sh
# 결과 기록: metric=N, latency=Nms
```

### 2. 반복

```
가설 → Edit → dev-iterate.sh → ITERATE_RESULT 파싱 → keep/revert → 기록 → 반복
```

### 3. 결과 테이블

| # | 상수 | 값 | metric | keep | latency | 가설 |
|---|------|-----|--------|------|---------|------|
| 0 | baseline | - | 3 | - | 1773ms | baseline |
| 1 | ... | ... | ... | ✓/✗ | ... | ... |

### 4. 최종

최적값을 확정하고 `make check` 통과 확인.

---

## 품질 metric 상세

`scripts/dev-quality-metric.sh`가 측정하는 항목 (총 100점):

| 항목 | 배점 | 기준 |
|---|---|---|
| korean_ratio | 25 | 응답의 한국어 비율 (>50%=25, >30%=20, >10%=10) |
| substance | 25 | 응답 길이/내용 충실도 (>100자=25, >30자=10) |
| clean | 20 | 내부 토큰 누출/AI 필러 없음 |
| latency | 15 | 응답 시간 (<10s=15, <20s=12, <30s=8) |
| streaming | 15 | 이벤트 흐름 정상 (delta>5 + event>10 = 15) |
| penalty | -10/err | 도구 에러당 -10점 |
