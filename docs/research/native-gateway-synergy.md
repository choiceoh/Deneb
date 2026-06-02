---
title: "네이티브 앱과 게이트웨이 시너지 개선"
summary: "PR 1922 이후 native client 단일 표면을 기준으로 앱과 게이트웨이의 계약을 정리하고, 즉시 구현한 개선과 다음 과제를 정리한다."
read_when:
  - Native Android client와 Gateway 사이 기능 계약을 바꿀 때
  - PR 1922 이후 Telegram 제거 영향도를 확인할 때
  - 앱 설정/토픽/세션 UX를 확장할 때
---

# 네이티브 앱과 게이트웨이 시너지 개선

**기준:** PR 1922 `feat(server): retire Telegram bot — native client is the sole surface`
**일시:** 2026-06-02
**결론:** 네이티브 앱이 이제 유일한 사용자 표면이므로, 게이트웨이는 앱이 스스로 상태와 기능을 발견할 수 있는 계약을 제공해야 한다. 앱은 그 계약을 읽어 설정, 토픽, 세션, 캡처, 푸시 UX를 더 안정적으로 구성한다.

## 왜 바꾸는가

PR 1922 이전에는 Telegram bot이 “가벼운 기본 표면”이고 native client가 “풍부한 보조 표면”에 가까웠다. PR 1922 이후에는 native client가 유일한 daily surface가 되었다. 따라서 앱은 더 이상 과거 Telegram 맥락을 추정하면 안 되고, 게이트웨이도 native client가 필요한 정보를 명시적으로 알려줘야 한다.

이번 변경의 핵심은 세 가지다.

1. 앱이 게이트웨이 버전, 현재 모델, 기능 플래그, HTTP 엔드포인트를 한 번에 확인한다.
2. 기존 `topics.map`을 native topic/session 계약으로 변환해 앱이 바로 열 수 있는 세션 키를 받는다.
3. Android 설정과 대화 서랍이 이 정보를 사용해 “연결됨 / 무엇이 가능함 / 어느 토픽으로 이어짐”을 사용자에게 보여준다.

## 이번에 구현한 것

### 1. `miniapp.client.hello`

게이트웨이에 native handshake RPC를 추가했다.

- 메서드: `miniapp.client.hello`
- 인증: `client-token`으로 검증된 `clientauth.Identity` 필요
- 응답: `version`, `nativeApiVersion`, `model`, `capabilities`, `endpoints`, `tsMs`

좋은점:

- Android 앱이 여러 RPC를 찍어보지 않고 한 번에 상태를 확인한다.
- 기능 플래그가 생겨서 Gmail, Calendar, Wiki, Search, Cron, Capture 같은 화면을 “가능할 때만” 정확하게 열 수 있다.
- 설정 화면에서 게이트웨이 연결 상태를 사용자에게 설명할 수 있다.
- 앞으로 API 버전 협상이 필요해져도 `nativeApiVersion`을 기준으로 분기할 수 있다.

### 2. `miniapp.topics.list` / `miniapp.topics.resolve`

기존 `deneb.json`의 `topics.map`을 native client용 토픽 목록으로 변환한다.

- `sourceId`: 기존 설정의 topic/source ID
- `key`: 지식 파일 키
- `label`: 앱 표시명
- `sessionKey`: 앱이 이어서 사용할 게이트웨이 세션 키
- `isDefault`: 업무 홈 여부

기본 업무 홈은 항상 `client:main`으로 제공된다. 설정이 비어 있어도 앱은 빈 대화 화면 대신 업무 홈을 열 수 있다.

좋은점:

- 앱이 legacy thread ID를 알 필요가 없다.
- 토픽 선택은 “표시명 → sessionKey”만 따르면 된다.
- 업무 홈, 코딩, 개인, 잡담 같은 토픽이 세션과 지식 주입을 같은 키로 공유한다.
- 과거 `topics.map` 설정을 버리지 않고 native 계약으로 재사용한다.

