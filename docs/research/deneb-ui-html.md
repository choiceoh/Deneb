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
- **Void 태그** (닫는 태그 없음): `hr img input icon slider progress avatar point br`.
- **형제 자동 닫힘**: `li→li`, `option→option`, `td/th→td/th`, `tr→tr(+열린 td/th)`,
  `tab→tab`, `chip→chip`, `point→point`. (모델이 HTML5 습관대로 안 닫는 것 수용)
- **EOF 자동 닫힘**: 열린 태그 전부 — 스트리밍 절단 내성 (잘린 JSON과 달리 graceful).
- **Raw-text 태그**: `markdown`, `code` — 대응 닫는 태그(`</markdown`, `</code`)까지
  원문 그대로 캡처 후 엔티티만 디코드. 내부 백틱 펜스는 `&#96;` 로 이스케이프
  (서버 조립기는 항상 이스케이프; 모델 계약은 코드에 `<code>` 노드 사용 지시).
- 알 수 없는 태그: 노드 스킵(서브트리 포함), 검증기에서는 Issue. 알 수 없는 속성: 무시.
- 컨테이너(column/row/card/box/accordion/li/tab) 직속의 비공백 텍스트 런은
  암시적 `text`(body) 노드로 수용. 루트에 형제가 여럿이면 암시적 `column` 래핑.

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
- enum 속성 위반, 알 수 없는 태그/액션은 Issue 로 보고.
- legacy JSON 본문은 기존 경로로 계속 검증(표시 전용 하위호환).

## 공유 테스트 벡터

세 구현의 테스트가 아래 시나리오를 공통으로 커버한다 (각 저장소의 *_test 파일):
레터 카드 3장 / select+option(미닫음) / 미닫은 li / 루트 형제 다수(암시 column) /
truncation(EOF 자동 닫힘) / 엔티티+`&#96;` 코드펜스 / table th+td / 액션 4종 /
알 수 없는 태그 / id 누락 인터랙티브 / raw-text markdown 내 `<` 문자.
