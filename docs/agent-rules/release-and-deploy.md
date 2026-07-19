---
description: "릴리스, 어드바이저리, 프로덕션 배포 워크플로우"
globs: ["scripts/deploy*", "scripts/dev/publish-apk.sh", "client-android/app/androidApp/build.gradle.kts", "client-android/app/composeApp/build.gradle.kts", ".github/workflows/release*", ".github/workflows/publish-apk.yml"]
---

# Release & Advisory Workflows

- Release and publish remain explicit-approval actions.

# Production Deployment

## 자동 배포 (srv4-로컬 — 기본 경로)

> **2026-07-06 srv4 통일**: 게이트웨이 호스트(srv4)가 직접 pull→빌드→핫스왑한다.
> 옛 srv1 원격 빌드·배송 오케스트레이터는 은퇴했다 (srv1 유닛은 `.bak` 보존).

- 머지 → srv4 의 systemd **user** 타이머 `deneb-auto-deploy.timer`(1분 간격,
  quiet 300초 — 연속 머지는 정착 후 1회 배포)가 `scripts/deploy/auto-deploy.sh`
  를 실행: origin/main 새 head 감지 시 `make gateway-prod` 빌드 후
  `deneb-gateway.service` 를 SIGUSR1 **핫스왑**. 로그: srv4 `/tmp/deneb-auto-deploy.log`.
- **턴-인지 idle 게이트**: 스왑 직전 `scripts/deploy/wait-idle.sh`가 `/health`의
  `activity.active_turns`가 0이 될 때까지 대기(기본 420초 — 5분 턴 데드라인 초과,
  `DENEB_DEPLOY_IDLE_WAIT_SEC`, 0=비활성; 타임아웃 시 그대로 진행 — 배포를 영구
  차단하지 않음). 활성 턴 중 스왑하면 graceful drain 전체가 신규 요청 정전이
  되므로, idle에서 스왑해 정전 창을 부팅 시간(~수 초)으로 줄인다.
- **HTTP 소켓 액티베이션** (opt-in, `scripts/systemd/setup-http-socket.sh`):
  `deneb-http.socket`(FileDescriptorName=http)이 게이트웨이 HTTP 소켓을 systemd
  소유로 옮겨 핫스왑 중 신규 연결이 거부 대신 커널 백로그에 대기 — idle 게이트와
  합쳐 배포 정전 창이 사실상 0. sd_listen_fds 처리는 `infra/sdsocket`(LMTP와 공유,
  activation env 1회 캡처라 두 소비자가 서로의 env를 지우지 못함). 유닛 없으면
  게이트웨이가 스스로 바인드(무변화). 롤백은 setup 스크립트 말미 출력 참조.
- Go 툴체인은 srv4 **유저 공간** `~/go-sdk/go` (sudo 없이 tarball 설치 — 이 호스트는
  passwordless sudo 가 없다; 서비스 유닛 PATH 에 반영). `loginctl enable-linger`
  활성 상태라 세션이 없어도 user 유닛이 상주한다.
- 일시 정지: srv4 에 `~/.deneb/auto-deploy.paused` 파일 생성(PAUSE_FILE), 재개는 삭제.
- 상태 확인: `systemctl --user list-timers | grep deneb` · `tail /tmp/deneb-auto-deploy.log`.
- **토폴로지 전반 실측**: `scripts/audit/topology-parity.sh` (srv4 에서 실행) — 이 문서와 sidecar-models.md 의 검증 가능한 주장을 실측 대조. 주간 자동(`topology-parity.yml`, 불일치 시 `doc-drift` 이슈) + 수동 실행 가능. 인프라를 손으로 바꿨다면 이걸 돌려 문서와 화해시킬 것.
- 스크립트는 **항상 exit 0** (실패는 로그로만) — 빨간 unit 상태를 보고 타이머를 꺼버리는
  사고 방지. 같은 커밋 재시도는 600초 스로틀.

## 배포 롤백 워치 (deploy-watch)

