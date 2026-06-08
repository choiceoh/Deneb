---
description: "릴리스, 어드바이저리, 프로덕션 배포 워크플로우"
globs: ["scripts/release*", "scripts/deploy*", "scripts/dev/publish-apk.sh", "client-android/app/androidApp/build.gradle.kts", "client-android/app/composeApp/build.gradle.kts", ".github/workflows/release*", ".github/workflows/publish-apk.yml"]
---

# Release & Advisory Workflows

- Release and publish remain explicit-approval actions.

# Production Deployment

## DGX Spark Production Build

- `make gateway-prod` — Full production binary (output: `dist/deneb-gateway`).

## DGX Spark Operations

- Restart gateway: `pkill -9 -f deneb-gateway || true; nohup ./gateway-go/deneb-gateway --bind loopback --port 18789 > /tmp/deneb-gateway.log 2>&1 &`
- Verify: `ss -ltnp | rg 18789`, `tail -n 120 /tmp/deneb-gateway.log`.

## Native Client APK Publishing

> 여러 에이전트 worktree가 공유 serve dir(`~/.cache/deneb-apk`, http.server `:19010`)에 동시 배포한다. 충돌을 막는 장치가 코드에 있으니 **반드시 단일 스크립트로만 배포**한다.

- **APK 배포는 `scripts/dev/publish-apk.sh` 경유만.** 직접 `assembleFossDebug` + `cp` + 수동 `version.json` 작성 금지 — 동시 빌드가 같은 파일명을 서로 덮어쓴다 (실제로 두 세션의 155 빌드가 충돌한 이력).
- APK 파일명에 **커밋 해시**가 박힌다 (`deneb-<ver>-<code>-<sha>-fossDebug.apk`, `androidApp/build.gradle.kts`). 다른 커밋 빌드는 안 덮어쓰고 전부 보존된다.
- 스크립트가 빌드 + serve dir 복사 + `version.json`(실제 산출물의 code/name/url) 생성을 한 번에 한다.
- **빌드 전 스모크 게이트(자동)**: 빌드에 들어가기 전에 `native-app-smoke.sh`(라이브 화면 워크)를 돌려 런타임 렌더 크래시(#1959류)를 막는다. 크래시/wrong-screen 감지 시 publish 중단, 하네스 기동 불가 시 warn+continue, `DENEB_SKIP_SMOKE=1` 로 우회. 상세: `.claude/rules/native-live-app.md`.
- env: `DENEB_APK_DIR`(기본 `~/.cache/deneb-apk`), `DENEB_APK_BASE_URL`(기본 localhost — 배포 머신에서 tailnet URL로 export), `ANDROID_HOME`, `DENEB_SKIP_SMOKE`(스모크 게이트 우회).
- **versionCode는 수동 bump 불필요** — `publish-apk.sh`가 게시 시 자동 할당한다. flock으로 직렬화한 채 공유 serve dir의 최대 code + 1(libs 값을 바닥으로)을 골라 `-PdenebVersionCode`로 gradle 두 모듈(androidApp `versionCode` + composeApp `BuildKonfig`)에 주입하므로, 동시에 게시하는 두 worktree가 같은 code를 잡는 사고(155/162/164 충돌)가 구조적으로 불가능하다. 이 code는 APK versionCode·파일명·`DenebUpdate.kt`의 `DENEB_VERSION_CODE`(= 생성된 `Version.appVersionCode`, PR #1965)에 모두 일관 반영된다. `libs.versions.toml`의 `android-versionCode`는 이제 floor/IDE 기본값일 뿐 — **릴리스마다 손대야 하는 건 `appVersion`(versionName)뿐**이다 (인앱 업데이트는 strictly-greater 비교).

```bash
# 새 빌드 배포 (배포 머신)
DENEB_APK_BASE_URL=http://<gateway-host>:19010 \
  scripts/dev/publish-apk.sh "인앱 업데이트에 표시될 릴리스 노트"
```

## Automated OTA publish (GitHub Action)

> `.github/workflows/publish-apk.yml` 는 위 `publish-apk.sh` 를 **gx10 self-hosted 러너**에서 그대로 실행한다. 빌드·스모크 게이트·flock versionCode·serve dir 동작이 수동 배포와 100% 동일한, 재구현이 아닌 얇은 트리거다 (게이트웨이가 호스트 로컬 디스크에서 APK 를 서빙하고 스모크가 그 호스트의 Xvfb+게이트웨이를 요구하므로 GitHub-hosted 러너로는 불가능 → self-hosted 필연).

- **트리거**: main 에 `client-android/app/gradle/libs.versions.toml` 변경이 머지될 때 자동(= `appVersion` bump = 의도된 릴리스만). 비릴리스 네이티브 커밋(테스트·리팩터)은 versionCode 만 올려 사용자에게 무의미한 업데이트 알림을 쏘므로 일부러 제외했다. 수동 `workflow_dispatch`(노트 입력)도 가능. **fork PR 로는 절대 안 돈다** (호스트 러너에서 미신뢰 코드 실행 차단).
- **노트**: dispatch 입력 우선, 없으면 head 커밋 제목. 사용자에게 보이는 정돈된 한국어 패치노트는 어차피 컴파일된 `DenebPatchNotes` 가 오프라인으로 보여주므로 version.json 노트는 보조다.
- **여전히 사람이 하는 것**: `appVersion`(versionName) bump + `DenebPatchNotes` head 항목은 PR 에서 손으로 (테스트가 head==appVersion 강제). 액션은 머지된 그 버전을 빌드·게시할 뿐 — *"release/publish 는 명시 승인"* 원칙은 "버전을 올린 PR 을 머지하는 행위" 가 그 승인이 되는 형태로 유지된다.

### gx10 self-hosted 러너 1회 셋업 (운영자만)

워크플로가 `runs-on: [self-hosted, gx10]` 이라 gx10 에 러너가 등록돼야 동작한다. **게이트웨이와 같은 사용자(choiceoh)로 실행**해야 `~/.cache/deneb-apk` 가 게이트웨이가 읽는 serve dir 와 일치한다 (HOME 이 다르면 게시해도 OTA 에 안 뜬다).

```bash
# gx10 에서 choiceoh 로. URL/토큰은 GitHub > Settings > Actions > Runners > New self-hosted runner 에서 복사.
mkdir -p ~/actions-runner && cd ~/actions-runner
curl -o runner.tar.gz -L <runner-linux-arm64-tarball-url>
tar xzf runner.tar.gz
./config.sh --url https://github.com/choiceoh/deneb \
  --token <REG_TOKEN> --labels gx10 --name gx10-apk --unattended
sudo ./svc.sh install choiceoh && sudo ./svc.sh start   # 재부팅 후 자동 상주
```

- 호스트 전제(이미 충족): `~/android-sdk`(ANDROID_HOME 기본), JDK 21, Xvfb/matchbox 등 스모크 하네스 의존(`native-live-app.md`), **프로덕션 게이트웨이 가동 중**(스모크가 붙는다).
- 레포 변수 `DENEB_APK_BASE_URL` 를 게이트웨이 도달 base 로 설정(`Settings > Secrets and variables > Actions > Variables`). 미설정이어도 동작하나 version.json url 이 로컬 기본값이 된다(인앱 업데이터는 게이트웨이 다운로드 라우트로 받으므로 무해).
- 커스텀 라벨 `gx10` 은 `.github/actionlint.yaml` 에 등록돼 있어 워크플로 린트(`workflow-sanity.yml`)를 통과한다.
