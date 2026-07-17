---
name: morning-letter
version: "1.2.1"
category: productivity
description: "매일 아침 모닝레터 생성 및 발송. 날씨, 환율, 구리시세, 일정, 메일, 미결 전자결재 요약을 수집해 아침 브리핑을 작성한다. Use when: 모닝레터, morning letter, 아침 브리핑, 오늘의 브리핑, daily briefing. NOT for: 일반 메일 분석, 회신 작성, 장문 회의록 정리."
metadata:
  {
    "deneb":
      {
        "emoji": "🌅",
        "tags": ["briefing", "daily", "morning", "summary"],
        "triggers": ["모닝레터", "아침 브리핑", "오늘의 브리핑", "morning letter"],
        "requires_tools": ["morning_letter"],
      },
  }
---

# 모닝레터

매일 아침 발송하는 일일 브리핑 레터.

## 실행 조건

- 크론잡 `morning-letter`가 매일 08:00 KST에 자동 트리거
- 사용자가 "모닝레터", "아침 브리핑" 등을 요청할 때 수동 실행
- 기본 모델: 게이트웨이의 메인 모델을 자동 사용 (크론잡 payload에 model 미지정 시 defaultModel 폴백)

## 필수 환경변수

- `METALPRICEAPI_KEY`: MetalpriceAPI 무료 키 (구리시세 조회용)

## 실행 절차

### 1단계: 데이터 수집

`morning_letter` 도구를 바로 한 번 호출한다. 정확한 트리거로 이 스킬이 자동
로드되면 필수 도구 스키마도 첫 요청부터 함께 활성화된다. 파라미터 없이 호출하면
8개 섹션(날씨·환율·구리시세·일정·메일·마감·미해결질문·미결 전자결재)을 병렬
수집하여 JSON 데이터를 반환한다.

```json
{"tool": "morning_letter"}
```

반환 데이터 구조:
- `date`: 오늘 날짜 (한국어, 요일 포함)
- `sections.weather`: 기온, 체감온도, 날씨상태, 습도, 최저/최고, 강수확률
- `sections.exchange`: USD/KRW 환율 — `usd_krw`(숫자, 해석용)와 `usd_krw_token`(카드 `value`에 넣을 플레이스홀더). EUR도 오지만 레터에는 쓰지 않는다
- `sections.copper`: LME 구리시세 USD/ton — `price_per_ton_usd`(숫자, 해석용)와 `token`(카드 `value`에 넣을 플레이스홀더)
- `sections.calendar`: 오늘 일정 목록
- `sections.email`: 전일 수신 메일 목록 (발신자, 제목, 스니펫)
- `sections.deadlines`: 위키에서 스캔한 임박 마감 목록 (`items`: title, category, due, days_left)
- `sections.open_questions`: 7일 이상 열려 있는 프로젝트 미해결 질문 (`items`: project, question, asked, age_days) — 내부 소스로 답을 못 찾은 것들이니 "사람에게 확인할 것"으로 승격하는 섹션
- `sections.groupware_pending`: 미결 전자결재 (`count`, `stale_count`, `items`: doc_id, title, drafter, date, age_hours, escalation_level, stale_label). `stale_label`이 있으면 방치(4h/24h) 건 — 카드에서 강조. 미설정(`configured:false`)이거나 0건이면 카드 생략

각 섹션에 `ok: false`이면 해당 섹션 조회에 실패한 것이다.

반환된 섹션을 해당 데이터의 정본으로 사용한다. `calendar`·`mail_archive`·`wiki`로
같은 일정·메일·마감·미해결질문을 다시 수집하지 마라. 프롬프트가 위키의 운용 규칙이나
프로젝트 맥락 확인을 별도로 요구하면 그 확인은 유지하되, `morning_letter` 호출 뒤에는
중복 조회하지 않는다. 특정 섹션이 `ok:false`이고 결과에 꼭 필요할 때만 그 섹션 하나를
개별 도구로 보완한다.

미해결 질문 카드 규칙: `open_questions.items`가 비어 있지 않으면 마감 카드 뒤에
"확인 필요" 카드를 하나 추가한다 — 항목마다 `{project}: {question} ({age_days}일째)`
한 줄. 비어 있으면 카드째 생략(다른 빈 섹션과 동일 규칙).

