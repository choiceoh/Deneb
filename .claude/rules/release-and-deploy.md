---
description: "릴리스, 어드바이저리, 프로덕션 배포 워크플로우"
globs: ["scripts/release*", "scripts/deploy*", "scripts/dev/publish-apk.sh", "client-android/app/androidApp/build.gradle.kts", "client-android/app/composeApp/build.gradle.kts", ".github/workflows/release*"]
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
