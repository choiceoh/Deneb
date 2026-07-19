# deneb-ui HTML Grammar (v2 wire format)

> **단일 진실원.** ```` ```deneb-ui ```` 펜스 블록의 본문 포맷. 2026-07 JSON→라벨 HTML 전환
> ("AST as HTML" 패턴 — LLM의 사전학습 HTML 유창성을 활용해 커스텀 JSON 스키마 수리
> 계층을 제거). 구현 3곳이 이 스펙을 따른다:
>
> - Gateway: `gateway-go/internal/pipeline/chat/denebui/html.go` (파서+검증)
> - Native:  `client-android/.../ui/dynamicui/DenebUiHtml.kt` (파서)
> - Andromeda: `andromeda/src/markdown/denebUiHtml.ts` (파서)
>
> 셋은 같은 미니 토크나이저 규칙을 포팅한 것이며 (브라우저 DOMParser는 table
> foster-parenting 이 커스텀 태그를 재배열하므로 사용 금지), 공유 테스트 벡터로
> 발산을 막는다. 본문 첫 비공백 문자가 `<` 면 HTML, `{`/`[` 면 legacy JSON
> (strict, 표시 전용 — 신규 저작 금지).

## 토크나이저 (XML-lite + 관용)

- 태그/속성 이름 대소문자 무시. 속성값 `"…"`, `'…'`, 따옴표 없음 모두 허용.
- 불리언 속성은 값 생략 가능 (`required` == `required="true"`).
- 셀프클로징 `<tag/>` 허용. 주석 `<!-- … -->` 무시. `<!DOCTYPE …>` 무시.
- 엔티티: `&lt; &gt; &amp; &quot; &#39; &nbsp;` + 수치(`&#NN;` `&#xHH;`).
- **Void 태그** (닫는 태그 없음): `hr img input icon slider progress avatar point br spacer`.
- **형제 자동 닫힘**: `li→li`, `option→option`, `td/th→td/th`, `tr→tr(+열린 td/th)`,
  `tab→tab`, `chip→chip`, `point→point`. (모델이 HTML5 습관대로 안 닫는 것 수용)
- **EOF 자동 닫힘**: 열린 태그 전부 — 스트리밍 절단 내성 (잘린 JSON과 달리 graceful).
- **Raw-text 태그**: `markdown`, `code` — 대응 닫는 태그(`</markdown`, `</code`)까지
  원문 그대로 캡처 후 엔티티만 디코드. 내부 백틱 펜스는 `&#96;` 로 이스케이프
  (서버 조립기는 항상 이스케이프; 모델 계약은 코드에 `<code>` 노드 사용 지시).
  단, `<code>`의 부모가 `text`/인라인 태그면 블록 노드 대신 `` `…` `` 백틱 런으로
  부모 텍스트 흐름에 병합 (인라인 코드 습관 수용).
- 컨테이너(column/row/card/box/accordion/li/tab) 직속의 비공백 텍스트 런은
  암시적 `text`(body) 노드로 수용. 루트에 형제가 여럿이면 암시적 `column` 래핑.

## 관용/자동 보정 (v2.1 — 모델 실수를 콘텐츠 보존으로 흡수)

- **펜스 오프너 관용**: ```` ```deneb-ui ```` 오프너가 프로즈 문장 꼬리에 붙어도
  (`…가져올게요.```deneb-ui`) 펜스로 인식 — 앞부분은 프로즈로 유지. 오프너 직후에
  본문 첫 태그가 이어진 형태(`…```deneb-ui<column>`)도 수용 — 잔여부가 `<`로
  시작할 때만이며, 문장 중간 언급(`​```deneb-ui 펜스는…`)은 프로즈로 남는다.
  3구현 공통: Go `ExtractFences`/`HasFence` · TS `splitDenebUi` · Kotlin
  `BlockScanner.splitGluedDenebUiFences`.
- **펜스 클로저 관용 (HTML 본문 한정)**: HTML 본문은 계약상 백틱을 `&#96;`로
  이스케이프하므로 본문 라인에 나타난 생 백틱 3연속 런은 클로저일 수밖에 없다 —
  ````…</column>``` 뒤 프로즈```` 형태를 분리해 앞은 본문 마지막 라인, 뒤는
  프로즈로. legacy JSON 본문은 문자열 값에 백틱 런이 올 수 있어 **단독 라인
  클로저만** 유지.
- **인라인 런 사이 공백 보존**: 인라인 태그 사이의 공백-only 텍스트 런은 한 칸
  공백으로 보존 (`<b>A</b> <b>B</b>` → `**A** **B**`) — 드롭하면 마커가 붙어
  (`**A****B**`) 인라인 마크다운 렌더가 깨진다. 블록 자식 앞뒤의 공백은 레이아웃
  공백으로 계속 무시.
- **알 수 없는 태그: unwrap** — 노드는 안 만들되 자식(암시 텍스트 포함)을 부모로
  승격. 서브트리 소실 금지. 검증기는 Issue 유지 (드리프트 텔레메트리).
  알 수 없는 속성: 무시 (기존과 동일).
- **제네릭 래퍼** (`div section article header footer main aside figure center nav
  thead tbody tfoot`): unwrap과 동일하되 Issue 없음 — 수용된 HTML 유창성.
  unwrap은 자식 노드와 **structural 중간물(tr/td 등)도 함께 승격** —
  `<thead>/<tbody>` 안의 행이 표에서 사라지지 않는다.
- **인라인 서식 태그** (`b strong`→`**` · `i em`→`*` · `u s del strike mark small
  span sub sup`→맨몸 텍스트 · `a href`→`[라벨](url)`): 노드 대신 마크다운 마킹된
  텍스트 런으로 부모 텍스트 흐름에 병합 — 렌더러의 인라인 토크나이저가 그린다.
  단 plain-value 슬롯(badge/button 라벨 등)에는 마커 없이 맨몸 텍스트.
- **텍스트 런 병합 + 마크다운 블록 승격**: 컨테이너/루트의 암시 텍스트 런은
  버퍼링해 하나로 병합. 병합 결과가 마크다운 블록 구조(파이프 행 ≥2 · `#` 헤딩 ·
  불릿/번호 리스트 ≥2 · ``` 펜스)면 `text` 대신 `markdown` 노드로 — "카드 안
  마크다운 표" 습관이 전체 마크다운 렌더러(표 포함)로 살아난다. `<text>` 요소
  자체도 값이 블록 구조면 markdown 노드로 승격.
- **블록 별칭**: `p`→text(body) · `h1`→text(headline) · `h2 h3 title`→text(title) ·
  `h4-h6`→text(bold) · `label`→text(caption) · `spacer`→divider ·
  `kv label="키"`→text("키 — 값"). 블록 자식이 섞이면 column 래핑(텍스트 먼저).
  (title/label/spacer/kv 는 2026-07-18 리젝트 텔레메트리에서 실측된 모델 습관을
  정식 승격한 것.)
- **수치 관용**: 정확 파스 실패 시 첫 숫자 런 추출 — 천단위 콤마 허용, 단위/기호
  무시 (`"1,200톤"`→1200, `"68%"`→68, `"16px"`→16).
- **progress 백분율 정규화**: value>1 이면 /100 후 [0,1] 클램프 (`"68"`/`"68%"`→0.68).
- **enum 정규화**: badge color `red`→error `green`→success `yellow/amber/orange`→warning
  `blue`→primary `gray/grey/neutral`→secondary · alert severity `warn/caution`→warning
  `danger/critical/fatal`→error `ok/done`→success `note/notice/information`→info ·
  chart type `bars/column(s)`→bar `lines/area/trend`→line · text style
  `heading/header`→headline `subtitle/subheading`→title.

## 태그 → 노드 매핑

| 태그 (별칭) | 노드 | 속성 | 콘텐츠 |
|---|---|---|---|
| `column` (`col`) | column | id | 자식 노드 |
| `row` | row | id | 자식 노드 |
| `card` | card | id | 자식 노드 |
| `box` | box | id, `align`→contentAlignment | 자식 노드 |
| `hr` (`divider`) | divider | id | void |
| `text` | text | id, `style`(headline·title·body·caption), `bold`, `italic`, `color` | 내부 텍스트 = value |
| `markdown` | markdown | id | raw-text = value (마크다운 전문) |
| `img` (`image`) | image | id, `src`(=`url`), `alt`, `height`, `aspect-ratio` | void |
| `icon` | icon | id, `name`, `size`, `color` | void |
| `code` | code | id, `language`(=`lang`) | raw-text = code |
| `blockquote` (`quote`) | quote | id, `source` | 내부 텍스트 = text |
| `badge` | badge | id, `color` | 내부 텍스트 = value |
| `stat` | stat | id, `value`, `label`, `description` | 내부 텍스트 = value 폴백 |
| `avatar` | avatar | id, `name`, `src`(=imageUrl), `size` | void |
| `progress` | progress | id, `value`(0..1), `label` | void |
| `alert` | alert | id, `severity`(info·success·warning·error), `title` | 내부 텍스트 = message |
| `countdown` | countdown | id, `seconds`, `label`, 액션 속성 | 내부 텍스트 = label 폴백 |
| `chart` | chart | id, `type`(bar·line)→chartType, `label` | `<point label value/>` 자식 |
| `table` | table | id | `<tr>` + `<th>`/`<td>`: th 행 → headers, td 행 → rows (셀은 텍스트) |
| `ul` / `ol` (`list` + `ordered`) | list | id | `<li>` — li 의 자식 노드(텍스트는 암시 text) |
| `tabs` | tabs | id, `selected-index` | `<tab label="…">` 자식 노드 |
| `accordion` | accordion | id, `title`, `expanded` | 자식 노드 |
| `button` | button | id, `variant`(filled·outlined·text·tonal), `enabled`, 액션 속성 | 내부 텍스트 = label |
| `input` | text/date/time_input | **id 필수**, `type`(text*·date·time·checkbox), `label`, `placeholder`, `value`, `multiline`, `keyboard`, `required` | void. `type=checkbox` → checkbox |
| `textarea` | text_input(multiline) | id 필수, 동일 | 내부 텍스트 = value |
| `checkbox` | checkbox | id 필수, `label`, `checked` | 내부 텍스트 = label 폴백 |
| `switch` | switch | id 필수, `label`, `checked` | 내부 텍스트 = label 폴백 |
| `select` | select | id 필수, `label`, `placeholder`, `required` | `<option [selected]>값</option>` — selected 속성 → selected |
| `radio-group` (`radiogroup`) | radio_group | id 필수, `label`, `required` | `<option [selected]>` 동일 |
| `slider` | slider | id 필수, `label`, `value`, `min`, `max`, `step` | void |
| `chips` (`chip-group`) | chip_group | id 필수, `selection`(single*·multi·none), `required` | `<chip value="…">라벨</chip>` |

`*` = 기본값. 수치/불리언 속성은 관용 파싱(실패 시 무시→기본값).

## 액션 속성 (button · countdown)

**롱프레스 액션 (모든 노드)**: `longpress="이벤트명"`(+ `data-*="…"`)는 노드에
press-hold 콜백을 단다 — 탭 `event=`와 별개(한 노드가 둘 다 가질 수 있음).
렌더러는 `row`/`text`에 바인딩(네이티브 combinedClickable onLongClick·데스크톱
~500ms 프레스홀드). 인터랙티브 카드에서만 발사. 예: 모닝 카드 마감 `<row>`를
길게 눌러 완료 처리(`longpress="deadline_done" data-path="위키경로"`).

| 속성 | UiAction | 비고 |
|---|---|---|
| `event="이벤트명"` | callback | `data-*="…"` → data 맵, `collect="id1,id2"` → collectFrom |
| `href="URL"` | open_url | 앵커 유창성 |
| `toggle="targetId"` | toggle | |
| `copy="텍스트"` | copy_to_clipboard | |

우선순위(복수 지정 시): `event` > `href` > `toggle` > `copy`.

## 검증 규칙 (게이트웨이 denebui.Validate)

- 인터랙티브 노드(input/textarea/checkbox/switch/select/radio-group/slider/chips)는
  비어있지 않은 `id` 필수 — 기존 JSON 검증과 동일.
- enum 속성 위반(정규화 이후에도 남는 값), 알 수 없는 태그(unwrap 후에도 Issue)와
  알 수 없는 액션은 Issue 로 보고.
- legacy JSON 본문은 기존 경로로 계속 검증(표시 전용 하위호환).

## 공유 테스트 벡터

세 구현의 테스트가 아래 시나리오를 공통으로 커버한다 (각 저장소의 *_test 파일):
레터 카드 3장 / select+option(미닫음) / 미닫은 li / 루트 형제 다수(암시 column) /
truncation(EOF 자동 닫힘) / 엔티티+`&#96;` 코드펜스 / table th+td / 액션 4종 /
알 수 없는 태그 unwrap(+게이트웨이 Issue) / id 누락 인터랙티브 / raw-text markdown
내 `<` 문자 / div 래퍼 unwrap / 인라인 `<b>` 병합(`**`) / `<a>`+인라인 `<code>`
병합 / badge 안 인라인 마커 억제 / 카드 안 마크다운 표→markdown 노드 /
`<text>` 불릿 블록 승격 / h2·p 별칭 / progress `68%`→0.68 / point `1,200톤`→1200 /
badge `red`→error / alert `warn`→warning / chart `column`→bar /
프로즈 꼬리에 붙은 펜스 오프너(+문장 중간 언급은 프로즈 유지) /
인접 인라인 런 공백 보존(`**A** **B**`) / `<ul>` 비순서·`<ol>`/`ordered` 속성만 순서 /
`<thead>/<tbody>` unwrap 시 행 보존 / 본문 꼬리에 붙은 클로저(HTML 한정, 뒤 프로즈 분리) /
원라이너 펜스(````프로즈.```deneb-ui<text>…</text>``` 끝````) / legacy JSON은 단독 라인 클로저 유지 /
발명 태그 별칭(title→title·label→caption·kv→"키 — 값"·spacer→divider).
