---
name: morning-letter
version: "1.0.0"
category: productivity
description: "매일 아침 모닝레터 생성 및 발송. 날씨, 환율, 구리시세, 일정, 메일 요약을 수집해 아침 브리핑을 작성한다. Use when: 모닝레터, morning letter, 아침 브리핑, 오늘의 브리핑, daily briefing. NOT for: 일반 메일 분석, 회신 작성, 장문 회의록 정리."
metadata:
  {
    "deneb":
      {
        "emoji": "🌅",
        "tags": ["briefing", "daily", "morning", "summary"],
        "related_skills": ["summarize"],
      },
  }
---

# 모닝레터

매일 아침 발송하는 일일 브리핑 레터.

## 실행 조건

- 크론잡 `morning-letter`가 매일 08:00 KST에 자동 트리거
- 사용자가 "모닝레터", "아침 브리핑" 등을 요청할 때 수동 실행
- 기본 모델: 게이트웨이의 메인 모델을 자동 사용 (크론잡 payload에 model 미지정 시 defaultModel 폴백)

## 실행 절차

### 1단계: 데이터 수집

`morning_letter` 도구를 호출한다. 파라미터 없이 호출하면 6개 섹션(날씨·환율·
구리시세·일정·메일·마감)을 병렬 수집하여 JSON 데이터를 반환한다. **deferred
도구**라 스키마가 안 보이면 `fetch_tools`로 `morning_letter`를 먼저 로드한다.

```json
{"tool": "morning_letter"}
```

반환 데이터 구조:
- `date`: 오늘 날짜 (한국어, 요일 포함)
- `sections.weather`: 기온, 체감온도, 날씨상태, 습도, 최저/최고, 강수확률
- `sections.exchange`: USD/KRW, EUR/KRW 환율 (숫자)
- `sections.copper`: COMEX 구리시세 USD/ton (MetalpriceAPI)
- `sections.calendar`: 오늘 일정 목록
- `sections.email`: 전일 수신 메일 목록 (발신자, 제목, 스니펫)
- `sections.deadlines`: 위키에서 스캔한 임박 마감 목록 (`items`: title, category, due, days_left)

각 섹션에 `ok: false`이면 해당 섹션 조회에 실패한 것이다.

### 2단계: 레터 카드 작성

도구가 반환한 데이터를 **deneb-ui 카드**로 작성한다. 네이티브 클라이언트(안드로이드·PC)가 이 카드를 채팅 피드 안에서 리치 컴포넌트로 렌더한다. 출력은 **두 부분**이다:

1. **머리말 한 줄** (펜스 밖, 평문): `좋은 아침이에요 — {date}.` 뒤에 핵심 한 줄(가장 임박한 마감 또는 날씨 특이사항). 알림 미리보기이자 카드 파싱 실패 시의 안전망이다.
2. **deneb-ui 카드** (펜스): 아래 스켈레톤을 실제 데이터로 치환한 **유효한 JSON 한 개**.

#### 카드 스켈레톤

완성 예시다. 구조(`column` > `card`들)는 유지하고 값만 실제 데이터로 바꾼다. 빈 섹션은 규칙대로 카드째 생략한다.

