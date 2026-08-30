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
| 시맨틱 트리 (비전 불필요) | `scripts/dev/ui-inspect.sh <screen> [actions]` | mock 데이터 화면의 Compose 시맨틱 트리를 **텍스트로** 덤프 + 노드 텍스트로 클릭 구동 — 비전 모델이 아닌 에이전트도 한국어 라벨을 정확히 검증 |
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
| `start [phone\|phone2x\|desktop]` | Xvfb + WM + 게이트웨이 시드 + 앱 기동. idempotent(이미 떠 있으면 지오메트리만 재적용). |
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

> **좌표 = 스크린샷 픽셀 그대로** (모든 프로파일에서). phone = **412×915**(갤럭시 S26 dp) @1x라 거기선 px == dp 이기도 하다. 매 단계 `shot` → Read로 다음 좌표를 잡는다(앱은 매번 같은 자리에 그려진다).

## 환경 / 프로파일

| 변수 | 기본 | 용도 |
|---|---|---|
| profile 인자 | `phone`(412×915 @1x) | `phone2x`(같은 dp, 2배 픽셀 그리드)·`desktop`(1280×800)도 가능. ★**phone=모바일 UI 분기(하단 탭바)·desktop=데스크톱 분기(좌측 레일)** — 창 크기뿐 아니라 실제 플랫폼 분기를 렌더 |
| `NATIVE_W` / `NATIVE_H` | 프로파일값 | 더 큰 프레임(예: `NATIVE_W=480 NATIVE_H=1040`) |
| `DENEB_GATEWAY_URL` | `http://100.111.114.20:18789` (srv4 — 2026-06-20 프로덕션 이사) | 다른 게이트웨이로 시드 (dev 게이트웨이 연결은 ↓ 전용 섹션 참조) |
| `DENEB_INSTANCE` | worktree 이름 | **인스턴스 격리 키** — 디스플레이/상태디렉토리/VNC포트가 이 값의 해시 오프셋으로 분리되어, 동시에 도는 다른 에이전트 worktree의 앱을 서로 죽이거나 잘못된 화면을 캡처하지 않는다 |
| `NATIVE_DISPLAY` | `:99`+오프셋 | Xvfb 디스플레이 (인스턴스별 자동 산정; 직접 지정 시 우선) |
| `NATIVE_WM` | `1` | `0`이면 WM 끔(키보드 포커스 불안정 — 비권장) |
| `NATIVE_APP_XMX` | `1024m` | 앱 JVM 힙 캡 |

