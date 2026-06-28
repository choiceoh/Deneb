# 네이티브 앱 배터리 최적화 개선방안 (client-android)

> **방법**: `client-android/` (KMP, Android/iOS/desktop) 의 배터리 소모원을 코드로 매핑 → Android 표준 전력 패턴과 대조 → 개선안 우선순위화. 단일 사용자 daily-driver = Galaxy S26.
> **일시**: 2026-06-27 (2026-06-28 adversarial 리뷰 반영 정정)
> **한 줄 결론**: 지배적 standby 소모원은 **`dataSync` 포그라운드 서비스가 SSE 연결을 24/7 살려두며 프로세스를 Doze 에서 제외**하는 것(`DaemonService` + `TaskScheduler.startPushSubscription`)이다. 가장 큰 standby 절감은 **백그라운드에서 SSE/FGS 를 내려 Doze 진입**(M1)이지만, "FCM 이 백그라운드 전달을 인수"는 **현 게이트웨이로는 부분적으로만 참** — 이미지 리포트·에러/플릿 알림은 FCM 폴백이 없고(`pushHub.publish` 직접), FCM 게이트가 *전역* `subscriberCount()==0`(데스크톱 구독 시 폰 억제)이며 서버 크리덴셜이 별도다(§3.1, 코드 검증). 따라서 **client-only M1 은 알림 소실**. **즉시 안전한 건 M2(연결성 인지 재연결)·M3(backoff 튜닝)** 이고, **M1/M4 는 §3.1 게이트웨이 선결조건 + device(S26) 검증 후**. **이 PR 구현 상태(§6)**: A(게이트웨이 FCM 폴백, 로컬 검증 완료) + M2/M3 + **M1/M4 ON**(`BACKGROUND_DOZE_ENABLED=true`, 운영자 결정 — 배터리 우선, 잔여 엣지케이스는 fix-as-surfaces). (※이 환경엔 Android SDK·실기기가 없어 네이티브 코드의 컴파일/lint/동작 검증은 PR CI[`kotlin-lint`/`android-compile`] + 호스트 S26 에서 수행.)

---

## 1. 배터리 소모 지도 (코드 근거)

| 소모원 | 위치 | 형태 | 주기/트리거 | **백그라운드에서도 도나?** |
|---|---|---|---|---|
| **SSE 이벤트 스트림** | `DenebGatewayClient.kt:1676-1741` (`subscribeEvents`) | 영속 연결, 30s keepalive, 120s socket timeout, 지수 backoff(2→60s) | 연속 | ✅ **예 — 데몬이 24/7 유지** |
| **포그라운드 서비스** | `DaemonService.kt:14-91` (`foregroundServiceType=dataSync`) | START_STICKY 영속 알림 | 연속 | ✅ **예 — Doze 면제** |
| 채팅 스트리밍 | `DenebGatewayClient.kt:1566-1662` (`sendStreaming`) | 단명 스트림 | 채팅 턴 중만 | ❌ 포그라운드 전용 |
| 네이티브 sync pull | `DenebGatewayClient.kt:987-1067` | 커서 기반 RPC | SSE 푸시/포그라운드 복귀 시 | ✅ (데몬 scope) |
| 홈 캐시 warm | `DenebGatewayClient.kt:1072-1077` | 스로틀 새로고침 | 최대 10분마다 | ✅ |
| usage digest | `DenebGatewayClient.kt:1085-1090` | 스로틀 forward | 최대 6시간마다 | ✅ |
| 위치 forward | `DenebGatewayClient.kt:1097-1102` + `LocationSensor.android.kt:24-50` | **one-shot** balanced-power | 최대 10분마다(sync 시) | ✅ (단 비연속) |
| **FCM 푸시** | `FcmService.kt:24-39` | 진짜 Firebase 푸시 | 게이트웨이가 SSE 없을 때 | ✅ (프로세스 killed 도 깨움) |
| 지오펜스 집/직장 | `DenebGeofenceReceiver.kt:25-52` | OS 콜백 | 경계 ENTER/EXIT | ✅ (OS 레벨, 효율적) |
| 알림 리스너 | `DenebNotificationListenerService.kt:34-183` | 시스템 콜백 | 알림당 + dedup/coalesce | ✅ (온디바이스 필터, 저비용) |
| 위젯 | `DenebWidgetProvider.kt` | 시스템 사이클 | 30분 | ✅ (시스템 주도) |

**효율적이라 손 안 댈 것**: 위치(one-shot balanced-power, 10분 스로틀, 연속추적 없음), 지오펜스(OS 레벨), 알림 리스너(온디바이스 dedup+coalesce), 위젯(시스템 30분 사이클). **WakeLock·AlarmManager·exact alarm·연속 위치추적은 없음** — 이미 깨끗.

