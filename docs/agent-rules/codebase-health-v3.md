---
description: "Health Bench 3.0 점수·finding·baseline·도메인 ratchet 규약"
globs: ["scripts/audit/health-bench-v3.py", "scripts/audit/codebase_health_v3.py", "scripts/audit/health_v3/**", "scripts/audit/health-v3-*.json"]
---

# Codebase Health Bench 3.0

Health Bench 3.0은 구조·런타임·RSI 피트니스를 하나의 벤치로 묶고, 월드클래스
절대 기준으로 점수를 매긴다. 설계 SoT는
`docs/research/health-bench-3.0.md` 이다.
v1/v2 점수와 비교하거나 산술 변환하지 않는다.

## 평가 원칙

- Overall은 도메인 점수의 **가중 기하평균**이다 (Structure 0.55 · Runtime 0.25 ·
  Fitness 0.20). 한 도메인의 개선으로 다른 도메인 퇴행을 가리지 않는다.
- 필수 증거가 `unavailable`이면 측정 실패다. 모르는 상태를 100으로 채우지 않는다.
- Fitness는 초기에는 advisory(`ratcheted=false`)이다. `--check`는 Structure와
  Runtime만 래칫한다.
- LOC·파일 수·테스트 복제는 점수가 아니다. 파일 분할만으로 점수가 오르면 rubric
  버그다.

## Rubric 3.0.0

| Domain | Weight | Metrics |
|---|---:|---|
| Structure | 0.55 | change-blast, boundary-contracts, ai-maintainability, complexity-safety, test-truth, test-craft, delivery-proof, dead-surface |
| Runtime | 0.25 | stability, error-rate, llm-serving, turn-reliability, tool-reliability, latency |
| Fitness | 0.20 | live-delta, trend-28d, finding-land, feed-card |

첫 baseline expect-band는 **45–55**이다.

## Baseline과 ratchet

```bash
make health-v3
make health-v3-check
make health-v3-deep
make health-v3-test
python3 scripts/audit/health-bench-v3.py --update-baseline
python3 scripts/audit/health-bench-v3.py --refresh-runtime-cache --write-snapshot
```

- `scripts/audit/health-v3-baseline.json` — 일방향 래칫
- `~/.deneb/data/health-v3-runtime-cache.json` — fast 프로필 Runtime 입력 (TTL 72h,
  라이브 정본 — deep/`--refresh-runtime-cache` 가 여기 쓴다).
  `scripts/audit/health-v3-runtime-cache.json` 은 리프레시를 한 번도 안 돌린 호스트용
  체크인 seed 폴백일 뿐이다 — 체크인 캐시를 라이브로 쓰면 auto-deploy 트리 청결 때문에
  갱신을 되돌리게 되고, TTL 이 지나면 fast 소비자 전체가 조용히 죽는다 (2026-07-18~27
  실사고: health-finding miner 가 9일간 v2 폴백)
- Baseline 손편집으로 check를 통과시키지 않는다. 운영자 승인 후에만
  `--update-baseline` / `--write-baseline`을 사용한다.

## 어디서 자동으로 도는가

- **나이틀리** `nightly-drift.yml`의 `health-v3` 잡 (srv4, advisory). PR CI는
  래칫하지 않는다 — health-v2 웨지(8일 연속 나이틀리 RED)를 반복하지 않으려고
  래칫은 페이징 경로 밖에 둔다. 회귀하면 `health-bench` 라벨로 추적 이슈
  하나를 열고 갱신할 뿐, 나이틀리를 레드로 만들지 않는다.
- **srv4 타이머** `deneb-bench-refresh.timer` (매일 04:33)는 스냅샷만 쓰고
  래칫을 판정하지 않는다 — 회귀 감지는 위 나이틀리 잡이 담당한다.
- 체크인된 runtime 캐시는 TTL 72h라 대개 만료 상태다(2026-07-25 기준 235h).
  나이틀리는 항상 `--refresh-runtime-cache`로 라이브 저널을 다시 읽는다.
  게이트웨이 저널이 없는 호스트에서 `make health-v3-check`는 Runtime 증거
  부재로 fail-closed 되는 것이 정상이다.

## 변경 시 검증

Scorer를 바꾸면 `make health-v3-test`로 다음을 확인한다.

- 워크시트 목표 구간(~50)과 geometric composite
- 약한 도메인이 composite를 끌어내림 (상쇄 금지)
- Runtime 캐시 TTL fail-closed
- Fitness 상승이 Structure 래칫 실패를 가리지 않음
