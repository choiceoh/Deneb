---
name: evening-letter
version: "1.0.0"
category: productivity
description: "매일 저녁 이브닝레터 생성 및 발송. 내일 일정, 미처리 메일, 임박 마감을 수집해 하루를 마무리하고 내일을 준비하는 회고 브리핑을 작성한다. Use when: 이브닝레터, evening letter, 저녁 브리핑, 하루 마무리, 내일 준비, daily wrap-up. NOT for: 모닝레터(아침 브리핑), 일반 메일 분석, 회신 작성, 장문 회의록 정리."
metadata:
  {
    "deneb":
      {
        "emoji": "🌙",
        "tags": ["briefing", "daily", "evening", "summary", "wrap-up"],
        "related_skills": ["morning-letter", "summarize"],
      },
  }
---

# 이브닝레터

매일 저녁 발송하는 하루 마무리 + 내일 준비 레터. 모닝레터의 저녁 짝.

## 실행 조건

- 크론잡 `evening-letter`가 매일 저녁(예: 21:00 KST)에 자동 트리거
- 사용자가 "이브닝레터", "저녁 브리핑", "하루 마무리" 등을 요청할 때 수동 실행
- 기본 모델: 게이트웨이의 메인 모델을 자동 사용 (크론잡 payload에 model 미지정 시 defaultModel 폴백)

## 모닝레터와의 차이

저녁은 **앞을 보는** 시간이다. 아침의 시장 데이터(날씨·환율·구리시세)는 빼고,
하루를 닫고 내일을 여는 데 필요한 3개 섹션만 모은다.

| | 모닝레터 🌅 | 이브닝레터 🌙 |
|---|---|---|
| 성격 | 하루 시작 브리핑 | 하루 마무리 + 내일 준비 |
| 섹션 | 날씨·환율·구리·일정·메일·마감 (6) | 일정·메일·마감 (3) |
| 어조 | 활기차게 여는 인사 | 차분한 회고와 정리 |

## 실행 절차

### 1단계: 데이터 수집

`evening_letter` 도구를 호출한다. 파라미터 없이 호출하면 3개 섹션(일정·메일·
마감)을 병렬 수집하여 JSON 데이터를 반환한다. **deferred 도구**라 스키마가 안
보이면 `fetch_tools`로 `evening_letter`를 먼저 로드한다.

```json
{"tool": "evening_letter"}
```

반환 데이터 구조:
- `date`: 오늘 날짜 (한국어, 요일 포함)
- `sections.calendar`: 일정 목록 (오늘 잔여 + 내일)
- `sections.email`: 수신 메일 목록 (발신자, 제목, 스니펫)
- `sections.deadlines`: 위키에서 스캔한 임박 마감 목록 (`items`: title, category, due, days_left)

각 섹션에 `ok: false`이면 해당 섹션 조회에 실패한 것이다.

### 2단계: 레터 카드 작성

도구가 반환한 데이터를 **deneb-ui 카드**로 작성한다. 네이티브 클라이언트(안드로이드·PC)가 채팅 피드 안에서 리치 컴포넌트로 렌더한다. 저녁은 **앞을 보는** 시간이라 시장 데이터(날씨·환율·구리)는 빼고 **3개 카드**(내일 일정·챙길 메일·임박 마감)만 담는다. 출력은 **두 부분**:

1. **머리말 한 줄** (펜스 밖, 평문): `오늘도 수고하셨어요 — {date}.` 뒤에 핵심 한 줄(내일이 빡빡하면 일찍 쉬라는 배려, 또는 가장 임박한 마감). 차분한 회고 어조.
2. **deneb-ui 카드** (펜스): 아래 스켈레톤을 실제 데이터로 치환한 **유효한 JSON 한 개**.

#### 카드 스켈레톤

완성 예시다. 구조(`column` > `card`들)는 유지하고 값만 실제 데이터로 바꾼다. 빈 섹션은 규칙대로 카드째 생략한다.

```deneb-ui
{
  "type": "column",
  "children": [
    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "calendar", "size": 16 },
        { "type": "text", "value": "내일 일정", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "10:00 — 분기 리뷰" },
        { "type": "text", "value": "15:00 — 거래처 콜" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "mail", "size": 16 },
        { "type": "text", "value": "챙길 메일", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "이대리 — 내일 회의자료 공유" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "alarm", "size": 16 },
        { "type": "text", "value": "임박 마감", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "부가세 신고", "style": "body" },
        { "type": "badge", "value": "D-2" } ] } ] }
  ]
}
```

#### 슬롯 채우기 규칙

- **카드 헤더**: 각 카드 첫 `row`는 `icon` + `text(caption)`. 아이콘 고정 — 일정 `calendar`, 메일 `mail`, 마감 `alarm`.
- **내일 일정**: `calendar.events`는 오늘+내일 포함 — 지난 오늘 일정은 접고 **내일**만 `list`로, 각 항목 `text` `"HH:MM — 제목"`, 최대 8건. 없으면 `list` 대신 `text` `"내일 일정 없음 — 여유로운 하루"`.
- **챙길 메일**: `list`, 각 항목 `text` `"발신자 — 제목 요약"`, "내일 답장할 것" 관점 상위 3~5건. 발신자는 이름만. 없으면 `text` `"처리할 메일 없음"`.
- **임박 마감**: 마감마다 `row`에 `text(body)` 제목 + `badge` `"D-N"`. `days_left`로 D-N(0=`"D-day"`, 음수=`"기한 초과"`), 오름차순. 결제기한·납기 누락 금지. **`items`가 비면 이 카드 전체를 생략한다.**
- **실패 섹션**(`ok:false`): 그 카드 본문에 `text` `"조회 실패"`.
- **전체 실패**: 모든 섹션 실패여도 머리말 한 줄 + 최소 카드(날짜)는 출력.

#### JSON 규칙 (엄수)

- 펜스는 deneb-ui 블록 **정확히 한 개**, 그 안은 **유효한 JSON 객체 하나**. 한국어는 그대로 쓴다(이스케이프 불필요). 따옴표·쉼표·중괄호 짝을 정확히 맞춘다.
- 노드·필드를 지어내지 마라. 쓸 노드는 예시의 것뿐: `column`/`card`/`row`/`text`(style: `caption`·`body`)/`list`/`icon`/`badge`.
- 펜스 앞뒤에 머리말 한 줄 외의 설명·상태 텍스트를 넣지 마라.

#### 전달 규칙 (중요)

- **최종 응답 = 머리말 한 줄 + deneb-ui 카드.** 그 외 별도 텍스트나 끝 턴의 `NO_REPLY`, "완료", "발송했어" 같은 확인 문구를 덧붙이지 마라. 마지막 턴의 텍스트가 곧 전달되는 메시지다.
- `message` 툴을 호출하지 마라. 크론/채팅 인프라가 최종 텍스트를 자동으로 원본 채널로 전달한다.
- 채널 연결 상태를 추측하지 마라. "텔레그램이 연결이 안 되어서", "여기에 직접 전달", "채널 미연결" 같은 문구를 **절대 사용하지 마라** — 당신이 지금 응답하는 곳이 곧 사용자 표면이다.
- 툴 에러가 발생해도 에러 메시지를 사용자에게 전달하지 마라. 수집된 데이터를 그대로 포맷하여 최종 레터만 출력하라.
