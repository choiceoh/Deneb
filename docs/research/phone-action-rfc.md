# RFC: 폰 액션을 SSH/Termux → 인앱 Intent 실행으로

> 상태: **draft (검토 대기)** · 작성: 2026-06 · 관련: `tools/phone.go`, `client-android` (FcmService·Platform.android), `client_push.go`(SSE), `push/fcm.go`(FCM data) · 계기: orailnoor/private-agent 영상(안드로이드 Accessibility 에이전트)에서 "앱이 직접 폰을 조작" 아키텍처만 발췌

## 0. TL;DR

Deneb의 폰 **액션**을 깨진 **SSH/Termux 브리지**에서 **네이티브 인앱 Intent 실행**으로 옮긴다. 게이트웨이 phone 툴이 SSH로 `am start`/`input`을 쏘는 대신, **이미 존재하는 게이트웨이→앱 채널**(SSE foreground / FCM data background)로 액션 명령을 보내고 **앱이 Android Intent로 실행**한다.

- **로그 증거**: 로그 스윕에서 `phone ssh failed (255) ×22/일` — phone 툴의 SSH 경로가 죽어 있음(st26 Termux sshd/키). 인앱 실행은 이 의존을 제거한다.
- **이미 깔린 것**: 앱에 `DenebNotificationListenerService`(알림 읽기)·`SmsReader`(SMS 읽기)·`FcmService`(FCM 수신)·`Platform.android` Intent(`ACTION_VIEW`/`ACTION_SEND`). 게이트웨이엔 SSE 푸시허브 + FCM data payload. **새 채널 불필요 — 재사용.**
- **영상에서 안 가져오는 것**: 연속 스크린샷→멀티모달→탭을 *기본* 제어로. 토큰 폭식·fragile. (Accessibility는 P3 fallback 한정.)

## 1. 배경 — 현재 폰 능력

| 능력 | 현황 | 경로 |
|---|---|---|
| 알림 읽기 | ✅ | `DenebNotificationListenerService` → 게이트웨이 |
| SMS 읽기 | ✅ | `SmsReader` |
| 폰에 알림 push | ✅ | FCM (`push/fcm.go`) + SSE (`client_push.go`) |
| 폰 **읽기**(location·battery·calllog·contacts·clipboard) | ⚠️ | `phone.go` → `ssh` exec (Termux) |
| 폰 **액션**(notify·tts·기타) | ⚠️ | `phone.go` → `ssh` exec |

`phone.go`는 `exec.CommandContext(ctx, "ssh", …)`로 폰(Termux)에 명령한다. 이게 **깨진 부분**: `DENEB_PHONE_SSH`→st26 ssh가 실패(`phoneSSHFailureBackoff`까지 둠), 로그에 `phone ssh failed (255) ×22`. 즉 **읽기·액션 둘 다 fragile한 SSH 단일점**에 묶여 있다.

## 2. 빈 곳 & 영상에서 발췌할 것

영상(orailnoor/private-agent)의 핵심은 **폰 조작을 앱 자체가 실행**한다는 것(SSH/root 없이). Deneb의 안전한 읽기·푸시는 **이미 있으니** 가져올 건 그게 아니라:

→ **폰 액션을 앱 인앱 실행으로.** 단 영상은 *모든* 액션을 Accessibility 좌표탭으로 하는데, Deneb는 **Intent/API 되는 건 Intent로**(신뢰·저비용), Accessibility는 fallback만.

## 3. 설계 — push 명령 → 인앱 Intent

```
게이트웨이 phone 툴 (action)
        │  구조화 액션 명령 { "kind":"phone_action", "action":"open_url", "args":{...} }
        ├── foreground: clientPushHub → SSE /api/v1/miniapp/events   (새 pushKindPhoneAction)
        └── background: FCM data payload (FcmService가 앱 깨워 실행)
                                   │
                          앱: FcmService / SSE 소비처 → dispatchPhoneAction()
                                   │
                          Android Intent (ACTION_VIEW/SEND/CALL/SENDTO, MediaStore 카메라 …)
```

- **명령 운반**: 기존 채널 재사용. 앱 foreground면 SSE(`clientPushEvent`에 `pushKindPhoneAction` 추가 + 구조화 payload), background면 FCM `data` payload(`FCMSender.Send(..., data)` 이미 지원). FCM은 앱이 죽어 있어도 깨운다.
- **앱 실행**: `FcmService`(FCM)와 SSE 소비처가 액션을 `dispatchPhoneAction(action, args)`로 디스패치 → 적절한 `Intent` + `startActivity`. 앱에 `ACTION_VIEW`·`ACTION_SEND` 이미 있음 → `ACTION_CALL`/`ACTION_SENDTO`(SMS)/카메라 추가.
- **게이트웨이 툴**: phone 툴의 액션 분기를 SSH-exec 대신 명령 emit으로. 결과 불필요한 fire-and-forget.