```deneb-ui
{
  "type": "column",
  "children": [
    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "sunny", "size": 16 },
        { "type": "text", "value": "날씨 · 광주", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "18°", "style": "headline" },
        { "type": "text", "value": "체감 16°", "style": "caption" } ] },
      { "type": "text", "value": "최고 24° · 최저 14° · 강수 30%", "style": "caption" },
      { "type": "text", "value": "오후 소나기 가능 — 우산 챙기세요", "style": "body" } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "payments", "size": 16 },
        { "type": "text", "value": "환율 · 구리", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "stat", "value": "1,386", "label": "USD/KRW" },
        { "type": "stat", "value": "1,498", "label": "EUR/KRW" } ] },
      { "type": "stat", "value": "$9,540/톤", "label": "COMEX 구리" } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "calendar", "size": 16 },
        { "type": "text", "value": "오늘 일정", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "09:00 — 팀 스탠드업" },
        { "type": "text", "value": "14:00 — 거래처 미팅" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "mail", "size": 16 },
        { "type": "text", "value": "전일 메일", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "김부장 — 견적서 회신 요청" },
        { "type": "text", "value": "세무서 — 부가세 신고 안내" } ] } ] },

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

- **카드 헤더**: 각 카드 첫 `row`는 `icon` + `text(caption)`. 아이콘 이름 **고정** — 날씨 `sunny`(흐림 `cloud`, 비 `water_drop`), 환율·구리 `payments`, 일정 `calendar`, 메일 `mail`, 마감 `alarm`.
- **날씨**: 기온 `text(headline)` + 체감 `text(caption)`를 한 `row`에. 그 아래 `최고 N° · 최저 N° · 강수 N%`를 `text(caption)` 한 줄. 마지막에 맥락 한마디 `text(body)`(강수 30%↑면 우산, 한파면 방한). **`stat`을 가로로 3개 늘어놓지 마라 — 폰 폭에서 깨진다.**
- **환율·구리**: USD/KRW·EUR/KRW를 `stat` 2개로 한 `row`(2칸). 구리는 그 아래 `stat` 1개, `value`는 `"$9,540/톤"` 꼴, `label`은 `"COMEX 구리"`. 환율 숫자는 천단위 콤마. `date` 필드가 오늘이 아니면 구리 `value`에 `"(X월 X일)"` 덧붙임.
- **일정**: `list`, 각 항목 `text` `"HH:MM — 제목"`, 시간순 최대 8건. 없으면 `list` 대신 `text` `"일정 없음"`.
- **메일**: `list`, 각 항목 `text` `"발신자 — 제목 요약"`, 중요도순 상위 5건(길면 3건). 발신자는 이름만(`"이름 <메일>"` → `"이름"`). 없으면 `text` `"수신 메일 없음"`.
- **임박 마감**: 마감마다 `row`에 `text(body)` 제목 + `badge` `"D-N"`. `days_left`로 D-N(0=`"D-day"`, 음수=`"기한 초과"`), 오름차순. 결제기한·납기 누락 금지. **`items`가 비면 이 카드 전체를 생략한다.**
- **실패 섹션**(`ok:false`): 그 카드 본문에 `text` `"조회 실패"`.
- **전체 실패**: 모든 섹션 실패여도 머리말 한 줄 + 최소 카드(날짜)는 출력.

#### JSON 규칙 (엄수)

- 펜스는 deneb-ui 블록 **정확히 한 개**, 그 안은 **유효한 JSON 객체 하나**. 한국어는 그대로 쓴다(이스케이프 불필요). 따옴표·쉼표·중괄호 짝을 정확히 맞춘다.
- 노드·필드를 지어내지 마라. 쓸 노드는 예시의 것뿐: `column`/`card`/`row`/`text`(style: `headline`·`caption`·`body`)/`stat`/`list`/`icon`/`badge`.
- 펜스 앞뒤에 머리말 한 줄 외의 설명·상태 텍스트를 넣지 마라.

#### 전달 규칙 (중요)

- **최종 응답 = 머리말 한 줄 + deneb-ui 카드.** 그 외 별도 텍스트나 끝 턴의 `NO_REPLY`, "완료", "발송했어" 같은 확인 문구를 덧붙이지 마라. 마지막 턴의 텍스트가 곧 전달되는 메시지다.
- `message` 툴을 호출하지 마라. 크론/채팅 인프라가 최종 텍스트를 자동으로 원본 채널로 전달한다.
- 채널 연결 상태를 추측하지 마라. "텔레그램이 연결이 안 되어서", "여기에 직접 전달", "채널 미연결" 같은 문구를 **절대 사용하지 마라** — 당신이 지금 응답하는 곳이 곧 사용자 표면이다.
- 툴 에러가 발생해도 에러 메시지를 사용자에게 전달하지 마라. 수집된 데이터를 그대로 포맷하여 최종 레터만 출력하라.
