# Deneb Android 개인업무 런처 전환 — 설계 검토

> **상태**: 착수 (Phase 0 진행 중)
> **시작**: 2026-06-20
> **결정**: Deneb 네이티브 클라(client-android)의 **안드로이드 타깃을 업무 특화 홈 런처로 전환**. 범용 런처(Nova/Niagara)가 아니라 **오프라인-퍼스트 셸 + 강한 게이트웨이로 구동되는 생성형 홈**.

이 문서는 단계마다 갱신하는 살아있는 설계 문서다. 코드 진실원은 아니고, 방향·결정·검증전략의 단일 참조점이다.

---

## 0. 왜 — 차별점

일반 런처는 온디바이스 dumb 클라이언트(아이콘 격자 + 멍청한 위젯). Deneb 런처는 **강한 게이트웨이(로컬 LLM·150+ 도구·OCR/ASR/임베딩·위키 지식·자율 에이전트·DGX GPU)의 thin 클라이언트** → 홈이 "아이콘+위젯"에서 **비서실장 라이브 콕핏**으로.

페르소나 합치: "비서실장형 단일 에이전트"가 폰 켜면 첫 화면. 단일 사용자·단일 기기(S26 데일리드라이버) = 런처의 단일·오피니어네이티드 모델과 정확히 일치.

## 1. 검증된 사실 (코드 조사)

| 항목 | 결과 | 함의 |
|---|---|---|
| 오프라인 캐시 | ⚠️ 부분 — **메일은 이미 로컬 캐시**(`DenebClientMail`의 owner-fingerprint 캐시, cache-then-network). **피드/캘린더는 미캐시** → Tailscale 끊김 때 피드만 비었던 원인 | ★ 피드 캐시 신설이 1번 작업 → ✅ **완료**(`WorkFeedCache`, 메일 패턴 미러). 캘린더 캐시는 잔여 |
| 빌드 플레이버 | `foss`(기본·OTA) / `playStore` | 런처 거동을 foss/토글 뒤로 격리, 되돌리기 안전 |
| 상주 기반 | `DaemonService`(포그라운드) + `DaemonController`(expect/actual) | 상주·생존 토대 있음 |
| 안드로이드 통합 | 위젯·FCM·공유캡처·딥링크·캘린더/연락처/SMS·음성·OCR | 런처 표면이 이미 80% |
| 검증 한계 | 런처 거동(HOME 인텐트·PackageManager·ROLE_HOME)은 **안드로이드 전용** | 데스크톱 하네스로 UI만, 실거동은 **S26**에서 |

## 2. 아키텍처 — 3층

**A. 런처 셸 (순수 로컬, 게이트웨이 무관) — 토대**
HOME 인텐트(activity-alias, 토글로 opt-in) + 뒤로/홈 시맨틱 / 로컬 시계·날짜 / 앱 드로어·즐겨찾기(PackageManager) / 캡처 진입 / **★ 브리핑 로컬 캐시**. 게이트웨이 없어도 100% 뜬다.

**B. 브리핑 레이어 (연결 시 enrich) — 생성형 홈**
기존 피드/메일/캘린더 글랜스를 셸 위에. ★ 게이트웨이가 **홈 콘텐츠를 조립**(server-driven UI: `DenebUiNode`/워크피드 카드 기계 재사용). 조용한 "오프라인" 배지 + 복구 시 자동갱신(native-sync/SSE).

**C. 센싱 강화 (정보 수집, 별 트랙·권한 게이트)**
`NotificationListenerService`(시스템 알림 → 게이트웨이가 트리아지/요약) — 현 SSH watcher 정식 대체 / UsageStats·포그라운드·잠금해제 리듬 / **프라이버시 경계 명시**.

> **불가침 규칙**: 모든 게이트웨이 기능은 enrich 레이어. 셸은 게이트웨이 죽어도 뜬다. 홈은 서버 응답에 블록 금지(즉시표시 + async enrich + 캐시).