## 4. 액션셋 (P1)

| action | Intent | 비고 |
|---|---|---|
| `open_url` / `open_app` | `ACTION_VIEW` (uri/package) | 이미 있음 |
| `share` | `ACTION_SEND` | 이미 있음 |
| `message` | `ACTION_SENDTO` (sms:/mailto:) 또는 앱 deep-link | 수신자·본문 |
| `dial` / `call` | `ACTION_DIAL`(확인) / `ACTION_CALL`(권한) | 민감 → 승인 |
| `photo` | `MediaStore.ACTION_IMAGE_CAPTURE` | 찍어서 게이트웨이로 회신(P2 채널) |

## 5. 단계

**P1 — 단방향 액션 (이 RFC 목표).** 위 액션셋을 push→Intent로. 응답 불필요. 깨진 SSH 의존 제거의 핵심. 앱 변경 + 게이트웨이 툴 분기.

**P2 — 읽기 (요청/응답).** battery·location·contacts·clipboard를 앱 Android API로 읽어 SSH 읽기 대체. 게이트웨이가 SSE로 요청 push → 앱이 읽어 `POST /api/v1/miniapp/rpc`로 회신(왕복). photo 회신도 여기. SSH 읽기 단계적 폐기.

**P3 — Accessibility fallback (선택, 게이트).** Intent/API 없는 서드파티 앱을 정말 조작해야 할 때만 스크린샷→모델→탭. 비용·안전 게이트, 명시적 사용자 허용. **기본 제어 아님.**

## 6. 보안

- **액션 allowlist**: phone 툴이 emit하는 action은 고정 집합만. 임의 명령 실행 없음.
- **민감 액션 승인**: `call`(실제 발신)·`message`(외부 발송)은 기존 untrusted-tool 승인 게이트(`untrusted_tool_gate.go`) 통과 — 첫 호출 needs_approval, 확인 후 실행.
- **untrusted 입력 차단**: 외부 메일/카카오 페이스트가 폰 액션을 트리거하지 못하게 기존 게이트 적용(이미 forwarded-email/kakao-paste를 untrusted로 표시).
- **로컬 채널**: SSE/FCM은 인증된 client-token. 명령은 게이트웨이→앱(서버 발신)만.

## 7. 안 가져올 것 (명시)

- **연속 스크린샷 → 멀티모달 → 좌표 탭을 *기본* 제어로** — 매 액션 멀티모달 호출(토큰 폭식, 영상 본인도 "DeepSeek 권장" 비용), 좌표추정 오탭 fragile. **P3 fallback 한정.**
- Telegram 제어 — Deneb는 telegram 채널 폐기(앱+PC만). 불필요.

## 8. 검증

- 게이트웨이: gofmt/gofumpt·`go build ./...`·`go vet`·`go test`. 액션 명령 직렬화 + allowlist 단위 테스트.
- **앱(Compose/Android): 실기기 검증 필요 — 작성자가 못 함.** Intent 실행·권한(CALL/카메라)·FCM data 깨우기·SSE 디스패치는 st26에서 사용자가 확인. (`renderPreviews`는 UI만, Intent 실행은 실기기.)

## 9. 마이그레이션

- P1 도입 후 phone 툴 액션 분기를 SSH→인앱으로 교체, `DENEB_PHONE_SSH` 의존 축소.
- P2 후 읽기까지 옮기면 SSH/Termux 경로 완전 폐기 → `phone ssh failed ×22` 소멸.
- 과도기: SSH 경로를 fallback으로 남겨 두되 로그 경고(인앱 우선).

## 10. 구현 체크리스트 (P1)

- [ ] `clientPushEvent`에 구조화 payload + `pushKindPhoneAction` (또는 별도 command 이벤트 타입)
- [ ] FCM data payload로 동일 액션 운반 (background)
- [ ] 앱 `FcmService`/SSE 소비처 → `dispatchPhoneAction(action, args)` → Intent
- [ ] 게이트웨이 phone 툴: 액션 분기 SSH→emit, allowlist
- [ ] 민감 액션(call/message) 승인 게이트 연결
- [ ] 단위 테스트(명령 직렬화·allowlist) + 앱 실기기 검증(사용자)
