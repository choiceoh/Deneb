---
title: "Native-only 후속 개선 검토"
summary: "PR 1922/1927 이후 native client 단일 표면에서 남은 legacy 라우팅, 문서, 도구 설명을 분류하고 다음 PR 후보를 정한다."
read_when:
  - PR 1922 이후 남은 Telegram/legacy 흔적을 정리할 때
  - native Android client 중심으로 heartbeat, session restore, auto-resume을 손볼 때
  - 다음 개선 PR 범위를 고를 때
---

# Native-only 후속 개선 검토

**기준:** `origin/main` @ `407028d0` (`#1926`, `#1927` 포함)
**일시:** 2026-06-02
**목적:** native client가 유일한 사용자 표면이 된 뒤, 아직 Telegram 중심 가정이 남아 실제 동작이나 문서 신뢰도를 해칠 수 있는 지점을 다음 PR 후보로 정리한다.

## 결론

다음으로 가장 가치 있는 PR은 **native session lifecycle 복구**다. 하트비트, 세션 복원, 자동 재개가 아직 `telegram:` 세션만 정상 대상으로 본다. PR 1922 이후에는 이 경로가 native client의 `client:*` 세션을 놓칠 수 있으므로, autonomous heartbeat와 crash recovery가 조용히 약해진 상태다.

권장 순서:

| 순위 | 후보 | 영향 | 크기 |
|---|---|---|---|
| P0 | Heartbeat를 `client:*` / `client:main`으로 라우팅 | 자동 작업이 다시 실제 native 홈에서 돈다 | S |
| P0 | session restore / auto-resume을 `client:*`로 일반화 | 재시작 후 대화 목록·중단 턴 복구가 맞아진다 | M |
| P1 | proactive delivery 운영 문서 native-only 재작성 | 운영자가 더 이상 `/use-forum`/forum target을 따르지 않는다 | S |
| P1 | LLM tool schema의 Telegram UI 설명 제거 | 모델이 native app에서 잘못된 affordance를 떠올리지 않는다 | S-M |
| P2 | Mini App 용어 정리 | 유지보수 문맥이 깔끔해진다 | M |

## P0. Heartbeat native session 라우팅

현재 `heartbeat_task.go`는 하트비트 실행 대상을 “최근 active telegram session”으로 제한한다.

- `gateway-go/internal/runtime/server/heartbeat_task.go:8`
- `gateway-go/internal/runtime/server/heartbeat_task.go:110`
- `gateway-go/internal/runtime/server/heartbeat_task.go:119`

문제:

- native client의 기본 홈은 `client:main`이다.
- 앱에서 새 대화를 시작하면 `client:main:<uuid>`도 생긴다.
- 그런데 heartbeat는 `telegram:` prefix가 아니면 skip한다.
- 결과적으로 native-only 전환 뒤 `HEARTBEAT.md`가 있어도 최근 활동이 native 세션이면 실행되지 않는다.

다음 PR 제안:

- `resolveHeartbeatSessionKey(last string) string` 같은 작은 헬퍼를 만든다.
- `client:` 세션이면 그대로 사용한다.
- 최근 활동이 없거나 cron/system 같은 내부 세션이면 `client:main`으로 fallback한다.
- 기존 idle guard는 유지한다.
- Telegram legacy transcript가 남아 있으면 더 이상 우선하지 않는다.

테스트:

- `TestHeartbeatRunsOnClientMain`
- `TestHeartbeatRunsOnRecentClientSession`
- `TestHeartbeatFallsBackToClientMainWhenNoClientActivity`
- `TestHeartbeatSkipsWhenRecentlyActive`

## P0. Session restore / auto-resume native 일반화

### Session restore

현재 `restoreAndWakeSessions`는 transcript 디렉터리에서 `telegram:` 파일만 복원한다.

- `gateway-go/internal/runtime/server/session_restore.go:13`
- `gateway-go/internal/runtime/server/session_restore.go:36`
- `gateway-go/internal/runtime/server/session_restore.go:67`

문제:

- Android 대화 서랍은 `miniapp.sessions.recent`를 통해 session manager를 본다.
- 재시작 직후 `client:*` transcript가 있어도 session manager에 복원되지 않으면 최근 대화 목록이 빈약해질 수 있다.
- `client:main:<uuid>`는 사용자가 만든 실제 새 대화이므로 transient로 취급하면 안 된다.

다음 PR 제안:

- restore 대상 prefix를 `client:`로 바꾼다.
- `telegram:`은 migration/legacy 표시 목적이면 optional로 복원하되 channel은 `legacy` 또는 `telegram`으로 남긴다.
- `client:*`는 `Channel: "client"`, `Kind: session.KindDirect`, `StatusDone`으로 복원한다.

### Auto-resume

현재 auto-resume은 run marker를 보다가 `telegram:`이 아니면 marker를 삭제한다.

- `gateway-go/internal/runtime/server/auto_resume.go:362`
- `gateway-go/internal/runtime/server/auto_resume.go:368`
- `gateway-go/internal/runtime/server/auto_resume.go:432`

문제:

- native client turn이 도중에 끊기면 `client:*` marker가 생길 수 있다.
- 현 코드는 그 marker를 “non-telegram”으로 보고 삭제할 수 있다.
- 즉 crash recovery가 native 세션에서 작동하지 않는다.

