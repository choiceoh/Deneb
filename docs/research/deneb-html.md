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

- **스트리밍 중(펜스 미닫힘)엔 절대 부분 렌더하지 않는다** — 반쯤 로드된 문서의
  스크립트가 실행되면 안 된다. placeholder("웹 응답 생성/구성 중…") 유지.
- 높이는 페이지가 보고(scrollHeight)하고 프레임이 clamp(160–900px)해 따라간다 —
  트랜스크립트 안에서 이중 스크롤 없이 맞춰 자란다.
- stale 게이팅: 마지막 어시스턴트 턴이 아닌 행에선 `deneb.send` 회신을 무시한다
  (deneb-ui 콜백과 동일 규칙). 페이지 자체(로컬 스크립트)는 계속 동작.

## 검증

- 게이트웨이: `go test ./internal/pipeline/chat/denebui` (htmlanswer_test.go).
- Andromeda: `DenebHtml.test.tsx` (분할·CSP/브리지 주입·샌드박스 속성).
- 네이티브: `DenebHtmlParsingTest.kt` (라우팅·pending·플레인 프로젝션).
