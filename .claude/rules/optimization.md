---
description: "상수/파라미터 반복 최적화 전략 — 라이브 테스트 연동"
globs: ["gateway-go/**/*.go"]
---

# Iterative Optimization

> 클로드 코드가 직접 반복 최적화를 수행할 때 이 전략을 따른다.

## 도구

| Command | What |
|---|---|
| `scripts/dev/iterate.sh` | 빌드→서버→smoke 2체크→결과 (~2초) |
| `scripts/dev/iterate.sh --metric quality` | 내장 프리셋 (smoke\|quality\|combined) |
| `scripts/dev/iterate.sh --metric CMD` | 커스텀 metric 커맨드 |
| `scripts/dev/quality-metric.sh [MSG]` | 텔레그램 채팅 품질 점수 0-100 (15~60초) |

### metric 프리셋 선택 가이드

| 수정 대상 | 추천 metric | 이유 |
|---|---|---|
| 시스템 프롬프트/채팅 파이프라인 | `quality` (0~100) | 한국어 응답 품질 직접 측정 |
| 전반적 품질 | `combined` | smoke(20%) + quality(80%) |
| 인프라/시작 성능 | `smoke` (0~2) | 빠른 빌드+시작 확인 |
| 특정 메시지 응답 | `custom "메시지"` | 해당 메시지 품질 직접 측정 |

---

## 최적화 전략

### 기본 원칙

- 목표: 지정된 metric을 maximize 또는 minimize
- 루프: 매 반복마다 ONE change → 테스트 → keep(개선) or revert(퇴보)

### Change Granularity

- **한 번에 하나의 집중된 atomic change.** 관련 없는 변경을 절대 합치지 마라.
- 개선 안 되면 **변경 전부 revert.** 부분 유지 없음.
- **작은 변화가 큰 리팩터보다 낫다.** 2줄 변경으로 metric 개선 > 50줄 재작성으로 무변화.

### Strategy: Exploration vs Exploitation

iteration 번호에 따라 전략을 전환한다:

| Phase | Iterations | 전략 |
|---|---|---|
| **Early** | 1~3 | 넓게 탐색. 다양한 접근법, 하이퍼파라미터 시도 |
| **Exploration** | 4~15 | 효과 있는 것 활용. 최적 접근법 정제 |
| **Exploitation** | 16~30 | 미세 조정. 작고 정밀한 조정으로 이득 짜내기 |
| **Fine-tune** | 30+ | 쉬운 이득은 끝남. 극도로 작은 변화만 |

### Learning from History

- 히스토리를 꼼꼼히 분석. **어떤 TYPE의 변경**이 효과적인지 패턴 파악.
- X 증가가 효과 → X를 더 증가 시도 (수확 체감 주의).
- 특정 방향이 **계속 실패하면 그 방향을 멈춰라.**
- keep된 변경이 **왜** 효과 있었는지 이해한 후에 다음을 결정.

### Stuck Recovery (연속 실패)

| 연속 실패 | 대응 |
|---|---|
| **3회 (Mild)** | 전략 전환. 하이퍼파라미터 대신 구조 변경, 또는 더 보수적인 변경. |
| **5회 (Moderate)** | 현재 접근을 **완전히 포기.** keep된 실험으로 돌아가서 그 원리부터 다시 시작. |
| **8회+ (Critical)** | 극단적 조치. **가장 단순한 작동 구성으로 복원.** 최근 실패의 **반대** 방향 시도. |

---

## 실행 절차

### 1. Baseline

```bash
scripts/dev/iterate.sh
# 결과 기록: metric=N, latency=Nms
```

### 2. 반복

```
가설 → Edit → dev-iterate.sh → ITERATE_RESULT 파싱 → keep/revert → 기록 → 반복
```

### 3. 결과 테이블

| # | 상수 | 값 | metric | keep | latency | 가설 |
|---|------|-----|--------|------|---------|------|
| 0 | baseline | - | 2 | - | 1773ms | baseline |
| 1 | ... | ... | ... | ✓/✗ | ... | ... |

### 4. 최종

최적값을 확정하고 `make check` 통과 확인.

---

## 품질 metric 상세

`scripts/dev/quality-metric.sh`가 측정하는 항목 (총 100점):

| 항목 | 배점 | 기준 |
|---|---|---|
| korean_ratio | 25 | 응답의 한국어 비율 (>50%=25, >30%=20, >10%=10) |
| substance | 25 | 응답 길이/내용 충실도 (>100자=25, >30자=10) |
| clean | 20 | 내부 토큰 누출/AI 필러 없음 |
| latency | 15 | 응답 시간 (<10s=15, <20s=12, <30s=8) |
| streaming | 15 | 이벤트 흐름 정상 (delta>5 + event>10 = 15) |
| penalty | -10/err | 도구 에러당 -10점 |
