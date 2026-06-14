---
description: "client-android 디자인 시스템 경계 — 컨트롤은 Material, 외형은 Deneb 타이포"
globs: ["client-android/app/composeApp/src/**/*.kt"]
---

# Native Client Design System (client-android)

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

- **헤더/제목**이 둘 다 있으면 → Deneb (`DenebScreenScaffold`). Material 향 `DenebViewHeader`/`DenebSurface` 는 제거 완료 — `deneb/DenebUi.kt` 에는 공용 상태 헬퍼(`DenebLoading`/`DenebError`/`DenebEmpty`)와 `humanBytes` 만 남아 있다.
- **컨트롤**이 둘 다 가능하면 → Material. (예: 직접 그린 "☑" 글리프 대신 `Checkbox`)

## 도그마 금지 — "실용적 Material"을 지키는 경계 케이스

> **Doctrine (2026-06-14, 슈퍼앱 방향):** 구조·상호작용·접근성은 **Material/Apple이 검증한 패턴을 최대한 채택**, 색·무게·장식·여백은 **Deneb 절제**. 충돌하면 — 동작은 Material/Apple, 외형은 Deneb. 제품 북극성 = 토스식 슈퍼앱(킬러기능+최적화+간편UX+올인원, [[project_superapp_vision]]). 즉 "컨트롤=Material, 외형=Deneb"의 더 공격적 버전.

idiom 문서엔 "no cards / no icons"라 써 있지만 **기능을 돕는 곳은 남긴다**:

- **카드 ★재정의(2026-06, 디자인 리프레시)**: 설정류 리스트는 **그룹 인셋 카드**(`DenebGroup`+`DenebListRow`: 둥근 컨테이너+은은한 모노 wash+인셋 하airline+leading 아이콘/제목/부제/chevron, iOS·토스식)가 기본 idiom. 콘텐츠 리스트(메일·검색 등)는 bare `DenebRow`(단일 하airline) 유지. 즉 옛 평면 에디토리얼 "no cards"는 폐기되고 그룹 카드로 진화. 독립 콜아웃(AI 분석)은 `denebInsightContainer()` tint 박스.
- **칩**: 첨부·관련항목 `AssistChip`은 상호작용·접근성 있는 Material 유지(외형만 정돈). `DenebChip`이 중간 지점.
- **아이콘**: 기능 아이콘(보내기·중지·Meet·상태 점)은 유지, **장식** 아이콘만 배제. ★**내비게이션 아이콘 허용**(2026-06-14): 폰 하단 탭바·데스크톱 레일·세션 진입점은 **아이콘+라벨**(Material icons, `Outlined`=비활성/`Filled`=활성 Apple식, M3 `NavigationBar` substrate로 인셋·리플·`Role.Tab` a11y·햅틱). 단 **리스트 행·콘텐츠 제목(메일·세션·위키)은 계속 아이콘리스**, 컬러 탭 없음(모노크롬), 활성=ink+절제된 인디케이터. 한 줄 규칙: 아이콘은 **내비게이션 + 그룹 리스트 행**에. (디자인 리프레시로 `DenebListRow`가 행 leading 아이콘을 가짐 — 콘텐츠 *제목*엔 여전히 안 붙임.)
- **색 ★2액센트(2026-06, 디자인 리프레시)**: 모노크롬 AMOLED 베이스 + 절제된 2색. **쿨 `MaterialTheme.colorScheme.primary`(다크 0xFF7FA8D0)=상호작용·선택·CTA** (여태 ink로 억눌렀던 것을 비로소 사용), **웜 애프리콧 `denebInsight()`=AI 분석·인사이트** (쿨↔웜 보색; Deneb 분석↔비서 이중 페르소나의 색 매핑). 둘 다 작은 마크·소프트 fill(`denebInsightContainer()`)에만 — 화면 전체엔 안 칠함. 토큰 정의=`Theme.kt` 액센트 doctrine.

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
4. ✅ 드로어 inline 값 → `DenebType` 참조(세션 드로어=`subject`/`rowTitle`/`meta`/`body`, 데스크톱 레일=신설 `railItem` 20sp ExtraLight — Latin 전용이라 Hangul-hairline 예외) + divider 스윕(deneb 화면의 `outlineVariant` 구분선 25곳 → `denebHairline()`) + hint 스윕(수작업 `onSurfaceVariant.copy(alpha=…)` 6곳 → `denebHint()`; **일반 `onSurfaceVariant` 본문색 ~79곳은 의도적으로 유지** — 카드 내부 등 surface-상대 컨텍스트가 섞여 있어 일괄 치환은 라이브 시각 검증 없이는 위험) + `DenebUi.kt` 닥스트링 수정.

착수 전 `grep -r DenebScreenScaffold`로 진행 상황 확인. 관련: [[project_native_design_system_dead]]