### 2단계: 레터 카드 작성

도구가 반환한 데이터를 **deneb-ui 카드**로 작성한다. 네이티브 클라이언트(안드로이드·PC)가 이 카드를 채팅 피드 안에서 리치 컴포넌트로 렌더한다. 출력은 **두 부분**이다:

1. **머리말 한 줄** (펜스 밖, 평문): `좋은 아침이에요 — {date}.` 뒤에 핵심 한 줄(가장 임박한 마감 또는 날씨 특이사항). 알림 미리보기이자 카드 파싱 실패 시의 안전망이다.
2. **deneb-ui 카드** (펜스): 아래 스켈레톤을 실제 데이터로 치환한 **라벨 HTML 마크업 한 덩어리** (루트 `<column>` 하나).

#### 카드 스켈레톤

완성 예시다. 구조(`<column>` > `<card>`들)는 유지하고 값만 실제 데이터로 바꾼다. 빈 섹션은 규칙대로 카드째 생략한다.

```deneb-ui
<column>
  <text style="headline">7월 7일 화요일</text>
  <text style="caption">아침 레터 · 데네브</text>
  <hr/>
  <card>
    <row><icon name="sunny" size="16"/><text style="caption">날씨 · 광주</text></row>
    <row><text style="headline">18°</text><text style="caption">체감 16°</text></row>
    <text style="caption">최고 24° · 최저 14° · 강수 30%</text>
    <text style="body">오후 소나기 가능 — 우산 챙기세요</text>
  </card>
  <card>
    <row><icon name="payments" size="16"/><text style="caption">환율 · 구리</text></row>
    <row><stat value="{{market:usd_krw}}" label="USD/KRW"/><stat value="${{market:copper}} /t" label="LME 구리"/></row>
  </card>
  <card>
    <row><icon name="calendar" size="16"/><text style="caption">오늘 일정</text></row>
    <ul><li>09:00 — 팀 스탠드업</li><li>14:00 — 거래처 미팅</li></ul>
  </card>
  <card>
    <row><icon name="mail" size="16"/><text style="caption">전일 메일</text></row>
    <ul><li>김부장 — 견적서 회신 요청</li><li>세무서 — 부가세 신고 안내</li></ul>
  </card>
  <card>
    <row><icon name="alarm" size="16"/><text style="caption">임박 마감</text></row>
    <row><text style="body">부가세 신고</text><badge color="warning">D-2</badge></row>
    <row><text style="body">진코 선입금 상계</text><badge color="error">기한 초과</badge></row>
  </card>
  <card>
    <row><icon name="assignment" size="16"/><text style="caption">미결 전자결재 · 2건</text></row>
    <ul><li>지출품의 — 김승리 <badge color="warning">4시간째 방치</badge></li><li>휴가신청 — 이대리</li></ul>
  </card>
</column>
```

#### 슬롯 채우기 규칙