- **프로덕션 연결**(실데이터). 메일/일정/세션이 진짜로 보이고, **채팅을 보내면 실제 에이전트 턴이 돈다** — 입력 메커니즘만 볼 땐 Enter/전송 누르지 말 것.
- ★**`phone2x` = 서브-dp 디테일 판정용**(2026-07-26 추가): Skiko는 Xvfb에서 스케일 팩터를 못 읽어 하네스 앱이 **1 px per dp**로 그렸다. 실기기는 ~3x라, 헤어라인·1dp 보더·2dp 캐럿 같은 것들은 1x 스크린샷에서 **반쯤 켜진 픽셀 한 줄**로 뭉개져 판정 자체가 불가능하다(실제로 인라인 코드 칩이 그려졌는지를 눈으로 못 가려 PIL 픽셀 샘플링으로 확인해야 했다). `phone2x`는 dp는 그대로 412×915, 창을 824×1830 **물리 픽셀**로 열고 앱에 `-Ddeneb.ui.scale=2`로 밀도를 알려준다(`desktopMain/kotlin/ai/deneb/main.kt`가 `LocalDensity`를 덮어씀; `fontScale`은 1 유지 — 큰 글씨 설정이 아니라 촘촘한 화면을 흉내내는 것이라). **좌표는 여전히 스크린샷 픽셀** — 스크린샷 px == 물리 px == xdotool 좌표는 어느 프로파일에서나 참이고, 달라지는 건 px↔dp 비율뿐이다. 대신 픽셀이 4배라 스크린샷 토큰도 4배 — **기본은 `phone`을 쓰고, 심미/정밀 검수할 때만 `phone2x`**.
- ★**phone 프로파일 = 실제 모바일 UI**(2026-06-14 추가): 예전엔 항상 `Platform.Desktop`(phone 은 창 크기만)이라 폰 전용 분기를 못 봤는데, 이제 phone 프로파일이 `-Ddeneb.platform=phone` 으로 `currentPlatform=Mobile.Android` 를 강제하고 창을 프로파일 크기로 연다(`-Ddeneb.window.{width,height}` — Compose 가 1280 기본을 재적용해 좁은 레이아웃을 잘라먹던 클립 제거). 그래서 **하단 탭바·모달 드로어 등 폰 전용 분기를 헤드리스로 검증**할 수 있다. desktop 프로파일은 `currentPlatform=Platform.Desktop` 을 세팅하지만, 데스크탑 제품 UI 가 제거돼(모바일 전용, Andromeda 가 데스크탑 소유) **이제 모바일 UI 를 넓은 1280 창에 렌더**한다 — 즉 phone 프로파일이 실제 검증용이고 desktop 은 잔존 빌드 타깃이다. 구현: `Platform.jvm.kt` 가 `deneb.platform` 시스템 프로퍼티를, `desktopMain/kotlin/ai/deneb/main.kt` 가 `deneb.window.*` 를 인식(프로덕션 런치는 미설정). **단 실제 Android 인셋·소프트 키보드·엣지 제스처는 여전히 실기기 필요** — 이건 레이아웃·네비게이션 검증이지 OS 런타임 동작 검증이 아니다.

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
| 스크립트가 조용히 죽음 | `set -e`+`pipefail`에서 `xdotool/pgrep` no-match exit1이 `x="$(…)"` 할당을 즉사 | 헬퍼에 `\|\| true` 필수(`app_wid`/`xvfb_pid`/`wm_pid` 적용됨) <!-- docref:ignore --> |
| 첫 화면이 검은 띠/토글 누락 | shot이 정착 직전 transient | 잠깐 뒤 다시 `shot`, 또는 `start` 재실행(geometry 재적용) |
| 검정 화면만 | GL 없는 Xvfb에 하드웨어 렌더 시도 | `-Dskiko.renderApi=SOFTWARE`(적용됨) |

> **종료는 PID 기반**(`app_jvm.pid`는 창에서 `getwindowpid`로). `pkill -f <패턴>`은 그 문자열을 argv에 담은 셸까지 죽인다(셸 자살) — 스크립트는 절대 쓰지 않는다.

## 일회성 셋업 (srv4 에 2026-07-06 완료 — 새 머신에서만)

```bash
sudo apt-get install -y xvfb x11vnc novnc websockify matchbox-window-manager \
  fluxbox xdotool scrot x11-utils openjdk-21-jre
# ANDROID_HOME=~/android-sdk, python3 cryptography (시드용) 필요
# ★openjdk-21-jre 는 헤드풀 버전이어야 한다 — jdk-headless 만 있으면
#   libawt_xawt.so 부재로 Compose 창 생성이 UnsatisfiedLinkError 로 죽는다.
# ★ARM64 는 ~/.gradle/gradle.properties 의 aapt2FromMavenOverride 필수
#   (release-and-deploy.md 러너 셋업 참조 — 하네스 gradle 빌드도 같은 함정).
# 실행 위치: ~/deneb-dev (개발 체크아웃). ~/deneb(프로덕션)에서 빌드 금지.
```

## 배포 전 스모크 (`native-app-smoke.sh`)

