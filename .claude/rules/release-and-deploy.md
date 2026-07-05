---
description: "릴리스, 어드바이저리, 프로덕션 배포 워크플로우"
globs: ["scripts/deploy*", "scripts/dev/publish-apk.sh", "client-android/app/androidApp/build.gradle.kts", "client-android/app/composeApp/build.gradle.kts", ".github/workflows/release*", ".github/workflows/publish-apk.yml"]
---

# Release & Advisory Workflows

- Release and publish remain explicit-approval actions.

# Production Deployment

## DGX Spark Production Build

- `make gateway-prod` — Full production binary (output: `dist/deneb-gateway`).

## DGX Spark Operations

- Restart gateway: `pkill -9 -f deneb-gateway || true; nohup ./dist/deneb-gateway --bind loopback --port 18789 > /tmp/deneb-gateway.log 2>&1 &`
- Verify: `ss -ltnp | rg 18789`, `tail -n 120 /tmp/deneb-gateway.log`.

## Native Client APK Publishing

> 여러 에이전트 worktree가 공유 serve dir(`~/.cache/deneb-apk`, http.server `:19010`)에 동시 배포한다. 충돌을 막는 장치가 코드에 있으니 **반드시 단일 스크립트로만 배포**한다.

- **APK 배포는 `scripts/dev/publish-apk.sh` 경유만.** 직접 `assembleFossDebug` + `cp` + 수동 `version.json` 작성 금지 — 동시 빌드가 같은 파일명을 서로 덮어쓴다 (실제로 두 세션의 155 빌드가 충돌한 이력).
- APK 파일명 = **versionCode + 커밋 해시** (`deneb-<code>-<sha>-<variant>.apk`, `androidApp/build.gradle.kts`). semantic versionName(2.9.x)은 제거됨 — 빌드는 code 로만 식별. 다른 커밋 빌드는 안 덮어쓰고 전부 보존된다.
- 스크립트가 빌드 + serve dir 복사 + `version.json`(실제 산출물의 code/url/notes) 생성을 한 번에 한다.
- **빌드 전 스모크 게이트(자동)**: 빌드에 들어가기 전에 `native-app-smoke.sh`(라이브 화면 워크)를 돌려 런타임 렌더 크래시(#1959류)를 막는다. 크래시/wrong-screen 감지 시 publish 중단, 하네스 기동 불가 시 warn+continue, `DENEB_SKIP_SMOKE=1` 로 우회. 상세: `.claude/rules/native-live-app.md`.
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
> 깨져 있으면(키 경로/비번/alias 누락) debug 폴백 대신 **hard fail** 한다.

빌드 러너(srv1, publish-apk 실행 계정)에서 1회:

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

> `.github/workflows/publish-apk.yml` 는 위 `publish-apk.sh` 를 **srv1(구 gx10) self-hosted 러너**에서 실행하는 얇은 트리거다 (빌드·flock versionCode 모두 수동 배포와 동일). self-hosted 인 이유는 **Android SDK 가 srv1 에 있어서**다 — 프로덕션 게이트웨이는 2026-06-20 srv4 로 이사했으므로, 게시 스텝이 빌드 후 최신 APK+version.json 을 **ssh/rsync 로 `srv4:~/.cache/deneb-apk/` 에 동기화**한다 (러너 로컬 serve dir 는 스테이징일 뿐, OTA 는 srv4 게이트웨이가 서빙). **단 CI 는 native-app 스모크 게이트를 건너뛴다**(`DENEB_SKIP_SMOKE=1`) — 무인 환경에서 픽셀탭·실데이터 의존 스모크가 잘 튀어(실제로 메일상세 단계가 세션 드로어로 빠져 멀쩡한 빌드를 막은 이력) 자동발행을 망가뜨리기 때문. 렌더 크래시 방어는 네이티브 PR 의 사전 스모크·`renderPreviews`·컴파일/유닛테스트가 맡는다.

- **트리거**: main 에 `client-android/**` 변경이 머지될 때 자동. versionCode 단독화라 릴리스마다 바뀌는 버전 파일이 없어 게이트할 대상이 없다 — **네이티브 변경을 머지하는 것 자체가 새 빌드 발행 신호(연속 배포)**. 수동 `workflow_dispatch`(노트 입력)도 가능. **fork PR 로는 절대 안 돈다** (호스트 러너에서 미신뢰 코드 실행 차단).
- **노트**: dispatch 입력 우선, 없으면 head 커밋 제목. 사용자에게 보이는 정돈된 한국어 changelog 는 어차피 컴파일된 `DenebPatchNotes` 가 오프라인으로 보여주므로 version.json 노트는 보조다.
- **버전 bump 불필요**: versionName 제거로 릴리스 시 손댈 버전 파일이 사라졌다. `DenebPatchNotes` 는 version 라벨 없는 시간순 changelog 라 빌드마다 강제 갱신 불필요(테스트는 비어있지 않으면 통과) — **사용자 영향 변경 때만** 노트를 추가. *"release/publish 는 명시 승인"* 원칙은 "네이티브 변경 PR 을 머지하는 행위" 가 그 승인.
- **★ 패치노트는 조각파일로 (충돌 방지)**: 사용자 표시 노트는 `DenebPatchNotes.kt` 의 frozen 히스토리에 prepend하지 **말고**, `client-android/app/changelog.d/YYYY-MM-DD-<slug>.md` 조각파일을 **새로 추가**한다(PR마다 새 파일 → 같은 줄 안 건드림 → 머지 충돌 0). 빌드 시 `composeApp/build.gradle.kts` 가 조각들을 파일명 역순(최신 먼저)으로 모아 `build/generated/.../DenebChangelogGenerated.kt`(`GENERATED_CHANGELOG_FRAGMENTS`)로 생성하고(**커밋 안 함** — 커밋하면 그 파일이 다시 공유 prepend 파일이 됨), `DENEB_PATCH_NOTES = GENERATED_CHANGELOG_FRAGMENTS + frozen 히스토리`로 합쳐 버전 카드가 그대로 읽는다. 한 줄=하이라이트 1개, `#` 줄=주석. 상세=`changelog.d/README.md`. 내부/빌드/테스트-only 변경엔 조각 불필요. ★이 규약은 **PR 게이트로 강제**된다: `.github/workflows/patch-notes-gate.yml` 이 `feat(native|miniapp|calendar|markdown|chat)` 제목 + `client-android/` 변경 PR 에서 조각(또는 히스토리 편집) 부재 시 실패한다 — 사용자 영향 없는 예외는 `skip-patch-notes` 라벨로 우회.
- **교착 방지**: `publish-apk.sh` 의 flock 은 `-w 600`(10분 대기 상한). 이전 발행이 hang 채 락을 쥐면 후속(자동·수동)이 job 30분 timeout 까지 막히던 사고(2026-06-08 좀비 publish 가 serve-dir 락 점유 → CI 포함 전 발행 블록)를 막아 빠르게 실패한다. 락 점유 의심 시 `fuser ~/.cache/deneb-apk/.publish.lock` 로 holder 확인 후 정리.

### srv1(구 gx10) self-hosted 러너 1회 셋업 (운영자만)

워크플로가 `runs-on: [self-hosted, gx10]` 이라 srv1 에 러너가 등록돼야 동작한다 (호스트명은 srv1 로 개명됐지만 **러너 라벨은 리터럴 `gx10` 그대로**다 — 라벨을 바꾸려면 러너 재등록 + 워크플로 + actionlint.yaml 세 곳 동시 수정). 러너의 `~/.cache/deneb-apk` 는 **로컬 스테이징**이고, 게시 스텝이 ssh/rsync 로 `srv4:~/.cache/deneb-apk/` (게이트웨이가 읽는 실제 serve dir)에 동기화한다 — 러너 계정에 **srv4 ssh 접근**이 있어야 OTA 에 뜬다.

```bash
# srv1 에서 choiceoh 로. URL/토큰은 GitHub > Settings > Actions > Runners > New self-hosted runner 에서 복사.
mkdir -p ~/actions-runner && cd ~/actions-runner
curl -o runner.tar.gz -L <runner-linux-arm64-tarball-url>
tar xzf runner.tar.gz
./config.sh --url https://github.com/choiceoh/deneb \
  --token <REG_TOKEN> --labels gx10 --name gx10-apk --unattended
sudo ./svc.sh install choiceoh && sudo ./svc.sh start   # 재부팅 후 자동 상주
```

- 호스트 전제(이미 충족): `~/android-sdk`(ANDROID_HOME 기본), JDK 21, Xvfb/matchbox 등 스모크 하네스 의존(`native-live-app.md`), **srv4 ssh 접근**(게시 동기화), 스모크는 srv4 프로덕션 게이트웨이에 원격으로 붙는다(하네스 기본 URL).
- 레포 변수 `DENEB_APK_BASE_URL` 를 게이트웨이 도달 base 로 설정(`Settings > Secrets and variables > Actions > Variables`). 미설정이어도 동작하나 version.json url 이 로컬 기본값이 된다(인앱 업데이터는 게이트웨이 다운로드 라우트로 받으므로 무해).
- 커스텀 라벨 `gx10` 은 `.github/actionlint.yaml` 에 등록돼 있어 워크플로 린트(`workflow-sanity.yml`)를 통과한다.