## 3. 게이트웨이 구동 기능 (B·C층의 기회)

1. **홈 검색창 = 풀 에이전트** (앱검색 아님 — 위키·메일·캘린더·150+도구로 답/실행)
2. **생성형 홈** — 홈 콘텐츠가 에이전트 출력 (server-driven UI 기존 기계 재사용)
3. **알림 트리아지** — 시스템 알림을 서버 LLM이 우선순위·요약·교차합성
4. **시맨틱·예측 앱 실행** — 의도→앱, 컨텍스트 기반 예측
5. **캡처·음성 → 서버 처리** (무거운 건 DGX 오프로드)
6. **지식-그라운디드 글랜스** (다음 미팅에 위키 맥락)
7. **사전 푸시로 캐시 워밍** (오프라인에도 서버가 계산한 최신 홈)

## 4. 단계별 로드맵

| 단계 | 내용 | 검증 |
|---|---|---|
| **0. 셸 스파이크** | 앱 드로어(UI→provider) · HOME activity-alias + 토글 · 뒤로/홈 시맨틱 · **오프라인 피드 캐시** · 피드 홈을 루트로 | UI=하네스, 거동=S26 1주 실사용 |
| **1. MVP** | 즐겨찾기 독 · 기본홈 프롬프트(ROLE_HOME) · **홈 검색=에이전트** · 셸 무잔크 | S26 |
| **2. 생성형 홈** | 서버 조립 홈 카드 · 원탭 캡처 · 지식 글랜스 | S26 |
| **3. 센싱 (별 트랙)** | 알림 리스너 등 — 프라이버시 설계 선행 | S26 |

## 5. 결정 (기본값, 진행하며 조정)

- 앱 드로어: 전체앱 검색 드로어 + 즐겨찾기(Phase 1) — 처음엔 전체앱(폰 못 쓰는 사고 방지)
- One UI Home: **토글 유지**(둘 다 두고 전환, 안전)
- 센싱(C층): **나중 별 트랙**(프라이버시 먼저)
- 오프라인 캐시: 마지막 하루치(피드+다음미팅+임박메일)

## 6. 진행 로그