- `auto-deploy.sh`가 deploy OK 직후 `scripts/deploy/deploy-watch.sh`를 백그라운드로
  발사하고, 워치가 잠금을 인계받아 **동일 head를 감시 중이라고 확인할 때까지**
  배포를 검증 완료로 보지 않는다. 스크립트 부재·인계 시간초과는
  `~/.deneb/auto-deploy.unverified-head`에 남고 다음 no-op tick이 재시도한다.
  워치는 기본 600초 동안
  30초 간격으로 `/health`와 게이트웨이 저널 ERROR 수를 감시하고, health 연속 2회
  실패 또는 ERROR 예산(기본 30) 초과 시 **직전 바이너리(`dist/deneb-gateway.bak-prev`)
  복원 + MainPID `kill -TERM`**(systemd `Restart=always`가 복원 바이너리를 재기동)으로
  자동 롤백한다 (git 무접촉; 유닛이 `RefuseManualStop=yes`라 `systemctl restart`는
  거부됨 — 2026-07-12 실측; 다운그레이드 가드는 SIGUSR1 경로에만 있어 hard restart가
  복원 바이너리를 부팅).
- 롤백된 head는 `~/.deneb/auto-deploy.regressed-head`에 기록되어 **더 새로운
  커밋이 main에 landing될 때까지** 재배포가 차단된다. 로그: `/tmp/deneb-deploy-watch.log`.
- env: `DENEB_DEPLOY_WATCH_SEC`·`_POLL_SEC`·`_HANDOFF_SEC`·`_ERROR_BUDGET`,
  자동 배포의 확인 제한은 `DENEB_DEPLOY_WATCH_START_SEC`. 이 워치가 RSI L4
  소스 자가편집의 auto-apply 전제 안전망이다 (docs/research 로드맵 L4 절).

## 코딩 디스패치 (RSI L4 실행 레인 — 오퍼레이터 활성 필요)

- `scripts/dev/coding-dispatch.sh`: 자기교정 큐(`~/.deneb/data/self_correction_candidates.jsonl`)의
  미배차 소스 후보(scope=code, 증거 기반 Source, 리뷰 승인 accepted 우선 → proposed) 1건을 골라 프로덕션
  클론의 **시도별 고유 브랜치·워크트리**에서 로그인된 **Codex CLI 헤드리스**
  (`codex exec`, `DENEB_DISPATCH_CODEX_BIN`으로 경로 재정의)로
  구현을 배차한다. 세션 프롬프트가 CLAUDE.md 게이트 준수 + 그린 시 `pr.sh land`
  랜딩까지 지시하며, 배차 마커(`~/.deneb/data/coding_dispatch/<id>.json`)와 일일
  상한(`DENEB_DISPATCH_DAILY_CAP`, 기본 2)이 재배차·토큰 예산을 통제한다.
  systemd의 제한된 PATH에서는 `DENEB_DISPATCH_GH_BIN`으로 GitHub CLI 경로도
  명시해, squash merge 결과가 L4 결과 장부에 `landed`로 기록되게 한다.
- **상설화(타이머)는 자동 설치하지 않는다** — 무인 자율 코딩 루프의 스위치는
  오퍼레이터가 직접 켠다. 수동 1회 실행: `scripts/dev/coding-dispatch.sh`.
  상설 활성(오퍼레이터, srv4에서 1회):

```bash
mkdir -p ~/.config/systemd/user
cat > ~/.config/systemd/user/deneb-coding-dispatch.service <<'UNIT'
[Unit]
Description=Deneb RSI L4 coding dispatch (self-correction queue -> Codex headless)
[Service]
Type=oneshot
Environment=DENEB_DISPATCH_CODEX_BIN=%h/.local/bin/codex
Environment=DENEB_DISPATCH_GH_BIN=%h/.local/bin/gh
ExecStart=%h/deneb/scripts/dev/coding-dispatch.sh
UNIT
cat > ~/.config/systemd/user/deneb-coding-dispatch.timer <<'UNIT'
[Unit]
Description=Run Deneb coding dispatch every 2 hours
[Timer]
OnBootSec=10min
# OnActiveSec re-arms the schedule when the timer unit itself is restarted
# (daemon-reload + restart with the service inactive and boot long past leaves
# OnUnitActiveSec with nothing to anchor to — observed 2026-07-18: the lane
# silently stopped with "Trigger: n/a" until manually restarted).
OnActiveSec=10min
OnUnitActiveSec=2h
Persistent=true
[Install]
WantedBy=timers.target
UNIT
systemctl --user daemon-reload && systemctl --user enable --now deneb-coding-dispatch.timer
```

