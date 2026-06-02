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

- **헤더/제목**이 둘 다 있으면 → Deneb (`DenebScreenScaffold`). Material 향 `DenebViewHeader`/`DenebSurface`(`deneb/DenebUi.kt`, 닥스트링이 idiom과 모순)는 마이그레이션 끝나면 제거.
- **컨트롤**이 둘 다 가능하면 → Material. (예: 직접 그린 "☑" 글리프 대신 `Checkbox`)

## 도그마 금지 — "실용적 Material"을 지키는 경계 케이스

idiom 문서엔 "no cards / no icons"라 써 있지만 **기능을 돕는 곳은 남긴다**:

- **카드**: 리스트·행은 flat+하airline. 하지만 독립 *콜아웃 블록*(메일 상세의 AI 분석·발신자 컨텍스트)은 `ElevatedCard`가 그룹핑에 실익 → 유지(또는 하airline 박스, 화면 보고 판단). 모든 카드를 기계적으로 없애지 않는다.
- **칩**: 첨부·관련항목 `AssistChip`은 상호작용·접근성 있는 Material 유지(외형만 정돈). `KaiChip`이 중간 지점.
- **아이콘**: 기능 아이콘(보내기·중지·Meet·상태 점)은 유지, **장식** 아이콘만 배제.

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

1. 기반: `DenebScreenScaffold` 확장(`tabBar` 슬롯 등) + 규칙 문서 + **파일럿 1화면**(일정 상세) → PNG 검증.
2. 상세 화면 팬아웃 (사람·메일상세·크론·토픽문서·위키·카테고리페이지).
3. 상위 화면 (메일·사람·검색·일정·카테고리, tabBar 모드 + `DenebRow` 리스트).
4. 드로어 inline 값 → `DenebType` 참조 + 경쟁 헤더 제거 + divider/hint 스윕 + `DenebUi.kt` 닥스트링 수정.

착수 전 `grep -r DenebScreenScaffold`로 진행 상황 확인. 관련: [[project_native_design_system_dead]]
