# deneb-html — 웹페이지형 HTML 응답 (wire format)

> **단일 진실원.** ```` ```deneb-html ```` 펜스 = 에이전트가 저작한 **자기완결 HTML
> 문서** 하나. 클라이언트는 이를 **채팅 트랜스크립트 안에서 바로**(별도 화면·페이지
> 전환 없음) 샌드박스 렌더한다. deneb-ui 카드(구조화 데이터, `deneb-ui-html.md`)와
> 상보적: 커스텀 디자인·스크립트 인터랙션이 답을 실질적으로 좋게 만들 때만 쓴다.

## 저작 계약 (시스템 프롬프트 소통 섹션이 정본 주입점)

- 본문 = 완전한 자기완결 HTML 문서. **인라인 CSS/JS만** — 외부 리소스 금지
  (클라이언트가 네트워크를 차단하므로 로드 자체가 실패한다).
- **백틱 문자 전면 금지** (JS 템플릿 리터럴 포함) — 생 ``` 런은 펜스를 조기
  종료시킨다. 문자열 연결로 대체.
- 페이지 → 챗 회신: `window.deneb.send('메시지')` — 클라이언트가 사용자
  메시지로 브리지한다 (예: 버튼 onclick).
- 응답당 deneb-html 최대 1개, deneb-ui 와 동시 사용 금지. UI 텍스트 한국어,
  ~380px 폭 라이트 표면 기준 디자인.

## 서버 정규화 (`denebui/htmlanswer.go`, NormalizeFinalReply 경유)

- 오프너는 **단독 라인 엄격 매치** (deneb-ui 와 달리 glued 관용 없음 — 문장 중
  언급은 프로즈 유지).
- 첫 정상 펜스만 유지(미닫힘이면 닫아줌). 추가 펜스 / `MaxHTMLAnswerBytes`(96KB)
  초과 / 마크업이 아닌 본문 → ```` ```html ```` 코드블록으로 열화 (구 클라이언트의
  기본 표시와 동일 = 하위호환 경로와 합류).
- 텔레메트리: 채택 `deneb-html answer authored`(Info) · 열화
  `deneb-html answer degraded to code block`(Warn).

## 클라이언트 렌더 (샌드박스 계약)

| 클라 | 표면 | 샌드박스 | 브리지 |
|---|---|---|---|
| Andromeda | `components/DenebHtml.tsx` — iframe `sandbox="allow-scripts"` srcdoc | 주입 CSP `default-src 'none'; style/script 'unsafe-inline'; img data:` (네트워크 전면 차단) + 고유 오리진 | postMessage `{__deneb:"prompt"}` · `{__deneb:"height"}` |
| 네이티브 Android | `ui/dynamicui/DenebHtmlView.android.kt` — WebView | `blockNetworkLoads`·file/content 접근 차단·내비게이션 전부 삼킴 | `DenebNative.send/height` JavascriptInterface |
| 네이티브 기타 타깃 | 자리표시자 ("이 기기에서는 미리보기 미지원") | — | — |

공통 규칙:

- **베이스 스타일시트 + 마이크로 디자인 시스템**: 두 클라 모두 문서 앞에 동일한
  CSS를 주입한다 — 변수 기반 베이스(한국어 시스템 폰트·14px/1.6 리듬·경계선 표·
  여백) + **테마 3종**(`<body class="theme-dark|theme-warm|theme-mono">`, 생략 =
  클린 라이트 — 응답마다 무드 다양화) + **유틸리티 클래스**(`card`·`grid`·
  `stat-value/stat-label`·`badge(+ok|warn|bad)`·`bar>i`(게이지, width:%)·`muted`·
  `accent`·`button.primary`). 페이지 자체 스타일이 뒤에 와서 자연히 이긴다.
  정의는 `DenebHtml.tsx` `BASE_CSS` ↔ `DenebHtmlView.android.kt` `BASE_CSS` —
  **둘을 항상 동일하게 유지**. 프롬프트 계약은 "베이스가 있으니 재지정하지 말
  것"을 고지해 문서를 짧게(=생성을 빠르게) 유도한다.

- **스트리밍 중(펜스 미닫힘)엔 절대 부분 렌더하지 않는다** — 반쯤 로드된 문서의
  스크립트가 실행되면 안 된다. placeholder("웹 응답 생성/구성 중…") 유지.
- **높이는 프레임이 내용에 맞춰 자란다** — 카드는 트랜스크립트 안에서 스스로
  스크롤하지 않는다(이중 스크롤 금지). 페이지가 자기 **본문 높이**를 보고하고
  `denebHtmlFrameHeight`(네이티브 `DenebHtmlFrame.kt` ↔ 안드로메다
  `denebHtmlSandbox.ts` 쌍둥이)가 프레임 높이를 정한다. **마지막 보고가 이긴다 —
  양방향으로.**
  - **★★측정은 `body` 박스 + 자기 마진이다. `documentElement.scrollHeight`를
    쓰지 말 것** — 그건 `max(내용, 뷰포트)`라, 프레임이 한 번 내용보다 커지면
    이후로는 **자기 높이만 되돌려받아** 과대추정을 영영 못 고친다. 측정과 함수는
    한 기계다: 한쪽만 바꾸면 아래 두 결함 중 하나가 돌아온다.
  - **줄어들 수 있어야 한다.** 첫 측정은 폰트가 자리잡기 전이나 최종 너비가
    정해지기 전에 떨어질 수 있고, 그때 값이 실제보다 크다. 커지기만 하는 규칙은
    그 값에 얼어붙어 **내용 아래에 화면 몇 개 분량의 여백**을 남긴다
    (2026-08-31 실제 배포·철회). 그래서 `load` 외에 `document.fonts.ready`와
    `ResizeObserver`에서도 다시 잰다.
  - 상한 8000px은 **폭주 방지턱이지 분량 예산이 아니다**. `min-height:100vh`처럼
    뷰포트에 비례하는 페이지만 — 보고값이 방금 준 프레임에서 파생되므로 — 여기
    닿는다. 실제 카드는 한참 아래다. 구 900px 상한은 6KB짜리 브리핑 카드의 9개
    섹션 중 3개만 보여주고 나머지를 **닿을 수 없게** 잘랐다. 상한이 낮으면 조용히
    내용을 잃는다.
- stale 게이팅: 마지막 어시스턴트 턴이 아닌 행에선 `deneb.send` 회신을 무시한다
  (deneb-ui 콜백과 동일 규칙). 페이지 자체(로컬 스크립트)는 계속 동작.

## 검증

- 게이트웨이: `go test ./internal/pipeline/chat/denebui` (htmlanswer_test.go).
- Andromeda: `DenebHtml.test.tsx` (분할·CSP/브리지 주입·샌드박스 속성·프레임 높이).
- 네이티브: `DenebHtmlParsingTest.kt` (라우팅·pending·플레인 프로젝션) +
  `DenebHtmlFrameTest.kt` (프레임 높이 — 잘림 없음·축소 가능·폭주 정지) +
  `DenebHtmlPreludeContractTest.kt` (양쪽 주입 스크립트가 뷰포트 클램프 높이를
  보고하지 않는지 — 축소 가능성의 전제).
