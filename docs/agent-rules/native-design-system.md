---
description: "client-android 디자인 시스템 경계 — 컨트롤은 Material, 외형은 Deneb 타이포"
globs: ["client-android/app/composeApp/src/**/*.kt"]
---

# Native Client Design System (client-android)

> **디자인 원리는 여기 없다 — [ADR 0007](../adr/0007-design-north-star.md)이 소유한다** (북극성 "메뉴가 아니라 결과" + 제1원리 6개). 이 문서는 그 원리의 **구현 규칙**이다: 어느 컴포넌트를 쓰고 어떻게 검증하는가. **원리와 규칙이 충돌하면 원리가 이긴다.** 규칙을 다 지켜도 원리를 어길 수 있다는 것이 ADR 0007을 쓴 이유다 (설정 트리에서 실제로 11곳).

> **컨트롤은 머티리얼, 외형은 Deneb.** 두 시스템은 경쟁이 아니라 레이어가 다르다. Material을 뜯어내지 말고, 그 위에 Deneb 타이포 스킨을 입힌다.

## 한 줄 원칙

- **Material** = 상호작용 · 상태 · 접근성 · 테마의 **기반(substrate)**.
- **Deneb 타이포**(`ui/DenebType.kt`, `ui/DenebDesign.kt`) = 타입 · 화면 프레임 · 행 구조 · 하airline의 **표현(presentation) 스킨**.

