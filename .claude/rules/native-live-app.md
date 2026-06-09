---
description: "client-android 실제 앱을 서버에서 라이브로 띄워 보고/조작 검증 (Xvfb+matchbox+Compose Desktop, 프로덕션 연결)"
globs: ["client-android/**", "scripts/dev/native-app.sh"]
---

# Native Live-App Harness (서버에서 실제 앱 검증)

> **`renderPreviews`는 mock 데이터 정적 PNG일 뿐이다.** 실제 동작·실데이터·상호작용을 보려면 진짜 앱을 띄워라. `scripts/dev/native-app.sh`가 **client-android의 Compose Desktop 타깃**(`ai.deneb.MainKt`, commonMain을 Android/iOS와 공유하는 *바로 그 앱*)을 헤드리스 X 디스플레이에서 실행해, 에이전트가 스크린샷으로 보고 tap/type으로 조작하게 한다 — Deneb 네이티브 앱에 한정된 "computer use".

## 언제 무엇을 쓰나

| 검증 깊이 | 도구 | 본다 |
|---|---|---|
| 컴파일만 | `./gradlew :composeApp:compileKotlinDesktop` | 타입/빌드 |
| 컴포저블 외형 | `./gradlew :composeApp:renderPreviews` → `/tmp/deneb-render/*.png` | **mock** 데이터 정적 PNG |
| **실제 앱 라이브** | **`scripts/dev/native-app.sh`** | **프로덕션 실데이터 + 상호작용 + 상태 흐름** |
| 시스템 제스처 | 실기기 (Galaxy S26) | 엣지 스와이프 등 — 하네스로 재현 불가 |

UI 변경(레이아웃/네비/상태/입력)을 "실제로 그렇게 보이고 동작하나"까지 봐야 할 때 이 하네스를 쓴다.

## 빠른 사용

```bash
scripts/dev/native-app.sh start          # Xvfb + matchbox + 앱 기동 (phone, 프로덕션 자동연결)
scripts/dev/native-app.sh shot home      # → ~/.cache/deneb-native/shots/home.png  (Read 도구로 확인)
scripts/dev/native-app.sh tap 245 37     # 스크린샷에서 본 픽셀을 클릭
scripts/dev/native-app.sh type "안녕"     # 입력 (먼저 필드를 tap 해 포커스)
scripts/dev/native-app.sh key Return     # 키 (Return/Escape/ctrl+a/BackSpace/Tab…)
scripts/dev/native-app.sh view           # noVNC 노출 → 사람도 브라우저로 관전/조작
scripts/dev/native-app.sh stop
```

## 명령 레퍼런스

| 명령 | 동작 |
|---|---|
| `start [phone\|desktop]` | Xvfb + WM + 게이트웨이 시드 + 앱 기동. idempotent(이미 떠 있으면 지오메트리만 재적용). |
| `shot [name]` | 앱 창 스크린샷 → `~/.cache/deneb-native/shots/<name>.png`. 경로를 stdout으로 출력 → Read. |
| `tap X Y` / `dbltap X Y` | 클릭 / 더블클릭 (좌표 = 스크린샷 픽셀). |
| `type "텍스트"` | 포커스된 필드에 입력. **먼저 필드를 `tap` 해야 한다.** |
| `key KEY [KEY…]` | 키 입력. xdotool 키심(`ctrl+a`, `Return`, `Escape`, `BackSpace`, `Tab`, `Down`). |
| `swipe X1 Y1 X2 Y2` | 드래그(리스트 fling-scroll). |
| `scroll up\|down [n]` | 창 중앙에서 휠 스크롤. |
| `find "텍스트"` | 화면 OCR(tesseract kor+eng) → 그 텍스트의 픽셀 좌표 `X Y` 출력. 픽셀 하드코딩 대신 **텍스트로** 탭 위치를 잡는다. |
| `assert "텍스트"` | 화면 OCR → 텍스트 있으면 exit 0, 없으면 1. **기대 화면이 실제로 떴는지** 검증(스모크가 wrong-screen/blank-render를 잡는 근거). |
| `taptext "텍스트"` | OCR-find 후 그 텍스트를 탭. 레이아웃이 바뀌어도 안 깨지는 네비. ★앵커는 **OCR이 실제로 읽는** 문자열로(예: `← 뒤로`는 화살표 탓에 빗나감→`보관`, `역할별`보다 `경량`이 안정적). 화살표/아이콘 인접 텍스트는 피하고 `assert`로 먼저 검증. |
| `wait-for "텍스트" [초]` | OCR을 폴링해 그 텍스트가 뜰 때까지 대기(기본 8초, 0.4초 간격). 고정 `sleep` 대신 **렌더 완료를 실제로 기다린다** — 화면 전환 후 정착 대기용. 뜨면 exit 0, 타임아웃 1. 콜드 첫 탭이 애니메이션 중 발사돼 빗나가는 flake를 줄인다. |
| `seed [url] [token]` | `~/.deneb-client` 게이트웨이 설정 재기록(기본: 프로덕션). |
| `status` / `logs [n]` | 상태 / 앱 로그. |
| `restart [profile]` / `stop` | 재시작 / 전체 종료. |