- 2026-06-20: 검토 완료, 방향 확정. Phase 0 착수 — 설계 문서 + 앱 드로어 UI(공유 컴포저블).
- 2026-06-20: 앱 드로어 provider(PackageManager expect/actual) + nav 배선(더보기→앱) + foss `<queries>`. 컴파일 검증(desktop+android).
- 2026-06-20: **UI 중단, 기반/백그라운드 우선**(사용자 지시). ✅ **오프라인 워크피드 캐시**(`WorkFeedCache` + `AppSettings` 키 + DenebGatewayClient init 복원/refresh 저장, 메일 캐시 패턴 미러) + 코덱 단위테스트. 홈이 게이트웨이 없어도 마지막 브리핑을 렌더.
- 2026-06-20: **센싱 설계 확정** — "다 읽되 다 보여주지 않는다"(넓은 캡처 + 게이트웨이 LLM 트리아지 + 좁은 surface, 재알림 없음). 발견: **이미 `/api/event/ingest` 능동판정 파이프라인 존재**(OTP·스팸·영수증·일상알림 → NO_REPLY, 신호만 proactiveRelay). ✅ **센싱 게이트웨이 측**: 판정 로직을 `ingestPhoneEventAsync`로 추출 + **`miniapp.event.ingest` RPC**(토큰 인증 — 네이티브가 닿는 surface, 기존 엔드포인트는 loopback-only)로 동일 판정. 빌드·vet·등록테스트 green. **잔여=Android NotificationListenerService**(캡처: 온디바이스 보안/잡음 프리필터 → ingest, S26 검증).
- 2026-06-20: ✅ **센싱 캡처 측(Android)** — `DenebNotificationListenerService`(온디바이스 프리필터: 자기앱·상시·그룹요약·미디어/시스템/서비스/진행·저우선·민감앱(비번/OTP앱)·secret 제외 → 통과분만 `miniapp.event.ingest`) + foss 매니페스트 서비스 선언 + `DenebGatewayClient.ingestEvent`. **센싱 루프 코드 완성**(알림→온디바이스 필터→게이트웨이 판정→신호만 피드/푸시). 컴파일 검증(desktop + androidApp foss + 매니페스트 머지). 잔여: S26 알림접근 권한 후 실동작 검증 + 설정 토글 UI(나중).
- 2026-06-20: **센싱 하드닝(백그라운드 더)** — ① 게이트웨이 **tiered 트리아지**: `pilot.CallTinyLLM` 1차 yes/no 게이트(`worthFullJudgment`)로 비싼 풀 판정 턴 전에 광고/OTP/일상 알림 컷(notification/sms만, context/clipboard는 통과, fail-open). ② Android **온디바이스 dedup/스로틀**: 45s 윈도우·200키 bounded LRU로 재게시/업데이트 알림 폭주 방지. 컴파일·vet·server 테스트·detekt/spotless green.
- 2026-06-20: ✅ **캘린더 오프라인 캐시** — `CalendarEvent` @Serializable + `CalendarCache`(메일/피드 패턴 미러, owner-fingerprint) + `AppSettings` 키 + DenebGatewayClient init 복원 + `refreshCalendar` 저장 + 코덱 테스트. **오프라인 셸 완성: 피드·메일·캘린더 모두 게이트웨이 없어도 마지막 상태 렌더.**
- 2026-06-20: ✅ **백그라운드 캐시 워밍** — 조사 결과 네이티브 백그라운드는 주기 폴러 없이 SSE/FCM/포그라운드로만 도는 `syncNativeState()` 이벤트 구동(`TaskScheduler`+`DaemonService` 상주). 기존엔 그 안에서 **피드만**(그것도 비었을 때) 갱신 → 캘린더·메일은 화면 진입 때만 갱신돼 오래 백그라운드면 낡은 글랜스. ★ 성공한 sync(=게이트웨이 도달) 뒤 `warmHomeCachesThrottled()`로 캘린더+메일을 **10분 스로틀** 워밍(버스티 SSE 폭주 방지, 각 refresh가 owner-fingerprint 캐시 persist+credEpoch 펜스). 이제 오프라인 셸이 *마지막 방문*이 아니라 *최근 백그라운드* 상태를 렌더. 컴파일·detekt·spotless green.
- 2026-06-20: ✅ **서버측 사전푸시 (캘린더)** — 조사로 채널 매핑: 워크피드는 이미 native-sync 이벤트로 푸시(`workfeed.*`), 메일도 이미 proactiveRelay→`nativeSync.Append`+push로 커버. **유일 갭=캘린더** — `localcal.Store` CRUD가 sync 이벤트를 안 흘려 에이전트/크론이 만든 일정(예: "내일 3시 미팅 잡아줘")이 클라 워밍(최대 10분) 전엔 글랜스에 안 떴다. ★ `TypeCalendarChanged`("calendar.changed") 신설 + `localcal.Store`에 **디커플드 옵저버**(`SetChangeObserver`, 락 밖 호출 — 플랫폼 스토어는 nativesync 무의존)를 달아 Create/Update/Delete가 단일 choke point에서 이벤트 emit(모든 변경경로=RPC·툴·메일제안·크론 커버). 클라는 `calendar.changed` 수신 시 warm 스로틀을 무시하고 즉시 캘린더 새로고침. push 채널은 알림과 결합돼 있어(silent wake 없음) **append-only로 mirror**(over-notification 회피). `make ci` 8/8 green + localcal 옵저버 단위테스트. **백그라운드/서버푸시 트랙 일단락.**
- 2026-06-20: ✅ **UsageStats 센싱** — 앱 사용 리듬 센싱(C층). 낱개 앱전환은 노이즈라 **on-device에서 압축한 코어스 다이제스트**(앱 라벨+분, 상위 5개, 6시간 윈도우)만 forward. `expect fun readWorkUsageDigest()`(commonMain) + Android actual(`UsageStatsManager`+AppOps 권한 체크+런처블 앱만+민감앱 제외) + 3개 스텁(desktop/ios/wasm=null). `DenebGatewayClient`가 sync 경로에서 **6시간 스로틀**로 `ingestEvent("usage", …)`(권한 없어도 스로틀 무장→매 sync 프로빙 방지). 게이트웨이 `usage` 타입=전용 kindLabel+**기본침묵 guidance**(거래처앱 장시간 집중 등 분명한 업무신호일 때만 surface)+tiny-gate 우회(notificationLike=false, 6h 스로틀이라 풀판정 ~4회/일). foss 매니페스트에 `PACKAGE_USAGE_STATS`(특별접근, Settings 부여). `make ci` 9/9 green(android-compile 포함)+게이트웨이 단위테스트. **잔여=S26 Usage access 권한 후 실동작 + 설정 토글 UI(나중).** ⚠️ speculative breadth(소비자=ambient 컨텍스트, 추후 예측앱실행 UI).

