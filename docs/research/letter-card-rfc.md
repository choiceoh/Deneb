# RFC: 모닝/이브닝레터를 deneb-ui 카드로

> 상태: **draft (검토 대기)** · 작성: 2026-06 · 관련: [[skills/productivity/morning-letter]], [[skills/productivity/evening-letter]], PR #2921(레터 이모지 정리), `.claude/rules/native-design-system.md`

## 0. TL;DR

모닝/이브닝레터를 **평문 마크다운**에서 **deneb-ui 카드**(네이티브 리치 컴포넌트)로 전환한다.
계기는 "레터 이모지가 vibe-coded라 마음에 안 든다 → 모노크롬 → 예쁜 모노크롬 아이콘은 텍스트로 불가"라는 한계였고,
**deneb-ui 카드가 그 한계의 정답**이다(아이콘=모노크롬 벡터, 추가로 stat·countdown·탭액션).

- **앱 코드 변경 거의 없음** — deneb-ui 렌더러는 이미 출하됨(`ui/dynamicui/DenebUiRenderer.kt`). 주 작업은 **SKILL.md가 평문 대신 카드 JSON을 emit**하게 바꾸는 것.
- **시각 검증 가능** — `RenderPreview.kt`(desktopMain)가 이미 `DenebUiRenderer`를 호출(line 1200/1212). 샘플 레터 JSON을 `./gradlew :composeApp:renderPreviews`로 PNG 렌더 → Read로 확인. "실기기 못 봄" 리스크 해소.
- **크로스채널** — 서버 flatten 부재 → **평문 요약(floor) + 카드(enrichment)** 이중 구조로 graceful degrade.

## 1. 배경 · 문제

현재 레터는 크론(`morning-letter` 08:00 / `evening-letter` 21:00)이 트리거 → 에이전트가 `morning_letter`/`evening_letter` 도구로 데이터 수집 → 평문 마크다운 레터 작성 → 원본 채널로 전달.