---

## 2. 핵심 진단 — 24/7 SSE 는 FCM 과 중복이다

배터리 소모의 **거의 전부**가 위 표의 상위 두 줄(SSE + 포그라운드 서비스)에 몰려 있다. 나머지는 스로틀/온디맨드/시스템주도라 작다.

문제의 본질:

1. **포그라운드 서비스가 프로세스를 Doze 에서 제외**한다(`FOREGROUND_SERVICE_DATA_SYNC`). Doze 는 Android 의 standby 전력 절감 핵심인데, 데몬이 그걸 통째로 끈다 → 화면 꺼진 밤새 CPU/라디오가 계속 runnable.
2. **SSE 30s keepalive 가 라디오를 30초마다 깨운다.** 셀룰러 라디오는 idle 로 내려갔다 keepalive 마다 high-power 로 복귀(radio tail). 30초 간격은 FCM 자체 heartbeat(셀룰러 ~4.5분)보다 9배 공격적.
3. **그런데 이 영속 연결은 FCM 과 기능 중복이다.** `FcmService.kt:10-20` 주석: 게이트웨이는 *라이브 SSE 가 없으면* FCM 으로 프로액티브 리포트를 보낸다. 즉 **백그라운드에서 SSE 를 내려도 프로액티브 전달은 FCM 으로 그대로 도착**한다(트레이 알림). 백그라운드의 SSE 가 추가로 주는 건 "사용자가 안 보고 있는 in-app 피드의 실시간 갱신"뿐 — standby 비용 대비 가치가 낮다.

**결론**: 백그라운드 24/7 SSE 는 **standby 배터리를 위해 지불하는데 사용자 가치는 FCM 이 이미 커버**한다. 이게 단일 최대 레버다.

---

## 3. 개선 방법 (영향·위험순)

### M1 — SSE/포그라운드서비스를 포그라운드 게이팅, 백그라운드는 FCM 위임 ★본안

- **무엇**: 앱이 백그라운드로 가면(`ProcessLifecycleOwner` `onStop`) SSE 구독을 끊고 `DaemonService` 를 `stopForeground`/stop → 프로세스가 Doze 진입. 포그라운드 복귀(`onStart`)에 SSE 재연결 + `syncNativeState` 커서 드레인.
- **부분적으로만 안전하다 (★ adversarial 리뷰로 정정, §3.1)**:
  - `appInForeground` 플래그(`TaskScheduler`)가 **이미 존재** — 지금은 트레이 vs in-app 알림 분기에만 쓰는 걸 **연결 라이프사이클 게이트로 확장**.
  - 커서 기반 sync 가 복귀 시 catch-up 의 *기반*이지만 **무손실은 조건부** — 현재 `syncNativeState`(`DenebGatewayClient.kt:1001-1026`)는 `limit=100` 으로 당기다 `pages < 4` 에서 멈춰 `hasMore` 여도 **최대 400 이벤트만** 드레인한다. 긴 백그라운드 후 복귀 1회로는 누락 가능 → **`hasMore==false` 까지 루프/후속 pull 필요**(선결).
  - "FCM 이 프로액티브를 인수"는 **현 게이트웨이로는 부분적으로만 참** — §3.1 의 게이트웨이 측 갭(이미지 리포트·에러/플릿 알림 미폴백, 글로벌 subscriber 게이트, 서버 크리덴셜)이 메워지기 전엔 백그라운드 SSE 차단이 **여러 알림을 조용히 떨군다**.
- **이득**: 화면 꺼진 동안 Doze 진입 → standby drain 대폭↓(영속 소켓 + 30s keepalive + Doze 면제가 한꺼번에 사라짐). Android 표준 패턴.
- **★제약 1 — FCM 가용성은 클라+서버 둘 다**: 클라 `google-services.json`(`build.gradle.kts:16`)은 **토큰 등록만** 켠다. 실제 발송은 게이트웨이 `DENEB_FCM_CREDENTIALS_FILE`(`push/config.go:Enabled()`)이 있어야 살아난다 — 없으면 `pushFCM==nil` 이라 `DeliverFallback` 스킵(`proactive_relay.go:388`). **M1 의 "FCM 가용" 게이트 = 클라 토큰 + 서버 크리덴셜 둘 다 충족** 일 때만. 어느 쪽이라도 없으면 백그라운드 SSE 유지(M3).
- **★제약 2 — 활성 전송 예외**: 사용자가 긴 에이전트 턴을 보낸 직후 백그라운드/잠금하면, FGS 즉시 종료가 in-flight `chat/stream` POST 의 프로세스/네트워크 keepalive 를 끊는다(FCM 은 프로액티브만 커버, user-initiated 스트림은 아님). **활성 스트림 중에는 FGS 를 내리지 말 것**(짧은 grace 또는 전송완료까지 유예).