### UI 트랙 (백그라운드 소진 후 착수)
- 2026-06-20: ✅ **런처 모드 토글 (UI 트랙 1번)** — "런처가 진짜 런처가 되는" 근간. foss 매니페스트에 **HOME `<activity-alias>`**(`ai.deneb.HomeAlias`→MainActivity, `android:enabled="false"`로 **휴면 기본**=안전 opt-in) + `LauncherMode` expect/actual(Android는 `setComponentEnabledSetting`로 alias 토글·`isEnabled`로 실제 상태 읽기·`Settings.ACTION_HOME_SETTINGS` 딥링크; 나머지 타깃 unsupported no-op) + 설정에 **`런처` 섹션**(`ConfigTab.LAUNCHER`+`LauncherTab`, `supported`일 때만 노출→데스크톱/iOS/Play는 숨김). alias의 component-enabled 상태가 진실원이라 AppSettings 키 불필요(드리프트 0). 켜면 시스템 설정서 기본 홈 선택, 끄면 후보서 빠져 복귀(가역). 머지 매니페스트 검증=`enabled="false"`+targetActivity 해소 확인. `make ci` 9/9 green. **잔여=S26에서 실제 홈으로 설정·홈버튼 라우팅·복귀 검증**(섹션이 foss Android에서만 떠 데스크톱 하네스론 토글 자체 미검증).
- 2026-06-20: ✅ **홈 버튼 → 피드 루트 (UI 트랙 2번)** — 런처일 때 홈 버튼이 *마지막 화면*이 아니라 *브리핑(피드)*으로 가도록. 기존 pulse 패턴 재사용(`DataRepository.openHomeRequested` + `requestOpenHome`/`consumeOpenHomeRequest`, DenebGatewayClient·FakeDataRepository 구현). `MainActivity.handleDeepLinkIntent`(onCreate+onNewIntent 둘 다 경유)가 `ACTION_MAIN`+`CATEGORY_HOME` 인텐트(=Deneb가 홈 앱일 때만 도착, 자연 게이팅) 감지→`requestOpenHome`. `App`이 collect→`navigateToDenebSection(navController, DenebFeed)`(시작지로 popUpTo+singleTop=루트 리셋)→consume. `make ci` 9/9 green. **잔여=S26에서 홈버튼 실제 라우팅 검증**(인텐트라 데스크톱 하네스 불가). 다음 후보=스와이프업 앱 드로어(제스처, S26 튜닝).
- 2026-06-20: ✅ **앱 드로어 폴리시 (UI 트랙 3번, 검증 가능)** — 사용자 "검증 가능한 UI 계속" 선택에 맞춰 데스크톱 하네스로 렌더 확인되는 드로어를 다듬음. ① **로딩↔빈상태 분리**: 프로바이더가 오프스레드 로드라 기존엔 매 진입 시 빈 리스트→`"앱 없음"`이 깜빡였다. `AppDrawerScreen`에 `loaded` 플래그 추가→`AppDrawer`가 `!loaded`면 `DenebLoading`, `loaded && empty`면 `DenebEmpty`(디자인시스템 헬퍼)로 분기. ② **Enter→단일결과 실행**(런처 idiom): 검색이 1개로 좁혀지면 키보드 Search로 바로 실행(`onSearch`). `renderPreviews`의 `app_drawer`(14개 목업)로 **실제 렌더 확인**(검색창+4열 격자, 폴리시 무회귀). `make ci` 검증 후 랜딩.