이건 이미 암묵적으로 사실이다 — `DenebType`는 `MaterialTheme.colorScheme`를 읽고, `DenebRow`는 `Surface`를 쓰며, 접근성 위해 글리프 토글을 진짜 Material `Checkbox`로 바꿨다(#1907). 이 문서는 그 경계를 명문화한다.

## 누가 무엇을 소유하나

| Deneb 타이포 스킨 (정체성·구조) | Material (기능·상태·a11y) |
|---|---|
| 모든 텍스트 → `DenebType.*` (`viewTitle`/`subject`/`rowTitle`/`rowTitleStrong`/`rowSubtitle`/`snippet`/`meta`/`sectionLabel`/`body`/`button`/`hint`) | 버튼: `Button`/`FilledTonalButton`/`OutlinedButton`/`TextButton` |
| 화면 프레임 → `DenebScreenScaffold(title, onBack, tabBar?)` (flat AMOLED, `←`, 제목) | 폼: `Switch`·`Checkbox`·`SegmentedButton`·`OutlinedTextField`·`Slider` |
| 리스트 행 → `DenebRow { … }` (행 아래 하airline, 노카드, 여백, 전체 탭) | 오버레이: `AlertDialog`·`ModalBottomSheet`·`Snackbar`·`ModalNavigationDrawer` |
| 섹션 헤더 → `DenebSectionLabel("…")` (트랙트 캡스) | 비동기: `PullToRefreshBox`·`CircularProgressIndicator` |
| 구분선 → `denebHairline()` · 힌트색 → `denebHint()` | 시맨틱: `selectable`/`toggleable`/`Role`, `contentDescription` |
| — | 색 토큰: `MaterialTheme.colorScheme` **단일 소스** (다크모드·브랜드) |

타이틀 매핑: 화면 페이지 제목 = 섹션 명사 → `DenebScreenScaffold` title(`viewTitle`). 콘텐츠 항목 제목(메일 제목·일정명·위키 타이틀) = `DenebType.subject`. 행 1차/2차/시각 = `rowTitle(Strong)`/`rowSubtitle`/`meta`.

## 겹칠 때 타이브레이커

- **헤더/제목**이 둘 다 있으면 → Deneb (`DenebScreenScaffold`). Material 향 `DenebViewHeader`/`DenebSurface` 는 제거 완료 — `deneb/DenebUi.kt` 에는 공용 상태 헬퍼(`DenebLoading`/`DenebError`/`DenebEmpty`)·`rememberDiscardGuard`(뒤로가기 폐기 가드)·`humanBytes` 만 남아 있다.
- **컨트롤**이 둘 다 가능하면 → Material. (예: 직접 그린 "☑" 글리프 대신 `Checkbox`)

## 도그마 금지 — "실용적 Material"을 지키는 경계 케이스

> **Doctrine (2026-06-14, 2026-08-30 개정):** 구조·상호작용·접근성은 **Material/Apple이 검증한 패턴을 최대한 채택**, 색·무게·장식·여백은 **Deneb 절제**. 충돌하면 — 동작은 Material/Apple, 외형은 Deneb. 즉 "컨트롤=Material, 외형=Deneb"의 더 공격적 버전.
>
> ★**북극성 정정(2026-08-30, ADR 0007)**: 원문은 제품 북극성을 "토스식 슈퍼앱"으로 적고 `[[project_superapp_vision]]`을 인용했으나 **그 위키 페이지는 890페이지 중에 없다** — 존재하지 않는 근거였다. 방향도 제품과 반대다(슈퍼앱=다서비스·대중 온보딩·서비스 메뉴 vs Deneb=단일 사용자 비서실장 단일 페르소나, ADR 0001). 북극성은 **"메뉴가 아니라 결과"**로 대체됐고, 토스는 **장인 참조**(밀도·한국어 타이포·절제·모션 품질)로만 남는다.

idiom 문서엔 "no cards / no icons"라 써 있지만 **기능을 돕는 곳은 남긴다**:

- **카드 ★재정의(2026-06, 디자인 리프레시)**: 설정류 리스트는 **그룹 인셋 카드**(`DenebGroup`+`DenebListRow`: 둥근 컨테이너+은은한 모노 wash+인셋 하airline+leading 아이콘/제목/부제/chevron, iOS·토스식)가 기본 idiom. 콘텐츠 리스트(메일·검색 등)는 bare `DenebRow`(단일 하airline) 유지. 즉 옛 평면 에디토리얼 "no cards"는 폐기되고 그룹 카드로 진화. 독립 콜아웃(AI 분석)은 `denebInsightContainer()` tint 박스.
- **칩**: 첨부·관련항목 `AssistChip`은 상호작용·접근성 있는 Material 유지(외형만 정돈). `DenebChip`이 중간 지점.
- **아이콘**: 기능 아이콘(보내기·중지·Meet·상태 점)은 유지, **장식** 아이콘만 배제. ★**내비게이션 아이콘 허용**(2026-06-14): 폰 하단 탭바·데스크톱 레일·세션 진입점은 **아이콘+라벨**(Material icons, `Outlined`=비활성/`Filled`=활성 Apple식, M3 `NavigationBar` substrate로 인셋·리플·`Role.Tab` a11y·햅틱). 단 **리스트 행·콘텐츠 제목(메일·세션·위키)은 계속 아이콘리스**, 컬러 탭 없음(모노크롬), 활성=ink+절제된 인디케이터. 한 줄 규칙: 아이콘은 **내비게이션 + 그룹 리스트 행**에. (디자인 리프레시로 `DenebListRow`가 행 leading 아이콘을 가짐 — 콘텐츠 *제목*엔 여전히 안 붙임.)
- **색 ★2액센트(2026-06, 디자인 리프레시)**: 모노크롬 AMOLED 베이스 + 절제된 2색. **쿨 `MaterialTheme.colorScheme.primary`(다크 0xFF7FA8D0)=상호작용·선택·CTA** (여태 ink로 억눌렀던 것을 비로소 사용), **웜 애프리콧 `denebInsight()`=AI 분석·인사이트** (쿨↔웜 보색; Deneb 분석↔비서 이중 페르소나의 색 매핑). 둘 다 작은 마크·소프트 fill(`denebInsightContainer()`)에만 — 화면 전체엔 안 칠함. 토큰 정의=`Theme.kt` 액센트 doctrine.

## 폴리시 기준 ★수준 강화 (2026-07)

세 차원이 "폴리시드"의 정의다 — 새 화면·화면 변경은 셋 다 통과해야 한다:

1. **모션** — 단일 소스 `ui/DenebMotion.kt`(토큰·프리셋·`denebPressable`·`denebBreathing`) + `ui/DenebNavMotion.kt`(화면 전환). 인라인 `tween(매직넘버)` 금지 — 새 의도면 토큰을 추가하고 이름을 붙인다. **화면 전환 문법: 바텀바 유지 = 측면 이동(빠른 페이드) / 바 숨김 = 드릴인(끝에서 슬라이드+부모 1/4 패럴랙스, pop은 역방향)** — 분류식은 `denebBottomBarRoutes` 하나라 크롬과 모션이 항상 일치한다. 리스트→상세 연속성은 `denebSharedBounds("surface-field-id")`(예: `"mail-subject-42"`) — 스코프 부재 시 no-op이므로 프리뷰·데스크톱 분할뷰에 안전, previewable body에 스코프 배선 불요. 새 destination은 `composable<T>` 대신 `denebComposable<T>`. **햅틱은 시맨틱 어휘로만**(`ui/components/Haptics.kt`): 탭=`tap`(VirtualKey 강도), 커밋=`confirm`/파괴=`reject`/토글=`toggle(on)`/PTR 트리거=`refresh`; `combinedClickable`(=`denebPressable` onLongClick)은 롱프레스 햅틱을 **자동 발사**하므로 수동 `longPress()` 중복 금지; 뒤로/취소/수동 이벤트(스트림·도착)는 무음.
2. **상태 완결성** — 콘텐츠 화면은 4상태(로딩·빈·오류(재시도)·콘텐츠)를 전부 설계한다. `DenebLoading`/`DenebError(onRetry)`/`DenebEmpty` + 리스트형 로딩은 `components/Skeleton.kt` 스켈레톤 우선(스피너는 액션 진행에만).
3. **타이포·여백 리듬** — 모든 텍스트 `DenebType.*`, 수직 여백 4dp 배수, 행 인셋 24dp 표준, 시각·수치 열은 tabular 숫자. 다크/라이트 + 긴 한국어 제목/조밀 데이터로 PNG 검수.

검증 배분: 정적(타이포·상태)은 `renderPreviews` PNG — 상태별 프리뷰 포함. 모션은 토큰 준수를 코드 리뷰로, 체감은 운영자 실기기로 판정한다(움직임은 PNG가 못 본다).

★**PNG는 이제 골든으로 고정된다** (`make render-golden-check`, Kotlin 레인 게이트). 그리기만 하고 아무도 비교하지 않던 117장은 2px 이동을 못 잡았다 — 2026-08-30에 나온 네이티브 결함 셋(한국어 행간 29→36px · 경과 접미가 상태보다 2px 위 · 두 줄일 때 접미 소실)이 전부 그 크기였고 코틀린 diff로는 하나도 안 보였다. 감도 실증: `snippet` lineHeight를 **1sp** 바꾸자 8개 픽스처가 정확한 bbox와 함께 걸렸다.

- 시각 변경이 의도된 것이면 `make render-golden-update` 후 골든을 함께 커밋한다 — **그 PNG diff가 곧 리뷰 자료**다(PR에서 before/after가 보인다).
- 실패 시 `/tmp/deneb-render-diff/*.png`에 `골든 | 현재 | 증폭 차이` 3단 스트립이 남는다.
- ★**골든은 OLED 블랙 단일**(ADR 0007). 테마가 하나뿐이라 픽스처당 PNG 한 장이고 파일명에 `_dark`/`_light` 접미가 없다. 2026-08-30 이전에는 `DarkColorScheme` 68장 + `LightColorScheme` 46장이었고 **정작 쓰는 OLED는 0장**이었다 — `isOledFlavor` 분기 5곳이 검수된 적이 없었다는 뜻이다. 새 픽스처는 `OledColorScheme`으로 렌더한다.
- 전제는 결정성이다: 같은 트리를 두 번 렌더하면 115/117이 바이트 동일. 픽스처 시계는 `PREVIEW_NOW_MS`로 얼렸고(상대 시각이 매 실행 흔들리던 4장 해결), `workfeed_*` 2장은 컴포넌트 내부가 실시각을 읽어 이름으로 제외한다.
- **새 화면·컴포넌트는 픽스처를 함께 낸다.** 픽스처가 없으면 게이트가 볼 수 없고, 실제로 챗 입력창과 대기 줄이 그렇게 검수 사각에 있었다.
- ★**실시각을 읽는 코드가 픽스처에 닿으면 골든은 자정에 깨진다.** 샘플 데이터만 `PREVIEW_NOW_MS`로 얼리는 것으로는 부족하다 — `lifecycleTime()`처럼 문자열을 만드는 쪽이 `Clock.System.now()`를 읽으면 한쪽만 얼어 diff가 매일 하루씩 벌어진다. 게이트 가동 다음 날 아침 `self_improvement_coding` 2장이 "12일 전"→"13일 전"으로 실제로 터졌다. **같은 날 두 번 렌더하는 결정성 증명으로는 안 잡힌다** — 이 부류는 단위 경계를 넘을 때만 드러난다. 상대 시각 헬퍼는 `nowMs` 인자를 받게 하고(기본값=실시각, 앱 동작 무변경) 골든에 그려지는 호출자만 얼린 값을 넘긴다.
- **기기·환경에 따라 달라지는 값을 골든에 담지 않는다.** 대표 사례가 "화면 배율" 카드다 — `defaultUiScale`이 데스크톱에서 `GDK_SCALE`/`GDK_DPI_SCALE`을 읽으므로 퍼센트 텍스트와 슬라이더 눈금이 개발자 환경마다 달라진다. 그런 값이 들어간 화면은 픽스처 높이를 조절해 프레임 밖으로 밀어내거나 픽스처를 만들지 않는다.

## 행동 불변 (이미 들인 작업 보존)

표현만 바꾸고 **#1904/#1907의 로딩/오류/빈/재시도·쓰기 실패 표시·햅틱·접근성은 그대로 이식**한다. `DenebRow`의 `onClick`에 `rememberHaptics().tap()` 유지, 상태 분기(`DenebLoading`/`DenebError(onRetry)`/`DenebEmpty`) 유지.

## 검증 — 컴파일이 못 잡는 시각 변경

`compileKotlinDesktop` 통과는 "보기에 맞나"를 보장하지 않는다. 헤드리스 렌더 하네스로 실제 PNG를 본다:

```bash
cd client-android/app && ANDROID_HOME=~/android-sdk ./gradlew :composeApp:renderPreviews
# → /tmp/deneb-render/*.png  (Read 도구로 직접 확인)
```

- 마이그레이션하는 화면은 **stateless body 컴포저블**(예: `CalendarEventContent(ev, onJoinMeet)`)로 분리해 `desktopMain/.../RenderPreview.kt`가 mock 데이터로 렌더 가능하게 한다. 화면 = stateful shell(client·load·상태) + previewable body(순수 표현).
- 다크/라이트 둘 다, 긴 한국어 제목/조밀 데이터로 legibility 확인.

## 점진 이행 (한 번에 안 함 — 다중 PR)

1. ✅ 기반: `DenebScreenScaffold` 확장(`tabBar` 슬롯 등) + 규칙 문서 + **파일럿 1화면**(일정 상세) → PNG 검증.
2. ✅ 상세 화면 팬아웃 (사람·메일상세·크론·위키·카테고리페이지·일기).
3. ✅ 상위 화면 (메일·사람·검색·일정·카테고리·설정·할일·크론). 메일 리스트의 데스크톱 분할 뷰 패널은 scaffold의 `fillWidth` 파라미터로 수용(380dp 패널에서 760dp 캡 대신 부모 채움); 메일상세 우측 패널만 타이틀 없는 bare 프레임 유지(리스트 패널이 곧 내비게이션).
4. ✅ 드로어 inline 값 → `DenebType` 참조(세션 드로어=`subject`/`rowTitle`/`meta`/`body`, 데스크톱 레일=당시 신설 `railItem` 20sp ExtraLight — 이후 데스크톱 레일 은퇴로 스타일 자체를 삭제) + divider 스윕(deneb 화면의 `outlineVariant` 구분선 25곳 → `denebHairline()`) + hint 스윕(수작업 `onSurfaceVariant.copy(alpha=…)` 6곳 → `denebHint()`; **일반 `onSurfaceVariant` 본문색 ~79곳은 의도적으로 유지** — 카드 내부 등 surface-상대 컨텍스트가 섞여 있어 일괄 치환은 라이브 시각 검증 없이는 위험) + `DenebUi.kt` 닥스트링 수정.

착수 전 `grep -r DenebScreenScaffold`로 진행 상황 확인.