### 3. Android 연동

Android `DenebGatewayClient`에 상태와 토픽 흐름을 추가했다.

- `clientStatus: StateFlow<ClientStatus?>`
- `denebTopics: StateFlow<List<ClientTopic>>`
- `refreshClientStatus()`
- `refreshTopics()`
- 대화 목록은 native topics를 먼저 보여주고, 그 아래 최근 세션을 붙인다.
- 현재 세션 키를 `currentConversationId`로 직접 노출해 서랍의 활성 표시가 따라온다.

설정 화면에는 게이트웨이 상태 카드가 추가됐다.

- Gateway 버전
- Native API 버전
- 현재 모델
- 활성 capability 목록
- 토픽 목록 요약

좋은점:

- 사용자는 “토큰을 넣었는데 실제 연결됐는지”를 바로 확인한다.
- 앱은 토픽/세션을 서버 계약에 맞춰 열기 때문에 하드코딩이 줄어든다.
- PR 1922 이후에도 남아 있던 Telegram 용어가 사용자 화면에서 사라진다.

## 변경했을 때의 전체 이점

가장 큰 이점은 **앱과 게이트웨이 사이의 추측이 줄어든다**는 점이다. 앱은 더 이상 “아마 이 기능이 있겠지”라고 가정하지 않고, 게이트웨이가 알려주는 기능 플래그와 토픽 목록을 기준으로 움직인다.

운영 측면의 이점도 크다.

- 새 기능을 게이트웨이에 붙이면 capability만 추가해도 앱이 점진적으로 대응할 수 있다.
- 토픽 이름이나 세션 키를 바꿔도 앱 업데이트 없이 서버 계약으로 흡수할 수 있다.
- 장애가 있을 때 설정 화면의 상태 카드가 1차 진단점이 된다.
- 네이티브 앱이 유일한 표면이라는 PR 1922의 방향이 코드와 문서에 같이 반영된다.

사용자 경험 측면에서는 다음이 좋아진다.

- 첫 실행 후 업무 홈이 안정적으로 열린다.
- 대화 서랍에서 토픽과 최근 세션의 구분이 더 자연스럽다.
- 현재 모델과 활성 기능을 앱 안에서 확인할 수 있다.
- 알림, 캡처, 주소록, 위젯 같은 native-only 기능을 Telegram과 비교하지 않고 앱 자체의 기능으로 설명할 수 있다.

## 남은 과제

### P0. Android 컴파일 환경 정리

현재 작업 환경에는 Java/JDK가 잡혀 있지 않아 Android Gradle 컴파일을 끝까지 돌리지 못했다. 로컬 Android Studio JBR 또는 JDK 17 경로를 `JAVA_HOME`에 잡은 뒤 `:composeApp:compileDebugKotlinAndroid`를 재실행해야 한다.

### P1. 토픽 UX 다듬기

이번 변경은 대화 서랍 목록에 토픽을 먼저 올리는 최소 구현이다. 다음 단계에서는 토픽 행을 최근 세션과 시각적으로 구분하거나, 별도 “토픽” 섹션으로 분리하는 것이 좋다.

### P1. 운영 문서 전면 정리

일부 오래된 문서에는 아직 “Telegram + native client” 병행 구조가 남아 있다. PR 1922 이후에는 문서의 기본 설명을 native-only로 맞춰야 한다.

### P2. Capability 기반 화면 게이팅

현재는 상태 카드가 capability를 표시한다. 다음 단계에서는 각 화면 진입 조건도 capability를 사용해 더 정확히 막거나 안내할 수 있다.

### P2. 토픽 지식 파일과 토픽 목록의 왕복 편집

`miniapp.topicdocs.*`는 지식 파일을 편집하고, `miniapp.topics.*`는 토픽 목록을 보여준다. 둘을 연결해 “토픽 선택 → 지식 문서 열기” 흐름을 만들면 앱과 게이트웨이의 운영성이 더 좋아진다.