미관 개선 시도의 막다른 길:
- 컬러 이모지(🌅☁️💱) → "vibe-coded" 느낌, 모노크롬 원함.
- 모노크롬 추상 글자(◆, PR #2921) → 모노지만 안 예쁨/의미 약함.
- **텍스트는 "예쁜 + 모노크롬 + 의미 있는 아이콘"을 담을 수 없다** — 이모지는 OS가 컬러로 렌더하고, 평문엔 모노크롬 강제 수단이 없으며, 진짜 벡터 아이콘은 *글자가 아니라 앱이 그리는 컴포넌트*다.

→ **그 컴포넌트가 deneb-ui다.** 카드로 가면 아이콘 미관 목표를 달성하면서 데이터 구조·상호작용까지 얻는다.

## 2. 왜 카드인가 (대안 B 대비)

대안 B = "마크다운 렌더러에 `InlineTextContent`를 추가해 이모지→인라인 벡터 아이콘 치환"(전 채팅에 보편 적용).

| | 카드 (이 RFC) | B (이모지→인라인 아이콘) |
|---|---|---|
| 바꾸는 것 | **SKILL.md만** (렌더러 기존) | 마크다운 렌더러 **신규 앱코드**(`InlineTextContent` baseline 정렬) |
| 실기기 검증 못 하는 내 코드 | 거의 없음 (PNG로 확인) | 있음 (baseline 시각) |
| 예쁜 모노크롬 아이콘 | `icon` 노드로 달성 | 이게 목표 |
| 추가 획득 | stat·countdown·list·탭액션 = 실유틸리티 | 없음 (미관만) |

**더 높은 가치 + 더 낮은 앱리스크.** B는 "아이콘을 전 채팅에 보편 적용"하고 싶을 때 별도로 추진 가능(상호 배타 아님).

## 3. deneb-ui 능력 (확인된 근거)

- **노드 스키마** `ui/dynamicui/DenebUiNode.kt` — 레터에 필요한 노드 전부 존재:
  `card`/`column`/`row`/`divider`, `text`(style: headline/title/body/caption), `markdown`(full md), `stat`{value,label,description}, `list`{items:[node]}, `countdown`{seconds,label,action}, `icon`{name,size,color}, `accordion`{title,children,expanded}, `badge`{value,color}, `chip_group`, `button`{label,action,variant}, `table`, `chart`.
- **아이콘 레지스트리** `ui/dynamicui/DenebUiMedia.kt:resolveIcon` — 레터용 모노크롬 벡터 충분:
  날씨 `sunny`/`cloud`/`water_drop`/`light_mode`/`dark_mode`/`bolt`, 환율·돈 `payments`/`savings`/`trending_up`/`trending_down`/`swap_horiz`, 구리 `bar_chart`/`inventory`, 일정 `calendar`(=date_range/schedule)/`clock`, 메일 `mail`(=email), 마감 `alarm`/`timer`/`flag`/`task`/`warning`.
- **탭 액션(P2)** `ui/dynamicui/UiAction.kt` — `CallbackAction`(이벤트+폼입력을 게이트웨이로, deneb-ui prompt contract), `OpenUrlAction`, `CopyToClipboardAction`, `ToggleAction`.
- **인라인 렌더** `ui/chat/composables/BotMessage.kt:187` — 채팅 메시지 버블 *안에서* deneb-ui fence를 카드로 렌더. **별개 화면 아님, 피드 inline.**
- **서버 검증** `pipeline/chat/denebui/denebui.go:Validate` — fence 본문을 스키마 검증(JSON/NDJSON). 잘못된 카드를 잡을 수 있음.
- **기존 카드 선례** `runtime/server/workfeed_extract.go:163` "morning-letter cards keep their own headings" + `proactive_relay.go:283` `CollapsedReportFence` — 인프라가 이미 이 방향.

## 4. 설계 — 모닝레터 카드

메시지 = **① 평문 한 줄 헤더/floor + ② deneb-ui fence(카드)**. (floor 근거는 §6.)

````
좋은 아침이에요 — {date}. {핵심 한 줄: 날씨/제일 임박한 마감}

```deneb-ui
{
  "type": "column",
  "children": [
    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "sunny", "size": 18 },
        { "type": "text", "value": "날씨 · 광주", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "stat", "value": "18°", "label": "기온", "description": "체감 16°" },
        { "type": "stat", "value": "24°/14°", "label": "최고/최저" },
        { "type": "stat", "value": "30%", "label": "강수" } ] },
      { "type": "text", "value": "오후 소나기 가능 — 우산 챙기세요", "style": "body" } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "payments", "size": 18 },
        { "type": "text", "value": "환율 · 구리", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "stat", "value": "1,386", "label": "USD/KRW" },
        { "type": "stat", "value": "1,498", "label": "EUR/KRW" },
        { "type": "stat", "value": "$9,540", "label": "구리/t" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "calendar", "size": 18 },
        { "type": "text", "value": "오늘 일정", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "09:00 — 팀 스탠드업" },
        { "type": "text", "value": "14:00 — 거래처 미팅" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "mail", "size": 18 },
        { "type": "text", "value": "전일 메일", "style": "caption" } ] },
      { "type": "list", "items": [
        { "type": "text", "value": "김부장 — 견적서 회신 요청" },
        { "type": "text", "value": "세무서 — 부가세 신고 안내" } ] } ] },

    { "type": "card", "children": [
      { "type": "row", "children": [
        { "type": "icon", "name": "alarm", "size": 18 },
        { "type": "text", "value": "임박 마감", "style": "caption" } ] },
      { "type": "row", "children": [
        { "type": "text", "value": "부가세 신고", "style": "body" },
        { "type": "badge", "value": "D-2" } ] } ] }
  ]
}
```
````

설계 노트:
- **섹션 = `card`**, 헤더 = `row[icon + text(caption)]` (디자인 규칙의 "그룹 리스트 행 leading 아이콘" idiom).
- **수치 = `stat`** 가로 strip (날씨·환율·구리). 모바일 380dp에서 3개가 빡빡하면 2개로 — PNG로 확인.
- **마감 = `badge "D-N"`**. `countdown`(초 단위 라이브)은 *당일* 마감에만 선택적; 다일(多日)은 badge가 깔끔.
- **floor 한 줄**(fence 밖)은 텍스트채널·파싱실패 대비 최소 정보(§6, §10).

## 5. 설계 — 이브닝레터 카드

모닝의 저녁 짝. 시장 섹션(날씨·환율·구리) 제거, **내일 일정·챙길 메일·임박 마감 3개 카드** + 회고 어조. 구조는 §4와 동일, floor 한 줄은 "오늘 마무리 + 내일 핵심".

## 6. 크로스채널 — 핵심 선결 결정

서버측 **deneb-ui→평문 flatten은 없다**(`denebui` 패키지에 `ToText`류 부재 확인). 즉 텍스트 채널(텔레그램 등)로 카드 fence가 가면 자동 평문화되지 않는다. `proactive_relay.go`는 이미 **deliverBody(평문) + transcriptBody(fenced)** 이중 표현을 쓰고, `autoreply/reply/normalize_reply.go:72`는 fenced 블록을 strip한다.

→ 두 설계:

**Design A — 네이티브 우선 (권장).** 데이터는 카드에, fence 밖 평문은 **compact floor**(날짜 + 1줄 헤드라인). 네이티브=리치 카드, 텍스트채널=floor 한 줄(데이터 축약). 레터를 앱에서 읽는다면 최적. 단순·PNG검증·파싱실패 안전(floor 잔존).

**Design B — 채널 패리티.** 현행 평문 레터 전체를 유지(모든 채널) + 카드 fence를 덧붙임. 텍스트채널=full 평문(strip), 네이티브=평문+카드. **네이티브 중복** 발생 → 카드 있을 때 평문 데이터 숨기는 작은 앱 수정 필요.

**권장: Design A.** 이 앱은 단일 사용자·네이티브가 주 표면이고(최근 키보드/스크롤/레터 미관 등 네이티브 폴리시 작업 흐름), floor 한 줄이 파싱실패·텍스트채널 양쪽의 안전망이 된다.
**선결 확인 1건:** 크론 레터가 실제로 전달되는 기본 채널이 네이티브 앱인가(텔레그램 평문이 주 소비처가 아닌가). 네이티브 주면 A 확정.

## 7. 에이전트가 카드를 emit하는 법

deneb-ui 카탈로그는 **클라이언트(`data/ChatSystemPromptBuilder.kt`)가 시스템 프롬프트에 주입** → 크론(서버) 경로엔 카탈로그가 없을 수 있다. 그래서:

- **SKILL.md 템플릿에 카드 JSON 스켈레톤을 literal로 박는다.** 에이전트는 카탈로그를 추론하지 않고 **스켈레톤을 복사 + 데이터로 치환**한다(빈 섹션은 생략 규칙 명시).
- `morning_letter`/`evening_letter` 도구의 JSON 데이터 → 스켈레톤 슬롯 매핑을 SKILL.md "조율 규칙"에 1:1로 적는다.
- 출력 규칙: fence 밖 floor 1줄 → `\`\`\`deneb-ui` 블록 1개. 그 외 군더더기 금지(기존 "전달 규칙" 유지).

## 8. 디자인 시스템 정합

`.claude/rules/native-design-system.md` 준수:
- **모노크롬 AMOLED 베이스 + 2액센트.** 쿨 `primary`=상호작용/CTA(P2 버튼), 웜 애프리콧 `denebInsight()`=AI/비서 페르소나. → **레터의 따뜻한 어조 = 웜 애프리콧 액센트**에 매핑(인사/회고 한마디를 insight tint로). 색은 작은 마크에만.
- **아이콘은 기능 아이콘만**(섹션 헤더 leading) — 장식 금지. 콘텐츠 제목(메일 제목 등)엔 아이콘 안 붙임.
- 타이포는 렌더러가 MaterialTheme 기반 → 다크모드 자동. (단 렌더러의 `DenebType` 정합도는 별도 이슈 — P1은 "깔끔한 Material"로 충분, PNG로 판단.)

## 9. 시각 검증 (이 RFC의 핵심 de-risk)

`compileKotlinDesktop` 통과 ≠ 보기 좋음. 디자인 규칙대로 PNG 하네스 사용:
1. `desktopMain/.../RenderPreview.kt`에 모닝/이브닝 **샘플 레터 JSON을 `DenebUiRenderer`로 렌더하는 프리뷰 추가**(이미 같은 파일이 DenebUiRenderer를 호출 중 — line 1200/1212).
2. `cd client-android/app && ANDROID_HOME=~/android-sdk ./gradlew :composeApp:renderPreviews` → `/tmp/deneb-render/*.png`.
3. **Read로 PNG 확인** — 다크/라이트, 긴 한국어 제목, 조밀 데이터(마감 5건) legibility.
4. 보기 나쁘면 JSON 스켈레톤 조정 후 반복. **구현 전 이 루프로 카드 모양을 확정**한다.

## 10. 검증 · 실패 fallback

- **빌드/형식 게이트**: gofmt/gofumpt -l, `go build ./...`, `go vet`, `go test`(게이트웨이). 스킬 변경이 주이므로 Go 변경은 도구 매핑 정도.
- **카드 JSON 검증**: `denebui.Validate`로 스켈레톤 샘플을 테스트(유효성 회귀 테스트 추가 가능).
- **파싱 실패 fallback**: 카드 JSON이 깨져도 **floor 한 줄(fence 밖)이 잔존** → 사용자는 최소 정보 확보. (Design A의 floor가 이 안전망 역할.)

## 11. 범위

**P1 (표시 카드 — 이 RFC의 구현 목표).** stat·list·badge·icon·prose floor. 탭 액션 없음. → 아이콘 미관 목표 달성 + 구조화. 안전·PNG검증.

**P2 (상호작용 — 후속).** `button`+`CallbackAction`으로 탭 처리: 메일 "답장", 일정 "열기", 마감 "처리". `UiAction` 콜백이 게이트웨이로 이벤트를 보내 에이전트가 후속 처리. **버튼 라벨이 실제 가능한 동작만**(디자인 프롬프트 테스트가 "콜백이 못 하는 동작 라벨" 경고). P1 안정화 후 별도 RFC/PR.

## 12. PR #2921(◆)과의 관계

#2921은 레터 이모지를 ◆/☼/☾로 치환 중. 카드로 가면 **◆ 마커는 무의미**(카드가 아이콘 사용). 단 #2921의 *평문 정리*는 Design A의 floor/Design B의 평문 fallback에 여전히 유효.
→ **권장:** 이 RFC 채택 시 #2921은 "floor 평문 다듬기"로 축소하거나 카드 PR에 흡수. 별도 ◆ 장식은 폐기.

## 13. 리스크 · 오픈 질문

1. **(선결) 크론 레터의 기본 전달 채널** = 네이티브? → Design A/B 결정(§6).
2. 에이전트가 매일 **유효 JSON**을 안정적으로 — SKILL.md literal 스켈레톤 + Validate + floor fallback로 완화. 한국어 escaping 주의.
3. **렌더러 타이포 정합도** — deneb-ui 카드가 hand-built Deneb 화면만큼 정제됐나? PNG로 판정. 부족하면 렌더러 보정은 P1 범위 밖(scope creep).
4. **stat 가로 밀도** — 380dp에서 stat 3개 wrap 여부, PNG 확인.
5. `badge.color`/`countdown` 등 일부 필드의 렌더 동작(허용 color 토큰 등)은 구현 시 RenderXxx로 확인.

## 14. 구현 체크리스트 (P1)

- [ ] §6 선결 질문 확정(전달 채널) → A/B 픽스
- [ ] `RenderPreview.kt`에 모닝/이브닝 샘플 JSON 프리뷰 추가 → PNG 렌더 → 모양 확정
- [ ] `skills/productivity/morning-letter/SKILL.md` 템플릿을 카드 스켈레톤+조율규칙으로 교체
- [ ] `skills/productivity/evening-letter/SKILL.md` 동일
- [ ] `denebui.Validate` 기반 스켈레톤 유효성 테스트
- [ ] 도구 다이어리 요약 등 부수 텍스트 정리(§12)
- [ ] PR — Verification(렌더 PNG 첨부 + go 게이트) 후 사용자 머지