## 전형적 워크플로우 (UI 변경 검증)

```bash
# 1) 코드 수정 후 기동(데몬 warm면 ~1–7초, 콜드 첫 빌드는 수~수십 초)
scripts/dev/native-app.sh start
scripts/dev/native-app.sh shot before        # Read로 현재 화면 확인 → 좌표 파악
# 2) 화면 이동·조작
scripts/dev/native-app.sh tap 245 37          # 예: 설정 탭
scripts/dev/native-app.sh shot settings       # Read로 결과 확인
# 3) 입력 흐름
scripts/dev/native-app.sh tap 200 865         # 입력창 포커스
scripts/dev/native-app.sh type "테스트"
scripts/dev/native-app.sh shot typed          # 입력 반영 확인
scripts/dev/native-app.sh stop
```
> **좌표 = 스크린샷 픽셀 그대로.** phone = **412×915**(갤럭시 S26 dp). Linux Compose는 density 1이라 px == dp == xdotool 좌표. 매 단계 `shot` → Read로 다음 좌표를 잡는다(앱은 매번 같은 자리에 그려진다).

## 환경 / 프로파일

| 변수 | 기본 | 용도 |
|---|---|---|
| profile 인자 | `phone`(412×915) | `desktop`(1280×800)도 가능 |
| `NATIVE_W` / `NATIVE_H` | 프로파일값 | 더 큰 프레임(예: `NATIVE_W=480 NATIVE_H=1040`) |
| `DENEB_GATEWAY_URL` | `http://100.105.145.6:18789` | 다른 게이트웨이로 시드 (dev 게이트웨이 연결은 ↓ 전용 섹션 참조) |
| `DENEB_INSTANCE` | worktree 이름 | **인스턴스 격리 키** — 디스플레이/상태디렉토리/VNC포트가 이 값의 해시 오프셋으로 분리되어, 동시에 도는 다른 에이전트 worktree의 앱을 서로 죽이거나 잘못된 화면을 캡처하지 않는다 |
| `NATIVE_DISPLAY` | `:99`+오프셋 | Xvfb 디스플레이 (인스턴스별 자동 산정; 직접 지정 시 우선) |
| `NATIVE_WM` | `1` | `0`이면 WM 끔(키보드 포커스 불안정 — 비권장) |
| `NATIVE_APP_XMX` | `1024m` | 앱 JVM 힙 캡 |

- **프로덕션 연결**(실데이터). 메일/일정/세션이 진짜로 보이고, **채팅을 보내면 실제 에이전트 턴이 돈다** — 입력 메커니즘만 볼 땐 Enter/전송 누르지 말 것.

## dev 게이트웨이 연결 (수정 빌드를 prod 배포 없이 검증)

기본은 prod(18789) 연결이지만, `scripts/dev/live-test.sh` 가 띄운 dev 게이트웨이에 붙이면 **로컬 수정 빌드를 배포 전에** native-app e2e 로 돌릴 수 있다.

```bash
# 1) dev 게이트웨이 기동 (포트 충돌 피하려 worktree별 인스턴스 격리)
export DENEB_INSTANCE="$(basename "$PWD")"
scripts/dev/live-test.sh restart
scripts/dev/live-test.sh status        # ← "port NNNNN" 확인 (기본 인스턴스=18790, 명명 인스턴스는 해시 포트)

# 2) 그 포트로 native-app 시드 + 기동
DENEB_GATEWAY_URL=http://127.0.0.1:<dev-port> scripts/dev/native-app.sh start
scripts/dev/native-app.sh shot home    # 홈 데이터가 차 있으면 인증 통과
```