다음 PR 제안:

- `parseResumableSession(sessionKey)`로 일반화한다.
- `client:`는 chat delivery 없이 `sessionKey`만 넣고 synthetic `chat.send`를 보낸다.
- `telegram:`은 legacy compatibility로 유지하거나 별도 path로 남긴다.
- `cron`, `system`, `btw`, `acp`는 계속 skip/delete한다.

테스트:

- `TestAutoResume_DispatchesClientSession`
- `TestAutoResume_DeletesDoneClientMarker`
- `TestAutoResume_SkipsCronAndSystem`
- 기존 Telegram 테스트는 legacy로 이름 변경하거나 축소한다.

## P1. Proactive delivery 문서 재작성

`docs/operations/proactive-delivery.md`는 아직 `/use-forum`, Telegram General topic, forum thread target 중심이다.

문제:

- 운영 문서가 현재 실제 표면과 다르다.
- `server_chat_config.go`에는 이미 `deliverTo`가 Telegram target-specific이라 더 이상 consult하지 않는다는 주석이 있다.
- 문서만 보고 운영하면 존재하지 않는 `/use-forum` 경로를 따라가게 된다.

다음 PR 제안:

- active home을 `client:main`으로 재정의한다.
- Gmail poll / wiki dreaming / cron handoff가 native proactive relay와 transcript mirror를 어떻게 쓰는지로 설명한다.
- `deliverTo`, `threadId`, forum topic target은 legacy/deprecated 섹션으로 내려 보낸다.
- `docs/operations/mail-analysis.md`, `docs/operations/index.md`의 링크 설명도 같이 맞춘다.

## P1. Tool schema Telegram affordance 제거

LLM이 실제로 읽는 tool description에 Telegram UI가 남아 있다.

- `gateway-go/internal/pipeline/chat/toolreg/core.go:212`
- generated schema에는 “send to Telegram”, “Target channel (e.g., telegram)”, “Telegram inline-keyboard button label” 표현이 남아 있다.

문제:

- native app에서는 Telegram inline keyboard가 없다.
- 모델이 clarification이 필요할 때 잘못된 UI affordance를 전제로 행동할 수 있다.
- generated schema는 prompt에 들어가므로 단순 주석보다 영향이 크다.

다음 PR 제안:

- `clarify` 설명을 “native choice prompt / plain numbered fallback”으로 바꾼다.
- `message`/cron delivery schema의 Telegram 예시를 `client` 또는 generic `channel`로 바꾼다.
- schema generator를 재실행해 `tool_schemas*.json`을 갱신한다.

주의:

- `ToolClarify` 자체가 아직 Telegram inline directive를 만드는 구조라면, 이 PR에서 설명만 바꾸지 말고 native fallback 동작을 같이 확인해야 한다.
- 더 큰 개선은 `kai-ui` 선택 폼으로 clarify를 렌더링하는 것이다. 이것은 별도 PR이 더 안전하다.

## P2. Mini App 용어 정리

`miniapp.*` RPC namespace는 그대로 둘 수 있다. 이미 Android client가 이 계약을 사용하고 있고, API 이름을 바꾸면 호환성 비용이 크다.

다만 코드 주석과 문서에는 “Mini App webview”가 아직 많이 남아 있다.

정리 기준:

- API namespace: `miniapp.*` 유지
- 사용자/문서 표현: `native client`
- 서버 주석: “native miniapp API” 정도로 절충
- package rename은 하지 않는다

## 이번에 바로 하지 않을 것

- 과거 연구 문서의 Telegram 언급 전체 삭제: 역사적 분석 가치가 있으므로 noise가 아니다.
- 테스트 fixture의 `telegram:` 전면 치환: session key 호환성 테스트로 의미가 있는 경우가 많다.
- `platform/telegram` 관련 secret reference 문서 전면 삭제: 실제 config migration 정책을 먼저 정해야 한다.

## 다음 PR 추천

가장 좋은 다음 PR 제목:

`fix(server): route heartbeat and resume through native sessions`

범위:

- `heartbeat_task.go`
- `session_restore.go`
- `auto_resume.go`
- 관련 테스트
- proactive-delivery 문서는 같은 PR에 넣지 말고 후속 문서 PR로 분리

이유:

- 기능 회복 효과가 즉각적이다.
- PR 1922/1927의 native-only 방향과 직접 맞물린다.
- 문서 정리보다 실제 사용자 체감이 크다.
- 변경 범위가 서버 lifecycle에 모이므로 검증 포인트가 명확하다.

## 실행 메모 (2026-06-03)

작업 시작 후 이 문서의 P0 범위를 구현 대상으로 확정했다.

- Heartbeat는 최근 `client:*` 세션을 우선하고, 없으면 `client:main`으로 fallback한다.
- Chat handler는 사용자 기원 native/legacy 세션 활동을 `ActivityTracker`에 기록한다.
- Session restore는 `client:*` transcript를 `Channel: "client"`로 복원한다.
- Auto-resume은 `client:*` run marker를 삭제하지 않고 synthetic `chat.send`로 이어 붙인다.
- Legacy direct `telegram:<id>`는 transcript migration compatibility 용도로 유지한다.
