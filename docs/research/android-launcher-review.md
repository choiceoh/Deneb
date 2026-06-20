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