- **client-token 인증은 이제 자동 시드된다.** dev 게이트웨이는 `DENEB_STATE_DIR=/tmp/deneb…-dev-state`(≠ `~/.deneb`)를 쓰므로, 예전엔 그 state dir 에 `client_token` 이 없어 `clientauth` 가 꺼진 채 모든 `miniapp.*` RPC 를 401("missing/invalid client token")로 막았다 → **홈 빈 화면 + 채팅 "게이트웨이 오류"**. `lib-server.sh:devlib_seed_client_token` 가 기동 시 **prod `~/.deneb/client_token` 을 dev state dir 로 미러링**(prod 회전 시 갱신)하므로, native-app.sh 가 앱(`~/.deneb-client`)에 시드하는 토큰과 같은 값이라 그대로 통과한다. 예전 수동 우회(`cp ~/.deneb/client_token /tmp/…-dev-state/`)는 더 이상 필요 없다.
- **재시작 불필요**: 서버 `clientauth.Verify` 가 토큰 파일을 매 요청 새로 읽으므로 시드만 돼 있으면 되고, `live-test.sh` 는 기동 **전에** 시드하니 신경 쓸 것 없다.
- **opt-in 전제**: 미러링은 prod 에 `~/.deneb/client_token` 이 있을 때만 동작한다(없으면 `go run ./cmd/deneb-client-token` 으로 1회 생성). 단일 사용자 host 라 dev state dir(`/tmp`)에 토큰이 떨어지는 건 이미 거기 있는 dev config(프로바이더 키 포함)와 동일한 보안 경계.
- **앱 설정은 host 전역**: native-app.sh 의 `~/.deneb-client` 시드는 인스턴스 격리(디스플레이/state/포트)와 달리 **host 단일**이라, 한 host 에서 prod·dev 두 게이트웨이로 동시에 두 앱을 띄울 수는 없다(마지막 `start`/`seed` 가 `~/.deneb-client` 를 덮어씀). prod↔dev 전환은 순차로.
- 검증 후엔 `scripts/dev/native-app.sh stop` + `scripts/dev/live-test.sh stop` 로 정리.

## 동작 원리 (앱 코드 무수정)

- **Xvfb**(가상 디스플레이) + **matchbox-window-manager**(데코·툴바 없는 단일창 키오스크) + **Compose Desktop 앱** + **scrot** 캡처 + **xdotool** 입력. 선택적으로 x11vnc+noVNC.
- **게이트웨이 자동연결**: 앱의 암호화 설정(`~/.deneb-client/settings.aes`, `EncryptedFileSettings.kt`와 AES-256-GCM byte 호환)에 `deneb.gatewayUrl`/`deneb.clientToken`을 직접 시드. 토큰은 `~/.deneb/client_token`(프로덕션)에서 읽음. **앱 소스는 건드리지 않는다.**

## 트러블슈팅 (증상 → 원인 → 해결, 전부 직접 디버깅으로 확립)

| 증상 | 원인 | 해결 |
|---|---|---|
| `UnsatisfiedLinkError: libawt_xawt.so` | 헤드리스 JRE엔 GUI 라이브러리 없음 | `sudo apt-get install -y openjdk-21-jre` (헤드풀, 같은 경로에 채워짐) |
| `HeadlessException` (창 생성 시) | Gradle 빌드 JVM이 headless, fork된 앱이 상속 | `-Djava.awt.headless=false` (스크립트가 이미 부여) |
| 앱이 **Koin 직후 silent death** | `gradle run`은 클라이언트 죽으면 앱 죽임 → `start` 중단 시 연쇄 | `setsid`로 detach (스크립트 적용됨) |
| **타이핑이 안 들어감**(필드 포커스는 됨) | WM 없으면 X포커스↔Compose필드포커스 어긋남 | matchbox WM 필수(스크립트가 기동). `ensure_focus`가 이미 포커스면 windowfocus 생략 |
| 기동 실패 `errno=12 ENOMEM` | strict overcommit(`vm.overcommit_memory=2`), 앱 기본힙 32GB | `-Xmx1024m` 캡(적용됨). 데몬 죽이지 말 것(`/proc/meminfo` 헤드룸 확인) |
| 창이 1280×800에 멈춤 | Compose가 첫 컴포지션에 WindowState 재적용 | `force_geometry` 재확인 루프(적용됨). `start` 재실행으로 self-heal |
| 스크립트가 조용히 죽음 | `set -e`+`pipefail`에서 `xdotool/pgrep` no-match exit1이 `x="$(…)"` 할당을 즉사 | 헬퍼에 `\|\| true` 필수(`app_wid`/`xvfb_pid`/`wm_pid` 적용됨) |
| 첫 화면이 검은 띠/토글 누락 | shot이 정착 직전 transient | 잠깐 뒤 다시 `shot`, 또는 `start` 재실행(geometry 재적용) |
| 검정 화면만 | GL 없는 Xvfb에 하드웨어 렌더 시도 | `-Dskiko.renderApi=SOFTWARE`(적용됨) |

> **종료는 PID 기반**(`app_jvm.pid`는 창에서 `getwindowpid`로). `pkill -f <패턴>`은 그 문자열을 argv에 담은 셸까지 죽인다(셸 자살) — 스크립트는 절대 쓰지 않는다.

## 일회성 셋업 (이미 완료됨 — 새 머신에서만)