## 수동 배포 (폴백)

- `make gateway-prod` — 프로덕션 바이너리 (`dist/deneb-gateway`).
- 재시작: `scripts/deploy/deploy.sh` (systemd 감지 시 SIGUSR1 핫스왑) 또는
  `systemctl --user restart deneb-gateway`.
- 검증: `curl -s localhost:18789/health`, `journalctl --user -u deneb-gateway -n 120`.

## Native Client APK Publishing

> 여러 에이전트 worktree가 공유 serve dir(`~/.cache/deneb-apk`, http.server `:19010`)에 동시 배포한다. 충돌을 막는 장치가 코드에 있으니 **반드시 단일 스크립트로만 배포**한다.

- **APK 배포는 `scripts/dev/publish-apk.sh` 경유만.** 직접 `assembleFossDebug` + `cp` + 수동 `version.json` 작성 금지 — 동시 빌드가 같은 파일명을 서로 덮어쓴다 (실제로 두 세션의 155 빌드가 충돌한 이력).
- APK 파일명 = **versionCode + 커밋 해시** (`deneb-<code>-<sha>-<variant>.apk`, `androidApp/build.gradle.kts`). semantic versionName(2.9.x)은 제거됨 — 빌드는 code 로만 식별. 다른 커밋 빌드는 안 덮어쓰고 전부 보존된다.
- 스크립트가 빌드 + serve dir 복사 + `version.json`(실제 산출물의 code/url/notes) 생성을 한 번에 한다.
- **fossRelease 는 R8 매핑도 함께 게시** — `deneb-<code>-<sha>-fossRelease.mapping.prt` (AGP 9.2 파티션 포맷 `.prt`). 크래시 스택 리트레이스: `java -cp $ANDROID_HOME/build-tools/<최신>/lib/d8.jar com.android.tools.r8.retrace.Retrace --partition-map <mapping.prt> <stack>` — cmdline-tools 의 구 retrace(8.2.33)는 `.prt` 를 파싱하지 못한다. 매핑 부재 시 경고만 하고 게시는 계속된다. (근거: 2026-07-12 크래시 트리아지 — 빌드 609/611/614 는 매핑이 어디에도 없어 커밋별 `git archive` + `minifyFossReleaseWithR8` 재빌드로 재생성해야 했다.)
- **빌드 전 스모크 게이트(자동)**: 빌드에 들어가기 전에 `native-app-smoke.sh`(라이브 화면 워크)를 돌려 런타임 렌더 크래시(#1959류)를 막는다. 크래시/wrong-screen 감지 시 publish 중단, 하네스 기동 불가 시 warn+continue, `DENEB_SKIP_SMOKE=1` 로 우회. 상세: `docs/agent-rules/native-live-app.md`.
- env: `DENEB_APK_DIR`(기본 `~/.cache/deneb-apk`), `DENEB_APK_BASE_URL`(기본 localhost — 배포 머신에서 tailnet URL로 export), `DENEB_APK_VARIANT`(기본 **fossRelease** — R8 프로덕션 빌드 `packageFossReleaseUniversalApk`; debug 변형은 opt-in), `ANDROID_HOME`, `DENEB_SKIP_SMOKE`(스모크 게이트 우회), `DENEB_APK_SIGNING_ENV`(release 서명 env 파일, 기본 `~/.deneb/apk-signing.env` — ↓ "APK release 서명").
- **versionCode는 수동 bump 불필요** — `publish-apk.sh`가 게시 시 자동 할당한다. flock으로 직렬화한 채 공유 serve dir의 최대 code + 1(libs 값을 바닥으로)을 골라 `-PdenebVersionCode`로 gradle 두 모듈(androidApp `versionCode` + composeApp `Version`)에 주입하므로, 동시에 게시하는 두 worktree가 같은 code를 잡는 사고(155/162/164 충돌)가 구조적으로 불가능하다. 이 code는 APK versionCode·파일명·`DenebUpdate.kt`의 `DENEB_VERSION_CODE`(= 생성된 `Version.appVersionCode`)·설정 "빌드 N" 표시에 모두 일관 반영된다.
- ★**semantic versionName(appVersion)은 제거됨 — versionCode 단독 식별 (versionCode-only).** 릴리스마다 손댈 버전 파일이 없다: `publish-apk.sh`만 돌리면 끝. 인앱 업데이트는 code의 strictly-greater 비교라 versionName이 애초에 식별에 불필요했고, 수동 관리 versionName이 위장·중복·패치노트 미동기 버그의 공통 뿌리였어서 통째 제거했다. `libs.versions.toml`의 `android-versionCode`는 floor/IDE 기본값일 뿐. 표시·로그·User-Agent·파일명 모두 "빌드 N"(code).

```bash
# 새 빌드 배포 (배포 머신)
DENEB_APK_BASE_URL=http://<gateway-host>:19010 \
  scripts/dev/publish-apk.sh "인앱 업데이트에 표시될 릴리스 노트"
```

### APK release 서명 (1회 셋업, 운영자)

> fossRelease 는 `KEYSTORE_FILE` env 미설정 시 **debug keystore 폴백 서명**된다
> (`androidApp/build.gradle.kts` signingConfigs). 사이드로드 + 이 앱의 권한 조합
> (SMS·알림 접근·주소록·백그라운드 위치)에서 debug 서명은 핀테크 악성 앱 검사
> (토스가 실제로 플래그한 이력, 2026-07)에 걸리고, debug key 는 머신 로컬이라
> 러너 홈 초기화 = 기존 설치 전부 OTA 단절이다. `publish-apk.sh` 가
> `~/.deneb/apk-signing.env` 를 발견하면 자동으로 release 서명하고, 파일이
> 깨져 있으면(키 경로/비번/alias 누락) **hard fail**, ★**부재 시에도 hard fail**
> 한다(2026-07-06 강화 — 의도적 debug 서명은 `DENEB_ALLOW_DEBUG_SIGNING=1`).
> 근거: srv1→srv4 러너 이사 때 서명 재료가 안 따라가 구 warn-폴백 분기가
> debug-서명 578~580 을 조용히 발행 → 폰이 release-서명 설치 위에 거부
> ("앱이 설치되지 않음"). ★**러너를 다른 호스트로 옮기면
> `~/.deneb/apk-signing.env` + `~/.deneb/keys/`(keystore·cert 해시)를 반드시
> 함께 복사**한다 (`scp -p`, 600/700 권한 유지).

빌드 러너(srv4, publish-apk 실행 계정)에서 1회:

```bash
mkdir -p ~/.deneb/keys && chmod 700 ~/.deneb/keys
keytool -genkeypair -v -keystore ~/.deneb/keys/deneb-release.p12 -storetype PKCS12 \
  -alias deneb -keyalg RSA -keysize 4096 -validity 10950 \
  -dname "CN=Deneb, O=Deneb" \
  -storepass '<강한 비밀번호>' -keypass '<강한 비밀번호>'   # 반드시 같은 값 (gradle keyPassword=KEYSTORE_PASSWORD)
cat > ~/.deneb/apk-signing.env <<EOF
KEYSTORE_FILE=$HOME/.deneb/keys/deneb-release.p12
KEYSTORE_PASSWORD='<강한 비밀번호>'
KEY_ALIAS=deneb
EOF
chmod 600 ~/.deneb/apk-signing.env
# 서명 인증서 SHA-256 (토스 등 오탐 신고에 제출할 고정 해시)
keytool -exportcert -keystore ~/.deneb/keys/deneb-release.p12 -alias deneb \
  -storepass '<강한 비밀번호>' | sha256sum
```

- gradle 이 `keyPassword = KEYSTORE_PASSWORD` 로 스토어/키에 같은 비번을 쓰므로
  PKCS12(단일 비번)와 정합 — 별도 keypass 를 만들지 말 것.
- ★ **env 파일은 shell 로 source 된다** (`publish-apk.sh`) — 비밀번호 값은 위처럼
  **작은따옴표로 감싼다** (`$`·공백·백틱·`#` 등 메타문자가 깨지는 것 방지). 대신
  비밀번호 자체에 작은따옴표(`'`)는 넣지 말 것. heredoc 도 `$` 를 확장하므로
  비밀번호에 `$` 가 있으면 heredoc 대신 에디터로 직접 작성한다.
- ★ **서명 전환 발행은 인플레이스 업데이트 불가**: 기존 debug-서명 설치 위에
  설치가 거부된다(서명 불일치). 첫 release-서명 발행의 릴리스 노트에 "기존 앱
  삭제 후 재설치 필요"를 명시하고, 폰에서 1회 수동 재설치한다. 이후 발행부터는
  평소처럼 인앱 OTA.
- ★ **keystore 는 오프사이트 백업 스코프 밖** — memory-backup(위키·일기·transcripts…)에
  포함되지 않는다. `~/.deneb/keys/deneb-release.p12` + 비밀번호를 별도 보관
  (분실 = 또 한 번의 전체 재설치 + 오탐 신고 해시 재제출).
- 토스 오탐 대응: 전환 후에도 플래그가 남으면 토스 고객센터에 패키지명(`ai.deneb`) +
  위 SHA-256 으로 오탐 신고. 서명이 고정돼야 신고/화이트리스트가 성립한다.

## Automated OTA publish (GitHub Action)

> `.github/workflows/publish-apk.yml` 는 위 `publish-apk.sh` 를 **srv4 self-hosted 러너(`srv4-apk`)**에서 실행하는 얇은 트리거다 (빌드·flock versionCode 모두 수동 배포와 동일). **2026-07-06 개발/배포 srv4 통일**: 러너가 게이트웨이 호스트에 살므로 러너 로컬 `~/.cache/deneb-apk` 가 곧 OTA serve dir 다 — 옛 srv1 러너 시절의 ssh/rsync 동기화 스텝은 self-skip 가드(hostname=srv4)로 남겨뒀다(러너가 다시 오프호스트로 갈 때만 발동). 그 분리 시절의 함정: 러너(srv1)와 게이트웨이(srv4)의 serve dir 가 갈라져 **빌드 563~574 가 폰에 배달되지 않는 split-brain** 이 실제로 발생했었다(2026-07-06 발견·백필로 복구). **단 CI 는 native-app 스모크 게이트를 건너뛴다**(`DENEB_SKIP_SMOKE=1`) — 무인 환경에서 픽셀탭·실데이터 의존 스모크가 잘 튀어(실제로 메일상세 단계가 세션 드로어로 빠져 멀쩡한 빌드를 막은 이력) 자동발행을 망가뜨리기 때문. 렌더 크래시 방어는 네이티브 PR 의 사전 스모크·`renderPreviews`·컴파일/유닛테스트가 맡는다.

- **트리거**: main 에 `client-android/**` 변경이 머지될 때 자동. versionCode 단독화라 릴리스마다 바뀌는 버전 파일이 없어 게이트할 대상이 없다 — **네이티브 변경을 머지하는 것 자체가 새 빌드 발행 신호(연속 배포)**. 수동 `workflow_dispatch`(노트 입력)도 가능. **fork PR 로는 절대 안 돈다** (호스트 러너에서 미신뢰 코드 실행 차단).
- **노트**: dispatch 입력 우선, 없으면 head 커밋 제목. 사용자에게 보이는 정돈된 한국어 changelog 는 어차피 컴파일된 `DenebPatchNotes` 가 오프라인으로 보여주므로 version.json 노트는 보조다.
- **버전 bump 불필요**: versionName 제거로 릴리스 시 손댈 버전 파일이 사라졌다. `DenebPatchNotes` 는 version 라벨 없는 시간순 changelog 라 빌드마다 강제 갱신 불필요(테스트는 비어있지 않으면 통과) — **사용자 영향 변경 때만** 노트를 추가. *"release/publish 는 명시 승인"* 원칙은 "네이티브 변경 PR 을 머지하는 행위" 가 그 승인.
- **★ 패치노트는 조각파일로 (충돌 방지)**: 사용자 표시 노트는 `DenebPatchNotes.kt` 의 frozen 히스토리에 prepend하지 **말고**, `client-android/app/changelog.d/YYYY-MM-DD-<slug>.md` 조각파일을 **새로 추가**한다(PR마다 새 파일 → 같은 줄 안 건드림 → 머지 충돌 0). 빌드 시 `composeApp/build.gradle.kts` 가 조각들을 파일명 역순(최신 먼저)으로 모아 `build/generated/.../DenebChangelogGenerated.kt`(`GENERATED_CHANGELOG_FRAGMENTS`)로 생성하고(**커밋 안 함** — 커밋하면 그 파일이 다시 공유 prepend 파일이 됨), `DENEB_PATCH_NOTES = GENERATED_CHANGELOG_FRAGMENTS + frozen 히스토리`로 합쳐 버전 카드가 그대로 읽는다. 한 줄=하이라이트 1개, `#` 줄=주석. 상세=`changelog.d/README.md`. 내부/빌드/테스트-only 변경엔 조각 불필요. ★이 규약은 **PR 게이트로 강제**된다: `.github/workflows/patch-notes-gate.yml` 이 `feat(native|miniapp|calendar|markdown|chat)` 제목 + `client-android/` 변경 PR 에서 조각(또는 히스토리 편집) 부재 시 실패한다 — 사용자 영향 없는 예외는 `skip-patch-notes` 라벨로 우회.
- **교착 방지**: `publish-apk.sh` 의 flock 은 `-w 600`(10분 대기 상한). 이전 발행이 hang 채 락을 쥐면 후속(자동·수동)이 job 30분 timeout 까지 막히던 사고(2026-06-08 좀비 publish 가 serve-dir 락 점유 → CI 포함 전 발행 블록)를 막아 빠르게 실패한다. 락 점유 의심 시 `fuser ~/.cache/deneb-apk/.publish.lock` 로 holder 확인 후 정리.

### srv4 self-hosted 러너 1회 셋업 (운영자만)

워크플로가 `runs-on: [self-hosted, srv4]` 이라 srv4 에 러너가 등록돼야 동작한다. 러너가 게이트웨이와 같은 호스트라 `~/.cache/deneb-apk` 가 **곧 serve dir** — 별도 동기화 불필요. 현역 러너: `srv4-apk` (2026-07-06 등록, srv1 의 구 `gx10-apk` 는 해제).

```bash
# srv4 에서 choiceoh 로. URL/토큰은 GitHub > Settings > Actions > Runners > New self-hosted runner 에서 복사.
mkdir -p ~/actions-runner-deneb && cd ~/actions-runner-deneb
curl -o runner.tar.gz -L <runner-linux-arm64-tarball-url>
tar xzf runner.tar.gz
./config.sh --url https://github.com/choiceoh/Deneb \
  --token <REG_TOKEN> --labels srv4 --name srv4-apk --unattended
# idempotent append — 재실행해도 중복 없음 (.env 는 config.sh 가 만든 기존 항목 보존)
grep -q '^ANDROID_HOME=' .env 2>/dev/null || echo 'ANDROID_HOME=/home/choiceoh/android-sdk' >> .env
grep -q '^JAVA_HOME='    .env 2>/dev/null || echo 'JAVA_HOME=/usr/lib/jvm/java-21-openjdk-arm64' >> .env  # pragma: allowlist secret
sudo ./svc.sh install choiceoh && sudo ./svc.sh start   # 재부팅 후 자동 상주
```

- 호스트 전제(2026-07-06 충족): `~/android-sdk`(ANDROID_HOME 기본), JDK 21(+**헤드풀 `openjdk-21-jre`** — 하네스/renderPreviews 의 AWT), Xvfb/matchbox 등 스모크 하네스 의존(`native-live-app.md`). ★**ARM64 필수 오버라이드**: `~/.gradle/gradle.properties` 에 `org.gradle.java.home=/usr/lib/jvm/java-21-openjdk-arm64` + `android.aapt2FromMavenOverride=$HOME/android-sdk/build-tools/<버전>/aapt2` — `<버전>` 은 설치된 최신 build-tools (`ls ~/android-sdk/build-tools | sort -V | tail -1`) 를 그대로 적는다(레포는 build-tools 버전을 고정하지 않으므로 SDK 갱신 시 이 경로도 따라 갱신). 구글 메이븐이 내려주는 aapt2 는 x86_64 라 이 오버라이드 없이는 `Syntax error: ")" unexpected` 로 빌드가 죽는다(2026-07-06 이전 검증에서 실측).
- 레포 변수 `DENEB_APK_BASE_URL` 를 게이트웨이 도달 base 로 설정(`Settings > Secrets and variables > Actions > Variables`). 미설정이어도 동작하나 version.json url 이 로컬 기본값이 된다(인앱 업데이터는 게이트웨이 다운로드 라우트로 받으므로 무해).
- 커스텀 라벨 `srv4` 는 `.github/actionlint.yaml` 에 등록돼 있어 워크플로 린트(`workflow-sanity.yml`)를 통과한다.