### 3.1 M1 선결 조건 (adversarial 리뷰로 발견, 코드 검증 완료 — 일부 구현됨)

> M1 의 "FCM 이 백그라운드 전달을 인수"는 **현 게이트웨이로는 성립하지 않는** 경로가 다수다. 백그라운드 SSE(M1)를 *활성화*하기 전에 아래가 선행돼야 사용자 알림이 안 샌다. 모두 소스 대조로 확인. **✅=이 PR 에서 구현, 🔲=미구현(M1 활성화 전 필수).**

| 상태 | 갭 | 근거 | 영향 / 선결 |
|---|---|---|---|
| ✅ | **이미지 프로액티브 미폴백** | `deliverNativeImage` 가 `pushHub.publish` 만(`proactive_relay.go`) | → `publishProactive` 로 FCM 폴백 추가 (이 PR) |
| ✅ | **글로벌 subscriber 게이트** | FCM 이 전역 `subscriberCount()==0`(`proactive_relay.go:388`); 데스크톱 Andromeda 가 같은 `/events` 구독 | → `mobileSubscriberCount()` 술어 + 클라 `X-Deneb-Client-Kind: mobile` 헤더 (이 PR) |
| ✅ | **비-리포트 pushHub 발행 미폴백** | 에러(`notify_relay.go`)·플릿(`server_http_fleet_hook.go`) `pushHub.publish` 직접 | → 둘 다 `publishProactive` 경유로 FCM 폴백 (이 PR) |
| ✅ | **스케줄러 취소 누락** | FGS 종료만으론 SSE 안 끊김 — `TaskScheduler` 가 process-lifetime scope+pushJob 소유, `ChatViewModel` 도 `start()` 호출 | → `TaskScheduler.stop()` + `BackgroundConnectionPolicy` 가 단일 소유 (이 PR) |
| 🔲 | **서버 크리덴셜 게이트** | `push.Config.Enabled()` 는 `CredentialsFile` 필요(`push/config.go`) | 클라 google-services 있어도 서버 크리덴셜 없으면 발송 0 → M1 게이트에 서버 크리덴셜 확인 포함 |
| 🔲 | **acked 토큰 게이트** | `FcmRegistration` 은 best-effort·실패 삼킴, `Notifier.DeliverFallback` 은 토큰 store 비면 early-return(`notifier.go:79-86`) | 토큰 등록이 조용히 실패하면 폴백 타깃 0 → M1 게이트는 **확인된 등록 토큰**(또는 test push 성공) 요구 |
| 🔲 | **sync 페이지 캡** | `pages < 4` × `limit=100`(`DenebGatewayClient.kt:1001-1026`) | >400 백로그면 복귀 1회로 미드레인 → `hasMore` 까지 루프 |
| 🔲 | **네이티브-sync 보존 한도** | 서버가 `native_sync.jsonl` 5MB 초과 시 최근 3,000 이벤트만 유지(`nativesync/store.go:16-24,121-131`); 커서가 잘린 tail 밑이면 `Pull` 이 못 돌려줌 | 며칠/바쁜 구간 백그라운드면 stale 커서가 이벤트 영구 누락 → 보존 확대 또는 **snapshot/full-refresh** 경로 |
| 🔲 | **활성 chat 스트림** | FGS 종료가 in-flight 스트림 keepalive 절단(`ChatViewModel.kt`, `DenebGatewayClient.kt:451-463`) | 잠금 시 진행 중 답변 중단 → 활성 전송 예외 |
| 🔲 | **FCM 알림 탭 라우팅** | killed/백그라운드 시 notification payload 는 `onMessageReceived` 안 탐 → `sendProactiveReportNotification`(=`EXTRA_OPEN_WORK_TOPIC` 부여) 우회; `handleDeepLinkIntent` 는 그 extra 만 반응, FCM `data["kind"]` 무시 | FCM 알림 탭이 업무토픽 대신 기본 런처 열림 → FCM click-action/data-intent 딥링크 핸들러 |

### M2 — 연결성 인지 재연결 (NetworkCallback) ★저위험 보완

