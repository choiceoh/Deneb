# 네이티브 앱 배터리 최적화 개선방안 (client-android)

> **방법**: `client-android/` (KMP, Android/iOS/desktop) 의 배터리 소모원을 코드로 매핑 → Android 표준 전력 패턴과 대조 → 개선안 우선순위화. 단일 사용자 daily-driver = Galaxy S26.
> **일시**: 2026-06-27
> **한 줄 결론**: 지배적 standby 소모원은 **`dataSync` 포그라운드 서비스가 SSE 연결을 24/7 살려두며 프로세스를 Doze 에서 제외**하는 것(`DaemonService` + `TaskScheduler.startPushSubscription`)이다. 그런데 이건 **이미 동작하는 FCM 폴백과 대부분 중복**이다 — 게이트웨이는 "라이브 SSE 클라이언트가 없을 때" FCM 으로 프로액티브 리포트를 보내도록 이미 설계돼 있다(`FcmService.kt`). **본안 = 백그라운드에선 SSE/포그라운드서비스를 내리고 FCM 에 위임, 포그라운드 복귀 시 재연결+커서 드레인.** 표준 Android 아키텍처이자 최대 standby 절감. 단 **FCM 가용성(`google-services.json` 빌드타임 게이팅)**에 따라 graceful degrade 필요.

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
- **왜 안전한가**:
  - **커서 기반 sync** 라 끊겨도 무손실 — `syncNativeState`(`:987-1067`)가 persisted 커서를 재생해 백그라운드 동안 쌓인 이벤트를 복귀 시 한 번에 당긴다.
  - **프로액티브 전달은 FCM 이 인수** — 게이트웨이가 SSE 부재를 감지해 FCM 발사(이미 구현된 폴백 경로). 사용자는 트레이 알림을 그대로 받는다.
  - `appInForeground` 플래그(`TaskScheduler`)가 **이미 존재** — 지금은 트레이 vs in-app 알림 분기에만 쓰는 걸, **연결 라이프사이클 게이트로 확장**.
- **이득**: 화면 꺼진 동안 Doze 진입 → standby drain 대폭↓(영속 소켓 + 30s keepalive + Doze 면제가 한꺼번에 사라짐). Android 표준 패턴.
- **트레이드오프**: 백그라운드 프로액티브가 SSE(즉시) → FCM(Doze 배칭, 약간 지연)로. 프로액티브 비서엔 high-priority FCM 이 표준이고 게이트웨이가 이미 그 길을 가지므로 회귀 아님.
- **★제약 (M4 와 연동 필수)**: FCM 은 **`google-services.json` 빌드타임 게이팅**(`build.gradle.kts:16` — 없으면 no-push degrade). daily-driver 호스트 빌드는 FCM 있음 → 안전. **FCM 없는 빌드(공개 CI/FOSS)에서는 백그라운드 SSE 차단 시 프로액티브 0** → 그땐 M3(저비용 영속 연결)로 폴백. 즉 **빌드의 FCM 가용성에 따라 분기**: FCM 가용 → M1, 비가용 → M3.

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
| **M1** SSE 포그라운드 게이팅 + FCM 백그라운드 | 🔥 최대 (standby drain 대부분) | 중 (FCM 가용성 분기 필요) | 🟢 **본안** — FCM 가용 빌드부터 |
| **M2** NetworkCallback 재연결 | 중 | 낮음 | 🟢 **즉시 채택 권장** (M1 무관 단독 가치) |
| **M4** Doze/세이버 인지 | 중 | 낮음 | 🟢 M1 과 묶어서 |
| **M3** keepalive/backoff 튜닝 | 소~중 | 낮음 | 🟡 FCM-less 빌드 폴백으로만 |
| **M5** 위젯/알림 | 소 | 낮음 | ⛔ 후순위 |

**착수 순서 제안**: **M2(저위험 단독)** → **M1+M4(FCM 가용 빌드 한정, degrade 경로 포함)**. M3 는 FCM 불가 빌드의 폴백으로만.

---

## 5. 측정 (개선 검증 — 추측 금지)

- **선결**: 변경 전/후 **standby drain** 을 같은 조건에서 비교. `adb shell dumpsys batterystats --charged` + Battery Historian, 또는 `dumpsys deviceidle` 로 Doze 진입 여부 확인(M1 의 핵심 가설 = "백그라운드에서 실제로 Doze 에 드는가").
- **라디오 wakeup**: `dumpsys batterystats` 의 mobile radio active time / wakeup count 로 keepalive 영향 측정(M3).
- **회귀 가드**: M1 적용 후 **프로액티브 리포트가 FCM 으로 실제 도착하는지**(앱 백그라운드/killed 시 트레이 알림 수신) + 포그라운드 복귀 시 **커서 드레인으로 누락 0** 확인. `native-app.sh` 하네스로 포그라운드 재연결 흐름을, 실기기로 백그라운드 FCM 전달을 검증(시스템 Doze 는 실기기 필요 — `.claude/rules/native-live-app.md` 한계).
- **반증가능 예측**(편집 전 선언): "M1 은 화면-off 1시간 standby drain 을 −X%, 프로액티브 전달 성공률을 ±0(FCM 가용 빌드) 으로 바꾼다 — 백그라운드에서 Doze 에 들기 때문." Doze 진입이 dumpsys 로 확인 안 되면(OEM 가 포그라운드 서비스 잔재로 막으면) 원인 규명 후 keep/revert.

---

## 6. 관련 문서

- 네이티브 라이브 검증(실기기 한계 포함): `.claude/rules/native-live-app.md`
- 코드: `DenebGatewayClient.kt`(SSE/sync), `DaemonService.kt`(포그라운드 서비스), `FcmService.kt`/`FcmRegistration.kt`(푸시), `DenebApplication.kt`(`ProcessLifecycleOwner`), `androidApp/build.gradle.kts:16`(FCM 빌드 게이팅)
- 선행 네이티브 리뷰 포맷: `docs/research/android-launcher-review.md`
