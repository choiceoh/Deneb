---
description: "client-android 실제 앱을 서버에서 라이브로 띄워 보고/조작 검증 (Xvfb+matchbox+Compose Desktop, 프로덕션 연결)"
globs: ["client-android/**", "scripts/dev/native-app.sh"]
---

# Native Live-App Harness (서버에서 실제 앱 검증)

> **`renderPreviews`는 mock 데이터 정적 PNG일 뿐이다.** 실제 동작·실데이터·상호작용을 보려면 진짜 앱을 띄워라. `scripts/dev/native-app.sh`가 **client-android의 Compose Desktop 타깃**(`com.inspiredandroid.kai.MainKt`, commonMain을 Android/iOS와 공유하는 *바로 그 앱*)을 헤드리스 X 디스플레이에서 실행해, 에이전트가 스크린샷으로 보고 tap/type으로 조작하게 한다 — Deneb 네이티브 앱에 한정된 "computer use".

## 언제 무엇을 쓰나

| 검증 깊이 | 도구 | 본다 |
|---|---|---|
| 컴파일만 | `./gradlew :composeApp:compileKotlinDesktop` | 타입/빌드 |
| 컴포저블 외형 | `./gradlew :composeApp:renderPreviews` → `/tmp/deneb-render/*.png` | **mock** 데이터 정적 PNG |
| **실제 앱 라이브** | **`scripts/dev/native-app.sh`** | **프로덕션 실데이터 + 상호작용 + 상태 흐름** |
| 시스템 제스처 | 실기기 (Galaxy S25) | 엣지 스와이프 등 — 하네스로 재현 불가 |

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
| `seed [url] [token]` | `~/.kai` 게이트웨이 설정 재기록(기본: 프로덕션). |
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
> **좌표 = 스크린샷 픽셀 그대로.** phone = **412×915**(갤럭시 S25 dp). Linux Compose는 density 1이라 px == dp == xdotool 좌표. 매 단계 `shot` → Read로 다음 좌표를 잡는다(앱은 매번 같은 자리에 그려진다).

## 환경 / 프로파일

| 변수 | 기본 | 용도 |
|---|---|---|
| profile 인자 | `phone`(412×915) | `desktop`(1280×800)도 가능 |
| `NATIVE_W` / `NATIVE_H` | 프로파일값 | 더 큰 프레임(예: `NATIVE_W=480 NATIVE_H=1040`) |
| `DENEB_GATEWAY_URL` | `http://100.105.145.6:18789` | 다른 게이트웨이로 시드 |
| `DENEB_INSTANCE` | worktree 이름 | **인스턴스 격리 키** — 디스플레이/상태디렉토리/VNC포트가 이 값의 해시 오프셋으로 분리되어, 동시에 도는 다른 에이전트 worktree의 앱을 서로 죽이거나 잘못된 화면을 캡처하지 않는다 |
| `NATIVE_DISPLAY` | `:99`+오프셋 | Xvfb 디스플레이 (인스턴스별 자동 산정; 직접 지정 시 우선) |
| `NATIVE_WM` | `1` | `0`이면 WM 끔(키보드 포커스 불안정 — 비권장) |
| `NATIVE_APP_XMX` | `1024m` | 앱 JVM 힙 캡 |

- **프로덕션 연결**(실데이터). 메일/일정/세션이 진짜로 보이고, **채팅을 보내면 실제 에이전트 턴이 돈다** — 입력 메커니즘만 볼 땐 Enter/전송 누르지 말 것.

## 동작 원리 (앱 코드 무수정)

- **Xvfb**(가상 디스플레이) + **matchbox-window-manager**(데코·툴바 없는 단일창 키오스크) + **Compose Desktop 앱** + **scrot** 캡처 + **xdotool** 입력. 선택적으로 x11vnc+noVNC.
- **게이트웨이 자동연결**: 앱의 암호화 설정(`~/.kai/settings.aes`, `EncryptedFileSettings.kt`와 AES-256-GCM byte 호환)에 `deneb.gatewayUrl`/`deneb.clientToken`을 직접 시드. 토큰은 `~/.deneb/client_token`(프로덕션)에서 읽음. **앱 소스는 건드리지 않는다.**

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

`scripts/dev/native-app-smoke.sh` 가 위 하네스를 자동으로 몰아 **핵심 화면을 한 바퀴** 돈다 — 채팅(업무 피드) → 메일 → 일정 → 검색 → 사람 → 카테고리 → 설정 4탭 → 세션 드로어(12개). `compileKotlinDesktop`·단위테스트가 못 잡는 **런타임 크래시**(예: 158/#1959 의 LazyColumn 중복키 `IllegalArgumentException` — 실데이터 렌더 때만 터짐)를 APK 게시 전에 차단하는 **수동 게이트**.

- **prod 데이터라 픽셀-골든 비교 안 함.** 화면마다 ①그 화면이 렌더되는 동안 앱 로그에 새 예외/크래시 라인(`Exception`/`Caused by:`/`already used`/`*Exception` …)이 없고 ②앱 JVM(`app_jvm.pid`)이 살아있고 ③**그 화면의 앵커 텍스트가 OCR로 실제 보이는지**(`native-app.sh assert` — wrong-screen/blank-render 차단; 크래시 없는 **nav 실패**까지 잡는다)를 검사. 스크린샷은 `shots/smoke-*.png` 로 보관(Read 로 육안 확인).
- **읽기 전용**: tap + Escape 로만 이동, 전송/입력/액션 버튼 안 누름 → prod 게이트웨이에 안전.
- 네비 좌표는 phone(412×915) 픽셀 하드코딩(드로어 항목·상단바). 네비 레이아웃이 바뀌면 재매핑(`start`→`shot`→Read→새 좌표). alive 판정은 `status`(매번 윈도우 재탐색이라 tap 직후 flaky) 말고 `app_jvm.pid` `kill -0`.
- **게시 직전 실행**: `scripts/dev/native-app-smoke.sh` → PASS 면 `publish-apk.sh`, FAIL 이면 해당 `smoke-*.png` 를 Read.

## 한계 / 주의

- **시스템 제스처**(엣지 스와이프 등)는 재현 불가 — 실기기 필요. 관련: [[reference_native_client_build_verify]], [[reference_native_nested_drawer_gesture]].
- 빌드가 매번 `client-android/app/iosApp/Configuration/Config.xcconfig`(APP_VERSION) 재생성 → **커밋 전 `git checkout --`로 원복**.
- 단일 사용자·단일 머신 전용(gx10). 디스플레이 `:99`, noVNC 포트 6080은 Tailnet 한정.