### 패러다임 재정의 → "스와이프업으로 꺼내는 나이아가라 서랍" (2026-06-20)
> 프론트엔드 계획 탐구에서 콕핏 → 나이아가라 콕핏 → 얇은 레이어로 좁힌 결론(사용자): **Deneb는 평소 리치 앱 그대로, 위로 스와이프할 때만 나이아가라식 앱 서랍을 꺼낸다.** 나이아가라 미니멀리즘은 정체성이 아니라 "당겨 쓰는 서랍". 콕핏·글랜스·생성형 홈은 채택 안 함(Deneb 사용성을 바꾸므로). 계획=`.claude/plans/ancient-conjuring-sunrise.md`.
- 2026-06-20: ✅ **나이아가라 앱 서랍 표면 (P1, 검증 가능)** — `AppDrawer.kt` 아이콘 4열 그리드 → **세로 텍스트 리스트 + ㄱㄴㄷ/A–Z 스크럽 인덱스**. 한글 초성 추출 `hangulInitial(c)=(c-0xAC00)/588`(쌍자음 base 폴딩→14자 인덱스), 섹션 정렬(한글→영문→#), 우측 스크럽을 손가락으로 훑으면 `scrollToItem`. 검색·Enter실행·로딩/빈상태 유지. `LauncherApps.android.kt`는 텍스트-온리라 **아이콘 라스터화 제거**(100+ 드로어블 낭비 삭제). 모노크롬·타이포 idiom과 정확히 일치(그리드 컬러 아이콘=유일 off-brand 제거). `renderPreviews` `app_drawer`(목업 14개)로 **실제 렌더 확인**(초성 섹션 ㄷ/ㅁ/ㅅ/ㅇ/ㅈ…+스크럽). `make ci` 9/9 green.
- 2026-06-20: ✅ **스와이프업 발동 (P2)** — **자체앱(`DenebAppHub`) 화면에서** 바닥 엣지존 위-스와이프 → 외부 앱 드로어(`DenebApps` Niagara 리스트). 사용자 지시 "자체앱 화면에서만 발동"이 정확히 맞음: 자체앱(#2737 re-land, Deneb 미니앱 그리드)은 앱 surface라 "Deneb 앱→위로 스와이프→폰 전체 앱" 레이어링이 자연스럽고, 스크롤 피드를 안 건드려 충돌 회피. `App.kt`에 `bottomSwipeUpToApps` modifier(`ChatModeScreen.modeSwipeToggle` 엣지존 패턴 미러: 바닥 56dp 존에서 시작한 수직>수평 드래그만, 64dp commit, 위로면 `navigate(DenebApps)`; 탭=무브먼트 0이라 바텀바로 통과, 피드/그리드 스크롤=존 위라 무관). 게이팅 `launcherEnabled && onAppHub`(런처모드 + 자체앱 탭). `make ci` 9/9 green(컴파일·네비 검증). ⚠️ **느낌(존 크기·임계·바텀바 공존·애니메이션)은 S26 실기기 튜닝** — 데스크톱 하네스는 터치 재현 불가. **나이아가라 서랍 P1+P2 코드 완성.**