`scripts/dev/native-app-smoke.sh` 가 위 하네스를 **phone 프로파일**로 몰아 **핵심 화면을 한 바퀴** 돈다 — 피드 → 채팅 → 메일 → 달력 → (더보기로) 검색 → 카테고리 → 사람 → 설정(+모델/크론/알림 탭) → 세션 드로어 → **메일 상세**(13개) + **리스트 5종 스크롤 프로브**(피드·메일·검색·카테고리·사람). 데스크탑 제품이 은퇴(Andromeda 가 소유)해 클라가 모바일 전용이므로 스모크도 **실제 모바일 UI**(하단 탭바·더보기 메뉴·햄버거 세션 드로어)를 검증한다. `compileKotlinDesktop`·단위테스트가 못 잡는 **런타임 크래시**(예: 158/#1959 의 LazyColumn 중복키 `IllegalArgumentException` — 실데이터 렌더 때만 터짐)를 APK 게시 전에 차단하는 **수동 게이트**.

- **prod 데이터라 픽셀-골든 비교 안 함.** 화면마다 ①그 화면이 렌더되는 동안 앱 로그에 새 예외/크래시 라인(`Exception`/`Caused by:`/`already used`/`*Exception` …)이 없고 ②앱 JVM(`app_jvm.pid`)이 살아있고 ③**그 화면의 앵커 텍스트가 OCR로 실제 보이는지**(`native-app.sh assert` — wrong-screen/blank-render 차단; 크래시 없는 **nav 실패**까지 잡는다)를 검사. 스크린샷은 `shots/smoke-*.png` 로 보관(Read 로 육안 확인).
- **읽기 전용**: tap + Escape 로만 이동, 전송/입력/액션 버튼 안 누름 → prod 게이트웨이에 안전.
- **스크롤 프로브**(`scroll_probe`): 리스트 화면은 최상단 1뷰포트만 검사하면 #1959 같은 **below-the-fold 항목**의 렌더 크래시를 놓친다. work-feed·메일·사람·카테고리는 `scroll down` 3회로 하단 항목을 컴포즈시키며 매 스텝 로그·생존을 재검사(스크롤은 content를 옮겨 앵커가 흔들리므로 크래시/생존만, `*-scrolled.png` 로 보관). 리스트 "load more" 는 GET 이라 읽기 전용 원칙 유지.
- **네비는 하단 탭바 픽셀 + 더보기 라벨 `taptext` 혼합**: 하단 탭(피드/채팅/메일/달력/더보기)은 라벨이 화면 제목과 충돌해(예: "메일"⊂"받은 메일") **고정 픽셀**(`y=858`, x=37/118/200/282/364)로 탭하고, 더보기 메뉴의 섹션 행·설정 알약 탭은 `taptext`(라벨) 후 `wait-for`→`assert` → 레이아웃이 흔들려도 안 깨진다. 콜드 첫 탭이 빗나가면 **앵커 미도달 시 1회 재탭**(`retry_nav`)으로 자가치유. 아이콘 전용/데이터 의존 컨트롤(햄버거 `25,37`·데이터 의존 메일 행 `200,185`)만 픽셀 탭. 폰엔 데스크탑의 우측 세션 버튼이 없다 — 세션은 햄버거 좌측 드로어로 연다.
- **로드 실패 관용**: 스모크는 **크래시 게이트**라, 앵커가 없어도 화면이 `다시 시도`(prod fetch 실패 시의 graceful degradation)를 깔끔히 렌더했으면 ok 로 본다 — 헤드리스에서 메일·사람·모델 fetch 가 자주 빈손이라 데이터 부재로 게이트를 흔들면 안 된다. 진짜 wrong-screen 은 앵커도 `다시 시도`도 없어 여전히 실패.
- **앵커 없는 화면**: 크론·토픽문서·알림은 고유 OCR 앵커가 없어(특히 알림의 "…알림 캡처를 지원하지 않습니다"는 `캡처를→BMS`로 오독, 유일 가독어 "알림"은 게이트웨이 탭에도 존재) **크래시/생존만** 검사. 앵커 후보는 반드시 라이브 `assert`로 가독성·고유성을 먼저 확인하고 추가.
- alive 판정은 `status`(매번 윈도우 재탐색이라 tap 직후 flaky) 말고 `app_jvm.pid` `kill -0`.
- **게시 게이트(자동 강제)**: `publish-apk.sh` 가 빌드 전에 이 스모크를 **자동 실행**한다 — 크래시/wrong-screen 감지 시 publish 중단(`smoke-*.png` 보고 수정), 하네스 기동 불가 시 warn+continue(인프라 갭은 코드 결함 아님), `DENEB_SKIP_SMOKE=1` 로 우회. 게이트와 무관히 수동 단독 실행도 가능(`scripts/dev/native-app-smoke.sh`). 158/#1959 가 게시된 건 이 게이트가 **문서에만 있고 강제되지 않아서**였다.

## 한계 / 주의

- **시스템 제스처**(엣지 스와이프 등)는 재현 불가 — 실기기 필요. 관련: [[reference_native_client_build_verify]], [[reference_native_nested_drawer_gesture]].
- 빌드가 매번 `client-android/app/iosApp/Configuration/Config.xcconfig`(APP_VERSION) 재생성 → **커밋 전 `git checkout --`로 원복**.
- 단일 사용자·단일 머신 전용(**srv4** — 2026-07-06 개발/배포 통일로 srv1 에서 이전; 실행 체크아웃은 `~/deneb-dev`). 디스플레이 `:99`, noVNC 포트 6080은 Tailnet 한정.

## 모션 검수 (녹화 → 필름스트립)

정지 화면은 움직임을 못 보여주고, **움직임이 "폴리시드"의 절반**이다(ADR 0007이 모션
원리를 미룬 이유가 "아직 아무도 이 앱이 움직이는 걸 본 적이 없다"였다).

```bash
scripts/dev/native-app.sh rec-start nav 60   # 디스플레이 녹화 시작 (60fps)
scripts/dev/native-app.sh tap 200 145        # 평소처럼 조작
scripts/dev/native-app.sh rec-stop 14        # 종료 → mp4 + 14프레임 필름스트립 PNG
```

`rec-stop`은 **mp4와 스트립 PNG를 둘 다** 낸다 — 사람은 영상을 보고, 에이전트는
스트립을 읽는다(에이전트는 영상을 볼 수 없다).

- ★**균등 간격 스트립으로 전환을 판정하지 말 것.** 14프레임/2초 = 0.16s 간격인데
  Compose 전환은 130~300ms라 **한 칸에 통째로 숨는다.** 전환 구간을 찾은 뒤
  `ffmpeg -ss <t> -frames:v 1`로 **33ms 간격 재추출**해야 곡선이 보인다.
- 픽셀 해상도가 홀수면(phone 프로파일 915) H.264가 안 열린다 — `rec-start`가 pad
  필터로 짝수화한다. 이 가드를 지우면 0바이트 mp4가 조용히 나온다.
- ★**전달되는 것과 안 되는 것**: 애니메이션 **시간·곡선·순서**는 공용 Compose 코드라
  그대로 옮겨간다. **프레임 페이싱·잔더링**은 아니다 — Xvfb + SOFTWARE 렌더러다.
  체감 부드러움은 여전히 실기기로 판정한다.

### 눈이 아니라 재는 쪽: `motion-analyze.py`

스트립은 "움직이는가"까지만 답한다. **얼마나 오래·어떤 이징으로·평행이동인가**는
재야 한다:

```bash
scripts/dev/motion-analyze.py <video.mp4> [--strip] [--floor 0.1]
```

전환 구간을 **스스로 찾아** 구간별로 시작 시각·지속(ms)·종류·이징·최대 단일 프레임
비중·정지 프레임 수를 낸다. 손으로 창을 짚어 재추출할 필요가 없다.

- ★**"변화 상자가 움직였다" ≠ 평행이동.** 위→아래 스태거는 변하는 영역이 아래로
  내려가서 slide처럼 보인다 — 첫 판본이 피드 행 확장을 "세로 541px slide"로 오독했다.
  지금은 뒤 프레임을 앞 프레임의 **평행이동으로 재구성**해 보고(설명력 배수), 안 되면
  `stagger`로 부른다. 실제 slide는 설명력 1.5x 이상이 나온다.
- ★**지속 시간은 floor에 민감하다.** 페이드의 끝 프레임은 변화량이 아주 작아 floor를
  올리면 꼬리가 잘린다 — 같은 탭 전환이 floor 0.4에서 150ms, 0.05에서 200ms다.
  숫자를 인용하기 전에 `--floor`로 교차 확인할 것. 리포트가 쓴 floor를 같이 찍는다.
- 정지 프레임 경고는 **애니메이션 끊김이거나 녹화 드롭**이다. Xvfb+SOFTWARE라 이
  경로로는 둘을 구별할 수 없다 — 프레임 페이싱 판정은 실기기 몫이다.

**2026-08-30 첫 측정** (이 경로로 처음 관찰):

| 전환 | 측정값 |
|---|---|
| 하단 탭 전환 | **150ms 페이드**, 이동 10x39px = **측면 이동 없음**, front-loaded(50% 지점 33%). 독트린은 "측면 이동(빠른 페이드)"라고 적었는데 **페이드만** 구현돼 있다 |
| 피드 행 확장 | **617ms stagger**, 평행이동 아님(설명력 1.0x), back-loaded(50% 지점 62%). 컨테이너는 즉시 튀고 글자만 순차로 채워진다 |