- **무엇**: 현재 `ConnectivityManager` `NetworkCallback` **미사용**(탐색 확인). 네트워크가 끊겨도 SSE backoff 루프가 죽은 망에 2→60s 재시도를 반복 → 라디오 헛깨움. `NetworkCallback` 등록해 **연결 복귀 시에만** 재연결, 오프라인이면 backoff 루프 suspend.
- **이득**: 지하철·엘리베이터·비행기 등 무신호 구간의 헛된 재시도 제거(배터리+정확성). M1 채택 여부와 무관하게 단독으로 가치.
- **위험**: 낮음. 표준 패턴, 연결 로직 국소 변경.

### M3 — 영속 연결이 product 요구면 더 싸게 (M1 의 FCM-less 폴백)

- **keepalive 간격↑**: 30s 는 과공격적. socket timeout(120s) 아래에서 90s 정도로 늘리면 라디오 wakeup 1/3. (서버 keepalive 캐던스와 함께 조정 — `STREAM_SOCKET_TIMEOUT_MS`.)
- **backoff 상한↑ + 지터**: 60s 상한을 늘리고 지터 추가로 동기화된 재시도 폭주 방지.
- 적용처: FCM 불가 빌드, 또는 "밤에도 즉시 in-app" 가 진짜 요구일 때. M1 이 가능하면 M3 보다 M1 이 항상 우월(Doze 진입이 keepalive 튜닝보다 훨씬 큼).

### M4 — Doze/배터리세이버 인지

- **무엇**: `ACTION_DEVICE_IDLE_MODE_CHANGED`/`ACTION_POWER_SAVE_MODE_CHANGED` 수신 → 시스템이 절전 진입하면 선제적으로 SSE 내리고 FCM(high-priority 는 Doze 에서도 깨움)에 의존. M1 의 일반화(앱 백그라운드뿐 아니라 시스템 절전 신호까지).
- **이득**: 사용자가 배터리세이버 켰을 때 그 의도를 존중. M1 과 같은 메커니즘 재사용.

### M5 — 작은 것들 (낮은 우선순위)

- 위젯 30분 고정 사이클(최대 48 RPC/일)을 FCM-주도 invalidation 으로 바꾸면 미세 절감 — 효과 작아 후순위.
- 알림 리스너는 이미 dedup(45s)+coalesce(2s/≥3개)로 잘 묶임 — 손댈 것 없음.

---

## 4. 우선순위 / 판정

| 방법 | 영향 | 위험 | 판정 |
|---|---|---|---|
| **M2** NetworkCallback 재연결 | 중 | 낮음 | 🟢 **즉시 채택 권장** — FCM 핸드오프 무관, 클라 국소 변경 |
| **M3** keepalive/backoff 튜닝 | 소~중 | 낮음 | 🟢 **즉시 채택 가능** (backoff 지터/캡은 클라 단독; 서버 keepalive 변경은 별도) |
| **M1** SSE 포그라운드 게이팅 + FCM 백그라운드 | 🔥 최대 (standby drain 대부분) | **높음** (§3.1 🔲 잔여 + device 검증 미완) | 🟢 **ON (운영자 결정)** — A 로 게이트웨이 측 갭은 닫음; 잔여 엣지케이스는 fix-as-surfaces |
| **M4** Doze/세이버 인지 | 중 | 중 (M1 과 동일 핸드오프 의존) | 🟢 M1 과 함께 ON |
| **M5** 위젯/알림 | 소 | 낮음 | ⛔ 후순위 |

**착수 순서 (실제)**: **A(게이트웨이 FCM 폴백)+M2+M3 먼저** → A 로 §3.1 핵심 갭(이미지/에러/플릿 폴백·per-mobile 술어·스케줄러 취소) 해소 → **M1/M4 를 ON**(운영자 결정: 배터리 우선, §3.1 🔲 잔여는 발생 시 수정). ⚠️ **`BACKGROUND_DOZE_ENABLED=true`** 로 활성화됨 — 되돌리려면 false 한 줄. 잔여 위험(acked-토큰·sync 보존/페이지·FCM 탭 딥링크·활성 스트림)은 §3.1 참조, 증상 보이면 fast-follow.

> **★ 검토 변경 이력**: 이 §3.1·우선순위는 PR #2922 의 adversarial 리뷰(Codex)가 짚은 6개 갭을 **소스 대조로 검증한 뒤** 반영했다. 초판은 M1 을 "FCM 가용 빌드부터 본안"으로 과신했으나, FCM 핸드오프가 *현 게이트웨이로는* 이미지 리포트·에러/플릿 알림·멀티-구독·서버 크리덴셜에서 성립하지 않아 **client-only M1 은 알림 소실**임이 확인됐다.

---