- **마스트헤드**: 카드들 앞에 `text(headline)`로 날짜("M월 D일 요일" 꼴), 그 아래 `text(caption)`으로 `아침 레터 · 데네브`, 이어서 `<hr/>` 한 줄. 레터의 1면 제호다.
- **카드 헤더**: 각 카드 첫 `row`는 `icon` + `text(caption)`. 아이콘 이름 **고정** — 날씨 `sunny`(흐림 `cloud`, 비 `water_drop`), 환율·구리 `payments`, 일정 `calendar`, 메일 `mail`, 마감 `alarm`, 미결결재 `assignment`.
- **날씨**: 기온 `text(headline)` + 체감 `text(caption)`를 한 `row`에. 그 아래 `최고 N° · 최저 N° · 강수 N%`를 `text(caption)` 한 줄. 마지막에 맥락 한마디 `text(body)`(강수 30%↑면 우산, 한파면 방한). **`stat`을 가로로 3개 늘어놓지 마라 — 폰 폭에서 깨진다.**
- **환율·구리**: USD/KRW와 LME 구리를 `stat` 2개로 한 `row`(2칸). **EUR/KRW는 쓰지 않는다.** **시세 숫자를 직접 쓰지 마라** — 도구 JSON이 준 토큰(exchange `usd_krw_token`, copper `token`)을 스켈레톤처럼 `value`에 그대로 배치하면 발송 시 서버가 실측 숫자로 자동 치환한다. 토큰은 숫자만 치환되므로 구리 `value`는 `"${{market:copper}} /t"` 꼴로 단위를 감싼다(`label`은 `"LME 구리"`). `date` 필드가 오늘이 아니면 구리 `value`에 `"(X월 X일)"` 덧붙임. 섹션이 `ok: false`거나 토큰 필드가 없으면 그 stat 대신 `<text>조회 실패</text>`.
- **일정**: `<ul>`, 각 항목 `<li>HH:MM — 제목</li>`, 시간순 최대 8건. 없으면 `<ul>` 대신 `<text>일정 없음</text>`.
- **메일**: `<ul>`, 각 항목 `<li>발신자 — 제목 요약</li>`, 중요도순 상위 5건(길면 3건). 발신자는 이름만(`"이름 <메일>"` → `"이름"`). 없으면 `<text>수신 메일 없음</text>`.
- **임박 마감**: 마감마다 `<row>`에 `<text style="body">` 제목 + `<badge>D-N</badge>`. `days_left`로 D-N(0=`"D-day"`, 음수=`"기한 초과"`), 오름차순. 긴급도를 배지 색으로: D-3 이하 `<badge color="warning">`, 기한 초과·D-day `<badge color="error">`, 그 외 색 없음. 결제기한·납기 누락 금지. **항목이 없으면 이 카드 전체를 생략한다.**
- **미결 전자결재** (`groupware_pending`): `count>0`일 때만 카드. 아이콘 `assignment`. 각 항목 `<li>제목 — 기안자</li>`; `stale_label` 있으면 뒤에 `<badge color="warning">방치</badge>` 또는 배지 텍스트로 stale_label. `stale_count>0`이면 머리말/카드 캡션에 방치 건수 언급. **0건·미설정이면 카드 생략.**
- **실패 섹션**(`ok:false`): 그 카드 본문에 `<text>조회 실패</text>`.
- **전체 실패**: 모든 섹션 실패여도 머리말 한 줄 + 최소 카드(날짜)는 출력.

#### 마크업 규칙 (엄수)

- 펜스는 deneb-ui 블록 **정확히 한 개**, 그 안은 루트 `<column>` **하나**의 HTML 마크업. 한국어는 그대로 쓴다(이스케이프 불필요).
- 태그를 지어내지 마라. 쓸 태그는 예시의 것뿐: `column`/`card`/`row`/`text`(style: `headline`·`caption`·`body`)/`stat`/`ul`·`li`/`icon`/`badge`/`hr`(마스트헤드 구분선).
- 여는 태그는 닫는다(`<card>…</card>`). 속성값은 큰따옴표. 카드 본문에 백틱(`` ` ``)이나 코드펜스를 넣지 마라.
- 변수/치환 문법은 **도구 JSON이 명시적으로 준 시세 토큰(`{{market:…}}`)만** 허용 — 이것만 발송 시 서버가 치환한다. 그 외의 `{{…}}`·`${…}` 문법을 지어내면 치환되지 않고 그대로 노출된다. 시세 외 모든 수치·텍스트는 실제 값으로 쓴다.
- 강조는 `<b>`/`<strong>`이 아니라 `**굵게**` 인라인 마크다운으로 (`text`·`li` 안에서 지원된다).
- 펜스 앞뒤에 머리말 한 줄 외의 설명·상태 텍스트를 넣지 마라.

#### 전달 규칙 (중요)

- **최종 응답 = 머리말 한 줄 + deneb-ui 카드.** 그 외 별도 텍스트나 끝 턴의 `NO_REPLY`, "완료", "발송했어" 같은 확인 문구를 덧붙이지 마라. 마지막 턴의 텍스트가 곧 전달되는 메시지다.
- `message` 툴을 호출하지 마라. 크론/채팅 인프라가 최종 텍스트를 자동으로 원본 채널로 전달한다.
- 채널 연결 상태를 추측하지 마라. "텔레그램이 연결이 안 되어서", "여기에 직접 전달", "채널 미연결" 같은 문구를 **절대 사용하지 마라** — 당신이 지금 응답하는 곳이 곧 사용자 표면이다.
- 툴 에러가 발생해도 에러 메시지를 사용자에게 전달하지 마라. 수집된 데이터를 그대로 포맷하여 최종 레터만 출력하라.