```bash
sudo apt-get install -y xvfb x11vnc novnc websockify matchbox-window-manager \
  fluxbox xdotool scrot x11-utils openjdk-21-jre
# ANDROID_HOME=~/android-sdk, python3 cryptography (시드용) 필요
```

## 배포 전 스모크 (`native-app-smoke.sh`)

`scripts/dev/native-app-smoke.sh` 가 위 하네스를 자동으로 몰아 **핵심 화면을 한 바퀴** 돈다 — 채팅(업무 피드) → 메일 → 일정 → 검색 → 사람 → 카테고리 → 설정 4탭 → 세션 드로어 → **메일 상세**(13개) + **리스트 4종 스크롤 프로브**(work-feed·메일·사람·카테고리). `compileKotlinDesktop`·단위테스트가 못 잡는 **런타임 크래시**(예: 158/#1959 의 LazyColumn 중복키 `IllegalArgumentException` — 실데이터 렌더 때만 터짐)를 APK 게시 전에 차단하는 **수동 게이트**.

- **prod 데이터라 픽셀-골든 비교 안 함.** 화면마다 ①그 화면이 렌더되는 동안 앱 로그에 새 예외/크래시 라인(`Exception`/`Caused by:`/`already used`/`*Exception` …)이 없고 ②앱 JVM(`app_jvm.pid`)이 살아있고 ③**그 화면의 앵커 텍스트가 OCR로 실제 보이는지**(`native-app.sh assert` — wrong-screen/blank-render 차단; 크래시 없는 **nav 실패**까지 잡는다)를 검사. 스크린샷은 `shots/smoke-*.png` 로 보관(Read 로 육안 확인).
- **읽기 전용**: tap + Escape 로만 이동, 전송/입력/액션 버튼 안 누름 → prod 게이트웨이에 안전.
- **스크롤 프로브**(`scroll_probe`): 리스트 화면은 최상단 1뷰포트만 검사하면 #1959 같은 **below-the-fold 항목**의 렌더 크래시를 놓친다. work-feed·메일·사람·카테고리는 `scroll down` 3회로 하단 항목을 컴포즈시키며 매 스텝 로그·생존을 재검사(스크롤은 content를 옮겨 앵커가 흔들리므로 크래시/생존만, `*-scrolled.png` 로 보관). 리스트 "load more" 는 GET 이라 읽기 전용 원칙 유지.
- **네비는 텍스트 주도**(`taptext`): 드로어 항목·설정 탭은 라벨로 탭하고 `wait-for` 로 정착을 기다린 뒤 앵커를 `assert` → 레이아웃이 흔들려도 안 깨진다. 콜드 첫 탭이 애니메이션 중 발사돼 빗나가면 **앵커 미도달 시 1회 재탭**(`retry_nav`)으로 자가치유. 아이콘 전용 컨트롤(햄버거 `25,37`·세션 `388,37`·데이터 의존 메일 행 `200,185`)만 픽셀 탭으로 남긴다.
- **앵커 없는 화면**: 크론·토픽문서·알림은 고유 OCR 앵커가 없어(특히 알림의 "…알림 캡처를 지원하지 않습니다"는 `캡처를→BMS`로 오독, 유일 가독어 "알림"은 게이트웨이 탭에도 존재) **크래시/생존만** 검사. 앵커 후보는 반드시 라이브 `assert`로 가독성·고유성을 먼저 확인하고 추가.
- alive 판정은 `status`(매번 윈도우 재탐색이라 tap 직후 flaky) 말고 `app_jvm.pid` `kill -0`.
- **게시 게이트(자동 강제)**: `publish-apk.sh` 가 빌드 전에 이 스모크를 **자동 실행**한다 — 크래시/wrong-screen 감지 시 publish 중단(`smoke-*.png` 보고 수정), 하네스 기동 불가 시 warn+continue(인프라 갭은 코드 결함 아님), `DENEB_SKIP_SMOKE=1` 로 우회. 게이트와 무관히 수동 단독 실행도 가능(`scripts/dev/native-app-smoke.sh`). 158/#1959 가 게시된 건 이 게이트가 **문서에만 있고 강제되지 않아서**였다.

## 한계 / 주의

- **시스템 제스처**(엣지 스와이프 등)는 재현 불가 — 실기기 필요. 관련: [[reference_native_client_build_verify]], [[reference_native_nested_drawer_gesture]].
- 빌드가 매번 `client-android/app/iosApp/Configuration/Config.xcconfig`(APP_VERSION) 재생성 → **커밋 전 `git checkout --`로 원복**.
- 단일 사용자·단일 머신 전용(gx10). 디스플레이 `:99`, noVNC 포트 6080은 Tailnet 한정.