## 5. 측정 (개선 검증 — 추측 금지)

- **선결**: 변경 전/후 **standby drain** 을 같은 조건에서 비교. `adb shell dumpsys batterystats --charged` + Battery Historian, 또는 `dumpsys deviceidle` 로 Doze 진입 여부 확인(M1 의 핵심 가설 = "백그라운드에서 실제로 Doze 에 드는가").
- **라디오 wakeup**: `dumpsys batterystats` 의 mobile radio active time / wakeup count 로 keepalive 영향 측정(M3).
- **회귀 가드**: M1 적용 후 **프로액티브 리포트가 FCM 으로 실제 도착하는지**(앱 백그라운드/killed 시 트레이 알림 수신) + 포그라운드 복귀 시 **커서 드레인으로 누락 0** 확인. `native-app.sh` 하네스로 포그라운드 재연결 흐름을, 실기기로 백그라운드 FCM 전달을 검증(시스템 Doze 는 실기기 필요 — `.claude/rules/native-live-app.md` 한계).
- **반증가능 예측**(편집 전 선언): "M1 은 화면-off 1시간 standby drain 을 −X%, 프로액티브 전달 성공률을 ±0(FCM 가용 빌드) 으로 바꾼다 — 백그라운드에서 Doze 에 들기 때문." Doze 진입이 dumpsys 로 확인 안 되면(OEM 가 포그라운드 서비스 잔재로 막으면) 원인 규명 후 keep/revert.

---

## 6. 구현 상태 (이 PR)

**A — 게이트웨이 FCM 폴백 완성 (Go, 로컬 검증 완료 ✅)**: 이미지/에러/플릿 발행을 `publishProactive` 로 통일해 FCM 폴백 추가, 술어를 `mobileSubscriberCount()` 로 전환(데스크톱이 폰 폴백 안 막음). `go build`·`go vet`·서버 패키지 테스트(+신규 `mobileSubscriberCount`/`clientKindFromHeader` 테스트) 통과. **오늘도 존재하던 killed-phone 알림 소실 버그를 함께 수정.**

**B — M2+M3 (Kotlin, CI 게이트)**: M3=backoff 캡 60s→120s. M2=`BackgroundConnectionPolicy` 가 `ConnectivityManager` 기본망 콜백으로 무신호 시 SSE 취소(`TaskScheduler.stop`)·복귀 시 재개. `ACCESS_NETWORK_STATE` 추가. 클라가 `X-Deneb-Client-Kind: mobile` 전송(A 의 술어 짝).

**C — M1/M4 (Kotlin, ★ON — `BACKGROUND_DOZE_ENABLED=true`)**: 같은 policy 가 백그라운드 시 SSE+FGS 를 내려 Doze 진입시키고 백그라운드 전달을 FCM 에 위임. **운영자 결정으로 활성화**(배터리 우선; §3.1 🔲 잔여 엣지케이스는 증상 발생 시 수정). A 로 게이트웨이 측 핵심 갭(이미지/에러/플릿 폴백·per-mobile 술어·스케줄러 취소)은 이미 닫혔다. 되돌리려면 플래그 false 한 줄(M2 는 그대로 유지).

> ⚠️ **검증 한계 + 잔여 위험**: 이 환경엔 Android SDK·실기기가 없어 Kotlin(B/C)은 로컬 컴파일/lint/동작검증 불가 → PR CI(`kotlin-lint`/`android-compile`)가 컴파일·lint 게이트. **실제 배터리/Doze/FCM 동작은 S26 에서 확인 권장**. M1-ON 의 잔여 위험(§3.1 🔲): ①게이트웨이에 FCM 크리덴셜 없으면 백그라운드 알림 0(가장 먼저 확인) ②며칠+busy 백그라운드면 sync 누락(보존/페이지) ③FCM 알림 탭이 업무토픽 대신 런처 ④백그라운드 진입 중 진행 중 채팅 스트림 라이브뷰 끊김(결과는 서버 transcript 에 남아 복귀 시 노출). 증상 보이면 해당 fast-follow.

---

## 7. 관련 문서

- 네이티브 라이브 검증(실기기 한계 포함): `.claude/rules/native-live-app.md`
- 코드: `DenebGatewayClient.kt`(SSE/sync), `DaemonService.kt`(포그라운드 서비스), `FcmService.kt`/`FcmRegistration.kt`(푸시), `DenebApplication.kt`(`ProcessLifecycleOwner`), `androidApp/build.gradle.kts:16`(FCM 빌드 게이팅)
- 선행 네이티브 리뷰 포맷: `docs/research/android-launcher-review.md`
