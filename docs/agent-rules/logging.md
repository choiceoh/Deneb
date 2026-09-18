---
description: slog 레벨 가이드 — 사용자 무응답/데이터 손실을 로그에 묻지 않기
globs: ["gateway-go/**/*.go"]
---

# Logging Conventions

> **이 프로젝트는 `replyFunc nil`, 미디어 delivery 실패 같은 "유저 무응답" 사건이 `Warn`에 파묻혀 있던 이력이 있습니다. 레벨 선택 규칙을 일관되게 따르세요.**

## 레벨 기준

| 레벨 | 언제 |
|---|---|
| **`Error`** | **사용자가 관찰하는 장애** — 응답 드롭, 상태 손상, 데이터 손실, 외부 시스템(네이티브 클라 push/Gmail) 호출 영구 실패 |
| **`Warn`** | 회복 가능한 이상 — 1회 재시도 후 성공한 케이스, 선택적 기능 실패, 느린 응답 |
| **`Info`** | 평상시 상태 변화 — agent run 시작/완료, 구독 등록, 기동 단계 |
| **`Debug`** | 운영자가 문제 추적할 때만 필요한 상세 — 내부 상태, 토큰 카운트, 재시도 횟수 |

## 규칙

### 1. "사용자가 응답 못 받는 사건"은 무조건 `Error`

- replyFunc 실패, media 전송 실패, 네이티브 클라 delivery/push 영구 실패 (재시도 소진)
- `Warn`으로 찍지 말 것 — 운영자가 평상시 로그에서 안 봄

### 2. 재시도 있는 실패는 2단계 로깅

```go
if err := call(); err != nil {
    logger.Warn("transient fail, retrying", ...)
    err = call()
    if err != nil {
        logger.Error("permanent fail after retry", ...)  // ← 여기가 Error
    }
}
```

### 3. broadcast로 운영자에게도 알림

사용자 영향 있는 실패는 `Error` + `deps.broadcast("chat.delivery_failed", ...)` 병행. 로그만 남기면 UI/모니터링에 안 뜸.

### 4. panic 복구는 항상 `Error`

```go
defer func() {
    if r := recover(); r != nil {
        logger.Error("panic in X", "panic", r)
    }
}()
```

### 5. `Debug`는 샘플링하거나 조건부 적용

- 에이전트 turn마다 30줄씩 찍는 경로는 피하기
- 토큰 수·지연·컨텍스트 크기는 Info에서 필드로 제공, Debug는 정말 필요할 때만

### 6. 필드 네이밍

- 세션: `session` 또는 `sessionKey`
- 채널: `channel` (client 등)
- 에러: `error` (기본)
- ID: `runId`, `jobId`, `messageId` (camelCase 유지)

### 7. 의도된 열화를 찍는 `Warn`은 `(advisory)` 마커를 단다

가드가 **설계대로 동작해서** 찍히는 `Warn`(과금 후보 건너뜀·불건강 모델 우회·
예산 안에서의 축소·인용 검증 실패 드롭 등)은 메시지 끝에 `(advisory)`를 붙인다.
`internal/core/observe`의 `IsAdvisory`가 이 마커를 읽어 genesis 런타임 오류
채굴과 anomaly digest에서 제외한다. 마커가 없으면 자가개선 루프가 이 줄을 코드
결함으로 착각해 하루치 배차 슬롯을 태우고 "이것은 의도된 동작"으로 기각당한다
(2026-08~09 runtime-error 기각 28건 중 11건이 이 경로).

판정은 **작성자가 쓴 마커**로만 한다. 메시지 모양으로 추론하지 말 것 — graceful
degradation은 진짜 결함을 정확히 이 문장 모양으로 강등시키므로, 추론을 넓히면
warn 레인 전체가 죽는다. 같은 가드의 **의도되지 않은** 갈래(폴백이 못 쓰는
체크포인트, 블록 통째 누락)는 마커 없이 두어 채굴 대상으로 남긴다.

마커는 `error` 속성이 아니라 메시지에 단다 — 에러 문자열 안의 "advisory"는 우연이다.

## 금지

- ❌ `log.Printf` / `fmt.Printf` — 구조화 안 됨, `slog` 사용
- ❌ `logger.Warn("...", "error", err)` + `return nil` — 에러를 묻어버림. `Error`로 올리거나 caller에 return
- ❌ 에러 메시지에 credential, API token, 세션 토큰 포함

## PR 체크리스트

- [ ] 사용자 관찰 가능한 실패가 `Error`로 로그되는가
- [ ] 재시도 성공 케이스는 `Warn`, 최종 실패는 `Error`인가
- [ ] delivery/persistence 실패는 broadcast 병행되는가
- [ ] 새 `Warn`/`Error`가 정말 의미 있는 이벤트인가 (스팸 금지)
- [ ] 의도된 열화 `Warn`에 `(advisory)` 마커가 있고, 같은 가드의 의도되지 않은 갈래는 마커 없이 남겼는가
